package sla

import (
	"time"
)

// SLADefinition represents an SLA policy
type SLADefinition struct {
	ID              int                    `json:"id"`
	Name            string                 `json:"name"`
	Description     string                 `json:"description"`
	ServiceType     string                 `json:"serviceType"`
	Priority        string                 `json:"priority"`
	ResponseTime    int                    `json:"responseTime"`   // in minutes
	ResolutionTime  int                    `json:"resolutionTime"` // in minutes
	BusinessHours   map[string]interface{} `json:"businessHours"`
	EscalationRules map[string]interface{} `json:"escalationRules"`
	Conditions      map[string]interface{} `json:"conditions"`
	IsActive        bool                   `json:"isActive"`
	TenantID        int                    `json:"tenantId"`
	// OwnerProviderTenantID 归属服务商租户（方案 §4.1）：
	// 0=租户自用；非0=provider 合同 SLA（经 sla_definition_grants 下发，customer 只读）。
	OwnerProviderTenantID int       `json:"ownerProviderTenantId"`
	CreatedAt             time.Time `json:"createdAt"`
	UpdatedAt             time.Time `json:"updatedAt"`
}

// SLADefinitionGrant SLA 定义下发授权（D-SLA-1，方案 §4.1）。
// 授权行归属 provider 租户（TenantID=provider）；customer 端只读（RLS 授信通道）。
type SLADefinitionGrant struct {
	ID               int `json:"id"`
	SLADefinitionID  int `json:"slaDefinitionId"`
	CustomerTenantID int `json:"customerTenantId"`
	ProviderTenantID int `json:"providerTenantId"`
	// TicketTypeID：customer 租户内的工单类型 ID；0=类型无关授权（匹配低优先级）。
	TicketTypeID int64      `json:"ticketTypeId"`
	ValidUntil   *time.Time `json:"validUntil,omitempty"`
	IsActive     bool       `json:"isActive"`
	TenantID     int        `json:"tenantId"`
	CreatedAt    time.Time  `json:"createdAt"`
	UpdatedAt    time.Time  `json:"updatedAt"`
	// DefinitionName 由列表/详情查询富化（可选展示字段）。
	DefinitionName string `json:"definitionName,omitempty"`
}

// ProvidedDefinition customer 视角的"服务商下发定义"（D-SLA-1 只读视图）。
// P1-2 起数据源升级为合同链（active 合同 → bindings → 定义；grant 写入口已停用），
// customer 不可修改（四层写保护）。
type ProvidedDefinition struct {
	ContractID       int        `json:"contractId"`
	ContractNo       string     `json:"contractNo,omitempty"`
	SLADefinitionID  int        `json:"slaDefinitionId"`
	Name             string     `json:"name"`
	Description      string     `json:"description"`
	ServiceType      string     `json:"serviceType"`
	Priority         string     `json:"priority"`
	ResponseTime     int        `json:"responseTime"`
	ResolutionTime   int        `json:"resolutionTime"`
	TicketTypeID     int64      `json:"ticketTypeId"`
	ProviderTenantID int        `json:"providerTenantId"`
	ValidUntil       *time.Time `json:"validUntil,omitempty"`
}

// MSPContract MSP SLA 合同（D-SLA-4，方案 §4.4）。
// 归属 provider 租户（TenantID=provider）；customer 端只读（RLS 012 授信）。
type MSPContract struct {
	ID               int                    `json:"id"`
	ProviderTenantID int                    `json:"providerTenantId"`
	CustomerTenantID int                    `json:"customerTenantId"`
	ContractNo       string                 `json:"contractNo"`
	Name             string                 `json:"name"`
	EffectiveFrom    time.Time              `json:"effectiveFrom"`
	EffectiveUntil   *time.Time             `json:"effectiveUntil,omitempty"`
	Status           string                 `json:"status"`
	Penalties        map[string]interface{} `json:"penalties,omitempty"`
	SettlementCycle  string                 `json:"settlementCycle"`
	Currency         string                 `json:"currency,omitempty"`
	TenantID         int                    `json:"tenantId"`
	CreatedAt        time.Time              `json:"createdAt"`
	UpdatedAt        time.Time              `json:"updatedAt"`
	// Bindings 定义绑定集：创建时作为入参（必填至少一条）；详情/列表时预加载返回。
	Bindings []MSPContractSLABinding `json:"bindings,omitempty"`
}

