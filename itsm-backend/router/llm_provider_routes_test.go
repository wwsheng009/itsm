package router

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"itsm-backend/ent/enttest"
	aiHandler "itsm-backend/handlers/ai"

	"github.com/gin-gonic/gin"
	_ "github.com/mattn/go-sqlite3"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap/zaptest"
)

// QA-3 回归门禁（多 LLM Provider 方案 §5.3 / §6.2 场景 8；§3.5 回滚语义）：
// 灰度开关关闭时 bootstrap 不构造 handler（app.go:989 仅在 MultiProviderEnabled 时注入），
// RouterConfig.LLMProviderAdminHandler 为 nil ⇒ 整组 /api/v1/ai/providers* 路由不注册，
// 对既有客户端表现为 404（端点不存在），即"无新路由"。
func TestSetupRoutes_LLMProviderAdminRoutesAbsentWhenHandlerNil(t *testing.T) {
	client := enttest.Open(t, "sqlite3", "file:router_llmprov_nil?mode=memory&cache=shared&_fk=1")
	defer client.Close()
	logger := zaptest.NewLogger(t).Sugar()

	gin.SetMode(gin.TestMode)
	r := gin.New()
	require.NotPanics(t, func() {
		SetupRoutes(r, &RouterConfig{
			JWTSecret: "test-secret",
			Logger:    logger,
			Client:    client,
			// LLMProviderAdminHandler 保持 nil：等价开关关闭时的 bootstrap 注入结果
		})
	})

	for _, route := range r.Routes() {
		assert.NotContains(t, route.Path, "/ai/providers",
			"开关关闭时不得注册多 Provider 管理路由：%s", route.Path)
	}

	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/v1/ai/providers", nil))
	assert.Equal(t, http.StatusNotFound, w.Code,
		"开关关闭时 /ai/providers 不可达（404），与引入该能力前一致")
}

// 反向断言：开关开启（handler 被注入）时同一路径必须已注册——未带令牌时不是 404，
// 而是进入鉴权链（401/403），确保上面的"404"确实由门禁导致而非路径拼写错误。
func TestSetupRoutes_LLMProviderAdminRoutesRegisteredWhenHandlerPresent(t *testing.T) {
	client := enttest.Open(t, "sqlite3", "file:router_llmprov_on?mode=memory&cache=shared&_fk=1")
	defer client.Close()
	logger := zaptest.NewLogger(t).Sugar()

	gin.SetMode(gin.TestMode)
	r := gin.New()
	require.NotPanics(t, func() {
		SetupRoutes(r, &RouterConfig{
			JWTSecret:               "test-secret",
			Logger:                  logger,
			Client:                  client,
			LLMProviderAdminHandler: aiHandler.NewLLMProviderAdminHandler(nil),
		})
	})

	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/v1/ai/providers", nil))
	require.NotEqual(t, http.StatusNotFound, w.Code,
		"开关开启时应已注册 /ai/providers（未带令牌应被鉴权链拦截而非 404）")
}
