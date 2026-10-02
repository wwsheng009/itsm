package service

import (
	"context"
	"strconv"
	"testing"
	"time"

	_ "github.com/mattn/go-sqlite3"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap/zaptest"

	"itsm-backend/ent/enttest"
	"itsm-backend/ent/tenant"
)

// TestMSPAuditService_Summary 锁定 IP-P1-8 看板聚合口径：
// 窗口过滤、拒绝类计数、按 source/action/target/membership 聚合、客户名回填、最近拒绝排序。
func TestMSPAuditService_Summary(t *testing.T) {
	client := enttest.Open(t, "sqlite3", "file:msp_audit_summary?mode=memory&cache=shared&_fk=1")
	defer client.Close()
	ctx := context.Background()
	logger := zaptest.NewLogger(t).Sugar()
	svc := NewMSPAuditService(client, logger)

	provider, err := client.Tenant.Create().
		SetName("Provider").SetCode("prov-audit").SetType(tenant.Type("msp_provider")).SetStatus("active").
		Save(ctx)
	require.NoError(t, err)
	alpha, err := client.Tenant.Create().
		SetName("Alpha Corp").SetCode("cust-alpha").SetType(tenant.Type("msp_customer")).SetStatus("active").
		SetMspProviderID(provider.ID).
		Save(ctx)
	require.NoError(t, err)
	beta, err := client.Tenant.Create().
		SetName("Beta LLC").SetCode("cust-beta").SetType(tenant.Type("msp_customer")).SetStatus("active").
		SetMspProviderID(provider.ID).
		Save(ctx)
	require.NoError(t, err)

	// home 租户成员（membership 聚合）。
	mspUser, err := client.User.Create().
		SetUsername("msp-audit-user").SetEmail("msp-audit@example.com").SetName("msp-audit").
		SetPasswordHash("hash").SetTenantID(provider.ID).
		Save(ctx)
	require.NoError(t, err)
	member, err := client.UserTenantMembership.Create().
		SetUserID(mspUser.ID).SetTenantID(provider.ID).SetAccountKind("provider").
		SetSource("home").SetIsDefault(true).SetStatus("active").
		Save(ctx)
	require.NoError(t, err)

	at := func(hoursAgo int) time.Time { return time.Now().Add(-time.Duration(hoursAgo) * time.Hour) }
	write := func(action, source string, target, membership, status int, hoursAgo int, reason string) {
		t.Helper()
		create := client.AuditLog.Create().
			SetCreatedAt(at(hoursAgo)).
			SetTenantID(provider.ID).
			SetAction(action).SetSource(source).SetResource("msp_customer").
			SetPath("/api/v1/msp/workbench/tickets").SetMethod("GET").
			SetStatusCode(status).SetActorAccount("msp-user").
			SetRequestBody(`{"reasonCode":"` + reason + `"}`)
		if target > 0 {
			create = create.SetTargetTenantID(target)
		}
		if membership > 0 {
			create = create.SetMembershipID(membership)
		}
		_, err := create.Save(ctx)
		require.NoError(t, err)
	}

	write("tenant.scope_denied", "workbench", alpha.ID, member.ID, 403, 2, "MSP_ALLOCATION_REQUIRED")
	write("tenant.scope_denied", "header", alpha.ID, 0, 403, 3, "MSP_ALLOCATION_REQUIRED")
	write("tenant.probe_denied", "header", beta.ID, 0, 401, 1, "TENANT_MISMATCH_REJECTED")
	write("workbench.action", "workbench", beta.ID, member.ID, 200, 4, "")
	write("auth.login", "login", 0, 0, 200, 5, "")
	// 窗口外（40 天前，默认窗口 30 天不应计入）。
	write("tenant.scope_denied", "header", beta.ID, 0, 403, 40*24, "MSP_ALLOCATION_REQUIRED")
	// 其它租户的行（不属于 home，必须被过滤）。
	if _, err := client.AuditLog.Create().
		SetCreatedAt(at(1)).SetTenantID(alpha.ID).
		SetAction("tenant.scope_denied").SetSource("header").SetResource("msp_customer").
		SetPath("/x").SetMethod("GET").SetStatusCode(403).
		Save(ctx); err != nil {
		require.NoError(t, err)
	}

	summary, err := svc.Summary(ctx, provider.ID, 30)
	require.NoError(t, err)

	assert.Equal(t, 30, summary.WindowDays)
	assert.Equal(t, 5, summary.TotalEvents, "窗口内 home 行 = 5（排除窗口外与其它租户）")
	assert.Equal(t, 3, summary.DeniedEvents, "scope_denied×2 + probe_denied×1")
	assert.Equal(t, 4, summary.CrossTenantEvents, "target 非 home 的行：3 条拒绝 + 1 条工作台动作")

	// 按 source：workbench=2（scope_denied + workbench.action）、header=2（两条拒绝）、login=1。
	bySource := map[string]int{}
	for _, row := range summary.BySource {
		bySource[row.Key] = row.Count
	}
	assert.Equal(t, 2, bySource["workbench"])
	assert.Equal(t, 2, bySource["header"])
	assert.Equal(t, 1, bySource["login"])

	// 按目标租户（含名称回填）：alpha=2（两条拒绝）、beta=2（probe + 工作台动作）。
	byTarget := map[string]MSPAuditAggRow{}
	for _, row := range summary.ByTargetTenant {
		byTarget[row.Key] = row
	}
	assert.Equal(t, 2, byTarget[strconv.Itoa(alpha.ID)].Count)
	assert.Equal(t, "Alpha Corp", byTarget[strconv.Itoa(alpha.ID)].Label)
	assert.Equal(t, 2, byTarget[strconv.Itoa(beta.ID)].Count)
	assert.Equal(t, "Beta LLC", byTarget[strconv.Itoa(beta.ID)].Label)

	// membership 聚合：member.ID 出现 2 次。
	require.Len(t, summary.ByMembership, 1)
	assert.Equal(t, strconv.Itoa(member.ID), summary.ByMembership[0].Key)
	assert.Equal(t, 2, summary.ByMembership[0].Count)

	// 最近拒绝：最新在前，reasonCode 解析，客户名回填。
	require.Len(t, summary.RecentDenials, 3)
	assert.Equal(t, "tenant.probe_denied", summary.RecentDenials[0].Action)
	assert.Equal(t, "TENANT_MISMATCH_REJECTED", summary.RecentDenials[0].ReasonCode)
	assert.Equal(t, beta.ID, summary.RecentDenials[0].TargetTenantID)
	assert.Equal(t, "Beta LLC", summary.RecentDenials[0].TargetName)
	assert.Equal(t, 401, summary.RecentDenials[0].StatusCode)
	assert.Equal(t, "tenant.scope_denied", summary.RecentDenials[1].Action)

	// 窗口收敛：>90 → 90；<=0 → 默认 30。
	wide, err := svc.Summary(ctx, provider.ID, 365)
	require.NoError(t, err)
	assert.Equal(t, 90, wide.WindowDays)
	assert.Equal(t, 6, wide.TotalEvents, "90 天窗口纳入 40 天前的行")

	def, err := svc.Summary(ctx, provider.ID, 0)
	require.NoError(t, err)
	assert.Equal(t, 30, def.WindowDays)
}

