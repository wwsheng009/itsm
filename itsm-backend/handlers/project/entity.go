// Package project 域切片：项目管理（P0）。
//
// 权威定义：docs/project-management/plan/p0-api-and-pages.md、p0-integration-contracts.md。
// 本包遵循 vertical slice 约定：entity（本文件）/ repository / service / handler。
package project

import (
	"time"

	"itsm-backend/ent"
)

// ---------------------------------------------------------------------------
// 状态机（P0；01 §3 / API 文档 §1.1）
// ---------------------------------------------------------------------------

const (
	StatusPlanning  = "planning"
	StatusActive    = "active"
	StatusSuspended = "suspended"
	StatusCompleted = "completed"
	StatusCancelled = "cancelled"
)

const (
	ActionActivate = "activate"
	ActionSuspend  = "suspend"
	ActionResume   = "resume"
	ActionComplete = "complete"
	ActionCancel   = "cancel"
)

// actionTransitions：动作 → 允许的来源状态 → 目标状态。
var actionTransitions = map[string]map[string]string{
	ActionActivate: {StatusPlanning: StatusActive},
	ActionSuspend:  {StatusActive: StatusSuspended},
	ActionResume:   {StatusSuspended: StatusActive},
	ActionComplete: {StatusActive: StatusCompleted, StatusSuspended: StatusCompleted},
	ActionCancel:   {StatusPlanning: StatusCancelled, StatusActive: StatusCancelled, StatusSuspended: StatusCancelled},
}

// 项目类型与健康度词表（P0）。
var (
	validTypes   = map[string]bool{"internal": true, "improvement": true, "delivery": true}
	validHealths = map[string]bool{"green": true, "yellow": true, "red": true}
)

// normalizeStatus：存量自由值兼容（读侧）。空值按 active 处理；其余保持原样，
// 非法存量值在流转时按"非法流转"拒绝（fail-closed）。
func normalizeStatus(s string) string {
	if s == "" {
		return StatusActive
	}
	return s
}

// TaskOpen：任务是否未终结（结项门禁口径）。
func taskIsOpen(status string) bool {
	return status != "done" && status != "cancelled"
}

// terminalStatuses：单据终态集合（结项门禁；与 MSP 写面口径一致，见集成契约 §3.3）。
// 注意：change 的 completed/failed 故意不视为终态——结项要求"已关闭"而非"已完成"。
var terminalStatuses = map[string]map[string]bool{
	"ticket":          {"closed": true, "cancelled": true},
	"incident":        {"closed": true, "cancelled": true},
	"problem":         {"closed": true, "cancelled": true},
	"change":          {"closed": true, "cancelled": true, "rejected": true, "rolled_back": true},
	"service_request": {"completed": true, "cancelled": true, "rejected": true},
}

func isTerminal(domain, status string) bool {
	set, ok := terminalStatuses[domain]
	if !ok {
		return false
	}
	return set[status]
}

// ---------------------------------------------------------------------------
// 请求 DTO（P0）
// ---------------------------------------------------------------------------

// CreateRequest 创建项目（body）。code 缺省时自动生成 PRJ-YYYYMMDD-XXXX。
type CreateRequest struct {
	TenantID     int        `json:"-"`
	Name         string     `json:"name" binding:"required"`
	Code         string     `json:"code"`
	Description  string     `json:"description"`
	Type         string     `json:"type"`
	ManagerID    *int       `json:"managerId"`
	DepartmentID *int       `json:"departmentId"`
	OwnerTeamID  *int       `json:"ownerTeamId"`
	StartDate    *time.Time `json:"startDate"`
	EndDate      *time.Time `json:"endDate"`
	// P2（PE-22）轻量预算：成对提供（只给其一 → 400），见 plan/p2-budget-hours-design.md D-33。
	BudgetAmount   *float64 `json:"budgetAmount"`
	BudgetCurrency *string  `json:"budgetCurrency"`
}

// UpdateRequest 基础字段更新（不含状态；nil = 不修改）。
type UpdateRequest struct {
	Name         *string    `json:"name"`
	Description  *string    `json:"description"`
	Type         *string    `json:"type"`
	Health       *string    `json:"health"`
	Progress     *int       `json:"progress"`
	ManagerID    *int       `json:"managerId"`
	DepartmentID *int       `json:"departmentId"`
	OwnerTeamID  *int       `json:"ownerTeamId"`
	StartDate    *time.Time `json:"startDate"`
	EndDate      *time.Time `json:"endDate"`
	Version      *int       `json:"version"`
	// P2（PE-22）轻量预算：nil=不修改；成对提供；amount<=0 = 清空（同写同清）。
	BudgetAmount   *float64 `json:"budgetAmount"`
	BudgetCurrency *string  `json:"budgetCurrency"`
}

// StatusRequest 状态流转（专用端点；complete 走结项门禁）。
type StatusRequest struct {
	Action  string `json:"action" binding:"required"`
	Reason  string `json:"reason"`
	Version *int   `json:"version"`
}

// ---------------------------------------------------------------------------
// 响应 DTO（类型名 ProjectResponse 供 common.inferDomainAlias 映射到领域别名 projects）
// ---------------------------------------------------------------------------

