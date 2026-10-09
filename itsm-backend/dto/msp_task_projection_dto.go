package dto

import "time"

// 跨租户单据流转 P2-②：任务投影契约（§7.3 / §8.5）。
//
// 任务行始终属客户租户（owner）；provider 侧通过投影看到"待我处理"（不复制任务行）。
// 授权 = Allocation ∩ 客户租户 RBAC（task:read/update，C2 基线）∩ 分配规则。

// MSPTaskItem 投影任务条目。
type MSPTaskItem struct {
	TaskID               string                   `json:"taskId"`
	TaskName             string                   `json:"taskName"`
	TaskDefinitionKey    string                   `json:"taskDefinitionKey"`
	ProcessDefinitionKey string                   `json:"processDefinitionKey"`
	ProcessInstanceID    int                      `json:"processInstanceId"`
	Status               string                   `json:"status"`
	Priority             string                   `json:"priority"`
	Assignee             string                   `json:"assignee,omitempty"`
	DueDate              *time.Time               `json:"dueDate,omitempty"`
	CreatedTime          time.Time                `json:"createdTime"`
	CustomerTenantID     int                      `json:"customerTenantId"`
	CustomerTenantName   string                   `json:"customerTenantName,omitempty"`
	// Source 投影来源：assigned=显式指派/候选给我；queue=服务商待处理池（保留标记 msp_provider）。
	Source         string                   `json:"source"`
	AllowedActions []WorkbenchAllowedAction `json:"allowedActions"`
}

// MSPTaskListResponse 投影列表响应。
type MSPTaskListResponse struct {
	Items []MSPTaskItem `json:"items"`
	Total int           `json:"total"`
}

// MSPTaskQuery 投影列表查询。
type MSPTaskQuery struct {
	CustomerTenantIDs []int `json:"customerTenantIds"`
	Limit             int   `json:"limit"`
}

// MSPTaskClaimRequest 认领请求（可选声明客户租户；不在允许集合 → 403，防枚举）。
type MSPTaskClaimRequest struct {
	CustomerTenantID int `json:"customerTenantId,omitempty"`
}

// MSPTaskCompleteRequest 完成请求（variables 透传流程变量，可为空）。
type MSPTaskCompleteRequest struct {
	CustomerTenantID int                    `json:"customerTenantId,omitempty"`
	Variables        map[string]interface{} `json:"variables,omitempty"`
}
