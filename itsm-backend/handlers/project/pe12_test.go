package project

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"testing"
	"time"

	"itsm-backend/common"
	"itsm-backend/ent/auditlog"
	"itsm-backend/ent/projectgate"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fakeGateApproval 阶段门审批端口假实现（模拟 BPMN 触发/裁决结果）。
type fakeGateApproval struct {
	start        GateApprovalStart
	startErr     error
	decideResult GateApprovalDecision
	decideErr    error
	submitCalls  int
	decideCalls  int
	lastDecision string
	lastComment  string
}

func (f *fakeGateApproval) SubmitGate(_ context.Context, _, _ int, _ string, _ map[string]any, _ string) (GateApprovalStart, error) {
	f.submitCalls++
	return f.start, f.startErr
}

func (f *fakeGateApproval) DecideGate(_ context.Context, _, _, _ int, decision, comment string) (GateApprovalDecision, error) {
	f.decideCalls++
	f.lastDecision = decision
	f.lastComment = comment
	return f.decideResult, f.decideErr
}

// TestGateRulesAndSequence 门禁规则 + 顺序前置（PE-12）。
func TestGateRulesAndSequence(t *testing.T) {
	svc, client, tid := newTestEnv(t)
	ctx := context.Background()

	manager := seedUserWithMembership(t, client, tid, "g1", "home", nil)
	p, err := svc.Create(ctx, &CreateRequest{TenantID: tid, Name: "阶段门项目"})
	require.NoError(t, err)

	// 立项门：未任命经理 → manager_missing（顺序首门无前置）。
	_, err = svc.SubmitGate(ctx, tid, p.ID, manager.ID, GateInitiation, "pm", true)
	var blocked *GateBlockedError
	require.ErrorAs(t, err, &blocked)
	assert.Equal(t, "manager_missing", blocked.Blockers[0].Type)
	// 阻塞明细已落库（可追溯）。
	record, err := svc.repo.GetGate(ctx, tid, p.ID, GateInitiation)
	require.NoError(t, err)
	require.NotEmpty(t, record.Blockers)
	assert.Equal(t, GateStatusPending, record.Status)

	managerID := manager.ID
	updated, err := svc.Update(ctx, tid, p.ID, &UpdateRequest{ManagerID: &managerID})
	require.NoError(t, err)
	require.Equal(t, manager.ID, updated.ManagerID)

	// 顺序前置：plan 门在 initiation 未通过时被拒（gate_not_passed 指向 initiation）。
	_, err = svc.SubmitGate(ctx, tid, p.ID, manager.ID, GatePlan, "pm", true)
	require.ErrorAs(t, err, &blocked)
	assert.Equal(t, "gate_not_passed", blocked.Blockers[0].Type)
	assert.Equal(t, GateStatusPending, blocked.Blockers[0].Status)

	// 立项门通过（无成员 → member_missing；登记后自动通过）。
	_, err = svc.SubmitGate(ctx, tid, p.ID, manager.ID, GateInitiation, "pm", true)
	require.ErrorAs(t, err, &blocked)
	assert.Equal(t, "member_missing", blocked.Blockers[0].Type)
	_, err = svc.AddMember(ctx, tid, p.ID, manager.ID, &MemberAddRequest{UserID: manager.ID, ProjectRole: "pm"})
	require.NoError(t, err)
	initiation, err := svc.SubmitGate(ctx, tid, p.ID, manager.ID, GateInitiation, "pm", true)
	require.NoError(t, err)
	assert.Equal(t, GateStatusPassed, initiation.Status)
	// 重复提交已通过的门 → 409 语义。
	_, err = svc.SubmitGate(ctx, tid, p.ID, manager.ID, GateInitiation, "pm", true)
	require.ErrorIs(t, err, ErrGateAlreadyPassed)

	// plan 门：任务缺 → task_missing；里程碑缺 → milestone_missing。
	_, err = svc.SubmitGate(ctx, tid, p.ID, manager.ID, GatePlan, "pm", true)
	require.ErrorAs(t, err, &blocked)
	assert.Equal(t, "task_missing", blocked.Blockers[0].Type)
	task, err := svc.CreateTask(ctx, tid, p.ID, manager.ID, &TaskCreateRequest{Name: "实施"})
	require.NoError(t, err)
	_, err = svc.SubmitGate(ctx, tid, p.ID, manager.ID, GatePlan, "pm", true)
	require.ErrorAs(t, err, &blocked)
	assert.Equal(t, "milestone_missing", blocked.Blockers[0].Type)

	due := time.Now().Add(48 * time.Hour)
	milestone, err := svc.CreateMilestone(ctx, tid, p.ID, manager.ID, &MilestoneCreateRequest{Name: "上线", DueDate: &due})
	require.NoError(t, err)
	plan, err := svc.SubmitGate(ctx, tid, p.ID, manager.ID, GatePlan, "pm", true)
	require.NoError(t, err)
	assert.Equal(t, GateStatusPassed, plan.Status)

	// plan 通过 → 自动冻结基线 v1。
	base, err := svc.repo.LatestBaseline(ctx, tid, p.ID)
	require.NoError(t, err)
	assert.Equal(t, 1, base.Version)
	// 重复提交 plan（已通过）→ 不再重复冻结。
	_, err = svc.SubmitGate(ctx, tid, p.ID, manager.ID, GatePlan, "pm", true)
	require.ErrorIs(t, err, ErrGateAlreadyPassed)

	// acceptance 门：任务未完成 + 里程碑未达成。
	_, err = svc.SubmitGate(ctx, tid, p.ID, manager.ID, GateAcceptance, "pm", true)
	require.ErrorAs(t, err, &blocked)
	assert.Equal(t, "task_open", blocked.Blockers[0].Type)
	_, err = svc.TransitionTask(ctx, tid, p.ID, task.ID, &TaskStatusRequest{Action: TaskActionStart})
	require.NoError(t, err)
	_, err = svc.TransitionTask(ctx, tid, p.ID, task.ID, &TaskStatusRequest{Action: TaskActionComplete})
	require.NoError(t, err)
	_, err = svc.SubmitGate(ctx, tid, p.ID, manager.ID, GateAcceptance, "pm", true)
	require.ErrorAs(t, err, &blocked)
	assert.Equal(t, "milestone_open", blocked.Blockers[0].Type)
	_, err = svc.TransitionMilestone(ctx, tid, p.ID, milestone.ID, &MilestoneStatusRequest{Action: MilestoneActionReach})
	require.NoError(t, err)
	acceptance, err := svc.SubmitGate(ctx, tid, p.ID, manager.ID, GateAcceptance, "pm", true)
	require.NoError(t, err)
	assert.Equal(t, GateStatusPassed, acceptance.Status)

	// closure 门：关联未终态单据 → record_open；单据关闭后通过。
	requester := seedUserWithMembership(t, client, tid, "req", "home", nil)
	ticket, err := client.Ticket.Create().
		SetTenantID(tid).
		SetTitle("结项阻塞工单").
		SetTicketNumber(fmt.Sprintf("TK-PE12-%d", p.ID)).
		SetRequesterID(requester.ID).
		SetStatus("open").
		SetProjectID(p.ID).
		Save(ctx)
	require.NoError(t, err)
	_, err = svc.SubmitGate(ctx, tid, p.ID, manager.ID, GateClosure, "pm", true)
	require.ErrorAs(t, err, &blocked)
	assert.Equal(t, "record_open", blocked.Blockers[0].Type)
	_, err = client.Ticket.UpdateOneID(ticket.ID).SetStatus("closed").Save(ctx)
	require.NoError(t, err)
	closure, err := svc.SubmitGate(ctx, tid, p.ID, manager.ID, GateClosure, "pm", true)
	require.NoError(t, err)
	assert.Equal(t, GateStatusPassed, closure.Status)

	// 全链路放行：activate + complete。
	activated, err := svc.Transition(ctx, tid, p.ID, &StatusRequest{Action: ActionActivate})
	require.NoError(t, err)
	assert.Equal(t, StatusActive, activated.Status)
	// PE-15 结项清单：复盘草稿 + 资产登记 + 成员回收齐备（门禁在清单齐备后放行）。
	fulfillClosureChecklist(t, svc, tid, p.ID, manager.ID)
	completed, err := svc.Transition(ctx, tid, p.ID, &StatusRequest{Action: ActionComplete})
	require.NoError(t, err)
	assert.Equal(t, StatusCompleted, completed.Status)
}

