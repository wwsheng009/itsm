package dto

import (
	"time"
)

// SLA定义相关DTO
type CreateSLADefinitionRequest struct {
	Name            string                 `json:"name" binding:"required" example:"标准服务SLA"`
	Description     string                 `json:"description" example:"标准IT服务的SLA定义"`
	ServiceType     string                 `json:"serviceType" example:"standard"`
	Priority        string                 `json:"priority" example:"medium"`
	ResponseTime    int                    `json:"responseTime" binding:"required,min=1" example:"30"`
	ResolutionTime  int                    `json:"resolutionTime" binding:"required,min=1" example:"240"`
	BusinessHours   map[string]interface{} `json:"businessHours" example:"{\"timezone\":\"Asia/Shanghai\"}"`
	EscalationRules map[string]interface{} `json:"escalationRules" example:"{\"levels\":[]}"`
	Conditions      map[string]interface{} `json:"conditions" example:"{\"priority\":[\"low\",\"medium\"]}"`
	IsActive        bool                   `json:"isActive" example:"true"`
}

type UpdateSLADefinitionRequest struct {
	Name            *string                `json:"name,omitempty"`
	Description     *string                `json:"description,omitempty"`
	ServiceType     *string                `json:"serviceType,omitempty"`
	Priority        *string                `json:"priority,omitempty"`
	ResponseTime    *int                   `json:"responseTime,omitempty"`
	ResolutionTime  *int                   `json:"resolutionTime,omitempty"`
	BusinessHours   map[string]interface{} `json:"businessHours,omitempty"`
	EscalationRules map[string]interface{} `json:"escalationRules,omitempty"`
	Conditions      map[string]interface{} `json:"conditions,omitempty"`
	IsActive        *bool                  `json:"isActive,omitempty"`
}

type SLADefinitionResponse struct {
	ID              int                    `json:"id" example:"1"`
	Name            string                 `json:"name" example:"标准服务SLA"`
	Description     string                 `json:"description" example:"标准IT服务的SLA定义"`
	ServiceType     string                 `json:"serviceType" example:"standard"`
	Priority        string                 `json:"priority" example:"medium"`
	ResponseTime    int                    `json:"responseTime" example:"30"`
	ResolutionTime  int                    `json:"resolutionTime" example:"240"`
	BusinessHours   map[string]interface{} `json:"businessHours"`
	EscalationRules map[string]interface{} `json:"escalationRules"`
	Conditions      map[string]interface{} `json:"conditions"`
	IsActive        bool                   `json:"isActive" example:"true"`
	TenantID        int                    `json:"tenantId" example:"1"`
	// OwnerProviderTenantID 归属服务商租户（0=租户自用；非0=provider 合同 SLA，方案 §4.1）。
	OwnerProviderTenantID int       `json:"ownerProviderTenantId" example:"0"`
	CreatedAt             time.Time `json:"createdAt" example:"2024-01-01T00:00:00Z"`
	UpdatedAt             time.Time `json:"updatedAt" example:"2024-01-01T00:00:00Z"`
}

// CreateSLAGrantRequest provider 下发 SLA 定义给客户的授权请求（D-SLA-1，方案 §4.1）。
// customerTenantId 必须是当前服务商归属客户（handle 内校验 msp_provider_id）；
// ticketTypeId=0 表示类型无关授权，非 0 时为客户租户内的工单类型 ID。
type CreateSLAGrantRequest struct {
	SLADefinitionID  int        `json:"slaDefinitionId" binding:"required" example:"1"`
	CustomerTenantID int        `json:"customerTenantId" binding:"required" example:"2"`
	TicketTypeID     int64      `json:"ticketTypeId" example:"0"`
	ValidUntil       *time.Time `json:"validUntil,omitempty"`
}

// UpdateSLAGrantRequest 仅允许调整授权有效期（类型/归属/定义不可变，变更走停用后重建）。
type UpdateSLAGrantRequest struct {
	ValidUntil      *time.Time `json:"validUntil,omitempty"`
	ClearValidUntil bool       `json:"clearValidUntil,omitempty"`
}

