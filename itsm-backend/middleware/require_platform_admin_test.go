package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
)

// require_platform_admin_test.go：平台治理面身份硬校验（IP-P2-7 收口补丁 4）。
// 判定取「主角色 + m2m 附加角色」并集，与 GetContextRoles 同口径。

func TestRequirePlatformAdmin(t *testing.T) {
	gin.SetMode(gin.TestMode)

	cases := []struct {
		name   string
		role   string
		roles  []string
		expect int
	}{
		{name: "super_admin passes", role: "super_admin", expect: http.StatusOK},
		{name: "sysadmin passes", role: "sysadmin", expect: http.StatusOK},
		{name: "customer admin denied", role: "admin", expect: http.StatusForbidden},
		{name: "agent denied", role: "agent", expect: http.StatusForbidden},
		{name: "m2m platform role passes", role: "admin", roles: []string{"admin", "sysadmin"}, expect: http.StatusOK},
		{name: "m2m msp role denied", role: "admin", roles: []string{"admin", "msp_manager"}, expect: http.StatusForbidden},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r := gin.New()
			r.GET("/probe",
				func(c *gin.Context) {
					c.Set("role", tc.role)
					if tc.roles != nil {
						c.Set("roles", tc.roles)
					}
				},
				RequirePlatformAdmin(),
				func(c *gin.Context) { c.Status(http.StatusOK) },
			)

			w := httptest.NewRecorder()
			req := httptest.NewRequest(http.MethodGet, "/probe", nil)
			r.ServeHTTP(w, req)

			assert.Equal(t, tc.expect, w.Code)
		})
	}
}
