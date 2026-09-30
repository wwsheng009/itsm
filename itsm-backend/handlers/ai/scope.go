package ai

import (
	"context"
	"errors"

	"itsm-backend/ent"
	"itsm-backend/ent/configurationitem"
	"itsm-backend/ent/incident"
	"itsm-backend/ent/ticket"
	"itsm-backend/middleware"
	"itsm-backend/service/bot"
)

// B3-01 入口上下文在调用链内的传递（HTTP → Chat/ChatStream → 工具面/执行面 → run 记录）。
//
// 与 botIDKey 的关系：两者都是**请求级**上下文，均由服务端从请求体解析后写入；
// 工具参数中同名的键一律剥离（args_guard.go），模型无法覆写。

type scopeKey struct{}

// WithScope 注入入口上下文（零值 = 不注入，等价 chat/无目标）。
func WithScope(ctx context.Context, scope bot.Scope) context.Context {
	return context.WithValue(ctx, scopeKey{}, scope)
}

// ScopeFromContext 读取入口上下文（零值 = chat/无目标）。
func ScopeFromContext(ctx context.Context) bot.Scope {
	if scope, ok := ctx.Value(scopeKey{}).(bot.Scope); ok {
		return scope
	}
	return bot.Scope{}
}

// EntrypointFromContext 读取入口值（缺省 = chat，保持既有工具面口径）。
func EntrypointFromContext(ctx context.Context) string {
	if entrypoint := ScopeFromContext(ctx).Entrypoint; entrypoint != "" {
		return entrypoint
	}
	return bot.EntrypointChat
}

// entTargetChecker 是基于 ent 的目标对象预检实现（G9）：
// 租户内**存在**（对象级隔离）+ 角色对该资源具备读权限。
//
// 资源映射：ticket → ticket、incident → incident、ci → ci（与 authz 目录一致）。
type entTargetChecker struct {
	client *ent.Client
}

func newEntTargetChecker(client *ent.Client) *entTargetChecker {
	if client == nil {
		return nil
	}
	return &entTargetChecker{client: client}
}

func (c *entTargetChecker) CheckTarget(ctx context.Context, tenantID, userID int, role, targetType string, targetID int) error {
	if c == nil || c.client == nil {
		return bot.ErrScopeCheckerUnavailable
	}
	if tenantID <= 0 || userID <= 0 || targetID <= 0 {
		return bot.ErrScopeTargetDenied
	}
	switch targetType {
	case bot.TargetTypeTicket:
		if !middleware.HasResourcePermission(ctx, c.client, role, "ticket", "read", tenantID) {
			return bot.ErrScopeTargetDenied
		}
		exists, err := c.client.Ticket.Query().
			Where(ticket.ID(targetID), ticket.TenantID(tenantID)).Exist(ctx)
		if err != nil || !exists {
			return bot.ErrScopeTargetDenied
		}
		return nil
	case bot.TargetTypeIncident:
		if !middleware.HasResourcePermission(ctx, c.client, role, "incident", "read", tenantID) {
			return bot.ErrScopeTargetDenied
		}
		exists, err := c.client.Incident.Query().
			Where(incident.ID(targetID), incident.TenantID(tenantID)).Exist(ctx)
		if err != nil || !exists {
			return bot.ErrScopeTargetDenied
		}
		return nil
	case bot.TargetTypeCI:
		if !middleware.HasResourcePermission(ctx, c.client, role, "ci", "read", tenantID) {
			return bot.ErrScopeTargetDenied
		}
		exists, err := c.client.ConfigurationItem.Query().
			Where(configurationitem.ID(targetID), configurationitem.TenantID(tenantID)).Exist(ctx)
		if err != nil || !exists {
			return bot.ErrScopeTargetDenied
		}
		return nil
	default:
		return bot.ErrScopeTargetTypeUnknown
	}
}

// resolveRequestScope 解析请求级入口上下文（HTTP 层单一入口）。
//
// 未注入解析器时（关闭态）：仅允许「无目标 + chat」，其余一律拒绝——
// 关闭态不得出现「带了目标但没人校验」的静默放行。
func resolveRequestScope(ctx context.Context, resolver *bot.ScopeResolver, tenantID, userID int, role string, in bot.ScopeInput) (bot.Scope, error) {
	if resolver == nil {
		entrypoint := in.Entrypoint
		if entrypoint != "" && entrypoint != bot.EntrypointChat {
			return bot.Scope{}, bot.ErrScopeEntrypointUnknown
		}
		if in.TargetType != "" || in.TargetID > 0 {
			return bot.Scope{}, bot.ErrScopeCheckerUnavailable
		}
		return bot.Scope{Entrypoint: bot.EntrypointChat}, nil
	}
	return resolver.Resolve(ctx, tenantID, userID, role, in)
}

// scopeHTTPStatus 映射入口上下文错误的 HTTP 语义：
//   - 未知入口/参数不完整/未知类型 → 400（调用方参数问题）；
//   - 目标不可用（不存在/跨租户/无权） → 404（与「跨租户一律表现为不存在」口径一致）；
//   - 校验器不可用 → 503 fail-closed。
func scopeHTTPStatus(err error) (int, string) {
	switch {
	case errors.Is(err, bot.ErrScopeEntrypointUnknown),
		errors.Is(err, bot.ErrScopeTargetPairIncomplete),
		errors.Is(err, bot.ErrScopeTargetTypeUnknown):
		return 400, "AI_SCOPE_INVALID"
	case errors.Is(err, bot.ErrScopeTargetDenied):
		return 404, "AI_SCOPE_TARGET_UNAVAILABLE"
	case errors.Is(err, bot.ErrScopeCheckerUnavailable):
		return 503, "AI_SCOPE_UNAVAILABLE"
	default:
		return 500, "AI_SCOPE_ERROR"
	}
}
