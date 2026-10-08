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
//   - 平台治理角色（super_admin/sysadmin）仅平台根作用域可见/可授予；平台根判定（D4=B）为
//     类型 internal/standard，或根租户标识 code=default 兜底——saas_msp 模式根租户类型为
//     msp_provider，但仍是平台治理主体。客户（msp_customer/saas_customer/legacy customer）与
//     服务商（msp_provider/legacy msp）的角色面一律隐藏，写入一律拒绝——非平台租户不该有
//     超级管理员/平台管理员。
//   - MSP 专属角色（msp_viewer/tech/specialist/manager/admin + provider_tenant_admin）仅服务商侧
//     词表；客户租户不得出现、持有或管理（由 membership 写路径承载服务商员工身份）。
//     provider 语义（IsMSPProviderTenantType）仍按类型判定，不受根租户兜底影响。

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

// tenantScopeOf 读取租户类型与标识（单次查询）；tenantID<=0 返回空值（保持旧行为，不查库）。
func tenantScopeOf(ctx context.Context, client *ent.Client, tenantID int) (kind, code string, err error) {
	if tenantID <= 0 {
		return "", "", nil
	}
	tn, err := client.Tenant.Query().Where(tenant.IDEQ(tenantID)).Only(ctx)
	if err != nil {
		return "", "", fmt.Errorf("查询租户失败: %w", err)
	}
	return string(tn.Type), tn.Code, nil
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

// hiddenRoleCodesForTenant 返回租户作用域下"不可见/不可管理/不可授予"的角色码集合：
// 平台根作用域（类型 internal/standard 或根租户 code=default，D4=B）不隐藏任何角色；
// 其余按租户类型口径（客户隐藏平台+MSP，服务商隐藏平台）。
func hiddenRoleCodesForTenant(kind, code string) []string {
	if isPlatformRootTenant(kind, code) {
		return nil
	}
	return hiddenRoleCodesForTenantType(kind)
}

// isHiddenRoleForTenant 判定角色码在给定租户作用域（类型+标识）下是否隐藏/禁止（大小写/空白容忍）。
func isHiddenRoleForTenant(kind, tenantCode, roleCode string) bool {
	roleCode = strings.ToLower(strings.TrimSpace(roleCode))
	for _, c := range hiddenRoleCodesForTenant(kind, tenantCode) {
		if c == roleCode {
			return true
		}
	}
	return false
}

// isPlatformRootTenantKind 平台根租户类型谓词（internal/standard）。
func isPlatformRootTenantKind(kind string) bool {
	return tenantmode.IsInternalTenantType(kind)
}

// rootTenantCode 根租户标识（D4=B）：seeder 固定根租户 code=default（tenants.code 唯一索引）。
const rootTenantCode = "default"

// isPlatformRootTenant 平台根作用域判定（D4=B 谓词拆分）：类型 internal/standard，
// 或根租户标识 code=default 兜底。仅承载"平台治理语义"（平台角色可见/可授予、
// 通配与 system:* 可分配）；provider 语义仍按类型判定，不受此影响。
func isPlatformRootTenant(kind, code string) bool {
	return isPlatformRootTenantKind(kind) || strings.EqualFold(strings.TrimSpace(code), rootTenantCode)
}
