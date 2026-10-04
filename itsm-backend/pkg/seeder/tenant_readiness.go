package seeder

import (
	"context"
	"fmt"

	"itsm-backend/dto"
	"itsm-backend/ent"
	"itsm-backend/ent/citype"
	"itsm-backend/ent/group"
	"itsm-backend/ent/menu"
	"itsm-backend/ent/permission"
	"itsm-backend/ent/role"
	"itsm-backend/ent/rolepermission"
	"itsm-backend/ent/sladefinition"
)

// TenantReadinessItem 是租户产品模板供给基线中的一项就绪度检查，
// 类型别名指向 dto.TenantReadinessItem：service 层通过接口消费本方法，
// 而 service 不能 import pkg/seeder（seeder → service 已有反向依赖），
// 共享 dto 定义是唯一不引入 import cycle 的方向。
type TenantReadinessItem = dto.TenantReadinessItem

// tenantReadinessCheck 是 7 项租户基线的共享定义（供给校验与 readiness API 同源）：
//   - key/label 面向 readiness HTTP 契约（英文键 + 中文标签）；
//   - legacyName 保留 validateTenantReadinessWithClient 既有错误文案中的检查名
//     （"validate tenant <legacyName>: ..."），不得随意改动，现有测试断言依赖它；
//   - count 返回该租户下的记录数。
type tenantReadinessCheck struct {
	key        string
	label      string
	legacyName string
	count      func(ctx context.Context, client *ent.Client, tenantID int) (int, error)
}

// tenantReadinessChecks 返回租户模板就绪度的 7 项 required 检查，顺序即 API items 顺序。
func tenantReadinessChecks() []tenantReadinessCheck {
	return []tenantReadinessCheck{
		{
			key: "roles", label: "角色", legacyName: "roles",
			count: func(ctx context.Context, client *ent.Client, tenantID int) (int, error) {
				return client.Role.Query().Where(role.TenantIDEQ(tenantID)).Count(ctx)
			},
		},
		{
			key: "permissions", label: "权限", legacyName: "permissions",
			count: func(ctx context.Context, client *ent.Client, tenantID int) (int, error) {
				return client.Permission.Query().Where(permission.TenantIDEQ(tenantID)).Count(ctx)
			},
		},
		{
			key: "role_permissions", label: "角色权限绑定", legacyName: "role permissions",
			count: func(ctx context.Context, client *ent.Client, tenantID int) (int, error) {
				return client.RolePermission.Query().Where(rolepermission.TenantIDEQ(tenantID)).Count(ctx)
			},
		},
		{
			key: "menus", label: "菜单", legacyName: "menus",
			count: func(ctx context.Context, client *ent.Client, tenantID int) (int, error) {
				return client.Menu.Query().Where(menu.TenantIDEQ(tenantID)).Count(ctx)
			},
		},
		{
			key: "groups", label: "审批组", legacyName: "groups",
			count: func(ctx context.Context, client *ent.Client, tenantID int) (int, error) {
				return client.Group.Query().Where(group.TenantIDEQ(tenantID)).Count(ctx)
			},
		},
		{
			key: "sla_definitions", label: "SLA 定义", legacyName: "SLA definitions",
			count: func(ctx context.Context, client *ent.Client, tenantID int) (int, error) {
				return client.SLADefinition.Query().Where(sladefinition.TenantIDEQ(tenantID)).Count(ctx)
			},
		},
		{
			key: "ci_types", label: "CI 类型", legacyName: "CI types",
			count: func(ctx context.Context, client *ent.Client, tenantID int) (int, error) {
				return client.CIType.Query().Where(citype.TenantIDEQ(tenantID)).Count(ctx)
			},
		},
	}
}

// TenantReadiness 返回租户 7 项模板基线的逐项计数（不判定 ready）。
// 供给判定（全部 required 项 > 0）由 service 层基于本结果计算。
func (s *Seeder) TenantReadiness(ctx context.Context, tenantID int) ([]TenantReadinessItem, error) {
	return tenantReadinessWithClient(ctx, s.client, tenantID)
}

func tenantReadinessWithClient(ctx context.Context, client *ent.Client, tenantID int) ([]TenantReadinessItem, error) {
	checks := tenantReadinessChecks()
	items := make([]TenantReadinessItem, 0, len(checks))
	for _, check := range checks {
		count, err := check.count(ctx, client, tenantID)
		if err != nil {
			return nil, fmt.Errorf("count tenant %s: %w", check.legacyName, err)
		}
		items = append(items, TenantReadinessItem{
			Key:      check.key,
			Label:    check.label,
			Count:    count,
			Required: true,
		})
	}
	return items, nil
}

// validateTenantReadinessWithClient 保持既有语义与错误文案不变：
//   - 查询失败 → "validate tenant <check>: <err>"
//   - 计数为 0 → "validate tenant <check>: no records installed"
func validateTenantReadinessWithClient(ctx context.Context, client *ent.Client, tenantID int) error {
	for _, check := range tenantReadinessChecks() {
		count, err := check.count(ctx, client, tenantID)
		if err != nil {
			return fmt.Errorf("validate tenant %s: %w", check.legacyName, err)
		}
		if count == 0 {
			return fmt.Errorf("validate tenant %s: no records installed", check.legacyName)
		}
	}
	return nil
}
