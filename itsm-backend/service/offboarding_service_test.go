package service

import (
	"context"
	"testing"
	"time"

	_ "github.com/mattn/go-sqlite3"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap/zaptest"

	"itsm-backend/ent/enttest"
	"itsm-backend/ent/mspallocation"
	"itsm-backend/ent/usertenantmembership"
	"itsm-backend/middleware"
)

// =============================================================================
// IP-P2-8：离职编排（停用 + 全量会话失效 + 全部分配回收 + 审计 + 幂等 + 防自离职）
// =============================================================================

func TestOffboardingService_OffboardUser_FullOrchestration(t *testing.T) {
	client := enttest.Open(t, "sqlite3", testDSN())
	defer client.Close()
	ctx := context.Background()
	logger := zaptest.NewLogger(t).Sugar()
	middleware.SetAuthzStaticFallback(false)
	t.Cleanup(func() { middleware.SetAuthzStaticFallback(false) })
	middleware.InvalidateAllPermissionCaches()

	provider := msTenant(t, ctx, client, "Provider", "off-prov", "msp_provider", 0)
	customerA := msTenant(t, ctx, client, "CustA", "off-cust-a", "msp_customer", provider.ID)
	customerB := msTenant(t, ctx, client, "CustB", "off-cust-b", "msp_customer", provider.ID)
	msRoleWithPerm(t, ctx, client, customerA.ID, "msp_tech", "incident", "read")
	msRoleWithPerm(t, ctx, client, customerB.ID, "msp_tech", "incident", "read")
	admin := msUser(t, ctx, client, "off-admin", provider.ID, "admin", "provider_admin")
	staff := msUser(t, ctx, client, "off-staff", provider.ID, "end_user", "provider_agent")

	allocSvc := NewMSPAllocationService(client, logger)
	_, err := allocSvc.Create(ctx, staff.ID, customerA.ID, "backup")
	require.NoError(t, err)
	_, err = allocSvc.Create(ctx, staff.ID, customerB.ID, "backup")
	require.NoError(t, err)

	svc := NewOffboardingService(client, allocSvc, logger)
	res, err := svc.OffboardUser(ctx, staff.ID, provider.ID, OffboardMeta{
		ActorUserID:  admin.ID,
		ActorAccount: "off-admin",
		Source:       "header",
		Trigger:      "user.offboard",
	})
	require.NoError(t, err)
	require.NotNil(t, res)
	assert.True(t, res.Deactivated)
	assert.False(t, res.AlreadyInactive)
	assert.Equal(t, 2, res.AllocationsRevoked, "两个客户的有效分配必须全部回收")
	assert.Equal(t, 0, res.AllocationsPending)

	// ① 账号停用
	u, err := client.User.Get(ctx, staff.ID)
	require.NoError(t, err)
	assert.False(t, u.Active)

	// ② 全部分配已解除
	activeAllocs, err := client.MSPAllocation.Query().
		Where(mspallocation.MspUserIDEQ(staff.ID), mspallocation.DeassignedAtIsNil()).
		Count(ctx)
	require.NoError(t, err)
	assert.Equal(t, 0, activeAllocs)

	// ③ membership 软删（两个客户作用域均失效）
	for _, customerID := range []int{customerA.ID, customerB.ID} {
		liveCount, err := client.UserTenantMembership.Query().
			Where(
				usertenantmembership.UserID(staff.ID),
				usertenantmembership.TenantID(customerID),
				usertenantmembership.DeletedAtIsNil(),
			).
			Count(ctx)
		require.NoError(t, err)
		assert.Equal(t, 0, liveCount, "离职后客户作用域 membership 必须软删")
	}

	// ④ 审计：user.offboard ×1 + membership.revoke ×2（分配侧由既有通道产生）
	assert.Equal(t, 1, msAuditCount(t, ctx, client, "user.offboard"))
	assert.Equal(t, 2, msAuditCount(t, ctx, client, MembershipAuditActionRevoke))

	// ⑤ 全量会话失效：用户级 refresh 吊销（access 由同一 store 承载）
	refreshRevoked, err := middleware.IsUserRefreshRevoked(ctx, staff.ID, time.Now().Add(-time.Minute))
	require.NoError(t, err)
	assert.True(t, refreshRevoked, "离职后该用户全部在途 refresh 必须被吊销")

	// ⑥ 幂等重跑：alreadyInactive + 0 回收（审计仍留痕，可追溯重跑）
	res2, err := svc.OffboardUser(ctx, staff.ID, provider.ID, OffboardMeta{
		ActorUserID: admin.ID, ActorAccount: "off-admin", Source: "header",
	})
	require.NoError(t, err)
	assert.True(t, res2.AlreadyInactive)
	assert.False(t, res2.Deactivated)
	assert.Equal(t, 0, res2.AllocationsRevoked)
	assert.Equal(t, 0, res2.AllocationsPending)
	assert.Equal(t, 2, msAuditCount(t, ctx, client, "user.offboard"))

	// ⑦ 防自离职（服务层兜底）
	_, err = svc.OffboardUser(ctx, admin.ID, provider.ID, OffboardMeta{ActorUserID: admin.ID, ActorAccount: "off-admin"})
	assert.ErrorIs(t, err, ErrSelfOffboard)

	// ⑧ 目标用户不存在 / 不属于调用者租户
	_, err = svc.OffboardUser(ctx, staff.ID, customerA.ID, OffboardMeta{ActorUserID: admin.ID})
	assert.ErrorIs(t, err, ErrOffboardUserNotFound)
}

// TestOffboardingService_OffboardUser_NoAllocations 无分配用户的纯停用（customer/普通用户场景）。
func TestOffboardingService_OffboardUser_NoAllocations(t *testing.T) {
	client := enttest.Open(t, "sqlite3", testDSN())
	defer client.Close()
	ctx := context.Background()
	logger := zaptest.NewLogger(t).Sugar()
	middleware.SetAuthzStaticFallback(false)
	t.Cleanup(func() { middleware.SetAuthzStaticFallback(false) })

	customer := msTenant(t, ctx, client, "Cust", "off-cust-only", "msp_customer", 0)
	admin := msUser(t, ctx, client, "off-cust-admin", customer.ID, "admin", "")
	staff := msUser(t, ctx, client, "off-cust-user", customer.ID, "end_user", "")

	svc := NewOffboardingService(client, NewMSPAllocationService(client, logger), logger)
	res, err := svc.OffboardUser(ctx, staff.ID, customer.ID, OffboardMeta{ActorUserID: admin.ID, ActorAccount: "off-cust-admin"})
	require.NoError(t, err)
	assert.True(t, res.Deactivated)
	assert.Equal(t, 0, res.AllocationsRevoked)
	assert.Equal(t, 0, res.AllocationsPending)
	assert.Equal(t, 1, msAuditCount(t, ctx, client, "user.offboard"))
}
