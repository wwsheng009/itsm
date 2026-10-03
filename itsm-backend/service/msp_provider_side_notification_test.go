package service

// A12 通知双投递（provider 侧）单测：
//   - 托管工单的 created/assigned/status_changed/commented 事件在客户侧之外，
//     向 provider 租户投递（托管处理人 + provider 管理员；排除 actor / 停用 / 非 provider 用户）；
//   - 非托管工单、provider 租户停用/类型非法、脏 managed_by_user_id → fail-closed 不投递；
//   - Tx 变体与主表同事务（提交同生、回滚同死）。

import (
	"context"
	"fmt"
	"testing"
	"time"

	_ "github.com/mattn/go-sqlite3"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap/zaptest"

	"itsm-backend/ent"
	"itsm-backend/ent/enttest"
	entNotification "itsm-backend/ent/notification"
	entTicket "itsm-backend/ent/ticket"
	entUser "itsm-backend/ent/user"
)

type dualDeliveryEnv struct {
	client        *ent.Client
	provider      *ent.Tenant
	customer      *ent.Tenant
	otherTenant   *ent.Tenant
	requester     *ent.User
	providerAdmin *ent.User
	tech          *ent.User
	techOther     *ent.User
	adminInactive *ent.User
	foreignTech   *ent.User
	ticket        *ent.Ticket
	svc           *TicketNotificationService
}

func newDualDeliveryEnv(t *testing.T) *dualDeliveryEnv {
	t.Helper()
	dsn := fmt.Sprintf("file:dual_delivery_%d?mode=memory&cache=shared&_fk=1", time.Now().UnixNano())
	client := enttest.Open(t, "sqlite3", dsn)
	t.Cleanup(func() { _ = client.Close() })
	ctx := context.Background()

	provider, err := client.Tenant.Create().
		SetName("P1").SetCode("p1").SetDomain("p1.example.com").
		SetType("msp_provider").SetStatus("active").Save(ctx)
	require.NoError(t, err)
	customer, err := client.Tenant.Create().
		SetName("C1").SetCode("c1").SetDomain("c1.example.com").
		SetType("msp_customer").SetStatus("active").SetMspProviderID(provider.ID).Save(ctx)
	require.NoError(t, err)
	otherTenant, err := client.Tenant.Create().
		SetName("C2").SetCode("c2").SetDomain("c2.example.com").
		SetType("standard").SetStatus("active").Save(ctx)
	require.NoError(t, err)

	mkUser := func(username string, tenantID int, role string, mspRole string, active bool) *ent.User {
		builder := client.User.Create().
			SetUsername(username).SetEmail(username + "@example.com").SetName(username).
			SetPasswordHash("hash").SetActive(active).SetTenantID(tenantID).SetRole(entUser.Role(role))
		if mspRole != "" {
			builder = builder.SetMspRole(entUser.MspRole(mspRole))
		}
		u, err := builder.Save(ctx)
		require.NoError(t, err)
		return u
	}

	env := &dualDeliveryEnv{
		client:        client,
		provider:      provider,
		customer:      customer,
		otherTenant:   otherTenant,
		requester:     mkUser("dd-requester", customer.ID, "end_user", "", true),
		providerAdmin: mkUser("dd-provider-admin", provider.ID, "admin", "provider_admin", true),
		tech:          mkUser("dd-tech", provider.ID, "agent", "provider_agent", true),
		techOther:     mkUser("dd-tech-other", provider.ID, "agent", "provider_agent", true),
		adminInactive: mkUser("dd-provider-admin-off", provider.ID, "admin", "provider_admin", false),
		foreignTech:   mkUser("dd-foreign-tech", otherTenant.ID, "agent", "provider_agent", true),
	}

	env.ticket, err = client.Ticket.Create().
		SetTicketNumber("DD-000001").SetTitle("双投递单测工单").SetType("incident").
		SetPriority("medium").SetStatus("open").
		SetRequesterID(env.requester.ID).SetAssigneeID(env.tech.ID).SetTenantID(customer.ID).
		SetIsManagedByMsp(true).SetMspProviderID(provider.ID).SetManagedByUserID(env.tech.ID).
		Save(ctx)
	require.NoError(t, err)
	env.svc = NewTicketNotificationService(client, zaptest.NewLogger(t).Sugar())
	return env
}

