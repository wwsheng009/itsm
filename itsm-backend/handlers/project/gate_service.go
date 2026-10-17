package project

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"itsm-backend/ent"
)

// 阶段门服务（PE-12）。
//
// 语义：
//   - 四道门固定顺序（initiation → plan → acceptance → closure），后门要求前门 passed；
//   - 提交（submit）执行本门门禁规则：不通过 → GateBlockedError（409 + blockers）且记录阻塞明细；
//     通过 → 触发 BPMN 审批（business_type=project，business_sub_type=<gate>，
//     business_key=project:<id>）；无绑定/无流程定义时降级为自动通过（记 auto_approved）；
//   - 裁决（decide）：approve 在流程走完（或无流程）后置 passed；reject 必须带原因，置 rejected（可重新提交）；
//   - 放行副作用：计划门通过 → 冻结计划基线（无基线时建 v1）；
//   - 状态机门禁：activate 要求立项门 passed，complete 要求结项门 passed（见 Transition）。

// 门服务哨兵错误。
var (
	// ErrGateAlreadyPassed 门已通过（重复提交 409）。
	ErrGateAlreadyPassed = errors.New("stage gate already passed")
	// ErrGateInReview 门审批中（重复提交/重复裁决 409）。
	ErrGateInReview = errors.New("stage gate is in review")
	// ErrGateNoApproval 门不在审批中（无待裁决记录 → 409）。
	ErrGateNoApproval = errors.New("stage gate is not awaiting approval")
	// ErrGateApprovalFailed 流程审批推进失败（审批人/流程状态异常 → 403/409 由调用方细化）。
	ErrGateApprovalFailed = errors.New("stage gate approval failed")
)

// GateApprovalStart 门审批启动结果。
type GateApprovalStart struct {
	// ProcessInstanceID 流程实例ID；0 = 无绑定/无流程定义（调用方降级为自动通过）。
	ProcessInstanceID int
	// ApprovalChainID 绑定中的审批链ID（可空）。
	ApprovalChainID string
}

// GateApprovalDecision 门裁决推进结果。
type GateApprovalDecision struct {
	// Handled 是否命中了待办流程任务（false = 无流程任务，业务可直接裁决）。
	Handled bool
	// FlowFinished 流程是否已结束（approve 时决定门是否可置 passed）。
	FlowFinished bool
}

// GateApprovalPort 阶段门审批端口（bootstrap 注入；未注入时提交即自动通过，便于未部署流程的租户）。
type GateApprovalPort interface {
	// SubmitGate 触发阶段门审批流程。
	SubmitGate(ctx context.Context, tenantID, projectID int, gate string, variables map[string]any, triggeredBy string) (GateApprovalStart, error)
	// DecideGate 完成当前待办审批任务（decision=approve|reject）。
	DecideGate(ctx context.Context, tenantID, actorID, projectID int, decision, comment string) (GateApprovalDecision, error)
}

// SetGateApproval 注入阶段门审批端口（bootstrap；测试可注入假实现）。
func (s *Service) SetGateApproval(port GateApprovalPort) {
	s.gateApproval = port
}

// ListGates 阶段门列表（可见性谓词；不存在/不可见 → 404）。
func (s *Service) ListGates(ctx context.Context, tenantID, projectID, viewerID int, admin bool) ([]*ent.ProjectGate, error) {
	if _, err := s.GetForViewer(ctx, tenantID, projectID, viewerID, admin); err != nil {
		return nil, err
	}
	return s.repo.ListGates(ctx, tenantID, projectID)
}

