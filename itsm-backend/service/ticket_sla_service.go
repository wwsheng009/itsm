package service

import (
	"context"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"itsm-backend/common"
	"itsm-backend/ent"
	"itsm-backend/ent/sladefinition"
	"itsm-backend/ent/sladefinitiongrant"
	"itsm-backend/ent/tenant"
	"itsm-backend/ent/ticket"

	"go.uber.org/zap"
)

// TicketSLAServiceInterface 工单SLA服务接口
type TicketSLAServiceInterface interface {
	// GetTicketSLAInfo 获取工单SLA信息
	GetTicketSLAInfo(ctx context.Context, ticketID int, tenantID int) (*TicketSLAInfoResult, error)
	// GetOverdueTickets 获取逾期工单
	GetOverdueTickets(ctx context.Context, tenantID int) ([]*ent.Ticket, error)
	// GetTicketStats 获取工单统计
	GetTicketStats(ctx context.Context, tenantID int) (*TicketStats, error)
	// CalculateSLADeadline 计算SLA截止时间
	CalculateSLADeadline(ctx context.Context, tenantID int, ticketType, priority string) (*SLADeadlineResult, error)
	// CalculateSLADeadlineFromRequest 根据请求参数计算SLA截止时间（包含SLADefinitionID）
	// v1.11 方案 §4.2：增加 ticketTypeID（grant 类型绑定匹配）与 ticketTypeCode（L3b 动态类型匹配）参数。
	CalculateSLADeadlineFromRequest(ctx context.Context, tenantID int, ticketType, priority string, ticketTypeID int, ticketTypeCode string) (*SLADeadlineResult, error)
	// AdjustToBusinessHours 调整时间到工作时间
	AdjustToBusinessHours(t time.Time) time.Time
}

// TicketSLAInfoResult 工单SLA信息（计算结果）
type TicketSLAInfoResult struct {
	TicketID           int        `json:"ticketId"`
	TicketNumber       string     `json:"ticketNumber"`
	Priority           string     `json:"priority"`
	TicketType         string     `json:"ticketType"`
	ResponseDeadline   *time.Time `json:"responseDeadline"`
	ResolutionDeadline *time.Time `json:"resolutionDeadline"`
	ResponseTimeUsed   int        `json:"responseTimeUsed"`   // 分钟
	ResolutionTimeUsed int        `json:"resolutionTimeUsed"` // 分钟
	ResponseBreached   bool       `json:"responseBreached"`
	ResolutionBreached bool       `json:"resolutionBreached"`
	SLAStatus          string     `json:"slaStatus"` // ok, warning, breached
}

// SLADeadlineResult SLA截止时间计算结果
type SLADeadlineResult struct {
	SLADefinitionID    int
	ResponseDeadline   *time.Time
	ResolutionDeadline *time.Time
	BusinessHoursOnly  bool
	// MatchedLayer 匹配层级（方案 §4.2 v1.11）：L1/L2/L3a/L3b/L3c/L4。
	// 供报表区分来源基准（L1=合同域 / L2=等级策略 / L3=客户自建 / L4=未绑定）。
	MatchedLayer string
}

// TicketStats 工单统计
type TicketStats struct {
	TotalTickets      int `json:"totalTickets"`
	OpenTickets       int `json:"openTickets"`
	InProgressTickets int `json:"inProgressTickets"`
	ResolvedTickets   int `json:"resolvedTickets"`
	ClosedTickets     int `json:"closedTickets"`
	OverdueTickets    int `json:"overdueTickets"`
	BreachedTickets   int `json:"breachedTickets"`
}

// TicketSLAService 工单SLA服务
type TicketSLAService struct {
	client              *ent.Client
	logger              *zap.SugaredLogger
	nowFunc             func() time.Time
	policySvc           *SLAPolicyService
	unboundAlertEnabled bool
}

// NewTicketSLAService 创建工单SLA服务
func NewTicketSLAService(client *ent.Client, logger *zap.SugaredLogger) *TicketSLAService {
	return &TicketSLAService{
		client:              client,
		logger:              logger,
		nowFunc:             time.Now,
		policySvc:           NewSLAPolicyService(client),
		unboundAlertEnabled: slaUnboundAlertEnabledFromEnv(os.Getenv("SLA_UNBOUND_ALERT_ENABLED")),
	}
}

// withNowFunc overrides the clock used for deadline calculation. Intended for tests.
func (s *TicketSLAService) withNowFunc(f func() time.Time) *TicketSLAService {
	s.nowFunc = f
	return s
}

// SetUnboundAlertEnabled 覆盖 L4 未绑定告警开关（测试/灰度用；env 默认关，方案 §4.2）。
func (s *TicketSLAService) SetUnboundAlertEnabled(enabled bool) *TicketSLAService {
	s.unboundAlertEnabled = enabled
	return s
}

// slaUnboundAlertEnabledFromEnv 解析 SLA_UNBOUND_ALERT_ENABLED：
// 显式 true/1/yes/on 开；其余（含未设置）关——方案 §4.2 "默认 false"。
func slaUnboundAlertEnabledFromEnv(raw string) bool {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "true", "1", "yes", "on":
		return true
	default:
		return false
	}
}

