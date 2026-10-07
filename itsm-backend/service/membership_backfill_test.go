package service

import (
	"context"
	"testing"
	"time"

	_ "github.com/mattn/go-sqlite3"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap/zaptest"

	"itsm-backend/ent"
	"itsm-backend/ent/enttest"
	"itsm-backend/ent/mspallocation"
	"itsm-backend/ent/usertenantmembership"
	"itsm-backend/internal/membershipcheck"
	"itsm-backend/middleware"
)

// =============================================================================
// MSW-FU-1：存量 membership 回填修复器（重放 20260504 回填 1/2 语义）
//   - dry-run 不写库；apply 创建缺失；reconcile 校正同源漂移；异源不覆盖；
//   - 回填后巡检 0 漂移（MSW-A6 / FU-1 验收闭环）。
// =============================================================================

// backfillFacts 把 ent 全量事实映射为巡检输入（与 cmd/membership-drift-check 同口径）。
func backfillFacts(t *testing.T, ctx context.Context, client *ent.Client) membershipcheck.Facts {
	t.Helper()
	var f membershipcheck.Facts

	tenants, err := client.Tenant.Query().All(ctx)
	require.NoError(t, err)
	for _, tn := range tenants {
		f.Tenants = append(f.Tenants, membershipcheck.TenantFact{ID: tn.ID, Type: string(tn.Type)})
	}
	roles, err := client.Role.Query().All(ctx)
	require.NoError(t, err)
	for _, r := range roles {
		f.Roles = append(f.Roles, membershipcheck.RoleFact{ID: r.ID, TenantID: r.TenantID, Code: r.Code})
	}
	users, err := client.User.Query().All(ctx)
	require.NoError(t, err)
	for _, u := range users {
		f.Users = append(f.Users, membershipcheck.UserFact{
			ID: u.ID, Username: u.Username, Active: u.Active,
			Role: string(u.Role), MSPRole: string(u.MspRole), TenantID: u.TenantID,
		})
	}
	mbs, err := client.UserTenantMembership.Query().All(ctx)
	require.NoError(t, err)
	for _, m := range mbs {
		mspRole := ""
		if m.MspRole != nil {
			mspRole = *m.MspRole
		}
		f.Memberships = append(f.Memberships, membershipcheck.MembershipFact{
			ID: m.ID, UserID: m.UserID, TenantID: m.TenantID,
			AccountKind: string(m.AccountKind), Source: string(m.Source),
			RoleID: m.RoleID, MSPRole: mspRole, Status: string(m.Status),
			IsDefault: m.IsDefault, DeletedAt: m.DeletedAt != nil,
		})
	}
	allocs, err := client.MSPAllocation.Query().Where(mspallocation.DeassignedAtIsNil()).All(ctx)
	require.NoError(t, err)
	for _, a := range allocs {
		f.Allocations = append(f.Allocations, membershipcheck.AllocationFact{
			MSPUserID: a.MspUserID, CustomerTenantID: a.CustomerTenantID, ProviderTenantID: a.ProviderTenantID,
		})
	}
	return f
}

