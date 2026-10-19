package ai_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"

	"itsm-backend/ent/enttest"
	"itsm-backend/handlers/ai"
	"itsm-backend/service/bot"
)

// O1-01 条件项端点契约：GET /api/v1/ai/conversations/:id/trace
// （会话维度：消息回合 →（若有）运行 → 工具/确认 → 产物；纯问答无 run 同样可回放；
// 跨租户/未命中 404；未装配 503）。

type conversationTraceResponse struct {
	Code int `json:"code"`
	Data struct {
		Conversation struct {
			ID    int    `json:"id"`
			Title string `json:"title"`
		} `json:"conversation"`
		Messages []struct {
			Role      string `json:"role"`
			Content   string `json:"content"`
			Truncated bool   `json:"truncated"`
		} `json:"messages"`
		MessageCount      int  `json:"messageCount"`
		MessagesTruncated bool `json:"messagesTruncated"`
		Runs              []struct {
			ID     int    `json:"id"`
			Status string `json:"status"`
		} `json:"runs"`
		ToolInvocations []struct {
			ToolName          string `json:"toolName"`
			ConfirmationState string `json:"confirmationState"`
			ArgsRedacted      string `json:"argsRedacted"`
		} `json:"toolInvocations"`
		Artifacts []struct {
			Kind  string `json:"kind"`
			Title string `json:"title"`
		} `json:"artifacts"`
	} `json:"data"`
}

func setupConversationTraceRouter(t *testing.T, withTraceService bool) (*gin.Engine, int) {
	t.Helper()
	ctx := context.Background()
	client := enttest.Open(t, "sqlite3", fmt.Sprintf("file:conv-trace-%s-%v?mode=memory&cache=shared&_fk=1", t.Name(), withTraceService))
	t.Cleanup(func() { _ = client.Close() })

	tenant := client.Tenant.Create().SetName("会话轨迹端点").SetCode("conv-trace-ep").SetDomain("conv-trace.test").SaveX(ctx)
	base := time.Date(2026, 10, 18, 10, 0, 0, 0, time.UTC)

	conv := client.Conversation.Create().
		SetTenantID(tenant.ID).SetUserID(7).SetBotID(3).SetTitle("连不上网").
		SetCreatedAt(base).
		SaveX(ctx)
	client.Message.Create().
		SetTenantID(tenant.ID).SetConversationID(conv.ID).SetRole("user").
		SetContent("我连不上网了").SetCreatedAt(base).
		SaveX(ctx)
	client.Message.Create().
		SetTenantID(tenant.ID).SetConversationID(conv.ID).SetRole("assistant").
		SetContent("请先确认范围").SetCreatedAt(base.Add(time.Second)).
		SaveX(ctx)

	// 纯问答也允许有工具调用（无 run）：conversation_id 归属即可回放。
	client.ToolInvocation.Create().
		SetTenantID(tenant.ID).SetConversationID(conv.ID).
		SetToolName("search_kb").SetArguments(`{"raw":"secret"}`).SetArgsRedacted(`{"q":"***"}`).
		SetStatus("success").SetNeedsApproval(false).SetApprovalState("auto").
		SaveX(ctx)
	client.BotArtifact.Create().
		SetTenantID(tenant.ID).SetOwnerUserID(7).SetConversationID(conv.ID).
		SetKind("draft").SetToolName("draft_reply").SetTitle("答复草稿").
		SaveX(ctx)

	svc := ai.NewService(ai.NewEntRepository(client), zap.NewNop().Sugar(), nil, nil, nil, nil, nil, nil, nil, nil, nil)
	svc.SetEntClient(client)
	if withTraceService {
		svc.SetTraceService(bot.NewTraceService(client))
	}
	h := ai.NewHandler(svc)

	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.GET("/api/v1/ai/conversations/:id/trace", func(c *gin.Context) {
		c.Set("tenant_id", tenant.ID)
		c.Set("user_id", 7)
		c.Set("role", "super_admin")
		h.GetConversationTrace(c)
	})
	return r, conv.ID
}

func TestGetConversationTrace_ReturnsPureQAChain(t *testing.T) {
	r, convID := setupConversationTraceRouter(t, true)

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, fmt.Sprintf("/api/v1/ai/conversations/%d/trace", convID), nil)
	r.ServeHTTP(w, req)

	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	var body conversationTraceResponse
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &body))
	require.Equal(t, 0, body.Code)

	assert.Equal(t, convID, body.Data.Conversation.ID)
	assert.Equal(t, "连不上网", body.Data.Conversation.Title)
	require.Len(t, body.Data.Messages, 2)
	assert.Equal(t, 2, body.Data.MessageCount)
	assert.False(t, body.Data.MessagesTruncated)
	assert.Equal(t, "user", body.Data.Messages[0].Role)
	assert.Equal(t, "assistant", body.Data.Messages[1].Role)

	// 纯问答：无运行记录，但工具/产物仍按会话归属回放。
	assert.Empty(t, body.Data.Runs)
	require.Len(t, body.Data.ToolInvocations, 1)
	assert.Equal(t, "search_kb", body.Data.ToolInvocations[0].ToolName)
	// 非确认单语义（auto + success）按 fail-safe 归一为 unknown（不冒充已确认）。
	assert.Equal(t, "unknown", body.Data.ToolInvocations[0].ConfirmationState)
	assert.Equal(t, `{"q":"***"}`, body.Data.ToolInvocations[0].ArgsRedacted)
	require.Len(t, body.Data.Artifacts, 1)
	assert.Equal(t, "draft", body.Data.Artifacts[0].Kind)
}

func TestGetConversationTrace_NotFoundAndUnavailable(t *testing.T) {
	r, convID := setupConversationTraceRouter(t, true)

	// 未命中 → 404（与跨租户同口径由 service 层测试覆盖）。
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/ai/conversations/999999/trace", nil)
	r.ServeHTTP(w, req)
	assert.Equal(t, http.StatusNotFound, w.Code, w.Body.String())

	// 非法 id → 400。
	w = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodGet, "/api/v1/ai/conversations/abc/trace", nil)
	r.ServeHTTP(w, req)
	assert.Equal(t, http.StatusBadRequest, w.Code, w.Body.String())

	// 未装配轨迹服务 → 503（bot 运行面关闭的兼容态）。
	noTrace, _ := setupConversationTraceRouter(t, false)
	w = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodGet, fmt.Sprintf("/api/v1/ai/conversations/%d/trace", convID), nil)
	noTrace.ServeHTTP(w, req)
	assert.Equal(t, http.StatusServiceUnavailable, w.Code, w.Body.String())
}