// GetTicketSLAInfo 获取工单SLA信息
func (s *TicketSLAService) GetTicketSLAInfo(ctx context.Context, ticketID int, tenantID int) (*TicketSLAInfoResult, error) {
	// 查询工单
	t, err := s.client.Ticket.Query().
		Where(ticket.IDEQ(ticketID), ticket.TenantID(tenantID), ticket.DeletedAtIsNil()).
		Only(ctx)
	if err != nil {
		s.logger.Errorw("Failed to find ticket", "ticketID", ticketID, "error", err)
		return nil, err
	}

	// 计算已用时间
	responseTimeUsed := int(time.Since(t.CreatedAt).Minutes())
	resolutionTimeUsed := int(time.Since(t.CreatedAt).Minutes())

	// 如果已有首次响应时间或解决时间，使用实际时间
	if !t.FirstResponseAt.IsZero() {
		responseTimeUsed = int(t.FirstResponseAt.Sub(t.CreatedAt).Minutes())
	}
	if !t.ResolvedAt.IsZero() {
		resolutionTimeUsed = int(t.ResolvedAt.Sub(t.CreatedAt).Minutes())
	}

	// 获取SLA定义
	slaDef, err := s.getSLADefinition(ctx, tenantID, t.Type, t.Priority)
	if err != nil {
		s.logger.Warnw("Failed to get SLA definition", "error", err)
		// 返回没有SLA信息的结果
		return &TicketSLAInfoResult{
			TicketID:           t.ID,
			TicketNumber:       t.TicketNumber,
			Priority:           t.Priority,
			TicketType:         t.Type,
			ResponseTimeUsed:   responseTimeUsed,
			ResolutionTimeUsed: resolutionTimeUsed,
			SLAStatus:          "unknown",
		}, nil
	}

	// 计算截止时间。
	// 阻断7/C-8 修复：统一读取 slaDef.BusinessHours 配置，与 CalculateSLADeadlineFromRequest
	// 使用同一口径，消除"建单落库调整 / 查询展示不调整"的两路径结论相反问题。
	var responseDeadline, resolutionDeadline *time.Time
	if slaDef.ResponseTime > 0 {
		respDeadline := s.calculateDeadlineWithBusinessHours(ctx, t.TenantID, t.CreatedAt, slaDef.ResponseTime, slaDef.BusinessHours)
		responseDeadline = &respDeadline
	}
	if slaDef.ResolutionTime > 0 {
		resDeadline := s.calculateDeadlineWithBusinessHours(ctx, t.TenantID, t.CreatedAt, slaDef.ResolutionTime, slaDef.BusinessHours)
		resolutionDeadline = &resDeadline
	}

	// 判断是否违规
	responseBreached := false
	resolutionBreached := false
	slaStatus := "ok"

	if responseDeadline != nil && time.Now().After(*responseDeadline) {
		responseBreached = true
		slaStatus = "breached"
	}

	if resolutionDeadline != nil && time.Now().After(*resolutionDeadline) {
		resolutionBreached = true
		slaStatus = "breached"
	}

	// 检查警告状态（默认30分钟警告）
	if !responseBreached && !resolutionBreached && responseDeadline != nil {
		timeLeft := time.Until(*responseDeadline)
		if timeLeft.Minutes() < 30 {
			slaStatus = "warning"
		}
	}

	return &TicketSLAInfoResult{
		TicketID:           t.ID,
		TicketNumber:       t.TicketNumber,
		Priority:           t.Priority,
		TicketType:         t.Type,
		ResponseDeadline:   responseDeadline,
		ResolutionDeadline: resolutionDeadline,
		ResponseTimeUsed:   responseTimeUsed,
		ResolutionTimeUsed: resolutionTimeUsed,
		ResponseBreached:   responseBreached,
		ResolutionBreached: resolutionBreached,
		SLAStatus:          slaStatus,
	}, nil
}

// GetOverdueTickets 获取逾期工单
func (s *TicketSLAService) GetOverdueTickets(ctx context.Context, tenantID int) ([]*ent.Ticket, error) {
	// SLA 截止时间在建单时已落库，直接由数据库筛选，避免逐工单查询 SLA 定义的 N+1。
	now := time.Now()
	tickets, err := s.client.Ticket.Query().
		Where(
			ticket.TenantID(tenantID),
			ticket.DeletedAtIsNil(),
			ticket.StatusNEQ(common.TicketStatusClosed),
			ticket.StatusNEQ(common.TicketStatusResolved),
			ticket.SLAResolutionDeadlineNotNil(),
			ticket.SLAResolutionDeadlineLT(now),
		).
		All(ctx)
	if err != nil {
		s.logger.Errorw("Failed to query tickets", "error", err)
		return nil, err
	}

	return tickets, nil
}