func TestMembershipBackfill_DryRunApplyReconcile_CheckClean(t *testing.T) {
	client := enttest.Open(t, "sqlite3", testDSN())
	defer client.Close()
	ctx := context.Background()
	logger := zaptest.NewLogger(t).Sugar()
	middleware.SetAuthzStaticFallback(false)
	t.Cleanup(func() { middleware.SetAuthzStaticFallback(false) })
	middleware.InvalidateAllPermissionCaches()

	provider := msTenant(t, ctx, client, "BF Provider", "bf-prov", "msp_provider", 0)
	customer := msTenant(t, ctx, client, "BF Customer", "bf-cust", "msp_customer", provider.ID)
	msRoleWithPerm(t, ctx, client, provider.ID, "msp_manager", "ticket", "read")
	msRoleWithPerm(t, ctx, client, provider.ID, "msp_tech", "ticket", "read")
	msRoleWithPerm(t, ctx, client, customer.ID, "msp_specialist", "incident", "read")
	adminRole := msRoleWithPerm(t, ctx, client, customer.ID, "admin", "ticket", "read")

	// A：provider 管理员，无任何 membership（home 缺失）。
	userA := msUser(t, ctx, client, "bf-prov-admin", provider.ID, "admin", "provider_admin")
	// B：provider 员工 + 活跃分配（home 与客户作用域双缺失）。
	userB := msUser(t, ctx, client, "bf-prov-agent", provider.ID, "agent", "provider_agent")
	allocB, err := client.MSPAllocation.Create().
		SetMspUserID(userB.ID).SetCustomerTenantID(customer.ID).SetProviderTenantID(provider.ID).
		SetRole("specialist").SetAssignedAt(time.Now()).Save(ctx)
	require.NoError(t, err)
	// C：客户管理员，存量 home membership 但 role_id 为空（reconcile 场景）。
	userC := msUser(t, ctx, client, "bf-cust-admin", customer.ID, "admin", "")
	_, err = client.UserTenantMembership.Create().
		SetUserID(userC.ID).SetTenantID(customer.ID).
		SetAccountKind(usertenantmembership.AccountKindCustomer).
		SetSource(usertenantmembership.SourceHome).
		SetIsDefault(true).SetStatus(usertenantmembership.StatusActive).Save(ctx)
	require.NoError(t, err)

	svc := NewMembershipSyncService(client, logger)
	meta := MembershipAuditMeta{ActorAccount: "system", Trigger: "membership.backfill"}

	// 1) dry-run：只报告，不写库、不写审计。
	before, err := client.UserTenantMembership.Query().Count(ctx)
	require.NoError(t, err)
	rep, err := svc.BackfillMemberships(ctx, meta, MembershipBackfillOptions{})
	require.NoError(t, err)
	assert.False(t, rep.Applied)
	assert.Equal(t, 3, rep.UsersScanned)
	assert.Equal(t, 1, rep.AllocationsScanned)
	assert.Equal(t, 2, rep.HomeCreated, "A/B 缺 home membership（演练口径：将创建）")
	assert.Equal(t, 1, rep.HomeSkipped, "C 已有同源行（默认跳过）")
	assert.Equal(t, 1, rep.AllocCreated, "B 的分配缺客户作用域 membership")
	assert.Zero(t, rep.HomeConflicts)
	assert.Zero(t, rep.AllocConflicts)
	after, err := client.UserTenantMembership.Query().Count(ctx)
	require.NoError(t, err)
	assert.Equal(t, before, after, "dry-run 不得写库")
	assert.Zero(t, msAuditCount(t, ctx, client, "membership.sync"), "dry-run 不得写审计")

	// 2) apply（create-only）：A/B 补齐；C 保持原状。
	rep, err = svc.BackfillMemberships(ctx, meta, MembershipBackfillOptions{Apply: true})
	require.NoError(t, err)
	assert.Equal(t, 2, rep.HomeCreated)
	assert.Equal(t, 1, rep.AllocCreated)
	assert.Zero(t, len(rep.Errors))

	homeA := msLiveMembership(t, ctx, client, userA.ID, provider.ID)
	assert.Equal(t, usertenantmembership.SourceHome, homeA.Source)
	assert.Equal(t, usertenantmembership.AccountKindProvider, homeA.AccountKind)
	assert.True(t, homeA.IsDefault)
	require.NotNil(t, homeA.RoleID, "home 角色应解析到 msp_manager")
	require.NotNil(t, homeA.MspRole)
	assert.Equal(t, "msp_manager", *homeA.MspRole)

	homeB := msLiveMembership(t, ctx, client, userB.ID, provider.ID)
	assert.Equal(t, usertenantmembership.SourceHome, homeB.Source)
	allocMB := msLiveMembership(t, ctx, client, userB.ID, customer.ID)
	assert.Equal(t, usertenantmembership.SourceAllocation, allocMB.Source)
	assert.Equal(t, usertenantmembership.AccountKindProvider, allocMB.AccountKind)
	assert.False(t, allocMB.IsDefault)
	require.NotNil(t, allocMB.AllocationID)
	assert.Equal(t, allocB.ID, *allocMB.AllocationID)
	require.NotNil(t, allocMB.MspRole)
	assert.Equal(t, "msp_specialist", *allocMB.MspRole)

	stillStale := msLiveMembership(t, ctx, client, userC.ID, customer.ID)
	assert.Nil(t, stillStale.RoleID, "create-only 不校正既有行")
	assert.Equal(t, 3, msAuditCount(t, ctx, client, "membership.sync"))

	// 3) apply+reconcile：C 的 role_id 校正；二次执行幂等（无新增审计）。
	rep, err = svc.BackfillMemberships(ctx, meta, MembershipBackfillOptions{Apply: true, Reconcile: true})
	require.NoError(t, err)
	assert.Equal(t, 3, rep.HomeReconciled, "A/B/C 同源行进入校正路径")
	assert.Equal(t, 1, rep.AllocReconciled)
	reconciled := msLiveMembership(t, ctx, client, userC.ID, customer.ID)
	require.NotNil(t, reconciled.RoleID)
	assert.Equal(t, adminRole.ID, *reconciled.RoleID)
	assert.Equal(t, 4, msAuditCount(t, ctx, client, "membership.sync"), "仅 C 产生一次更新审计")

	rep, err = svc.BackfillMemberships(ctx, meta, MembershipBackfillOptions{Apply: true, Reconcile: true})
	require.NoError(t, err)
	assert.Zero(t, rep.HomeCreated)
	assert.Zero(t, rep.AllocCreated)
	assert.Zero(t, len(rep.Errors))
	assert.Equal(t, 4, msAuditCount(t, ctx, client, "membership.sync"), "幂等：无漂移不写审计")

	// 4) 巡检闭环：回填后 0 漂移（MSW-FU-1 验收）。
	findings := membershipcheck.Check(backfillFacts(t, ctx, client))
	assert.Empty(t, findings, "回填后巡检应 0 漂移，实际: %+v", findings)
}

