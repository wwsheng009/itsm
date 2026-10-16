package ticket

import (
	"testing"

	"itsm-backend/repository/base"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestRepository_List_DataScopeOwnedOrAssigned（2026-10-16 回归锁定）：
// 行级数据权限（阻断8）：OwnedOrAssigned 时仅返回「本人创建或被指派」的工单；
// CurrentUserID 缺失时 fail-closed（空集）。
//
// 背景：该谓词曾被上层跨包枚举直接转换静默绕过
// （datascope.OwnedOrAssigned=2 → ticket.DataScope(2) 不匹配 OwnedOrAssigned=1，
// 等价全量）。handlers/ticket 侧已改显式映射（toRepositoryDataScope），
// 本测试锁定仓储层契约本体，防止再次回归。
func TestRepository_List_DataScopeOwnedOrAssigned(t *testing.T) {
	fx := newRepoFixture(t)
	defer fx.client.Close()
	ctx := fx.ctx

	bob, err := fx.client.User.Create().
		SetUsername("bob").
		SetEmail("bob@test.com").
		SetName("Bob").
		SetPasswordHash("hash").
		SetRole("end_user").
		SetActive(true).
		SetTenantID(fx.tenant.ID).
		Save(ctx)
	require.NoError(t, err)

	mine, err := fx.repo.Create(ctx, &CreateParams{
		Title: "mine (requester=alice)", Priority: PriorityMedium, Type: TypeIncident, RequesterID: fx.user.ID,
	}, fx.tenant.ID)
	require.NoError(t, err)

	bobTicket, err := fx.repo.Create(ctx, &CreateParams{
		Title: "bob's ticket", Priority: PriorityMedium, Type: TypeIncident, RequesterID: bob.ID,
	}, fx.tenant.ID)
	require.NoError(t, err)

	assignee := fx.user.ID
	assignedToMe, err := fx.repo.Create(ctx, &CreateParams{
		Title: "assigned to alice", Priority: PriorityMedium, Type: TypeIncident,
		RequesterID: bob.ID, AssigneeID: &assignee,
	}, fx.tenant.ID)
	require.NoError(t, err)

	page := &base.QueryParams{Page: 1, PageSize: 50}

	// 本人 scope：自建 + 被指派 = 2 条；他人单据不可见。
	scoped, err := fx.repo.List(ctx, fx.tenant.ID, &FilterParams{
		DataScope: DataScopeOwnedOrAssigned, CurrentUserID: fx.user.ID,
	}, page)
	require.NoError(t, err)
	assert.Equal(t, 2, scoped.Total)
	ids := make(map[int]bool, len(scoped.Data))
	for _, tk := range scoped.Data {
		ids[tk.ID] = true
	}
	assert.True(t, ids[mine.ID], "requester=me 必须可见")
	assert.True(t, ids[assignedToMe.ID], "assignee=me 必须可见")
	assert.False(t, ids[bobTicket.ID], "他人单据不得可见")

	// 全量 scope：3 条。
	all, err := fx.repo.List(ctx, fx.tenant.ID, &FilterParams{DataScope: DataScopeAll}, page)
	require.NoError(t, err)
	assert.Equal(t, 3, all.Total)

	// fail-closed：未提供用户 ID 时返回空集而非全量。
	none, err := fx.repo.List(ctx, fx.tenant.ID, &FilterParams{
		DataScope: DataScopeOwnedOrAssigned, CurrentUserID: 0,
	}, page)
	require.NoError(t, err)
	assert.Equal(t, 0, none.Total)
}