// GetTicketStats 获取工单统计
func (s *TicketSLAService) GetTicketStats(ctx context.Context, tenantID int) (*TicketStats, error) {
	stats := &TicketStats{}

	// 统计总数
	total, err := s.client.Ticket.Query().
		Where(ticket.TenantID(tenantID), ticket.DeletedAtIsNil()).
		Count(ctx)
	if err != nil {
		return nil, err
	}
	stats.TotalTickets = total

	// 统计各状态数量
	openCount, err := s.client.Ticket.Query().
		Where(ticket.TenantID(tenantID), ticket.DeletedAtIsNil(), ticket.Status(common.TicketStatusOpen)).
		Count(ctx)
	if err != nil {
		return nil, err
	}
	stats.OpenTickets = openCount

	inProgressCount, err := s.client.Ticket.Query().
		Where(ticket.TenantID(tenantID), ticket.DeletedAtIsNil(), ticket.Status(common.TicketStatusInProgress)).
		Count(ctx)
	if err != nil {
		return nil, err
	}
	stats.InProgressTickets = inProgressCount

	resolvedCount, err := s.client.Ticket.Query().
		Where(ticket.TenantID(tenantID), ticket.DeletedAtIsNil(), ticket.Status(common.TicketStatusResolved)).
		Count(ctx)
	if err != nil {
		return nil, err
	}
	stats.ResolvedTickets = resolvedCount

	closedCount, err := s.client.Ticket.Query().
		Where(ticket.TenantID(tenantID), ticket.DeletedAtIsNil(), ticket.Status(common.TicketStatusClosed)).
		Count(ctx)
	if err != nil {
		return nil, err
	}
	stats.ClosedTickets = closedCount

	// 统计逾期工单
	overdueTickets, err := s.GetOverdueTickets(ctx, tenantID)
	if err != nil {
		return nil, err
	}
	stats.OverdueTickets = len(overdueTickets)

	return stats, nil
}

// CalculateSLADeadline 计算SLA截止时间
func (s *TicketSLAService) CalculateSLADeadline(ctx context.Context, tenantID int, ticketType, priority string) (*SLADeadlineResult, error) {
	slaDef, err := s.getSLADefinition(ctx, tenantID, ticketType, priority)
	if err != nil {
		return nil, err
	}

	result := &SLADeadlineResult{}

	now := time.Now()

	if slaDef.ResponseTime > 0 {
		respDeadline := s.calculateDeadlineWithBusinessHours(ctx, tenantID, now, slaDef.ResponseTime, slaDef.BusinessHours)
		result.ResponseDeadline = &respDeadline
	}

	if slaDef.ResolutionTime > 0 {
		resDeadline := s.calculateDeadlineWithBusinessHours(ctx, tenantID, now, slaDef.ResolutionTime, slaDef.BusinessHours)
		result.ResolutionDeadline = &resDeadline
	}

	return result, nil
}

// getSLADefinition 获取SLA定义
func (s *TicketSLAService) getSLADefinition(ctx context.Context, tenantID int, ticketType, priority string) (*ent.SLADefinition, error) {
	// 尝试查找匹配的类型和优先级
	sla, err := s.client.SLADefinition.Query().
		Where(
			sladefinition.TenantID(tenantID),
			sladefinition.ServiceType(ticketType),
			sladefinition.Priority(priority),
			sladefinition.IsActive(true),
		).
		Only(ctx)
	if err == nil {
		return sla, nil
	}

	// 如果没有精确匹配，尝试只匹配类型
	sla, err = s.client.SLADefinition.Query().
		Where(
			sladefinition.TenantID(tenantID),
			sladefinition.ServiceType(ticketType),
			sladefinition.PriorityIsNil(),
			sladefinition.IsActive(true),
		).
		Only(ctx)
	if err == nil {
		return sla, nil
	}

	// 如果没有匹配的类型，返回默认SLA
	return &ent.SLADefinition{
		ResponseTime:   60,  // 默认1小时响应
		ResolutionTime: 480, // 默认8小时解决
	}, nil
}

// calculateDeadlineWithBusinessHours applies an SLA definition's configured
// business calendar. An empty calendar intentionally preserves 24x7 SLA time.
//
// R4-b 2026-09-16：接受 ctx + tenantID，按租户时区注入 loc（覆盖 SLA 配置里的 time_zone 缺省值），
// 与 timer_cron 的 LoadTenantLocation 口径一致；上下文不可用时回退 ResolveLocation("")=Asia/Shanghai。
func (s *TicketSLAService) calculateDeadlineWithBusinessHours(ctx context.Context, tenantID int, startTime time.Time, durationMinutes int, businessHours map[string]interface{}) time.Time {
	if len(businessHours) == 0 || durationMinutes <= 0 {
		return startTime.Add(time.Duration(durationMinutes) * time.Minute)
	}
	cfg := parseBusinessHoursConfig(businessHours)
	if cfg.loc == nil {
		cfg.loc = LoadTenantLocation(ctx, s.client, tenantID)
	}
	return addBusinessMinutes(startTime, durationMinutes, cfg)
}

