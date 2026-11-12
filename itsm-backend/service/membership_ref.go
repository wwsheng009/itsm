package service

import (
	"context"

	"itsm-backend/ent"
	"itsm-backend/middleware"
)

// activeMembershipID 返回 (userID, tenantID) 的 active membership ID；解析不到返回 0。
// 仅用于历史服务路径的兼容双写：调用方没有 membership 上下文时不做 fail-closed，
// 由迁移巡检补齐。
func activeMembershipID(ctx context.Context, client *ent.Client, userID, tenantID int) int {
	if client == nil || userID <= 0 || tenantID <= 0 {
		return 0
	}
	membership, err := middleware.ResolveActiveMembership(ctx, client, userID, tenantID)
	if err != nil || membership == nil {
		return 0
	}
	return membership.ID
}
