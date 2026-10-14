package sla

import (
	"context"
	"errors"
	"fmt"
	"math"
	"strings"
	"time"

	"go.uber.org/zap"

	"itsm-backend/common"
	"itsm-backend/dto"
)

// 监控/绩效查询的失败语义。handler 据此映射 HTTP status 与业务 code，
// 不得把参数错误、缺少租户和内部故障一律返回 500。
var (
	// ErrTenantRequired 缺少租户上下文时 fail closed，禁止回退到默认租户。
	ErrTenantRequired = errors.New("sla: tenant context is required")
	// ErrInvalidWindow 统计窗口非法（结束时间必须晚于开始时间）。
	ErrInvalidWindow = errors.New("sla: invalid monitoring window")
	// ErrInvalidDimension 绩效分组维度不受支持。
	ErrInvalidDimension = errors.New("sla: unsupported performance dimension")
)

// DefaultMonitoringWindow 未显式传入统计窗口时沿用历史默认值：最近 30 天。
const DefaultMonitoringWindow = 30 * 24 * time.Hour

type Service struct {
	repo   Repository
	logger *zap.SugaredLogger
}

func NewService(repo Repository, logger *zap.SugaredLogger) *Service {
	return &Service{
		repo:   repo,
		logger: logger,
	}
}

// requireTenant 定义 CRUD 的统一 fail-closed 护栏（D-SLA-7，方案 §4.7）：
// 缺少租户上下文（<=0）时拒绝，禁止写入 tenant_id=0 行（依赖 DB RLS 兜底之外的应用层防线）。
func requireTenant(tenantID int) error {
	if tenantID <= 0 {
		return ErrTenantRequired
	}
	return nil
}

// SLADefinition CRUD

func (s *Service) CreateDefinition(ctx context.Context, def *SLADefinition) (*SLADefinition, error) {
	if err := requireTenant(def.TenantID); err != nil {
		return nil, err
	}
	s.logger.Infow("Creating SLA Definition", "name", def.Name)
	return s.repo.CreateDefinition(ctx, def)
}

func (s *Service) GetDefinition(ctx context.Context, id int, tenantID int) (*SLADefinition, error) {
	if err := requireTenant(tenantID); err != nil {
		return nil, err
	}
	return s.repo.GetDefinition(ctx, id, tenantID)
}

func (s *Service) ListDefinitions(ctx context.Context, tenantID int, page, size int) ([]*SLADefinition, int, error) {
	if err := requireTenant(tenantID); err != nil {
		return nil, 0, err
	}
	return s.repo.ListDefinitions(ctx, tenantID, page, size)
}

func (s *Service) UpdateDefinition(ctx context.Context, def *SLADefinition) (*SLADefinition, error) {
	if err := requireTenant(def.TenantID); err != nil {
		return nil, err
	}
	s.logger.Infow("Updating SLA Definition", "id", def.ID)
	return s.repo.UpdateDefinition(ctx, def)
}

func (s *Service) DeleteDefinition(ctx context.Context, id int, tenantID int) error {
	if err := requireTenant(tenantID); err != nil {
		return err
	}
	s.logger.Infow("Deleting SLA Definition", "id", id)
	return s.repo.DeleteDefinition(ctx, id, tenantID)
}

// SLA Definition Grants（D-SLA-1）

func (s *Service) CreateGrant(ctx context.Context, g *SLADefinitionGrant) (*SLADefinitionGrant, error) {
	if err := requireTenant(g.TenantID); err != nil {
		return nil, err
	}
	s.logger.Infow("Creating SLA grant",
		"definition_id", g.SLADefinitionID, "customer_tenant_id", g.CustomerTenantID, "provider_tenant_id", g.ProviderTenantID)
	return s.repo.CreateGrant(ctx, g)
}

func (s *Service) ListGrants(ctx context.Context, providerTenantID int, filters map[string]interface{}) ([]*SLADefinitionGrant, error) {
	if err := requireTenant(providerTenantID); err != nil {
		return nil, err
	}
	return s.repo.ListGrants(ctx, providerTenantID, filters)
}

