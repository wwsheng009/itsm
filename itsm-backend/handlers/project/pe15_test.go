package project

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"testing"
	"time"

	"itsm-backend/common"
	"itsm-backend/ent"
	"itsm-backend/ent/auditlog"
	"itsm-backend/ent/projectmember"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fakeClosureKnowledge 复盘 KB 草稿端口假实现。
type fakeClosureKnowledge struct {
	articleID int
	calls     int
	lastDraft RetroDraft
	err       error
}

func (f *fakeClosureKnowledge) CreateRetroDraft(_ context.Context, _, _ int, draft RetroDraft) (int, error) {
	f.calls++
	f.lastDraft = draft
	if f.err != nil {
		return 0, f.err
	}
	return f.articleID, nil
}

// fakeClosureSurvey CSAT 调研端口假实现。
type fakeClosureSurvey struct {
	surveyID int
	calls    int
	err      error
}

func (f *fakeClosureSurvey) CreateCSATSurvey(_ context.Context, _ int, _ CSATSurveyInput) (int, error) {
	f.calls++
	if f.err != nil {
		return 0, f.err
	}
	return f.surveyID, nil
}

// installClosurePorts 注入默认假端口（返回句柄供断言）。
func installClosurePorts(svc *Service) (*fakeClosureKnowledge, *fakeClosureSurvey) {
	knowledge := &fakeClosureKnowledge{articleID: 7001}
	survey := &fakeClosureSurvey{surveyID: 8001}
	svc.SetClosurePorts(knowledge, survey)
	return knowledge, survey
}

// fulfillClosureChecklist 走完结项清单人工/产物项（复盘草稿 + 资产登记 + 成员回收）。
func fulfillClosureChecklist(t *testing.T, svc *Service, tenantID, projectID, actorID int) {
	t.Helper()
	if svc.closureKnowledge == nil || svc.closureSurvey == nil {
		installClosurePorts(svc)
	}
	ctx := context.Background()
	_, err := svc.GenerateRetroDraft(ctx, tenantID, projectID, actorID, true)
	require.NoError(t, err)
	_, err = svc.ConfirmAssets(ctx, tenantID, projectID, actorID,
		&ClosureAssetsRequest{Action: "confirm", Note: "已登记资产/许可台账"}, true)
	require.NoError(t, err)
	_, err = svc.RecycleMembers(ctx, tenantID, projectID, actorID, true)
	require.NoError(t, err)
}

