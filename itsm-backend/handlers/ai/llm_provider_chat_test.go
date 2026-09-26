package ai_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"

	"itsm-backend/handlers/ai"
	"itsm-backend/service"
)

// 本文件覆盖主计划《多 LLM Provider 支持与可切换方案》BE-7 §3.4 的 chat 路径契约
// （P1 演进，2026-09-26：覆盖参数门禁由 system:write 降为 ai:read）：
//  1. 权限源不可得时非 super_admin 一律 403 AI_PROVIDER_FORBIDDEN（不静默忽略）；
//  2. 显式覆盖的解析失败可见地失败：404/409/422 + 契约字符串码；
//  3. 默认链解析失败保留现状语义（不新增失败面）。
//
// 全部用外部包 API（ai.NewService/SetLLMGateway/NewHandler）构造，不起 DB/RAG：
// 上述契约都发生在 RAG 调用之前。

type stubChatProvider struct{ response string }

func (s stubChatProvider) Chat(ctx context.Context, model string, messages []service.LLMMessage) (string, error) {
	return s.response, nil
}

// stubChatResolver 实现 service.ProviderResolver（§3.2 第 1 条窄接口）。
// 成功路径返回已构建的 provider（slotProvider 对 nil provider 兜底为 503，见 §3.2 防御性口径）。
type stubChatResolver struct {
	err error
}

func (s *stubChatResolver) Resolve(ctx context.Context, tenantID int, override string) (service.ProviderSlot, string, error) {
	if s.err != nil {
		return service.ProviderSlot{}, "", s.err
	}
	return service.ProviderSlot{
		Key:      override,
		Protocol: "openai",
		Provider: stubChatProvider{response: "ok"},
	}, service.ProviderSourceRequest, nil
}

func newProviderChatService(gw *service.LLMGateway) *ai.Service {
	svc := ai.NewService(nil, zap.NewNop().Sugar(), nil, nil, nil, nil, nil, nil, nil, nil, nil)
	if gw != nil {
		svc.SetLLMGateway(gw)
	}
	return svc
}

func postProviderChat(t *testing.T, h *ai.Handler, role, body string, stream bool) *httptest.ResponseRecorder {
	t.Helper()
	gin.SetMode(gin.TestMode)
	path := "/api/v1/ai/chat"
	if stream {
		path = "/api/v1/ai/chat/stream"
	}
	r := gin.New()
	r.POST(path, func(c *gin.Context) {
		c.Set("tenant_id", 1)
		c.Set("user_id", 7)
		c.Set("role", role)
		if stream {
			h.ChatStream(c)
			return
		}
		h.Chat(c)
	})
	req := httptest.NewRequest(http.MethodPost, path, bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w
}

func decodeErrorEnvelope(t *testing.T, w *httptest.ResponseRecorder) map[string]interface{} {
	t.Helper()
	var body map[string]interface{}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &body))
	return body
}

// P1 演进（2026-09-26）：provider 覆盖参数与 /ai/chat 端点同权限面（ai:read）。
// 权限源不可得（handler 未注入 ent client）时，非 super_admin 一律 fail-closed → 403
// （显式失败，绝不静默忽略）；super_admin 直通后由后续解析链给出可见失败。
func TestChatProviderOverrideFailClosedWithoutPermissionSource(t *testing.T) {
	h := ai.NewHandler(newProviderChatService(nil))

	for _, role := range []string{"sysadmin", "agent"} {
		for _, stream := range []bool{false, true} {
			w := postProviderChat(t, h, role, `{"query":"hello","provider":"deepseek-prod"}`, stream)
			require.Equal(t, http.StatusForbidden, w.Code,
				"role=%s stream=%v body=%s", role, stream, w.Body.String())
			body := decodeErrorEnvelope(t, w)
			assert.Equal(t, "AI_PROVIDER_FORBIDDEN", body["errorCode"])
		}
	}

	// super_admin 不受该门禁拦截：无网关时以 503 AI_PROVIDER_UNAVAILABLE 可见失败，
	// 证明请求已越过 403 权限门（与 TestChatProviderOverrideWithoutGatewayIsUnavailable 呼应）。
	w := postProviderChat(t, h, "super_admin", `{"query":"hello","provider":"deepseek-prod"}`, false)
	require.Equal(t, http.StatusServiceUnavailable, w.Code, "body=%s", w.Body.String())
	assert.Equal(t, "AI_PROVIDER_UNAVAILABLE", decodeErrorEnvelope(t, w)["errorCode"])
}

