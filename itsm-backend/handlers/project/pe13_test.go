package project

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestBaselineFreezeAndDiff 计划基线：冻结（不可变 + 版本递增 + reason 规则）与偏差计算（PE-13）。
func TestBaselineFreezeAndDiff(t *testing.T) {
	svc, client, tid := newTestEnv(t)
	ctx := context.Background()

	member := seedUserWithMembership(t, client, tid, "bl", "home", nil)
	p, err := svc.Create(ctx, &CreateRequest{TenantID: tid, Name: "基线项目"})
	require.NoError(t, err)
	_, err = svc.AddMember(ctx, tid, p.ID, member.ID, &MemberAddRequest{UserID: member.ID})
	require.NoError(t, err)

	planStart := time.Now().Add(24 * time.Hour)
	planEnd := time.Now().Add(48 * time.Hour)
	t1, err := svc.CreateTask(ctx, tid, p.ID, member.ID, &TaskCreateRequest{Name: "任务一", PlanStart: &planStart, PlanEnd: &planEnd})
	require.NoError(t, err)
	t2, err := svc.CreateTask(ctx, tid, p.ID, member.ID, &TaskCreateRequest{Name: "任务二", PlanStart: &planStart, PlanEnd: &planEnd})
	require.NoError(t, err)
	due := time.Now().Add(7 * 24 * time.Hour)
	_, err = svc.CreateMilestone(ctx, tid, p.ID, member.ID, &MilestoneCreateRequest{Name: "里程碑一", DueDate: &due})
	require.NoError(t, err)

	// 无基线时 diff → 404 语义。
	_, err = svc.BaselineDiff(ctx, tid, p.ID, member.ID, true)
	require.ErrorIs(t, err, ErrBaselineNotFound)

	// 冻结 v1（首版 reason 可选）。
	base1, err := svc.CreateBaseline(ctx, tid, p.ID, member.ID, &BaselineCreateRequest{})
	require.NoError(t, err)
	assert.Equal(t, 1, base1.Version)
	snap, err := snapshotFromMap(base1.Snapshot)
	require.NoError(t, err)
	assert.Equal(t, BaselineSnapshotSchemaVersion, snap.SchemaVersion)
	require.Len(t, snap.Tasks, 2)
	require.Len(t, snap.Milestones, 1)
	assert.Equal(t, "基线项目", snap.Project.Name)

	// 后续版本缺 reason → 400 语义（计划变更必须留因）。
	_, err = svc.CreateBaseline(ctx, tid, p.ID, member.ID, &BaselineCreateRequest{})
	require.ErrorIs(t, err, ErrValidation)

	// 计划变更：任务二计划结束 +3 天、进度 40；新增任务三；删除任务一。
	newEnd := planEnd.Add(72 * time.Hour)
	prog := 40
	_, err = svc.UpdateTask(ctx, tid, p.ID, t2.ID, &TaskUpdateRequest{PlanEnd: &newEnd, Progress: &prog})
	require.NoError(t, err)
	_, err = svc.CreateTask(ctx, tid, p.ID, member.ID, &TaskCreateRequest{Name: "任务三"})
	require.NoError(t, err)
	require.NoError(t, svc.DeleteTask(ctx, tid, p.ID, t1.ID))

	// diff（v1 vs 当前）：t2 changed（+3 天/进度 +40）、t3 added、t1 removed。
	diff, err := svc.BaselineDiff(ctx, tid, p.ID, member.ID, true)
	require.NoError(t, err)
	assert.Equal(t, 1, diff.Version)
	assert.Equal(t, BaselineDiffSummary{Added: 1, Removed: 1, Changed: 1}, diff.TaskSummary)
	var t2Dev *BaselineTaskDeviation
	for i := range diff.TaskChanges {
		if diff.TaskChanges[i].TaskID == t2.ID {
			t2Dev = &diff.TaskChanges[i]
		}
	}
	require.NotNil(t, t2Dev)
	require.NotNil(t, t2Dev.PlanEndShiftDays)
	assert.Equal(t, 3, *t2Dev.PlanEndShiftDays)
	assert.Equal(t, 40, t2Dev.ProgressDelta)
	assert.Equal(t, BaselineChangeChanged, t2Dev.ChangeType)
	assert.Equal(t, BaselineDiffSummary{Unchanged: 1}, diff.MilestoneSummary)

	// 冻结 v2（reason 必填）+ 列表倒序。
	base2, err := svc.CreateBaseline(ctx, tid, p.ID, member.ID, &BaselineCreateRequest{Reason: "范围调整：新增任务三"})
	require.NoError(t, err)
	assert.Equal(t, 2, base2.Version)
	assert.Equal(t, "范围调整：新增任务三", base2.Reason)

	list, err := svc.ListBaselines(ctx, tid, p.ID, member.ID, true)
	require.NoError(t, err)
	require.Len(t, list, 2)
	assert.Equal(t, 2, list[0].Version)

	// v2 冻结后即时 diff：全部无偏差。
	diff2, err := svc.BaselineDiff(ctx, tid, p.ID, member.ID, true)
	require.NoError(t, err)
	assert.Equal(t, 2, diff2.Version)
	assert.Equal(t, BaselineDiffSummary{Unchanged: 2}, diff2.TaskSummary)

	// v2 之后再变更：t2 再 +2 天、进度 60；新增 t4；删除 t3。
	newEnd2 := newEnd.Add(48 * time.Hour)
	prog2 := 60
	_, err = svc.UpdateTask(ctx, tid, p.ID, t2.ID, &TaskUpdateRequest{PlanEnd: &newEnd2, Progress: &prog2})
	require.NoError(t, err)
	_, err = svc.CreateTask(ctx, tid, p.ID, member.ID, &TaskCreateRequest{Name: "任务四"})
	require.NoError(t, err)
	t3, err := svc.repo.ListTasks(ctx, tid, p.ID, "", 0)
	require.NoError(t, err)
	var t3ID int
	for _, it := range t3 {
		if it.Name == "任务三" {
			t3ID = it.ID
		}
	}
	require.NotZero(t, t3ID)
	require.NoError(t, svc.DeleteTask(ctx, tid, p.ID, t3ID))

	diff3, err := svc.BaselineDiff(ctx, tid, p.ID, member.ID, true)
	require.NoError(t, err)
	assert.Equal(t, BaselineDiffSummary{Added: 1, Removed: 1, Changed: 1}, diff3.TaskSummary)

	// 冻结 v3 后 diff 收敛为全量 unchanged。
	_, err = svc.CreateBaseline(ctx, tid, p.ID, member.ID, &BaselineCreateRequest{Reason: "计划再基线"})
	require.NoError(t, err)
	diff4, err := svc.BaselineDiff(ctx, tid, p.ID, member.ID, true)
	require.NoError(t, err)
	assert.Equal(t, 3, diff4.Version)
	assert.Equal(t, BaselineDiffSummary{Unchanged: 2}, diff4.TaskSummary)
	assert.Equal(t, BaselineDiffSummary{Unchanged: 1}, diff4.MilestoneSummary)

	// 终态项目拒绝冻结。
	_, err = svc.Transition(ctx, tid, p.ID, &StatusRequest{Action: "cancel", Reason: "需求取消"})
	require.NoError(t, err)
	_, err = svc.CreateBaseline(ctx, tid, p.ID, member.ID, &BaselineCreateRequest{Reason: "关闭后冻结"})
	require.ErrorIs(t, err, ErrValidation)
}

