// B1-05：确认单过期扫描（周期性任务，可测试的纯引擎 + 存储接口）。
package bot

import (
	"context"
	"time"
)

// ExpirableConfirmation 是扫描命中后可过期的最小字段集。
type ExpirableConfirmation struct {
	ID        int
	TenantID  int
	ExpiresAt *time.Time
}

// ConfirmationStore 由持久化层实现（handlers/ai 提供 ent 版本）。
//
// 约束：
//   - ListExpirable 只返回 `approval_state=pending` 且 `expires_at < now` 的记录；
//   - Expire 必须带租户维度，且仅当记录仍为 pending 时才置为过期（幂等、并发安全）。
type ConfirmationStore interface {
	ListExpirable(ctx context.Context, now time.Time, limit int) ([]ExpirableConfirmation, error)
	Expire(ctx context.Context, tenantID, id int, now time.Time) (bool, error)
}

// SweepResult 是一次扫描的结果（供日志与测试断言）。
type SweepResult struct {
	Scanned int
	Expired int
}

// SweepOnce 执行一次过期扫描：逐条惰性判定 + 置过期。
//
// 幂等：重复扫描不会重复置位（Expire 返回 false 时不计入 Expired）；
// 单条失败不中断整批（返回已处理计数 + 首个错误，由调用方决定告警口径）。
func SweepOnce(ctx context.Context, store ConfirmationStore, now time.Time, limit int) (SweepResult, error) {
	var result SweepResult
	if store == nil {
		return result, nil
	}
	if limit <= 0 {
		limit = 200
	}
	items, err := store.ListExpirable(ctx, now, limit)
	if err != nil {
		return result, err
	}
	result.Scanned = len(items)
	for _, item := range items {
		if !IsExpiredAt(item.ExpiresAt, now) {
			continue // 扫描与判定之间的竞态：记录已被续期/决策
		}
		changed, expireErr := store.Expire(ctx, item.TenantID, item.ID, now)
		if expireErr != nil {
			return result, expireErr
		}
		if changed {
			result.Expired++
		}
	}
	return result, nil
}

// Sweeper 周期性执行 SweepOnce（bootstrap 在 bot.enabled 时启动）。
type Sweeper struct {
	Store    ConfirmationStore
	Interval time.Duration
	// OnResult 观测钩子（可空）：用于日志/指标。
	OnResult func(SweepResult)
	// OnError 观测钩子（可空）。
	OnError func(error)
}

// Run 阻塞运行直到 ctx 取消；Interval <= 0 时取 10min。
func (s *Sweeper) Run(ctx context.Context) {
	if s == nil || s.Store == nil {
		return
	}
	interval := s.Interval
	if interval <= 0 {
		interval = 10 * time.Minute
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case now := <-ticker.C:
			result, err := SweepOnce(ctx, s.Store, now, 0)
			if err != nil {
				if s.OnError != nil {
					s.OnError(err)
				}
				continue
			}
			if s.OnResult != nil {
				s.OnResult(result)
			}
		}
	}
}
