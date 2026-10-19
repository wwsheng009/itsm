package service

import (
	"context"
	"errors"
	"fmt"
	"time"

	"itsm-backend/common/tenantctx"
	"itsm-backend/ent"

	"go.uber.org/zap"
)

// O1-02 条件项闭环：AI 健康定时巡检 sweeper（方案 §13.5「告警通道：通知中心 + 平台级系统配置开关」）。
//
// 契约：
//   - 部署级开关默认关闭（config `ai_health.sweeper_enabled`）＝不默认打扰；
//     显式打开后启动即跑一轮 + 按间隔巡检（不改变既有显式触发端点的行为）；
//   - 每轮枚举租户（系统旁路；上限 200 租户/轮避免长事务），逐租户 Snapshot + SLO 评估 +
//     DispatchAlerts（复用既有 30 分钟去重与通知中心落库）；
//   - 收件人叠加订阅偏好过滤（AIHealthAlertEventType；无偏好行=放行）；
//   - 租户级失败/panic 隔离：单租户出错仅计入 Errors 并继续其余租户；
//   - 失败不阻断进程：Start 轮询错误仅告警。
type AIHealthSweeper struct {
	client     *ent.Client
	health     *AIHealthService
	logger     *zap.SugaredLogger
	interval   time.Duration
	windowDays int
	maxTenants int
}

const (
	aiHealthSweeperDefaultIntervalMinutes = 15
	aiHealthSweeperMaxTenantsPerRun       = 200
)

// ErrAIHealthSweeperUnavailable sweeper 未装配（client/health 缺一）。
var ErrAIHealthSweeperUnavailable = errors.New("ai health sweeper not initialized")

// NewAIHealthSweeper 构造巡检器。
// intervalMinutes<=0 → 15 分钟；windowDays<=0 → AIHealthDefaultWindowDays，上限 AIHealthMaxWindowDays。
func NewAIHealthSweeper(client *ent.Client, health *AIHealthService, logger *zap.SugaredLogger, intervalMinutes, windowDays int) *AIHealthSweeper {
	if intervalMinutes <= 0 {
		intervalMinutes = aiHealthSweeperDefaultIntervalMinutes
	}
	if windowDays <= 0 {
		windowDays = AIHealthDefaultWindowDays
	}
	if windowDays > AIHealthMaxWindowDays {
		windowDays = AIHealthMaxWindowDays
	}
	return &AIHealthSweeper{
		client:     client,
		health:     health,
		logger:     logger,
		interval:   time.Duration(intervalMinutes) * time.Minute,
		windowDays: windowDays,
		maxTenants: aiHealthSweeperMaxTenantsPerRun,
	}
}

// Interval 返回生效巡检间隔，供观测/测试断言。
func (s *AIHealthSweeper) Interval() time.Duration { return s.interval }

// WindowDays 返回生效评估窗口，供观测/测试断言。
func (s *AIHealthSweeper) WindowDays() int { return s.windowDays }

// AIHealthSweepSummary 是一轮巡检的汇总（观测/测试断言）。
type AIHealthSweepSummary struct {
	Tenants             int `json:"tenants"`
	Evaluated           int `json:"evaluated"`
	Alerts              int `json:"alerts"`
	Created             int `json:"created"`
	Skipped             int `json:"skipped"`
	SkippedByPreference int `json:"skippedByPreference"`
	Errors              int `json:"errors"`
}

// RunOnce 执行一轮全租户巡检。
func (s *AIHealthSweeper) RunOnce(ctx context.Context) (*AIHealthSweepSummary, error) {
	if s == nil || s.client == nil || s.health == nil {
		return nil, ErrAIHealthSweeperUnavailable
	}
	sysCtx := tenantctx.SystemContext(ctx, "ai_health:sweeper", "evaluate AI health SLOs for all tenants at tick")
	tenants, err := s.client.Tenant.Query().Limit(s.maxTenants).All(sysCtx)
	if err != nil {
		return nil, fmt.Errorf("health sweeper: 查询租户失败: %w", err)
	}
	if len(tenants) >= s.maxTenants && s.logger != nil {
		s.logger.Warnw("AI health sweeper tenant scan hit the per-run cap; remaining tenants wait for the next round",
			"cap", s.maxTenants)
	}
	summary := &AIHealthSweepSummary{Tenants: len(tenants)}
	for _, item := range tenants {
		s.evaluateTenant(sysCtx, item.ID, summary)
	}
	if s.logger != nil {
		s.logger.Infow("AI health sweeper completed one round",
			"tenants", summary.Tenants,
			"evaluated", summary.Evaluated,
			"alerts", summary.Alerts,
			"created", summary.Created,
			"skipped_by_preference", summary.SkippedByPreference,
			"errors", summary.Errors,
		)
	}
	return summary, nil
}

// evaluateTenant 单租户评估（panic/错误隔离：只影响该租户）。
func (s *AIHealthSweeper) evaluateTenant(sysCtx context.Context, tenantID int, summary *AIHealthSweepSummary) {
	defer func() {
		if r := recover(); r != nil {
			summary.Errors++
			if s.logger != nil {
				s.logger.Errorw("AI health sweeper tenant evaluation panicked", "tenant_id", tenantID, "panic", r)
			}
		}
	}()
	tenantCtx := tenantctx.WithTenantID(sysCtx, tenantID)
	result, err := s.health.DispatchAlerts(tenantCtx, tenantID, s.windowDays)
	if err != nil {
		summary.Errors++
		if s.logger != nil {
			s.logger.Warnw("AI health sweeper tenant evaluation failed", "tenant_id", tenantID, "error", err)
		}
		return
	}
	summary.Evaluated++
	summary.Alerts += len(result.Alerts)
	summary.Created += result.Created
	summary.Skipped += result.Skipped
	summary.SkippedByPreference += result.SkippedByPreference
}

// Start 启动定时巡检（goroutine；启动即跑一轮，失败仅告警不阻断进程）。
func (s *AIHealthSweeper) Start(ctx context.Context) {
	if s == nil || s.client == nil || s.health == nil {
		return
	}
	go func() {
		ticker := time.NewTicker(s.interval)
		defer ticker.Stop()
		for {
			if _, err := s.RunOnce(ctx); err != nil && s.logger != nil {
				s.logger.Warnw("AI health sweeper failed", "error", err)
			}
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
			}
		}
	}()
	if s.logger != nil {
		s.logger.Infow("AI health sweeper started", "interval", s.interval.String(), "window_days", s.windowDays)
	}
}