func (s *Service) GetGrant(ctx context.Context, id int, providerTenantID int) (*SLADefinitionGrant, error) {
	if err := requireTenant(providerTenantID); err != nil {
		return nil, err
	}
	return s.repo.GetGrant(ctx, id, providerTenantID)
}

func (s *Service) UpdateGrantValidUntil(ctx context.Context, id int, providerTenantID int, validUntil *time.Time) (*SLADefinitionGrant, error) {
	if err := requireTenant(providerTenantID); err != nil {
		return nil, err
	}
	return s.repo.UpdateGrantValidUntil(ctx, id, providerTenantID, validUntil)
}

func (s *Service) DeactivateGrant(ctx context.Context, id int, providerTenantID int) error {
	if err := requireTenant(providerTenantID); err != nil {
		return err
	}
	s.logger.Infow("Deactivating SLA grant", "grant_id", id, "provider_tenant_id", providerTenantID)
	return s.repo.DeactivateGrant(ctx, id, providerTenantID)
}

func (s *Service) ListProvidedDefinitions(ctx context.Context, customerTenantID int) ([]*ProvidedDefinition, error) {
	if err := requireTenant(customerTenantID); err != nil {
		return nil, err
	}
	return s.repo.ListProvidedDefinitions(ctx, customerTenantID)
}

// violationTransitionMatrix 违约归责状态机合法迁移矩阵（D-SLA-8，方案 §13.1）。
// accepted/waived 为终态（无出边）；pending→disputed 由 customer 发起，
// disputed→accepted/waived 裁决（权限码 sla:violation:settle）。
var violationTransitionMatrix = map[string][]string{
	"pending":  {"accepted", "disputed", "waived"},
	"disputed": {"accepted", "waived"},
}

// TransitionViolationResolutionState 违约归责状态机：
// 矩阵校验 + reason 必填校验（disputed/waived）+ settled 锁定 + CAS 并发冲突拒绝。
func (s *Service) TransitionViolationResolutionState(ctx context.Context, id, tenantID int, target, reason string, actorID int) error {
	if err := requireTenant(tenantID); err != nil {
		return err
	}
	switch target {
	case "accepted", "disputed", "waived":
	default:
		return common.NewBusinessError(common.ParamErrorCode, "无效的归责状态目标", "")
	}
	if (target == "disputed" || target == "waived") && strings.TrimSpace(reason) == "" {
		return common.NewBusinessError(common.ParamErrorCode, "该状态必须提供 reason", "")
	}

	current, settled, err := s.repo.GetViolationResolutionState(ctx, id, tenantID)
	if err != nil {
		return err
	}
	if settled {
		return common.NewBusinessError(common.ParamErrorCode, "该违约已进结算快照，状态已锁定", "")
	}
	allowed := false
	for _, t := range violationTransitionMatrix[current] {
		if t == target {
			allowed = true
			break
		}
	}
	if !allowed {
		return common.NewBusinessError(common.ParamErrorCode,
			fmt.Sprintf("非法状态迁移: %s → %s（终态不可逆）", current, target), "")
	}

	affected, err := s.repo.TransitionViolationResolutionState(ctx, id, tenantID, current, target, reason, actorID)
	if err != nil {
		return err
	}
	if affected == 0 {
		return common.NewBusinessError(common.ParamErrorCode, "状态已被并发修改，请刷新后重试", "")
	}
	s.logger.Infow("SLA violation resolution state transitioned",
		"id", id, "from", current, "to", target, "actor", actorID)
	return nil
}

// MSP SLA Contracts（D-SLA-4）

func (s *Service) CreateContract(ctx context.Context, c *MSPContract) (*MSPContract, error) {
	if err := requireTenant(c.TenantID); err != nil {
		return nil, err
	}
	s.logger.Infow("Creating MSP SLA contract",
		"contract_no", c.ContractNo, "customer_tenant_id", c.CustomerTenantID, "bindings", len(c.Bindings))
	return s.repo.CreateContract(ctx, c)
}

