package dto

import "time"

// DistributeWorkflowTemplateRequest 模板分发请求（跨租户单据流转 P2-①）：
// version 为空 = 使用 provider 最新已发布版本；仅已发布（published）模板可分发。
type DistributeWorkflowTemplateRequest struct {
	Version string `json:"version"`
}

// WorkflowTemplateDistribution 分发记录（copy-in 来源追踪 + 升级提示）。
type WorkflowTemplateDistribution struct {
	ID                      int       `json:"id"`
	CustomerTenantID        int       `json:"customerTenantId"`
	ProviderTenantID        int       `json:"providerTenantId"`
	SourceTemplateKey       string    `json:"sourceTemplateKey"`
	SourceVersion           string    `json:"sourceVersion"`
	CustomerTemplateKey     string    `json:"customerTemplateKey"`
	CustomerTemplateVersion string    `json:"customerTemplateVersion"`
	CustomerTemplateStatus  string    `json:"customerTemplateStatus"`
	DistributedBy           int       `json:"distributedBy"`
	DistributedAt           time.Time `json:"distributedAt"`
	LatestProviderVersion   string    `json:"latestProviderVersion"`
	UpgradeAvailable        bool      `json:"upgradeAvailable"`
}

// WorkflowTemplateDistributionListResponse 分发状态列表（按分发时间倒序）。
type WorkflowTemplateDistributionListResponse struct {
	Items []*WorkflowTemplateDistribution `json:"items"`
	Total int                             `json:"total"`
}

// DistributeWorkflowTemplateResponse 分发结果（返回本次生成的客户副本信息）。
type DistributeWorkflowTemplateResponse struct {
	Distribution *WorkflowTemplateDistribution `json:"distribution"`
}
