package project

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"itsm-backend/ent"
)

// 风险/问题台账域哨兵错误（P2，PE-21）。
var (
	ErrRiskNotFound           = errors.New("project risk not found")
	ErrInvalidRiskState       = errors.New("invalid risk status transition")
	ErrRiskResolutionRequired = errors.New("risk resolution is required to close")
	ErrRiskOwnerNotMember     = errors.New("risk owner is not an active project member")
	ErrRiskTicketNotFound     = errors.New("related ticket not found in tenant")
	ErrRiskPortUnavailable    = errors.New("risk conversion port unavailable")
	ErrRiskConversionFailed   = errors.New("risk conversion failed")
)

// RiskAlreadyConvertedError 二次转换（409 + existingId，D-28）。
type RiskAlreadyConvertedError struct {
	Target     string
	ExistingID int
}

func (e *RiskAlreadyConvertedError) Error() string {
	return fmt.Sprintf("risk already converted to %s (#%d)", e.Target, e.ExistingID)
}

// RiskConvertPartialError 转换产物已创建但回写关联失败（500；提示人工关联）。
type RiskConvertPartialError struct {
	Target    string
	CreatedID int
	Err       error
}

func (e *RiskConvertPartialError) Error() string {
	return fmt.Sprintf("%s #%d created but risk link failed: %v", e.Target, e.CreatedID, e.Err)
}

// Unwrap 暴露底层错误（审计/日志用）。
func (e *RiskConvertPartialError) Unwrap() error { return e.Err }

// ListRisks 风险/问题列表（可见性谓词复用 GetForViewer；kind/status/level 过滤）。
func (s *Service) ListRisks(
	ctx context.Context, tenantID, projectID, viewerID int, admin bool,
	kind, status, level string,
) ([]*ent.ProjectRisk, error) {
	if _, err := s.GetForViewer(ctx, tenantID, projectID, viewerID, admin); err != nil {
		return nil, err
	}
	if kind != "" && !validRiskKinds[kind] {
		return nil, fmt.Errorf("%w: invalid kind %q", ErrValidation, kind)
	}
	if status != "" && !validRiskStatuses[status] {
		return nil, fmt.Errorf("%w: invalid status %q", ErrValidation, status)
	}
	if level != "" && !validRiskLevels[level] {
		return nil, fmt.Errorf("%w: invalid level %q", ErrValidation, level)
	}
	return s.repo.ListRisks(ctx, RiskListFilter{
		TenantID:  tenantID,
		ProjectID: projectID,
		Kind:      kind,
		Status:    status,
		Level:     level,
	})
}

// GetRiskForViewer 单条风险（可见性谓词；不可见/不存在统一 404）。
func (s *Service) GetRiskForViewer(
	ctx context.Context, tenantID, projectID, riskID, viewerID int, admin bool,
) (*ent.ProjectRisk, error) {
	if _, err := s.GetForViewer(ctx, tenantID, projectID, viewerID, admin); err != nil {
		return nil, err
	}
	return s.getRisk(ctx, tenantID, projectID, riskID)
}

// getRisk 域内取单条（未软删；不存在 → ErrRiskNotFound）。
func (s *Service) getRisk(ctx context.Context, tenantID, projectID, riskID int) (*ent.ProjectRisk, error) {
	r, err := s.repo.GetRisk(ctx, tenantID, projectID, riskID)
	if err != nil {
		if ent.IsNotFound(err) {
			return nil, ErrRiskNotFound
		}
		return nil, err
	}
	return r, nil
}

// RiskOwnerNames 批量取负责人展示名（响应组装用；失败降级为空 map，不阻塞展示）。
func (s *Service) RiskOwnerNames(ctx context.Context, rows []*ent.ProjectRisk) map[int]string {
	ids := make([]int, 0, len(rows))
	seen := make(map[int]bool, len(rows))
	for _, r := range rows {
		if r.OwnerID > 0 && !seen[r.OwnerID] {
			seen[r.OwnerID] = true
			ids = append(ids, r.OwnerID)
		}
	}
	nameByID := make(map[int]string, len(ids))
	if len(ids) == 0 {
		return nameByID
	}
	users, err := s.repo.UsersByIDs(ctx, ids)
	if err != nil {
		return nameByID
	}
	for _, u := range users {
		nameByID[u.ID] = u.Name
	}
	return nameByID
}

