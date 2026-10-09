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
	"itsm-backend/middleware"
)

// =============================================================================
// IP-P2-8：停用/删除路径的存量会话吊销补齐（方案 §2 S2/S3 缺口）
//
// 探针口径：以「操作前 1 分钟」为签发时刻断言吊销（与 membership_sync_test 同法；
// Windows 时钟粒度下 time.Now() 同刻相等会使 Before 判定为假）。
// 反向断言（未涉及用户不受影响）带冲突守卫：内存吊销 store 按 userID 全局共享，
// 若该 ID 已被包内更早用例吊销，则跳过反向断言避免误报。
// =============================================================================

func refreshRevokedAt(t *testing.T, ctx context.Context, userID int, probe time.Time) bool {
	t.Helper()
	v, err := middleware.IsUserRefreshRevoked(ctx, userID, probe)
	require.NoError(t, err)
	return v
}

func newRevocationTestUser(t *testing.T, ctx context.Context, client *ent.Client, username string, tenantID int) *ent.User {
	t.Helper()
	u, err := client.User.Create().
		SetUsername(username).SetEmail(username + "@example.com").SetName(username).
		SetPasswordHash("x").SetActive(true).SetTenantID(tenantID).Save(ctx)
	require.NoError(t, err)
	return u
}

func TestUserService_DeleteUser_RevokesSessions(t *testing.T) {
	client := enttest.Open(t, "sqlite3", testDSN())
	defer client.Close()
	ctx := context.Background()
	svc := NewUserService(client, zaptest.NewLogger(t).Sugar())

	tn, err := client.Tenant.Create().SetName("T").SetCode("rev-del").SetStatus("active").Save(ctx)
	require.NoError(t, err)
	u := newRevocationTestUser(t, ctx, client, "rev-del-user", tn.ID)

	probe := time.Now().Add(-time.Minute)
	before := refreshRevokedAt(t, ctx, u.ID, probe)

	require.NoError(t, svc.DeleteUser(ctx, u.ID, tn.ID))
	assert.True(t, refreshRevokedAt(t, ctx, u.ID, probe), "软删（停用）后存量 refresh 必须被吊销")
	if before {
		t.Logf("note: user id %d 已被更早用例吊销（ID 复用），本断言为弱证据", u.ID)
	}

	got, err := client.User.Get(ctx, u.ID)
	require.NoError(t, err)
	assert.False(t, got.Active)
}

func TestUserService_BatchUpdateUsers_DeactivateRevokesSessions(t *testing.T) {
	client := enttest.Open(t, "sqlite3", testDSN())
	defer client.Close()
	ctx := context.Background()
	svc := NewUserService(client, zaptest.NewLogger(t).Sugar())

	tn, err := client.Tenant.Create().SetName("T").SetCode("rev-batch").SetStatus("active").Save(ctx)
	require.NoError(t, err)
	u1 := newRevocationTestUser(t, ctx, client, "rev-batch-1", tn.ID)
	u2 := newRevocationTestUser(t, ctx, client, "rev-batch-2", tn.ID)
	untouched := newRevocationTestUser(t, ctx, client, "rev-batch-3", tn.ID)

	probe := time.Now().Add(-time.Minute)
	untouchedBefore := refreshRevokedAt(t, ctx, untouched.ID, probe)

	require.NoError(t, svc.BatchUpdateUsers(ctx, &dto.BatchUpdateUsersRequest{
		UserIDs: []int{u1.ID, u2.ID},
		Action:  "deactivate",
	}, tn.ID))

	for _, u := range []*ent.User{u1, u2} {
		assert.True(t, refreshRevokedAt(t, ctx, u.ID, probe), "批量停用后存量 refresh 必须被吊销（user_id=%d）", u.ID)
	}

	// 作用域隔离：未涉及用户不受影响（带 ID 复用冲突守卫）。
	if untouchedBefore {
		t.Logf("note: untouched user id %d 已被更早用例吊销（ID 复用），跳过反向断言", untouched.ID)
	} else {
		assert.False(t, refreshRevokedAt(t, ctx, untouched.ID, probe), "批量停用不得影响未涉及用户")
	}

	// 非 deactivate 动作（department）不应触发吊销。
	probe2 := time.Now().Add(-time.Minute)
	deptBefore := refreshRevokedAt(t, ctx, untouched.ID, probe2)
	require.NoError(t, svc.BatchUpdateUsers(ctx, &dto.BatchUpdateUsersRequest{
		UserIDs: []int{untouched.ID},
		Action:  "department", Department: "IT",
	}, tn.ID))
	if !deptBefore {
		assert.False(t, refreshRevokedAt(t, ctx, untouched.ID, probe2), "department 动作不得吊销会话")
	}
}