// TestGateApprovalFlow 流程绑定审批：in_review → 拒绝（原因必填）→ 重提交 → 多级审批 → 通过。
func TestGateApprovalFlow(t *testing.T) {
	svc, client, tid := newTestEnv(t)
	ctx := context.Background()

	manager := seedUserWithMembership(t, client, tid, "g2", "home", nil)
	managerID := manager.ID
	p, err := svc.Create(ctx, &CreateRequest{TenantID: tid, Name: "审批流项目", ManagerID: &managerID})
	require.NoError(t, err)
	_, err = svc.AddMember(ctx, tid, p.ID, manager.ID, &MemberAddRequest{UserID: manager.ID, ProjectRole: "pm"})
	require.NoError(t, err)

	approval := &fakeGateApproval{start: GateApprovalStart{ProcessInstanceID: 4242, ApprovalChainID: "chain-gate"}}
	svc.SetGateApproval(approval)

	// 提交 → 触发流程 → in_review（不再自动通过）。
	gate, err := svc.SubmitGate(ctx, tid, p.ID, manager.ID, GateInitiation, "pm", true)
	require.NoError(t, err)
	assert.Equal(t, GateStatusInReview, gate.Status)
	assert.Equal(t, 4242, gate.ProcessInstanceID)
	assert.Equal(t, "chain-gate", gate.ApprovalChainID)
	assert.Equal(t, 1, approval.submitCalls)

	// 审批中：激活仍被门禁拒绝（状态 in_review）。
	_, err = svc.Transition(ctx, tid, p.ID, &StatusRequest{Action: ActionActivate})
	var blocked *GateBlockedError
	require.ErrorAs(t, err, &blocked)
	assert.Equal(t, GateStatusInReview, blocked.Blockers[0].Status)

	// 审批中：重复提交 409。
	_, err = svc.SubmitGate(ctx, tid, p.ID, manager.ID, GateInitiation, "pm", true)
	require.ErrorIs(t, err, ErrGateInReview)

	// 拒绝未填原因 → 400；填原因 → rejected（可在修复后重新提交）。
	_, err = svc.DecideGate(ctx, tid, p.ID, manager.ID, GateInitiation, &GateDecisionRequest{Decision: "reject"}, true)
	require.ErrorIs(t, err, ErrReasonRequired)
	rejected, err := svc.DecideGate(ctx, tid, p.ID, manager.ID, GateInitiation,
		&GateDecisionRequest{Decision: "reject", Comment: "预算未确认"}, true)
	require.NoError(t, err)
	assert.Equal(t, GateStatusRejected, rejected.Status)
	assert.Equal(t, "预算未确认", rejected.DecisionComment)

	// 已裁决（非 in_review）→ 再次裁决 409。
	_, err = svc.DecideGate(ctx, tid, p.ID, manager.ID, GateInitiation,
		&GateDecisionRequest{Decision: "approve"}, true)
	require.ErrorIs(t, err, ErrGateNoApproval)

	// 重新提交 → 再次 in_review；多级审批（未走完）保持 in_review。
	gate, err = svc.SubmitGate(ctx, tid, p.ID, manager.ID, GateInitiation, "pm", true)
	require.NoError(t, err)
	assert.Equal(t, GateStatusInReview, gate.Status)
	approval.decideResult = GateApprovalDecision{Handled: true, FlowFinished: false}
	again, err := svc.DecideGate(ctx, tid, p.ID, manager.ID, GateInitiation,
		&GateDecisionRequest{Decision: "approve", Comment: "一级通过"}, true)
	require.NoError(t, err)
	assert.Equal(t, GateStatusInReview, again.Status, "流程未走完时门保持 in_review")

	// 流程走完 → passed。
	approval.decideResult = GateApprovalDecision{Handled: true, FlowFinished: true}
	passed, err := svc.DecideGate(ctx, tid, p.ID, manager.ID, GateInitiation,
		&GateDecisionRequest{Decision: "approve", Comment: "终审通过"}, true)
	require.NoError(t, err)
	assert.Equal(t, GateStatusPassed, passed.Status)
	assert.Equal(t, "approve", approval.lastDecision)

	// 通过后可激活。
	activated, err := svc.Transition(ctx, tid, p.ID, &StatusRequest{Action: ActionActivate})
	require.NoError(t, err)
	assert.Equal(t, StatusActive, activated.Status)

	// 审批端口故障 → 门保持原状且错误可识别（approve 分支）。
	other, err := svc.Create(ctx, &CreateRequest{TenantID: tid, Name: "故障项目", ManagerID: &managerID})
	require.NoError(t, err)
	_, err = svc.AddMember(ctx, tid, other.ID, manager.ID, &MemberAddRequest{UserID: manager.ID, ProjectRole: "pm"})
	require.NoError(t, err)
	approval.startErr = fmt.Errorf("bpmn down")
	_, err = svc.SubmitGate(ctx, tid, other.ID, manager.ID, GateInitiation, "pm", true)
	require.ErrorIs(t, err, ErrGateApprovalFailed)
	stored, err := svc.repo.GetGate(ctx, tid, other.ID, GateInitiation)
	require.NoError(t, err)
	assert.Equal(t, GateStatusPending, stored.Status, "触发失败不得留下半成品状态")
}