// SubmitGate 提交阶段门：门禁规则 → 触发审批（或自动通过）→ 记录状态与阻塞明细。
func (s *Service) SubmitGate(
	ctx context.Context, tenantID, projectID, actorID int, gate string, actorName string, admin bool,
) (*ent.ProjectGate, error) {
	gate = strings.ToLower(strings.TrimSpace(gate))
	if !IsValidGate(gate) {
		return nil, fmt.Errorf("%w: unknown gate %q", ErrValidation, gate)
	}
	project, err := s.GetForViewer(ctx, tenantID, projectID, actorID, admin)
	if err != nil {
		return nil, err
	}
	if project.Status == StatusCompleted || project.Status == StatusCancelled {
		return nil, fmt.Errorf("%w: project is closed", ErrValidation)
	}

	record, err := s.ensureGateRecord(ctx, tenantID, projectID, gate)
	if err != nil {
		return nil, err
	}
	switch record.Status {
	case GateStatusPassed:
		return nil, fmt.Errorf("%w: gate %q", ErrGateAlreadyPassed, gate)
	case GateStatusInReview:
		return nil, fmt.Errorf("%w: gate %q", ErrGateInReview, gate)
	}

	blockers, err := s.evaluateGateRules(ctx, tenantID, project, gate)
	if err != nil {
		return nil, err
	}
	if len(blockers) > 0 {
		// 记录阻塞明细（状态保持原值，版本递增），返回 409 语义。
		updated, casErr := s.repo.GateSubmitCAS(ctx, tenantID, record.ID, record.Status, record.Version, GateSubmitPatch{
			SubmittedBy: actorID,
			Blockers:    gateBlockersToJSON(blockers),
		}, record.Status)
		if casErr != nil {
			return nil, casErr
		}
		_ = updated
		return nil, &GateBlockedError{Gate: gate, Blockers: blockers}
	}

	// 门禁通过 → 触发审批流程（无绑定降级自动通过）。
	toStatus := GateStatusPassed
	patch := GateSubmitPatch{SubmittedBy: actorID, SubmittedAt: s.now()}
	if s.gateApproval != nil {
		start, startErr := s.gateApproval.SubmitGate(ctx, tenantID, projectID, gate, map[string]any{
			"projectId": projectID,
			"projectCode": project.Code,
			"projectName": project.Name,
			"gate":        gate,
			"operatorId":  actorID,
			"managerId":   project.ManagerID,
		}, actorName)
		if startErr != nil {
			return nil, fmt.Errorf("%w: %v", ErrGateApprovalFailed, startErr)
		}
		patch.ProcessInstanceID = start.ProcessInstanceID
		patch.ApprovalChainID = start.ApprovalChainID
		if start.ProcessInstanceID > 0 {
			toStatus = GateStatusInReview
		}
	}
	if toStatus == GateStatusPassed {
		// 降级自动通过：先做放行副作用（计划门冻结基线），失败则整体失败可重试。
		if sideErr := s.applyGatePassSideEffects(ctx, tenantID, projectID, gate, actorID); sideErr != nil {
			return nil, sideErr
		}
		now := s.now()
		patch.DecidedBy = actorID
		patch.DecidedAt = &now
		patch.DecisionComment = "auto_approved_no_flow"
	}

	updated, err := s.repo.GateSubmitCAS(ctx, tenantID, record.ID, record.Status, record.Version, patch, toStatus)
	if err != nil {
		return nil, err
	}
	if gate == GateClosure {
		// PE-15：结项门提交即自动生成复盘 KB 草稿（幂等、尽力而为；失败不阻塞门提交，
		// 可由结项清单「生成复盘草稿」手动重试）。
		s.autoGenerateRetroDraft(ctx, tenantID, projectID, actorID)
	}
	return updated, nil
}

