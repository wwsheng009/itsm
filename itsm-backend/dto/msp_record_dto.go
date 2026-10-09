// msp_record_dto.go：跨租户单据读面 DTO（工作台 ②；跨租户单据流转 P2-③ / IP-P2-12）。
//
// 三类单据（变更/问题/服务请求）共用同一投影契约：
//   - 条目 = 通用字段 + allowedActions[]（XF5：UI 唯一契约，前端禁止推断）；
//   - 详情 = 条目字段 + 三类并集字段（omitempty 按类型下发）。
package dto

import "time"

// MSPRecordQuery 聚合读查询（customerTenantIds 仅作筛选，授权集合来自服务端）。
type MSPRecordQuery struct {
	CustomerTenantIDs []int
	Status            string
	Q                 string
	Limit             int
	Cursor            string
}

// MSPRecordItem 列表条目（三类共用投影）。
type MSPRecordItem struct {
	RecordType       string                   `json:"recordType"` // change | problem | service_request
	ID               int                      `json:"id"`
	CustomerTenantID int                      `json:"customerTenantId"`
	CustomerName     string                   `json:"customerName,omitempty"`
	Number           string                   `json:"number"`
	Title            string                   `json:"title"`
	Status           string                   `json:"status"`
	Priority         string                   `json:"priority,omitempty"`
	AssigneeID       int                      `json:"assigneeId,omitempty"`
	AssigneeName     string                   `json:"assigneeName,omitempty"`
	RequesterID      int                      `json:"requesterId,omitempty"`
	RequesterName    string                   `json:"requesterName,omitempty"`
	UpdatedAt        time.Time                `json:"updatedAt"`
	AllowedActions   []WorkbenchAllowedAction `json:"allowedActions"`
}

// MSPRecordListResponse 聚合读响应（逐租户查询 + 内存合并；nextCursor 为逐租户位置编码）。
type MSPRecordListResponse struct {
	Items      []MSPRecordItem `json:"items"`
	NextCursor string          `json:"nextCursor,omitempty"`
	Total      int             `json:"total"`
}

// MSPRecordDetailResponse 单条详情（读面；写面见 IP-P2-13）。
//
// 字段为三类并集：变更（type/riskLevel/impactScope/justification/planned*）、
// 问题（category/rootCause/workaround/resolution/impact/resolvedAt/closedAt）、
// 服务请求（catalogId/currentLevel/totalLevels/currentApprover/approverComment/approvedAt/startedAt/completedAt/completionNote）。
type MSPRecordDetailResponse struct {
	MSPRecordItem
	Description string    `json:"description,omitempty"`
	CreatedAt   time.Time `json:"createdAt"`

	// 变更（change）
	Type             string     `json:"type,omitempty"`
	RiskLevel        string     `json:"riskLevel,omitempty"`
	ImpactScope      string     `json:"impactScope,omitempty"`
	Justification    string     `json:"justification,omitempty"`
	PlannedStartDate *time.Time `json:"plannedStartDate,omitempty"`
	PlannedEndDate   *time.Time `json:"plannedEndDate,omitempty"`

	// 问题（problem）
	Category   string     `json:"category,omitempty"`
	RootCause  string     `json:"rootCause,omitempty"`
	Workaround string     `json:"workaround,omitempty"`
	Resolution string     `json:"resolution,omitempty"`
	Impact     string     `json:"impact,omitempty"`
	ResolvedAt *time.Time `json:"resolvedAt,omitempty"`
	ClosedAt   *time.Time `json:"closedAt,omitempty"`

	// 服务请求（service_request）
	CatalogID       int        `json:"catalogId,omitempty"`
	CurrentLevel    int        `json:"currentLevel,omitempty"`
	TotalLevels     int        `json:"totalLevels,omitempty"`
	CurrentApprover string     `json:"currentApprover,omitempty"`
	ApproverComment string     `json:"approverComment,omitempty"`
	ApprovedAt      *time.Time `json:"approvedAt,omitempty"`
	StartedAt       *time.Time `json:"startedAt,omitempty"`
	CompletedAt     *time.Time `json:"completedAt,omitempty"`
	CompletionNote  string     `json:"completionNote,omitempty"`
}

// UpdateMSPProblemStatusRequest 跨租户问题状态推进请求（P2-③ 写面首批 / IP-P2-13）。
type UpdateMSPProblemStatusRequest struct {
	CustomerTenantID int    `json:"customerTenantId"`
	Status           string `json:"status"`
	Resolution       string `json:"resolution"`
}

// UpdateMSPChangeStatusRequest 跨租户变更非审批流转请求（P2-③ 写面扩展 / IP-P2-15）。
// status ∈ scheduled|in_progress|completed|closed|cancelled（approve/reject/rollback 不在 MSP 面）。
type UpdateMSPChangeStatusRequest struct {
	CustomerTenantID int    `json:"customerTenantId"`
	Status           string `json:"status"`
	Comment          string `json:"comment"`
}

// UpdateMSPRequestStatusRequest 跨租户服务请求履约状态请求（P2-③ 写面扩展 / IP-P2-15）。
// status ∈ provisioning|delivered|failed（approval 保留客户治理面）。
type UpdateMSPRequestStatusRequest struct {
	CustomerTenantID int    `json:"customerTenantId"`
	Status           string `json:"status"`
}
