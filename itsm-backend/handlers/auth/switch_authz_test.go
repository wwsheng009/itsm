package auth

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"
	"time"

	_ "github.com/mattn/go-sqlite3"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap/zaptest"

	"itsm-backend/ent"
	"itsm-backend/ent/auditlog"
	"itsm-backend/ent/enttest"
	"itsm-backend/ent/usertenantmembership"
	"itsm-backend/middleware"
	"itsm-backend/service"
)

// =============================================================================
// MSW-2/A3：切换授权判定（Q1=C invite 依据）+ 全分支审计（reasonCode/basis）。
// =============================================================================

func switchAuditPayload(t *testing.T, ctx context.Context, client *ent.Client, action string) map[string]string {
	t.Helper()
	row, err := client.AuditLog.Query().
		Where(auditlog.ActionEQ(action)).
		Order(ent.Desc(auditlog.FieldID)).
		First(ctx)
	require.NoError(t, err)
	var payload map[string]string
	body := ""
	if row.RequestBody != nil {
		body = *row.RequestBody
	}
	require.NoError(t, json.Unmarshal([]byte(body), &payload))
	return payload
}

func TestSwitchTenant_InviteBasisAndDenyAudits(t *testing.T) {
	ctx := context.Background()
	logger := zaptest.NewLogger(t).Sugar()
	client := enttest.Open(t, "sqlite3", fmt.Sprintf("file:auth_switch_authz_%d?mode=memory&cache=shared&_fk=1", time.Now().UnixNano()))
	defer func() { _ = client.Close() }()
	middleware.SetAuthzStaticFallback(false)
	t.Cleanup(func() { middleware.SetAuthzStaticFallback(false) })
	middleware.InvalidateAllPermissionCaches()

	provider, err := client.Tenant.Create().
		SetName("Provider").SetCode("switch-authz-prov").SetStatus("active").SetType("msp_provider").
		Save(ctx)
	require.NoError(t, err)
	customer, err := client.Tenant.Create().
		SetName("Customer").SetCode("switch-authz-cust").SetStatus("active").SetType("msp_customer").
		SetMspProviderID(provider.ID).
		Save(ctx)
	require.NoError(t, err)
	customer2, err := client.Tenant.Create().
		SetName("Customer2").SetCode("switch-authz-cust2").SetStatus("active").SetType("msp_customer").
		SetMspProviderID(provider.ID).
		Save(ctx)
	require.NoError(t, err)
	customerRole := authClosureRole(t, ctx, client, customer.ID, "agent", "incident", "read")
	inviter, err := client.User.Create().
		SetUsername("switch-inviter").SetEmail("switch-inviter@example.com").SetName("Inviter").
		SetPasswordHash("x").SetRole("admin").SetActive(true).SetTenantID(customer.ID).
		Save(ctx)
	require.NoError(t, err)
	staff, err := client.User.Create().
		SetUsername("switch-staff").SetEmail("switch-staff@example.com").SetName("Staff").
		SetPasswordHash("x").SetRole("end_user").SetMspRole("provider_agent").SetActive(true).SetTenantID(provider.ID).
		Save(ctx)
	require.NoError(t, err)

	// Q1=C：客户侧显式确认的邀请产物（source=invite ∧ invited_by 非空）。
	_, err = client.UserTenantMembership.Create().
		SetUserID(staff.ID).SetTenantID(customer.ID).
		SetAccountKind(usertenantmembership.AccountKindProvider).
		SetSource(usertenantmembership.SourceInvite).
		SetRoleID(customerRole.ID).SetInvitedBy(inviter.ID).
		SetStatus(usertenantmembership.StatusActive).SetIsDefault(false).
		Save(ctx)
	require.NoError(t, err)

	authSvc := NewService(client, "switch-authz-secret", logger, nil)

	// 1) invite 依据放行：目标租户权限非空 + 审计 basis=invite。
	resp, err := authSvc.SwitchTenant(ctx, staff.ID, customer.ID)
	require.NoError(t, err)
	require.NotNil(t, resp.User)
	assert.Equal(t, customer.ID, resp.User.TenantID)
	assert.Equal(t, []string{"incident:read"}, resp.User.Permissions)
	switchCount, err := client.AuditLog.Query().Where(auditlog.ActionEQ("tenant.switch")).Count(ctx)
	require.NoError(t, err)
	assert.Equal(t, 1, switchCount)
	assert.Equal(t, "invite", switchAuditPayload(t, ctx, client, "tenant.switch")["basis"])

	// 2) 停用账号拒绝：不得换发令牌 + reasonCode=user_inactive。
	_, err = client.User.UpdateOneID(staff.ID).SetActive(false).Save(ctx)
	require.NoError(t, err)
	_, err = authSvc.SwitchTenant(ctx, staff.ID, customer.ID)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "账号已被停用")
	assert.Equal(t, "user_inactive", switchAuditPayload(t, ctx, client, "tenant.switch_denied")["reasonCode"])
	_, err = client.User.UpdateOneID(staff.ID).SetActive(true).Save(ctx)
	require.NoError(t, err)

	// 3) 目标暂停（allocation 依据）：拒绝 + reasonCode=target_suspended。
	allocSvc := service.NewMSPAllocationService(client, logger)
	_, err = allocSvc.Create(ctx, staff.ID, customer.ID, "primary")
	require.NoError(t, err) // 既有 invite 行 → 同步冲突仅告警，不阻断分配
	_, err = client.Tenant.UpdateOneID(customer.ID).SetStatus("suspended").Save(ctx)
	require.NoError(t, err)
	_, err = authSvc.SwitchTenant(ctx, staff.ID, customer.ID)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "租户已被暂停")
	assert.Equal(t, "target_suspended", switchAuditPayload(t, ctx, client, "tenant.switch_denied")["reasonCode"])

	// 4) 目标过期：拒绝 + reasonCode=target_expired。
	_, err = client.Tenant.UpdateOneID(customer.ID).SetStatus("active").SetExpiresAt(time.Now().Add(-time.Hour)).Save(ctx)
	require.NoError(t, err)
	_, err = authSvc.SwitchTenant(ctx, staff.ID, customer.ID)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "租户已过期")
	assert.Equal(t, "target_expired", switchAuditPayload(t, ctx, client, "tenant.switch_denied")["reasonCode"])

	// 5) 无依据（另一客户租户）：不暴露目标存在性 → no_grant。
	_, err = authSvc.SwitchTenant(ctx, staff.ID, customer2.ID)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "无权限访问该租户")
	assert.Equal(t, "no_grant", switchAuditPayload(t, ctx, client, "tenant.switch_denied")["reasonCode"])

	// 6) super_admin 目标不存在 → target_not_found（已授权者才进入目标分支，全分支审计）。
	root, err := client.User.Create().
		SetUsername("switch-root").SetEmail("switch-root@example.com").SetName("Root").
		SetPasswordHash("x").SetRole("super_admin").SetActive(true).SetTenantID(provider.ID).
		Save(ctx)
	require.NoError(t, err)
	_, err = authSvc.SwitchTenant(ctx, root.ID, 99999)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "租户不存在")
	assert.Equal(t, "target_not_found", switchAuditPayload(t, ctx, client, "tenant.switch_denied")["reasonCode"])
}
