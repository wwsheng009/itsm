package middleware

import (
	"context"

	"itsm-backend/ent"
)

// HasSystemWritePermission 判定角色在当前租户是否具备 system:write 权限。
//
// 语义与路由注册处统一挂载的 RequirePermission("system","write") 完全一致：
// 复用 hasResourcePermission → AuthorizeResourceForRole 这一单一真源
// （super_admin 走既有旁路，sysadmin/其余角色走权限数据，权收立即生效）。
//
// 消费方（主计划《多 LLM Provider 支持与可切换方案》v1.6 BE-7 §3.4）：
// /ai/chat 与 /ai/chat/stream 的 provider 单次覆盖参数仅系统管理员可用；
// 越权必须显式失败（403 AI_PROVIDER_FORBIDDEN），绝不静默忽略覆盖参数——
// 「静默降级」会让管理员误以为生效了指定的 provider，属于错误观测。
//
// fail-closed：client 为 nil 时 super_admin 仍直通（既有语义），其余角色一律拒绝。
func HasSystemWritePermission(ctx context.Context, client *ent.Client, role string, tenantID int) bool {
	return HasResourcePermission(ctx, client, role, "system", "write", tenantID)
}