// CreateRisk 创建风险/问题：终态项目拒绝（D-30）；负责人须为项目活跃成员（D-31）；
// 关联工单须同租户存在（弱引用校验）。
func (s *Service) CreateRisk(
	ctx context.Context, tenantID, projectID, actorID int, req *RiskCreateRequest,
) (*ent.ProjectRisk, error) {
	current, err := s.Get(ctx, tenantID, projectID)
	if err != nil {
		return nil, err
	}
	if current.Status == StatusCompleted || current.Status == StatusCancelled {
		return nil, fmt.Errorf("%w: project is closed", ErrValidation)
	}
	kind := strings.TrimSpace(req.Kind)
	if kind == "" {
		kind = RiskKindRisk
	}
	if !validRiskKinds[kind] {
		return nil, fmt.Errorf("%w: invalid kind %q", ErrValidation, req.Kind)
	}
	title := strings.TrimSpace(req.Title)
	if title == "" {
		return nil, fmt.Errorf("%w: title is required", ErrValidation)
	}
	level := strings.TrimSpace(req.Level)
	if level == "" {
		level = RiskLevelMedium
	}
	if !validRiskLevels[level] {
		return nil, fmt.Errorf("%w: invalid level %q", ErrValidation, req.Level)
	}
	probability := strings.TrimSpace(req.Probability)
	if probability != "" && !validRiskLevels[probability] {
		return nil, fmt.Errorf("%w: invalid probability %q", ErrValidation, req.Probability)
	}
	impact := strings.TrimSpace(req.Impact)
	if impact != "" && !validRiskLevels[impact] {
		return nil, fmt.Errorf("%w: invalid impact %q", ErrValidation, req.Impact)
	}
	if req.OwnerID > 0 {
		if err := s.ensureAssignee(ctx, tenantID, projectID, req.OwnerID); err != nil {
			return nil, ErrRiskOwnerNotMember
		}
	}
	if req.RelatedTicketID > 0 {
		if err := s.ensureTicketExists(ctx, tenantID, req.RelatedTicketID); err != nil {
			return nil, err
		}
	}
	return s.repo.CreateRisk(ctx, RiskCreateParams{
		TenantID:        tenantID,
		ProjectID:       projectID,
		Kind:            kind,
		Title:           title,
		Description:     strings.TrimSpace(req.Description),
		Level:           level,
		Probability:     probability,
		Impact:          impact,
		OwnerID:         req.OwnerID,
		RelatedTicketID: req.RelatedTicketID,
		CreatedBy:       actorID,
	})
}

// UpdateRisk 更新风险字段（不含状态）；version 可选乐观锁（冲突 409）。
func (s *Service) UpdateRisk(
	ctx context.Context, tenantID, projectID, riskID int, req *RiskUpdateRequest,
) (*ent.ProjectRisk, error) {
	if _, err := s.getRisk(ctx, tenantID, projectID, riskID); err != nil {
		return nil, err
	}
	if req.Kind != nil && !validRiskKinds[strings.TrimSpace(*req.Kind)] {
		return nil, fmt.Errorf("%w: invalid kind %q", ErrValidation, *req.Kind)
	}
	if req.Title != nil && strings.TrimSpace(*req.Title) == "" {
		return nil, fmt.Errorf("%w: title is required", ErrValidation)
	}
	if req.Level != nil && !validRiskLevels[strings.TrimSpace(*req.Level)] {
		return nil, fmt.Errorf("%w: invalid level %q", ErrValidation, *req.Level)
	}
	if req.Probability != nil {
		v := strings.TrimSpace(*req.Probability)
		if v != "" && !validRiskLevels[v] {
			return nil, fmt.Errorf("%w: invalid probability %q", ErrValidation, *req.Probability)
		}
	}
	if req.Impact != nil {
		v := strings.TrimSpace(*req.Impact)
		if v != "" && !validRiskLevels[v] {
			return nil, fmt.Errorf("%w: invalid impact %q", ErrValidation, *req.Impact)
		}
	}
	if req.Kind == nil && req.Title == nil && req.Description == nil && req.Level == nil &&
		req.Probability == nil && req.Impact == nil && req.OwnerID == nil && req.RelatedTicketID == nil {
		return nil, ErrNoUpdateFields
	}
	if req.OwnerID != nil && *req.OwnerID > 0 {
		if err := s.ensureAssignee(ctx, tenantID, projectID, *req.OwnerID); err != nil {
			return nil, ErrRiskOwnerNotMember
		}
	}
	if req.RelatedTicketID != nil && *req.RelatedTicketID > 0 {
		if err := s.ensureTicketExists(ctx, tenantID, *req.RelatedTicketID); err != nil {
			return nil, err
		}
	}

	p := RiskUpdateParams{}
	if req.Kind != nil {
		v := strings.TrimSpace(*req.Kind)
		p.Kind = &v
	}
	if req.Title != nil {
		v := strings.TrimSpace(*req.Title)
		p.Title = &v
	}
	if req.Description != nil {
		v := strings.TrimSpace(*req.Description)
		p.Description = &v
	}
	if req.Level != nil {
		v := strings.TrimSpace(*req.Level)
		p.Level = &v
	}
	if req.Probability != nil {
		v := strings.TrimSpace(*req.Probability)
		p.Probability = &v
	}
	if req.Impact != nil {
		v := strings.TrimSpace(*req.Impact)
		p.Impact = &v
	}
	p.OwnerID = req.OwnerID
	p.RelatedTicketID = req.RelatedTicketID

	updated, err := s.repo.UpdateRiskCAS(ctx, tenantID, projectID, riskID, p, derefInt(req.Version))
	if err != nil {
		if ent.IsNotFound(err) {
			latest, getErr := s.getRisk(ctx, tenantID, projectID, riskID)
			if getErr != nil {
				return nil, getErr
			}
			return nil, &CASConflictError{CurrentVersion: latest.Version}
		}
		return nil, err
	}
	return updated, nil
}

