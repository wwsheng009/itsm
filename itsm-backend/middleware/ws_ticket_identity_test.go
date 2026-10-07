package middleware

import (
	"net/http/httptest"
	"testing"

	"itsm-backend/ent/enttest"

	"github.com/gin-gonic/gin"
)

// TestWSTicketIssuanceIsIdentitySelfService 锁定身份自省面口径（2026-10-06 修复）：
// POST /api/v1/ws/ticket 颁发的是「调用者本人会话」的一次性票据，授权依据是
// 已认证身份（AuthMiddleware 先行校验），不依赖资源动作权限。修复前该路由不在
// ResourceActionMap（无 RequirePermission 声明，属白名单身份自省面），非 super_admin
// 角色在预检 nil 映射处被 fail-closed 403「权限不足」，通知 WS 对全部普通角色不可用
// （super_admin 直通掩盖了该缺陷）。
func TestWSTicketIssuanceIsIdentitySelfService(t *testing.T) {
	gin.SetMode(gin.TestMode)
	client := enttest.Open(t, "sqlite3", "file:ws_ticket_identity?mode=memory&cache=shared&_fk=1")
	defer client.Close()

	for _, role := range []string{"admin", "provider_tenant_admin", "customer_user", "end_user"} {
		c, _ := gin.CreateTestContext(httptest.NewRecorder())
		c.Request = httptest.NewRequest("POST", "/api/v1/ws/ticket", nil)
		c.Set("user_id", 42)
		c.Set("tenant_id", 7)
		c.Set("role", role)

		if !hasPermission(client, role, "POST", "/api/v1/ws/ticket", 42, 7, c) {
			t.Fatalf("role %s: POST /api/v1/ws/ticket 应仅凭已认证身份放行", role)
		}
	}
}

// TestUnmappedNonSelfServicePathStaysDenied 反向守护：无映射的普通路径仍 fail-closed，
// 防止「身份自省放行」被误扩为通用豁免。
func TestUnmappedNonSelfServicePathStaysDenied(t *testing.T) {
	gin.SetMode(gin.TestMode)
	client := enttest.Open(t, "sqlite3", "file:ws_ticket_identity_neg?mode=memory&cache=shared&_fk=1")
	defer client.Close()

	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest("POST", "/api/v1/unmapped/probe", nil)
	c.Set("user_id", 42)
	c.Set("tenant_id", 7)
	c.Set("role", "end_user")

	if hasPermission(client, "end_user", "POST", "/api/v1/unmapped/probe", 42, 7, c) {
		t.Fatal("无映射的普通路径不得放行（应 fail-closed）")
	}
}
