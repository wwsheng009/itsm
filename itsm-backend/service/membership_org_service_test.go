package service

import (
	"context"
	"fmt"
	"testing"
	"time"

	"itsm-backend/ent"
	"itsm-backend/ent/enttest"
	"itsm-backend/ent/usertenantmembership"
	"itsm-backend/ent/usertenantmembershiporg"

	_ "github.com/mattn/go-sqlite3"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap/zaptest"
)

// =============================================================================
// IP-P1-3 组织归属测试：同租户挂接、跨租户拒绝（A5 DoD）、主组织唯一、
// 软删复活、多态组织类型。
//
// DB 层复合 FK（(membership_id, tenant_id) → user_tenant_memberships(id, tenant_id)）
// 由迁移 20260505 在 Postgres 落地；SQLite enttest 无法表达该约束，
// 因此这里覆盖应用层双重校验的"应用侧拒绝"，DB 侧由迁移 + 巡检脚本断言。
// =============================================================================

type membershipOrgFixture struct {
	client     *ent.Client
	ctx        context.Context
	tenantA    *ent.Tenant
	tenantB    *ent.Tenant
	userA      *ent.User
	membership *ent.UserTenantMembership
	deptA      *ent.Department
	deptA2     *ent.Department
	deptB      *ent.Department
	teamA      *ent.Team
	groupA     *ent.Group
	projectB   *ent.Project
}

func newMembershipOrgFixture(t *testing.T) *membershipOrgFixture {
	t.Helper()
	ctx := context.Background()
	dsn := fmt.Sprintf("file:membership_org_%d?mode=memory&cache=shared&_fk=1", time.Now().UnixNano())
	client := enttest.Open(t, "sqlite3", dsn)
	t.Cleanup(func() { _ = client.Close() })

	tenantA, err := client.Tenant.Create().SetName("Org A").SetCode("org-a").SetStatus("active").SetType("msp_provider").Save(ctx)
	require.NoError(t, err)
	tenantB, err := client.Tenant.Create().SetName("Org B").SetCode("org-b").SetStatus("active").SetType("msp_customer").Save(ctx)
	require.NoError(t, err)

	userA, err := client.User.Create().
		SetUsername("org-user-a").SetEmail("org-user-a@example.com").SetName("Org User A").
		SetPasswordHash("hash").SetActive(true).SetRole("end_user").SetTenantID(tenantA.ID).
		Save(ctx)
	require.NoError(t, err)

	membership, err := client.UserTenantMembership.Create().
		SetUserID(userA.ID).SetTenantID(tenantA.ID).
		SetAccountKind(usertenantmembership.AccountKindProvider).
		SetSource(usertenantmembership.SourceHome).SetIsDefault(true).
		Save(ctx)
	require.NoError(t, err)

	deptA, err := client.Department.Create().SetName("Dept A").SetCode("dept-a").SetTenantID(tenantA.ID).Save(ctx)
	require.NoError(t, err)
	deptA2, err := client.Department.Create().SetName("Dept A2").SetCode("dept-a2").SetTenantID(tenantA.ID).Save(ctx)
	require.NoError(t, err)
	deptB, err := client.Department.Create().SetName("Dept B").SetCode("dept-b").SetTenantID(tenantB.ID).Save(ctx)
	require.NoError(t, err)
	teamA, err := client.Team.Create().SetName("Team A").SetCode("team-a").SetTenantID(tenantA.ID).Save(ctx)
	require.NoError(t, err)
	groupA, err := client.Group.Create().SetName("Group A").SetTenantID(tenantA.ID).Save(ctx)
	require.NoError(t, err)
	projectB, err := client.Project.Create().SetName("Project B").SetCode("proj-b").SetTenantID(tenantB.ID).Save(ctx)
	require.NoError(t, err)

	return &membershipOrgFixture{
		client: client, ctx: ctx, tenantA: tenantA, tenantB: tenantB, userA: userA, membership: membership,
		deptA: deptA, deptA2: deptA2, deptB: deptB, teamA: teamA, groupA: groupA, projectB: projectB,
	}
}

func (f *membershipOrgFixture) liveCount(t *testing.T, orgType string, orgID int64) int {
	t.Helper()
	count, err := f.client.UserTenantMembershipOrg.Query().
		Where(
			usertenantmembershiporg.MembershipIDEQ(f.membership.ID),
			usertenantmembershiporg.OrgTypeEQ(usertenantmembershiporg.OrgType(orgType)),
			usertenantmembershiporg.OrgIDEQ(orgID),
			usertenantmembershiporg.DeletedAtIsNil(),
		).
		Count(f.ctx)
	require.NoError(t, err)
	return count
}

