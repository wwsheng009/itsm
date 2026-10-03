package router

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"itsm-backend/ent/enttest"
	authHandler "itsm-backend/handlers/auth"
	domainCommon "itsm-backend/handlers/common"

	"github.com/gin-gonic/gin"
	_ "github.com/mattn/go-sqlite3"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap/zaptest"
)

// TestSetupRoutes_RLSStatsEndpoint（监控接入，报告 §5）：
// 端点必须注册在认证组内（未认证 401），并锁定路径/方法契约。
func TestSetupRoutes_RLSStatsEndpoint(t *testing.T) {
	gin.SetMode(gin.TestMode)
	client := enttest.Open(t, "sqlite3", "file:rls-stats-routes?mode=memory&cache=shared&_fk=1")
	logger := zaptest.NewLogger(t).Sugar()
	const jwtSecret = "rls-stats-route-test-secret"
	handler := authHandler.NewHandler(authHandler.NewService(client, jwtSecret, logger, nil))
	commonHandler := domainCommon.NewHandler(domainCommon.NewService(domainCommon.NewEntRepository(client), jwtSecret, logger, client))

	router := gin.New()
	SetupRoutes(router, &RouterConfig{
		JWTSecret: jwtSecret, Logger: logger, Client: client,
		AuthHandler: handler, CommonHandler: commonHandler,
	})

	// 未认证 → 401（路由在认证组内，不允许匿名读取运行指标）。
	req := httptest.NewRequest(http.MethodGet, "/api/v1/admin/rls/stats", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	require.Equal(t, http.StatusUnauthorized, w.Code, w.Body.String())

	// 路由契约：GET /api/v1/admin/rls/stats 必须存在。
	found := false
	for _, rt := range router.Routes() {
		if rt.Method == http.MethodGet && rt.Path == "/api/v1/admin/rls/stats" {
			found = true
			break
		}
	}
	require.True(t, found, "RLS stats route must be registered")
}