func (s *Service) ListContracts(ctx context.Context, providerTenantID int, filters map[string]interface{}) ([]*MSPContract, error) {
	if err := requireTenant(providerTenantID); err != nil {
		return nil, err
	}
	return s.repo.ListContracts(ctx, providerTenantID, filters)
}

func (s *Service) GetContract(ctx context.Context, id int, providerTenantID int) (*MSPContract, error) {
	if err := requireTenant(providerTenantID); err != nil {
		return nil, err
	}
	return s.repo.GetContract(ctx, id, providerTenantID)
}

func (s *Service) UpdateContract(ctx context.Context, c *MSPContract) (*MSPContract, error) {
	if err := requireTenant(c.TenantID); err != nil {
		return nil, err
	}
	s.logger.Infow("Updating MSP SLA contract", "id", c.ID, "status", c.Status)
	return s.repo.UpdateContract(ctx, c)
}

// SLA Compliance and Monitoring

// SLAComplianceResult 返回值
type SLAComplianceResult struct {
	TicketID                int        `json:"ticketId"`
	TenantID                int        `json:"tenantId"`
	Found                   bool       `json:"found"`
	CreatedAt               *time.Time `json:"createdAt,omitempty"`
	FirstResponseAt         *time.Time `json:"firstResponseAt,omitempty"`
	ResolvedAt              *time.Time `json:"resolvedAt,omitempty"`
	ActualResponseMinutes   float64    `json:"actualResponseMinutes"`
	ActualResolutionMinutes float64    `json:"actualResolutionMinutes"`
	Compliant               bool       `json:"compliant"`
	Message                 string     `json:"message"`
}

// CheckSLACompliance P1-07 修复：真正计算 actual_response_minutes。
// 阻断3 修复：合规判定必须基于 SLA 截止时间，而非"是否已首次响应"。
// 旧逻辑 `res.Compliant = !firstResponseAt.IsZero()` 把"已响应"等同于"合规"，
// 导致超时响应的工单仍被判为合规；同时未解决工单永远判为"不合规"，
// 与 SLA 定义中"未到截止时间即合规"的语义相悖。
func (s *Service) CheckSLACompliance(ctx context.Context, ticketID int, tenantID int) (*SLAComplianceResult, error) {
	s.logger.Infow("Checking SLA Compliance", "ticketID", ticketID)
	createdAt, firstResponseAt, resolvedAt, slaResponseDeadline, slaResolutionDeadline, found, err := s.repo.GetTicketSLA(ctx, ticketID, tenantID)
	if err != nil {
		return nil, err
	}
	res := &SLAComplianceResult{
		TicketID: ticketID,
		TenantID: tenantID,
		Found:    found,
	}
	if !found {
		res.Message = "工单不存在或不属于该租户"
		return res, nil
	}
	res.CreatedAt = &createdAt
	if !firstResponseAt.IsZero() {
		t := firstResponseAt
		res.FirstResponseAt = &t
		res.ActualResponseMinutes = firstResponseAt.Sub(createdAt).Minutes()
	}
	if !resolvedAt.IsZero() {
		t := resolvedAt
		res.ResolvedAt = &t
		res.ActualResolutionMinutes = resolvedAt.Sub(createdAt).Minutes()
	}

	// 合规判定：
	// 1) 响应合规：已首次响应 且 响应时间 <= slaResponseDeadline（deadline 未配置时视为合规）
	// 2) 解决合规：已解决 且 解决时间 <= slaResolutionDeadline（deadline 未配置时视为合规）
	// 3) 仍处于处理中（未解决）：若当前时间未超过 slaResolutionDeadline，则视为临时合规
	responseCompliant := true
	resolutionCompliant := true

	if !slaResponseDeadline.IsZero() {
		// 已配置响应截止时间
		if firstResponseAt.IsZero() {
			// 尚未响应：截止时间已过则违规
			responseCompliant = time.Now().Before(slaResponseDeadline)
		} else {
			// 已响应：比对响应时间与截止时间
			responseCompliant = !firstResponseAt.After(slaResponseDeadline)
		}
	}

	if !slaResolutionDeadline.IsZero() {
		// 已配置解决截止时间
		if resolvedAt.IsZero() {
			// 尚未解决：截止时间已过则违规
			resolutionCompliant = time.Now().Before(slaResolutionDeadline)
		} else {
			// 已解决：比对解决时间与截止时间
			resolutionCompliant = !resolvedAt.After(slaResolutionDeadline)
		}
	}

	res.Compliant = responseCompliant && resolutionCompliant
	switch {
	case !res.Compliant:
		res.Message = fmt.Sprintf("SLA 违规：响应合规=%v 解决合规=%v", responseCompliant, resolutionCompliant)
	case !firstResponseAt.IsZero() && !resolvedAt.IsZero():
		res.Message = fmt.Sprintf("SLA 计时正常，首次响应 %.1f 分钟，解决 %.1f 分钟",
			res.ActualResponseMinutes, res.ActualResolutionMinutes)
	case !firstResponseAt.IsZero():
		res.Message = fmt.Sprintf("SLA 计时正常，首次响应 %.1f 分钟，待解决",
			res.ActualResponseMinutes)
	default:
		res.Message = "SLA 计时正常，等待首次响应"
	}
	return res, nil
}

