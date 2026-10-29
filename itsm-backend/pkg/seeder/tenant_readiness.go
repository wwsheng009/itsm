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
	"itsm-backend/database"
	"itsm-backend/ent/servicecatalogitem"
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
	// required=true 的项参与「基线就绪」硬校验（validateTenantReadiness 计数为 0 即不就绪）；
	// required=false 仅作为「业务数据待配置」提示项展示，不影响开通/就绪结论
	// （P1-1：服务目录项 / 工单类型属于业务数据，不应冲淡基线就绪语义）。
	required   bool
	count      func(ctx context.Context, client *ent.Client, tenantID int) (int, error)
}

// tenantReadinessChecks 返回租户模板就绪度的 7 项 required 检查，顺序即 API items 顺序。
func tenantReadinessChecks() []tenantReadinessCheck {
	return []tenantReadinessCheck{
		{
			key: "roles", label: "角色", legacyName: "roles",
			required: true,
			count: func(ctx context.Context, client *ent.Client, tenantID int) (int, error) {
				return client.Role.Query().Where(role.TenantIDEQ(tenantID)).Count(ctx)
			},
		},
		{
			key: "permissions", label: "权限", legacyName: "permissions",
			required: true,
			count: func(ctx context.Context, client *ent.Client, tenantID int) (int, error) {
				return client.Permission.Query().Where(permission.TenantIDEQ(tenantID)).Count(ctx)
			},
		},
		{
			key: "role_permissions", label: "角色权限绑定", legacyName: "role permissions",
			required: true,
			count: func(ctx context.Context, client *ent.Client, tenantID int) (int, error) {
				return client.RolePermission.Query().Where(rolepermission.TenantIDEQ(tenantID)).Count(ctx)
			},
		},
		{
			key: "menus", label: "菜单", legacyName: "menus",
			required: true,
			count: func(ctx context.Context, client *ent.Client, tenantID int) (int, error) {
				return client.Menu.Query().Where(menu.TenantIDEQ(tenantID)).Count(ctx)
			},
		},
		{
			key: "groups", label: "审批组", legacyName: "groups",
			required: true,
			count: func(ctx context.Context, client *ent.Client, tenantID int) (int, error) {
				return client.Group.Query().Where(group.TenantIDEQ(tenantID)).Count(ctx)
			},
		},
		{
			key: "sla_definitions", label: "SLA 定义", legacyName: "SLA definitions",
			required: true,
			count: func(ctx context.Context, client *ent.Client, tenantID int) (int, error) {
				return client.SLADefinition.Query().Where(sladefinition.TenantIDEQ(tenantID)).Count(ctx)
			},
		},
		{
			key: "ci_types", label: "CI 类型", legacyName: "CI types",
			required: true,
			count: func(ctx context.Context, client *ent.Client, tenantID int) (int, error) {
				return client.CIType.Query().Where(citype.TenantIDEQ(tenantID)).Count(ctx)
			},
		},
		// 业务数据提示项（非 required）：新租户开通「全绿」后，区分「基线就绪」与
		// 「业务数据待配置」，让开通向导能提示客户去创建服务目录 / 工单类型（P1-1）。
		{
			key: "service_catalog_items", label: "服务目录项", legacyName: "service catalog items",
			required: false,
			count: func(ctx context.Context, client *ent.Client, tenantID int) (int, error) {
				return client.ServiceCatalogItem.Query().Where(servicecatalogitem.TenantIDEQ(tenantID)).Count(ctx)
			},
		},
		{
			key: "ticket_types", label: "工单类型", legacyName: "ticket types",
			required: false,
			count: func(ctx context.Context, client *ent.Client, tenantID int) (int, error) {
				rawDB := database.GetRawDB()
				if rawDB == nil {
					return 0, nil
				}
				var cnt int
				if err := rawDB.QueryRowContext(ctx, "SELECT COUNT(*) FROM ticket_types WHERE tenant_id = $1", tenantID).Scan(&cnt); err != nil {
					// 表不存在或查询失败：业务数据提示项，不计为就绪失败。
					return 0, nil
				}
				return cnt, nil
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
			Required: check.required,
		})
	}
	return items, nil
}

// validateTenantReadinessWithClient 保持既有语义与错误文案不变：
//   - 查询失败 → "validate tenant <check>: <err>"
//   - 计数为 0 → "validate tenant <check>: no records installed"
func validateTenantReadinessWithClient(ctx context.Context, client *ent.Client, tenantID int) error {
	for _, check := range tenantReadinessChecks() {
		if !check.required {
			// 业务数据提示项不参与「基线就绪」硬校验（P1-1）。
			continue
		}
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