// MSPContractBindingInput 合同定义绑定入参（D-SLA-4；ticketTypeId=0 表示类型无关绑定）。
type MSPContractBindingInput struct {
	SLADefinitionID int   `json:"slaDefinitionId" binding:"required" example:"1"`
	TicketTypeID    int64 `json:"ticketTypeId" example:"0"`
}

// CreateMSPContractRequest 创建 MSP SLA 合同（provider 面；至少一条定义绑定）。
type CreateMSPContractRequest struct {
	CustomerTenantID int                       `json:"customerTenantId" binding:"required" example:"2"`
	ContractNo       string                    `json:"contractNo" binding:"required" example:"HT-2026-001"`
	Name             string                    `json:"name" binding:"required" example:"年度运维合同"`
	EffectiveFrom    *time.Time                `json:"effectiveFrom,omitempty"`
	EffectiveUntil   *time.Time                `json:"effectiveUntil,omitempty"`
	Status           string                    `json:"status,omitempty" example:"active"`
	Penalties        map[string]interface{}    `json:"penalties,omitempty"`
	SettlementCycle  string                    `json:"settlementCycle,omitempty" example:"monthly"`
	Currency         string                    `json:"currency,omitempty" example:"CNY"`
	Bindings         []MSPContractBindingInput `json:"bindings" binding:"required,min=1,dive"`
}

// UpdateMSPContractRequest 更新合同：仅传字段被更新；Bindings 非 nil 时全量替换（至少一条）。
type UpdateMSPContractRequest struct {
	Name            *string                    `json:"name,omitempty"`
	Status          *string                    `json:"status,omitempty"`
	EffectiveUntil  *time.Time                 `json:"effectiveUntil,omitempty"`
	Penalties       map[string]interface{}     `json:"penalties,omitempty"`
	SettlementCycle *string                    `json:"settlementCycle,omitempty"`
	Currency        *string                    `json:"currency,omitempty"`
	Bindings        *[]MSPContractBindingInput `json:"bindings,omitempty"`
}

// SLA违规相关DTO
type CreateSLAViolationRequest struct {
	SLADefinitionID int    `json:"slaDefinitionId" binding:"required" example:"1"`
	TicketID        int    `json:"ticketId" binding:"required" example:"1"`
	ViolationType   string `json:"violationType" binding:"required" example:"response_time"`
	Description     string `json:"description" example:"响应时间超时"`
	Severity        string `json:"severity" example:"medium"`
}

type UpdateSLAViolationRequest struct {
	Description     *string `json:"description,omitempty"`
	Severity        *string `json:"severity,omitempty"`
	IsResolved      *bool   `json:"isResolved,omitempty"`
	ResolutionNotes *string `json:"resolutionNotes,omitempty"`
}

// SettleViolationRequest 违约裁决（D-SLA-8，方案 §13.1）：
// accepted=认账进结算；waived=豁免不进结算（reason 必填）。
type SettleViolationRequest struct {
	Decision string `json:"decision" binding:"required,oneof=accepted waived" example:"accepted"`
	Reason   string `json:"reason" example:"经双方确认属客户侧原因"`
}

