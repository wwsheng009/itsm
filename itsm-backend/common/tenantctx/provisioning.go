package tenantctx

import (
	"context"
	"fmt"
)

// provisioning.go：显式建号通道标记（IP-P0-5；canon K4/R9、ADR-004:A2/A3）。
//
// 与 SystemBypass 的区别：SystemBypass 面向迁移/seed/后台作业的"全域绕过"；
// ProvisioningBypass 只允许"在目标租户内建号"这一种跨租户写，且必须携带
// actor + channel + target，用于审计与静态检查（handler 不得自行拼装 bypass）。

// ProvisioningBypassInfo 记录建号通道的授权来源。
type ProvisioningBypassInfo struct {
	Actor          string
	Channel        string // platform | msp | tenant | invite
	TargetTenantID int
}

type provisioningKey struct{}

// WithProvisioningBypass 标记 ctx 处于显式建号通道。
// 调用顺序约定：先 WithTenantID(target) 再 WithProvisioningBypass(...)。
func WithProvisioningBypass(ctx context.Context, actor, channel string, targetTenantID int) context.Context {
	return context.WithValue(ctx, provisioningKey{}, ProvisioningBypassInfo{
		Actor:          actor,
		Channel:        channel,
		TargetTenantID: targetTenantID,
	})
}

// ProvisioningBypass 读取建号通道标记。
func ProvisioningBypass(ctx context.Context) (ProvisioningBypassInfo, bool) {
	info, ok := ctx.Value(provisioningKey{}).(ProvisioningBypassInfo)
	return info, ok
}

// IsProvisioningBypass 报告 ctx 是否处于显式建号通道。
func IsProvisioningBypass(ctx context.Context) bool {
	_, ok := ProvisioningBypass(ctx)
	return ok
}

// ProvisioningReason 返回可审计的 reason 字符串（写入审计事件与日志）。
func ProvisioningReason(ctx context.Context) (string, bool) {
	info, ok := ProvisioningBypass(ctx)
	if !ok {
		return "", false
	}
	return fmt.Sprintf("actor=%s;channel=%s;target_tenant=%d", info.Actor, info.Channel, info.TargetTenantID), true
}
