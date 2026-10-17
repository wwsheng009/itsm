package project

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"testing"
	"time"

	"itsm-backend/common"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestMilestoneWorkflow 里程碑 CRUD + 状态流转 + 负责人校验（PE-14）。
func TestMilestoneWorkflow(t *testing.T) {
	svc, client, tid := newTestEnv(t)
	ctx := context.Background()

	member := seedUserWithMembership(t, client, tid, "ms", "home", nil)
	p, err := svc.Create(ctx, &CreateRequest{TenantID: tid, Name: "里程碑项目"})
	require.NoError(t, err)
	_, err = svc.AddMember(ctx, tid, p.ID, member.ID, &MemberAddRequest{UserID: member.ID})
	require.NoError(t, err)

	due := time.Now().Add(14 * 24 * time.Hour)
	m1, err := svc.CreateMilestone(ctx, tid, p.ID, member.ID, &MilestoneCreateRequest{
		Name: "需求评审通过", DueDate: &due, OwnerID: member.ID,
	})
	require.NoError(t, err)
	assert.Equal(t, MilestoneStatusPlanned, m1.Status)

	// 非成员负责人拒绝。
	outsider := seedUserWithMembership(t, client, tid, "ms-out", "home", nil)
	_, err = svc.CreateMilestone(ctx, tid, p.ID, member.ID, &MilestoneCreateRequest{Name: "外部负责人", DueDate: &due, OwnerID: outsider.ID})
	require.ErrorIs(t, err, ErrMilestoneOwnerNotMember)

	// reach：planned → reached + reached_at 回填。
	reached, err := svc.TransitionMilestone(ctx, tid, p.ID, m1.ID, &MilestoneStatusRequest{Action: MilestoneActionReach})
	require.NoError(t, err)
	assert.Equal(t, MilestoneStatusReached, reached.Status)
	require.NotNil(t, reached.ReachedAt)

	// reached 后再 reach/miss 均非法。
	_, err = svc.TransitionMilestone(ctx, tid, p.ID, m1.ID, &MilestoneStatusRequest{Action: MilestoneActionMiss})
	require.ErrorIs(t, err, ErrInvalidMilestoneState)

	// miss：planned → missed。
	m2, err := svc.CreateMilestone(ctx, tid, p.ID, member.ID, &MilestoneCreateRequest{Name: "上线演练", DueDate: &due})
	require.NoError(t, err)
	missed, err := svc.TransitionMilestone(ctx, tid, p.ID, m2.ID, &MilestoneStatusRequest{Action: MilestoneActionMiss})
	require.NoError(t, err)
	assert.Equal(t, MilestoneStatusMissed, missed.Status)

	// missed → reached 允许（补达成）。
	late, err := svc.TransitionMilestone(ctx, tid, p.ID, m2.ID, &MilestoneStatusRequest{Action: MilestoneActionReach})
	require.NoError(t, err)
	assert.Equal(t, MilestoneStatusReached, late.Status)

	// 列表 + 更新 + 删除。
	items, err := svc.ListMilestones(ctx, tid, p.ID, member.ID, true, "")
	require.NoError(t, err)
	assert.Len(t, items, 2)
	newName := "需求评审（终稿）"
	updated, err := svc.UpdateMilestone(ctx, tid, p.ID, m1.ID, &MilestoneUpdateRequest{Name: &newName})
	require.NoError(t, err)
	assert.Equal(t, newName, updated.Name)
	require.NoError(t, svc.DeleteMilestone(ctx, tid, p.ID, m1.ID))
	_, err = svc.repo.GetMilestone(ctx, tid, p.ID, m1.ID)
	require.Error(t, err)
}

