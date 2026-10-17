package project

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"testing"

	"itsm-backend/common"
	"itsm-backend/ent/user"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestTaskWorkflow 任务生命周期 + WBS 深度 + 成员校验 + 删除门禁。
func TestTaskWorkflow(t *testing.T) {
	svc, client, tid := newTestEnv(t)
	ctx := context.Background()

	member := seedUserWithMembership(t, client, tid, "task", "home", nil)
	p, err := svc.Create(ctx, &CreateRequest{TenantID: tid, Name: "任务项目"})
	require.NoError(t, err)
	_, err = svc.AddMember(ctx, tid, p.ID, member.ID, &MemberAddRequest{UserID: member.ID, ProjectRole: "member"})
	require.NoError(t, err)

	// 非成员指派 → 拒绝。
	outsider := seedUserWithMembership(t, client, tid, "outsider", "home", nil)
	_, err = svc.CreateTask(ctx, tid, p.ID, member.ID, &TaskCreateRequest{Name: "外部指派", AssigneeID: outsider.ID})
	require.ErrorIs(t, err, ErrAssigneeNotMember)

	// 根任务 + 两层子任务（共 3 层）；第 4 层拒绝。
	root, err := svc.CreateTask(ctx, tid, p.ID, member.ID, &TaskCreateRequest{Name: "根任务", AssigneeID: member.ID})
	require.NoError(t, err)
	level2, err := svc.CreateTask(ctx, tid, p.ID, member.ID, &TaskCreateRequest{Name: "二级", ParentTaskID: root.ID})
	require.NoError(t, err)
	level3, err := svc.CreateTask(ctx, tid, p.ID, member.ID, &TaskCreateRequest{Name: "三级", ParentTaskID: level2.ID})
	require.NoError(t, err)
	_, err = svc.CreateTask(ctx, tid, p.ID, member.ID, &TaskCreateRequest{Name: "四级", ParentTaskID: level3.ID})
	require.ErrorIs(t, err, ErrTaskDepthExceeded)

	// 状态机：todo → in_progress → blocked → in_progress → done。
	started, err := svc.TransitionTask(ctx, tid, p.ID, root.ID, &TaskStatusRequest{Action: TaskActionStart})
	require.NoError(t, err)
	assert.Equal(t, TaskStatusInProgress, started.Status)
	assert.False(t, started.ActualStart.IsZero())

	blocked, err := svc.TransitionTask(ctx, tid, p.ID, root.ID, &TaskStatusRequest{Action: TaskActionBlock})
	require.NoError(t, err)
	assert.Equal(t, TaskStatusBlocked, blocked.Status)

	_, err = svc.TransitionTask(ctx, tid, p.ID, root.ID, &TaskStatusRequest{Action: TaskActionStart})
	require.ErrorIs(t, err, ErrTaskInvalidTransition)

	unblocked, err := svc.TransitionTask(ctx, tid, p.ID, root.ID, &TaskStatusRequest{Action: TaskActionUnblock})
	require.NoError(t, err)
	assert.Equal(t, TaskStatusInProgress, unblocked.Status)

	done, err := svc.TransitionTask(ctx, tid, p.ID, root.ID, &TaskStatusRequest{Action: TaskActionComplete})
	require.NoError(t, err)
	assert.Equal(t, TaskStatusDone, done.Status)
	assert.Equal(t, 100, done.Progress)
	assert.False(t, done.ActualEnd.IsZero())

	// 删除门禁：有子任务拒绝。
	require.ErrorIs(t, svc.DeleteTask(ctx, tid, p.ID, root.ID), ErrTaskHasChildren)
	require.NoError(t, svc.DeleteTask(ctx, tid, p.ID, level3.ID))
	require.NoError(t, svc.DeleteTask(ctx, tid, p.ID, level2.ID))
	require.NoError(t, svc.DeleteTask(ctx, tid, p.ID, root.ID))

	// 我的任务：仅未终结任务；跨可见项目。
	open, err := svc.CreateTask(ctx, tid, p.ID, member.ID, &TaskCreateRequest{Name: "待办任务", AssigneeID: member.ID})
	require.NoError(t, err)
	mine, err := svc.MyTasks(ctx, tid, member.ID, false, "", 50)
	require.NoError(t, err)
	require.Len(t, mine, 1)
	assert.Equal(t, open.ID, mine[0].ID)
	assert.Equal(t, "任务项目", mine[0].ProjectName)
}