// TestClosureChecklistFlow 结项清单全链路：清单派生 → 动作 → 门禁 → 结项 + CSAT 自动触发。
func TestClosureChecklistFlow(t *testing.T) {
	svc, client, tid := newTestEnv(t)
	ctx := context.Background()
	knowledge, survey := installClosurePorts(svc)

	manager := seedUserWithMembership(t, client, tid, "c1pm", "home", nil)
	managerID := manager.ID
	p, err := svc.Create(ctx, &CreateRequest{TenantID: tid, Name: "结项清单项目", ManagerID: &managerID})
	require.NoError(t, err)
	_, err = svc.AddMember(ctx, tid, p.ID, manager.ID, &MemberAddRequest{UserID: manager.ID, ProjectRole: "pm"})
	require.NoError(t, err)
	worker := seedUserWithMembership(t, client, tid, "c1w", "home", nil)
	_, err = svc.AddMember(ctx, tid, p.ID, manager.ID, &MemberAddRequest{UserID: worker.ID, ProjectRole: "member"})
	require.NoError(t, err)
	stakeholder := seedUserWithMembership(t, client, tid, "c1s", "invite", nil)
	_, err = svc.AddMember(ctx, tid, p.ID, manager.ID, &MemberAddRequest{UserID: stakeholder.ID, ProjectRole: "stakeholder"})
	require.NoError(t, err)

	// 四道门链路。
	_, err = svc.SubmitGate(ctx, tid, p.ID, manager.ID, GateInitiation, "pm", true)
	require.NoError(t, err)
	task, err := svc.CreateTask(ctx, tid, p.ID, manager.ID, &TaskCreateRequest{Name: "交付实施"})
	require.NoError(t, err)
	due := time.Now().Add(72 * time.Hour)
	milestone, err := svc.CreateMilestone(ctx, tid, p.ID, manager.ID, &MilestoneCreateRequest{Name: "验收", DueDate: &due})
	require.NoError(t, err)
	_, err = svc.SubmitGate(ctx, tid, p.ID, manager.ID, GatePlan, "pm", true)
	require.NoError(t, err)
	_, err = svc.TransitionTask(ctx, tid, p.ID, task.ID, &TaskStatusRequest{Action: TaskActionStart})
	require.NoError(t, err)
	_, err = svc.TransitionTask(ctx, tid, p.ID, task.ID, &TaskStatusRequest{Action: TaskActionComplete})
	require.NoError(t, err)
	_, err = svc.TransitionMilestone(ctx, tid, p.ID, milestone.ID, &MilestoneStatusRequest{Action: MilestoneActionReach})
	require.NoError(t, err)
	_, err = svc.SubmitGate(ctx, tid, p.ID, manager.ID, GateAcceptance, "pm", true)
	require.NoError(t, err)
	_, err = svc.SubmitGate(ctx, tid, p.ID, manager.ID, GateClosure, "pm", true)
	require.NoError(t, err)
	_, err = svc.Transition(ctx, tid, p.ID, &StatusRequest{Action: ActionActivate})
	require.NoError(t, err)

	// 清单视图：自动项已完成；结项门提交时已自动生成复盘草稿；余下人工项 pending。
	view, err := svc.GetClosure(ctx, tid, p.ID, manager.ID, true)
	require.NoError(t, err)
	require.Len(t, view.Items, 7)
	assert.Equal(t, "pending", view.Status)
	statusByKey := map[string]string{}
	for _, item := range view.Items {
		statusByKey[item.Key] = item.Status
	}
	assert.Equal(t, ClosureItemDone, statusByKey[ClosureItemTasks])
	assert.Equal(t, ClosureItemDone, statusByKey[ClosureItemRecords])
	assert.Equal(t, ClosureItemDone, statusByKey[ClosureItemGate])
	assert.Equal(t, ClosureItemDone, statusByKey[ClosureItemRetro], "结项门提交应自动生成复盘草稿")
	assert.Equal(t, 7001, view.RetroKbArticleID)
	assert.Equal(t, 1, knowledge.calls)
	assert.Equal(t, ClosureItemPending, statusByKey[ClosureItemAssets])
	assert.Equal(t, ClosureItemPending, statusByKey[ClosureItemMembers])
	assert.Equal(t, ClosureItemPending, statusByKey[ClosureItemCSAT])

	_, err = svc.Transition(ctx, tid, p.ID, &StatusRequest{Action: ActionComplete})
	var blocked *ClosureBlockedError
	require.ErrorAs(t, err, &blocked)
	blockedKeys := map[string]bool{}
	for _, b := range blocked.Blockers {
		blockedKeys[b.Type] = true
	}
	assert.True(t, blockedKeys[ClosureItemAssets] && blockedKeys[ClosureItemMembers])
	assert.False(t, blockedKeys[ClosureItemRetro], "复盘草稿已自动生成，不应再阻塞")

	// 复盘草稿：手动生成幂等（自动生成后直接回显，不重复建文章）。
	v1, err := svc.GenerateRetroDraft(ctx, tid, p.ID, manager.ID, true)
	require.NoError(t, err)
	assert.Equal(t, 7001, v1.RetroKbArticleID)
	assert.NotNil(t, v1.RetroGeneratedAt)
	assert.Equal(t, retroDraftCategory, knowledge.lastDraft.Category)
	assert.Contains(t, knowledge.lastDraft.Content, "## 复盘要点")
	assert.Contains(t, knowledge.lastDraft.Content, p.Code)
	v2, err := svc.GenerateRetroDraft(ctx, tid, p.ID, manager.ID, true)
	require.NoError(t, err)
	assert.Equal(t, 7001, v2.RetroKbArticleID)
	assert.Equal(t, 1, knowledge.calls, "已生成后应幂等回显，不重复创建")

	// 资产登记：waive 无原因 → 400 语义；豁免后 confirmed 状态被清除。
	_, err = svc.ConfirmAssets(ctx, tid, p.ID, manager.ID, &ClosureAssetsRequest{Action: "waive"}, true)
	require.ErrorIs(t, err, ErrValidation)
	_, err = svc.ConfirmAssets(ctx, tid, p.ID, manager.ID, &ClosureAssetsRequest{Action: "bogus"}, true)
	require.ErrorIs(t, err, ErrValidation)
	v3, err := svc.ConfirmAssets(ctx, tid, p.ID, manager.ID,
		&ClosureAssetsRequest{Action: "confirm", Note: "登记至资产台账 A-12"}, true)
	require.NoError(t, err)
	assert.NotNil(t, v3.AssetsConfirmedAt)
	assert.False(t, v3.AssetsWaived)
	v4, err := svc.ConfirmAssets(ctx, tid, p.ID, manager.ID,
		&ClosureAssetsRequest{Action: "waive", Reason: "本项目无新增资产/许可"}, true)
	require.NoError(t, err)
	assert.True(t, v4.AssetsWaived)
	assert.Nil(t, v4.AssetsConfirmedAt, "豁免应清除确认留痕")
	assert.Equal(t, "本项目无新增资产/许可", v4.AssetsWaiveReason)

	// 成员回收：仅 member 角色被回收；pm/stakeholder 保留；重复执行计数归零。
	v5, err := svc.RecycleMembers(ctx, tid, p.ID, manager.ID, true)
	require.NoError(t, err)
	assert.Equal(t, 1, v5.MembersRecycledCount)
	assert.NotNil(t, v5.MembersRecycledAt)
	assertMemberStatus(t, client, tid, p.ID, worker.ID, "removed")
	assertMemberStatus(t, client, tid, p.ID, manager.ID, "active")
	assertMemberStatus(t, client, tid, p.ID, stakeholder.ID, "active")
	v6, err := svc.RecycleMembers(ctx, tid, p.ID, manager.ID, true)
	require.NoError(t, err)
	assert.Equal(t, 0, v6.MembersRecycledCount, "重复回收不重复计数")

	// 清单齐备 → 结项成功 + CSAT 自动触发（尽力而为副作用）。
	completed, err := svc.Transition(ctx, tid, p.ID, &StatusRequest{Action: ActionComplete})
	require.NoError(t, err)
	assert.Equal(t, StatusCompleted, completed.Status)
	assert.False(t, completed.ClosedAt.IsZero())
	assert.Equal(t, 1, survey.calls, "结项应自动触发 CSAT")
	require.NotNil(t, survey)
	final, err := svc.GetClosure(ctx, tid, p.ID, manager.ID, true)
	require.NoError(t, err)
	assert.Equal(t, StatusCompleted, final.Status)
	assert.Equal(t, 8001, final.CSATSurveyID)
	assert.NotNil(t, final.CSATTriggeredAt)
	assert.Equal(t, ClosureItemDone, itemStatus(final, ClosureItemCSAT))
	// CSAT 幂等：再次触发不重复建调研。
	again, err := svc.TriggerCSAT(ctx, tid, p.ID, manager.ID, true)
	require.NoError(t, err)
	assert.Equal(t, 8001, again.CSATSurveyID)
	assert.Equal(t, 1, survey.calls)
}

