package middleware

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
)

// P1 演进（2026-09-26）回归门禁：《多 LLM Provider 支持与可切换方案》§3.4 的 provider
// 覆盖参数与选择器读端点（GET /ai/providers/available、GET /ai/user-preference）门禁
// 由 system:write 降为 ai:read。本测试锁定三个语义：
//  1. 内置普通角色（持 ai:read、无 system:write）通过门禁——P1 的放开目标；
//  2. 降级不得顺带放开 system:write——管理面仍限系统管理员（D12）；
//  3. 权限源不可得（client=nil）时非 super_admin 仍 fail-closed——不因降级产生绕过口径。
//
// 夹具与 TestHasResourcePermission 一致：HardcodeOnly 模式走 RolePermissions 兜底表
// （middleware/rbac.go：agent/technician/end_user 均含 {ai, read}，不含 {system, write}）。
func TestHasAIReadPermissionP1Downgrade(t *testing.T) {
	original := PermissionConfig.Mode
	PermissionConfig.Mode = PermissionConfigModeHardcodeOnly
	t.Cleanup(func() { PermissionConfig.Mode = original })

	ctx := context.Background()
	for _, role := range []string{"agent", "technician", "end_user"} {
		assert.True(t, HasAIReadPermission(ctx, nil, role, 1),
			"P1：%s 持 ai:read，应通过 provider 门禁", role)
		assert.False(t, hasResourcePermission(ctx, nil, role, "system", "write", 1),
			"P1 降级不得把 system:write 一并放给 %s", role)
	}

	assert.True(t, HasAIReadPermission(ctx, nil, "super_admin", 1), "super_admin 直通保留")
	assert.False(t, HasAIReadPermission(ctx, nil, "unknown_role", 1), "无权限角色 fail-closed")
}

// 默认 DBOnly 模式且权限源不可得（client=nil）时，非 super_admin 一律拒绝：
// 旧 system:write 门禁与新 ai:read 门禁都必须以权限数据为准，不得仅凭角色名放行。
func TestHasAIReadPermissionDBOnlyFailClosedWithoutPermissionSource(t *testing.T) {
	original := PermissionConfig.Mode
	PermissionConfig.Mode = PermissionConfigModeDBOnly
	t.Cleanup(func() { PermissionConfig.Mode = original })

	ctx := context.Background()
	assert.False(t, HasAIReadPermission(ctx, nil, "sysadmin", 1))
	assert.False(t, HasAIReadPermission(ctx, nil, "agent", 1))
	assert.True(t, HasAIReadPermission(ctx, nil, "super_admin", 1))
}
