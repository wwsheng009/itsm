package dto

import "time"

// ===== P2-3 D-TT-1 工单类型受控下发（方案 §4.5a，Accepted v1.11）=====

// TicketTypeDistributionItem 分发记录（provider 视角；含源/副本摘要与同步状态）。
type TicketTypeDistributionItem struct {
	ID               int        `json:"id"`
	CustomerTenantID int64      `json:"customerTenantId"`
	ProviderTenantID int64      `json:"providerTenantId"`
	SourceTypeID     int64      `json:"sourceTypeId"`
	SourceTypeCode   string     `json:"sourceTypeCode,omitempty"`
	SourceTypeName   string     `json:"sourceTypeName,omitempty"`
	CopyTypeID       int64      `json:"copyTypeId"`
	CopyTypeCode     string     `json:"copyTypeCode,omitempty"`
	CopyTypeName     string     `json:"copyTypeName,omitempty"`
	SyncStatus       string     `json:"syncStatus"` // synced|dirty|failed|orphaned|unbound
	LastSyncedAt     *time.Time `json:"lastSyncedAt,omitempty"`
	DistributedBy    int64      `json:"distributedBy"`
	DistributedAt    time.Time  `json:"distributedAt"`
	// UpdateAvailable 源有未发布变更（syncStatus=dirty 的等价表达，供前端"有可用更新"角标）。
	UpdateAvailable bool `json:"updateAvailable"`
}

// DistributeTicketTypeResponse 下发结果。
type DistributeTicketTypeResponse struct {
	Distribution *TicketTypeDistributionItem `json:"distribution"`
	// SLABinding 联动结果: created|updated|unchanged|skipped_no_sla|skipped_no_contract
	SLABinding string `json:"slaBinding,omitempty"`
}

// TicketTypeDistributionChange 发布 diff 的单个受控字段变更（INV-3 白名单内）。
type TicketTypeDistributionChange struct {
	Field string `json:"field"` // name|description|slaEnabled
	From  string `json:"from"`
	To    string `json:"to"`
}

// TicketTypeDistributionPreview 发布预览（① 编辑源 → ② 预览 diff）。
type TicketTypeDistributionPreview struct {
	DistributionID int                            `json:"distributionId"`
	Changes        []TicketTypeDistributionChange `json:"changes"`
	// SLAAction: create|update|none（合同 binding 通道，v1.11 修正 IM-28）。
	SLAAction           string `json:"slaAction"`
	SLADefinitionIDFrom int64  `json:"slaDefinitionIdFrom,omitempty"`
	SLADefinitionIDTo   int64  `json:"slaDefinitionIdTo,omitempty"`
	// UpToDate 无待发布变更。
	UpToDate bool `json:"upToDate"`
	// Note 非阻断提示（如无生效合同导致 SLA 联动暂缓）。
	Note string `json:"note,omitempty"`
}

// PublishTicketTypeDistributionResponse 发布结果（按副本逐个事务；失败可重试收敛）。
type PublishTicketTypeDistributionResponse struct {
	DistributionID int        `json:"distributionId"`
	Status         string     `json:"status"` // synced|failed
	SLAAction      string     `json:"slaAction,omitempty"`
	SyncedAt       *time.Time `json:"syncedAt,omitempty"`
	Error          string     `json:"error,omitempty"`
}

// UnbindTicketTypeDistributionResponse 解绑结果（副本转 native，INV-4 零 DELETE）。
type UnbindTicketTypeDistributionResponse struct {
	DistributionID int   `json:"distributionId"`
	CopyTypeID     int64 `json:"copyTypeId"`
	Released       bool  `json:"released"`
	BindingRemoved bool  `json:"bindingRemoved"`
}
