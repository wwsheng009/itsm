package project

import (
	"time"

	"itsm-backend/ent"
)

// ---------------------------------------------------------------------------
// 任务 DTO 与状态机（PE-4；权威定义：docs/project-management/plan/p0-api-and-pages.md §1.3、02 §4.2）
// 命名消歧：ProjectTask（本模块）≠ ProcessTask（BPMN）≠ ProvisioningTask（开通任务）。
// ---------------------------------------------------------------------------

const (
	TaskStatusTodo       = "todo"
	TaskStatusInProgress = "in_progress"
	TaskStatusBlocked    = "blocked"
	TaskStatusDone       = "done"
	TaskStatusCancelled  = "cancelled"
)

const (
	TaskActionStart    = "start"
	TaskActionBlock    = "block"
	TaskActionUnblock  = "unblock"
	TaskActionComplete = "complete"
	TaskActionCancel   = "cancel"
)

// taskActionTransitions：动作 → 允许来源状态 → 目标状态。
var taskActionTransitions = map[string]map[string]string{
	TaskActionStart:    {TaskStatusTodo: TaskStatusInProgress},
	TaskActionBlock:    {TaskStatusInProgress: TaskStatusBlocked},
	TaskActionUnblock:  {TaskStatusBlocked: TaskStatusInProgress},
	TaskActionComplete: {TaskStatusInProgress: TaskStatusDone, TaskStatusBlocked: TaskStatusDone},
	TaskActionCancel:   {TaskStatusTodo: TaskStatusCancelled, TaskStatusInProgress: TaskStatusCancelled, TaskStatusBlocked: TaskStatusCancelled},
}

// 任务优先级词表（对齐 common/validator.go：low|medium|high|critical）。
var validTaskPriorities = map[string]bool{"low": true, "medium": true, "high": true, "critical": true}

func taskIsTerminal(status string) bool {
	return status == TaskStatusDone || status == TaskStatusCancelled
}

// WBS 深度上限（含根层）。
const maxTaskDepth = 3

// TaskCreateRequest 创建任务。
type TaskCreateRequest struct {
	ParentTaskID  int        `json:"parentTaskId"`
	Name          string     `json:"name" binding:"required"`
	Description   string     `json:"description"`
	AssigneeID    int        `json:"assigneeId"`
	Priority      string     `json:"priority"`
	PlanStart     *time.Time `json:"planStart"`
	PlanEnd       *time.Time `json:"planEnd"`
	EstimateHours *float64   `json:"estimateHours"`
	SortOrder     int        `json:"sortOrder"`
}

// TaskUpdateRequest 更新任务（nil = 不修改）。
type TaskUpdateRequest struct {
	Name          *string    `json:"name"`
	Description   *string    `json:"description"`
	AssigneeID    *int       `json:"assigneeId"`
	Priority      *string    `json:"priority"`
	PlanStart     *time.Time `json:"planStart"`
	PlanEnd       *time.Time `json:"planEnd"`
	ActualStart   *time.Time `json:"actualStart"`
	ActualEnd     *time.Time `json:"actualEnd"`
	Progress      *int       `json:"progress"`
	EstimateHours *float64   `json:"estimateHours"`
	ActualHours   *float64   `json:"actualHours"`
	SortOrder     *int       `json:"sortOrder"`
	Version       *int       `json:"version"`
}

// TaskStatusRequest 任务状态流转。
type TaskStatusRequest struct {
	Action  string `json:"action" binding:"required"`
	Version *int   `json:"version"`
}

