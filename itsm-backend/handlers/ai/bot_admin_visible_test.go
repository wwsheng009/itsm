package ai

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestBotAdminHandler_ListVisibleBotsAudienceFilter B2-04 工作区选择器端点：
// audience 按角色过滤、draft 不下发、入口不匹配不下发、最小字段集、租户隔离。
func TestBotAdminHandler_ListVisibleBotsAudienceFilter(t *testing.T) {
	h := newBotAdminHarness(t)
	tenant := 11

	// 内部 Bot（默认 audience=internal）+ 已发布 + chat 入口。
	recorder := h.do(t, http.MethodPost, "/api/v1/admin/bots", tenant, map[string]any{
		"slug": "internal-ops", "name": "内部运维", "status": "ga", "entrypoints": []string{"chat"},
	})
	require.Equal(t, http.StatusCreated, recorder.Code, recorder.Body.String())
	internalID := int(decodeBody(t, recorder)["id"].(float64))

	// 终端用户 Bot。
	recorder = h.do(t, http.MethodPost, "/api/v1/admin/bots", tenant, map[string]any{
		"slug": "end-user-help", "name": "用户助手", "status": "ga", "audience": "end_user",
		"entrypoints": []string{"chat"},
	})
	require.Equal(t, http.StatusCreated, recorder.Code)
	endUserID := int(decodeBody(t, recorder)["id"].(float64))

	// 不限 audience。
	recorder = h.do(t, http.MethodPost, "/api/v1/admin/bots", tenant, map[string]any{
		"slug": "everyone", "name": "通用助手", "status": "pilot", "audience": "all",
		"entrypoints": []string{"chat"},
	})
	require.Equal(t, http.StatusCreated, recorder.Code)
	everyoneID := int(decodeBody(t, recorder)["id"].(float64))

	// draft：不下发。
	recorder = h.do(t, http.MethodPost, "/api/v1/admin/bots", tenant, map[string]any{
		"slug": "drafting", "name": "草稿", "status": "draft", "audience": "all",
		"entrypoints": []string{"chat"},
	})
	require.Equal(t, http.StatusCreated, recorder.Code)

	// 入口不含 chat：不下发。
	recorder = h.do(t, http.MethodPost, "/api/v1/admin/bots", tenant, map[string]any{
		"slug": "ticket-only", "name": "仅工单页", "status": "ga", "audience": "all",
		"entrypoints": []string{"ticket_detail"},
	})
	require.Equal(t, http.StatusCreated, recorder.Code)

	idsOf := func(rec *httptest.ResponseRecorder) map[int]bool {
		decoded := decodeBody(t, rec)
		items, _ := decoded["items"].([]any)
		out := map[int]bool{}
		for _, item := range items {
			row, _ := item.(map[string]any)
			// 最小字段集：不得包含管理面字段（riskLimit/entrypointsJson/systemPromptRef/status）。
			assert.NotContains(t, row, "riskLimit")
			assert.NotContains(t, row, "entrypointsJson")
			assert.NotContains(t, row, "systemPromptRef")
			assert.NotContains(t, row, "status")
			out[int(row["id"].(float64))] = true
		}
		return out
	}

	// ① end_user：看不到 internal；能看到 end_user/all（以及种子默认助手=internal → 不可见）。
	recorder = h.doAs(t, http.MethodGet, "/api/v1/agent/bots", tenant, "end_user", nil)
	require.Equal(t, http.StatusOK, recorder.Code, recorder.Body.String())
	got := idsOf(recorder)
	assert.False(t, got[internalID], "end_user 不得看到 internal Bot")
	assert.True(t, got[endUserID], "end_user 应看到 end_user Bot")
	assert.True(t, got[everyoneID], "end_user 应看到 all Bot")

	// ② 技术员（内部角色）：可见 internal/end_user/all。
	recorder = h.doAs(t, http.MethodGet, "/api/v1/agent/bots", tenant, "technician", nil)
	require.Equal(t, http.StatusOK, recorder.Code)
	got = idsOf(recorder)
	assert.True(t, got[internalID])
	assert.True(t, got[endUserID])
	assert.True(t, got[everyoneID])
	assert.Equal(t, 3, len(got), "draft 与仅工单入口的 Bot 不得出现在选择器")

	// ③ 租户隔离：另一租户看不到上述 Bot（只有自己的默认助手）。
	recorder = h.doAs(t, http.MethodGet, "/api/v1/agent/bots", 12, "technician", nil)
	require.Equal(t, http.StatusOK, recorder.Code)
	other := idsOf(recorder)
	assert.False(t, other[internalID])
	assert.False(t, other[endUserID])
	assert.False(t, other[everyoneID])

	// ④ 缺租户上下文 → 401。
	require.Equal(t, http.StatusUnauthorized, h.doAs(t, http.MethodGet, "/api/v1/agent/bots", 0, "technician", nil).Code)
}