// 系统管理员 + 显式覆盖 + 实例不存在 → 404 AI_PROVIDER_NOT_FOUND（可见地失败）。
func TestChatProviderOverrideNotFoundIsVisible(t *testing.T) {
	gw := service.NewLLMGateway(stubChatProvider{response: "ok"}, nil, nil, "openai").
		WithResolver(&stubChatResolver{err: service.ErrProviderNotFound})
	h := ai.NewHandler(newProviderChatService(gw))

	w := postProviderChat(t, h, "super_admin", `{"query":"hello","provider":"ghost"}`, false)
	require.Equal(t, http.StatusNotFound, w.Code, "body=%s", w.Body.String())
	body := decodeErrorEnvelope(t, w)
	assert.Equal(t, "AI_PROVIDER_NOT_FOUND", body["errorCode"])
}

// 系统管理员 + 显式覆盖 + 实例禁用 → 409 AI_PROVIDER_DISABLED。
func TestChatProviderOverrideDisabledIsVisible(t *testing.T) {
	gw := service.NewLLMGateway(stubChatProvider{response: "ok"}, nil, nil, "openai").
		WithResolver(&stubChatResolver{err: service.ErrProviderDisabled})
	h := ai.NewHandler(newProviderChatService(gw))

	w := postProviderChat(t, h, "super_admin", `{"query":"hello","provider":"legacy"}`, false)
	require.Equal(t, http.StatusConflict, w.Code, "body=%s", w.Body.String())
	body := decodeErrorEnvelope(t, w)
	assert.Equal(t, "AI_PROVIDER_DISABLED", body["errorCode"])
}

// 系统管理员 + 显式覆盖 + 密钥缺失 → 422 AI_PROVIDER_KEY_MISSING。
func TestChatProviderOverrideKeyMissingIsVisible(t *testing.T) {
	gw := service.NewLLMGateway(stubChatProvider{response: "ok"}, nil, nil, "openai").
		WithResolver(&stubChatResolver{err: service.ErrProviderKeyMissing})
	h := ai.NewHandler(newProviderChatService(gw))

	w := postProviderChat(t, h, "super_admin", `{"query":"hello","provider":"nokey"}`, false)
	require.Equal(t, http.StatusUnprocessableEntity, w.Code, "body=%s", w.Body.String())
	body := decodeErrorEnvelope(t, w)
	assert.Equal(t, "AI_PROVIDER_KEY_MISSING", body["errorCode"])
}

// ResolveChatProvider：网关缺失时显式覆盖必须失败（不静默），空覆盖保持现状零标注。
func TestResolveChatProviderWithoutGateway(t *testing.T) {
	svc := newProviderChatService(nil)

	_, err := svc.ResolveChatProvider(context.Background(), 1, 2, "any")
	require.Error(t, err)
	assert.True(t, errors.Is(err, service.ErrProviderUnavailable), "err=%v", err)

	resolution, err := svc.ResolveChatProvider(context.Background(), 1, 2, "")
	require.NoError(t, err)
	assert.Empty(t, resolution.Key)
	assert.Empty(t, resolution.Source)
}