// TestClosureIsolationAndGuards 隔离（跨租户/不可见）+ 终态拒绝 + 端口未配置 503 语义。
func TestClosureIsolationAndGuards(t *testing.T) {
	svc, client, tid := newTestEnv(t)
	ctx := context.Background()

	manager := seedUserWithMembership(t, client, tid, "c2pm", "home", nil)
	managerID := manager.ID
	p, err := svc.Create(ctx, &CreateRequest{TenantID: tid, Name: "隔离清单项目", ManagerID: &managerID})
	require.NoError(t, err)
	_, err = svc.AddMember(ctx, tid, p.ID, manager.ID, &MemberAddRequest{UserID: manager.ID, ProjectRole: "pm"})
	require.NoError(t, err)

	// 不可见用户（非管理角色 + 非成员）→ 404。
	outsider := seedUserWithMembership(t, client, tid, "c2out", "home", nil)
	_, err = svc.GetClosure(ctx, tid, p.ID, outsider.ID, false)
	require.ErrorIs(t, err, ErrNotFound)
	_, err = svc.GenerateRetroDraft(ctx, tid, p.ID, outsider.ID, false)
	require.ErrorIs(t, err, ErrNotFound)

	// 跨租户 → 404（租户过滤；同名项目在另一租户不可见）。
	otherTenant, err := client.Tenant.Create().SetName("隔离租户B").SetCode("C2B").SetStatus("active").Save(ctx)
	require.NoError(t, err)
	_, err = svc.GetClosure(ctx, otherTenant.ID, p.ID, manager.ID, true)
	require.ErrorIs(t, err, ErrNotFound)

	// 端口未注入 → 503 语义（GetClosure 不受影响）。
	_, err = svc.GenerateRetroDraft(ctx, tid, p.ID, manager.ID, true)
	require.ErrorIs(t, err, ErrClosurePortUnavailable)
	_, err = svc.TriggerCSAT(ctx, tid, p.ID, manager.ID, true)
	require.ErrorIs(t, err, ErrClosurePortUnavailable)

	// 终态（cancel）→ 动作 400。
	_, err = svc.Transition(ctx, tid, p.ID, &StatusRequest{Action: ActionCancel, Reason: "范围取消"})
	require.NoError(t, err)
	installClosurePorts(svc)
	_, err = svc.GenerateRetroDraft(ctx, tid, p.ID, manager.ID, true)
	require.ErrorIs(t, err, ErrValidation)
	_, err = svc.RecycleMembers(ctx, tid, p.ID, manager.ID, true)
	require.ErrorIs(t, err, ErrValidation)
	_, err = svc.ConfirmAssets(ctx, tid, p.ID, manager.ID, &ClosureAssetsRequest{Action: "confirm"}, true)
	require.ErrorIs(t, err, ErrValidation)
	_, err = svc.TriggerCSAT(ctx, tid, p.ID, manager.ID, true)
	require.ErrorIs(t, err, ErrValidation)

	// 取消后清单仍可读（终态只读留痕）。
	view, err := svc.GetClosure(ctx, tid, p.ID, manager.ID, true)
	require.NoError(t, err)
	assert.Equal(t, StatusCancelled, view.Status)
}

