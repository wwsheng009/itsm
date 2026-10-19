package ai

import (
	"context"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

// D0-04 页面上下文提示层单测：归一化、忽略语义与上下文注入（不参与授权，fail-open 提示）。

func TestNormalizePageKey(t *testing.T) {
	cases := map[string]string{
		"ticket.detail":    "ticket.detail",
		" Ticket.Detail ":  "ticket.detail",
		"ai.observability": "ai.observability",
		"a-b_c.d":          "a-b_c.d",
		"":                 "",
		"-bad":             "",
		"a/b":              "",
		"a b":              "",
		"键":                "",
	}
	for input, want := range cases {
		assert.Equal(t, want, normalizePageKey(input), "input=%q", input)
	}

	// 64 字符边界：允许；65 字符拒绝。
	assert.Equal(t, strings.Repeat("a", 64), normalizePageKey(strings.Repeat("a", 64)))
	assert.Equal(t, "", normalizePageKey(strings.Repeat("a", 65)))
}

func TestNormalizePageRoute(t *testing.T) {
	assert.Equal(t, "/tickets/42", normalizePageRoute(" /tickets/42 "))
	assert.Equal(t, "", normalizePageRoute("tickets/42"))
	assert.Equal(t, "", normalizePageRoute(""))
	// 超长截断到 255。
	assert.Len(t, normalizePageRoute("/"+strings.Repeat("a", 300)), 255)
}

func TestApplyPageContextHint(t *testing.T) {
	base := context.Background()

	// nil 提示：ctx 原样、无键（缺省请求体与现状零差异）。
	ctx, pageKey, route := applyPageContextHint(base, nil)
	assert.Equal(t, "", pageKey)
	assert.Equal(t, "", route)
	assert.Equal(t, "", PageKeyFromContext(ctx))

	// 合法：归一化后注入 key 与 route。
	ctx, pageKey, route = applyPageContextHint(base, &PageContextHint{PageKey: "Ticket.Detail", Route: "/tickets/42"})
	assert.Equal(t, "ticket.detail", pageKey)
	assert.Equal(t, "/tickets/42", route)
	assert.Equal(t, "ticket.detail", PageKeyFromContext(ctx))

	// 非法 pageKey：整体忽略（不 400，route 也不落）。
	ctx, pageKey, route = applyPageContextHint(base, &PageContextHint{PageKey: "bad key", Route: "/tickets/42"})
	assert.Equal(t, "", pageKey)
	assert.Equal(t, "", route)
	assert.Equal(t, "", PageKeyFromContext(ctx))

	// 非法 route：key 仍保留，route 置空。
	ctx, pageKey, route = applyPageContextHint(base, &PageContextHint{PageKey: "ticket.detail", Route: "not-relative"})
	assert.Equal(t, "ticket.detail", pageKey)
	assert.Equal(t, "", route)
}

func TestPageKeyFromContext_NilSafe(t *testing.T) {
	// 显式验证 nil ctx 安全（与 ScopeFromContext 同口径）。
	assert.Equal(t, "", PageKeyFromContext(nil))

	// WithPageKey 空值不注入。
	ctx := WithPageKey(context.Background(), "")
	assert.Equal(t, "", PageKeyFromContext(ctx))
}