// TestHandlerBaselineEndpoints 端点级：冻结/列表/偏差 + 404/400 映射（PE-13）。
func TestHandlerBaselineEndpoints(t *testing.T) {
	r, _, _ := newHandlerEnv(t)

	_, env := doJSON(t, r, http.MethodPost, "/projects", map[string]any{"name": "基线接口项目"})
	var created ProjectResponse
	require.NoError(t, json.Unmarshal(env.Data, &created))

	// 无基线 → diff 404。
	w, _ := doJSON(t, r, http.MethodGet, fmt.Sprintf("/projects/%d/baselines/diff", created.ID), nil)
	require.Equal(t, http.StatusNotFound, w.Code)

	// 空 body 冻结 v1。
	w, env = doJSON(t, r, http.MethodPost, fmt.Sprintf("/projects/%d/baselines", created.ID), nil)
	require.Equal(t, http.StatusOK, w.Code)
	var b1 ProjectBaselineResponse
	require.NoError(t, json.Unmarshal(env.Data, &b1))
	assert.Equal(t, 1, b1.Version)
	require.NotNil(t, b1.Snapshot)
	assert.Equal(t, BaselineSnapshotSchemaVersion, b1.Snapshot.SchemaVersion)

	// 列表。
	w, env = doJSON(t, r, http.MethodGet, fmt.Sprintf("/projects/%d/baselines", created.ID), nil)
	require.Equal(t, http.StatusOK, w.Code)
	var list []ProjectBaselineResponse
	require.NoError(t, json.Unmarshal(env.Data, &list))
	assert.Len(t, list, 1)

	// diff 200。
	w, env = doJSON(t, r, http.MethodGet, fmt.Sprintf("/projects/%d/baselines/diff", created.ID), nil)
	require.Equal(t, http.StatusOK, w.Code)
	var diff BaselineDiffResponse
	require.NoError(t, json.Unmarshal(env.Data, &diff))
	assert.Equal(t, 1, diff.Version)

	// v2 缺 reason → 400；带 reason → 200。
	w, _ = doJSON(t, r, http.MethodPost, fmt.Sprintf("/projects/%d/baselines", created.ID), map[string]any{})
	require.Equal(t, http.StatusBadRequest, w.Code)
	w, env = doJSON(t, r, http.MethodPost, fmt.Sprintf("/projects/%d/baselines", created.ID), map[string]any{"reason": "计划变更"})
	require.Equal(t, http.StatusOK, w.Code)
	var b2 ProjectBaselineResponse
	require.NoError(t, json.Unmarshal(env.Data, &b2))
	assert.Equal(t, 2, b2.Version)
	assert.Equal(t, "计划变更", b2.Reason)
}