// TestGateIsolationAndTerminal 跨租户/不可见 404 + 终态项目 400 + 非法门 400。
func TestGateIsolationAndTerminal(t *testing.T) {
	svc, client, tid := newTestEnv(t)
	ctx := context.Background()

	manager := seedUserWithMembership(t, client, tid, "g3", "home", nil)
	managerID := manager.ID
	p, err := svc.Create(ctx, &CreateRequest{TenantID: tid, Name: "隔离项目", ManagerID: &managerID})
	require.NoError(t, err)
	_, err = svc.AddMember(ctx, tid, p.ID, manager.ID, &MemberAddRequest{UserID: manager.ID, ProjectRole: "pm"})
	require.NoError(t, err)

	// 先留一条门记录（自动通过）用于列表断言。
	_, err = svc.SubmitGate(ctx, tid, p.ID, manager.ID, GateInitiation, "pm", true)
	require.NoError(t, err)

	// 非法门 → 400。
	_, err = svc.SubmitGate(ctx, tid, p.ID, manager.ID, "bogus", "pm", true)
	require.ErrorIs(t, err, ErrValidation)

	// 不存在项目 → 404。
	_, err = svc.SubmitGate(ctx, tid, 999999, manager.ID, GateInitiation, "pm", true)
	require.ErrorIs(t, err, ErrNotFound)

	// 不可见用户（非管理角色 + 非成员）→ 404。
	outsider := seedUserWithMembership(t, client, tid, "g3c", "home", nil)
	_, err = svc.SubmitGate(ctx, tid, p.ID, outsider.ID, GateInitiation, "outsider", false)
	require.ErrorIs(t, err, ErrNotFound)

	// 终态项目（cancel）→ 400。
	_, err = svc.Transition(ctx, tid, p.ID, &StatusRequest{Action: ActionCancel, Reason: "范围取消"})
	require.NoError(t, err)
	_, err = svc.SubmitGate(ctx, tid, p.ID, manager.ID, GateInitiation, "pm", true)
	require.ErrorIs(t, err, ErrValidation)

	// 列表（可见性 + 顺序）。
	gates, err := svc.ListGates(ctx, tid, p.ID, manager.ID, true)
	require.NoError(t, err)
	require.Len(t, gates, 1) // 仅提交过一次（pending 记录）
	assert.Equal(t, GateInitiation, gates[0].Gate)
	assert.Equal(t, GateStatusPassed, gates[0].Status)
}

