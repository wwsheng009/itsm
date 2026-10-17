package project

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	"itsm-backend/ent"
)

// 基线域哨兵错误（PE-13）。
var (
	ErrBaselineNotFound        = errors.New("project baseline not found")
	ErrBaselineVersionConflict = errors.New("project baseline version conflict")
)

// baselineReasonMaxLen 基线变更原因上限（对齐 ent MaxLen(512)）。
const baselineReasonMaxLen = 512

// ListBaselines 基线列表（可见性谓词复用 GetForViewer；version 倒序）。
func (s *Service) ListBaselines(ctx context.Context, tenantID, projectID, viewerID int, admin bool) ([]*ent.ProjectBaseline, error) {
	if _, err := s.GetForViewer(ctx, tenantID, projectID, viewerID, admin); err != nil {
		return nil, err
	}
	return s.repo.ListBaselines(ctx, tenantID, projectID)
}

// CreateBaseline 冻结计划基线：快照当前任务计划/里程碑/项目计划面。
//
// 规则：终态项目拒绝冻结；首版 reason 可选、后续版本必填；
// 并发冻结由 (project_id, version) 唯一索引兜底，冲突自动重取版本重试。
func (s *Service) CreateBaseline(ctx context.Context, tenantID, projectID, actorID int, req *BaselineCreateRequest) (*ent.ProjectBaseline, error) {
	current, err := s.Get(ctx, tenantID, projectID)
	if err != nil {
		return nil, err
	}
	if current.Status == StatusCompleted || current.Status == StatusCancelled {
		return nil, fmt.Errorf("%w: project is closed", ErrValidation)
	}
	reason := ""
	if req != nil {
		reason = strings.TrimSpace(req.Reason)
	}
	if utf8.RuneCountInString(reason) > baselineReasonMaxLen {
		return nil, fmt.Errorf("%w: reason too long (max %d)", ErrValidation, baselineReasonMaxLen)
	}

	tasks, err := s.repo.ListTasks(ctx, tenantID, projectID, "", 0)
	if err != nil {
		return nil, err
	}
	milestones, err := s.repo.ListMilestones(ctx, tenantID, projectID, "")
	if err != nil {
		return nil, err
	}
	payload, err := snapshotToMap(newBaselineSnapshot(s.now(), current, tasks, milestones))
	if err != nil {
		return nil, err
	}

	for attempt := 0; attempt < 3; attempt++ {
		version, err := s.repo.NextBaselineVersion(ctx, tenantID, projectID)
		if err != nil {
			return nil, err
		}
		if version > 1 && reason == "" {
			return nil, fmt.Errorf("%w: reason is required for baseline change (version %d)", ErrValidation, version)
		}
		created, err := s.repo.CreateBaseline(ctx, BaselineCreateParams{
			TenantID:  tenantID,
			ProjectID: projectID,
			Version:   version,
			Snapshot:  payload,
			Reason:    reason,
			CreatedBy: actorID,
		})
		if err == nil {
			return created, nil
		}
		if !ent.IsConstraintError(err) {
			return nil, err
		}
		// 版本竞争：重取 NextBaselineVersion 后重试。
	}
	return nil, ErrBaselineVersionConflict
}

// BaselineDiff 最新基线 vs 当前状态偏差（实时计算；无基线 404 语义）。
func (s *Service) BaselineDiff(ctx context.Context, tenantID, projectID, viewerID int, admin bool) (*BaselineDiffResponse, error) {
	current, err := s.GetForViewer(ctx, tenantID, projectID, viewerID, admin)
	if err != nil {
		return nil, err
	}
	base, err := s.repo.LatestBaseline(ctx, tenantID, projectID)
	if err != nil {
		if ent.IsNotFound(err) {
			return nil, ErrBaselineNotFound
		}
		return nil, err
	}
	snap, err := snapshotFromMap(base.Snapshot)
	if err != nil {
		return nil, fmt.Errorf("%w: decode snapshot: %v", ErrValidation, err)
	}
	tasks, err := s.repo.ListTasks(ctx, tenantID, projectID, "", 0)
	if err != nil {
		return nil, err
	}
	milestones, err := s.repo.ListMilestones(ctx, tenantID, projectID, "")
	if err != nil {
		return nil, err
	}
	return computeBaselineDiff(base, snap, current, tasks, milestones), nil
}

