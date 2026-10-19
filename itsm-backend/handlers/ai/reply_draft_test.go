package ai_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strconv"
	"testing"

	_ "github.com/mattn/go-sqlite3"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"

	"itsm-backend/common"
	"itsm-backend/ent"
	"itsm-backend/ent/enttest"
	"itsm-backend/handlers/ai"
	"itsm-backend/service"
)

// V0-01 答复草稿端点契约：只读生成 + 知识引用 + 租户隔离 + 未装配 fail-visible。

type stubReplyLLMProvider struct {
	reply string
	err   error
}

func (s stubReplyLLMProvider) Chat(_ context.Context, _ string, _ []service.LLMMessage) (string, error) {
	return s.reply, s.err
}

type replyDraftHarness struct {
	engine      *gin.Engine
	client      *ent.Client
	tenantID    int
	otherTenant int
}

func newReplyDraftHarness(t *testing.T, provider service.LLMProvider) *replyDraftHarness {
	t.Helper()
	ctx := context.Background()
	dsn := filepath.Join(t.TempDir(), "reply-draft.db") + "?_fk=1&_busy_timeout=15000"
	client := enttest.Open(t, "sqlite3", dsn)
	t.Cleanup(func() { _ = client.Close() })

	tenantA := client.Tenant.Create().SetCode("v0-a").SetName("V0 Tenant A").SaveX(ctx)
	tenantB := client.Tenant.Create().SetCode("v0-b").SetName("V0 Tenant B").SaveX(ctx)

	svc := ai.NewService(nil, zap.NewNop().Sugar(), nil, nil, nil, nil, nil, nil, nil, nil, nil)
	svc.SetEntClient(client)
	if provider != nil {
		svc.SetLLMGateway(service.NewLLMGateway(provider, nil, nil, "test"))
	}
	handler := ai.NewHandler(svc)

	gin.SetMode(gin.TestMode)
	engine := gin.New()
	engine.POST("/api/v1/ai/tickets/:id/reply-draft", func(c *gin.Context) {
		c.Set("tenant_id", tenantA.ID)
		c.Set("user_id", 42)
		c.Set("role", "super_admin")
		handler.ReplyDraft(c)
	})
	return &replyDraftHarness{engine: engine, client: client, tenantID: tenantA.ID, otherTenant: tenantB.ID}
}

func (h *replyDraftHarness) seedTicket(t *testing.T, tenantID int, number string) *ent.Ticket {
	t.Helper()
	ctx := context.Background()
	user := h.client.User.Create().
		SetUsername("v0-user-" + number).SetEmail("v0-" + number + "@example.com").
		SetName("V0 User").SetPasswordHash("x").SetTenantID(tenantID).SaveX(ctx)
	return h.client.Ticket.Create().
		SetTenantID(tenantID).SetTitle("无法登录").
		SetDescription("SSO 登录后提示权限不足，请协助排查。").
		SetTicketNumber(number).SetRequesterID(user.ID).SaveX(ctx)
}

func (h *replyDraftHarness) do(t *testing.T, ticketID int) (int, map[string]interface{}) {
	t.Helper()
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost,
		"/api/v1/ai/tickets/"+strconv.Itoa(ticketID)+"/reply-draft", nil)
	h.engine.ServeHTTP(w, req)

	var resp struct {
		Code int                    `json:"code"`
		Data map[string]interface{} `json:"data"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp), w.Body.String())
	return w.Code, resp.Data
}

func TestReplyDraft_SuccessReturnsDraftAndCitations(t *testing.T) {
	h := newReplyDraftHarness(t, stubReplyLLMProvider{
		reply: "您好，已收到您的问题，请提供登录时间与报错截图，我们将尽快排查。",
	})
	ticketRow := h.seedTicket(t, h.tenantID, "V0-1")

	code, data := h.do(t, ticketRow.ID)

	require.Equal(t, http.StatusOK, code)
	assert.Equal(t, float64(ticketRow.ID), data["ticketId"])
	assert.Contains(t, data["draft"], "请提供登录时间")
	assert.Equal(t, false, data["degraded"])
	citations, _ := data["citations"].([]interface{})
	assert.Empty(t, citations, "RAG 未装配 → 空引用（best-effort grounding）")
}

func TestReplyDraft_TenantIsolationAndNotFound(t *testing.T) {
	h := newReplyDraftHarness(t, stubReplyLLMProvider{reply: "x"})
	otherTicket := h.seedTicket(t, h.otherTenant, "V0-2")

	code, _ := h.do(t, otherTicket.ID)
	require.Equal(t, http.StatusNotFound, code, "跨租户一律表现为不存在")

	code, _ = h.do(t, 99999)
	require.Equal(t, http.StatusNotFound, code)
}

func TestReplyDraft_UnavailableWhenGatewayMissing(t *testing.T) {
	h := newReplyDraftHarness(t, nil)
	ticketRow := h.seedTicket(t, h.tenantID, "V0-3")

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost,
		"/api/v1/ai/tickets/"+strconv.Itoa(ticketRow.ID)+"/reply-draft", nil)
	h.engine.ServeHTTP(w, req)

	require.Equal(t, http.StatusServiceUnavailable, w.Code)
	var resp map[string]interface{}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.Equal(t, float64(common.ServiceUnavailableCode), resp["code"])
}

func TestReplyDraft_ProviderFailureIsVisible(t *testing.T) {
	h := newReplyDraftHarness(t, stubReplyLLMProvider{err: assert.AnError})
	ticketRow := h.seedTicket(t, h.tenantID, "V0-4")

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost,
		"/api/v1/ai/tickets/"+strconv.Itoa(ticketRow.ID)+"/reply-draft", nil)
	h.engine.ServeHTTP(w, req)

	require.Equal(t, http.StatusServiceUnavailable, w.Code, "Provider 失败不静默返回空草稿")
}
