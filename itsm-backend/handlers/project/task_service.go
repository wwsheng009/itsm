package project

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"itsm-backend/ent"
)

// 任务域哨兵错误（PE-4）。
var (
	ErrTaskNotFound           = errors.New("project task not found")
	ErrTaskDepthExceeded      = errors.New("task nesting depth exceeded")
	ErrTaskParentCrossProject = errors.New("parent task belongs to another project")
	ErrTaskHasChildren        = errors.New("task has children")
	ErrTaskInvalidTransition  = errors.New("invalid task status transition")
	ErrAssigneeNotMember      = errors.New("assignee is not an active project member")
	ErrTaskCycle              = errors.New("task parent cycle detected")
)

// ListTasks 任务列表（tree|flat）。
func (s *Service) ListTasks(ctx context.Context, tenantID, projectID, viewerID int, admin bool, status string, assigneeID int) ([]*ent.ProjectTask, error) {
	if _, err := s.GetForViewer(ctx, tenantID, projectID, viewerID, admin); err != nil {
		return nil, err
	}
	return s.repo.ListTasks(ctx, tenantID, projectID, status, assigneeID)
}

// validateHours 任务工时校验（PE-22）：nil=未填报；非 nil 须 >=0 且 <= MaxTaskHours。
func validateHours(field string, v *float64) error {
	if v == nil {
		return nil
	}
	if *v < 0 || *v > MaxTaskHours {
		return fmt.Errorf("%w: %s must be within 0..%g", ErrValidation, field, float64(MaxTaskHours))
	}
	return nil
}

// HoursSummary 项目任务工时汇总（PE-22；可见性收窄：不可见/不存在统一 404）。
// 口径：项目全部未删任务的 estimate_hours/actual_hours 求和（不做状态过滤），
// 供概览展示与 PE-24 报表引用（见 plan/p2-budget-hours-design.md D-35）。
func (s *Service) HoursSummary(ctx context.Context, tenantID, projectID, viewerID int, admin bool) (*TaskHoursSummary, error) {
	if _, err := s.GetForViewer(ctx, tenantID, projectID, viewerID, admin); err != nil {
		return nil, err
	}
	return s.repo.SumTaskHours(ctx, tenantID, projectID)
}

// GetTask 任务详情。
func (s *Service) GetTask(ctx context.Context, tenantID, projectID, taskID, viewerID int, admin bool) (*ent.ProjectTask, error) {
	if _, err := s.GetForViewer(ctx, tenantID, projectID, viewerID, admin); err != nil {
		return nil, err
	}
	task, err := s.repo.GetTask(ctx, tenantID, projectID, taskID)
	if err != nil {
		if ent.IsNotFound(err) {
			return nil, ErrTaskNotFound
		}
		return nil, err
	}
	return task, nil
}

// CreateTask 创建任务：WBS ≤3 层、父任务同项目、负责人须为活跃成员。
func (s *Service) CreateTask(ctx context.Context, tenantID, projectID, actorID int, req *TaskCreateRequest) (*ent.ProjectTask, error) {
	projectEntity, err := s.Get(ctx, tenantID, projectID)
	if err != nil {
		return nil, err
	}
	if projectEntity.Status == StatusCompleted || projectEntity.Status == StatusCancelled {
		return nil, fmt.Errorf("%w: project is closed", ErrValidation)
	}
	name := strings.TrimSpace(req.Name)
	if name == "" {
		return nil, fmt.Errorf("%w: task name is required", ErrValidation)
	}
	priority := strings.TrimSpace(req.Priority)
	if priority == "" {
		priority = "medium"
	}
	if !validTaskPriorities[priority] {
		return nil, fmt.Errorf("%w: invalid task priority %q", ErrValidation, req.Priority)
	}
	if req.PlanStart != nil && req.PlanEnd != nil && req.PlanEnd.Before(*req.PlanStart) {
		return nil, fmt.Errorf("%w: planEnd before planStart", ErrValidation)
	}
	// PE-22：工时口径校验（估算/实际均须 >=0；空=未填报）。
	if err := validateHours("estimateHours", req.EstimateHours); err != nil {
		return nil, err
	}

	if req.ParentTaskID > 0 {
		parent, err := s.repo.GetTaskByID(ctx, tenantID, req.ParentTaskID)
		if err != nil {
			if ent.IsNotFound(err) {
				return nil, fmt.Errorf("%w: parent task not found", ErrValidation)
			}
			return nil, err
		}
		if parent.ProjectID != projectID {
			return nil, ErrTaskParentCrossProject
		}
		depth, err := s.taskDepth(ctx, tenantID, req.ParentTaskID)
		if err != nil {
			return nil, err
		}
		if depth+1 > maxTaskDepth {
			return nil, ErrTaskDepthExceeded
		}
	}

	if req.AssigneeID > 0 {
		if err := s.ensureAssignee(ctx, tenantID, projectID, req.AssigneeID); err != nil {
			return nil, err
		}
	}

	return s.repo.CreateTask(ctx, TaskCreateParams{
		TenantID:      tenantID,
		ProjectID:     projectID,
		ParentTaskID:  req.ParentTaskID,
		Name:          name,
		Description:   strings.TrimSpace(req.Description),
		AssigneeID:    req.AssigneeID,
		Priority:      priority,
		PlanStart:     req.PlanStart,
		PlanEnd:       req.PlanEnd,
		EstimateHours: req.EstimateHours,
		SortOrder:     req.SortOrder,
		CreatedBy:     actorID,
	})
}

