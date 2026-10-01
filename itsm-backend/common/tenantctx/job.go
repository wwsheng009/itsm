package tenantctx

import (
	"context"
	"errors"
	"fmt"
)

// ErrTenantMismatch 表示任务携带的租户与 ctx 租户不一致（fail-closed 拒绝）。
var ErrTenantMismatch = errors.New("tenantctx: job tenant mismatch")

// EnsureJobTenant 校验后台任务执行前的租户上下文（集成分析 §5.2 fail-closed）：
//
//  1. 任务必须携带正数 tenant_id（无租户身份的 job 不允许执行）；
//  2. ctx 已携带 tenant 时必须与任务租户一致 —— 错误 ctx 不得执行跨租户 job；
//
// 系统旁路（WithSystemBypass/SystemContext）仅用于"领取/枚举"阶段，不能豁免本校验：
// 执行前必须显式 WithTenantID(task.TenantID) 收窄。
func EnsureJobTenant(ctx context.Context, tenantID int) error {
	if tenantID <= 0 {
		return fmt.Errorf("%w: job tenant_id missing or invalid (%d)", ErrNoTenant, tenantID)
	}
	if v, ok := TenantID(ctx); ok && v != tenantID {
		return fmt.Errorf("%w: ctx=%d job=%d", ErrTenantMismatch, v, tenantID)
	}
	return nil
}
