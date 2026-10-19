package ai_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"

	"itsm-backend/ent"
	"itsm-backend/ent/enttest"
	"itsm-backend/handlers/ai"
)

// I2-02 合规：会话导出端点契约（Markdown 快照；跨租户=不存在同口径）。

type conversationExportResponse struct {
	Code int `json:"code"`
	Data struct {
		ConversationID int    `json:"conversationId"`
		Title          string `json:"title"`
		MessageCount   int    `json:"messageCount"`
		Markdown       string `json:"markdown"`
	} `json:"data"`
}

func setupExportRouter(t *testing.T) (*gin.Engine, *ent.Client, int, int, int) {
	t.Helper()
	ctx := context.Background()
	client := enttest.Open(t, "sqlite3", fmt.Sprintf("file:export-%s?mode=memory&cache=shared&_fk=1", t.Name()))
	t.Cleanup(func() { _ = client.Close() })

	tenant := client.Tenant.Create().SetName("导出租户").SetCode("exp-1").SetDomain("exp.test").SaveX(ctx)
	other := client.Tenant.Create().SetName("其他租户").SetCode("exp-2").SetDomain("exp2.test").SaveX(ctx)

	conv := client.Conversation.Create().
		SetTenantID(tenant.ID).SetUserID(7).SetTitle("打印机故障处理").SaveX(ctx)
	client.Message.Create().
		SetConversationID(conv.ID).SetTenantID(tenant.ID).SetRole("user").
		SetContent("打印机一直卡纸，怎么处理？").SaveX(ctx)
	client.Message.Create().
		SetConversationID(conv.ID).SetTenantID(tenant.ID).SetRole("assistant").
		SetContent(`{"answer":"建议先清理纸路，再检查搓纸轮。"}`).SaveX(ctx)

	foreignConv := client.Conversation.Create().
		SetTenantID(other.ID).SetUserID(8).SetTitle("他租户会话").SaveX(ctx)

	svc := ai.NewService(ai.NewEntRepository(client), zap.NewNop().Sugar(), nil, nil, nil, nil, nil, nil, nil, nil, nil)
	svc.SetEntClient(client)
	h := ai.NewHandler(svc)

	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.GET("/api/v1/ai/conversations/:id/export", func(c *gin.Context) {
		c.Set("tenant_id", tenant.ID)
		c.Set("user_id", 7)
		c.Set("role", "admin")
		h.ExportConversation(c)
	})
	return r, client, tenant.ID, conv.ID, foreignConv.ID
}

func TestExportConversation_MarkdownSnapshot(t *testing.T) {
	r, _, _, convID, _ := setupExportRouter(t)

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, fmt.Sprintf("/api/v1/ai/conversations/%d/export", convID), nil)
	r.ServeHTTP(w, req)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())

	var body conversationExportResponse
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &body))
	require.Equal(t, 0, body.Code)
	assert.Equal(t, convID, body.Data.ConversationID)
	assert.Equal(t, "打印机故障处理", body.Data.Title)
	assert.Equal(t, 2, body.Data.MessageCount)

	md := body.Data.Markdown
	assert.Contains(t, md, "# 打印机故障处理")
	assert.Contains(t, md, "消息数：2")
	assert.Contains(t, md, "## 用户 ·")
	assert.Contains(t, md, "打印机一直卡纸")
	assert.Contains(t, md, "## AI ·")
	// 结构化回答优先抽取可读文本（不落 JSON 壳）。
	assert.Contains(t, md, "建议先清理纸路")
	assert.NotContains(t, md, `"answer"`)
}

func TestExportConversation_NotFoundAndTenantIsolation(t *testing.T) {
	r, _, _, _, foreignConvID := setupExportRouter(t)

	// 跨租户 → 404（与不存在同口径，不泄漏存在性）。
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, fmt.Sprintf("/api/v1/ai/conversations/%d/export", foreignConvID), nil)
	r.ServeHTTP(w, req)
	assert.Equal(t, http.StatusNotFound, w.Code, w.Body.String())

	// 未命中 → 404；非法 id → 400。
	w = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodGet, "/api/v1/ai/conversations/999999/export", nil)
	r.ServeHTTP(w, req)
	assert.Equal(t, http.StatusNotFound, w.Code, w.Body.String())

	w = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodGet, "/api/v1/ai/conversations/abc/export", nil)
	r.ServeHTTP(w, req)
	assert.Equal(t, http.StatusBadRequest, w.Code, w.Body.String())
}
