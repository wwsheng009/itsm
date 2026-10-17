package project

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"itsm-backend/ent"
)

// 里程碑域哨兵错误（PE-14）。
var (
	ErrMilestoneNotFound       = errors.New("project milestone not found")
	ErrInvalidMilestoneState   = errors.New("invalid milestone status transition")
	ErrMilestoneOwnerNotMember = errors.New("milestone owner is not an active project member")
)

// ListMilestones 里程碑列表（可见性谓词复用 GetForViewer）。
func (s *Service) ListMilestones(ctx context.Context, tenantID, projectID, viewerID int, admin bool, status string) ([]*ent.ProjectMilestone, error) {
	if _, err := s.GetForViewer(ctx, tenantID, projectID, viewerID, admin); err != nil {
		return nil, err
	}
	if status != "" && status != MilestoneStatusPlanned && status != MilestoneStatusReached && status != MilestoneStatusMissed {
		return nil, fmt.Errorf("%w: invalid status %q", ErrValidation, status)
	}
	return s.repo.ListMilestones(ctx, tenantID, projectID, status)
}

// MyMilestones 我的里程碑到期聚合（PE-16，个人工作台）：跨可见项目、owner=当前用户、仅 planned、
// 默认只看 N 天内到期（含逾期；dueWithinDays<=0 不限），due_date 升序，limit 收敛到 1..200（默认 50）。
func (s *Service) MyMilestones(ctx context.Context, tenantID, viewerID int, admin bool, dueWithinDays, limit int) ([]MyMilestoneResponse, error) {
	if limit <= 0 {
		limit = 50
	}
	if limit > 200 {
		limit = 200
	}
	// 可见性收窄：非管理角色仅统计其为活跃成员的项目（与 /projects/my-tasks 同一口径）。
	var projectIDs []int
	if !admin {
		ids, err := s.repo.MemberProjectIDs(ctx, tenantID, viewerID)
		if err != nil {
			return nil, err
		}
		if len(ids) == 0 {
			return []MyMilestoneResponse{}, nil
		}
		projectIDs = ids
	}
	var dueBefore *time.Time
	if dueWithinDays > 0 {
		t := time.Now().AddDate(0, 0, dueWithinDays)
		dueBefore = &t
	}
	rows, err := s.repo.ListMilestonesByOwner(ctx, tenantID, viewerID, projectIDs, MilestoneStatusPlanned, dueBefore, limit)
	if err != nil {
		return nil, err
	}
	if len(rows) == 0 {
		return []MyMilestoneResponse{}, nil
	}
	names := make(map[int]string, len(rows))
	codes := make(map[int]string, len(rows))
	for _, m := range rows {
		if _, ok := names[m.ProjectID]; ok {
			continue
		}
		p, err := s.repo.Get(ctx, tenantID, m.ProjectID)
		if err != nil {
			names[m.ProjectID] = ""
			continue
		}
		names[m.ProjectID] = p.Name
		codes[m.ProjectID] = p.Code
	}
	return ToMyMilestoneResponseList(rows, names, codes, time.Now()), nil
}

// CreateMilestone 创建里程碑：目标日期必填；负责人须为项目活跃成员；终态项目拒绝。
func (s *Service) CreateMilestone(ctx context.Context, tenantID, projectID, actorID int, req *MilestoneCreateRequest) (*ent.ProjectMilestone, error) {
	current, err := s.Get(ctx, tenantID, projectID)
	if err != nil {
		return nil, err
	}
	if current.Status == StatusCompleted || current.Status == StatusCancelled {
		return nil, fmt.Errorf("%w: project is closed", ErrValidation)
	}
	name := strings.TrimSpace(req.Name)
	if name == "" {
		return nil, fmt.Errorf("%w: milestone name is required", ErrValidation)
	}
	if req.DueDate == nil {
		return nil, fmt.Errorf("%w: dueDate is required", ErrValidation)
	}
	if req.OwnerID > 0 {
		if err := s.ensureAssignee(ctx, tenantID, projectID, req.OwnerID); err != nil {
			return nil, ErrMilestoneOwnerNotMember
		}
	}
	return s.repo.CreateMilestone(ctx, MilestoneCreateParams{
		TenantID:    tenantID,
		ProjectID:   projectID,
		Name:        name,
		Description: strings.TrimSpace(req.Description),
		DueDate:     *req.DueDate,
		OwnerID:     req.OwnerID,
	})
}