// DecideGate 裁决阶段门：approve 在流程结束（或无流程）后置 passed；reject 必须带原因。
func (s *Service) DecideGate(
	ctx context.Context, tenantID, projectID, actorID int, gate string, req *GateDecisionRequest, admin bool,
) (*ent.ProjectGate, error) {
	gate = strings.ToLower(strings.TrimSpace(gate))
	if !IsValidGate(gate) {
		return nil, fmt.Errorf("%w: unknown gate %q", ErrValidation, gate)
	}
	req.Normalize()
	if !req.Valid() {
		return nil, fmt.Errorf("%w: decision must be approve|reject", ErrValidation)
	}
	if req.Decision == "reject" && req.Comment == "" {
		return nil, ErrReasonRequired
	}
	if _, err := s.GetForViewer(ctx, tenantID, projectID, actorID, admin); err != nil {
		return nil, err
	}
	record, err := s.repo.GetGate(ctx, tenantID, projectID, gate)
	if err != nil {
		if ent.IsNotFound(err) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	if record.Status != GateStatusInReview {
		return nil, fmt.Errorf("%w: gate %q status=%s", ErrGateNoApproval, gate, record.Status)
	}

	// 流程任务推进（有流程实例时）；approve 需流程结束才放行。
	if s.gateApproval != nil && record.ProcessInstanceID > 0 {
		decision, decideErr := s.gateApproval.DecideGate(ctx, tenantID, actorID, projectID, req.Decision, req.Comment)
		if decideErr != nil {
			return nil, fmt.Errorf("%w: %v", ErrGateApprovalFailed, decideErr)
		}
		if req.Decision == "approve" && decision.Handled && !decision.FlowFinished {
			// 流程多级审批中：本环节通过，门保持 in_review，等待后续审批人。
			return record, nil
		}
	}

	toStatus := GateStatusPassed
	if req.Decision == "reject" {
		toStatus = GateStatusRejected
	}
	if toStatus == GateStatusPassed {
		if sideErr := s.applyGatePassSideEffects(ctx, tenantID, projectID, gate, actorID); sideErr != nil {
			return nil, sideErr
		}
	}
	updated, err := s.repo.GateDecisionCAS(ctx, tenantID, record.ID, GateStatusInReview, record.Version, GateDecisionPatch{
		DecidedBy:       actorID,
		DecidedAt:       s.now(),
		DecisionComment: req.Comment,
		Blockers:        []map[string]any{},
	}, toStatus)
	if err != nil {
		return nil, err
	}
	return updated, nil
}

// requireGatePassed 状态机门禁：门必须已通过，否则 409（GateBlockedError）。
func (s *Service) requireGatePassed(ctx context.Context, tenantID, projectID int, gate string) error {
	record, err := s.repo.GetGate(ctx, tenantID, projectID, gate)
	if err != nil {
		if ent.IsNotFound(err) {
			return &GateBlockedError{Gate: gate, Blockers: []GateBlocker{{
				Type:   "gate_not_passed",
				Detail: fmt.Sprintf("阶段门 %q 未提交，请先在项目工作台提交", gate),
				Status: GateStatusPending,
			}}}
		}
		return err
	}
	if record.Status != GateStatusPassed {
		return &GateBlockedError{Gate: gate, Blockers: []GateBlocker{{
			Type:   "gate_not_passed",
			Detail: fmt.Sprintf("阶段门 %q 未通过（当前 %s）", gate, record.Status),
			RefID:  record.ID,
			Status: record.Status,
		}}}
	}
	return nil
}

// ensureGateRecord 取门记录；不存在则创建 pending 记录。
func (s *Service) ensureGateRecord(ctx context.Context, tenantID, projectID int, gate string) (*ent.ProjectGate, error) {
	record, err := s.repo.GetGate(ctx, tenantID, projectID, gate)
	if err == nil {
		return record, nil
	}
	if !ent.IsNotFound(err) {
		return nil, err
	}
	created, createErr := s.repo.CreateGate(ctx, GateCreateParams{
		TenantID: tenantID, ProjectID: projectID, Gate: gate, Status: GateStatusPending,
	})
	if createErr != nil {
		// 并发创建：唯一索引冲突后退回读取。
		if reread, rereadErr := s.repo.GetGate(ctx, tenantID, projectID, gate); rereadErr == nil {
			return reread, nil
		}
		return nil, createErr
	}
	return created, nil
}

// evaluateGateRules 执行门禁规则，返回阻塞明细（空 = 通过）。
func (s *Service) evaluateGateRules(ctx context.Context, tenantID int, project *ent.Project, gate string) ([]GateBlocker, error) {
	blockers := make([]GateBlocker, 0, 6)

	// 顺序前置：前门必须 passed。
	if prev := previousGate(gate); prev != "" {
		prevRecord, err := s.repo.GetGate(ctx, tenantID, project.ID, prev)
		if err != nil && !ent.IsNotFound(err) {
			return nil, err
		}
		prevStatus := GateStatusPending
		if prevRecord != nil {
			prevStatus = prevRecord.Status
		}
		if prevStatus != GateStatusPassed {
			blockers = append(blockers, GateBlocker{
				Type:   "gate_not_passed",
				Detail: fmt.Sprintf("前置阶段门 %q 未通过（当前 %s）", prev, prevStatus),
				Status: prevStatus,
			})
		}
	}

	switch gate {
	case GateInitiation:
		if project.ManagerID == 0 {
			blockers = append(blockers, GateBlocker{Type: "manager_missing", Detail: "未任命项目经理"})
		}
		members, err := s.repo.ListMembers(ctx, tenantID, project.ID)
		if err != nil {
			return nil, err
		}
		if len(members) == 0 {
			blockers = append(blockers, GateBlocker{Type: "member_missing", Detail: "未登记项目成员/干系人"})
		}
	case GatePlan:
		tasks, err := s.repo.ListTasks(ctx, tenantID, project.ID, "", 0)
		if err != nil {
			return nil, err
		}
		if countActiveTasks(tasks) == 0 {
			blockers = append(blockers, GateBlocker{Type: "task_missing", Detail: "计划为空：至少需要 1 个任务"})
		}
		milestones, err := s.repo.ListMilestones(ctx, tenantID, project.ID, "")
		if err != nil {
			return nil, err
		}
		if len(milestones) == 0 {
			blockers = append(blockers, GateBlocker{Type: "milestone_missing", Detail: "计划为空：至少需要 1 个里程碑"})
		}
	case GateAcceptance:
		tasks, err := s.repo.ListTasks(ctx, tenantID, project.ID, "", 0)
		if err != nil {
			return nil, err
		}
		if open, first := openTaskBlocker(tasks); open > 0 {
			b := GateBlocker{Type: "task_open", Total: open, Detail: fmt.Sprintf("%d 个任务未完成", open)}
			if first != nil {
				b.RefID = first.ID
				b.Status = first.Status
			}
			blockers = append(blockers, b)
		}
		milestones, err := s.repo.ListMilestones(ctx, tenantID, project.ID, "")
		if err != nil {
			return nil, err
		}
		if open, first := openMilestoneBlocker(milestones); open > 0 {
			b := GateBlocker{Type: "milestone_open", Total: open, Detail: fmt.Sprintf("%d 个里程碑未达成", open)}
			if first != nil {
				b.RefID = first.ID
				b.Status = first.Status
			}
			blockers = append(blockers, b)
		}
	case GateClosure:
		records, err := s.repo.NonTerminalRecords(ctx, tenantID, project.ID)
		if err != nil {
			return nil, err
		}
		for _, item := range records {
			blockers = append(blockers, GateBlocker{
				Type:   "record_open",
				Detail: item.Title,
				RefID:  item.ID,
				Number: item.Number,
				Status: item.Status,
			})
		}
	}
	return blockers, nil
}

// applyGatePassSideEffects 放行副作用：计划门通过 → 冻结计划基线（无基线时建 v1，不可变）。
func (s *Service) applyGatePassSideEffects(ctx context.Context, tenantID, projectID int, gate string, actorID int) error {
	if gate != GatePlan {
		return nil
	}
	_, err := s.repo.LatestBaseline(ctx, tenantID, projectID)
	if err == nil {
		return nil // 已有基线：不重复冻结（后续变更走新版本 + 原因）
	}
	if !ent.IsNotFound(err) {
		return err
	}
	if _, err := s.CreateBaseline(ctx, tenantID, projectID, actorID, &BaselineCreateRequest{
		Reason: "计划门通过自动冻结",
	}); err != nil {
		return fmt.Errorf("计划门冻结基线失败: %w", err)
	}
	return nil
}

// countActiveTasks 统计非取消任务。
func countActiveTasks(tasks []*ent.ProjectTask) int {
	n := 0
	for _, task := range tasks {
		if task.Status != "cancelled" {
			n++
		}
	}
	return n
}

// openTaskBlocker 未完成任务数（todo/in_progress/blocked）+ 首个明细。
func openTaskBlocker(tasks []*ent.ProjectTask) (int, *ent.ProjectTask) {
	count := 0
	var first *ent.ProjectTask
	for _, task := range tasks {
		switch task.Status {
		case "done", "cancelled":
			continue
		}
		count++
		if first == nil {
			first = task
		}
	}
	return count, first
}

// openMilestoneBlocker 未达成里程碑数 + 首个明细。
func openMilestoneBlocker(milestones []*ent.ProjectMilestone) (int, *ent.ProjectMilestone) {
	count := 0
	var first *ent.ProjectMilestone
	for _, m := range milestones {
		if m.Status == "reached" {
			continue
		}
		count++
		if first == nil {
			first = m
		}
	}
	return count, first
}
