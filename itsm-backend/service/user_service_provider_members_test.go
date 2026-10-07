package service

import (
	"context"
	"testing"
	"time"

	_ "github.com/mattn/go-sqlite3"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap/zaptest"

	"itsm-backend/dto"
	"itsm-backend/ent"
	"itsm-backend/ent/enttest"
	"itsm-backend/ent/usertenantmembership"
)

// Q2=B / MSW-2：客户侧成员列表只读展示服务方成员（独立区块，不计入分页 total，不可管理）。
func TestUserService_ListUsers_ProviderMembersReadOnly(t *testing.T) {
	client := enttest.Open(t, "sqlite3", testDSN())
	defer func() { _ = client.Close() }()
	ctx := context.Background()
	logger := zaptest.NewLogger(t).Sugar()

	provider, err := client.Tenant.Create().SetName("P").SetCode("upm-p").SetStatus("active").SetType("msp_provider").Save(ctx)
	require.NoError(t, err)
	customer, err := client.Tenant.Create().SetName("C").SetCode("upm-c").SetStatus("active").SetType("msp_customer").SetMspProviderID(provider.ID).Save(ctx)
	require.NoError(t, err)

	mkUser := func(name string, tenantID int, active bool) *ent.User {
		u, err := client.User.Create().
			SetUsername(name).SetEmail(name + "@example.com").SetName(name).
			SetPasswordHash("x").SetTenantID(tenantID).SetRole("end_user").SetActive(active).
			Save(ctx)
		require.NoError(t, err)
		return u
	}
	mkUser("upm-cust-admin", customer.ID, true)
	staff := mkUser("upm-staff", provider.ID, true)
	suspendedStaff := mkUser("upm-suspended", provider.ID, true)
	inactiveStaff := mkUser("upm-inactive", provider.ID, false)
	deletedStaff := mkUser("upm-deleted", provider.ID, true)

	mkMembership := func(userID int, status usertenantmembership.Status, deleted bool) {
		q := client.UserTenantMembership.Create().
			SetUserID(userID).SetTenantID(customer.ID).
			SetAccountKind(usertenantmembership.AccountKindProvider).
			SetSource(usertenantmembership.SourceAllocation).
			SetMspRole("provider_agent").
			SetStatus(status).SetIsDefault(false)
		if deleted {
			q = q.SetDeletedAt(time.Now())
		}
		_, err := q.Save(ctx)
		require.NoError(t, err)
	}
	mkMembership(staff.ID, usertenantmembership.StatusActive, false)
	mkMembership(suspendedStaff.ID, usertenantmembership.StatusSuspended, false)
	mkMembership(inactiveStaff.ID, usertenantmembership.StatusActive, false)
	mkMembership(deletedStaff.ID, usertenantmembership.StatusActive, true)

	svc := NewUserService(client, logger)

	resp, err := svc.ListUsers(ctx, &dto.ListUsersRequest{Page: 1, PageSize: 10}, customer.ID)
	require.NoError(t, err)
	assert.EqualValues(t, 1, resp.Pagination.Total, "total 仅统计 users 表（服务方成员独立字段）")
	require.Len(t, resp.ProviderMembers, 2, "仅 active 存活映射；账号停用仍如实展示")
	byID := map[int]*dto.UserDetailResponse{}
	for _, m := range resp.ProviderMembers {
		byID[m.ID] = m
	}
	got := byID[staff.ID]
	require.NotNil(t, got)
	assert.True(t, got.ReadOnly, "服务方成员必须标记只读（前端隐藏管理动作）")
	assert.Equal(t, "provider", got.AccountKind)
	assert.Equal(t, "provider_agent", got.Role)
	assert.Equal(t, customer.ID, got.TenantID)
	assert.True(t, got.Active)
	assert.Nil(t, byID[suspendedStaff.ID], "status=suspended 的映射不展示")
	assert.Nil(t, byID[deletedStaff.ID], "软删映射不展示")
	require.NotNil(t, byID[inactiveStaff.ID], "账号停用但映射存活时展示（状态如实）")
	assert.False(t, byID[inactiveStaff.ID].Active)

	// status=active 过滤排除停用账号；search 命中/未命中同列表语义。
	resp, err = svc.ListUsers(ctx, &dto.ListUsersRequest{Page: 1, PageSize: 10, Status: "active"}, customer.ID)
	require.NoError(t, err)
	require.Len(t, resp.ProviderMembers, 1)
	assert.Equal(t, staff.ID, resp.ProviderMembers[0].ID)

	resp, err = svc.ListUsers(ctx, &dto.ListUsersRequest{Page: 1, PageSize: 10, Search: "upm-staff"}, customer.ID)
	require.NoError(t, err)
	require.Len(t, resp.ProviderMembers, 1)
	assert.Equal(t, staff.ID, resp.ProviderMembers[0].ID)

	resp, err = svc.ListUsers(ctx, &dto.ListUsersRequest{Page: 1, PageSize: 10, Search: "no-such-member"}, customer.ID)
	require.NoError(t, err)
	assert.Empty(t, resp.ProviderMembers)

	// provider 租户：无服务方区块（不产生自引用展示）。
	resp, err = svc.ListUsers(ctx, &dto.ListUsersRequest{Page: 1, PageSize: 10}, provider.ID)
	require.NoError(t, err)
	assert.Empty(t, resp.ProviderMembers)
}
