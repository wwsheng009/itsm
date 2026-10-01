package tenantctx

import (
	"context"
	"errors"
	"testing"
)

func TestEnsureJobTenant(t *testing.T) {
	base := context.Background()

	// 无 ctx 租户（如枚举后的直接调用）→ 允许，由调用方显式注入。
	if err := EnsureJobTenant(base, 7); err != nil {
		t.Fatalf("missing ctx tenant should be injectable, got %v", err)
	}
	// 任务无租户身份 → fail-closed。
	if err := EnsureJobTenant(base, 0); !errors.Is(err, ErrNoTenant) {
		t.Fatalf("want ErrNoTenant, got %v", err)
	}
	// ctx 租户一致 → 通过。
	if err := EnsureJobTenant(WithTenantID(base, 7), 7); err != nil {
		t.Fatalf("matching tenant should pass, got %v", err)
	}
	// 错误 ctx（跨租户）→ 拒绝；系统旁路不免除执行期 mismatch。
	for name, ctx := range map[string]context.Context{
		"plain":         WithTenantID(base, 9),
		"system_bypass": WithSystemBypass(WithTenantID(base, 9)),
	} {
		if err := EnsureJobTenant(ctx, 7); !errors.Is(err, ErrTenantMismatch) {
			t.Fatalf("%s: want ErrTenantMismatch, got %v", name, err)
		}
	}
	// 纯系统旁路（无租户）+ 显式任务租户 → 允许（由执行器注入）。
	if err := EnsureJobTenant(WithSystemBypass(base), 7); err != nil {
		t.Fatalf("system bypass without tenant should pass, got %v", err)
	}
}