// Violations

func (s *Service) GetSLAViolations(ctx context.Context, tenantID int, page, size int, filters map[string]interface{}) ([]*SLAViolation, int, error) {
	return s.repo.ListViolations(ctx, tenantID, page, size, filters)
}

func (s *Service) UpdateSLAViolationStatus(ctx context.Context, id int, isResolved bool, notes string, tenantID int) (*SLAViolation, error) {
	s.logger.Infow("Updating SLA Violation status", "id", id, "isResolved", isResolved)
	err := s.repo.UpdateViolationStatus(ctx, id, isResolved, notes, tenantID)
	if err != nil {
		return nil, err
	}
	// Return updated violation - fetch from repo
	violations, _, err := s.repo.ListViolations(ctx, tenantID, 1, 1, map[string]interface{}{"id": id})
	if err != nil || len(violations) == 0 {
		return nil, err
	}
	return violations[0], nil
}

// Metrics

func (s *Service) GetSLAMetrics(ctx context.Context, tenantID int, filters map[string]interface{}) ([]*SLAMetric, error) {
	return s.repo.GetMetrics(ctx, tenantID, filters)
}

func (s *Service) GetSLAMonitoring(ctx context.Context, tenantID int, start, end time.Time) (*SLAMonitoringData, error) {
	if tenantID <= 0 {
		return nil, ErrTenantRequired
	}
	if end.IsZero() {
		end = time.Now().UTC()
	}
	if start.IsZero() {
		start = end.Add(-DefaultMonitoringWindow)
	}
	if !end.After(start) {
		return nil, ErrInvalidWindow
	}

	res, err := s.repo.GetSLAMonitoring(ctx, tenantID, start, end)
	if err != nil {
		// 原始错误只进结构化日志，响应体不得携带 SQL/驱动细节。
		s.logger.Errorw("failed to load SLA monitoring data",
			"tenantId", tenantID,
			"startTime", start,
			"endTime", end,
			"error", err,
		)
		return nil, err
	}
	return res, nil
}

// SLAPerformanceQuery 是按维度聚合绩效的查询条件。
type SLAPerformanceQuery struct {
	Dimension   string
	Start       time.Time
	End         time.Time
	ServiceType string
	Priority    string
	Page        int
	PageSize    int
}

// SLAPerformanceResult 是分页后的绩效结果。Total 是符合过滤条件的分组行数；
// Truncated 为 true 表示窗口内工单数命中扫描上限，分组结果不完整。
type SLAPerformanceResult struct {
	Items     []*SLAPerformanceRow
	Total     int
	Page      int
	PageSize  int
	Truncated bool
}

