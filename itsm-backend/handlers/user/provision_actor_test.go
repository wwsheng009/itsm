package user

import (
	"net/http/httptest"
	"testing"

	"itsm-backend/middleware"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
)

// F1 回归（2026-10-12）：MSP 跨租户建号需要 ProvisionActor.MSPRole 非空，
// 否则 service.resolveChannel 会误判为 CROSS_TENANT_FORBIDDEN。
// 身份来源：msp_role 上下文键（MSPMiddleware 写入）或 MSPContext 回退。
func TestProvisionActorFromContext_MSPRoleSources(t *testing.T) {
	gin.SetMode(gin.TestMode)

	t.Run("msp_role 上下文键优先", func(t *testing.T) {
		c, _ := gin.CreateTestContext(httptest.NewRecorder())
		c.Set("user_id", 7)
		c.Set("tenant_id", 3)
		c.Set("role", "admin")
		c.Set("msp_role", "provider_admin")

		actor := provisionActorFromContext(c)

		assert.Equal(t, "provider_admin", actor.MSPRole)
		assert.Equal(t, 7, actor.UserID)
		assert.Equal(t, 3, actor.HomeTenantID)
	})

	t.Run("回退 MSPContext", func(t *testing.T) {
		c, _ := gin.CreateTestContext(httptest.NewRecorder())
		c.Set("user_id", 7)
		c.Set("tenant_id", 3)
		c.Set("role", "admin")
		c.Set(middleware.MSPContextKey, &middleware.MSPContext{
			IsMSP:     true,
			MSPUserID: 7,
			Role:      "provider_admin",
		})

		actor := provisionActorFromContext(c)

		assert.Equal(t, "provider_admin", actor.MSPRole)
	})

	t.Run("非 MSP 请求为空", func(t *testing.T) {
		c, _ := gin.CreateTestContext(httptest.NewRecorder())

		actor := provisionActorFromContext(c)

		assert.Empty(t, actor.MSPRole)
	})
}
