package common

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// TestUserPreferences_Roundtrip IP-P1-6c：白名单合并读写、非白名单拒绝、超限拒绝、nil 删除。
func TestUserPreferences_Roundtrip(t *testing.T) {
	client, svc, _, u, ctx := newAuthScopeFixture(t)
	defer client.Close()

	prefs, err := svc.GetUserPreferences(ctx, u.ID)
	require.NoError(t, err)
	require.Empty(t, prefs, "默认应为空偏好")

	want := map[string]any{"customerTenantIds": []any{1, 2}, "mode": "subset"}
	prefs, err = svc.UpdateUserPreferences(ctx, u.ID, map[string]any{"workbenchFilter": want})
	require.NoError(t, err)
	require.NotNil(t, prefs["workbenchFilter"])

	got, err := svc.GetUserPreferences(ctx, u.ID)
	require.NoError(t, err)
	require.Equal(t, want["mode"], got["workbenchFilter"].(map[string]any)["mode"])

	// 非白名单键整体拒绝。
	_, err = svc.UpdateUserPreferences(ctx, u.ID, map[string]any{"evil": 1})
	require.ErrorIs(t, err, ErrPreferenceKeyNotAllowed)

	// 超限拒绝。
	_, err = svc.UpdateUserPreferences(ctx, u.ID, map[string]any{"workbenchFilter": strings.Repeat("x", maxPreferenceBytes+1)})
	require.ErrorIs(t, err, ErrPreferencePayloadTooLarge)

	// nil 删除键。
	prefs, err = svc.UpdateUserPreferences(ctx, u.ID, map[string]any{"workbenchFilter": nil})
	require.NoError(t, err)
	require.NotContains(t, prefs, "workbenchFilter")
}

// TestUserPreferences_AssistantMemory V2-01：个性化记忆结构校验（合法往返 / 上限 / 未知字段拒绝）。
func TestUserPreferences_AssistantMemory(t *testing.T) {
	client, svc, _, u, ctx := newAuthScopeFixture(t)
	defer client.Close()

	valid := map[string]any{
		"version":   float64(1),
		"phrases":   []any{"重置密码", "申请 VPN"},
		"templates": []any{map[string]any{"name": "网络故障", "body": "无法访问内网，已尝试重启"}},
		"recent":    []any{map[string]any{"key": "inline_apply:triage_detail", "at": "2026-10-18T10:00:00.000Z"}},
	}
	prefs, err := svc.UpdateUserPreferences(ctx, u.ID, map[string]any{"assistantMemory": valid})
	require.NoError(t, err)
	stored, ok := prefs["assistantMemory"].(map[string]any)
	require.True(t, ok)
	require.Len(t, stored["phrases"], 2)

	got, err := svc.GetUserPreferences(ctx, u.ID)
	require.NoError(t, err)
	require.NotNil(t, got["assistantMemory"])

	// 未知子字段整体拒绝（防任意 KV 膨胀）。
	_, err = svc.UpdateUserPreferences(ctx, u.ID, map[string]any{
		"assistantMemory": map[string]any{"rawConversation": "should never be stored"},
	})
	require.ErrorIs(t, err, ErrPreferenceValueInvalid)

	// 超长常用语拒绝。
	_, err = svc.UpdateUserPreferences(ctx, u.ID, map[string]any{
		"assistantMemory": map[string]any{"phrases": []any{strings.Repeat("x", memoryMaxPhraseRunes+1)}},
	})
	require.ErrorIs(t, err, ErrPreferenceValueInvalid)

	// recent 键必须为结构化短值（拒绝自由文本/非法字符）。
	_, err = svc.UpdateUserPreferences(ctx, u.ID, map[string]any{
		"assistantMemory": map[string]any{"recent": []any{map[string]any{
			"key": "这是一段对话正文", "at": "2026-10-18T10:00:00Z",
		}}},
	})
	require.ErrorIs(t, err, ErrPreferenceValueInvalid)

	// 非对象拒绝。
	_, err = svc.UpdateUserPreferences(ctx, u.ID, map[string]any{"assistantMemory": "raw text"})
	require.ErrorIs(t, err, ErrPreferenceValueInvalid)

	// nil 删除键（合规：用户清空记忆）。
	prefs, err = svc.UpdateUserPreferences(ctx, u.ID, map[string]any{"assistantMemory": nil})
	require.NoError(t, err)
	require.NotContains(t, prefs, "assistantMemory")
}
