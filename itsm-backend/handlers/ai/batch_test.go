package ai_test

import (
	"bytes"
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

	"itsm-backend/ent"
	"itsm-backend/ent/enttest"
	"itsm-backend/handlers/ai"
	"itsm-backend/service"
)

// V1-01 列表页 AI 批量端点契约：只读生成、租户隔离（逐项 not_found）、上限 fail-visible、
// 有界并发保序、单条失败只降级该条（不整包 5xx）。

type batchAIHarness struct {
	engine      *gin.Engine
	client      *ent.Client
	tenantID    int
	otherTenant int
}

func newBatchAIHarness(t *testing.T, provider service.LLMProvider) *batchAIHarness {
	t.Helper()
	ctx := context.Background()
	dsn := filepath.Join(t.TempDir(), "batch-ai.db") + "?_fk=1&_busy_timeout=15000"
	client := enttest.Open(t, "sqlite3", dsn)
	t.Cleanup(func() { _ = client.Close() })

	tenantA := client.Tenant.Create().SetCode("v1-a").SetName("V1 Tenant A").SaveX(ctx)
	tenantB := client.Tenant.Create().SetCode("v1-b").SetName("V1 Tenant B").SaveX(ctx)

	// 分诊走关键词兜底（gateway=nil）→ 确定性分类，不依赖 Provider JSON。
	triageSvc := service.NewTriageService(nil, zap.NewNop())
	svc := ai.NewService(nil, zap.NewNop().Sugar(), nil, nil, nil, nil, nil, nil, triageSvc, nil, nil)
	svc.SetEntClient(client)
	if provider != nil {
		svc.SetLLMGateway(service.NewLLMGateway(provider, nil, nil, "test"))
	}
	handler := ai.NewHandler(svc)

	gin.SetMode(gin.TestMode)
	engine := gin.New()
	engine.POST("/api/v1/ai/tickets/batch-triage", func(c *gin.Context) {
		c.Set("tenant_id", tenantA.ID)
		c.Set("user_id", 42)
		c.Set("role", "super_admin")
		handler.BatchTriage(c)
	})
	engine.POST("/api/v1/ai/tickets/batch-reply-drafts", func(c *gin.Context) {
		c.Set("tenant_id", tenantA.ID)
		c.Set("user_id", 42)
		c.Set("role", "super_admin")
		handler.BatchReplyDrafts(c)
	})
	return &batchAIHarness{engine: engine, client: client, tenantID: tenantA.ID, otherTenant: tenantB.ID}
}

func (h *batchAIHarness) seedTicket(t *testing.T, tenantID int, number, title, description string) *ent.Ticket {
	t.Helper()
	ctx := context.Background()
	user := h.client.User.Create().
		SetUsername("v1-user-" + number).SetEmail("v1-" + number + "@example.com").
		SetName("V1 User").SetPasswordHash("x").SetTenantID(tenantID).SaveX(ctx)
	return h.client.Ticket.Create().
		SetTenantID(tenantID).SetTitle(title).
		SetDescription(description).
		SetTicketNumber(number).SetRequesterID(user.ID).SaveX(ctx)
}