// UpdateTask 更新任务（乐观锁可选；改派须活跃成员）。
func (s *Service) UpdateTask(ctx context.Context, tenantID, projectID, taskID int, req *TaskUpdateRequest) (*ent.ProjectTask, error) {
	if _, err := s.GetTask(ctx, tenantID, projectID, taskID, 0, true); err != nil {
		return nil, err
	}
	if req.Priority != nil && !validTaskPriorities[*req.Priority] {
		return nil, fmt.Errorf("%w: invalid task priority %q", ErrValidation, *req.Priority)
	}
	if req.Progress != nil && (*req.Progress < 0 || *req.Progress > 100) {
		return nil, fmt.Errorf("%w: progress must be within 0-100", ErrValidation)
	}
	if req.PlanStart != nil && req.PlanEnd != nil && req.PlanEnd.Before(*req.PlanStart) {
		return nil, fmt.Errorf("%w: planEnd before planStart", ErrValidation)
	}
	// PE-22：工时口径校验（>=0；空=未填报）。
	if err := validateHours("estimateHours", req.EstimateHours); err != nil {
		return nil, err
	}
	if err := validateHours("actualHours", req.ActualHours); err != nil {
		return nil, err
	}
	if req.AssigneeID != nil && *req.AssigneeID > 0 {
		if err := s.ensureAssignee(ctx, tenantID, projectID, *req.AssigneeID); err != nil {
			return nil, err
		}
	}
	if req.Name == nil && req.Description == nil && req.AssigneeID == nil && req.Priority == nil &&
		req.PlanStart == nil && req.PlanEnd == nil && req.ActualStart == nil && req.ActualEnd == nil &&
		req.Progress == nil && req.EstimateHours == nil && req.ActualHours == nil && req.SortOrder == nil {
		return nil, ErrNoUpdateFields
	}

	version := derefInt(req.Version)
	updated, err := s.repo.UpdateTaskCAS(ctx, tenantID, projectID, taskID, TaskUpdateParams{
		Name:          req.Name,
		Description:   req.Description,
		AssigneeID:    req.AssigneeID,
		Priority:      req.Priority,
		PlanStart:     req.PlanStart,
		PlanEnd:       req.PlanEnd,
		ActualStart:   req.ActualStart,
		ActualEnd:     req.ActualEnd,
		Progress:      req.Progress,
		EstimateHours: req.EstimateHours,
		ActualHours:   req.ActualHours,
		SortOrder:     req.SortOrder,
	}, version)
	if err != nil {
		if ent.IsNotFound(err) {
			current, getErr := s.repo.GetTask(ctx, tenantID, projectID, taskID)
			if getErr != nil {
				return nil, ErrTaskNotFound
			}
			return nil, &CASConflictError{CurrentVersion: current.Version}
		}
		return nil, err
	}
	return updated, nil
}