// ListSLAPerformance 校验维度与窗口，聚合后做服务端分页。
func (s *Service) ListSLAPerformance(ctx context.Context, tenantID int, q SLAPerformanceQuery) (*SLAPerformanceResult, error) {
	if tenantID <= 0 {
		return nil, ErrTenantRequired
	}
	if q.Dimension != SLADimensionServiceType && q.Dimension != SLADimensionPriority {
		return nil, ErrInvalidDimension
	}
	if q.End.IsZero() {
		q.End = time.Now().UTC()
	}
	if q.Start.IsZero() {
		q.Start = q.End.Add(-DefaultMonitoringWindow)
	}
	if !q.End.After(q.Start) {
		return nil, ErrInvalidWindow
	}

	page := q.Page
	if page <= 0 {
		page = 1
	}
	pageSize := q.PageSize
	if pageSize <= 0 {
		pageSize = 20
	}
	if pageSize > 200 {
		pageSize = 200
	}

	rows, truncated, err := s.repo.ListSLAPerformance(ctx, tenantID, q.Dimension, q.Start, q.End, q.ServiceType, q.Priority)
	if err != nil {
		s.logger.Errorw("failed to load SLA performance rows",
			"tenantId", tenantID,
			"dimension", q.Dimension,
			"error", err,
		)
		return nil, err
	}

	total := len(rows)
	start := (page - 1) * pageSize
	if start > total {
		start = total
	}
	end := start + pageSize
	if end > total {
		end = total
	}

	return &SLAPerformanceResult{
		Items:     rows[start:end],
		Total:     total,
		Page:      page,
		PageSize:  pageSize,
		Truncated: truncated,
	}, nil
}

// Alert Rules

func (s *Service) CreateAlertRule(ctx context.Context, rule *SLAAlertRule) (*SLAAlertRule, error) {
	return s.repo.CreateAlertRule(ctx, rule)
}

func (s *Service) GetAlertRule(ctx context.Context, id int, tenantID int) (*SLAAlertRule, error) {
	return s.repo.GetAlertRule(ctx, id, tenantID)
}

func (s *Service) ListAlertRules(ctx context.Context, tenantID int, filters map[string]interface{}) ([]*SLAAlertRule, error) {
	return s.repo.ListAlertRules(ctx, tenantID, filters)
}

func (s *Service) UpdateAlertRule(ctx context.Context, rule *SLAAlertRule) (*SLAAlertRule, error) {
	return s.repo.UpdateAlertRule(ctx, rule)
}

func (s *Service) DeleteAlertRule(ctx context.Context, id int, tenantID int) error {
	return s.repo.DeleteAlertRule(ctx, id, tenantID)
}

// SLAAlertHistoryDefaultCooldownMinutes DDD 层默认告警抑制间隔（分钟）
//
// 与 service.SLAAlertService 的 defaultAlertCooldownMinutes 保持一致。
// 同一 (ticket_id, alert_rule_id) 在窗口内只保留一条未解决告警。
const SLAAlertHistoryDefaultCooldownMinutes = 15

func (s *Service) GetAlertHistory(ctx context.Context, tenantID int, page, size int, filters map[string]interface{}) ([]*SLAAlertHistory, int, error) {
	histories, total, err := s.repo.ListAlertHistory(ctx, tenantID, page, size, filters)
	if err != nil {
		return nil, 0, err
	}
	// 为每条 history 计算 cooldown 字段（未解决的告警仍处于冷却窗口时返回剩余秒数）
	now := time.Now()
	for _, h := range histories {
		h.CooldownMinutes = SLAAlertHistoryDefaultCooldownMinutes
		if h.ResolvedAt == nil {
			elapsed := now.Sub(h.CreatedAt)
			cooldownDur := time.Duration(SLAAlertHistoryDefaultCooldownMinutes) * time.Minute
			if elapsed < cooldownDur {
				remaining := cooldownDur - elapsed
				h.CooldownRemainingSeconds = int(remaining.Seconds())
				h.SuppressedByCooldown = true
			} else {
				h.CooldownRemainingSeconds = 0
				h.SuppressedByCooldown = false
			}
		} else {
			h.CooldownRemainingSeconds = 0
			h.SuppressedByCooldown = false
		}
	}
	return histories, total, nil
}