func (h *batchAIHarness) post(t *testing.T, path string, payload interface{}) (int, map[string]interface{}) {
	t.Helper()
	body, err := json.Marshal(payload)
	require.NoError(t, err)
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, path, bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	h.engine.ServeHTTP(w, req)

	var resp struct {
		Code int                    `json:"code"`
		Data map[string]interface{} `json:"data"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp), w.Body.String())
	return w.Code, resp.Data
}

func batchItems(t *testing.T, data map[string]interface{}) []map[string]interface{} {
	t.Helper()
	raw, ok := data["items"].([]interface{})
	require.True(t, ok, "items 必须为数组：%v", data)
	items := make([]map[string]interface{}, 0, len(raw))
	for _, entry := range raw {
		item, ok := entry.(map[string]interface{})
		require.True(t, ok)
		items = append(items, item)
	}
	return items
}

func TestBatchTriage_OrderTenantIsolationAndSuggestions(t *testing.T) {
	h := newBatchAIHarness(t, nil)
	t1 := h.seedTicket(t, h.tenantID, "V1-1", "数据库连接失败", "慢查询导致连接池耗尽")
	t2 := h.seedTicket(t, h.tenantID, "V1-2", "网络无法上网", "交换机丢包")
	other := h.seedTicket(t, h.otherTenant, "V1-3", "跨租户工单", "不应可见")

	code, data := h.post(t, "/api/v1/ai/tickets/batch-triage", map[string]interface{}{
		"ticketIds": []int{t1.ID, other.ID, t2.ID, 99999},
	})

	require.Equal(t, http.StatusOK, code)
	items := batchItems(t, data)
	require.Len(t, items, 4, "保序返回请求顺序")
	assert.Equal(t, float64(t1.ID), items[0]["ticketId"])
	assert.Equal(t, "ok", items[0]["status"])
	assert.Equal(t, "database", items[0]["category"], "关键词兜底：数据库关键字")
	assert.NotEmpty(t, items[0]["priority"])
	assert.Greater(t, items[0]["confidence"].(float64), 0.0)
	assert.Equal(t, float64(other.ID), items[1]["ticketId"])
	assert.Equal(t, "not_found", items[1]["status"], "跨租户一律表现为不存在")
	assert.Equal(t, "ok", items[2]["status"])
	assert.Equal(t, "network", items[2]["category"])
	assert.Equal(t, float64(99999), items[3]["ticketId"])
	assert.Equal(t, "not_found", items[3]["status"])
	assert.Equal(t, float64(2), data["processed"])
	assert.Equal(t, false, data["degraded"], "缺 id 不是降级，降级只看已找到工单是否全部生成")
}

func TestBatchTriage_DedupesAndValidatesLimit(t *testing.T) {
	h := newBatchAIHarness(t, nil)
	t1 := h.seedTicket(t, h.tenantID, "V1-4", "应用报错", "接口 500")

	code, data := h.post(t, "/api/v1/ai/tickets/batch-triage", map[string]interface{}{
		"ticketIds": []int{t1.ID, t1.ID, -3, t1.ID},
	})
	require.Equal(t, http.StatusOK, code)
	assert.Len(t, batchItems(t, data), 1, "同 id 去重、非正数丢弃")

	over := make([]int, 0, 21)
	for i := 1; i <= 21; i++ {
		over = append(over, i)
	}
	code, _ = h.post(t, "/api/v1/ai/tickets/batch-triage", map[string]interface{}{"ticketIds": over})
	require.Equal(t, http.StatusBadRequest, code, "分诊上限 20 fail-visible")

	code, _ = h.post(t, "/api/v1/ai/tickets/batch-triage", map[string]interface{}{"ticketIds": []int{}})
	require.Equal(t, http.StatusBadRequest, code, "空集 400")

	code, _ = h.post(t, "/api/v1/ai/tickets/batch-triage", map[string]interface{}{})
	require.Equal(t, http.StatusBadRequest, code, "缺 ticketIds 400")
}

func TestBatchTriage_UnavailableWhenServiceMissing(t *testing.T) {
	ctx := context.Background()
	dsn := filepath.Join(t.TempDir(), "batch-ai-unavail.db") + "?_fk=1"
	client := enttest.Open(t, "sqlite3", dsn)
	t.Cleanup(func() { _ = client.Close() })
	tenant := client.Tenant.Create().SetCode("v1-u").SetName("V1 Unavailable").SaveX(ctx)

	svc := ai.NewService(nil, zap.NewNop().Sugar(), nil, nil, nil, nil, nil, nil, nil, nil, nil)
	svc.SetEntClient(client)
	handler := ai.NewHandler(svc)

	gin.SetMode(gin.TestMode)
	engine := gin.New()
	engine.POST("/api/v1/ai/tickets/batch-triage", func(c *gin.Context) {
		c.Set("tenant_id", tenant.ID)
		c.Set("user_id", 42)
		c.Set("role", "super_admin")
		handler.BatchTriage(c)
	})

	body, _ := json.Marshal(map[string]interface{}{"ticketIds": []int{1}})
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/ai/tickets/batch-triage", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	engine.ServeHTTP(w, req)

	require.Equal(t, http.StatusServiceUnavailable, w.Code, "分诊服务未装配 fail-visible")
}

func TestBatchReplyDrafts_SuccessAndIsolation(t *testing.T) {
	h := newBatchAIHarness(t, stubReplyLLMProvider{reply: "您好，已收到，请提供报错截图。"})
	t1 := h.seedTicket(t, h.tenantID, "V1-5", "无法登录", "SSO 登录后提示权限不足")
	t2 := h.seedTicket(t, h.tenantID, "V1-6", "打印机故障", "无法打印")
	other := h.seedTicket(t, h.otherTenant, "V1-7", "跨租户工单", "不应可见")

	code, data := h.post(t, "/api/v1/ai/tickets/batch-reply-drafts", map[string]interface{}{
		"ticketIds": []int{t1.ID, other.ID, t2.ID},
	})

	require.Equal(t, http.StatusOK, code)
	items := batchItems(t, data)
	require.Len(t, items, 3)
	assert.Equal(t, "ok", items[0]["status"])
	assert.Contains(t, items[0]["draft"], "请提供报错截图")
	assert.Equal(t, "not_found", items[1]["status"])
	assert.Equal(t, "ok", items[2]["status"])
	assert.Equal(t, float64(2), data["succeeded"])
	assert.Equal(t, false, data["degraded"])
}

func TestBatchReplyDrafts_ProviderFailureDegradesItemNotBatch(t *testing.T) {
	h := newBatchAIHarness(t, stubReplyLLMProvider{err: assert.AnError})
	t1 := h.seedTicket(t, h.tenantID, "V1-8", "设备离线", "工位上不了网")

	code, data := h.post(t, "/api/v1/ai/tickets/batch-reply-drafts", map[string]interface{}{
		"ticketIds": []int{t1.ID},
	})

	require.Equal(t, http.StatusOK, code, "单条失败不整包 5xx")
	items := batchItems(t, data)
	require.Len(t, items, 1)
	assert.Equal(t, "failed", items[0]["status"])
	assert.NotEmpty(t, items[0]["reason"])
	assert.Equal(t, float64(0), data["succeeded"])
	assert.Equal(t, true, data["degraded"])
}

func TestBatchReplyDrafts_RejectsOverDraftLimit(t *testing.T) {
	h := newBatchAIHarness(t, stubReplyLLMProvider{reply: "x"})
	ids := make([]int, 0, 11)
	for i := 1; i <= 11; i++ {
		ids = append(ids, i)
	}
	code, _ := h.post(t, "/api/v1/ai/tickets/batch-reply-drafts", map[string]interface{}{"ticketIds": ids})
	require.Equal(t, http.StatusBadRequest, code, "草稿上限 10 fail-visible")
}
