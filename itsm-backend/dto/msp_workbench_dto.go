package dto

import "time"

// WorkbenchTicketQuery 工作台跨客户工单列表查询（IP-P0-7；§3.0-D）。
type WorkbenchTicketQuery struct {
	CustomerTenantIDs []int      // 已解析并校验的目标租户集合（"all" 已展开为服务端允许集合）
	Status            string     // 可选：精确状态
	Priority          string     // 可选：精确优先级
	AssigneeID        int        // 可选：处理人
	Q                 string     // 可选：编号/标题模糊
	UpdatedAfter      *time.Time // 可选：更新时间下界
	Sort              string     // updated（默认）| sla
	Cursor            string     // opaque base64url 游标
	Limit             int        // 默认 50，上限 200
}

// WorkbenchAllowedAction 条目级 allowedActions 元素（前端仅按该数组渲染）。
type WorkbenchAllowedAction struct {
	Action     string `json:"action"`
	Allowed    bool   `json:"allowed"`
	ReasonCode string `json:"reasonCode,omitempty"`
	ReasonText string `json:"reasonText,omitempty"`
}

// WorkbenchTicketItem 工作台列表项。
type WorkbenchTicketItem struct {
	ID               int                      `json:"id"`
	CustomerTenantID int                      `json:"customerTenantId"`
	CustomerName     string                   `json:"customerName"`
	TicketNumber     string                   `json:"ticketNumber"`
	Title            string                   `json:"title"`
	Status           string                   `json:"status"`
	Priority         string                   `json:"priority"`
	AssigneeID       int                      `json:"assigneeId,omitempty"`
	AssigneeName     string                   `json:"assigneeName,omitempty"`
	UpdatedAt        time.Time                `json:"updatedAt"`
	SLADeadline      *time.Time               `json:"slaDeadline,omitempty"`
	AllowedActions   []WorkbenchAllowedAction `json:"allowedActions"`
}

// WorkbenchTicketListResponse 工作台列表响应。
type WorkbenchTicketListResponse struct {
	Items      []WorkbenchTicketItem `json:"items"`
	NextCursor string                `json:"nextCursor,omitempty"`
	Total      int                   `json:"total"`
}

// WorkbenchSummaryCustomer 单客户徽标计数。
type WorkbenchSummaryCustomer struct {
	CustomerTenantID int    `json:"customerTenantId"`
	CustomerName     string `json:"customerName"`
	Open             int    `json:"open"`
	SLARisk          int    `json:"slaRisk"`
	Unassigned       int    `json:"unassigned"`
}

// WorkbenchSummaryResponse 工作台计数徽标响应。
type WorkbenchSummaryResponse struct {
	GeneratedAt time.Time                  `json:"generatedAt"`
	TTLSeconds  int                        `json:"ttlSeconds"`
	Customers   []WorkbenchSummaryCustomer `json:"customers"`
}

// WorkbenchReplyRequest 条目级回复（服务端按单据租户授权 + 审计）。
type WorkbenchReplyRequest struct {
	CustomerTenantID int    `json:"customerTenantId" binding:"required"`
	Content          string `json:"content" binding:"required"`
}

// WorkbenchStatusRequest 条目级改状态（状态机校验）。
type WorkbenchStatusRequest struct {
	CustomerTenantID int    `json:"customerTenantId" binding:"required"`
	Status           string `json:"status" binding:"required"`
}

// WorkbenchBatchRequest 批量操作请求（IP-P1-6）：低危动作 + 条目清单 + 动作载荷。
type WorkbenchBatchRequest struct {
	Action  string                `json:"action" binding:"required"`
	Items   []WorkbenchBatchItem  `json:"items" binding:"required,dive"`
	Payload WorkbenchBatchPayload `json:"payload"`
}

// WorkbenchBatchItem 批量条目（显式声明目标租户，服务端逐条校验资源租户一致）。
type WorkbenchBatchItem struct {
	TicketID         int `json:"ticketId" binding:"required"`
	CustomerTenantID int `json:"customerTenantId" binding:"required"`
}

// WorkbenchBatchPayload 批量动作载荷（按 action 取用对应字段）。
type WorkbenchBatchPayload struct {
	Content    string `json:"content,omitempty"`
	Status     string `json:"status,omitempty"`
	AssigneeID int    `json:"assigneeId,omitempty"`
}

// WorkbenchBatchItemResult 逐条结果（部分失败不影响其余条目）。
type WorkbenchBatchItemResult struct {
	TicketID         int    `json:"ticketId"`
	CustomerTenantID int    `json:"customerTenantId"`
	OK               bool   `json:"ok"`
	ReasonCode       string `json:"reasonCode,omitempty"`
	Message          string `json:"message,omitempty"`
}

// WorkbenchBatchResponse 批量结果（batchId 供整批审计回溯）。
type WorkbenchBatchResponse struct {
	BatchID   string                     `json:"batchId"`
	Succeeded int                        `json:"succeeded"`
	Failed    int                        `json:"failed"`
	Results   []WorkbenchBatchItemResult `json:"results"`
}

// ---------- IP-P2-4a 工作台自定义视图（保存的过滤器组合） ----------

// WorkbenchViewFilter 保存的过滤器组合（与 WorkbenchTicketQuery 对齐；空 customerTenantIds = 全部客户）。
type WorkbenchViewFilter struct {
	CustomerTenantIDs []int  `json:"customerTenantIds"`
	Status            string `json:"status,omitempty"`
	Priority          string `json:"priority,omitempty"`
	AssigneeID        int    `json:"assigneeId,omitempty"`
	Q                 string `json:"q,omitempty"`
	Sort              string `json:"sort,omitempty"`
}

// WorkbenchViewResponse 视图（isOwner=false 表示他人分享，只读）。
type WorkbenchViewResponse struct {
	ID          int                 `json:"id"`
	Name        string              `json:"name"`
	Filters     WorkbenchViewFilter `json:"filters"`
	IsShared    bool                `json:"isShared"`
	IsDefault   bool                `json:"isDefault"`
	IsOwner     bool                `json:"isOwner"`
	OwnerUserID int                 `json:"ownerUserId"`
	CreatedAt   time.Time           `json:"createdAt"`
	UpdatedAt   time.Time           `json:"updatedAt"`
}

// WorkbenchViewCreateRequest 创建视图。
type WorkbenchViewCreateRequest struct {
	Name     string              `json:"name" binding:"required"`
	Filters  WorkbenchViewFilter `json:"filters"`
	IsShared bool                `json:"isShared"`
}

// WorkbenchViewUpdateRequest 更新视图（owner 本人）。
type WorkbenchViewUpdateRequest struct {
	Name     string              `json:"name" binding:"required"`
	Filters  WorkbenchViewFilter `json:"filters"`
	IsShared bool                `json:"isShared"`
}
