package service

import (
	"context"

	"itsm-backend/ent"
	"itsm-backend/middleware"
)

// resolveAuditMembershipID 返回 actor 在审计行归属租户内的 membership ID。
// 审计直写路径的归因口径与 HTTP 审计中间件一致：tenant_id 是行归属租户，
// membership_id 必须是该租户内的身份；解析失败或不存在时返回 0，保留 legacy NULL。
func resolveAuditMembershipID(ctx context.Context, client *ent.Client, userID, tenantID int) int {
	if client == nil || userID <= 0 || tenantID <= 0 {
		return 0
	}
	membership, err := middleware.ResolveActiveMembership(ctx, client, userID, tenantID)
	if err != nil || membership == nil {
		return 0
	}
	return membership.ID
}
