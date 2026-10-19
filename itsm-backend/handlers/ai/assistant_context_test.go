package ai_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	_ "github.com/mattn/go-sqlite3"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"

	"itsm-backend/ent/enttest"
	"itsm-backend/handlers/ai"
	"itsm-backend/service"
	"itsm-backend/service/bot"
)

// D1-01 /ai/assistant/context 契约：未知 pageKey 降级；场景 Bot 可见性过滤；
// 建议工具门禁（授权 ∩ RBAC ∩ 风险）；策略层关闭时不过滤（兼容态）。

type assistantContextHarness struct {
	engine *gin.Engine
	admin  *bot.TemplateAdmin
}

func newAssistantContextHarness(t *testing.T, withPolicy bool) *assistantContextHarness {
	t.Helper()
	dsn := filepath.Join(t.TempDir(), "assistant-context.db") + "?_fk=1&_busy_timeout=15000"
	client := enttest.Open(t, "sqlite3", dsn)
	t.Cleanup(func() { _ = client.Close() })

	registry := service.NewToolRegistry(nil, nil, nil, client)
	svc := ai.NewService(nil, zap.NewNop().Sugar(), nil, registry, nil, nil, nil, nil, nil, nil, nil)
	svc.SetEntClient(client)
	if withPolicy {
		svc.SetBotPolicy(bot.NewPolicy(client))
	}
	handler := ai.NewHandler(svc)

	gin.SetMode(gin.TestMode)
	engine := gin.New()
	group := engine.Group("/api/v1")
	group.Use(func(c *gin.Context) {
		c.Set("tenant_id", 7)
		c.Set("user_id", 42)
		role := c.Request.Header.Get("X-Test-Role")
		if role == "" {
			role = "super_admin"
		}
		c.Set("role", role)
		c.Next()
	})
	group.GET("/ai/assistant/context", handler.GetAssistantContext)

	return &assistantContextHarness{engine: engine, admin: bot.NewTemplateAdmin(client)}
}

func (h *assistantContextHarness) seedSceneBot(t *testing.T, slug, status, audience string, entrypoints []string, grants map[string]string) {
	t.Helper()
	ctx := context.Background()
	tpl, err := h.admin.CreateTemplate(ctx, 7, bot.TemplateInput{
		Slug:        slug,
		Name:        slug,
		Audience:    audience,
		RiskLimit:   bot.RiskActLow,
		Entrypoints: entrypoints,
		Status:      status,
	})
	require.NoError(t, err)
	for name, risk := range grants {
		_, err := h.admin.UpsertGrant(ctx, 7, tpl.ID, bot.GrantInput{ToolName: name, RiskLimit: risk})
		require.NoError(t, err)
	}
}