func TestMembershipBackfill_SourceConflictNotOverwritten(t *testing.T) {
	client := enttest.Open(t, "sqlite3", testDSN())
	defer client.Close()
	ctx := context.Background()
	logger := zaptest.NewLogger(t).Sugar()
	middleware.SetAuthzStaticFallback(false)
	t.Cleanup(func() { middleware.SetAuthzStaticFallback(false) })
	middleware.InvalidateAllPermissionCaches()

	provider := msTenant(t, ctx, client, "BFC Provider", "bfc-prov", "msp_provider", 0)
	customer := msTenant(t, ctx, client, "BFC Customer", "bfc-cust", "msp_customer", provider.ID)
	msRoleWithPerm(t, ctx, client, provider.ID, "msp_tech", "ticket", "read")
	msRoleWithPerm(t, ctx, client, customer.ID, "msp_tech", "incident", "read")

	// E：provider 员工，已有客户侧 invite membership（客户邀请协作），随后又产生分配。
	userE := msUser(t, ctx, client, "bfc-invited", provider.ID, "agent", "provider_agent")
	invite, err := client.UserTenantMembership.Create().
		SetUserID(userE.ID).SetTenantID(customer.ID).
		SetAccountKind(usertenantmembership.AccountKindCustomer).
		SetSource(usertenantmembership.SourceInvite).
		SetInvitedBy(userE.ID). // 仅满足 FK；邀请语义不在本用例范围
		SetStatus(usertenantmembership.StatusActive).Save(ctx)
	require.NoError(t, err)
	_, err = client.MSPAllocation.Create().
		SetMspUserID(userE.ID).SetCustomerTenantID(customer.ID).SetProviderTenantID(provider.ID).
		SetRole("primary").SetAssignedAt(time.Now()).Save(ctx)
	require.NoError(t, err)

	svc := NewMembershipSyncService(client, logger)
	rep, err := svc.BackfillMemberships(ctx, MembershipAuditMeta{ActorAccount: "system", Trigger: "membership.backfill"},
		MembershipBackfillOptions{Apply: true})
	require.NoError(t, err)
	assert.Equal(t, 1, rep.AllocConflicts, "异源（invite）存活行不覆盖")
	assert.Len(t, rep.Conflicts, 1)
	assert.Contains(t, rep.Conflicts[0], "existing_source=invite")

	live := msLiveMembership(t, ctx, client, userE.ID, customer.ID)
	assert.Equal(t, invite.ID, live.ID, "invite 行保持原样")
	assert.Equal(t, usertenantmembership.SourceInvite, live.Source)
	assert.Nil(t, live.AllocationID)

	// home membership 不受客户侧冲突影响，仍被补齐。
	homeE := msLiveMembership(t, ctx, client, userE.ID, provider.ID)
	assert.Equal(t, usertenantmembership.SourceHome, homeE.Source)
}
