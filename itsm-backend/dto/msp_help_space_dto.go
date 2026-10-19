package dto

import "time"

// P2 MSP 多租户帮助空间与分发授权 DTO。
//
// 语义对齐 workflow_template_distributions（P2-①）/ ticket_type_distributions（D-TT-1）先例：
// provider 设计 → 客户持有 copy-in 快照 → 来源追踪 + 升级提示（提示不强制）→ 解绑转客户原生。

// MSPHelpDocRequest 帮助空间文档创建/更新请求（slug 在 provider 内唯一，不可改写）。
type MSPHelpDocRequest struct {
	Slug    string `json:"slug"`
	Title   string `json:"title"`
	PageKey string `json:"pageKey"`
	BodyMD  string `json:"bodyMd"`
}

// MSPHelpDoc provider 侧帮助空间文档。
type MSPHelpDoc struct {
	ID          int        `json:"id"`
	Slug        string     `json:"slug"`
	Title       string     `json:"title"`
	PageKey     string     `json:"pageKey"`
	BodyMD      string     `json:"bodyMd"`
	Status      string     `json:"status"` // draft|published
	Version     int        `json:"version"`
	CreatedBy   int        `json:"createdBy"`
	UpdatedBy   int        `json:"updatedBy"`
	CreatedAt   time.Time  `json:"createdAt"`
	UpdatedAt   time.Time  `json:"updatedAt"`
	PublishedAt *time.Time `json:"publishedAt,omitempty"`
}

// MSPHelpDocListResponse 帮助空间文档列表（provider 视角，更新时间倒序）。
type MSPHelpDocListResponse struct {
	Items []*MSPHelpDoc `json:"items"`
	Total int           `json:"total"`
}

// MSPHelpDistribution 分发记录（copy-in 来源追踪 + 升级提示）。
type MSPHelpDistribution struct {
	ID                    int        `json:"id"`
	CustomerTenantID      int        `json:"customerTenantId"`
	ProviderTenantID      int        `json:"providerTenantId"`
	SourceDocID           int        `json:"sourceDocId"`
	SourceSlug            string     `json:"sourceSlug"`
	SourceVersion         int        `json:"sourceVersion"`
	LatestProviderVersion int        `json:"latestProviderVersion"`
	UpgradeAvailable      bool       `json:"upgradeAvailable"`
	Title                 string     `json:"title"`
	PageKey               string     `json:"pageKey"`
	Status                string     `json:"status"` // active|unbound（unbound=已转客户原生副本）
	DistributedBy         int        `json:"distributedBy"`
	DistributedAt         time.Time  `json:"distributedAt"`
	UnboundBy             int        `json:"unboundBy,omitempty"`
	UnboundAt             *time.Time `json:"unboundAt,omitempty"`
}

// MSPHelpDistributionListResponse 分发状态列表（active 优先，其后按分发时间倒序）。
type MSPHelpDistributionListResponse struct {
	Items []*MSPHelpDistribution `json:"items"`
	Total int                    `json:"total"`
}

// DistributeMSPHelpDocResponse 分发结果（本次生成的客户副本信息）。
type DistributeMSPHelpDocResponse struct {
	Distribution *MSPHelpDistribution `json:"distribution"`
}
