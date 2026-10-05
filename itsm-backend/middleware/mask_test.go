package middleware

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// TUM-3 复核：密码族键名掩码必须同时覆盖 snake_case 与 camelCase，
// 防止重置密码（mode=specified，字段 newPassword）明文落入审计/日志。
func TestMaskSensitiveFields_PasswordKeyVariants(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string
	}{
		{"password", `{"password":"NewPass456!"}`, `{"password":"***"}`},
		{"passwd", `{"passwd":"NewPass456!"}`, `{"passwd":"***"}`},
		{"pwd", `{"pwd":"NewPass456!"}`, `{"pwd":"***"}`},
		{"current_password", `{"current_password":"NewPass456!"}`, `{"current_password":"***"}`},
		{"new_password", `{"new_password":"NewPass456!"}`, `{"new_password":"***"}`},
		{"confirm_password", `{"confirm_password":"NewPass456!"}`, `{"confirm_password":"***"}`},
		{"old_password", `{"old_password":"NewPass456!"}`, `{"old_password":"***"}`},
		{"newPassword camelCase", `{"newPassword":"NewPass456!"}`, `{"newPassword":"***"}`},
		{"currentPassword camelCase", `{"currentPassword":"NewPass456!"}`, `{"currentPassword":"***"}`},
		{"confirmPassword camelCase", `{"confirmPassword":"NewPass456!"}`, `{"confirmPassword":"***"}`},
		{"oldPassword camelCase", `{"oldPassword":"NewPass456!"}`, `{"oldPassword":"***"}`},
		{"non-sensitive fields untouched", `{"username":"alice","active":false}`, `{"username":"alice","active":false}`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, MaskSensitiveFields(tc.in))
		})
	}
}

// 重置密码请求体（平台侧 TUM-2 mode=specified）整体掩码：明文不得残留。
func TestMaskSensitiveFields_ResetPasswordBody(t *testing.T) {
	const plain = "Xk7#mQ2pL9@vR4tZ"
	body := `{"mode":"specified","newPassword":"` + plain + `","requireChange":true}`
	masked := MaskSensitiveFields(body)
	assert.NotContains(t, masked, plain)
	assert.Contains(t, masked, `"newPassword":"***"`)
	assert.Contains(t, masked, `"mode":"specified"`)
}
