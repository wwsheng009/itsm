package middleware

import (
	"context"

	"itsm-backend/ent"
)

// HasAIReadPermission 判定角色在当前租户是否具备 ai:read 权限。
//
// 语义与路由注册处挂载的 RequirePermission("ai","read") 完全一致：
// 复用 hasResourcePermission → AuthorizeResourceForRole 这一单一真源
// （super_admin 走既有旁路，其余角色走权限数据，权收立即生效）。
//
// 消费方（主计划《多 LLM Provider 支持与可切换方案》§3.4；P1 演进 2026-09-26）：
// /ai/chat 与 /ai/chat/stream 的 provider 单次覆盖参数面向全部具备 ai:read 的使用者
// （P1 前为 system:write = 仅系统管理员）；越权必须显式失败（403 AI_PROVIDER_FORBIDDEN），
// 绝不静默忽略覆盖参数——「静默降级」会让调用方误以为生效了指定的 provider，属于错误观测。
//
// 与选择器读端点（GET /ai/providers/available、GET /ai/user-preference）同权限面，
// 保证「能拿到选择器数据的人 = 能用覆盖参数的人」这一口径不漂移。
//
// fail-closed：client 为 nil 时 super_admin 仍直通（既有语义），其余角色一律拒绝。
func HasAIReadPermission(ctx context.Context, client *ent.Client, role string, tenantID int) bool {
	return HasResourcePermission(ctx, client, role, "ai", "read", tenantID)
}