func (h *assistantContextHarness) get(t *testing.T, path, role string) (int, map[string]interface{}) {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, path, nil)
	if role != "" {
		req.Header.Set("X-Test-Role", role)
	}
	w := httptest.NewRecorder()
	h.engine.ServeHTTP(w, req)

	var resp struct {
		Code int                    `json:"code"`
		Data map[string]interface{} `json:"data"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp), w.Body.String())
	return w.Code, resp.Data
}

func names(items []interface{}) []string {
	out := make([]string, 0, len(items))
	for _, item := range items {
		if m, ok := item.(map[string]interface{}); ok {
			out = append(out, m["id"].(string))
		}
	}
	return out
}

func TestAssistantContext_RequiresPageKey(t *testing.T) {
	h := newAssistantContextHarness(t, false)
	code, _ := h.get(t, "/api/v1/ai/assistant/context", "")
	require.Equal(t, http.StatusBadRequest, code)
}

func TestAssistantContext_UnknownPageKeyDegrades(t *testing.T) {
	h := newAssistantContextHarness(t, true)
	code, data := h.get(t, "/api/v1/ai/assistant/context?pageKey=no.such.page", "")
	require.Equal(t, http.StatusOK, code)

	assert.Equal(t, false, data["known"])
	suggestions, _ := data["suggestions"].([]interface{})
	assert.Empty(t, suggestions)
	_, hasScene := data["scene"]
	assert.False(t, hasScene, "未知页面不返回 scene 块")
}

// 场景 Bot 缺失 → fail-closed：该页场景建议全部过滤（原因 template_missing）。
func TestAssistantContext_SceneBotMissingFiltersAll(t *testing.T) {
	h := newAssistantContextHarness(t, true)
	code, data := h.get(t, "/api/v1/ai/assistant/context?pageKey=ticket.detail&route=/tickets/42", "")
	require.Equal(t, http.StatusOK, code)

	assert.Equal(t, true, data["known"])
	assert.Equal(t, "enabled", data["filtering"])
	suggestions, _ := data["suggestions"].([]interface{})
	assert.Empty(t, suggestions)

	scene, _ := data["scene"].(map[string]interface{})
	require.NotNil(t, scene)
	assert.Equal(t, false, scene["eligible"])
	assert.Equal(t, bot.ReasonTemplateMissing, scene["reason"])

	filtered, _ := data["filtered"].([]interface{})
	require.Len(t, filtered, 3, "ticket.detail 的 3 条建议全部过滤")
	first := filtered[0].(map[string]interface{})
	assert.Equal(t, bot.ReasonTemplateMissing, first["reason"])
}

// 场景 Bot 可见 + 工具授权齐全 + RBAC 直通（super_admin）→ 全部建议下发。
func TestAssistantContext_SceneEligibleKeepsDeclaredTools(t *testing.T) {
	h := newAssistantContextHarness(t, true)
	h.seedSceneBot(t, "s1-ticket-assistant", bot.StatusPilot, bot.AudienceInternal,
		[]string{bot.EntrypointChat, "ticket_detail"},
		map[string]string{"list_tickets": bot.RiskRead, "list_kb": bot.RiskRead, "draft_ticket_fields": bot.RiskPlan})

	code, data := h.get(t, "/api/v1/ai/assistant/context?pageKey=ticket.detail", "super_admin")
	require.Equal(t, http.StatusOK, code)

	scene, _ := data["scene"].(map[string]interface{})
	require.NotNil(t, scene)
	assert.Equal(t, true, scene["eligible"])
	assert.Equal(t, "", scene["reason"])

	suggestions, _ := data["suggestions"].([]interface{})
	assert.ElementsMatch(t, []string{"triage_plan", "reply_draft", "kb_lookup"}, names(suggestions))
	filtered, _ := data["filtered"].([]interface{})
	assert.Empty(t, filtered)
}

// RBAC 拒绝：角色无工具资源权限 → 声明了 tools 的建议被过滤（未声明的不受影响）。
func TestAssistantContext_ToolGateDeniesByRBAC(t *testing.T) {
	h := newAssistantContextHarness(t, true)
	h.seedSceneBot(t, "s1-ticket-assistant", bot.StatusPilot, bot.AudienceInternal,
		[]string{bot.EntrypointChat, "ticket_detail"},
		map[string]string{"list_tickets": bot.RiskRead, "list_kb": bot.RiskRead})

	code, data := h.get(t, "/api/v1/ai/assistant/context?pageKey=ticket.detail", "limited_role")
	require.Equal(t, http.StatusOK, code)

	suggestions, _ := data["suggestions"].([]interface{})
	assert.Equal(t, []string{"reply_draft"}, names(suggestions), "仅未声明 tools 的 reply_draft 保留")

	filtered, _ := data["filtered"].([]interface{})
	require.Len(t, filtered, 2)
	for _, item := range filtered {
		assert.Equal(t, bot.ReasonRBACDenied, item.(map[string]interface{})["reason"])
	}
}

// 角色不可见（audience=internal vs end_user）→ 场景建议全部过滤。
func TestAssistantContext_AudienceDeniedFiltersAll(t *testing.T) {
	h := newAssistantContextHarness(t, true)
	h.seedSceneBot(t, "s1-ticket-assistant", bot.StatusPilot, bot.AudienceInternal,
		[]string{bot.EntrypointChat, "ticket_detail"}, nil)

	code, data := h.get(t, "/api/v1/ai/assistant/context?pageKey=ticket.detail", "end_user")
	require.Equal(t, http.StatusOK, code)

	scene, _ := data["scene"].(map[string]interface{})
	require.NotNil(t, scene)
	assert.Equal(t, false, scene["eligible"])
	assert.Equal(t, bot.ReasonAudienceDenied, scene["reason"])
	suggestions, _ := data["suggestions"].([]interface{})
	assert.Empty(t, suggestions)
}

// draft 模板不下发。
func TestAssistantContext_DraftTemplateFiltersAll(t *testing.T) {
	h := newAssistantContextHarness(t, true)
	h.seedSceneBot(t, "s1-ticket-assistant", bot.StatusDraft, bot.AudienceInternal,
		[]string{bot.EntrypointChat, "ticket_detail"}, nil)

	code, data := h.get(t, "/api/v1/ai/assistant/context?pageKey=ticket.detail", "super_admin")
	require.Equal(t, http.StatusOK, code)

	scene, _ := data["scene"].(map[string]interface{})
	require.NotNil(t, scene)
	assert.Equal(t, false, scene["eligible"])
	assert.Equal(t, bot.ReasonStatusDraft, scene["reason"])
}

// 策略层未装配（bot.enabled=false 兼容态）→ 不做过滤，与静态基线一致。
func TestAssistantContext_PolicyDisabledKeepsSuggestions(t *testing.T) {
	h := newAssistantContextHarness(t, false)
	code, data := h.get(t, "/api/v1/ai/assistant/context?pageKey=ticket.detail", "")
	require.Equal(t, http.StatusOK, code)

	assert.Equal(t, "disabled", data["filtering"])
	suggestions, _ := data["suggestions"].([]interface{})
	assert.Len(t, suggestions, 3)
	filtered, _ := data["filtered"].([]interface{})
	assert.Empty(t, filtered)

	scene, _ := data["scene"].(map[string]interface{})
	require.NotNil(t, scene)
	assert.Equal(t, true, scene["eligible"])
	assert.Equal(t, bot.ReasonScenePolicyDisabled, scene["reason"])
}

// 建议载荷携带 label/prompt/readOnly 全文（前端直接消费）。
func TestAssistantContext_SuggestionPayloadShape(t *testing.T) {
	h := newAssistantContextHarness(t, false)
	_, data := h.get(t, "/api/v1/ai/assistant/context?pageKey=dashboard", "")
	suggestions, _ := data["suggestions"].([]interface{})
	require.NotEmpty(t, suggestions)
	first := suggestions[0].(map[string]interface{})
	assert.Equal(t, "attention_today", first["id"])
	assert.Equal(t, "今天最需要关注什么？", first["label"])
	assert.NotEmpty(t, first["prompt"])
	assert.Equal(t, true, first["readOnly"])
	assert.Equal(t, float64(0), first["rank"])
}