// GetSLAStats 获取SLA统计信息
func (s *Service) GetSLAStats(ctx context.Context, tenantID int) (map[string]interface{}, error) {
	// 获取总SLA定义数
	definitions, _, err := s.repo.ListDefinitions(ctx, tenantID, 1, 1000)
	if err != nil {
		return nil, err
	}

	// 获取活跃SLA定义数
	activeCount := 0
	for _, def := range definitions {
		if def.IsActive {
			activeCount++
		}
	}

	// 获取总违规数
	violations, _, err := s.repo.ListViolations(ctx, tenantID, 1, 1000, map[string]interface{}{})
	if err != nil {
		return nil, err
	}

	// 获取待处理违规数
	openViolations := 0
	for _, v := range violations {
		if !v.IsResolved {
			openViolations++
		}
	}

	// 计算合规率 - 基于总工单数而非违规数
	// 从 repository 获取总工单数和达标工单数来计算
	var complianceRate float64
	totalTickets, metSLA, err := s.repo.GetTicketStats(ctx, tenantID)
	if err != nil || totalTickets == 0 {
		// 如果无法获取工单数据，回退到基于违规的计算；
		// 完全无样本时诚实返回 0（暂无数据），不伪装成 100% 合规。
		if len(violations) > 0 {
			complianceRate = float64(len(violations)-openViolations) / float64(len(violations)) * 100
		} else {
			complianceRate = 0
		}
	} else {
		complianceRate = float64(metSLA) / float64(totalTickets) * 100
	}
	// 保留一位小数，避免前端展示原始浮点噪声
	complianceRate = math.Round(complianceRate*10) / 10

	return map[string]interface{}{
		"total_definitions":       len(definitions),
		"active_definitions":      activeCount,
		"total_violations":        len(violations),
		"open_violations":         openViolations,
		"overall_compliance_rate": complianceRate,
	}, nil
}

// GetComplianceReport generates SLA compliance report for a date range
func (s *Service) GetComplianceReport(ctx context.Context, tenantID int, startDate, endDate time.Time) (*dto.SLAComplianceReport, error) {
	s.logger.Infow("Generating SLA compliance report", "tenantID", tenantID, "startDate", startDate, "endDate", endDate)

	total, met, violated, avgResp, avgRes, err := s.repo.GetComplianceReportData(ctx, tenantID, startDate, endDate)
	if err != nil {
		return nil, err
	}

	complianceRate := 0.0
	if total > 0 {
		complianceRate = float64(met) / float64(total) * 100
	}

	return &dto.SLAComplianceReport{
		TotalTickets:      total,
		MetSLA:            met,
		ViolatedSLA:       violated,
		ComplianceRate:    complianceRate,
		AvgResponseTime:   avgResp,
		AvgResolutionTime: avgRes,
		ReportPeriod: dto.SLAReportPeriod{
			StartDate: startDate.Format(time.RFC3339),
			EndDate:   endDate.Format(time.RFC3339),
		},
	}, nil
}

// ===== P2-2 结算报表（D-SLA-5/D-SLA-9，方案 §4.5/§13）=====

// GetSettlementReport provider 视角结算报表：白名单口径（§13.2）+ 双口径对账（§13.5）
// + matched_layer 过滤与合同基准重算双保险（§13.6）。归属校验在 repo 取数前完成。
func (s *Service) GetSettlementReport(ctx context.Context, providerTenantID, customerTenantID int, from, to time.Time) (*dto.SLASettlementReport, error) {
	if err := requireTenant(providerTenantID); err != nil {
		return nil, err
	}
	if err := requireTenant(customerTenantID); err != nil {
		return nil, err
	}
	if !to.After(from) {
		return nil, ErrInvalidWindow
	}
	scope, err := s.repo.GetSettlementScope(ctx, providerTenantID, customerTenantID, from, to)
	if err != nil {
		return nil, err
	}
	return s.buildSettlementReport(ctx, scope, from, to), nil
}

// LockSettlementSnapshot 出快照锁定（§13.3）：把报告范围内合同域违约置 settled_flag=true，
// 锁定后状态机拒绝对其的任何变更（新争议走下一周期）；幂等，返回锁定行数。
func (s *Service) LockSettlementSnapshot(ctx context.Context, providerTenantID, customerTenantID int, from, to time.Time) (int, error) {
	if err := requireTenant(providerTenantID); err != nil {
		return 0, err
	}
	if err := requireTenant(customerTenantID); err != nil {
		return 0, err
	}
	if !to.After(from) {
		return 0, ErrInvalidWindow
	}
	return s.repo.LockSettlementViolations(ctx, providerTenantID, customerTenantID, from, to)
}