// UpdateMilestone 更新里程碑（不改状态）。
func (s *Service) UpdateMilestone(ctx context.Context, tenantID, projectID, milestoneID int, req *MilestoneUpdateRequest) (*ent.ProjectMilestone, error) {
	if _, err := s.repo.GetMilestone(ctx, tenantID, projectID, milestoneID); err != nil {
		if ent.IsNotFound(err) {
			return nil, ErrMilestoneNotFound
		}
		return nil, err
	}
	if req.Name != nil && strings.TrimSpace(*req.Name) == "" {
		return nil, fmt.Errorf("%w: milestone name is required", ErrValidation)
	}
	if req.OwnerID != nil && *req.OwnerID > 0 {
		if err := s.ensureAssignee(ctx, tenantID, projectID, *req.OwnerID); err != nil {
			return nil, ErrMilestoneOwnerNotMember
		}
	}
	if req.Name == nil && req.Description == nil && req.DueDate == nil && req.OwnerID == nil {
		return nil, ErrNoUpdateFields
	}
	updated, err := s.repo.UpdateMilestone(ctx, tenantID, projectID, milestoneID, MilestoneUpdateParams{
		Name:        req.Name,
		Description: req.Description,
		DueDate:     req.DueDate,
		OwnerID:     req.OwnerID,
	})
	if err != nil {
		if ent.IsNotFound(err) {
			return nil, ErrMilestoneNotFound
		}
		return nil, err
	}
	return updated, nil
}

// TransitionMilestone 里程碑状态流转（reach|miss；reach 回填 reached_at）。
func (s *Service) TransitionMilestone(ctx context.Context, tenantID, projectID, milestoneID int, req *MilestoneStatusRequest) (*ent.ProjectMilestone, error) {
	action := strings.TrimSpace(req.Action)
	if _, ok := milestoneActionTransitions[action]; !ok {
		return nil, fmt.Errorf("%w: unknown action %q", ErrInvalidMilestoneState, req.Action)
	}
	current, err := s.repo.GetMilestone(ctx, tenantID, projectID, milestoneID)
	if err != nil {
		if ent.IsNotFound(err) {
			return nil, ErrMilestoneNotFound
		}
		return nil, err
	}
	to, ok := milestoneActionTransitions[action][current.Status]
	if !ok {
		return nil, fmt.Errorf("%w: action %q not allowed from %q", ErrInvalidMilestoneState, action, current.Status)
	}
	var reachedAt *time.Time
	if action == MilestoneActionReach {
		now := s.now()
		reachedAt = &now
	}
	updated, err := s.repo.MilestoneStatusCAS(ctx, tenantID, projectID, milestoneID, current.Status, to, reachedAt)
	if err != nil {
		if ent.IsNotFound(err) {
			return nil, fmt.Errorf("%w: concurrent update", ErrInvalidMilestoneState)
		}
		return nil, err
	}
	return updated, nil
}

// DeleteMilestone 删除里程碑（无软删字段：硬删；审计由中间件留痕）。
func (s *Service) DeleteMilestone(ctx context.Context, tenantID, projectID, milestoneID int) error {
	if err := s.repo.DeleteMilestone(ctx, tenantID, projectID, milestoneID); err != nil {
		if ent.IsNotFound(err) {
			return ErrMilestoneNotFound
		}
		return err
	}
	return nil
}