// TransitionTask 任务状态流转（start/block/unblock/complete/cancel）。
func (s *Service) TransitionTask(ctx context.Context, tenantID, projectID, taskID int, req *TaskStatusRequest) (*ent.ProjectTask, error) {
	action := strings.TrimSpace(req.Action)
	if _, ok := taskActionTransitions[action]; !ok {
		return nil, fmt.Errorf("%w: unknown action %q", ErrTaskInvalidTransition, req.Action)
	}
	current, err := s.repo.GetTask(ctx, tenantID, projectID, taskID)
	if err != nil {
		if ent.IsNotFound(err) {
			return nil, ErrTaskNotFound
		}
		return nil, err
	}
	to, ok := taskActionTransitions[action][current.Status]
	if !ok {
		return nil, fmt.Errorf("%w: action %q not allowed from %q", ErrTaskInvalidTransition, action, current.Status)
	}
	if req.Version != nil && *req.Version != current.Version {
		return nil, &CASConflictError{CurrentVersion: current.Version}
	}

	patch := TaskStatusPatch{}
	now := s.now()
	switch action {
	case TaskActionStart:
		if current.ActualStart.IsZero() {
			patch.ActualStart = &now
		}
		if current.Progress == 0 {
			progress := 10
			patch.Progress = &progress
		}
	case TaskActionComplete:
		patch.ActualEnd = &now
		progress := 100
		patch.Progress = &progress
	}

	updated, err := s.repo.TaskStatusCAS(ctx, tenantID, projectID, taskID, current.Status, to, derefInt(req.Version), patch)
	if err != nil {
		if ent.IsNotFound(err) {
			latest, getErr := s.repo.GetTask(ctx, tenantID, projectID, taskID)
			if getErr != nil {
				return nil, ErrTaskNotFound
			}
			return nil, &CASConflictError{CurrentVersion: latest.Version}
		}
		return nil, err
	}
	return updated, nil
}

// DeleteTask 软删任务（有子任务时 409）。
func (s *Service) DeleteTask(ctx context.Context, tenantID, projectID, taskID int) error {
	if _, err := s.repo.GetTask(ctx, tenantID, projectID, taskID); err != nil {
		if ent.IsNotFound(err) {
			return ErrTaskNotFound
		}
		return err
	}
	children, err := s.repo.CountTaskChildren(ctx, tenantID, projectID, taskID)
	if err != nil {
		return err
	}
	if children > 0 {
		return ErrTaskHasChildren
	}
	if err := s.repo.SoftDeleteTask(ctx, tenantID, projectID, taskID); err != nil {
		if ent.IsNotFound(err) {
			return ErrTaskNotFound
		}
		return err
	}
	return nil
}

// MyTasks 个人任务聚合（跨可见项目；默认排除终态）。
func (s *Service) MyTasks(ctx context.Context, tenantID, viewerID int, admin bool, status string, limit int) ([]MyTaskResponse, error) {
	var projectIDs []int
	if !admin {
		ids, err := s.repo.MemberProjectIDs(ctx, tenantID, viewerID)
		if err != nil {
			return nil, err
		}
		projectIDs = ids
	}
	tasks, err := s.repo.ListTasksByAssignee(ctx, tenantID, viewerID, projectIDs, status, limit)
	if err != nil {
		return nil, err
	}
	if len(tasks) == 0 {
		return []MyTaskResponse{}, nil
	}
	projectName := make(map[int]string, len(tasks))
	for _, t := range tasks {
		if _, ok := projectName[t.ProjectID]; ok {
			continue
		}
		p, err := s.repo.Get(ctx, tenantID, t.ProjectID)
		if err != nil {
			projectName[t.ProjectID] = ""
			continue
		}
		projectName[t.ProjectID] = p.Name
	}
	out := make([]MyTaskResponse, 0, len(tasks))
	for _, t := range tasks {
		out = append(out, MyTaskResponse{
			ProjectTaskResponse: ToTaskResponse(t),
			ProjectName:         projectName[t.ProjectID],
		})
	}
	return out, nil
}

// ensureAssignee 负责人须为项目活跃成员（集成契约 §3.1/04 §3.1）。
func (s *Service) ensureAssignee(ctx context.Context, tenantID, projectID, userID int) error {
	ok, err := s.repo.IsActiveMember(ctx, tenantID, projectID, userID)
	if err != nil {
		return err
	}
	if !ok {
		return ErrAssigneeNotMember
	}
	return nil
}

// taskDepth 沿 parent 链回溯计算深度（含环检测）。
func (s *Service) taskDepth(ctx context.Context, tenantID, taskID int) (int, error) {
	depth := 1
	seen := map[int]struct{}{taskID: {}}
	current := taskID
	for {
		task, err := s.repo.GetTaskByID(ctx, tenantID, current)
		if err != nil {
			if ent.IsNotFound(err) {
				return depth, nil
			}
			return 0, err
		}
		if task.ParentTaskID <= 0 {
			return depth, nil
		}
		if _, dup := seen[task.ParentTaskID]; dup {
			return 0, ErrTaskCycle
		}
		seen[task.ParentTaskID] = struct{}{}
		current = task.ParentTaskID
		depth++
		if depth > maxTaskDepth {
			return depth, nil
		}
	}
}

// nowFunc 供测试替换（当前未使用，保留扩展点）。
var _ = time.Now