type SLAViolationResponse struct {
	ID              int        `json:"id" example:"1"`
	SLADefinitionID int        `json:"slaDefinitionId" example:"1"`
	TicketID        int        `json:"ticketId" example:"1"`
	ViolationType   string     `json:"violationType" example:"response_time"`
	ViolationTime   time.Time  `json:"violationTime" example:"2024-01-01T00:00:00Z"`
	Description     string     `json:"description" example:"响应时间超时"`
	Severity        string     `json:"severity" example:"medium"`
	IsResolved      bool       `json:"isResolved" example:"false"`
	ResolvedAt      *time.Time `json:"resolvedAt,omitempty" example:"2024-01-01T00:00:00Z"`
	ResolutionNotes string     `json:"resolutionNotes" example:"已解决"`
	// D-SLA-8 归责状态机
	ResolutionState string     `json:"resolutionState" example:"pending"`
	DisputeReason   string     `json:"disputeReason,omitempty"`
	WaivedReason    string     `json:"waivedReason,omitempty"`
	WaivedBy        int        `json:"waivedBy,omitempty"`
	WaivedAt        *time.Time `json:"waivedAt,omitempty"`
	SettledFlag     bool       `json:"settledFlag" example:"false"`
	TenantID        int        `json:"tenantId" example:"1"`
	CreatedAt       time.Time  `json:"createdAt" example:"2024-01-01T00:00:00Z"`
	UpdatedAt       time.Time  `json:"updatedAt" example:"2024-01-01T00:00:00Z"`
}

// SLA指标相关DTO
type CreateSLAMetricRequest struct {
	SLADefinitionID int                    `json:"slaDefinitionId" binding:"required" example:"1"`
	MetricType      string                 `json:"metricType" binding:"required" example:"response_time"`
	MetricName      string                 `json:"metricName" binding:"required" example:"平均响应时间"`
	MetricValue     float64                `json:"metricValue" binding:"required" example:"25.5"`
	Unit            string                 `json:"unit" example:"分钟"`
	Metadata        map[string]interface{} `json:"metadata" example:"{\"source\":\"system\"}"`
}

type SLAMetricResponse struct {
	ID              int                    `json:"id" example:"1"`
	SLADefinitionID int                    `json:"slaDefinitionId" example:"1"`
	MetricType      string                 `json:"metricType" example:"response_time"`
	MetricName      string                 `json:"metricName" example:"平均响应时间"`
	MetricValue     float64                `json:"metricValue" example:"25.5"`
	Unit            string                 `json:"unit" example:"分钟"`
	MeasurementTime time.Time              `json:"measurementTime" example:"2024-01-01T00:00:00Z"`
	Metadata        map[string]interface{} `json:"metadata"`
	TenantID        int                    `json:"tenantId" example:"1"`
	CreatedAt       time.Time              `json:"createdAt" example:"2024-01-01T00:00:00Z"`
	UpdatedAt       time.Time              `json:"updatedAt" example:"2024-01-01T00:00:00Z"`
}

// SLAMonitoringRequest 监控大屏指标查询。窗口缺省时服务端使用最近 30 天。
// 监控接口只按认证中间件注入的租户上下文 + 时间窗口聚合，不接受请求体里的
// slaDefinitionId / ticketId 等自报字段，避免跨范围注入。
type SLAMonitoringRequest struct {
	StartTime string `json:"startTime" example:"2024-01-01T00:00:00Z"`
	EndTime   string `json:"endTime" example:"2024-01-31T23:59:59Z"`
}

type SLAReportPeriod struct {
	StartDate string `json:"startDate" example:"2024-01-01T00:00:00Z"`
	EndDate   string `json:"endDate" example:"2024-01-31T23:59:59Z"`
}

// SLAComplianceReport represents SLA compliance report
type SLAComplianceReport struct {
	TotalTickets      int             `json:"totalTickets" example:"100"`
	MetSLA            int             `json:"metSla" example:"95"`
	ViolatedSLA       int             `json:"violatedSla" example:"5"`
	ComplianceRate    float64         `json:"complianceRate" example:"95.0"`
	AvgResponseTime   float64         `json:"avgResponseTime" example:"28.5"`
	AvgResolutionTime float64         `json:"avgResolutionTime" example:"180.5"`
	ReportPeriod      SLAReportPeriod `json:"reportPeriod"`
}

// Existing SLA report DTOs remain unchanged...