func TestMembershipOrgService_SameTenantAttachAndPrimaryUniqueness(t *testing.T) {
	f := newMembershipOrgFixture(t)
	svc := NewMembershipOrgService(f.client, zaptest.NewLogger(t).Sugar())

	row1, err := svc.Attach(f.ctx, f.membership.ID, "department", int64(f.deptA.ID), true)
	require.NoError(t, err)
	assert.Equal(t, f.tenantA.ID, row1.TenantID)
	assert.True(t, row1.IsPrimary)

	// 同 (membership, org_type) 换主组织：旧主组织自动降级，部分唯一索引不冲突。
	row2, err := svc.Attach(f.ctx, f.membership.ID, "department", int64(f.deptA2.ID), true)
	require.NoError(t, err)
	assert.True(t, row2.IsPrimary)
	oldRow, err := f.client.UserTenantMembershipOrg.Get(f.ctx, row1.ID)
	require.NoError(t, err)
	assert.False(t, oldRow.IsPrimary, "同类型仅允许一个存活主组织")

	primaries, err := f.client.UserTenantMembershipOrg.Query().
		Where(
			usertenantmembershiporg.MembershipIDEQ(f.membership.ID),
			usertenantmembershiporg.OrgTypeEQ(usertenantmembershiporg.OrgTypeDepartment),
			usertenantmembershiporg.DeletedAtIsNil(),
			usertenantmembershiporg.IsPrimaryEQ(true),
		).
		Count(f.ctx)
	require.NoError(t, err)
	assert.Equal(t, 1, primaries)

	// 幂等重挂：复用原行，不产生重复。
	again, err := svc.Attach(f.ctx, f.membership.ID, "department", int64(f.deptA.ID), false)
	require.NoError(t, err)
	assert.Equal(t, row1.ID, again.ID)
	assert.Equal(t, 1, f.liveCount(t, "department", int64(f.deptA.ID)))

	// 多态：team/group 同租户可挂。
	_, err = svc.Attach(f.ctx, f.membership.ID, "team", int64(f.teamA.ID), false)
	require.NoError(t, err)
	_, err = svc.Attach(f.ctx, f.membership.ID, "group", int64(f.groupA.ID), false)
	require.NoError(t, err)
}

func TestMembershipOrgService_CrossTenantRejected(t *testing.T) {
	f := newMembershipOrgFixture(t)
	svc := NewMembershipOrgService(f.client, zaptest.NewLogger(t).Sugar())

	// 部门在 B 租户，membership 在 A 租户 → 应用层必须拒绝（A5 DoD）。
	_, err := svc.Attach(f.ctx, f.membership.ID, "department", int64(f.deptB.ID), false)
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrMembershipOrgCrossTenant)

	// 多态 table（project）同样拒绝。
	_, err = svc.Attach(f.ctx, f.membership.ID, "project", int64(f.projectB.ID), false)
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrMembershipOrgCrossTenant)

	// 拒绝后不得留下任何行（应用层先校验后写入）。
	count, err := f.client.UserTenantMembershipOrg.Query().
		Where(usertenantmembershiporg.MembershipIDEQ(f.membership.ID)).
		Count(f.ctx)
	require.NoError(t, err)
	assert.Zero(t, count)
}

func TestMembershipOrgService_DetachAndReattach(t *testing.T) {
	f := newMembershipOrgFixture(t)
	svc := NewMembershipOrgService(f.client, zaptest.NewLogger(t).Sugar())

	row, err := svc.Attach(f.ctx, f.membership.ID, "department", int64(f.deptA.ID), true)
	require.NoError(t, err)

	detached, err := svc.Detach(f.ctx, f.membership.ID, "department", int64(f.deptA.ID))
	require.NoError(t, err)
	assert.True(t, detached)

	list, err := svc.ListForMembership(f.ctx, f.membership.ID)
	require.NoError(t, err)
	assert.Empty(t, list, "软删行不得出现在存活清单")

	// 复用原行复活：unique(membership_id, org_type, org_id) 含历史行，不得新建。
	reattached, err := svc.Attach(f.ctx, f.membership.ID, "department", int64(f.deptA.ID), true)
	require.NoError(t, err)
	assert.Equal(t, row.ID, reattached.ID)
	assert.Nil(t, reattached.DeletedAt)
	assert.Equal(t, usertenantmembershiporg.StatusActive, reattached.Status)

	// 再次 detach 不存在行 → false。
	detached, err = svc.Detach(f.ctx, f.membership.ID, "department", 999999)
	require.NoError(t, err)
	assert.False(t, detached)
}

func TestMembershipOrgService_ValidationErrors(t *testing.T) {
	f := newMembershipOrgFixture(t)
	svc := NewMembershipOrgService(f.client, zaptest.NewLogger(t).Sugar())

	_, err := svc.Attach(f.ctx, f.membership.ID, "division", int64(f.deptA.ID), false)
	assert.ErrorIs(t, err, ErrMembershipOrgTypeInvalid)

	_, err = svc.Attach(f.ctx, f.membership.ID, "department", 999999, false)
	assert.ErrorIs(t, err, ErrMembershipOrgNotFound)

	_, err = svc.Attach(f.ctx, 999999, "department", int64(f.deptA.ID), false)
	assert.ErrorIs(t, err, ErrMembershipOrgMembershipInactive)
}
