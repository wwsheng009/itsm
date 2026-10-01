package service

import (
	"context"
	"fmt"
	"sync/atomic"
	"testing"
	"time"

	"itsm-backend/dto"
	"itsm-backend/ent"
	"itsm-backend/ent/enttest"
	entTenant "itsm-backend/ent/tenant"
	entUser "itsm-backend/ent/user"
	"itsm-backend/middleware"

	_ "github.com/mattn/go-sqlite3"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap/zaptest"
)

var workbenchDBCounter int64

type workbenchEnv struct {
	client  *ent.Client
	svc     *MSPWorkbenchService
	actor   MSPWorkbenchActor
	a, b, c *ent.Tenant
	tA      *ent.Ticket // A：open，未指派，SLA 已超期（slaRisk）
	tA2     *ent.Ticket // A：in_progress，已指派
	tB      *ent.Ticket // B：open（B 暂停 → 只读）
	tC      *ent.Ticket // C：未分配（不得出现在结果）
}

// newWorkbenchEnv 构造工作台夹具：
// provider（MSP）+ 客户 A（active，已分配）/ B（suspended，已分配）/ C（active，未分配）。
// 权限模式切到 Merge，使 msp_tech 走硬编码矩阵（IP-P0-9 已与 authz 词表对拍），
// 避免在 sqlite 夹具中重建 role_permissions 全量种子。
func newWorkbenchEnv(t *testing.T) *workbenchEnv {
	t.Helper()
	prevMode := middleware.PermissionConfig.Mode
	middleware.PermissionConfig.Mode = middleware.PermissionConfigModeMerge
	t.Cleanup(func() { middleware.PermissionConfig.Mode = prevMode })

	dsn := fmt.Sprintf("file:wbench_test_%d?mode=memory&cache=shared&_fk=1", atomic.AddInt64(&workbenchDBCounter, 1))
	client := enttest.Open(t, "sqlite3", dsn)
	t.Cleanup(func() { client.Close() })
	ctx := context.Background()

	tenant := func(name, code, typ, status string, providerID int) *ent.Tenant {
		tb := client.Tenant.Create().SetName(name).SetCode(code).SetType(entTenant.Type(typ)).SetStatus(status)
		if providerID > 0 {
			tb = tb.SetMspProviderID(providerID)
		}
		tn, err := tb.Save(ctx)
		require.NoError(t, err)
		return tn
	}
	mkUser := func(name string, tenantID int, mspRole string) *ent.User {
		ub := client.User.Create().
			SetUsername(name).SetEmail(name + "@example.com").SetName(name).
			SetPasswordHash("hash").SetTenantID(tenantID)
		if mspRole != "" {
			ub = ub.SetMspRole(entUser.MspRole(mspRole))
		}
		u, err := ub.Save(ctx)
		require.NoError(t, err)
		return u
	}

	provider := tenant("Provider", "wb-prov", "msp_provider", "active", 0)
	a := tenant("Customer A", "wb-a", "msp_customer", "active", provider.ID)
	b := tenant("Customer B", "wb-b", "msp_customer", "suspended", provider.ID)
	c := tenant("Customer C", "wb-c", "msp_customer", "active", provider.ID)

	mspUser := mkUser("wb-msp", provider.ID, "provider_agent")
	userA := mkUser("wb-user-a", a.ID, "")
	userB := mkUser("wb-user-b", b.ID, "")
	userC := mkUser("wb-user-c", c.ID, "")

	for _, pair := range [][2]int{{mspUser.ID, a.ID}, {mspUser.ID, b.ID}} {
		_, err := client.MSPAllocation.Create().
			SetMspUserID(pair[0]).SetCustomerTenantID(pair[1]).SetRole("primary").Save(ctx)
		require.NoError(t, err)
	}

	now := time.Now()
	past := now.Add(-2 * time.Hour)
	mkTicket := func(number, title, status string, tenantID, requesterID, assigneeID int, deadline *time.Time) *ent.Ticket {
		tb := client.Ticket.Create().
			SetTicketNumber(number).SetTitle(title).SetStatus(status).
			SetTenantID(tenantID).SetRequesterID(requesterID).SetPriority("high")
		if assigneeID > 0 {
			tb = tb.SetAssigneeID(assigneeID)
		}
		if deadline != nil {
			tb = tb.SetSLAResolutionDeadline(*deadline)
		}
		tk, err := tb.Save(ctx)
		require.NoError(t, err)
		return tk
	}
	tA := mkTicket("WB-A-1", "A 未指派", "open", a.ID, userA.ID, 0, &past)
	tA2 := mkTicket("WB-A-2", "A 已指派", "in_progress", a.ID, userA.ID, mspUser.ID, nil)
	tB := mkTicket("WB-B-1", "B 暂停只读", "open", b.ID, userB.ID, 0, nil)
	tC := mkTicket("WB-C-1", "C 未分配", "open", c.ID, userC.ID, 0, nil)

	svc := NewMSPWorkbenchService(client, NewTicketServiceForTest(client, zaptest.NewLogger(t).Sugar()), NewTicketCommentService(client, zaptest.NewLogger(t).Sugar()), zaptest.NewLogger(t).Sugar())

	return &workbenchEnv{
		client: client,
		svc:    svc,
		actor: MSPWorkbenchActor{
			UserID:           mspUser.ID,
			Username:         "wb-msp",
			HomeTenantID:     provider.ID,
			MSPRole:          "provider_agent",
			AllowedCustomers: []int{a.ID, b.ID},
		},
		a: a, b: b, c: c,
		tA: tA, tA2: tA2, tB: tB, tC: tC,
	}
}