// TestHandlerTaskEndpoints 任务端点：创建/树视图/流转/删除/我的任务。
func TestHandlerTaskEndpoints(t *testing.T) {
	r, client, tid := newHandlerEnv(t)
	member := seedUserWithMembership(t, client, tid, "ht-task", "home", nil)
	adminUser, err := client.User.Query().
		Where(user.UsernameEQ(fmt.Sprintf("ht-admin-%d", tid))).
		Only(context.Background())
	require.NoError(t, err)

	_, env := doJSON(t, r, http.MethodPost, "/projects", map[string]any{"name": "任务接口项目"})
	var created ProjectResponse
	require.NoError(t, json.Unmarshal(env.Data, &created))

	_, env = doJSON(t, r, http.MethodPost, fmt.Sprintf("/projects/%d/members", created.ID), map[string]any{"userId": member.ID})
	require.Equal(t, common.SuccessCode, env.Code)
	// 管理员本人也加入项目，才能被指派（负责人须为项目活跃成员）。
	_, env = doJSON(t, r, http.MethodPost, fmt.Sprintf("/projects/%d/members", created.ID), map[string]any{"userId": adminUser.ID})
	require.Equal(t, common.SuccessCode, env.Code)

	w, env := doJSON(t, r, http.MethodPost, fmt.Sprintf("/projects/%d/tasks", created.ID), map[string]any{
		"name": "根任务", "assigneeId": adminUser.ID,
	})
	require.Equal(t, http.StatusOK, w.Code)
	var root ProjectTaskResponse
	require.NoError(t, json.Unmarshal(env.Data, &root))
	assert.Equal(t, TaskStatusTodo, root.Status)

	_, env = doJSON(t, r, http.MethodPost, fmt.Sprintf("/projects/%d/tasks", created.ID), map[string]any{
		"name": "子任务", "parentTaskId": root.ID,
	})
	require.Equal(t, common.SuccessCode, env.Code)

	// 树视图。
	_, env = doJSON(t, r, http.MethodGet, fmt.Sprintf("/projects/%d/tasks?view=tree", created.ID), nil)
	var tree []ProjectTaskResponse
	require.NoError(t, json.Unmarshal(env.Data, &tree))
	require.Len(t, tree, 1)
	require.Len(t, tree[0].Children, 1)

	// 流转。
	w, env = doJSON(t, r, http.MethodPost, fmt.Sprintf("/projects/%d/tasks/%d/status", created.ID, root.ID), map[string]any{"action": TaskActionStart})
	require.Equal(t, http.StatusOK, w.Code)
	var started ProjectTaskResponse
	require.NoError(t, json.Unmarshal(env.Data, &started))
	assert.Equal(t, TaskStatusInProgress, started.Status)

	// 非法流转 → 409。
	w, _ = doJSON(t, r, http.MethodPost, fmt.Sprintf("/projects/%d/tasks/%d/status", created.ID, root.ID), map[string]any{"action": TaskActionStart})
	assert.Equal(t, http.StatusConflict, w.Code)

	// 我的任务（admin 可见全量）。
	_, env = doJSON(t, r, http.MethodGet, "/projects/my-tasks", nil)
	var mine []MyTaskResponse
	require.NoError(t, json.Unmarshal(env.Data, &mine))
	require.Len(t, mine, 1)

	// 有子任务删除 → 409。
	w, _ = doJSON(t, r, http.MethodDelete, fmt.Sprintf("/projects/%d/tasks/%d", created.ID, root.ID), nil)
	assert.Equal(t, http.StatusConflict, w.Code)
}
