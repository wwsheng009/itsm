package project

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ---------------------------------------------------------------------------
// 2026-10-21 第三轮回归遗留（B1 工时静默钳制 / 越界值污染汇总）
// 上游：docs/project-management/reviews/2026-10-21-项目功能回归验证报告-第三轮.md
// ---------------------------------------------------------------------------

// TestTaskHoursUpperBoundRejects B1：超限工时一律 400（不再静默截断），模板同口径。
func TestTaskHoursUpperBoundRejects(t *testing.T) {
	svc, client, tid := newTestEnv(t)
	ctx := context.Background()
	member := seedUserWithMembership(t, client, tid, "b1-hours", "home", nil)
	p, err := svc.Create(ctx, &CreateRequest{TenantID: tid, Name: "工时上限项目"})
	require.NoError(t, err)
	_, err = svc.AddMember(ctx, tid, p.ID, member.ID, &MemberAddRequest{UserID: member.ID})
	require.NoError(t, err)

	// 第三轮报告实测值 999999999 → 拒绝（不是钳制到上限后落库）。
	over := float64(999999999)
	_, err = svc.CreateTask(ctx, tid, p.ID, member.ID, &TaskCreateRequest{
		Name: "越界工时任务", EstimateHours: &over,
	})
	require.ErrorIs(t, err, ErrValidation)
	assert.Contains(t, err.Error(), "estimateHours")

	// 恰好等于上限 → 通过。
	atLimit := float64(MaxTaskHours)
	task, err := svc.CreateTask(ctx, tid, p.ID, member.ID, &TaskCreateRequest{
		Name: "上限工时任务", EstimateHours: &atLimit,
	})
	require.NoError(t, err)
	assert.Equal(t, atLimit, task.EstimateHours)

	// 更新路径同样拒绝。
	over2 := float64(MaxTaskHours + 1)
	_, err = svc.UpdateTask(ctx, tid, p.ID, task.ID, &TaskUpdateRequest{EstimateHours: &over2})
	require.ErrorIs(t, err, ErrValidation)
}

// TestHoursSummaryExcludesInvalid B1：历史越界工时不计入汇总，并单独回报（不静默）。
func TestHoursSummaryExcludesInvalid(t *testing.T) {
	svc, client, tid := newTestEnv(t)
	ctx := context.Background()
	member := seedUserWithMembership(t, client, tid, "b1-sum", "home", nil)
	p, err := svc.Create(ctx, &CreateRequest{TenantID: tid, Name: "工时汇总护栏"})
	require.NoError(t, err)
	_, err = svc.AddMember(ctx, tid, p.ID, member.ID, &MemberAddRequest{UserID: member.ID})
	require.NoError(t, err)

	// 正常任务 9h。
	nine := 9.0
	_, err = svc.CreateTask(ctx, tid, p.ID, member.ID, &TaskCreateRequest{Name: "正常任务", EstimateHours: &nine})
	require.NoError(t, err)

	// 模拟历史脏数据：直接写库绕过服务层校验（第三轮实测 1000000h 已落库）。
	_, err = client.ProjectTask.Create().
		SetTenantID(tid).SetProjectID(p.ID).SetName("历史越界任务").
		SetStatus("todo").SetEstimateHours(1000000).
		Save(ctx)
	require.NoError(t, err)

	summary, err := svc.HoursSummary(ctx, tid, p.ID, member.ID, true)
	require.NoError(t, err)
	assert.InDelta(t, 9.0, summary.EstimateHours, 1e-9, "越界值不得计入合计（否则得到 1000009）")
	assert.Equal(t, 2, summary.TaskCount, "任务总数仍按全量统计")
	assert.Equal(t, 1, summary.EstimatedTaskCount, "仅正常任务计入已填报")
	assert.Equal(t, 1, summary.InvalidHoursTasks)
	assert.InDelta(t, 1000000.0, summary.InvalidEstimateHours, 1e-9)

	// 响应层把口径与排除量一并回传（前端据此提示）。
	resp := ToHoursSummaryResponse(summary)
	require.NotNil(t, resp)
	assert.Equal(t, 1, resp.InvalidHoursTasks)
	assert.Equal(t, float64(MaxTaskHours), resp.MaxTaskHours)
}