// providerRecipientIDs 返回 provider 租户内、工作台深链通知的收件人集合。
func (e *dualDeliveryEnv) providerRecipientIDs(t *testing.T) map[int]bool {
	t.Helper()
	rows, err := e.client.Notification.Query().
		Where(
			entNotification.TenantIDEQ(e.provider.ID),
			entNotification.ActionURLEQ(mspWorkbenchActionURL),
		).
		All(context.Background())
	require.NoError(t, err)
	ids := map[int]bool{}
	for _, r := range rows {
		ids[r.UserID] = true
	}
	return ids
}

func TestMSPDualDelivery_Commented(t *testing.T) {
	env := newDualDeliveryEnv(t)
	ctx := context.Background()

	require.NoError(t, env.svc.NotifyTicketCommented(ctx, env.ticket.ID, env.requester.ID, nil, env.customer.ID))

	ids := env.providerRecipientIDs(t)
	assert.True(t, ids[env.tech.ID], "托管处理人应收到 provider 侧通知")
	assert.True(t, ids[env.providerAdmin.ID], "provider 管理员应收到 provider 侧通知")
	assert.False(t, ids[env.techOther.ID], "非处理人 provider 员工不应收到")
	assert.False(t, ids[env.adminInactive.ID], "停用管理员不应收到")
	assert.False(t, ids[env.requester.ID], "客户用户不应出现在 provider 侧投递")

	// 客户侧仍按既有语义投递（requester + assignee）。
	customerRows, err := env.client.Notification.Query().
		Where(entNotification.TenantIDEQ(env.customer.ID)).
		Order(ent.Asc(entNotification.FieldID)).All(ctx)
	require.NoError(t, err)
	require.NotEmpty(t, customerRows)
	assert.Equal(t, env.requester.ID, customerRows[0].UserID)

	// actor 排除：评论者 = 托管处理人时，provider 侧不再给自己发。
	_, err = env.client.Notification.Delete().
		Where(entNotification.TenantIDEQ(env.provider.ID)).Exec(ctx)
	require.NoError(t, err)
	require.NoError(t, env.svc.NotifyTicketCommented(ctx, env.ticket.ID, env.tech.ID, nil, env.customer.ID))
	ids = env.providerRecipientIDs(t)
	assert.False(t, ids[env.tech.ID], "评论者本人应从 provider 侧排除")
	assert.True(t, ids[env.providerAdmin.ID])
}

func TestMSPDualDelivery_AssignedAndStatusChanged(t *testing.T) {
	env := newDualDeliveryEnv(t)
	ctx := context.Background()

	require.NoError(t, env.svc.NotifyTicketAssigned(ctx, env.ticket.ID, env.techOther.ID, env.customer.ID))
	ids := env.providerRecipientIDs(t)
	assert.True(t, ids[env.tech.ID], "库内托管处理人应收到指派通知")
	assert.True(t, ids[env.providerAdmin.ID])
	assert.False(t, ids[env.techOther.ID], "收件人以库内快照为准，未落库的 assignee 参数不投递")

	require.NoError(t, env.svc.NotifyTicketStatusChanged(ctx, env.ticket.ID, "open", "in_progress", env.customer.ID))
	requesterRows, err := env.client.Notification.Query().
		Where(
			entNotification.TenantIDEQ(env.customer.ID),
			entNotification.UserIDEQ(env.requester.ID),
			entNotification.TitleEQ("status_changed"),
		).All(ctx)
	require.NoError(t, err)
	require.NotEmpty(t, requesterRows, "客户侧 requester 应收到状态变更通知")

	providerRows, err := env.client.Notification.Query().
		Where(
			entNotification.TenantIDEQ(env.provider.ID),
			entNotification.TitleEQ("status_changed"),
		).All(ctx)
	require.NoError(t, err)
	assert.Len(t, providerRows, 2, "provider 侧应收到 tech + provider_admin 两份状态通知")
	for _, row := range providerRows {
		assert.Equal(t, mspWorkbenchActionURL, row.ActionURL)
	}
}