// ProjectTaskResponse 任务响应（tree 视图携带 children）。
type ProjectTaskResponse struct {
	ID            int                    `json:"id"`
	ProjectID     int                    `json:"projectId"`
	ParentTaskID  *int                   `json:"parentTaskId,omitempty"`
	Name          string                 `json:"name"`
	Description   string                 `json:"description"`
	AssigneeID    *int                   `json:"assigneeId,omitempty"`
	Status        string                 `json:"status"`
	Priority      string                 `json:"priority"`
	PlanStart     *time.Time             `json:"planStart,omitempty"`
	PlanEnd       *time.Time             `json:"planEnd,omitempty"`
	ActualStart   *time.Time             `json:"actualStart,omitempty"`
	ActualEnd     *time.Time             `json:"actualEnd,omitempty"`
	Progress      int                    `json:"progress"`
	EstimateHours *float64               `json:"estimateHours,omitempty"`
	ActualHours   *float64               `json:"actualHours,omitempty"`
	LinkedTicket  *int                   `json:"linkedTicketId,omitempty"`
	SortOrder     int                    `json:"sortOrder"`
	Version       int                    `json:"version"`
	CreatedAt     time.Time              `json:"createdAt"`
	UpdatedAt     time.Time              `json:"updatedAt"`
	Children      []*ProjectTaskResponse `json:"children,omitempty"`
}

// MyTaskResponse 我的任务（含项目名，供个人工作台聚合展示）。
type MyTaskResponse struct {
	*ProjectTaskResponse
	ProjectName string `json:"projectName"`
}

// ToTaskResponse ent → 响应（NULL 语义保留）。
func ToTaskResponse(t *ent.ProjectTask) *ProjectTaskResponse {
	if t == nil {
		return nil
	}
	resp := &ProjectTaskResponse{
		ID:          t.ID,
		ProjectID:   t.ProjectID,
		Name:        t.Name,
		Description: t.Description,
		Status:      t.Status,
		Priority:    t.Priority,
		Progress:    t.Progress,
		SortOrder:   t.SortOrder,
		Version:     t.Version,
		CreatedAt:   t.CreatedAt,
		UpdatedAt:   t.UpdatedAt,
	}
	// ent Optional 非 Nillable 的 float 读侧为 0；0 视为"未设置"以保留 NULL 语义。
	if t.EstimateHours != 0 {
		v := t.EstimateHours
		resp.EstimateHours = &v
	}
	if t.ActualHours != 0 {
		v := t.ActualHours
		resp.ActualHours = &v
	}
	if t.ParentTaskID > 0 {
		id := t.ParentTaskID
		resp.ParentTaskID = &id
	}
	if t.AssigneeID > 0 {
		id := t.AssigneeID
		resp.AssigneeID = &id
	}
	if t.LinkedTicketID > 0 {
		id := t.LinkedTicketID
		resp.LinkedTicket = &id
	}
	if !t.PlanStart.IsZero() {
		v := t.PlanStart
		resp.PlanStart = &v
	}
	if !t.PlanEnd.IsZero() {
		v := t.PlanEnd
		resp.PlanEnd = &v
	}
	if !t.ActualStart.IsZero() {
		v := t.ActualStart
		resp.ActualStart = &v
	}
	if !t.ActualEnd.IsZero() {
		v := t.ActualEnd
		resp.ActualEnd = &v
	}
	return resp
}

// ToTaskResponseList 批量映射（flat 视图）。
func ToTaskResponseList(items []*ent.ProjectTask) []*ProjectTaskResponse {
	out := make([]*ProjectTaskResponse, 0, len(items))
	for _, it := range items {
		out = append(out, ToTaskResponse(it))
	}
	return out
}

// BuildTaskTree 平铺 → WBS 树（孤儿节点按根处理，保序入 children）。
func BuildTaskTree(items []*ent.ProjectTask) []*ProjectTaskResponse {
	nodes := make(map[int]*ProjectTaskResponse, len(items))
	order := make([]int, 0, len(items))
	for _, it := range items {
		node := ToTaskResponse(it)
		if node.Children == nil {
			node.Children = []*ProjectTaskResponse{}
		}
		nodes[it.ID] = node
		order = append(order, it.ID)
	}
	roots := make([]*ProjectTaskResponse, 0, len(items))
	for _, id := range order {
		node := nodes[id]
		if node.ParentTaskID != nil {
			if parent, ok := nodes[*node.ParentTaskID]; ok {
				parent.Children = append(parent.Children, node)
				continue
			}
		}
		roots = append(roots, node)
	}
	return roots
}
