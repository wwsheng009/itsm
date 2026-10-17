package project

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"itsm-backend/ent/user"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestMyMilestonesAggregation 个人里程碑到期聚合（PE-16）：
// owner=当前用户、仅 planned、默认 N 天窗口（含逾期）、非成员不可见、admin 直通、limit 收敛。
func TestMyMilestonesAggregation(t *testing.T) {
	svc, client, tid := newTestEnv(t)
	ctx := context.Background()

	member := seedUserWithMembership(t, client, tid, "myms", "home", nil)
	other := seedUserWithMembership(t, client, tid, "myms-other", "home", nil)
	outsider := seedUserWithMembership(t, client, tid, "myms-out", "home", nil)

	p, err := svc.Create(ctx, &CreateRequest{TenantID: tid, Name: "到期聚合项目"})
	require.NoError(t, err)
	_, err = svc.AddMember(ctx, tid, p.ID, member.ID, &MemberAddRequest{UserID: member.ID})
	require.NoError(t, err)
	_, err = svc.AddMember(ctx, tid, p.ID, other.ID, &MemberAddRequest{UserID: other.ID})
	require.NoError(t, err)

	soon := time.Now().Add(3 * 24 * time.Hour)
	far := time.Now().Add(90 * 24 * time.Hour)
	overdue := time.Now().Add(-24 * time.Hour)

	// 我在期：近期（窗口内）+ 逾期（含）+ 远期（窗口外）。
	_, err = svc.CreateMilestone(ctx, tid, p.ID, member.ID, &MilestoneCreateRequest{Name: "近期里程碑", DueDate: &soon, OwnerID: member.ID})
	require.NoError(t, err)
	_, err = svc.CreateMilestone(ctx, tid, p.ID, member.ID, &MilestoneCreateRequest{Name: "逾期里程碑", DueDate: &overdue, OwnerID: member.ID})
	require.NoError(t, err)
	_, err = svc.CreateMilestone(ctx, tid, p.ID, member.ID, &MilestoneCreateRequest{Name: "远期里程碑", DueDate: &far, OwnerID: member.ID})
	require.NoError(t, err)

	// 他人负责 + 已达成（reached）均不计入。
	_, err = svc.CreateMilestone(ctx, tid, p.ID, member.ID, &MilestoneCreateRequest{Name: "他人里程碑", DueDate: &soon, OwnerID: other.ID})
	require.NoError(t, err)
	reached, err := svc.CreateMilestone(ctx, tid, p.ID, member.ID, &MilestoneCreateRequest{Name: "已达成里程碑", DueDate: &soon, OwnerID: member.ID})
	require.NoError(t, err)
	_, err = svc.TransitionMilestone(ctx, tid, p.ID, reached.ID, &MilestoneStatusRequest{Action: MilestoneActionReach})
	require.NoError(t, err)

	// 默认 30 天窗口：近期 + 逾期（due_date 升序 → 逾期在前），附带项目名/编号与逾期标记。
	items, err := svc.MyMilestones(ctx, tid, member.ID, false, 30, 50)
	require.NoError(t, err)
	require.Len(t, items, 2)
	assert.Equal(t, "逾期里程碑", items[0].Name)
	assert.True(t, items[0].Overdue)
	assert.Negative(t, items[0].DaysLeft)
	assert.False(t, items[1].Overdue)
	assert.Equal(t, "到期聚合项目", items[0].ProjectName)
	assert.NotEmpty(t, items[0].ProjectCode)

	// 不限窗口：3 条（含远期）。
	all, err := svc.MyMilestones(ctx, tid, member.ID, false, 0, 50)
	require.NoError(t, err)
	assert.Len(t, all, 3)

	// limit 收敛：请求 1 条 → 仅最早一条。
	one, err := svc.MyMilestones(ctx, tid, member.ID, false, 0, 1)
	require.NoError(t, err)
	require.Len(t, one, 1)
	assert.Equal(t, "逾期里程碑", one[0].Name)

	// 他人视角：只看到自己负责的。
	mine, err := svc.MyMilestones(ctx, tid, other.ID, false, 0, 50)
	require.NoError(t, err)
	require.Len(t, mine, 1)
	assert.Equal(t, "他人里程碑", mine[0].Name)

	// admin 直通（owner 过滤仍生效；不受成员身份限制）。
	adminView, err := svc.MyMilestones(ctx, tid, other.ID, true, 0, 50)
	require.NoError(t, err)
	assert.Len(t, adminView, 1)

	// 无任何可见项目的用户 → 空（fail-closed）。
	empty, err := svc.MyMilestones(ctx, tid, outsider.ID, false, 0, 50)
	require.NoError(t, err)
	assert.Empty(t, empty)

	// 成员回收/移除后：可见性收窄生效（本人不再看到项目内里程碑）。
	require.NoError(t, svc.RemoveMember(ctx, tid, p.ID, member.ID))
	afterRemoval, err := svc.MyMilestones(ctx, tid, member.ID, false, 0, 50)
	require.NoError(t, err)
	assert.Empty(t, afterRemoval)
}

// TestHandlerMyMilestonesEndpoint 端点映射 + 参数校验（PE-16）。
func TestHandlerMyMilestonesEndpoint(t *testing.T) {
	r, client, tid := newHandlerEnv(t)
	ctx := context.Background()

	admin, err := client.User.Query().Where(user.TenantIDEQ(tid)).First(ctx)
	require.NoError(t, err)

	p, err := client.Project.Create().
		SetTenantID(tid).SetName("端点到期项目").SetCode("PE16").SetManagerID(admin.ID).
		Save(ctx)
	require.NoError(t, err)
	_, err = client.ProjectMember.Create().
		SetTenantID(tid).SetProjectID(p.ID).SetUserID(admin.ID).SetProjectRole("pm").
		Save(ctx)
	require.NoError(t, err)
	_, err = client.ProjectMilestone.Create().
		SetTenantID(tid).SetProjectID(p.ID).SetName("端点里程碑").
		SetDueDate(time.Now().Add(5 * 24 * time.Hour)).SetOwnerID(admin.ID).SetStatus(MilestoneStatusPlanned).
		Save(ctx)
	require.NoError(t, err)

	w, env := doJSON(t, r, http.MethodGet, "/projects/my-milestones", nil)
	require.Equal(t, http.StatusOK, w.Code)
	var items []MyMilestoneResponse
	require.NoError(t, json.Unmarshal(env.Data, &items))
	require.Len(t, items, 1)
	assert.Equal(t, "端点到期项目", items[0].ProjectName)
	assert.Equal(t, "PE16", items[0].ProjectCode)
	assert.False(t, items[0].Overdue)
	assert.GreaterOrEqual(t, items[0].DaysLeft, 4)

	// 参数校验：dueWithinDays 非负整数、limit 正整数。
	w, env = doJSON(t, r, http.MethodGet, "/projects/my-milestones?dueWithinDays=abc", nil)
	require.Equal(t, http.StatusBadRequest, w.Code)
	w, env = doJSON(t, r, http.MethodGet, "/projects/my-milestones?limit=0", nil)
	require.Equal(t, http.StatusBadRequest, w.Code)

	// 窗口为 0 = 不限：远期也在。
	w, env = doJSON(t, r, http.MethodGet, "/projects/my-milestones?dueWithinDays=0&limit=1", nil)
	require.Equal(t, http.StatusOK, w.Code)
	require.NoError(t, json.Unmarshal(env.Data, &items))
	require.Len(t, items, 1)
}