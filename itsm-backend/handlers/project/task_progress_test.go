package project

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestProjectProgressAggregatesLeafTasks 项目进度 = 未取消叶子任务进度的平均值。
//
// 覆盖 2026-10-18 E2E 发现的缺陷：任务完成/进度更新后 projects.progress 仍为 0。
// 口径：父任务不计入（由子任务汇总）；cancelled 与软删任务排除；四舍五入。
func TestProjectProgressAggregatesLeafTasks(t *testing.T) {
	svc, _, tid := newTestEnv(t)
	ctx := context.Background()

	p, err := svc.Create(ctx, &CreateRequest{TenantID: tid, Name: "进度汇总项目"})
	require.NoError(t, err)
	initialVersion := p.Version

	parent, err := svc.CreateTask(ctx, tid, p.ID, 0, &TaskCreateRequest{Name: "阶段一"})
	require.NoError(t, err)
	childA, err := svc.CreateTask(ctx, tid, p.ID, 0, &TaskCreateRequest{Name: "实施 A", ParentTaskID: parent.ID})
	require.NoError(t, err)
	childB, err := svc.CreateTask(ctx, tid, p.ID, 0, &TaskCreateRequest{Name: "实施 B"})
	require.NoError(t, err)

	// 父任务有子任务 → 不计入；两个叶子默认 0。
	got := getProject(t, svc, tid, p.ID)
	assert.Equal(t, 0, got.Progress)

	// A=100, B=0 → 50。
	prog100 := 100
	_, err = svc.UpdateTask(ctx, tid, p.ID, childA.ID, &TaskUpdateRequest{Progress: &prog100})
	require.NoError(t, err)
	got = getProject(t, svc, tid, p.ID)
	assert.Equal(t, 50, got.Progress)

	// A=100, B=50 → 75。
	prog50 := 50
	_, err = svc.UpdateTask(ctx, tid, p.ID, childB.ID, &TaskUpdateRequest{Progress: &prog50})
	require.NoError(t, err)
	got = getProject(t, svc, tid, p.ID)
	assert.Equal(t, 75, got.Progress)

	// 取消 B → 只剩 A=100。
	_, err = svc.TransitionTask(ctx, tid, p.ID, childB.ID, &TaskStatusRequest{Action: TaskActionCancel})
	require.NoError(t, err)
	got = getProject(t, svc, tid, p.ID)
	assert.Equal(t, 100, got.Progress)

	// 软删 A → 无有效叶子任务 → 0。
	require.NoError(t, svc.DeleteTask(ctx, tid, p.ID, childA.ID))
	got = getProject(t, svc, tid, p.ID)
	assert.Equal(t, 0, got.Progress)

	// 派生进度更新不应改变项目乐观锁版本（避免误伤用户编辑）。
	assert.Equal(t, initialVersion, got.Version)
}

func getProject(t *testing.T, svc *Service, tenantID, projectID int) *ProjectResponse {
	t.Helper()
	p, err := svc.Get(context.Background(), tenantID, projectID)
	require.NoError(t, err)
	return ToResponse(p)
}
