// msp_snapshot_backfill.go：存量工单 MSP 快照的 Go 版幂等回填（IP-P0-3 的 CLI 支撑）。
//
// 与 scripts/msp/backfill-ticket-msp-snapshot.sql 同一判据（resolveTicketMSPProvider）：
// 客户租户 active 且未过期、provider 租户存在且类型为 msp_provider（兼容 legacy msp）且 active。
// dry-run 与 apply 共用同一派生逻辑，避免 SQL 与应用侧规则漂移。
package service

import (
	"context"
	"fmt"

	"itsm-backend/ent"
	entTicket "itsm-backend/ent/ticket"

	"go.uber.org/zap"
)

// MSPBackfillResult 快照回填统计。
type MSPBackfillResult struct {
	Tenants    int // 命中的客户租户数（有候选工单）
	Scanned    int // 候选工单数（未软删且未标记/缺 provider）
	Candidates int // 派生成功、待回填的工单数
	Updated    int // apply 实际更新数（dry-run 恒为 0）
	Skipped    int // 派生失败（租户未托管/非客户/暂停等）而跳过的工单数
}

// BackfillTicketMSPSnapshots 幂等回填存量工单 MSP 快照。
//
// tenantID > 0 时仅处理该租户；apply=false 为 dry-run（只统计不写库）。
// 判定与应用侧建单派生一致（resolveTicketMSPProvider），不触碰已有正确快照的行。
func BackfillTicketMSPSnapshots(ctx context.Context, client *ent.Client, logger *zap.SugaredLogger, apply bool, tenantID int) (*MSPBackfillResult, error) {
	if client == nil {
		return nil, fmt.Errorf("ent client is nil")
	}
	if logger == nil {
		logger = zap.NewNop().Sugar()
	}

	candidate := func() *ent.TicketQuery {
		q := client.Ticket.Query().Where(
			entTicket.DeletedAtIsNil(),
			entTicket.Or(
				entTicket.IsManagedByMspEQ(false),
				entTicket.MspProviderIDIsNil(),
			),
		)
		if tenantID > 0 {
			q = q.Where(entTicket.TenantIDEQ(tenantID))
		}
		return q
	}

	// 1. 找出有候选工单的租户集合（去重）。
	rows, err := candidate().Select(entTicket.FieldTenantID).All(ctx)
	if err != nil {
		return nil, fmt.Errorf("query candidate tenants: %w", err)
	}
	seen := make(map[int]struct{}, len(rows))
	tenantIDs := make([]int, 0, len(rows))
	for _, r := range rows {
		if _, ok := seen[r.TenantID]; ok {
			continue
		}
		seen[r.TenantID] = struct{}{}
		tenantIDs = append(tenantIDs, r.TenantID)
	}

	res := &MSPBackfillResult{Tenants: len(tenantIDs)}
	for _, tid := range tenantIDs {
		n, err := candidate().Where(entTicket.TenantIDEQ(tid)).Count(ctx)
		if err != nil {
			return nil, fmt.Errorf("count candidates tenant %d: %w", tid, err)
		}
		res.Scanned += n

		providerID := resolveTicketMSPProvider(ctx, client, tid)
		if providerID == nil {
			res.Skipped += n
			logger.Debugw("skip tenant without valid provider", "tenant_id", tid, "candidates", n)
			continue
		}
		res.Candidates += n
		if !apply {
			continue
		}

		updated, err := client.Ticket.Update().
			Where(
				entTicket.TenantIDEQ(tid),
				entTicket.DeletedAtIsNil(),
				entTicket.Or(
					entTicket.IsManagedByMspEQ(false),
					entTicket.MspProviderIDIsNil(),
				),
			).
			SetIsManagedByMsp(true).
			SetMspProviderID(*providerID).
			Save(ctx)
		if err != nil {
			return nil, fmt.Errorf("backfill tenant %d: %w", tid, err)
		}
		res.Updated += updated
		logger.Infow("backfilled tenant snapshot", "tenant_id", tid, "provider_id", *providerID, "updated", updated)
	}
	return res, nil
}