// AdjustToBusinessHours 调整到工作时间（公开方法，供外部调用）。
// 阻断7 说明：此方法保留向后兼容，仅用于"把某个时刻对齐到最近的工作时段起点"，
// 不能用于计算 SLA 截止时间（截止时间必须用 calculateDeadline/addBusinessMinutes）。
func (s *TicketSLAService) AdjustToBusinessHours(t time.Time) time.Time {
	return adjustToBusinessHoursStart(t, defaultBusinessHoursConfig())
}

// businessHoursConfig 业务时间配置。
// 默认：周一至周五 9:00-18:00，无节假日。
type businessHoursConfig struct {
	workDays  map[time.Weekday]bool // 工作日集合
	startHour int                   // 工作时段起始小时（含），9 表示 09:00
	startMin  int                   // 工作时段起始分钟
	endHour   int                   // 工作时段结束小时（不含），18 表示 18:00
	endMin    int                   // 工作时段结束分钟
	holidays  map[string]bool       // 节假日集合，格式 "2006-01-02"
	is24x7    bool                  // 24×7 全天制：忽略工作日与起止时间，工时连续
	loc       *time.Location        // 时区：工作时间窗口按此时区计算；nil 时调用方补 LoadTenantLocation
}

// defaultBusinessHoursConfig 返回默认业务时间配置（周一至周五 9:00-18:00）。
// loc 故意留为 nil——上层 calculateDeadlineWithBusinessHours 会通过 LoadTenantLocation 补
// 租户时区，避免 time.Local 拖垮跨时区租户。
func defaultBusinessHoursConfig() businessHoursConfig {
	return businessHoursConfig{
		workDays: map[time.Weekday]bool{
			time.Monday: true, time.Tuesday: true, time.Wednesday: true,
			time.Thursday: true, time.Friday: true,
		},
		startHour: 9,
		endHour:   18,
		holidays:  map[string]bool{},
	}
}

// parseBusinessHoursConfig 从 SLADefinition.BusinessHours (map[string]interface{}) 解析配置。
// 配置格式参考 ent/schema/sla_policy.go 的 BusinessHoursConfig：
//
//	{ "work_days": [1,2,3,4,5], "start_time": "09:00", "end_time": "18:00",
//	  "time_zone": "Asia/Shanghai", "holiday_list": ["2026-01-01"],
//	  "is_24_7": false }
//
// 空或解析失败时返回默认配置（不阻断 SLA 计算）。
//
// R4-b 2026-09-16 修复：
//  1. 兼容旧键名（workdays/work_hour_start/work_hour_end/timezone/is_24_7），
//     不破坏历史已安装的 SLA 配置；
//  2. is_24_7=true 时把 start/end 当作 00:00-23:59 跨天窗口，工作日集合忽略，
//     避免 P1 模板声明 24×7 但被解析器 fallback 到 9-18 工时；
//  3. loc 字段兜底为 ResolveLocation("")=Asia/Shanghai，与 timer_cron 一致，
//     避免 time.Local 跨时区租户违约整体偏移（理想是用租户 TZ，下方 calculateDeadlineWithBusinessHours
//     接受 tenantID 时会再用 LoadTenantLocation 覆盖）。
func parseBusinessHoursConfig(raw map[string]interface{}) businessHoursConfig {
	cfg := defaultBusinessHoursConfig()
	if len(raw) == 0 {
		return cfg
	}

	// is_24_7：兼容 bool 与字符串。is_24_7=true 时**最后**短路为 00:00-23:59 全天制，
	// 必须放在所有 start_time/end_time 解析之后，否则会被覆盖回 9-18。
	if v, ok := raw["is_24_7"].(bool); ok {
		cfg.is24x7 = v
	} else if v, ok := raw["is_24_7"].(string); ok {
		cfg.is24x7 = v == "true" || v == "1"
	}

	// work_days：兼容 workdays / work_days
	parseDays := func() {
		if days, ok := raw["work_days"].([]interface{}); ok && len(days) > 0 {
			cfg.workDays = map[time.Weekday]bool{}
			dayMap := map[int]time.Weekday{
				1: time.Monday, 2: time.Tuesday, 3: time.Wednesday,
				4: time.Thursday, 5: time.Friday, 6: time.Saturday, 7: time.Sunday,
			}
			for _, d := range days {
				if dv, ok := d.(float64); ok {
					if wd, ok := dayMap[int(dv)]; ok {
						cfg.workDays[wd] = true
					}
				}
			}
		} else if days, ok := raw["workdays"].([]interface{}); ok && len(days) > 0 {
			cfg.workDays = map[time.Weekday]bool{}
			dayMap := map[int]time.Weekday{
				1: time.Monday, 2: time.Tuesday, 3: time.Wednesday,
				4: time.Thursday, 5: time.Friday, 6: time.Saturday, 7: time.Sunday,
			}
			for _, d := range days {
				if dv, ok := d.(float64); ok {
					if wd, ok := dayMap[int(dv)]; ok {
						cfg.workDays[wd] = true
					}
				}
			}
		}
	}
	parseDays()

	parseHM := func(s string) (int, int) {
		parts := strings.Split(s, ":")
		if len(parts) != 2 {
			return -1, -1
		}
		h, e1 := strconv.Atoi(parts[0])
		m, e2 := strconv.Atoi(parts[1])
		if e1 != nil || e2 != nil {
			return -1, -1
		}
		return h, m
	}
	// start_time / end_time：兼容 work_hour_start/work_hour_end 与 start_hour/end_hour
	if st, ok := raw["start_time"].(string); ok {
		if h, m := parseHM(st); h >= 0 {
			cfg.startHour, cfg.startMin = h, m
		}
	} else if h, ok := raw["start_hour"].(float64); ok {
		cfg.startHour = int(h)
	} else if h, ok := raw["work_hour_start"].(float64); ok {
		cfg.startHour = int(h)
	}
	if et, ok := raw["end_time"].(string); ok {
		if h, m := parseHM(et); h >= 0 {
			cfg.endHour, cfg.endMin = h, m
		}
	} else if h, ok := raw["end_hour"].(float64); ok {
		cfg.endHour = int(h)
	} else if h, ok := raw["work_hour_end"].(float64); ok {
		cfg.endHour = int(h)
	}
	if holidays, ok := raw["holiday_list"].([]interface{}); ok {
		for _, h := range holidays {
			if hs, ok := h.(string); ok {
				cfg.holidays[hs] = true
			}
		}
	}
	// 时区：兼容 "time_zone"（文档约定）与 "timezone"（DTO/旧模板）。
	if tz, ok := raw["time_zone"].(string); ok && tz != "" {
		if loc, e := time.LoadLocation(tz); e == nil {
			cfg.loc = loc
		}
	} else if tz, ok := raw["timezone"].(string); ok && tz != "" {
		if loc, e := time.LoadLocation(tz); e == nil {
			cfg.loc = loc
		}
	}
	// is_24_7 短路：放在所有解析之后，覆盖 start/end 与 work_days，
	// 让 24×7 模板（P1 紧急事件 / change_emergency）正确生效全天工时制。
	if cfg.is24x7 {
		cfg.workDays = map[time.Weekday]bool{
			time.Monday: true, time.Tuesday: true, time.Wednesday: true,
			time.Thursday: true, time.Friday: true, time.Saturday: true, time.Sunday: true,
		}
		cfg.startHour, cfg.startMin = 0, 0
		cfg.endHour, cfg.endMin = 23, 59
	}
	// loc 兜底：始终保证 cfg.loc 非 nil，避免调用方在 cfg.loc==nil 时退化到 time.Local。
	// 上层 calculateDeadlineWithBusinessHours(ctx, tenantID, ...) 会用 LoadTenantLocation
	// 覆盖此处的兜底值（Asia/Shanghai）为租户 TZ。
	if cfg.loc == nil {
		cfg.loc = ResolveLocation("")
	}
	return cfg
}

