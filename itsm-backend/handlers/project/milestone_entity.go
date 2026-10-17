package project

import (
	"time"

	"itsm-backend/ent"
)

// ---------------------------------------------------------------------------
// 里程碑 DTO 与状态机（P1，PE-14；权威定义：plan/p0-data-ddl.md §4.4）
// ---------------------------------------------------------------------------

const (
	MilestoneStatusPlanned = "planned"
	MilestoneStatusReached = "reached"
	MilestoneStatusMissed  = "missed"
)

const (
	MilestoneActionReach = "reach"
	MilestoneActionMiss  = "miss"
)

// milestoneActionTransitions：动作 → 允许来源状态 → 目标状态。
var milestoneActionTransitions = map[string]map[string]string{
	MilestoneActionReach: {
		MilestoneStatusPlanned: MilestoneStatusReached,
		MilestoneStatusMissed:  MilestoneStatusReached,
	},
	MilestoneActionMiss: {
		MilestoneStatusPlanned: MilestoneStatusMissed,
	},
}

// MilestoneCreateRequest 创建里程碑。
type MilestoneCreateRequest struct {
	Name        string     `json:"name" binding:"required"`
	Description string     `json:"description"`
	DueDate     *time.Time `json:"dueDate"`
	OwnerID     int        `json:"ownerId"`
}

// MilestoneUpdateRequest 更新里程碑（nil = 不修改）。
type MilestoneUpdateRequest struct {
	Name        *string    `json:"name"`
	Description *string    `json:"description"`
	DueDate     *time.Time `json:"dueDate"`
	OwnerID     *int       `json:"ownerId"`
}

// MilestoneStatusRequest 里程碑状态流转。
type MilestoneStatusRequest struct {
	Action string `json:"action" binding:"required"`
}

// ProjectMilestoneResponse 里程碑响应（类型名供领域别名 projectMilestones）。
type ProjectMilestoneResponse struct {
	ID          int        `json:"id"`
	ProjectID   int        `json:"projectId"`
	Name        string     `json:"name"`
	Description string     `json:"description"`
	DueDate     time.Time  `json:"dueDate"`
	Status      string     `json:"status"`
	OwnerID     *int       `json:"ownerId,omitempty"`
	ReachedAt   *time.Time `json:"reachedAt,omitempty"`
	CreatedAt   time.Time  `json:"createdAt"`
	UpdatedAt   time.Time  `json:"updatedAt"`
}

// ToMilestoneResponse ent → 响应（NULL 语义保留）。
func ToMilestoneResponse(m *ent.ProjectMilestone) *ProjectMilestoneResponse {
	if m == nil {
		return nil
	}
	resp := &ProjectMilestoneResponse{
		ID:          m.ID,
		ProjectID:   m.ProjectID,
		Name:        m.Name,
		Description: m.Description,
		DueDate:     m.DueDate,
		Status:      m.Status,
		CreatedAt:   m.CreatedAt,
		UpdatedAt:   m.UpdatedAt,
	}
	if m.OwnerID > 0 {
		id := m.OwnerID
		resp.OwnerID = &id
	}
	if m.ReachedAt != nil {
		t := *m.ReachedAt
		resp.ReachedAt = &t
	}
	return resp
}

// ToMilestoneResponseList 批量映射。
func ToMilestoneResponseList(items []*ent.ProjectMilestone) []*ProjectMilestoneResponse {
	out := make([]*ProjectMilestoneResponse, 0, len(items))
	for _, it := range items {
		out = append(out, ToMilestoneResponse(it))
	}
	return out
}

// MyMilestoneResponse 我的里程碑到期（含项目名/编号与逾期信息，供个人工作台聚合；PE-16）。
type MyMilestoneResponse struct {
	*ProjectMilestoneResponse
	ProjectName string `json:"projectName"`
	ProjectCode string `json:"projectCode,omitempty"`
	Overdue     bool   `json:"overdue"`
	DaysLeft    int    `json:"daysLeft"`
}

// ToMyMilestoneResponseList 批量映射（含项目标签与到期相对天数）。
func ToMyMilestoneResponseList(items []*ent.ProjectMilestone, names, codes map[int]string, now time.Time) []MyMilestoneResponse {
	out := make([]MyMilestoneResponse, 0, len(items))
	for _, it := range items {
		out = append(out, MyMilestoneResponse{
			ProjectMilestoneResponse: ToMilestoneResponse(it),
			ProjectName:              names[it.ProjectID],
			ProjectCode:              codes[it.ProjectID],
			Overdue:                  it.DueDate.Before(now),
			DaysLeft:                 int(it.DueDate.Sub(now).Hours() / 24),
		})
	}
	return out
}
