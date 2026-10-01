package router

import (
	"testing"

	domainCommon "itsm-backend/handlers/common"
	invitationHandler "itsm-backend/handlers/invitation"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"go.uber.org/zap/zaptest"

	"itsm-backend/ent/enttest"

	_ "github.com/mattn/go-sqlite3"
)

// TestSetupRoutes_InvitationRoutes 路由契约（IP-P1-4b §4.0-C）：
// 创建/撤销在认证的 /users 组，落地页/接受在公开 /auth 组。
func TestSetupRoutes_InvitationRoutes(t *testing.T) {
	gin.SetMode(gin.TestMode)
	logger := zaptest.NewLogger(t).Sugar()
	client := enttest.Open(t, "sqlite3", "file:invitation-routes?mode=memory&cache=shared&_fk=1")
	t.Cleanup(func() { _ = client.Close() })

	engine := gin.New()
	commonHandler := domainCommon.NewHandler(domainCommon.NewService(domainCommon.NewEntRepository(client), "invitation-route-secret", logger, client))
	SetupRoutes(engine, &RouterConfig{
		JWTSecret:         "invitation-route-secret",
		Logger:            logger,
		Client:            client,
		CommonHandler:     commonHandler,
		InvitationHandler: invitationHandler.NewHandler(nil, logger),
	})

	registered := map[string]bool{}
	for _, route := range engine.Routes() {
		registered[route.Method+" "+route.Path] = true
	}
	for _, want := range []string{
		"POST /api/v1/users/invitations",
		"POST /api/v1/users/invitations/:id/revoke",
		"GET /api/v1/auth/invitations/:token",
		"POST /api/v1/auth/invitations/:token/accept",
	} {
		assert.True(t, registered[want], "缺少路由 %s", want)
	}
}