// isHoliday 判断给定日期是否为节假日。
func (c businessHoursConfig) isHoliday(t time.Time) bool {
	return c.holidays[t.Format("2006-01-02")]
}

// isWorkDay 判断给定日期是否为工作日（工作日集合 + 非节假日）。
func (c businessHoursConfig) isWorkDay(t time.Time) bool {
	if c.isHoliday(t) {
		return false
	}
	return c.workDays[t.Weekday()]
}

// workDayStart 返回 t 所在工作日的工时开始时刻。
func (c businessHoursConfig) workDayStart(t time.Time) time.Time {
	y, m, d := t.Date()
	return time.Date(y, m, d, c.startHour, c.startMin, 0, 0, t.Location())
}

// workDayEnd 返回 t 所在工作日的工时结束时刻。
func (c businessHoursConfig) workDayEnd(t time.Time) time.Time {
	y, m, d := t.Date()
	return time.Date(y, m, d, c.endHour, c.endMin, 0, 0, t.Location())
}

// nextWorkDayStart 返回 t 之后下一个工作日的工时开始时刻。
func (c businessHoursConfig) nextWorkDayStart(t time.Time) time.Time {
	next := t.AddDate(0, 0, 1)
	for !c.isWorkDay(next) {
		next = next.AddDate(0, 0, 1)
	}
	return c.workDayStart(next)
}

// adjustToBusinessHoursStart 把时刻 t 对齐到最近的工作时段起点。
// 若 t 已在工作时段内，返回 t；否则返回下一个工作时段起点。
func adjustToBusinessHoursStart(t time.Time, cfg businessHoursConfig) time.Time {
	// 跳过非工作日
	for !cfg.isWorkDay(t) {
		t = cfg.nextWorkDayStart(t)
	}
	dayStart := cfg.workDayStart(t)
	dayEnd := cfg.workDayEnd(t)
	if t.Before(dayStart) {
		return dayStart
	}
	if !t.Before(dayEnd) {
		// 当天工时已结束，跳到下一个工作日
		return cfg.nextWorkDayStart(t)
	}
	return t
}

