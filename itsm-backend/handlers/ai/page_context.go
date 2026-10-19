package ai

import (
	"context"
	"regexp"
	"strings"
)

// D0-04 页面上下文提示层（pageKey/route）。
//
// 契约（方案 §4.2）：
//   - `pageContext` 仅作**提示与审计**，不参与授权；与 entrypoint 不同，它不进入
//     ScopeResolver 的 fail-closed 校验链（未知值不 400）；
//   - 未知/非法 pageKey 一律降级为忽略（前端版本先行时保持兼容）；
//   - pageKey 规范：小写字母/数字开头，允许 `._-`，不超过 64 字符；
//   - route 仅接受以 `/` 开头的相对路径，超长截断 255。
type PageContextHint struct {
	PageKey string `json:"pageKey"`
	Route   string `json:"route"`
}

var pageKeyPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9._-]{0,63}$`)

type pageKeyCtxKey struct{}

// WithPageKey 注入页面键提示（空值不注入，保持 ctx 原样）。
func WithPageKey(ctx context.Context, pageKey string) context.Context {
	if pageKey == "" {
		return ctx
	}
	return context.WithValue(ctx, pageKeyCtxKey{}, pageKey)
}

// PageKeyFromContext 读取页面键提示（缺省空串）。
func PageKeyFromContext(ctx context.Context) string {
	if ctx == nil {
		return ""
	}
	if pageKey, ok := ctx.Value(pageKeyCtxKey{}).(string); ok {
		return pageKey
	}
	return ""
}

// normalizePageKey 校验并归一化 pageKey；未知/非法 → 空串（调用方按"忽略"处理）。
func normalizePageKey(raw string) string {
	key := strings.ToLower(strings.TrimSpace(raw))
	if !pageKeyPattern.MatchString(key) {
		return ""
	}
	return key
}

// normalizePageRoute 校验并归一化 route（仅接受以 / 开头的相对路径；非法 → 空串）。
func normalizePageRoute(raw string) string {
	route := strings.TrimSpace(raw)
	if !strings.HasPrefix(route, "/") {
		return ""
	}
	if len(route) > 255 {
		route = route[:255]
	}
	return route
}

// applyPageContextHint 归一化提示并写入 ctx；返回（新 ctx、pageKey、route）。
// 无效提示返回原 ctx 与空值（调用方不记录、不报错）。
func applyPageContextHint(ctx context.Context, hint *PageContextHint) (context.Context, string, string) {
	if hint == nil {
		return ctx, "", ""
	}
	pageKey := normalizePageKey(hint.PageKey)
	if pageKey == "" {
		return ctx, "", ""
	}
	return WithPageKey(ctx, pageKey), pageKey, normalizePageRoute(hint.Route)
}
