package service

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"itsm-backend/common/tenantctx"

	"go.uber.org/zap"
)

// O0-02 条件项闭环：AI 埋点事件（ai_telemetry_events）保留期清理（方案 §13.1.4「合规」/ §13.3 数据策略）。
//
// 契约：
//   - 只按 occurred_at 删除早于保留期的行；会话正文不在本表，删除不影响对话、run 档案与轨迹回放
//     （轨迹来自 runs/messages 等既有表，本表仅行为/性能元数据）；
//   - 批量上限删除 + 循环扫空：每批只删最旧的 batchSize 行（ORDER BY occurred_at），
//     避免单条大 DELETE 长锁；单轮批次有硬上限，剩余留给下一轮（进度不丢）；
//   - 幂等：重复执行只作用于同一 criteria（occurred_at < cutoff），无匹配/空表 → 0 行 0 错误；
//   - 跨租户系统治理任务：tenantctx.SystemContext（先例 pat_housekeeping），
//     中途失败返回「已删行数 + 错误」；Start 轮询仅告警，不阻断进程与后续轮次；
//   - 缺省：保留 90 天（与聚合回溯上限 assistantTelemetryMaxLookbackDays 一致）、
//     间隔 24h、单批 5000 行；经 config `ai_telemetry.*` 覆盖（<=0 回落缺省）。
type AITelemetryHousekeepingService struct {
	db            *sql.DB
	logger        *zap.SugaredLogger
	retentionDays int
	interval      time.Duration
	batchSize     int
	maxBatches    int
}

const (
	// assistantTelemetryDefaultRetentionDays 缺省保留期（天）。
	assistantTelemetryDefaultRetentionDays = 90
	// assistantTelemetryDefaultCleanupBatchSize 单条 DELETE 的批量上限（行）。
	assistantTelemetryDefaultCleanupBatchSize = 5000
	// assistantTelemetryDefaultCleanupMaxBatches 单轮批次硬上限（5000×200 = 100 万行/轮）。
	assistantTelemetryDefaultCleanupMaxBatches = 200
)

// NewAITelemetryHousekeepingService 构造清理服务。
// retentionDays<=0 → 90 天；intervalMinutes<=0 → 24h；batchSize<=0 → 5000。
func NewAITelemetryHousekeepingService(db *sql.DB, logger *zap.SugaredLogger, retentionDays, intervalMinutes, batchSize int) *AITelemetryHousekeepingService {
	if retentionDays <= 0 {
		retentionDays = assistantTelemetryDefaultRetentionDays
	}
	if intervalMinutes <= 0 {
		intervalMinutes = 24 * 60
	}
	if batchSize <= 0 {
		batchSize = assistantTelemetryDefaultCleanupBatchSize
	}
	return &AITelemetryHousekeepingService{
		db:            db,
		logger:        logger,
		retentionDays: retentionDays,
		interval:      time.Duration(intervalMinutes) * time.Minute,
		batchSize:     batchSize,
		maxBatches:    assistantTelemetryDefaultCleanupMaxBatches,
	}
}

// RetentionDays 返回生效保留期（天），供观测/测试断言。
func (s *AITelemetryHousekeepingService) RetentionDays() int { return s.retentionDays }

// Interval 返回生效清理间隔，供观测/测试断言。
func (s *AITelemetryHousekeepingService) Interval() time.Duration { return s.interval }

// assistantTelemetryPurgeStmt 是每批删除语句：子查询只取最旧的 batchSize 行（§13.3 保留期策略）。
const assistantTelemetryPurgeStmt = `
	DELETE FROM ai_telemetry_events
	WHERE id IN (
		SELECT id FROM ai_telemetry_events
		WHERE occurred_at < $1
		ORDER BY occurred_at
		LIMIT $2
	)
`

// RunOnce 执行一轮保留期清理，返回本轮删除行数。
//
// 语义：
//   - 循环直至单批删除数 < batchSize（扫空）或达到批次硬上限（剩余留待下一轮）；
//   - 中途失败返回「已删行数 + 错误」，不吞掉部分进度；
//   - db 未装配 → ErrAssistantTelemetryUnavailable。
func (s *AITelemetryHousekeepingService) RunOnce(ctx context.Context) (int, error) {
	if s == nil || s.db == nil {
		return 0, ErrAssistantTelemetryUnavailable
	}
	cutoff := time.Now().AddDate(0, 0, -s.retentionDays)
	// 系统治理任务（跨租户；先例 pat_housekeeping）：SystemContext 记录 bypass 审计日志。
	qctx := tenantctx.SystemContext(ctx, "ai_telemetry_housekeeping", "purge assistant telemetry events past retention")

	total := 0
	for batch := 0; batch < s.maxBatches; batch++ {
		res, err := s.db.ExecContext(qctx, assistantTelemetryPurgeStmt, cutoff, s.batchSize)
		if err != nil {
			return total, fmt.Errorf("purge assistant telemetry events: %w", err)
		}
		affected, err := res.RowsAffected()
		if err != nil {
			// 驱动不支持 RowsAffected：无法判断是否扫空，保守终止本轮（已删行已提交）。
			return total, fmt.Errorf("assistant telemetry purge rows affected: %w", err)
		}
		n := int(affected)
		total += n
		if n < s.batchSize {
			break
		}
	}
	if total > 0 && s.logger != nil {
		s.logger.Infow("AI telemetry housekeeping purged events",
			"count", total,
			"retention_days", s.retentionDays,
			"cutoff", cutoff.UTC().Format(time.RFC3339),
		)
	}
	return total, nil
}

// Start 启动定时清理（goroutine；启动即跑一轮，失败仅告警不阻断进程）。
func (s *AITelemetryHousekeepingService) Start(ctx context.Context) {
	if s == nil || s.db == nil {
		return
	}
	go func() {
		ticker := time.NewTicker(s.interval)
		defer ticker.Stop()
		for {
			if _, err := s.RunOnce(ctx); err != nil && s.logger != nil {
				s.logger.Warnw("AI telemetry housekeeping failed", "error", err)
			}
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
			}
		}
	}()
	if s.logger != nil {
		s.logger.Infow("AI telemetry housekeeping started",
			"retention_days", s.retentionDays,
			"interval", s.interval.String(),
			"batch_size", s.batchSize,
		)
	}
}