// TestClosurePortFailuresDoNotBlockCompletion 端口故障：retro/csat 报错不阻塞结项；结项 CSAT 失败可重试。
func TestClosurePortFailuresDoNotBlockCompletion(t *testing.T) {
	svc, client, tid := newTestEnv(t)
	ctx := context.Background()
	knowledge, survey := installClosurePorts(svc)

	manager := seedUserWithMembership(t, client, tid, "c3pm", "home", nil)
	managerID := manager.ID
	p, err := svc.Create(ctx, &CreateRequest{TenantID: tid, Name: "端口故障项目", ManagerID: &managerID})
	require.NoError(t, err)
	_, err = svc.AddMember(ctx, tid, p.ID, manager.ID, &MemberAddRequest{UserID: manager.ID, ProjectRole: "pm"})
	require.NoError(t, err)
	_, err = svc.SubmitGate(ctx, tid, p.ID, manager.ID, GateInitiation, "pm", true)
	require.NoError(t, err)
	_, err = svc.Transition(ctx, tid, p.ID, &StatusRequest{Action: ActionActivate})
	require.NoError(t, err)

	// 复盘草稿创建失败 → 动作报错，清单项保持 pending。
	knowledge.err = fmt.Errorf("knowledge down")
	_, err = svc.GenerateRetroDraft(ctx, tid, p.ID, manager.ID, true)
	require.Error(t, err)
	assert.Equal(t, 1, knowledge.calls, "失败调用应已发起（端口被调用一次）")
	view, err := svc.GetClosure(ctx, tid, p.ID, manager.ID, true)
	require.NoError(t, err)
	assert.Equal(t, ClosureItemPending, itemStatus(view, ClosureItemRetro))
	knowledge.err = nil

	// plan/acceptance/closure 门链路（plan 需任务+里程碑；acceptance 需全部完成/达成）。
	task, err := svc.CreateTask(ctx, tid, p.ID, manager.ID, &TaskCreateRequest{Name: "实施"})
	require.NoError(t, err)
	_, err = svc.TransitionTask(ctx, tid, p.ID, task.ID, &TaskStatusRequest{Action: TaskActionStart})
	require.NoError(t, err)
	_, err = svc.TransitionTask(ctx, tid, p.ID, task.ID, &TaskStatusRequest{Action: TaskActionComplete})
	require.NoError(t, err)
	due := time.Now().Add(24 * time.Hour)
	milestone, err := svc.CreateMilestone(ctx, tid, p.ID, manager.ID, &MilestoneCreateRequest{Name: "M1", DueDate: &due})
	require.NoError(t, err)
	_, err = svc.TransitionMilestone(ctx, tid, p.ID, milestone.ID, &MilestoneStatusRequest{Action: MilestoneActionReach})
	require.NoError(t, err)
	_, err = svc.SubmitGate(ctx, tid, p.ID, manager.ID, GatePlan, "pm", true)
	require.NoError(t, err)
	_, err = svc.SubmitGate(ctx, tid, p.ID, manager.ID, GateAcceptance, "pm", true)
	require.NoError(t, err)
	_, err = svc.SubmitGate(ctx, tid, p.ID, manager.ID, GateClosure, "pm", true)
	require.NoError(t, err)

	// 结项门提交已自动补生成复盘草稿（首次失败后重试成功）；再完成资产登记与成员回收。
	view, err = svc.GetClosure(ctx, tid, p.ID, manager.ID, true)
	require.NoError(t, err)
	assert.Equal(t, ClosureItemDone, itemStatus(view, ClosureItemRetro), "结项门提交应自动生成复盘草稿")
	assert.Equal(t, 2, knowledge.calls, "自动生成重试一次（首次手动失败 + 门提交自动成功）")
	_, err = svc.GenerateRetroDraft(ctx, tid, p.ID, manager.ID, true)
	require.NoError(t, err)
	assert.Equal(t, 2, knowledge.calls, "已生成后手动调用应幂等回显")
	_, err = svc.ConfirmAssets(ctx, tid, p.ID, manager.ID, &ClosureAssetsRequest{Action: "confirm"}, true)
	require.NoError(t, err)
	_, err = svc.RecycleMembers(ctx, tid, p.ID, manager.ID, true)
	require.NoError(t, err)
	survey.err = fmt.Errorf("survey down")
	completed, err := svc.Transition(ctx, tid, p.ID, &StatusRequest{Action: ActionComplete})
	require.NoError(t, err, "CSAT 触发失败不得阻塞结项")
	assert.Equal(t, StatusCompleted, completed.Status)
	view, err = svc.GetClosure(ctx, tid, p.ID, manager.ID, true)
	require.NoError(t, err)
	assert.Equal(t, 0, view.CSATSurveyID)
	assert.Equal(t, ClosureItemPending, itemStatus(view, ClosureItemCSAT))

	// 故障恢复后手动重试成功（首次自动触发失败 1 次 + 手动重试 1 次）。
	survey.err = nil
	retried, err := svc.TriggerCSAT(ctx, tid, p.ID, manager.ID, true)
	require.NoError(t, err)
	assert.Equal(t, 8001, retried.CSATSurveyID)
	assert.Equal(t, 2, survey.calls)
}