// addBusinessMinutes 从 start 开始，只在工作时段内消耗 minutes 分钟，返回截止时刻。
// 阻断7 核心修复：正确排除非工作时段，而非把截止时刻平移到最近工时起点。
// 算法：
//  1. 若 start 在非工作时段，先把指针移到最近的工作时段起点。
//  2. 计算当天剩余工时；若 minutes <= 当天剩余工时，截止时刻 = 当前指针 + minutes。
//  3. 否则扣减当天剩余工时，跳到下一个工作日起点继续消耗，直到 minutes 耗尽。
func addBusinessMinutes(start time.Time, minutes int, cfg businessHoursConfig) time.Time {
	// is_24_7 时直接把 minutes 加到 start，不剔除任何时段（工时连续）。
	// 时区使用 cfg.loc 兜底为 ResolveLocation("") = Asia/Shanghai（与 timer_cron 一致），
	// 避免 time.Local 在跨时区容器里产生整体偏移。
	if cfg.is24x7 {
		loc := cfg.loc
		if loc == nil {
			loc = ResolveLocation("")
		}
		return start.In(loc).Add(time.Duration(minutes) * time.Minute)
	}
	remaining := time.Duration(minutes) * time.Minute
	loc := cfg.loc
	if loc == nil {
		loc = ResolveLocation("")
	}
	// 关键修复：把起始时刻换算到配置时区后再计算工作窗口，
	// 否则 9:00-18:00 窗口会按宿主机/UTC 时区计算，跨时区租户违约整体偏移。
	cursor := adjustToBusinessHoursStart(start.In(loc), cfg)

	// 防御性上限：避免极端 minutes 导致死循环（最多循环 365 天）。
	for i := 0; i < 366 && remaining > 0; i++ {
		dayEnd := cfg.workDayEnd(cursor)
		available := dayEnd.Sub(cursor)
		if available <= 0 {
			// 理论上 adjustToBusinessHoursStart 已保证 cursor 在工时内，
			// 此分支仅为防御性兜底，避免死循环。
			cursor = cfg.nextWorkDayStart(cursor)
			continue
		}
		if remaining <= available {
			return cursor.Add(remaining)
		}
		remaining -= available
		cursor = cfg.nextWorkDayStart(cursor)
	}
	return cursor
}

// CalculateSLADeadlineFromRequest 计算工单 SLA 截止时间（四级匹配链，方案 §4.2 v1.11）：
//
//	L1 provider 下发定义（合同个案；显式类型绑定 > 类型无关授权）
//	L2 等级策略（customer_tier + ticket_type + priority 特异性匹配）
//	L3b 租户自建定义（动态类型 code 快照优先 ∪ legacy 类型值；含无优先级回退）
//	L3c 类型无关租户默认定义（service_type 为空）
//	L4 未绑定 → fail-loud（结构化告警 + MatchedLayer=L4；工单照常创建，保持默认值）
//
// L3a（TicketType 直绑 default_sla_id）由调用方在建单流程先行处理，不经本函数。
func (s *TicketSLAService) CalculateSLADeadlineFromRequest(ctx context.Context, tenantID int, ticketType, priority string, ticketTypeID int, ticketTypeCode string) (*SLADeadlineResult, error) {
	now := s.nowFunc()

	// 标准化优先级（urgent → critical，保持存量口径）
	normalizedPriority := strings.ToLower(priority)
	if normalizedPriority == "urgent" {
		normalizedPriority = "critical"
	}

	// L1：provider 下发定义（合同个案优先）
	if def := s.matchGrantedDefinition(ctx, tenantID, ticketTypeID, now); def != nil {
		return s.buildDefinitionResult(ctx, tenantID, now, def, "L1"), nil
	}

	// L2：等级策略
	if policy := s.matchTierPolicy(ctx, tenantID, ticketType, normalizedPriority); policy != nil {
		return s.buildPolicyResult(ctx, tenantID, now, policy), nil
	}

	// L3b：租户自建定义（动态类型解析，删除 v1.11 前的 default→incident 静默兜底）
	if def := s.matchTenantDefinition(ctx, tenantID, ticketType, ticketTypeCode, normalizedPriority); def != nil {
		return s.buildDefinitionResult(ctx, tenantID, now, def, "L3b"), nil
	}

	// L3c：类型无关租户默认定义
	if def := s.matchTenantDefaultDefinition(ctx, tenantID); def != nil {
		return s.buildDefinitionResult(ctx, tenantID, now, def, "L3c"), nil
	}

	// L4：fail-loud——保持默认值（60/480 分钟），产生可观测告警信号与层级标记
	s.emitSLABindingAlert(ctx, tenantID, ticketType, ticketTypeID, normalizedPriority)
	return &SLADeadlineResult{
		SLADefinitionID:    0,
		MatchedLayer:       "L4",
		ResponseDeadline:   toPointer(s.calculateDeadlineWithBusinessHours(ctx, tenantID, now, 60, nil)),
		ResolutionDeadline: toPointer(s.calculateDeadlineWithBusinessHours(ctx, tenantID, now, 480, nil)),
		BusinessHoursOnly:  false,
	}, nil
}