type SLASummary struct {
	TotalTickets             int     `json:"totalTickets" example:"100"`
	CompliantTickets         int     `json:"compliantTickets" example:"95"`
	ViolatedTickets          int     `json:"violatedTickets" example:"5"`
	ComplianceRate           float64 `json:"complianceRate" example:"95.0"`
	AverageResponseTime      float64 `json:"averageResponseTime" example:"28.5"`
	AverageResolutionTime    float64 `json:"averageResolutionTime" example:"180.5"`
	ResponseTimeCompliance   float64 `json:"responseTimeCompliance" example:"96.0"`
	ResolutionTimeCompliance float64 `json:"resolutionTimeCompliance" example:"94.0"`
}

type SLADailyMetric struct {
	Date                  string  `json:"date" example:"2024-01-01"`
	Tickets               int     `json:"tickets" example:"10"`
	CompliantTickets      int     `json:"compliantTickets" example:"9"`
	ViolatedTickets       int     `json:"violatedTickets" example:"1"`
	ComplianceRate        float64 `json:"complianceRate" example:"90.0"`
	AverageResponseTime   float64 `json:"averageResponseTime" example:"25.5"`
	AverageResolutionTime float64 `json:"averageResolutionTime" example:"200.5"`
}

type SLATrends struct {
	ComplianceTrend     string  `json:"complianceTrend" example:"improving"` // improving, declining, stable
	ResponseTimeTrend   string  `json:"responseTimeTrend" example:"stable"`
	ResolutionTimeTrend string  `json:"resolutionTimeTrend" example:"improving"`
	TrendPercentage     float64 `json:"trendPercentage" example:"5.2"`
}

// SLA升级相关DTO
type SLAEscalationRequest struct {
	TicketID        int    `json:"ticketId" binding:"required" example:"1"`
	EscalationLevel int    `json:"escalationLevel" binding:"required" example:"1"`
	Reason          string `json:"reason" example:"响应时间即将超时"`
	NotifyUsers     []int  `json:"notifyUsers" example:"[1,2,3]"`
}

type SLAEscalationResponse struct {
	ID              int       `json:"id" example:"1"`
	TicketID        int       `json:"ticketId" example:"1"`
	EscalationLevel int       `json:"escalationLevel" example:"1"`
	Reason          string    `json:"reason" example:"响应时间即将超时"`
	Status          string    `json:"status" example:"active"`
	NotifiedUsers   []int     `json:"notifiedUsers" example:"[1,2,3]"`
	CreatedAt       time.Time `json:"createdAt" example:"2024-01-01T00:00:00Z"`
	UpdatedAt       time.Time `json:"updatedAt" example:"2024-01-01T00:00:00Z"`
}

// SLAMonitoringDashboard represents comprehensive SLA monitoring dashboard
type SLAMonitoringDashboard struct {
	ComplianceRate    float64            `json:"complianceRate"`
	ViolationRate     float64            `json:"violationRate"`
	TotalTickets      int                `json:"totalTickets"`
	AtRiskTickets     int                `json:"atRiskTickets"`
	BreachedTickets   int                `json:"breachedTickets"`
	UpcomingDeadlines []SLADeadline      `json:"upcomingDeadlines"`
	TopViolations     []SLAViolationItem `json:"topViolations"`
	SLAByPriority     map[string]float64 `json:"slaByPriority"`
	TrendData         []SLATrendPoint    `json:"trendData"`
}

type SLADeadline struct {
	TicketID    int       `json:"ticketId"`
	TicketTitle string    `json:"ticketTitle"`
	Deadline    time.Time `json:"deadline"`
	SLAPolicy   string    `json:"slaPolicy"`
	TimeLeft    string    `json:"timeLeft"`
}

type SLAViolationItem struct {
	TicketID    int    `json:"ticketId"`
	TicketTitle string `json:"ticketTitle"`
	SLAPolicy   string `json:"slaPolicy"`
	ViolatedAt  string `json:"violatedAt"`
	Delay       int    `json:"delayMinutes"`
}

type SLATrendPoint struct {
	Date           string  `json:"date"`
	ComplianceRate float64 `json:"complianceRate"`
	TicketCount    int     `json:"ticketCount"`
}
