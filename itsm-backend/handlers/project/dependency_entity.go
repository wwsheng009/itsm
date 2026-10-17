package project

import (
	"time"

	"itsm-backend/ent"
)

// ---------------------------------------------------------------------------
// 任务依赖 DTO（P1，PE-14；v1 仅 FS，见 plan/p0-data-ddl.md §4.3）
// ---------------------------------------------------------------------------

// DependencyCreateRequest 添加任务依赖。
type DependencyCreateRequest struct {
	DependsOnTaskID int    `json:"dependsOnTaskId" binding:"required"`
	DepType         string `json:"depType"`
}

// ProjectTaskDependencyResponse 依赖响应。
type ProjectTaskDependencyResponse struct {
	ID              int       `json:"id"`
	TaskID          int       `json:"taskId"`
	DependsOnTaskID int       `json:"dependsOnTaskId"`
	DepType         string    `json:"depType"`
	CreatedAt       time.Time `json:"createdAt"`
}

// ToDependencyResponse ent → 响应。
func ToDependencyResponse(d *ent.ProjectTaskDependency) *ProjectTaskDependencyResponse {
	if d == nil {
		return nil
	}
	return &ProjectTaskDependencyResponse{
		ID:              d.ID,
		TaskID:          d.TaskID,
		DependsOnTaskID: d.DependsOnTaskID,
		DepType:         d.DepType,
		CreatedAt:       d.CreatedAt,
	}
}

// ToDependencyResponseList 批量映射。
func ToDependencyResponseList(items []*ent.ProjectTaskDependency) []*ProjectTaskDependencyResponse {
	out := make([]*ProjectTaskDependencyResponse, 0, len(items))
	for _, it := range items {
		out = append(out, ToDependencyResponse(it))
	}
	return out
}