// matchGrantedDefinition L1：查询对当前租户生效的下发授权（D-SLA-1）。
// 类型维度：显式绑定当前工单类型（ticketTypeID>0）优先于类型无关授权（ticket_type_id=0）；
// 有效期：valid_until 为空或晚于 now；定义侧 is_active 必须为真。
func (s *TicketSLAService) matchGrantedDefinition(ctx context.Context, tenantID, ticketTypeID int, now time.Time) *ent.SLADefinition {
	grants, err := s.client.SLADefinitionGrant.Query().
		Where(
			sladefinitiongrant.CustomerTenantIDEQ(tenantID),
			sladefinitiongrant.IsActiveEQ(true),
			sladefinitiongrant.Or(
				sladefinitiongrant.ValidUntilIsNil(),
				sladefinitiongrant.ValidUntilGT(now),
			),
			sladefinitiongrant.TicketTypeIDIn(0, int64(ticketTypeID)),
		).
		WithSLADefinition().
		Order(ent.Asc(sladefinitiongrant.FieldTicketTypeID), ent.Asc(sladefinitiongrant.FieldID)).
		All(ctx)
	if err != nil {
		s.logger.Warnw("SLA grant lookup failed", "tenant_id", tenantID, "error", err)
		return nil
	}
	var typeAgnostic *ent.SLADefinition
	for _, g := range grants {
		def := g.Edges.SLADefinition
		if def == nil || !def.IsActive {
			continue
		}
		if ticketTypeID > 0 && g.TicketTypeID == int64(ticketTypeID) {
			return def // 显式类型绑定优先
		}
		if g.TicketTypeID == 0 && typeAgnostic == nil {
			typeAgnostic = def
		}
	}
	return typeAgnostic
}

// matchTierPolicy L2：按客户等级（Tenant.service_tier）匹配 SLA 策略。
// 等级缺失时仍可命中"无 tier 偏好"的通用策略（MatchSLAPolicy 既有语义）。
func (s *TicketSLAService) matchTierPolicy(ctx context.Context, tenantID int, ticketType, priority string) *ent.SLAPolicy {
	if s.policySvc == nil {
		return nil
	}
	tier := ""
	tn, err := s.client.Tenant.Query().
		Where(tenant.IDEQ(tenantID)).
		Select(tenant.FieldServiceTier).
		Only(ctx)
	if err == nil && tn != nil {
		tier = tn.ServiceTier
	}
	policy, err := s.policySvc.MatchSLAPolicy(ctx, tenantID, ticketType, priority, tier)
	if err != nil || policy == nil {
		return nil
	}
	return policy
}

// matchTenantDefinition L3b：租户自建定义——动态类型 code（快照优先）∪ legacy 类型值，
// 每个候选先做"类型+优先级"精确匹配、再做"同类型任意优先级"回退
//（与 v1.11 前 FromRequest 回退宽度一致，取最早 ID 稳定化）。
func (s *TicketSLAService) matchTenantDefinition(ctx context.Context, tenantID int, ticketType, ticketTypeCode, priority string) *ent.SLADefinition {
	candidates := make([]string, 0, 2)
	add := func(v string) {
		v = strings.TrimSpace(v)
		if v == "" {
			return
		}
		for _, c := range candidates {
			if c == v {
				return
			}
		}
		candidates = append(candidates, v)
	}
	add(ticketTypeCode) // 动态类型 code 快照优先
	add(ticketType)     // legacy 类型值兜底

	for _, code := range candidates {
		def, err := s.client.SLADefinition.Query().
			Where(
				sladefinition.TenantID(tenantID),
				sladefinition.ServiceTypeEQ(code),
				sladefinition.Priority(priority),
				sladefinition.IsActive(true),
			).
			First(ctx)
		if err == nil {
			return def
		}
		if !ent.IsNotFound(err) {
			s.logger.Warnw("SLA definition lookup failed", "tenant_id", tenantID, "service_type", code, "error", err)
			return nil
		}
		def, err = s.client.SLADefinition.Query().
			Where(
				sladefinition.TenantID(tenantID),
				sladefinition.ServiceTypeEQ(code),
				sladefinition.IsActive(true),
			).
			Order(ent.Asc(sladefinition.FieldID)).
			First(ctx)
		if err == nil {
			return def
		}
		if !ent.IsNotFound(err) {
			s.logger.Warnw("SLA definition fallback lookup failed", "tenant_id", tenantID, "service_type", code, "error", err)
			return nil
		}
	}
	return nil
}

// matchTenantDefaultDefinition L3c：类型无关租户默认定义（service_type 为空/NULL）。
func (s *TicketSLAService) matchTenantDefaultDefinition(ctx context.Context, tenantID int) *ent.SLADefinition {
	def, err := s.client.SLADefinition.Query().
		Where(
			sladefinition.TenantID(tenantID),
			sladefinition.IsActive(true),
			sladefinition.Or(
				sladefinition.ServiceTypeIsNil(),
				sladefinition.ServiceTypeEQ(""),
			),
		).
		Order(ent.Asc(sladefinition.FieldID)).
		First(ctx)
	if err != nil {
		if !ent.IsNotFound(err) {
			s.logger.Warnw("SLA tenant default lookup failed", "tenant_id", tenantID, "error", err)
		}
		return nil
	}
	return def
}