// TransitionRisk 状态流转（mitigate|close|reopen；close 必填 resolution = D-27）。
func (s *Service) TransitionRisk(
	ctx context.Context, tenantID, projectID, riskID int, req *RiskStatusRequest,
) (*ent.ProjectRisk, error) {
	action := strings.TrimSpace(req.Action)
	if _, ok := riskActionTransitions[action]; !ok {
		return nil, fmt.Errorf("%w: unknown action %q", ErrInvalidRiskState, req.Action)
	}
	current, err := s.getRisk(ctx, tenantID, projectID, riskID)
	if err != nil {
		return nil, err
	}
	to, ok := riskActionTransitions[action][current.Status]
	if !ok {
		return nil, fmt.Errorf("%w: action %q not allowed from %q", ErrInvalidRiskState, action, current.Status)
	}
	resolution := strings.TrimSpace(req.Resolution)
	if action == RiskActionClose && resolution == "" {
		return nil, ErrRiskResolutionRequired
	}
	updated, err := s.repo.RiskStatusCAS(ctx, tenantID, projectID, riskID, current.Status, to, resolution, derefInt(req.Version))
	if err != nil {
		if ent.IsNotFound(err) {
			return nil, fmt.Errorf("%w: concurrent update", ErrInvalidRiskState)
		}
		return nil, err
	}
	return updated, nil
}

// DeleteRisk 软删（已转换记录仍可删，关联实体不受影响）。
func (s *Service) DeleteRisk(ctx context.Context, tenantID, projectID, riskID int) error {
	if err := s.repo.SoftDeleteRisk(ctx, tenantID, projectID, riskID); err != nil {
		if ent.IsNotFound(err) {
			return ErrRiskNotFound
		}
		return err
	}
	return nil
}

// ConvertRisk 转问题单/变更单（D-28/D-29）：校验 → 端口创建 → CAS 回写关联。
// 返回（更新后的风险，产物ID，错误）。
func (s *Service) ConvertRisk(
	ctx context.Context, tenantID, projectID, riskID, actorID int, req *RiskConvertRequest,
) (*ent.ProjectRisk, int, error) {
	target := strings.TrimSpace(req.Target)
	if target != ConvertTargetProblem && target != ConvertTargetChange {
		return nil, 0, fmt.Errorf("%w: invalid target %q", ErrValidation, req.Target)
	}
	project, err := s.Get(ctx, tenantID, projectID)
	if err != nil {
		return nil, 0, err
	}
	if project.Status == StatusCompleted || project.Status == StatusCancelled {
		return nil, 0, fmt.Errorf("%w: project is closed", ErrValidation)
	}
	current, err := s.getRisk(ctx, tenantID, projectID, riskID)
	if err != nil {
		return nil, 0, err
	}
	// 已转换守卫（同目标二次转换 409 + existingId）。
	if target == ConvertTargetProblem && current.RelatedProblemID > 0 {
		return nil, current.RelatedProblemID, &RiskAlreadyConvertedError{Target: target, ExistingID: current.RelatedProblemID}
	}
	if target == ConvertTargetChange && current.RelatedChangeID > 0 {
		return nil, current.RelatedChangeID, &RiskAlreadyConvertedError{Target: target, ExistingID: current.RelatedChangeID}
	}

	input := RiskConvertInput{
		RiskID:      current.ID,
		Kind:        current.Kind,
		Title:       current.Title,
		Description: current.Description,
		Level:       current.Level,
		Probability: current.Probability,
		Impact:      current.Impact,
		ProjectID:   project.ID,
		ProjectCode: project.Code,
		ProjectName: project.Name,
	}

	var createdID int
	switch target {
	case ConvertTargetProblem:
		if s.riskProblem == nil {
			return nil, 0, ErrRiskPortUnavailable
		}
		createdID, err = s.riskProblem.CreateProblemFromRisk(ctx, tenantID, actorID, input)
	case ConvertTargetChange:
		if s.riskChange == nil {
			return nil, 0, ErrRiskPortUnavailable
		}
		createdID, err = s.riskChange.CreateChangeFromRisk(ctx, tenantID, actorID, input)
	}
	if err != nil {
		return nil, 0, fmt.Errorf("%w: %v", ErrRiskConversionFailed, err)
	}

	linked, err := s.repo.LinkRiskConversion(ctx, tenantID, projectID, riskID, target, createdID)
	if err != nil {
		return nil, createdID, &RiskConvertPartialError{Target: target, CreatedID: createdID, Err: err}
	}
	return linked, createdID, nil
}

// ensureTicketExists 关联工单弱引用校验（同租户存在）。
func (s *Service) ensureTicketExists(ctx context.Context, tenantID, ticketID int) error {
	ok, err := s.repo.TicketExists(ctx, tenantID, ticketID)
	if err != nil {
		return err
	}
	if !ok {
		return ErrRiskTicketNotFound
	}
	return nil
}