// MSPContractSLABinding 合同 ↔ SLA 定义绑定（含类型维度，实施期修正 IM-13）。
type MSPContractSLABinding struct {
	ID              int   `json:"id"`
	ContractID      int   `json:"contractId"`
	SLADefinitionID int   `json:"slaDefinitionId"`
	TicketTypeID    int64 `json:"ticketTypeId"`
	TenantID        int   `json:"tenantId"`
	// DefinitionName 富化展示字段（列表/详情查询时填充）。
	DefinitionName string `json:"definitionName,omitempty"`
}

// ===== P2-2 结算报表（D-SLA-5/D-SLA-9，方案 §4.5/§13）=====

// SettlementDefinitionRef 结算重算所需的定义引用（来自合同绑定，仅取活跃定义）。
type SettlementDefinitionRef struct {
	ID             int
	Name           string
	ResponseTime   int
	ResolutionTime int
	BusinessHours  map[string]interface{}
	IsActive       bool
}

// SettlementTicket 结算范围内的工单事实行（客观时间 + 快照字段，§13.5 事实层）。
type SettlementTicket struct {
	ID                    int
	TicketNumber          string
	Title                 string
	TicketTypeID          int64
	TicketTypeCode        string
	CreatedAt             time.Time
	FirstResponseAt       time.Time
	ResolvedAt            time.Time
	SLAResponseDeadline   time.Time
	SLAResolutionDeadline time.Time
	SLADefinitionID       int
	SLAMatchedLayer       string
}

// SettlementViolation 结算报表引用的违约行（含归责状态与来源层级快照）。
type SettlementViolation struct {
	ID              int
	TicketID        int
	SLADefinitionID int
	ViolationType   string
	ResolutionState string
	MatchedLayer    string
	SettledFlag     bool
	ViolationTime   time.Time
}

// SettlementScope 结算取数结果：
// ① customer 归属校验（取数前，fail-closed）；② 报告期生效合同 + 绑定覆盖（白名单条件③）；
// ③ 窗口内工单分类（覆盖/豁免/未覆盖，条件①②④）；④ 覆盖工单的违约行。
type SettlementScope struct {
	CustomerName   string
	Contract       *MSPContract
	Bindings       map[int64]*SettlementDefinitionRef // ticket_type_id(>0) -> 定义
	TypeAgnostic   *SettlementDefinitionRef           // 类型无关绑定（ticket_type_id=0）
	CoveredTickets []*SettlementTicket
	CoveredIDs     []int
	ExemptCount    int
	UncoveredCount int
	Violations     []*SettlementViolation
}

// SLAViolation represents a breach of SLA
type SLAViolation struct {
	ID        int `json:"id"`
	CreatedBy int `json:"createdBy"`
	TicketID  int `json:"ticketId"`
	// 以下字段来自关联工单（ticket edge），供监控大屏列表直接展示，
	// 避免前端拿不到标题/编号而退化为 "Unknown"。
	TicketNumber    string     `json:"ticketNumber,omitempty"`
	TicketTitle     string     `json:"ticketTitle,omitempty"`
	TicketPriority  string     `json:"ticketPriority,omitempty"`
	SLAName         string     `json:"slaName,omitempty"`
	SLADefinitionID int        `json:"slaDefinitionId"`
	ViolationType   string     `json:"violationType"` // e.g., "response", "resolution"
	ViolationTime   time.Time  `json:"violationTime"`
	Description     string     `json:"description"`
	Severity        string     `json:"severity"`
	IsResolved      bool       `json:"isResolved"`
	ResolvedAt      *time.Time `json:"resolvedAt,omitempty"`
	ResolutionNotes string     `json:"resolutionNotes"`
	// D-SLA-8 归责状态机（方案 §13.1）：pending|accepted|disputed|waived；
	// 终态不可逆；settledFlag=true 后随结算快照锁定。
	ResolutionState string     `json:"resolutionState"`
	DisputeReason   string     `json:"disputeReason,omitempty"`
	WaivedReason    string     `json:"waivedReason,omitempty"`
	WaivedBy        int        `json:"waivedBy,omitempty"`
	WaivedAt        *time.Time `json:"waivedAt,omitempty"`
	SettledFlag     bool       `json:"settledFlag"`
	TenantID        int        `json:"tenantId"`
}