func TestMSPDualDelivery_Guards(t *testing.T) {
	env := newDualDeliveryEnv(t)
	ctx := context.Background()

	// ① 非托管工单：不投递。
	plain, err := env.client.Ticket.Create().
		SetTicketNumber("DD-PLAIN").SetTitle("普通工单").SetType("incident").
		SetPriority("low").SetStatus("open").
		SetRequesterID(env.requester.ID).SetTenantID(env.customer.ID).Save(ctx)
	require.NoError(t, err)
	require.NoError(t, env.svc.NotifyTicketCommented(ctx, plain.ID, env.requester.ID, nil, env.customer.ID))
	assert.Empty(t, env.providerRecipientIDs(t))

	// ② provider 租户停用：不投递。
	require.NoError(t, env.client.Tenant.UpdateOneID(env.provider.ID).SetStatus("suspended").Exec(ctx))
	require.NoError(t, env.svc.NotifyTicketCommented(ctx, env.ticket.ID, env.requester.ID, nil, env.customer.ID))
	assert.Empty(t, env.providerRecipientIDs(t))

	// ③ provider 租户类型非法：不投递。
	require.NoError(t, env.client.Tenant.UpdateOneID(env.provider.ID).SetStatus("active").SetType("standard").Exec(ctx))
	require.NoError(t, env.svc.NotifyTicketCommented(ctx, env.ticket.ID, env.requester.ID, nil, env.customer.ID))
	assert.Empty(t, env.providerRecipientIDs(t))

	// ④ 脏 managed_by_user_id（他租户用户）→ 仅 provider 管理员收到，不跨租户投递。
	require.NoError(t, env.client.Tenant.UpdateOneID(env.provider.ID).SetType("msp_provider").Exec(ctx))
	require.NoError(t, env.client.Ticket.UpdateOneID(env.ticket.ID).
		SetManagedByUserID(env.foreignTech.ID).SetAssigneeID(env.foreignTech.ID).Exec(ctx))
	require.NoError(t, env.svc.NotifyTicketCommented(ctx, env.ticket.ID, env.requester.ID, nil, env.customer.ID))
	ids := env.providerRecipientIDs(t)
	assert.False(t, ids[env.foreignTech.ID], "他租户用户不得进入 provider 侧投递")
	assert.True(t, ids[env.providerAdmin.ID])
}

func TestMSPDualDelivery_CreatedTxAtomic(t *testing.T) {
	env := newDualDeliveryEnv(t)
	ctx := context.Background()
	env.svc.EnableTxOutbox()

	newTicket := func(tx *ent.Tx, number string) *ent.Ticket {
		tk, err := tx.Ticket.Create().
			SetTicketNumber(number).SetTitle("Tx 双投递工单").SetType("incident").
			SetPriority("medium").SetStatus("open").
			SetRequesterID(env.requester.ID).SetAssigneeID(env.tech.ID).SetTenantID(env.customer.ID).
			SetIsManagedByMsp(true).SetMspProviderID(env.provider.ID).SetManagedByUserID(env.tech.ID).
			Save(ctx)
		require.NoError(t, err)
		return tk
	}

	// 提交：provider 侧行与工单同生。
	tx, err := env.client.Tx(ctx)
	require.NoError(t, err)
	require.NoError(t, env.svc.NotifyTicketCreatedTx(ctx, tx, newTicket(tx, "DD-TX-1")))
	require.NoError(t, tx.Commit())
	ids := env.providerRecipientIDs(t)
	assert.True(t, ids[env.tech.ID], "托管处理人应收到 provider 侧 created 通知")
	assert.True(t, ids[env.providerAdmin.ID])

	// 回滚：provider 侧行与工单同死。
	before, err := env.client.Notification.Query().
		Where(entNotification.TenantIDEQ(env.provider.ID)).Count(ctx)
	require.NoError(t, err)
	tx2, err := env.client.Tx(ctx)
	require.NoError(t, err)
	tk2 := newTicket(tx2, "DD-TX-2")
	require.NoError(t, env.svc.NotifyTicketCreatedTx(ctx, tx2, tk2))
	require.NoError(t, tx2.Rollback())
	after, err := env.client.Notification.Query().
		Where(entNotification.TenantIDEQ(env.provider.ID)).Count(ctx)
	require.NoError(t, err)
	assert.Equal(t, before, after, "回滚后 provider 侧通知数不应变化")
	exists, err := env.client.Ticket.Query().Where(entTicket.IDEQ(tk2.ID)).Exist(ctx)
	require.NoError(t, err)
	assert.False(t, exists, "回滚后工单不应存在")
}
