package service

import (
	"context"
	"fmt"
	"testing"
	"time"

	"itsm-backend/ent"
	"itsm-backend/ent/enttest"
	"itsm-backend/ent/notification"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap/zaptest"
)

// ncSetup 构造最小租户/用户环境。
func ncSetup(t *testing.T) (*ent.Client, context.Context, *ent.Tenant, *ent.User) {
	t.Helper()
	client := enttest.Open(t, "sqlite3", testDSN())
	t.Cleanup(func() { _ = client.Close() })
	ctx := context.Background()

	tenant, err := client.Tenant.Create().
		SetName("Notify Cleanup Tenant").SetCode("nc-tenant").SetDomain("nc.test").SetStatus("active").
		Save(ctx)
	require.NoError(t, err)

	user, err := client.User.Create().
		SetUsername("nc-user").SetEmail("nc@x.com").SetName("NC User").
		SetPasswordHash("h").SetRole("admin").SetActive(true).SetTenantID(tenant.ID).
		Save(ctx)
	require.NoError(t, err)

	return client, ctx, tenant, user
}

func ncCreateNotification(t *testing.T, ctx context.Context, client *ent.Client, tenantID, userID int, actionURL string) *ent.Notification {
	t.Helper()
	row, err := client.Notification.Create().
		SetTitle("t").SetMessage("m").SetType("info").
		SetUserID(userID).SetTenantID(tenantID).
		SetActionURL(actionURL).SetActionText("查看").
		Save(ctx)
	require.NoError(t, err)
	return row
}

func ncCreateTicket(t *testing.T, ctx context.Context, client *ent.Client, tenantID, requesterID int, number string) *ent.Ticket {
	t.Helper()
	tk, err := client.Ticket.Create().
		SetTitle("ticket " + number).SetDescription("d").SetPriority("low").SetStatus("open").
		SetTicketNumber(number).SetRequesterID(requesterID).SetTenantID(tenantID).
		Save(ctx)
	require.NoError(t, err)
	return tk
}

func ncExists(t *testing.T, ctx context.Context, client *ent.Client, actionURL string) bool {
	t.Helper()
	ok, err := client.Notification.Query().Where(notification.ActionURLEQ(actionURL)).Exist(ctx)
	require.NoError(t, err)
	return ok
}

// TestCleanupResourceNotifications_TicketPatterns 验证工单深链（含查询串/子路径/MSP 工作台
// 深链两种参数顺序）被清理，相邻编号与其它资源不受影响。
func TestCleanupResourceNotifications_TicketPatterns(t *testing.T) {
	client, ctx, tenant, user := ncSetup(t)
	tkA := ncCreateTicket(t, ctx, client, tenant.ID, user.ID, "NC-A")
	tkB := ncCreateTicket(t, ctx, client, tenant.ID, user.ID, "NC-B")

	targetURLs := []string{
		fmt.Sprintf("/tickets/%d", tkA.ID),
		fmt.Sprintf("/tickets/%d?from=notification", tkA.ID),
		fmt.Sprintf("/tickets/%d/comments", tkA.ID),
		fmt.Sprintf("/msp/workbench?ticket=%d&customerTenantId=%d", tkA.ID, tenant.ID),
		fmt.Sprintf("/msp/workbench?customerTenantId=%d&ticket=%d", tkA.ID, tenant.ID),
	}
	keepURLs := []string{
		fmt.Sprintf("/tickets/%d", tkB.ID),
		fmt.Sprintf("/tickets/%d1", tkA.ID), // 相邻编号边界：225 vs 2251
		"/dashboard",
	}
	for _, u := range targetURLs {
		ncCreateNotification(t, ctx, client, tenant.ID, user.ID, u)
	}
	for _, u := range keepURLs {
		ncCreateNotification(t, ctx, client, tenant.ID, user.ID, u)
	}

	deleted, err := CleanupResourceNotifications(ctx, client, NotificationResourceTicket, tkA.ID)
	require.NoError(t, err)
	assert.Equal(t, len(targetURLs), deleted)

	for _, u := range targetURLs {
		assert.False(t, ncExists(t, ctx, client, u), "应已清理: %s", u)
	}
	for _, u := range keepURLs {
		assert.True(t, ncExists(t, ctx, client, u), "不应误删: %s", u)
	}
}