// settlementAdjudication 单工单归责聚合（合同域违约；§13.2 ⑤ 口径）。
type settlementAdjudication struct {
	state       string // accepted|disputed|pending|waived|none
	hasAccepted bool
	hasDisputed bool
	hasPending  bool
	hasWaived   bool
	lastID      int
}

// isInternalDomainLayer 违约来源是否为客户内部域（L2 等级策略 / L3a/b/c 自建定义）。
// 空值（存量行）不预排除——交由"合同基准重算"兜底（§13.6 双保险）。
func isInternalDomainLayer(layer string) bool {
	return layer == "L2" || strings.HasPrefix(layer, "L3")
}

// deadlineBreached 按给定截止时间判定工单是否超时（未动作且已过截止 = 违约）。
func deadlineBreached(firstResponseAt, resolvedAt, respDeadline, resDeadline, now time.Time) bool {
	if !respDeadline.IsZero() {
		if !firstResponseAt.IsZero() {
			if firstResponseAt.After(respDeadline) {
				return true
			}
		} else if now.After(respDeadline) {
			return true
		}
	}
	if !resDeadline.IsZero() {
		if !resolvedAt.IsZero() {
			if resolvedAt.After(resDeadline) {
				return true
			}
		} else if now.After(resDeadline) {
			return true
		}
	}
	return false
}

func verdictLabel(breached bool) string {
	if breached {
		return "breached"
	}
	return "met"
}

// penaltyNumber 从合同 penalties（JSON）读取数值字段（容忍 float64/int/int64 解码）。
func penaltyNumber(m map[string]interface{}, keys ...string) float64 {
	for _, k := range keys {
		v, ok := m[k]
		if !ok {
			continue
		}
		switch n := v.(type) {
		case float64:
			return n
		case int:
			return float64(n)
		case int64:
			return float64(n)
		}
	}
	return 0
}