func requireWorkbenchCode(t *testing.T, err error, want string) {
	t.Helper()
	ae, ok := AsCustomerAccessError(err)
	require.True(t, ok, "want CustomerAccessError, got %v", err)
	assert.Equal(t, want, ae.Code)
}

// TestWorkbenchListScopesAndAllowedActions 锁定 WB-A2/A3/A5 的服务端面：
// 未分配客户不出现在列表；暂停租户条目只读（allowedActions 全 false + CUSTOMER_INACTIVE）。
func TestWorkbenchListScopesAndAllowedActions(t *testing.T) {
	env := newWorkbenchEnv(t)
	resp, err := env.svc.ListTickets(context.Background(), env.actor, dto.WorkbenchTicketQuery{})
	require.NoError(t, err)
	require.Len(t, resp.Items, 3)

	byTenant := map[int][]dto.WorkbenchTicketItem{}
	for _, item := range resp.Items {
		byTenant[item.CustomerTenantID] = append(byTenant[item.CustomerTenantID], item)
		assert.NotEqual(t, env.c.ID, item.CustomerTenantID, "未分配客户 C 不得出现在列表")
	}
	require.Len(t, byTenant[env.a.ID], 2)
	require.Len(t, byTenant[env.b.ID], 1)

	for _, item := range byTenant[env.a.ID] {
		assert.Equal(t, "Customer A", item.CustomerName)
		require.Len(t, item.AllowedActions, 3)
		for _, act := range item.AllowedActions {
			assert.True(t, act.Allowed, "active 租户 + msp_tech 写权限：%s 应放行", act.Action)
			assert.Empty(t, act.ReasonCode)
		}
	}
	for _, item := range byTenant[env.b.ID] {
		for _, act := range item.AllowedActions {
			assert.False(t, act.Allowed, "暂停租户条目必须只读")
			assert.Equal(t, CodeCustomerInactive, act.ReasonCode)
		}
	}

	// 显式请求未分配客户 → 403 MSP_ALLOCATION_REQUIRED（防枚举，不返回“不存在”）。
	_, err = env.svc.ListTickets(context.Background(), env.actor, dto.WorkbenchTicketQuery{
		CustomerTenantIDs: []int{env.c.ID},
	})
	requireWorkbenchCode(t, err, CodeMSPAllocationRequired)

	// 非法游标 → INVALID_CURSOR。
	_, err = env.svc.ListTickets(context.Background(), env.actor, dto.WorkbenchTicketQuery{Cursor: "not-base64"})
	requireWorkbenchCode(t, err, CodeInvalidCursor)
}