// computeBaselineDiff 计算任务/里程碑偏差（added/removed/changed/unchanged）。
func computeBaselineDiff(base *ent.ProjectBaseline, snap *BaselineSnapshot, current *ent.Project, tasks []*ent.ProjectTask, milestones []*ent.ProjectMilestone) *BaselineDiffResponse {
	resp := &BaselineDiffResponse{
		BaselineID:       base.ID,
		Version:          base.Version,
		FrozenAt:         snap.FrozenAt,
		CreatedAt:        base.CreatedAt,
		TaskChanges:      make([]BaselineTaskDeviation, 0),
		MilestoneChanges: make([]BaselineMilestoneDeviation, 0),
	}
	// PE-22（D-34）：项目级预算偏差（v1 快照 frozen=false，不参与变更判定）。
	resp.Budget = computeBudgetDeviation(snap, current)

	baseTasks := make(map[int]BaselineTaskSnapshot, len(snap.Tasks))
	for _, t := range snap.Tasks {
		baseTasks[t.ID] = t
	}
	currentTasks := make(map[int]*ent.ProjectTask, len(tasks))
	for _, t := range tasks {
		currentTasks[t.ID] = t
	}
	for _, t := range tasks {
		dev := BaselineTaskDeviation{TaskID: t.ID, Name: t.Name}
		b, ok := baseTasks[t.ID]
		if !ok {
			dev.ChangeType = BaselineChangeAdded
			dev.CurrentStatus = t.Status
			dev.ProgressDelta = t.Progress
			resp.TaskSummary.Added++
			resp.TaskChanges = append(resp.TaskChanges, dev)
			continue
		}
		dev.BaselineStatus = b.Status
		dev.CurrentStatus = t.Status
		dev.StatusChanged = b.Status != t.Status
		dev.ProgressDelta = t.Progress - b.Progress
		if shift, changed := planDateShift(b.PlanEnd, optionalTimePtr(t.PlanEnd)); changed {
			dev.PlanEndShiftDays = &shift
		}
		if dev.StatusChanged || dev.ProgressDelta != 0 || dev.PlanEndShiftDays != nil || !sameTimePtr(b.PlanStart, optionalTimePtr(t.PlanStart)) || b.Name != t.Name {
			dev.ChangeType = BaselineChangeChanged
			resp.TaskSummary.Changed++
		} else {
			dev.ChangeType = BaselineChangeUnchanged
			resp.TaskSummary.Unchanged++
		}
		resp.TaskChanges = append(resp.TaskChanges, dev)
	}
	for _, b := range snap.Tasks {
		if _, ok := currentTasks[b.ID]; ok {
			continue
		}
		resp.TaskSummary.Removed++
		resp.TaskChanges = append(resp.TaskChanges, BaselineTaskDeviation{
			TaskID:         b.ID,
			Name:           b.Name,
			ChangeType:     BaselineChangeRemoved,
			BaselineStatus: b.Status,
		})
	}
	sort.Slice(resp.TaskChanges, func(i, j int) bool { return resp.TaskChanges[i].TaskID < resp.TaskChanges[j].TaskID })

	baseMilestones := make(map[int]BaselineMilestoneSnapshot, len(snap.Milestones))
	for _, m := range snap.Milestones {
		baseMilestones[m.ID] = m
	}
	currentMilestones := make(map[int]*ent.ProjectMilestone, len(milestones))
	for _, m := range milestones {
		currentMilestones[m.ID] = m
	}
	for _, m := range milestones {
		dev := BaselineMilestoneDeviation{MilestoneID: m.ID, Name: m.Name}
		b, ok := baseMilestones[m.ID]
		if !ok {
			dev.ChangeType = BaselineChangeAdded
			dev.CurrentStatus = m.Status
			resp.MilestoneSummary.Added++
			resp.MilestoneChanges = append(resp.MilestoneChanges, dev)
			continue
		}
		dev.BaselineStatus = b.Status
		dev.CurrentStatus = m.Status
		dev.StatusChanged = b.Status != m.Status
		shift := dayShift(b.DueDate, m.DueDate)
		if shift != 0 {
			dev.DueDateShiftDays = &shift
		}
		if dev.StatusChanged || dev.DueDateShiftDays != nil || b.Name != m.Name {
			dev.ChangeType = BaselineChangeChanged
			resp.MilestoneSummary.Changed++
		} else {
			dev.ChangeType = BaselineChangeUnchanged
			resp.MilestoneSummary.Unchanged++
		}
		resp.MilestoneChanges = append(resp.MilestoneChanges, dev)
	}
	for _, b := range snap.Milestones {
		if _, ok := currentMilestones[b.ID]; ok {
			continue
		}
		resp.MilestoneSummary.Removed++
		resp.MilestoneChanges = append(resp.MilestoneChanges, BaselineMilestoneDeviation{
			MilestoneID:    b.ID,
			Name:           b.Name,
			ChangeType:     BaselineChangeRemoved,
			BaselineStatus: b.Status,
		})
	}
	sort.Slice(resp.MilestoneChanges, func(i, j int) bool {
		return resp.MilestoneChanges[i].MilestoneID < resp.MilestoneChanges[j].MilestoneID
	})
	return resp
}

// computeBudgetDeviation 项目级预算偏差（PE-22/D-34）：仅 v2+ 快照参与 changed 判定。
func computeBudgetDeviation(snap *BaselineSnapshot, current *ent.Project) *BaselineBudgetDeviation {
	dev := &BaselineBudgetDeviation{
		Frozen:          snap.SchemaVersion >= 2,
		BaseAmount:      snap.Project.BudgetAmount,
		BaseCurrency:    snap.Project.BudgetCurrency,
		CurrentAmount:   positiveFloatPtr(current.BudgetAmount),
		CurrentCurrency: nonEmptyStringPtr(current.BudgetCurrency),
	}
	if dev.Frozen {
		dev.Changed = !floatPtrEqual(dev.BaseAmount, dev.CurrentAmount) ||
			!stringPtrEqual(dev.BaseCurrency, dev.CurrentCurrency)
	}
	return dev
}

// planDateShift 计划日期偏移（日历天；nil/同值返回 changed=false）。
func planDateShift(from, to *time.Time) (int, bool) {
	if from == nil || to == nil {
		if from == nil && to == nil {
			return 0, false
		}
		return 0, true // 一侧有值一侧无值：视为日期计划变更（shift=0）。
	}
	shift := dayShift(*from, *to)
	return shift, shift != 0
}

// dayShift 日历天差（to - from；本地日期粒度，避免时区/时刻噪声）。
func dayShift(from, to time.Time) int {
	fy, fm, fd := from.Date()
	ty, tm, td := to.Date()
	fromDay := time.Date(fy, fm, fd, 0, 0, 0, 0, time.UTC)
	toDay := time.Date(ty, tm, td, 0, 0, 0, 0, time.UTC)
	return int(toDay.Sub(fromDay).Hours() / 24)
}

func sameTimePtr(a, b *time.Time) bool {
	if a == nil || b == nil {
		return a == nil && b == nil
	}
	return a.Equal(*b)
}