// TestHandlerGateEndpoints 端点级：信封/别名、409 + blockers、未知门 400、审计留痕。
func TestHandlerGateEndpoints(t *testing.T) {
	r, client, tid := newHandlerEnv(t)

	ctx := context.Background()
	admin, err := client.User.Query().First(ctx)
	require.NoError(t, err)

	_, env := doJSON(t, r, http.MethodPost, "/projects", map[string]any{"name": "接口门项目", "managerId": admin.ID})
	var created ProjectResponse
	require.NoError(t, json.Unmarshal(env.Data, &created))

	// 未登记成员 → 409 + GATE 语义（gate + blockers 明细）。
	w, env := doJSON(t, r, http.MethodPost, fmt.Sprintf("/projects/%d/gates/initiation/submit", created.ID), nil)
	require.Equal(t, http.StatusConflict, w.Code)
	assert.Equal(t, common.ConflictCode, env.Code)
	var blocked struct {
		Gate     string        `json:"gate"`
		Blockers []GateBlocker `json:"blockers"`
	}
	require.NoError(t, json.Unmarshal(env.Data, &blocked))
	assert.Equal(t, GateInitiation, blocked.Gate)
	require.NotEmpty(t, blocked.Blockers)

	// 未知门 → 400。
	w, _ = doJSON(t, r, http.MethodPost, fmt.Sprintf("/projects/%d/gates/bogus/submit", created.ID), nil)
	assert.Equal(t, http.StatusBadRequest, w.Code)

	// 登记成员 → 通过（自动）→ 审计 gate_pass 落库。
	w, _ = doJSON(t, r, http.MethodPost, fmt.Sprintf("/projects/%d/members", created.ID), map[string]any{"userId": admin.ID, "projectRole": "pm"})
	require.Equal(t, http.StatusOK, w.Code)
	w, env = doJSON(t, r, http.MethodPost, fmt.Sprintf("/projects/%d/gates/initiation/submit", created.ID), nil)
	require.Equal(t, http.StatusOK, w.Code)
	var gate ProjectGateResponse
	require.NoError(t, json.Unmarshal(env.Data, &gate))
	assert.Equal(t, GateStatusPassed, gate.Status)
	assert.Equal(t, "auto_approved_no_flow", gate.DecisionComment)

	audits, err := client.AuditLog.Query().
		Where(auditlog.ResourceEQ("project"), auditlog.ActionEQ("gate_pass")).
		Count(ctx)
	require.NoError(t, err)
	assert.GreaterOrEqual(t, audits, 1, "门通过应写 auditlog(resource=project, action=gate_pass)")

	// 列表：items + 顺序 + 领域别名。
	w, env = doJSON(t, r, http.MethodGet, fmt.Sprintf("/projects/%d/gates", created.ID), nil)
	require.Equal(t, http.StatusOK, w.Code)
	var list struct {
		Items        []ProjectGateResponse `json:"items"`
		Gates        []string              `json:"gates"`
		ProjectGates []ProjectGateResponse `json:"projectGates"`
	}
	require.NoError(t, json.Unmarshal(env.Data, &list))
	require.Len(t, list.Items, 1)
	assert.Equal(t, []string{GateInitiation, GatePlan, GateAcceptance, GateClosure}, list.Gates)
	assert.Len(t, list.ProjectGates, 1, "领域别名 projectGates 应存在")
	// 租户归属：直接读表校验 tenant_id（跨租户隔离前提）。
	stored, err := client.ProjectGate.Query().
		Where(projectgate.IDEQ(gate.ID)).
		Only(ctx)
	require.NoError(t, err)
	assert.Equal(t, tid, stored.TenantID)

	// 裁决端点：非 in_review → 409。
	w, _ = doJSON(t, r, http.MethodPost, fmt.Sprintf("/projects/%d/gates/initiation/decision", created.ID), map[string]any{"decision": "approve"})
	assert.Equal(t, http.StatusConflict, w.Code)
}