// TestHandlerClosureEndpoints 端点级：清单信封/别名、动作留痕、409 阻塞明细、错误映射。
func TestHandlerClosureEndpoints(t *testing.T) {
	r, client, _ := newHandlerEnv(t)
	ctx := context.Background()
	admin, err := client.User.Query().First(ctx)
	require.NoError(t, err)

	_, env := doJSON(t, r, http.MethodPost, "/projects", map[string]any{"name": "接口结项项目", "managerId": admin.ID})
	var created ProjectResponse
	require.NoError(t, json.Unmarshal(env.Data, &created))

	w, env := doJSON(t, r, http.MethodGet, fmt.Sprintf("/projects/%d/closure", created.ID), nil)
	require.Equal(t, http.StatusOK, w.Code)
	var view ProjectClosureResponse
	require.NoError(t, json.Unmarshal(env.Data, &view))
	require.Len(t, view.Items, 7)
	assert.Equal(t, "pending", view.Status)
	assert.NotEmpty(t, view.Blockers)

	// 复盘草稿：生成 + 审计留痕；重复调用幂等。
	w, env = doJSON(t, r, http.MethodPost, fmt.Sprintf("/projects/%d/closure/retro", created.ID), nil)
	require.Equal(t, http.StatusOK, w.Code)
	require.NoError(t, json.Unmarshal(env.Data, &view))
	assert.Equal(t, 7001, view.RetroKbArticleID)
	w, env = doJSON(t, r, http.MethodPost, fmt.Sprintf("/projects/%d/closure/retro", created.ID), nil)
	require.Equal(t, http.StatusOK, w.Code)
	require.NoError(t, json.Unmarshal(env.Data, &view))
	assert.Equal(t, 7001, view.RetroKbArticleID)
	audits, err := client.AuditLog.Query().
		Where(auditlog.ResourceEQ("project"), auditlog.ActionEQ("closure_retro_generate")).
		Count(ctx)
	require.NoError(t, err)
	assert.GreaterOrEqual(t, audits, 1, "应写 auditlog(resource=project, action=closure_retro_generate)")

	// 资产登记：waive 缺原因 → 400；confirm → 200。
	w, _ = doJSON(t, r, http.MethodPost, fmt.Sprintf("/projects/%d/closure/assets", created.ID), map[string]any{"action": "waive"})
	assert.Equal(t, http.StatusBadRequest, w.Code)
	w, env = doJSON(t, r, http.MethodPost, fmt.Sprintf("/projects/%d/closure/assets", created.ID), map[string]any{"action": "confirm", "note": "台账已登记"})
	require.Equal(t, http.StatusOK, w.Code)
	require.NoError(t, json.Unmarshal(env.Data, &view))
	assert.NotNil(t, view.AssetsConfirmedAt)

	// 成员回收 + CSAT 触发（幂等）。
	w, env = doJSON(t, r, http.MethodPost, fmt.Sprintf("/projects/%d/closure/members/recycle", created.ID), nil)
	require.Equal(t, http.StatusOK, w.Code)
	require.NoError(t, json.Unmarshal(env.Data, &view))
	assert.NotNil(t, view.MembersRecycledAt)
	w, env = doJSON(t, r, http.MethodPost, fmt.Sprintf("/projects/%d/closure/csat", created.ID), nil)
	require.Equal(t, http.StatusOK, w.Code)
	require.NoError(t, json.Unmarshal(env.Data, &view))
	assert.Equal(t, 8001, view.CSATSurveyID)
	w, env = doJSON(t, r, http.MethodPost, fmt.Sprintf("/projects/%d/closure/csat", created.ID), nil)
	require.Equal(t, http.StatusOK, w.Code)
	require.NoError(t, json.Unmarshal(env.Data, &view))
	assert.Equal(t, 8001, view.CSATSurveyID)

	// 不存在项目 → 404。
	w, _ = doJSON(t, r, http.MethodGet, "/projects/999999/closure", nil)
	assert.Equal(t, http.StatusNotFound, w.Code)

	// 项目已完成态：清单可读且 status=completed（直接置库避免重跑全链路）。
	require.NoError(t, client.Project.UpdateOneID(created.ID).SetStatus(StatusCompleted).Exec(ctx))
	w, env = doJSON(t, r, http.MethodGet, fmt.Sprintf("/projects/%d/closure", created.ID), nil)
	require.Equal(t, http.StatusOK, w.Code)
	require.NoError(t, json.Unmarshal(env.Data, &view))
	assert.Equal(t, StatusCompleted, view.Status)
	assert.Equal(t, common.SuccessCode, env.Code)
}

// itemStatus 取清单项状态（测试辅助）。
func itemStatus(view *ProjectClosureResponse, key string) string {
	for _, item := range view.Items {
		if item.Key == key {
			return item.Status
		}
	}
	return ""
}

// assertMemberStatus 断言成员行状态。
func assertMemberStatus(t *testing.T, client *ent.Client, tenantID, projectID, userID int, want string) {
	t.Helper()
	row, err := client.ProjectMember.Query().
		Where(
			projectmember.TenantIDEQ(tenantID),
			projectmember.ProjectIDEQ(projectID),
			projectmember.UserIDEQ(userID),
		).
		Only(context.Background())
	require.NoError(t, err)
	assert.Equal(t, want, row.Status)
}