package project

import (
	"context"
	"time"

	"itsm-backend/ent"
)

// ---------------------------------------------------------------------------
// 单据关联 DTO（PE-5；权威定义：docs/project-management/plan/p0-integration-contracts.md §3.1/§3.2）
// ---------------------------------------------------------------------------

// 可挂接单据域白名单（同租户）。
var validRecordDomains = map[string]bool{
	"ticket":          true,
	"incident":        true,
	"problem":         true,
	"change":          true,
	"service_request": true,
}

// RecordItem 聚合读条目（跨域归一化）。
type RecordItem struct {
	Type      string    `json:"type"`
	ID        int       `json:"id"`
	Number    string    `json:"number,omitempty"`
	Title     string    `json:"title"`
	Status    string    `json:"status"`
	Priority  string    `json:"priority,omitempty"`
	UpdatedAt time.Time `json:"updatedAt"`
}

// RecordAttachRequest 挂接请求体（DELETE 也接受 query projectId）。
type RecordAttachRequest struct {
	ProjectID int `json:"projectId"`
}

// RecordProjectView 单据所属项目摘要（单据详情页展示"所属项目"；不进项目聚合读）。
type RecordProjectView struct {
	ID     int    `json:"id"`
	Name   string `json:"name"`
	Code   string `json:"code,omitempty"`
	Status string `json:"status"`
}

// RecordProjectResponse 单据→项目反向查询响应；project 为 null 表示未挂接（或项目对当前用户不可见）。
type RecordProjectResponse struct {
	Project *RecordProjectView `json:"project"`
}

// TaskTicketRequest 任务→工单生成请求。
type TaskTicketRequest struct {
	TicketTypeID *int   `json:"ticketTypeId"`
	Title        string `json:"title"`
	Priority     string `json:"priority"`
}

// TicketGenerateInput 生成入参（适配层契约；bootstrap 注入真实工单服务）。
type TicketGenerateInput struct {
	TenantID  int
	ProjectID int
	ActorID   int
	Task      *ent.ProjectTask
	Request   *TaskTicketRequest
}

// TicketGenerator 工单生成端口（由 bootstrap 注入既有工单服务适配器，避免跨域直连）。
type TicketGenerator interface {
	GenerateTicket(ctx context.Context, in TicketGenerateInput) (int, error)
}

// TicketGeneratorFunc 函数适配器。
type TicketGeneratorFunc func(ctx context.Context, in TicketGenerateInput) (int, error)

// GenerateTicket 实现 TicketGenerator。
func (f TicketGeneratorFunc) GenerateTicket(ctx context.Context, in TicketGenerateInput) (int, error) {
	return f(ctx, in)
}
