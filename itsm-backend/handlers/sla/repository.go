package sla

import (
	"context"
	"time"
)

// Repository interface for SLA domain
type Repository interface {
	// SLA Definitions
	CreateDefinition(ctx context.Context, s *SLADefinition) (*SLADefinition, error)
	GetDefinition(ctx context.Context, id int, tenantID int) (*SLADefinition, error)
	ListDefinitions(ctx context.Context, tenantID int, page, size int) ([]*SLADefinition, int, error)
	UpdateDefinition(ctx context.Context, s *SLADefinition) (*SLADefinition, error)
	DeleteDefinition(ctx context.Context, id int, tenantID int) error

	// SLA Definition Grants（D-SLA-1：provider → customer 下发授权；方案 §4.1）
	// CreateGrant 在单事务内完成：customer 归属校验（msp_provider_id == provider）
	// + deactivate-then-activate（同 customer+type 旧授权先停用，partial unique 约束配套）。
	CreateGrant(ctx context.Context, g *SLADefinitionGrant) (*SLADefinitionGrant, error)
	// ListGrants 按 provider 租户（=授权归属租户）列出授权。
	ListGrants(ctx context.Context, providerTenantID int, filters map[string]interface{}) ([]*SLADefinitionGrant, error)
	GetGrant(ctx context.Context, id int, providerTenantID int) (*SLADefinitionGrant, error)
	// UpdateGrantValidUntil 仅允许更新有效期（ticket_type_id/归属不可变，变更走停用后重建）。
	UpdateGrantValidUntil(ctx context.Context, id int, providerTenantID int, validUntil *time.Time) (*SLADefinitionGrant, error)
	// DeactivateGrant 停用授权（逻辑撤销；不物理删除，保留审计链）。
	DeactivateGrant(ctx context.Context, id int, providerTenantID int) error
	// ListProvidedDefinitions customer 侧只读视图：授权 join 定义（active 且未过期）。
	ListProvidedDefinitions(ctx context.Context, customerTenantID int) ([]*ProvidedDefinition, error)

	// MSP SLA Contracts（D-SLA-4，方案 §4.4）
	// CreateContract 单事务：customer 归属校验 + contract_no 租户内唯一 + bindings 批量创建
	//（每个绑定校验定义归属 provider）。
	CreateContract(ctx context.Context, c *MSPContract) (*MSPContract, error)
	ListContracts(ctx context.Context, providerTenantID int, filters map[string]interface{}) ([]*MSPContract, error)
	GetContract(ctx context.Context, id int, providerTenantID int) (*MSPContract, error)
	// UpdateContract 更新标量字段；Bindings 非 nil 时全量替换（先删后建，同事务）。
	UpdateContract(ctx context.Context, c *MSPContract) (*MSPContract, error)
	// FindActiveContractForCustomer 匹配/读面共用：customer 当前生效合同
	//（status=active + 合同期内；最新生效优先），预加载 bindings 及其定义。
	FindActiveContractForCustomer(ctx context.Context, customerTenantID int, now time.Time) (*MSPContract, error)

	// Violations
	CreateViolation(ctx context.Context, v *SLAViolation) (*SLAViolation, error)
	ListViolations(ctx context.Context, tenantID int, page, size int, filters map[string]interface{}) ([]*SLAViolation, int, error)
	UpdateViolationStatus(ctx context.Context, id int, isResolved bool, notes string, tenantID int) error

	// Metrics
	CreateMetric(ctx context.Context, m *SLAMetric) (*SLAMetric, error)
	GetMetrics(ctx context.Context, tenantID int, filters map[string]interface{}) ([]*SLAMetric, error)

	// GetSLAMonitoring 返回监控大屏的指标快照。start/end 是强制的统计窗口，
	// 实现必须将其下推到查询，不得忽略窗口参数。
	GetSLAMonitoring(ctx context.Context, tenantID int, start, end time.Time) (*SLAMonitoringData, error)

	// ListSLAPerformance 按维度（serviceType / priority）聚合窗口内绩效，
	// 返回按工单数降序确定的列表；第二个返回值表示窗口内工单数命中扫描上限。
	ListSLAPerformance(ctx context.Context, tenantID int, dimension string, start, end time.Time, serviceTypeFilter, priorityFilter string) ([]*SLAPerformanceRow, bool, error)

	// Alert Rules
	CreateAlertRule(ctx context.Context, r *SLAAlertRule) (*SLAAlertRule, error)
	GetAlertRule(ctx context.Context, id int, tenantID int) (*SLAAlertRule, error)
	ListAlertRules(ctx context.Context, tenantID int, filters map[string]interface{}) ([]*SLAAlertRule, error)
	UpdateAlertRule(ctx context.Context, r *SLAAlertRule) (*SLAAlertRule, error)
	DeleteAlertRule(ctx context.Context, id int, tenantID int) error

	// Alert History
	CreateAlertHistory(ctx context.Context, h *SLAAlertHistory) (*SLAAlertHistory, error)
	ListAlertHistory(ctx context.Context, tenantID int, page, size int, filters map[string]interface{}) ([]*SLAAlertHistory, int, error)

	// Compliance Report
	GetComplianceReportData(ctx context.Context, tenantID int, startDate, endDate time.Time) (totalTickets, metSLA, violatedSLA int, avgResponseTime, avgResolutionTime float64, err error)

	// Ticket Stats - 获取工单统计用于计算合规率
	GetTicketStats(ctx context.Context, tenantID int) (total int, metSLA int, err error)

	// GetTicketSLA - 获取单个工单的 SLA 计时信息（创建时间 / 首次响应时间 / 解决时间 /
	// SLA 响应截止时间 / SLA 解决截止时间）。
	// 阻断3 修复：返回 SLA 截止时间，让 Service 层基于 deadline 判断合规，
	// 而非仅检查"是否已首次响应"。
	GetTicketSLA(ctx context.Context, ticketID int, tenantID int) (createdAt, firstResponseAt, resolvedAt, slaResponseDeadline, slaResolutionDeadline time.Time, found bool, err error)
}