// TestCleanupResourceNotifications_ChangeAndServiceRequest 验证变更/服务请求深链清理。
func TestCleanupResourceNotifications_ChangeAndServiceRequest(t *testing.T) {
	client, ctx, tenant, user := ncSetup(t)

	changeRow, err := client.Change.Create().
		SetTitle("nc change").SetDescription("d").SetCreatedBy(user.ID).SetTenantID(tenant.ID).
		Save(ctx)
	require.NoError(t, err)

	sr, err := client.ServiceRequest.Create().
		SetTitle("nc sr").SetCatalogID(1).SetRequesterID(user.ID).SetTenantID(tenant.ID).
		Save(ctx)
	require.NoError(t, err)

	changeTarget := fmt.Sprintf("/changes/%d", changeRow.ID)
	changeKeep := fmt.Sprintf("/changes/%d1", changeRow.ID)
	srTarget := fmt.Sprintf("/service-requests/%d", sr.ID)
	srKeep := fmt.Sprintf("/service-requests/%d1", sr.ID)
	for _, u := range []string{changeTarget, changeKeep, srTarget, srKeep} {
		ncCreateNotification(t, ctx, client, tenant.ID, user.ID, u)
	}

	deleted, err := CleanupResourceNotifications(ctx, client, NotificationResourceChange, changeRow.ID)
	require.NoError(t, err)
	assert.Equal(t, 1, deleted)
	assert.False(t, ncExists(t, ctx, client, changeTarget))
	assert.True(t, ncExists(t, ctx, client, changeKeep))

	deleted, err = CleanupResourceNotifications(ctx, client, NotificationResourceServiceRequest, sr.ID)
	require.NoError(t, err)
	assert.Equal(t, 1, deleted)
	assert.False(t, ncExists(t, ctx, client, srTarget))
	assert.True(t, ncExists(t, ctx, client, srKeep))

	_, err = CleanupResourceNotifications(ctx, client, NotificationResourceType("unknown"), 1)
	assert.Error(t, err)
}

// TestScanAndCleanupOrphanNotifications 验证孤儿扫描：目标不存在或已软删均为孤儿；
// 有效目标与无深链通知不受影响。
func TestScanAndCleanupOrphanNotifications(t *testing.T) {
	client, ctx, tenant, user := ncSetup(t)
	alive := ncCreateTicket(t, ctx, client, tenant.ID, user.ID, "NC-ALIVE")
	softDeleted := ncCreateTicket(t, ctx, client, tenant.ID, user.ID, "NC-SOFT")
	_, err := client.Ticket.UpdateOneID(softDeleted.ID).SetDeletedAt(time.Now()).Save(ctx)
	require.NoError(t, err)

	validURL := fmt.Sprintf("/tickets/%d", alive.ID)
	softURL := fmt.Sprintf("/tickets/%d", softDeleted.ID)
	missingURL := "/tickets/999999"
	missingChangeURL := "/changes/888888"
	for _, u := range []string{validURL, softURL, missingURL, missingChangeURL, "/dashboard"} {
		ncCreateNotification(t, ctx, client, tenant.ID, user.ID, u)
	}

	orphans, err := ScanOrphanResourceNotifications(ctx, client, tenant.ID, 0)
	require.NoError(t, err)
	require.Len(t, orphans, 3)
	got := map[string]bool{}
	for _, o := range orphans {
		got[o.ActionURL] = true
	}
	assert.True(t, got[softURL], "软删工单的通知应判为孤儿")
	assert.True(t, got[missingURL])
	assert.True(t, got[missingChangeURL])
	assert.False(t, got[validURL])

	cleaned, err := CleanupOrphanResourceNotifications(ctx, client, tenant.ID, 0)
	require.NoError(t, err)
	assert.Len(t, cleaned, 3)
	assert.True(t, ncExists(t, ctx, client, validURL), "有效工单通知应保留")
	assert.True(t, ncExists(t, ctx, client, "/dashboard"), "非单据深链通知应保留")
	assert.False(t, ncExists(t, ctx, client, softURL))
	assert.False(t, ncExists(t, ctx, client, missingURL))
}

// TestTicketServiceDelete_CleansNotifications 验证产品删除路径联动清理：
// TicketService.DeleteTicket 软删工单后，指向它的通知被清理，其它工单通知保留。
func TestTicketServiceDelete_CleansNotifications(t *testing.T) {
	client, ctx, tenant, user := ncSetup(t)
	logger := zaptest.NewLogger(t).Sugar()
	svc := NewTicketServiceForTest(client, logger)

	tk := ncCreateTicket(t, ctx, client, tenant.ID, user.ID, "NC-DEL")
	other := ncCreateTicket(t, ctx, client, tenant.ID, user.ID, "NC-OTHER")

	targetURL := fmt.Sprintf("/tickets/%d", tk.ID)
	keepURL := fmt.Sprintf("/tickets/%d", other.ID)
	mspURL := fmt.Sprintf("/msp/workbench?ticket=%d&customerTenantId=%d", tk.ID, tenant.ID)
	for _, u := range []string{targetURL, keepURL, mspURL} {
		ncCreateNotification(t, ctx, client, tenant.ID, user.ID, u)
	}

	require.NoError(t, svc.DeleteTicket(ctx, tk.ID, tenant.ID, user.ID, "admin"))

	raw, err := client.Ticket.Get(ctx, tk.ID)
	require.NoError(t, err)
	assert.NotNil(t, raw.DeletedAt, "工单应为软删")
	assert.False(t, ncExists(t, ctx, client, targetURL), "工单通知应随删除清理")
	assert.False(t, ncExists(t, ctx, client, mspURL), "MSP 深链通知应随删除清理")
	assert.True(t, ncExists(t, ctx, client, keepURL), "其它工单通知应保留")
}