// TestDependencyWorkflow 依赖：FS/重复/自依赖/跨项目/成环/删除（PE-14）。
func TestDependencyWorkflow(t *testing.T) {
	svc, client, tid := newTestEnv(t)
	ctx := context.Background()

	member := seedUserWithMembership(t, client, tid, "dep", "home", nil)
	p, err := svc.Create(ctx, &CreateRequest{TenantID: tid, Name: "依赖项目"})
	require.NoError(t, err)
	_, err = svc.AddMember(ctx, tid, p.ID, member.ID, &MemberAddRequest{UserID: member.ID})
	require.NoError(t, err)

	taskA, err := svc.CreateTask(ctx, tid, p.ID, member.ID, &TaskCreateRequest{Name: "任务A"})
	require.NoError(t, err)
	taskB, err := svc.CreateTask(ctx, tid, p.ID, member.ID, &TaskCreateRequest{Name: "任务B"})
	require.NoError(t, err)
	taskC, err := svc.CreateTask(ctx, tid, p.ID, member.ID, &TaskCreateRequest{Name: "任务C"})
	require.NoError(t, err)

	// A 依赖 B（A → B）。
	dep, err := svc.AddDependency(ctx, tid, p.ID, taskA.ID, &DependencyCreateRequest{DependsOnTaskID: taskB.ID})
	require.NoError(t, err)
	assert.Equal(t, "fs", dep.DepType)

	// 重复 / 自依赖。
	_, err = svc.AddDependency(ctx, tid, p.ID, taskA.ID, &DependencyCreateRequest{DependsOnTaskID: taskB.ID})
	require.ErrorIs(t, err, ErrDependencyExists)
	_, err = svc.AddDependency(ctx, tid, p.ID, taskA.ID, &DependencyCreateRequest{DependsOnTaskID: taskA.ID})
	require.ErrorIs(t, err, ErrDependencySelf)

	// B 再依赖 C（B → C），合法。
	_, err = svc.AddDependency(ctx, tid, p.ID, taskB.ID, &DependencyCreateRequest{DependsOnTaskID: taskC.ID})
	require.NoError(t, err)

	// C 依赖 A → 成环（A→B→C→A）。
	_, err = svc.AddDependency(ctx, tid, p.ID, taskC.ID, &DependencyCreateRequest{DependsOnTaskID: taskA.ID})
	require.ErrorIs(t, err, ErrDependencyCycle)

	// 跨项目任务 → 404 语义。
	other, err := svc.Create(ctx, &CreateRequest{TenantID: tid, Name: "另一个项目"})
	require.NoError(t, err)
	otherTask, err := svc.CreateTask(ctx, tid, other.ID, member.ID, &TaskCreateRequest{Name: "别家任务"})
	require.NoError(t, err)
	_, err = svc.AddDependency(ctx, tid, p.ID, taskA.ID, &DependencyCreateRequest{DependsOnTaskID: otherTask.ID})
	require.ErrorIs(t, err, ErrTaskNotFound)

	// SS/FF 非 v1 范围。
	_, err = svc.AddDependency(ctx, tid, p.ID, taskA.ID, &DependencyCreateRequest{DependsOnTaskID: taskC.ID, DepType: "ss"})
	require.ErrorIs(t, err, ErrValidation)

	// 列表 + 删除 + 重复删除 404。
	deps, err := svc.ListDependencies(ctx, tid, p.ID, member.ID, true)
	require.NoError(t, err)
	assert.Len(t, deps, 2)
	require.NoError(t, svc.RemoveDependency(ctx, tid, p.ID, taskA.ID, taskB.ID))
	require.ErrorIs(t, svc.RemoveDependency(ctx, tid, p.ID, taskA.ID, taskB.ID), ErrDependencyNotFound)
}

// TestHandlerMilestoneAndDependencyEndpoints 端点级（祖先路径 + 409 映射）。
func TestHandlerMilestoneAndDependencyEndpoints(t *testing.T) {
	r, _, _ := newHandlerEnv(t)

	_, env := doJSON(t, r, http.MethodPost, "/projects", map[string]any{"name": "P1 接口项目"})
	var created ProjectResponse
	require.NoError(t, json.Unmarshal(env.Data, &created))

	// 里程碑：创建 + 列表 + 非法流转 409。
	due := time.Now().Add(7 * 24 * time.Hour).Format(time.RFC3339)
	w, env := doJSON(t, r, http.MethodPost, fmt.Sprintf("/projects/%d/milestones", created.ID), map[string]any{
		"name": "计划评审", "dueDate": due,
	})
	require.Equal(t, http.StatusOK, w.Code)
	var m ProjectMilestoneResponse
	require.NoError(t, json.Unmarshal(env.Data, &m))
	assert.Equal(t, MilestoneStatusPlanned, m.Status)

	w, _ = doJSON(t, r, http.MethodPost, fmt.Sprintf("/projects/%d/milestones/%d/status", created.ID, m.ID), map[string]any{"action": "bogus"})
	assert.Equal(t, http.StatusConflict, w.Code)

	_, env = doJSON(t, r, http.MethodGet, fmt.Sprintf("/projects/%d/milestones", created.ID), nil)
	var list []ProjectMilestoneResponse
	require.NoError(t, json.Unmarshal(env.Data, &list))
	require.Len(t, list, 1)

	// 依赖：建两个任务 + 自依赖 400 + 正常 200。
	mkTask := func(name string) int {
		_, e := doJSON(t, r, http.MethodPost, fmt.Sprintf("/projects/%d/tasks", created.ID), map[string]any{"name": name})
		var t2 ProjectTaskResponse
		require.NoError(t, json.Unmarshal(e.Data, &t2))
		return t2.ID
	}
	ta, tb := mkTask("A"), mkTask("B")
	w, _ = doJSON(t, r, http.MethodPost, fmt.Sprintf("/projects/%d/tasks/%d/dependencies", created.ID, ta), map[string]any{"dependsOnTaskId": ta})
	assert.Equal(t, http.StatusBadRequest, w.Code)
	w, env = doJSON(t, r, http.MethodPost, fmt.Sprintf("/projects/%d/tasks/%d/dependencies", created.ID, ta), map[string]any{"dependsOnTaskId": tb})
	require.Equal(t, http.StatusOK, w.Code)
	assert.Equal(t, common.SuccessCode, env.Code)

	// 重复 → 409。
	w, _ = doJSON(t, r, http.MethodPost, fmt.Sprintf("/projects/%d/tasks/%d/dependencies", created.ID, ta), map[string]any{"dependsOnTaskId": tb})
	assert.Equal(t, http.StatusConflict, w.Code)

}
