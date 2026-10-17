package project

import (
	"encoding/json"
	"time"

	"itsm-backend/ent"
)

// ---------------------------------------------------------------------------
// 计划基线 DTO / 快照结构 / 偏差结构（P1，PE-13）
//
// 权威定义：docs/project-management/plan/p0-data-ddl.md §4.5；
// 集成契约：plan/p0-integration-contracts.md §4（计划门通过时冻结；不可变；
// 变更 = 插入新 version + reason，禁止 UPDATE/DELETE）。
// ---------------------------------------------------------------------------

// BaselineSnapshotSchemaVersion 快照结构版本（未来字段演进时递增）。
// v2（PE-22）：项目快照增冻结预算（budgetAmount/budgetCurrency）；
// v1 旧快照解码容忍（缺失字段为零值），diff 中 budget.frozen=false 不参与变更判定。
const BaselineSnapshotSchemaVersion = 2

// BaselineCreateRequest 冻结基线请求。
//
// 业务规则：首版（version=1）reason 可选；后续版本为"计划变更"，reason 必填
// （双轨制：计划/基线变更走项目内变更日志，reason 即变更原因）。
type BaselineCreateRequest struct {
	Reason string `json:"reason"`
}

// BaselineProjectSnapshot 项目计划快照（范围/计划日期/进度）。
type BaselineProjectSnapshot struct {
	Code      string     `json:"code"`
	Name      string     `json:"name"`
	Status    string     `json:"status"`
	Progress  int        `json:"progress"`
	StartDate *time.Time `json:"startDate,omitempty"`
	EndDate   *time.Time `json:"endDate,omitempty"`
	// PE-22（D-34）：冻结预算（v2 起；v1 快照缺省）。
	BudgetAmount   *float64 `json:"budgetAmount,omitempty"`
	BudgetCurrency *string  `json:"budgetCurrency,omitempty"`
}

// BaselineTaskSnapshot 任务计划快照。
type BaselineTaskSnapshot struct {
	ID         int        `json:"id"`
	Name       string     `json:"name"`
	Status     string     `json:"status"`
	Priority   string     `json:"priority"`
	AssigneeID int        `json:"assigneeId,omitempty"`
	PlanStart  *time.Time `json:"planStart,omitempty"`
	PlanEnd    *time.Time `json:"planEnd,omitempty"`
	Progress   int        `json:"progress"`
}

// BaselineMilestoneSnapshot 里程碑快照。
type BaselineMilestoneSnapshot struct {
	ID        int        `json:"id"`
	Name      string     `json:"name"`
	Status    string     `json:"status"`
	DueDate   time.Time  `json:"dueDate"`
	OwnerID   int        `json:"ownerId,omitempty"`
	ReachedAt *time.Time `json:"reachedAt,omitempty"`
}

// BaselineSnapshot 快照根（project_baselines.snapshot JSONB 结构）。
type BaselineSnapshot struct {
	SchemaVersion int                         `json:"schemaVersion"`
	FrozenAt      time.Time                   `json:"frozenAt"`
	Project       BaselineProjectSnapshot     `json:"project"`
	Tasks         []BaselineTaskSnapshot      `json:"tasks"`
	Milestones    []BaselineMilestoneSnapshot `json:"milestones"`
}

// newBaselineSnapshot 由当前项目状态构造快照（冻结动作）。
func newBaselineSnapshot(now time.Time, p *ent.Project, tasks []*ent.ProjectTask, milestones []*ent.ProjectMilestone) *BaselineSnapshot {
	snap := &BaselineSnapshot{
		SchemaVersion: BaselineSnapshotSchemaVersion,
		FrozenAt:      now,
		Project: BaselineProjectSnapshot{
			Code:      p.Code,
			Name:      p.Name,
			Status:    p.Status,
			Progress:  p.Progress,
			StartDate: optionalTimePtr(p.StartDate),
			EndDate:   optionalTimePtr(p.EndDate),
			// PE-22：NULL/<=0 视为未设置预算（快照省略字段）。
			BudgetAmount:   positiveFloatPtr(p.BudgetAmount),
			BudgetCurrency: nonEmptyStringPtr(p.BudgetCurrency),
		},
		Tasks:      make([]BaselineTaskSnapshot, 0, len(tasks)),
		Milestones: make([]BaselineMilestoneSnapshot, 0, len(milestones)),
	}
	for _, t := range tasks {
		snap.Tasks = append(snap.Tasks, BaselineTaskSnapshot{
			ID:         t.ID,
			Name:       t.Name,
			Status:     t.Status,
			Priority:   t.Priority,
			AssigneeID: t.AssigneeID,
			PlanStart:  optionalTimePtr(t.PlanStart),
			PlanEnd:    optionalTimePtr(t.PlanEnd),
			Progress:   t.Progress,
		})
	}
	for _, m := range milestones {
		snap.Milestones = append(snap.Milestones, BaselineMilestoneSnapshot{
			ID:        m.ID,
			Name:      m.Name,
			Status:    m.Status,
			DueDate:   m.DueDate,
			OwnerID:   m.OwnerID,
			ReachedAt: m.ReachedAt,
		})
	}
	return snap
}

// optionalTimePtr 零值时间 → nil（ent optional 非 Nillable 字段以零值表示未设置）。
func optionalTimePtr(v time.Time) *time.Time {
	if v.IsZero() {
		return nil
	}
	t := v
	return &t
}

// positiveFloatPtr 预算金额：nil/<=0 → nil（未设置）。
func positiveFloatPtr(v *float64) *float64 {
	if v == nil || *v <= 0 {
		return nil
	}
	f := *v
	return &f
}