// buildSettlementReport 结算判定与组装（取数已在 repo 完成）。
func (s *Service) buildSettlementReport(ctx context.Context, scope *SettlementScope, from, to time.Time) *dto.SLASettlementReport {
	now := time.Now()
	covered := len(scope.CoveredTickets)
	report := &dto.SLASettlementReport{
		CustomerTenantID: scope.Contract.CustomerTenantID,
		CustomerName:     scope.CustomerName,
		ContractID:       scope.Contract.ID,
		ContractNo:       scope.Contract.ContractNo,
		ContractName:     scope.Contract.Name,
		Period: dto.SLAReportPeriod{
			StartDate: from.Format(time.RFC3339),
			EndDate:   to.Format(time.RFC3339),
		},
		CoveredTickets:   covered,
		ExemptTickets:    scope.ExemptCount,
		UncoveredTickets: scope.UncoveredCount,
		Rows:             make([]dto.SLASettlementRow, 0, covered),
	}

	// 违约归集（合同域；客户内部域单列排除计数——§13.6）
	aggs := make(map[int]*settlementAdjudication)
	for _, v := range scope.Violations {
		if isInternalDomainLayer(v.MatchedLayer) {
			report.ExcludedInternalViolations++
			continue
		}
		switch v.ResolutionState {
		case "accepted":
			report.AcceptedViolations++
		case "waived":
			report.WaivedViolations++
		case "disputed":
			report.DisputedViolations++
		default:
			report.PendingViolations++
		}
		agg := aggs[v.TicketID]
		if agg == nil {
			agg = &settlementAdjudication{state: "none"}
			aggs[v.TicketID] = agg
		}
		switch v.ResolutionState {
		case "accepted":
			agg.hasAccepted = true
		case "disputed":
			agg.hasDisputed = true
		case "waived":
			agg.hasWaived = true
		default:
			agg.hasPending = true
		}
		agg.lastID = v.ID
	}
	// 归责优先级：accepted > disputed > pending > waived（§13.2 ⑤：waived/未裁决 disputed 不计违约）
	for _, agg := range aggs {
		switch {
		case agg.hasAccepted:
			agg.state = "accepted"
		case agg.hasDisputed:
			agg.state = "disputed"
		case agg.hasPending:
			agg.state = "pending"
		case agg.hasWaived:
			agg.state = "waived"
		}
	}

	for _, t := range scope.CoveredTickets {
		def := scope.Bindings[t.TicketTypeID]
		if def == nil {
			def = scope.TypeAgnostic
		}
		if def == nil {
			continue // 理论不可达（覆盖判定保证至少一侧存在）；防御式跳过
		}
		respDL, resDL := s.repo.ComputeSLADeadlines(ctx, scope.Contract.CustomerTenantID, def.ResponseTime, def.ResolutionTime, def.BusinessHours, t.CreatedAt)
		contractBreached := deadlineBreached(t.FirstResponseAt, t.ResolvedAt, respDL, resDL, now)

		internalVerdict := "unknown"
		internalBreached := false
		if !t.SLAResponseDeadline.IsZero() || !t.SLAResolutionDeadline.IsZero() {
			internalBreached = deadlineBreached(t.FirstResponseAt, t.ResolvedAt, t.SLAResponseDeadline, t.SLAResolutionDeadline, now)
			internalVerdict = verdictLabel(internalBreached)
		}

		row := dto.SLASettlementRow{
			TicketID:        t.ID,
			TicketNumber:    t.TicketNumber,
			Title:           t.Title,
			TicketTypeID:    t.TicketTypeID,
			TicketTypeCode:  t.TicketTypeCode,
			CreatedAt:       t.CreatedAt.Format(time.RFC3339),
			ContractVerdict: verdictLabel(contractBreached),
			InternalVerdict: internalVerdict,
			DriftFlag:       internalVerdict != "unknown" && contractBreached != internalBreached,
			MatchedLayer:    t.SLAMatchedLayer,
		}
		if !t.FirstResponseAt.IsZero() {
			row.FirstResponseAt = t.FirstResponseAt.Format(time.RFC3339)
		}
		if !t.ResolvedAt.IsZero() {
			row.ResolvedAt = t.ResolvedAt.Format(time.RFC3339)
		}
		if agg := aggs[t.ID]; agg != nil {
			row.ViolationID = agg.lastID
			if agg.state != "none" {
				row.ViolationState = agg.state
			}
		}

		if contractBreached {
			report.ContractBreached++
			adjudication := "none"
			if agg := aggs[t.ID]; agg != nil {
				adjudication = agg.state
			}
			if adjudication == "waived" || adjudication == "disputed" {
				// §13.2 ⑤：waived / 未裁决 disputed 不计违约
				row.ExcludedReason = adjudication
			} else {
				report.SettlementBreaches++
			}
		}
		report.Rows = append(report.Rows, row)
	}

	report.ContractMet = covered - report.ContractBreached
	if covered > 0 {
		report.ComplianceRate = math.Round(float64(covered-report.SettlementBreaches)/float64(covered)*1000) / 10
	}

	// 信用额（§13.3：按 penalties 计算；credit_rate=单次违约信用额，cap=上限）
	report.Credit = dto.SLASettlementCredit{
		Currency:    scope.Contract.Currency,
		BreachUnits: report.SettlementBreaches,
	}
	rate := penaltyNumber(scope.Contract.Penalties, "credit_rate", "creditRate")
	capV := penaltyNumber(scope.Contract.Penalties, "cap")
	report.Credit.CreditRate = rate
	report.Credit.Cap = capV
	if rate > 0 {
		amount := float64(report.SettlementBreaches) * rate
		if capV > 0 && amount > capV {
			amount = capV
		}
		report.Credit.Amount = amount
		report.Credit.Note = "信用额 = 计违约数 × credit_rate（受 cap 上限约束）"
	} else {
		report.Credit.Note = "合同未配置 credit_rate，信用额按 0 计"
	}

	return report
}
