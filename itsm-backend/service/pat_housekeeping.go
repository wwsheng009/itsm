package service

import (
	"context"
	"time"

	"itsm-backend/common/tenantctx"
	"itsm-backend/ent"
	"itsm-backend/ent/personalaccesstoken"

	"go.uber.org/zap"
)

// PATHousekeepingService PAT 令牌行清理（T13，开放问题 Q8）：
// 清理「已过期超过保留期」的令牌行；吊销/轮换血缘由 audit_logs 承载，删行不损审计。
// 复用 expires_at 索引；启动即跑一轮 + 定时循环（仿 membership_expiry 模式）。
// 保留期/间隔经 config：PAT_HOUSEKEEPING_RETENTION_DAYS（默认 180）/ 间隔 24h 固定。
type PATHousekeepingService struct {
	client        *ent.Client
	logger        *zap.SugaredLogger
	retentionDays int
	interval      time.Duration
}

// NewPATHousekeepingService 构造（retentionDays<=0 → 180；intervalMinutes<=0 → 24h）。
func NewPATHousekeepingService(client *ent.Client, logger *zap.SugaredLogger, retentionDays, intervalMinutes int) *PATHousekeepingService {
	if retentionDays <= 0 {
		retentionDays = 180
	}
	if intervalMinutes <= 0 {
		intervalMinutes = 24 * 60
	}
	return &PATHousekeepingService{
		client:        client,
		logger:        logger,
		retentionDays: retentionDays,
		interval:      time.Duration(intervalMinutes) * time.Minute,
	}
}

// RunOnce 执行一轮清理；返回删除行数。
// 系统旁路（治理任务，跨租户；先例 membership_expiry / offboarding）。
func (s *PATHousekeepingService) RunOnce(ctx context.Context) (int, error) {
	before := time.Now().AddDate(0, 0, -s.retentionDays)
	qctx := tenantctx.SystemContext(ctx, "pat_housekeeping", "purge expired PAT rows past retention")
	n, err := s.client.PersonalAccessToken.Delete().
		Where(
			personalaccesstoken.ExpiresAtLT(before),
		).
		Exec(qctx)
	if err != nil {
		s.logger.Warnw("PAT housekeeping failed", "error", err)
		return 0, err
	}
	if n > 0 {
		s.logger.Infow("PAT housekeeping purged expired tokens", "count", n, "before", before.Format(time.RFC3339))
	}
	return n, nil
}

// Start 启动定时清理（goroutine；失败不阻断进程）。
func (s *PATHousekeepingService) Start(ctx context.Context) {
	go func() {
		ticker := time.NewTicker(s.interval)
		defer ticker.Stop()
		for {
			_, _ = s.RunOnce(ctx)
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
			}
		}
	}()
	s.logger.Infow("PAT housekeeping started", "retention_days", s.retentionDays, "interval", s.interval.String())
}