// nonEmptyStringPtr 币种：nil/空串 → nil。
func nonEmptyStringPtr(v *string) *string {
	if v == nil || *v == "" {
		return nil
	}
	s := *v
	return &s
}

// snapshotToMap 快照 → JSONB map。
func snapshotToMap(s *BaselineSnapshot) (map[string]any, error) {
	raw, err := json.Marshal(s)
	if err != nil {
		return nil, err
	}
	var m map[string]any
	if err := json.Unmarshal(raw, &m); err != nil {
		return nil, err
	}
	return m, nil
}

// snapshotFromMap JSONB map → 快照。
func snapshotFromMap(m map[string]any) (*BaselineSnapshot, error) {
	raw, err := json.Marshal(m)
	if err != nil {
		return nil, err
	}
	var s BaselineSnapshot
	if err := json.Unmarshal(raw, &s); err != nil {
		return nil, err
	}
	return &s, nil
}

// ProjectBaselineResponse 基线响应（类型名供领域别名 projectBaselines）。
type ProjectBaselineResponse struct {
	ID        int               `json:"id"`
	ProjectID int               `json:"projectId"`
	Version   int               `json:"version"`
	Reason    string            `json:"reason,omitempty"`
	CreatedBy int               `json:"createdBy,omitempty"`
	CreatedAt time.Time         `json:"createdAt"`
	Snapshot  *BaselineSnapshot `json:"snapshot,omitempty"`
}

// ToBaselineResponse ent → 响应（快照 JSONB 解码；解码失败时省略快照本体）。
func ToBaselineResponse(b *ent.ProjectBaseline) *ProjectBaselineResponse {
	if b == nil {
		return nil
	}
	resp := &ProjectBaselineResponse{
		ID:        b.ID,
		ProjectID: b.ProjectID,
		Version:   b.Version,
		Reason:    b.Reason,
		CreatedBy: b.CreatedBy,
		CreatedAt: b.CreatedAt,
	}
	if snap, err := snapshotFromMap(b.Snapshot); err == nil {
		resp.Snapshot = snap
	}
	return resp
}

// ToBaselineResponseList 批量映射。
func ToBaselineResponseList(items []*ent.ProjectBaseline) []*ProjectBaselineResponse {
	out := make([]*ProjectBaselineResponse, 0, len(items))
	for _, it := range items {
		out = append(out, ToBaselineResponse(it))
	}
	return out
}

// ---- 偏差（diff）结构 ----

// 偏差类型。
const (
	BaselineChangeAdded     = "added"
	BaselineChangeRemoved   = "removed"
	BaselineChangeChanged   = "changed"
	BaselineChangeUnchanged = "unchanged"
)

// BaselineDiffSummary 偏差汇总。
type BaselineDiffSummary struct {
	Added     int `json:"added"`
	Removed   int `json:"removed"`
	Changed   int `json:"changed"`
	Unchanged int `json:"unchanged"`
}

// BaselineTaskDeviation 任务偏差（基线 vs 当前）。
type BaselineTaskDeviation struct {
	TaskID           int    `json:"taskId"`
	Name             string `json:"name"`
	ChangeType       string `json:"changeType"`
	BaselineStatus   string `json:"baselineStatus,omitempty"`
	CurrentStatus    string `json:"currentStatus,omitempty"`
	StatusChanged    bool   `json:"statusChanged"`
	ProgressDelta    int    `json:"progressDelta"`
	PlanEndShiftDays *int   `json:"planEndShiftDays,omitempty"`
}

// BaselineMilestoneDeviation 里程碑偏差（基线 vs 当前）。
type BaselineMilestoneDeviation struct {
	MilestoneID      int    `json:"milestoneId"`
	Name             string `json:"name"`
	ChangeType       string `json:"changeType"`
	BaselineStatus   string `json:"baselineStatus,omitempty"`
	CurrentStatus    string `json:"currentStatus,omitempty"`
	StatusChanged    bool   `json:"statusChanged"`
	DueDateShiftDays *int   `json:"dueDateShiftDays,omitempty"`
}

// BaselineDiffResponse 最新基线 vs 当前状态偏差（实时计算，不落库）。
type BaselineDiffResponse struct {
	BaselineID       int                          `json:"baselineId"`
	Version          int                          `json:"version"`
	FrozenAt         time.Time                    `json:"frozenAt"`
	CreatedAt        time.Time                    `json:"createdAt"`
	TaskSummary      BaselineDiffSummary          `json:"taskSummary"`
	MilestoneSummary BaselineDiffSummary          `json:"milestoneSummary"`
	TaskChanges      []BaselineTaskDeviation      `json:"taskChanges"`
	MilestoneChanges []BaselineMilestoneDeviation `json:"milestoneChanges"`
	// PE-22（D-34）：项目级预算偏差（v1 快照 frozen=false，不参与变更判定）。
	Budget *BaselineBudgetDeviation `json:"budget,omitempty"`
}

// BaselineBudgetDeviation 项目级预算偏差（基线 vs 当前）。
type BaselineBudgetDeviation struct {
	Frozen          bool     `json:"frozen"`
	BaseAmount      *float64 `json:"baseAmount,omitempty"`
	BaseCurrency    *string  `json:"baseCurrency,omitempty"`
	CurrentAmount   *float64 `json:"currentAmount,omitempty"`
	CurrentCurrency *string  `json:"currentCurrency,omitempty"`
	Changed         bool     `json:"changed"`
}