// buildDefinitionResult 由 SLA 定义（L1/L3b/L3c）计算截止时间结果。
// 阻断7/C-8 语义保留：统一走 SLA 配置的工作日历（calculateDeadlineWithBusinessHours）。
func (s *TicketSLAService) buildDefinitionResult(ctx context.Context, tenantID int, now time.Time, def *ent.SLADefinition, layer string) *SLADeadlineResult {
	responseDeadline := s.calculateDeadlineWithBusinessHours(ctx, tenantID, now, def.ResponseTime, def.BusinessHours)
	resolutionDeadline := s.calculateDeadlineWithBusinessHours(ctx, tenantID, now, def.ResolutionTime, def.BusinessHours)
	return &SLADeadlineResult{
		SLADefinitionID:    def.ID,
		MatchedLayer:       layer,
		ResponseDeadline:   &responseDeadline,
		ResolutionDeadline: &resolutionDeadline,
		BusinessHoursOnly:  len(def.BusinessHours) > 0,
	}
}

// buildPolicyResult 由 SLA 策略（L2）计算截止时间结果。
// 策略未配置 business_hours 但声明排除周末/节假日时，回退"工作日全天"口径；
// 两者均未设置时保持 24x7（与定义口径一致）。
func (s *TicketSLAService) buildPolicyResult(ctx context.Context, tenantID int, now time.Time, p *ent.SLAPolicy) *SLADeadlineResult {
	businessHours := p.BusinessHours
	if len(businessHours) == 0 && (p.ExcludeWeekends || p.ExcludeHolidays) {
		businessHours = map[string]interface{}{
			"work_days":  []interface{}{1, 2, 3, 4, 5},
			"start_time": "00:00",
			"end_time":   "23:59",
		}
	}
	responseDeadline := s.calculateDeadlineWithBusinessHours(ctx, tenantID, now, p.ResponseTimeMinutes, businessHours)
	resolutionDeadline := s.calculateDeadlineWithBusinessHours(ctx, tenantID, now, p.ResolutionTimeMinutes, businessHours)
	return &SLADeadlineResult{
		SLADefinitionID:    0, // 策略不绑定具体定义（P2 报表以 MatchedLayer=L2 口径区分）
		MatchedLayer:       "L2",
		ResponseDeadline:   &responseDeadline,
		ResolutionDeadline: &resolutionDeadline,
		BusinessHoursOnly:  len(businessHours) > 0,
	}
}

// emitSLABindingAlert L4 fail-loud：结构化告警信号（开关 SLA_UNBOUND_ALERT_ENABLED 控制级别）。
// 实施期修正 IM-8：未复用 sla_alert_histories（其 alert_rule_id 为 Positive/Required，
// 未绑定场景无对应规则）；以结构化日志承载告警，"未绑定桶"由 P2 报表按 MatchedLayer=L4 统计。
func (s *TicketSLAService) emitSLABindingAlert(ctx context.Context, tenantID int, ticketType string, ticketTypeID int, priority string) {
	fields := []interface{}{
		"tenant_id", tenantID,
		"ticket_type", ticketType,
		"ticket_type_id", ticketTypeID,
		"priority", priority,
		"matched_layer", "L4",
	}
	if s.unboundAlertEnabled {
		s.logger.Errorw("SLA unbound: no matching definition; default deadlines applied", fields...)
		return
	}
	s.logger.Warnw("No SLA definition found, using defaults (unbound alert disabled)", fields...)
}

// CalculateSLADeadlineByDefinition applies the exact tenant-owned SLA bound to a TicketType.
func (s *TicketSLAService) CalculateSLADeadlineByDefinition(ctx context.Context, tenantID, definitionID int) (*SLADeadlineResult, error) {
	sla, err := s.client.SLADefinition.Query().Where(sladefinition.IDEQ(definitionID), sladefinition.TenantIDEQ(tenantID), sladefinition.IsActiveEQ(true)).Only(ctx)
	if err != nil {
		return nil, fmt.Errorf("bound SLA definition is unavailable: %w", err)
	}
	now := s.nowFunc()
	responseDeadline := s.calculateDeadlineWithBusinessHours(ctx, tenantID, now, sla.ResponseTime, sla.BusinessHours)
	resolutionDeadline := s.calculateDeadlineWithBusinessHours(ctx, tenantID, now, sla.ResolutionTime, sla.BusinessHours)
	return &SLADeadlineResult{SLADefinitionID: sla.ID, ResponseDeadline: &responseDeadline, ResolutionDeadline: &resolutionDeadline, BusinessHoursOnly: len(sla.BusinessHours) > 0}, nil
}

// toPointer 返回指针（辅助函数）
func toPointer[T any](v T) *T {
	return &v
}