// ProjectResponse 项目响应。
type ProjectResponse struct {
	ID           int    `json:"id"`
	Name         string `json:"name"`
	Code         string `json:"code"`
	Description  string `json:"description"`
	Type         string `json:"type"`
	Status       string `json:"status"`
	Health       string `json:"health"`
	Progress     int    `json:"progress"`
	ManagerID    *int   `json:"managerId,omitempty"`
	DepartmentID *int   `json:"departmentId,omitempty"`
	OwnerTeamID  *int   `json:"ownerTeamId,omitempty"`
	// PE-17 归口团队展示（读取侧降级：团队缺失/软删 → ownerTeamDeleted=true，UI 提示重新归口）。
	OwnerTeamName    string     `json:"ownerTeamName,omitempty"`
	OwnerTeamDeleted bool       `json:"ownerTeamDeleted,omitempty"`
	ParentID         *int       `json:"parentProjectId,omitempty"`
	StartDate        *time.Time `json:"startDate,omitempty"`
	EndDate          *time.Time `json:"endDate,omitempty"`
	ActualStart      *time.Time `json:"actualStart,omitempty"`
	ActualEnd        *time.Time `json:"actualEnd,omitempty"`
	ClosedAt         *time.Time `json:"closedAt,omitempty"`
	// P1 模板应用（PE-11）：非空 = 该项目已应用模板（一项目一模板，重复应用 409）。
	TemplateID        *int       `json:"templateId,omitempty"`
	TemplateAppliedAt *time.Time `json:"templateAppliedAt,omitempty"`
	// P2（PE-22）轻量预算：未设置时省略（NULL 语义）。
	BudgetAmount   *float64  `json:"budgetAmount,omitempty"`
	BudgetCurrency *string   `json:"budgetCurrency,omitempty"`
	Version        int       `json:"version"`
	TenantID       int       `json:"tenantId"`
	CreatedAt      time.Time `json:"createdAt"`
	UpdatedAt      time.Time `json:"updatedAt"`
}

// Blocker 结项/删除门禁阻塞项。
type Blocker struct {
	Type   string `json:"type"`
	ID     int    `json:"id"`
	Number string `json:"number,omitempty"`
	Title  string `json:"title,omitempty"`
	Status string `json:"status"`
}

// ToResponse 映射 ent.Project → 响应（保留 NULL 语义，避免零值伪造）。
func ToResponse(p *ent.Project) *ProjectResponse {
	if p == nil {
		return nil
	}
	resp := &ProjectResponse{
		ID:          p.ID,
		Name:        p.Name,
		Code:        p.Code,
		Description: p.Description,
		Type:        p.Type,
		Status:      normalizeStatus(p.Status),
		Health:      p.Health,
		Progress:    p.Progress,
		Version:     p.Version,
		TenantID:    p.TenantID,
		CreatedAt:   p.CreatedAt,
		UpdatedAt:   p.UpdatedAt,
	}
	if p.ManagerID > 0 {
		id := p.ManagerID
		resp.ManagerID = &id
	}
	if p.DepartmentID > 0 {
		id := p.DepartmentID
		resp.DepartmentID = &id
	}
	if p.OwnerTeamID > 0 {
		id := p.OwnerTeamID
		resp.OwnerTeamID = &id
	}
	if p.ParentProjectID > 0 {
		id := p.ParentProjectID
		resp.ParentID = &id
	}
	if !p.StartDate.IsZero() {
		t := p.StartDate
		resp.StartDate = &t
	}
	if !p.EndDate.IsZero() {
		t := p.EndDate
		resp.EndDate = &t
	}
	if !p.ActualStart.IsZero() {
		t := p.ActualStart
		resp.ActualStart = &t
	}
	if !p.ActualEnd.IsZero() {
		t := p.ActualEnd
		resp.ActualEnd = &t
	}
	if !p.ClosedAt.IsZero() {
		t := p.ClosedAt
		resp.ClosedAt = &t
	}
	if p.TemplateID > 0 {
		id := p.TemplateID
		resp.TemplateID = &id
	}
	if !p.TemplateAppliedAt.IsZero() {
		t := p.TemplateAppliedAt
		resp.TemplateAppliedAt = &t
	}
	// P2（PE-22）轻量预算：NULL/<=0 视为未设置（省略字段）。
	if p.BudgetAmount != nil && *p.BudgetAmount > 0 {
		v := *p.BudgetAmount
		resp.BudgetAmount = &v
	}
	if p.BudgetCurrency != nil && *p.BudgetCurrency != "" {
		v := *p.BudgetCurrency
		resp.BudgetCurrency = &v
	}
	return resp
}

// ToResponseList 批量映射。
func ToResponseList(items []*ent.Project) []*ProjectResponse {
	out := make([]*ProjectResponse, 0, len(items))
	for _, it := range items {
		out = append(out, ToResponse(it))
	}
	return out
}

// ToResponseWithOwnerTeams 单项目响应 + 归口团队富化（PE-17）。
func ToResponseWithOwnerTeams(p *ent.Project, refs map[int]TeamRef) *ProjectResponse {
	resp := ToResponse(p)
	applyOwnerTeam(resp, refs)
	return resp
}

// ToResponseListWithOwnerTeams 批量响应 + 归口团队富化（列表一次批量查询后调用）。
func ToResponseListWithOwnerTeams(items []*ent.Project, refs map[int]TeamRef) []*ProjectResponse {
	out := make([]*ProjectResponse, 0, len(items))
	for _, it := range items {
		resp := ToResponse(it)
		applyOwnerTeam(resp, refs)
		out = append(out, resp)
	}
	return out
}

// applyOwnerTeam 填充归口展示字段：存在 → 名称；缺失/软删 → deleted 标记（UI 降级提示）。
func applyOwnerTeam(resp *ProjectResponse, refs map[int]TeamRef) {
	if resp == nil || resp.OwnerTeamID == nil {
		return
	}
	ref, ok := refs[*resp.OwnerTeamID]
	if !ok {
		resp.OwnerTeamDeleted = true
		return
	}
	resp.OwnerTeamName = ref.Name
	resp.OwnerTeamDeleted = ref.Deleted
}
