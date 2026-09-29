// B1-05：确认单过期扫描的 ent 实现（bot.ConfirmationStore）。
//
// 与 service/bot/confirmation_sweeper.go 的分工：状态与扫描编排是纯逻辑，
// 本文件只做「查过期候选 + 条件置过期」两件持久化动作。
package ai

import (
	"context"
	"time"

	"itsm-backend/ent/toolinvocation"
	"itsm-backend/service/bot"
)

// entConfirmationStore 实现 bot.ConfirmationStore。
type entConfirmationStore struct{ svc *Service }

// ConfirmationStore 暴露确认单存储（bootstrap 的过期扫描任务使用）。
func (s *Service) ConfirmationStore() bot.ConfirmationStore {
	return &entConfirmationStore{svc: s}
}

// ListExpirable 返回 `approval_state=pending` 且 `expires_at < now` 的记录（跨租户，限量）。
func (s *entConfirmationStore) ListExpirable(ctx context.Context, now time.Time, limit int) ([]bot.ExpirableConfirmation, error) {
	if s == nil || s.svc == nil || s.svc.entClient == nil {
		return nil, nil
	}
	q := s.svc.entClient.ToolInvocation.Query().
		Where(
			toolinvocation.ApprovalStateEQ(string(bot.ConfirmationPending)),
			toolinvocation.ExpiresAtNotNil(),
			toolinvocation.ExpiresAtLT(now),
		).
		Order(toolinvocation.ByID())
	if limit > 0 {
		q = q.Limit(limit)
	}
	rows, err := q.All(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]bot.ExpirableConfirmation, 0, len(rows))
	for _, row := range rows {
		out = append(out, bot.ExpirableConfirmation{
			ID:        row.ID,
			TenantID:  row.TenantID,
			ExpiresAt: row.ExpiresAt,
		})
	}
	return out, nil
}

// Expire 条件更新：仅当记录仍为 pending 时置 expired（并发安全、幂等）。
// 返回 true 表示本次调用真的完成了置位。
func (s *entConfirmationStore) Expire(ctx context.Context, tenantID, id int, now time.Time) (bool, error) {
	if s == nil || s.svc == nil || s.svc.entClient == nil {
		return false, nil
	}
	affected, err := s.svc.entClient.ToolInvocation.Update().
		Where(
			toolinvocation.ID(id),
			toolinvocation.TenantID(tenantID),
			toolinvocation.ApprovalStateEQ(string(bot.ConfirmationPending)),
		).
		SetApprovalState(string(bot.ConfirmationExpired)).
		SetStatus(string(bot.ConfirmationExpired)).
		SetApprovalReason("确认单已过期（自动扫描）").
		SetApprovedAt(now).
		Save(ctx)
	if err != nil {
		return false, err
	}
	return affected > 0, nil
}

// SweepExpiredConfirmations 执行一次过期扫描（供定时任务与测试直接调用）。
//
// 返回扫描与置位计数；未装配 ent/存储时返回零值（安全 no-op）。
func (s *Service) SweepExpiredConfirmations(ctx context.Context, now time.Time, limit int) (bot.SweepResult, error) {
	if s == nil || s.entClient == nil {
		return bot.SweepResult{}, nil
	}
	return bot.SweepOnce(ctx, &entConfirmationStore{svc: s}, now, limit)
}