// ResolveChatProvider：默认链解析失败（无租户默认等）不新增失败面——网关回落静态 provider。
func TestResolveChatProviderDefaultChainFallsBackToStatic(t *testing.T) {
	gw := service.NewLLMGateway(stubChatProvider{response: "ok"}, nil, nil, "openai").
		WithResolver(&stubChatResolver{err: service.ErrProviderNotFound})
	svc := newProviderChatService(gw)

	resolution, err := svc.ResolveChatProvider(context.Background(), 1, 2, "")
	require.NoError(t, err)
	assert.Equal(t, service.ProviderSourceStatic, resolution.Source)
}

// 系统管理员 + 显式覆盖 + 解析成功 → 标注为 request 来源（handler 据此回带 provider/providerSource）。
func TestResolveChatProviderExplicitOverrideSuccess(t *testing.T) {
	gw := service.NewLLMGateway(stubChatProvider{response: "ok"}, nil, nil, "openai").
		WithResolver(&stubChatResolver{})
	svc := newProviderChatService(gw)

	resolution, err := svc.ResolveChatProvider(context.Background(), 1, 2, "deepseek-prod")
	require.NoError(t, err)
	assert.Equal(t, "deepseek-prod", resolution.Key)
	assert.Equal(t, service.ProviderSourceRequest, resolution.Source)
}

// 缺网关时显式覆盖必须 503 可见失败（§3.4 AI_PROVIDER_UNAVAILABLE），不得静默降级。
func TestChatProviderOverrideWithoutGatewayIsUnavailable(t *testing.T) {
	h := ai.NewHandler(newProviderChatService(nil))

	w := postProviderChat(t, h, "super_admin", `{"query":"hello","provider":"deepseek-prod"}`, false)
	require.Equal(t, http.StatusServiceUnavailable, w.Code, "body=%s", w.Body.String())
	body := decodeErrorEnvelope(t, w)
	assert.Equal(t, "AI_PROVIDER_UNAVAILABLE", body["errorCode"])
}

// sseErrorPayload 从 SSE 文本中提取带 errorCode 的 error 事件载荷。
func sseErrorPayload(t *testing.T, raw string) map[string]string {
	t.Helper()
	for _, line := range strings.Split(raw, "\n") {
		payload := strings.TrimPrefix(line, "data: ")
		if payload == line || !strings.Contains(payload, "errorCode") {
			continue
		}
		var decoded map[string]string
		if err := json.Unmarshal([]byte(payload), &decoded); err == nil {
			return decoded
		}
	}
	require.Failf(t, "未找到带 errorCode 的 SSE error 事件", "body=%s", raw)
	return nil
}

// 流式路径：显式覆盖的解析失败必须在 SSE error 事件内带契约 errorCode 可见地失败
// （§3.4 四种哨兵错误 → 404/409/422/503 字符串码）。
func TestChatStreamProviderOverrideFailureCarriesErrorCode(t *testing.T) {
	cases := []struct {
		name     string
		resolve  error
		wantCode string
	}{
		{"实例不存在", service.ErrProviderNotFound, "AI_PROVIDER_NOT_FOUND"},
		{"实例禁用", service.ErrProviderDisabled, "AI_PROVIDER_DISABLED"},
		{"密钥缺失", service.ErrProviderKeyMissing, "AI_PROVIDER_KEY_MISSING"},
		{"实例不可用", service.ErrProviderUnavailable, "AI_PROVIDER_UNAVAILABLE"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			gw := service.NewLLMGateway(stubChatProvider{response: "ok"}, nil, nil, "openai").
				WithResolver(&stubChatResolver{err: tc.resolve})
			h := ai.NewHandler(newProviderChatService(gw))

			w := postProviderChat(t, h, "super_admin", `{"query":"hello","provider":"broken"}`, true)
			require.Equal(t, http.StatusOK, w.Code, "SSE 已建流后状态码保持 200")
			raw := w.Body.String()
			assert.Contains(t, raw, "event: error")
			assert.NotContains(t, raw, "event: done", "解析失败不得产出 done 事件")
			payload := sseErrorPayload(t, raw)
			assert.Equal(t, tc.wantCode, payload["errorCode"])
			assert.Contains(t, payload["message"], "broken")
		})
	}
}