// TestWorkbenchSummaryCounts 徽标计数：open/slaRisk/unassigned 与列表一致（WB-A2）。
func TestWorkbenchSummaryCounts(t *testing.T) {
	env := newWorkbenchEnv(t)
	resp, err := env.svc.Summary(context.Background(), env.actor)
	require.NoError(t, err)
	require.Len(t, resp.Customers, 2)
	byID := map[int]dto.WorkbenchSummaryCustomer{}
	for _, c := range resp.Customers {
		byID[c.CustomerTenantID] = c
	}
	a := byID[env.a.ID]
	assert.Equal(t, 2, a.Open)
	assert.Equal(t, 1, a.SLARisk, "SLA 已超期的 open 工单计 1")
	assert.Equal(t, 1, a.Unassigned)
	b := byID[env.b.ID]
	assert.Equal(t, 1, b.Open)
	assert.Equal(t, 0, b.SLARisk)
	assert.Equal(t, 1, b.Unassigned)
}

// TestWorkbenchWriteAuthorizationChain 条目级授权链（WB-A1/A5）：
// 租户不一致 → 400；暂停租户写 → 403 CUSTOMER_INACTIVE；正常写 → 落库 + 审计含 target_tenant。
func TestWorkbenchWriteAuthorizationChain(t *testing.T) {
	env := newWorkbenchEnv(t)
	ctx := context.Background()

	// 声明租户与资源实际租户不一致 → RESOURCE_TENANT_MISMATCH。
	_, err := env.svc.Reply(ctx, env.actor, env.tA.ID, dto.WorkbenchReplyRequest{CustomerTenantID: env.b.ID, Content: "hello"})
	requireWorkbenchCode(t, err, CodeResourceTenantMismatch)

	// 暂停租户 → CUSTOMER_INACTIVE（读可写不可）。
	_, err = env.svc.Reply(ctx, env.actor, env.tB.ID, dto.WorkbenchReplyRequest{CustomerTenantID: env.b.ID, Content: "hello"})
	requireWorkbenchCode(t, err, CodeCustomerInactive)

	// 正常回复：评论落库 + 审计含 source=workbench / target_tenant_id。
	comment, err := env.svc.Reply(ctx, env.actor, env.tA.ID, dto.WorkbenchReplyRequest{CustomerTenantID: env.a.ID, Content: "MSP 已受理"})
	require.NoError(t, err)
	require.NotNil(t, comment)
	count, err := env.client.TicketComment.Query().Count(ctx)
	require.NoError(t, err)
	assert.Equal(t, 1, count)

	logs, err := env.client.AuditLog.Query().All(ctx)
	require.NoError(t, err)
	require.Len(t, logs, 1)
	assert.Equal(t, env.a.ID, logs[0].TenantID)
	require.NotNil(t, logs[0].RequestBody)
	assert.Contains(t, *logs[0].RequestBody, "workbench")
	assert.Contains(t, *logs[0].RequestBody, fmt.Sprintf("%d", env.a.ID))
	assert.Equal(t, "WORKBENCH_REPLY", logs[0].Action)

	// 状态流转：open → in_progress 合法；终态/暂停场景由资源状态与授权链拦截。
	updated, err := env.svc.ChangeStatus(ctx, env.actor, env.tA.ID, dto.WorkbenchStatusRequest{CustomerTenantID: env.a.ID, Status: "in_progress"})
	require.NoError(t, err)
	require.NotNil(t, updated)
	assert.Equal(t, "in_progress", string(updated.Status))
}

// TestWorkbenchCursorPagination 游标翻页无重复、无遗漏（WB-A2 刷新/分享 URL 基础）。
func TestWorkbenchCursorPagination(t *testing.T) {
	env := newWorkbenchEnv(t)
	ctx := context.Background()

	seen := map[int]bool{}
	page := 0
	cursor := ""
	for {
		resp, err := env.svc.ListTickets(ctx, env.actor, dto.WorkbenchTicketQuery{Limit: 2, Cursor: cursor})
		require.NoError(t, err)
		for _, item := range resp.Items {
			require.False(t, seen[item.ID], "游标翻页出现重复条目 id=%d", item.ID)
			seen[item.ID] = true
		}
		page++
		if resp.NextCursor == "" {
			break
		}
		cursor = resp.NextCursor
		require.LessOrEqual(t, page, 5, "游标未收敛，疑似死循环")
	}
	assert.Len(t, seen, 3, "A×2 + B×1 共 3 条全部返回")
}