// SLAAlertItem 是监控大屏告警列表的一行。
//
// 数据源是 SLAAlertHistory（真实触发的告警），附加工单优先级和剩余时间，
// 避免前端再用 violations 或空数组伪造告警。
type SLAAlertItem struct {
	ID            int               `json:"id"`
	TicketID      int               `json:"ticketId"`
	TicketNumber  string            `json:"ticketNumber"`
	TicketTitle   string            `json:"ticketTitle"`
	Priority      string            `json:"priority"`
	AlertLevel    string            `json:"alertLevel"`
	AlertRuleName string            `json:"alertRuleName"`
	ThresholdPct  int               `json:"thresholdPercentage"`
	ActualPct     float64           `json:"actualPercentage"`
	CreatedAt     time.Time         `json:"createdAt"`
	ResolvedAt    *time.Time        `json:"resolvedAt,omitempty"`
	TimeRemaining *SLATimeRemaining `json:"timeRemaining,omitempty"`
}

// SLATimeRemaining 显式表达「剩余时间」的口径：绑定工单的解决截止时间。
// 没有截止时间时整个对象为 nil，前端必须显示「—」而不是 0 小时。
type SLATimeRemaining struct {
	Hours    float64 `json:"hours"` // 可为负数，表示已超时
	Deadline string  `json:"deadline"`
}

// SLAMonitoringData 是 SLA 监控大屏（POST /api/v1/sla/monitoring）的权威响应契约。
//
// 口径约定，前端不得自行重新计算：
//   - 所有 *Rate 字段都是 0-100 的百分数，保留一位小数；
//   - 每个比率都带样本数量，样本为 0 表示「暂无数据」，此时比率固定为 0；
//     前端必须按样本数渲染空态，禁止把无样本伪装成 100% 或 0% 合规；
//   - 时长统一使用分钟并显式命名 Minutes（与 SLADefinition 的 responseTime /
//     resolutionTime 单位一致），避免历史上的小时/分钟歧义；
//   - 统计种群是「窗口内创建且未软删除」的工单，与合规报告保持同一口径。
type SLAMonitoringData struct {
	// 实际生效的统计窗口，RFC3339 字符串，供大屏展示统计周期
	StartTime string `json:"startTime"`
	EndTime   string `json:"endTime"`
	// Truncated 为 true 表示窗口内工单数超过扫描上限，分母已被截断，
	// 大屏必须提示样本不完整，而不是把偏小的分母当成全量真相。
	Truncated bool `json:"truncated"`

	// 工单解决率：生命周期状态为 resolved/closed 的工单占窗口内工单总数的比例
	// （不用 resolved_at 猜测，工单可能从 resolved 回退而时间戳仍保留）
	TotalTickets    int     `json:"totalTickets"`
	ResolvedTickets int     `json:"resolvedTickets"`
	ResolutionRate  float64 `json:"resolutionRate"`

	// SLA 合规：窗口内工单中当前仍有未解决违约的数量
	ViolatedTickets int     `json:"violatedTickets"`
	MetSlaTickets   int     `json:"metSlaTickets"`
	ComplianceRate  float64 `json:"complianceRate"`
	ViolationRate   float64 `json:"violationRate"`
	// AtRiskTickets：未解决且已超过响应/解决截止时间的工单数
	AtRiskTickets int `json:"atRiskTickets"`

	// 响应达成率：有响应截止时间且已首次响应的工单中，响应不晚于截止时间的比例
	ResponseTimeSamples    int     `json:"responseTimeSamples"`
	ResponseTimeMet        int     `json:"responseTimeMet"`
	ResponseTimeCompliance float64 `json:"responseTimeCompliance"`
	// 解决达成率：有解决截止时间且已解决的工单中，解决不晚于截止时间的比例
	ResolutionTimeSamples    int     `json:"resolutionTimeSamples"`
	ResolutionTimeMet        int     `json:"resolutionTimeMet"`
	ResolutionTimeCompliance float64 `json:"resolutionTimeCompliance"`

	AverageResponseMinutes   float64 `json:"averageResponseMinutes"`
	AverageResolutionMinutes float64 `json:"averageResolutionMinutes"`

	// 违约记录维度（记录数，非工单数）
	TotalViolations    int `json:"totalViolations"`
	ResolvedViolations int `json:"resolvedViolations"`
	ActiveViolations   int `json:"activeViolations"`

	// 活跃告警：截至窗口结束时刻仍未解决的告警数量；Alerts 是最近若干条明细
	ActiveAlerts int             `json:"activeAlerts"`
	Alerts       []*SLAAlertItem `json:"alerts"`

	ActiveSlas       int `json:"activeSlas"`
	ActiveAlertRules int `json:"activeAlertRules"`
}

