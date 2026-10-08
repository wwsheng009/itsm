package service

import (
	"context"
	"fmt"
	"strings"

	"itsm-backend/ent"
	"itsm-backend/ent/tenant"
	"itsm-backend/middleware"
	"itsm-backend/pkg/tenantmode"
)

// tenant_scope.go：租户作用域下的角色可见性/可授予性口径（IP-P2-7 收口，规则单一来源）。
//
// 产品口径：
//   - 平台治理角色（super_admin/sysadmin）仅平台根租户（internal/standard）可见/可授予；
//     客户（msp_customer/saas_customer/legacy customer）与服务商（msp_provider/legacy msp）
//     的角色面一律隐藏，写入一律拒绝——非平台租户不该有超级管理员/平台管理员。
//   - MSP 专属角色（msp_viewer/tech/specialist/manager/admin + provider_tenant_admin）仅服务商侧
//     词表；客户租户不得出现、持有或管理（由 membership 写路径承载服务商员工身份）。

// tenantTypeOf 读取租户类型原文；tenantID<=0 返回空串（保持旧行为，不查库）。
func tenantTypeOf(ctx context.Context, client *ent.Client, tenantID int) (string, error) {
	if tenantID <= 0 {
		return "", nil
	}
	tn, err := client.Tenant.Query().Where(tenant.IDEQ(tenantID)).Only(ctx)
	if err != nil {
		return "", fmt.Errorf("查询租户失败: %w", err)
	}
	return string(tn.Type), nil
}

// hiddenRoleCodesForTenantType 返回该租户类型下"不可见/不可管理/不可授予"的角色码集合。
func hiddenRoleCodesForTenantType(kind string) []string {
	switch {
	case tenantmode.IsCustomerTenantType(kind):
		out := middleware.PlatformRoleCodes()
		return append(out, middleware.MSPOnlyRoleCodes()...)
	case tenantmode.IsMSPProviderTenantType(kind):
		return middleware.PlatformRoleCodes()
	default:
		return nil
	}
}

// isHiddenRoleForTenantType 判定角色码在给定租户类型下是否隐藏/禁止（大小写/空白容忍）。
func isHiddenRoleForTenantType(kind, code string) bool {
	code = strings.ToLower(strings.TrimSpace(code))
	for _, c := range hiddenRoleCodesForTenantType(kind) {
		if c == code {
			return true
		}
	}
	return false
}

// isPlatformRootTenantKind 平台根租户（internal/standard）：平台治理能力（通配权限等）仅此作用域可用。
func isPlatformRootTenantKind(kind string) bool {
	return tenantmode.IsInternalTenantType(kind)
}