// SLA 绩效分组维度。服务类型来自 SLA 定义的 service_type（工单通过
// sla_definition_id 绑定），优先级直接取工单自身的 priority 字段。
const (
	SLADimensionServiceType = "serviceType"
	SLADimensionPriority    = "priority"

	// SLAPerformanceUnassignedKey 表示工单未绑定 SLA 定义，因此没有服务类型归属。
	// 必须单独成行展示，禁止静默丢弃这部分工单。
	SLAPerformanceUnassignedKey = "unassigned"
)

// SLAPerformanceRow 是按维度聚合后的单行绩效。
// 比率字段同样是 0-100 百分数，且都伴随样本数量；样本为 0 时比率为 0。
// Key 是服务类型的原始值或工单优先级；展示名由前端字典映射，未命中时直接回显 Key。
type SLAPerformanceRow struct {
	Key             string  `json:"key"`
	TotalTickets    int     `json:"totalTickets"`
	ResolvedTickets int     `json:"resolvedTickets"`
	ResolutionRate  float64 `json:"resolutionRate"`
	ViolatedTickets int     `json:"violatedTickets"`
	MetSlaTickets   int     `json:"metSlaTickets"`
	ComplianceRate  float64 `json:"complianceRate"`

	ResponseSamples           int     `json:"responseSamples"`
	ResponseAchievementRate   float64 `json:"responseAchievementRate"`
	ResolutionSamples         int     `json:"resolutionSamples"`
	ResolutionAchievementRate float64 `json:"resolutionAchievementRate"`

	AverageResponseMinutes   float64 `json:"averageResponseMinutes"`
	AverageResolutionMinutes float64 `json:"averageResolutionMinutes"`
}

// SLAMetric represents SLA performance indicators
type SLAMetric struct {
	ID              int       `json:"id"`
	SLADefinitionID int       `json:"slaDefinitionId"`
	MetricType      string    `json:"metricType"`
	MetricName      string    `json:"metricName"`
	MetricValue     float64   `json:"metricValue"`
	Unit            string    `json:"unit"`
	MeasurementTime time.Time `json:"measurementTime"`
	TenantID        int       `json:"tenantId"`
}

// SLAAlertRule defines when to trigger an alert before violation
type SLAAlertRule struct {
	ID                   int      `json:"id"`
	SLADefinitionID      int      `json:"slaDefinitionId"`
	Name                 string   `json:"name"`
	ThresholdPercentage  int      `json:"thresholdPercentage"` // 0-100
	AlertLevel           string   `json:"alertLevel"`
	NotificationChannels []string `json:"notificationChannels"`
	IsActive             bool     `json:"isActive"`
	TenantID             int      `json:"tenantId"`
}

// SLAAlertHistory records triggered alerts
type SLAAlertHistory struct {
	ID                       int        `json:"id"`
	AlertRuleID              int        `json:"alertRuleId"`
	TicketID                 int        `json:"ticketId"`
	TicketNumber             string     `json:"ticketNumber"`
	TicketTitle              string     `json:"ticketTitle"`
	AlertLevel               string     `json:"alertLevel"`
	ThresholdPercentage      int        `json:"thresholdPercentage"`
	ActualPercentage         float64    `json:"actualPercentage"`
	NotificationSent         bool       `json:"notificationSent"`
	CreatedAt                time.Time  `json:"createdAt"` // trigger time
	ResolvedAt               *time.Time `json:"resolvedAt,omitempty"`
	TenantID                 int        `json:"tenantId"`
	CooldownRemainingSeconds int        `json:"cooldownRemainingSeconds"`
	CooldownMinutes          int        `json:"cooldownMinutes"`
	SuppressedByCooldown     bool       `json:"suppressedByCooldown"`
}
