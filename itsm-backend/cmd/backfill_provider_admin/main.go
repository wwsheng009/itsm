// cmd/backfill_provider_admin：为存量 provider 租户回填组合角色 provider_tenant_admin
// （= admin ∪ msp_admin，2026-10-06 / 07:G11）。
//
// 背景：provider 首管策略改为绑定组合角色之前创建的租户，其首管 membership 仍指向
// 纯 MSP 角色（msp_admin / msp_manager 等）→ 自身 ITSM 菜单被权限过滤（只见 MSP 菜单）。本 CLI 幂等地：
//
//  1. 为每个 provider 租户补齐 provider_tenant_admin 角色行（default 模板租户一并补齐，
//     保证后续 ProvisionTenant 克隆包含该角色）；
//  2. 绑定权限 = 该租户 admin ∪ msp_admin 的权限并集（只增不减，不触碰其它角色）；
//  3. 把 bootstrap 管理员（is_bootstrap_admin）的 home membership 从纯 MSP 角色
//     （msp_admin / msp_manager / msp_tech 等，经 middleware.IsMSPOnlyRole 判定）
//     重绑到新角色，并补 user_roles 边（不删除历史边）；每次重绑写审计。
//
// 用法：
//
//	backfill_provider_admin                      # dry-run：只报告计划，不写库（默认）
//	backfill_provider_admin -dry-run=false       # 全部 provider 租户实际执行
//	backfill_provider_admin -tenant-id 10 -dry-run=false
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"sort"
	"strings"
	"time"

	"itsm-backend/common/tenantctx"
	"itsm-backend/config"
	"itsm-backend/database"
	"itsm-backend/ent"
	"itsm-backend/ent/role"
	"itsm-backend/ent/rolepermission"
	"itsm-backend/ent/tenant"
	"itsm-backend/ent/user"
	"itsm-backend/ent/usertenantmembership"
	"itsm-backend/middleware"
	"itsm-backend/pkg/tenantmode"
	"itsm-backend/service"

	"go.uber.org/zap"
)

const (
	targetRoleCode        = "provider_tenant_admin"
	targetRoleName        = "服务商租户管理员"
	targetRoleDescription = "服务商自有 ITSM + 跨客户 MSP（首管默认）"
)

func main() {
	tenantID := flag.Int("tenant-id", 0, "only backfill this tenant ID (0 = all provider tenants)")
	dryRun := flag.Bool("dry-run", true, "report planned changes without writing (pass -dry-run=false to apply)")
	flag.Parse()

	cfg, err := config.LoadConfig()
	if err != nil {
		fail(1, "load config: %v", err)
	}
	logger, err := zap.NewProduction()
	if err != nil {
		fail(1, "initialize logger: %v", err)
	}
	defer func() { _ = logger.Sync() }()
	sugar := logger.Sugar()

	client, err := database.InitDatabaseWithRLS(&cfg.Database, &cfg.RLS, sugar)
	if err != nil {
		fail(1, "connect database: %v", err)
	}
	defer func() { _ = client.Close() }()

	ctx := tenantctx.SystemContext(
		context.Background(),
		"cmd:backfill_provider_admin",
		"backfill provider_tenant_admin role (07:G11)",
	)

	tenants, err := resolveProviderTenants(ctx, client, *tenantID)
	if err != nil {
		sugar.Fatalw("resolve provider tenants failed", "error", err)
	}
	if len(tenants) == 0 {
		sugar.Infow("no provider tenants matched", "tenant_id", *tenantID)
		return
	}

	failures := 0
	for _, t := range tenants {
		if err := backfillTenant(ctx, client, sugar, t, *dryRun); err != nil {
			failures++
			sugar.Errorw("backfill tenant failed", "tenant_id", t.ID, "tenant_code", t.Code, "error", err)
		}
	}
	if failures > 0 {
		fail(1, "%d tenant(s) failed; see logs above", failures)
	}
}

// resolveProviderTenants 返回待处理的 provider 租户（含 default 模板租户）。
func resolveProviderTenants(ctx context.Context, client *ent.Client, tenantID int) ([]*ent.Tenant, error) {
	query := client.Tenant.Query().Order(ent.Asc(tenant.FieldID))
	if tenantID > 0 {
		query = query.Where(tenant.IDEQ(tenantID))
	}
	all, err := query.All(ctx)
	if err != nil {
		return nil, fmt.Errorf("query tenants: %w", err)
	}
	matched := make([]*ent.Tenant, 0, len(all))
	for _, t := range all {
		if !tenantmode.IsMSPProviderTenantType(string(t.Type)) {
			continue
		}
		matched = append(matched, t)
	}
	return matched, nil
}

// isRebindableMSPRole 判定 bootstrap 管理员当前 membership 角色是否属于可重绑的
// "纯 MSP 专属角色"：msp_viewer / msp_tech / msp_specialist / msp_manager / msp_admin
// （middleware.IsMSPOnlyRole 词表）。目标组合角色 provider_tenant_admin 自身由调用方
// alreadyDone 分支处理，此处显式排除，避免误判。
func isRebindableMSPRole(code string) bool {
	return middleware.IsMSPOnlyRole(code) && code != targetRoleCode
}

// backfillTenant 对单个 provider 租户执行幂等回填；dryRun 时只统计不写库。
func backfillTenant(ctx context.Context, client *ent.Client, sugar *zap.SugaredLogger, t *ent.Tenant, dryRun bool) error {
	adminRole, err := roleByCode(ctx, client, t.ID, "admin")
	if err != nil {
		return fmt.Errorf("resolve admin role: %w", err)
	}
	mspAdminRole, err := roleByCode(ctx, client, t.ID, "msp_admin")
	if err != nil {
		return fmt.Errorf("resolve msp_admin role: %w", err)
	}

	// 1) 角色行：不存在则创建；已存在则同步名称/描述（幂等）。
	target, err := client.Role.Query().
		Where(role.TenantIDEQ(t.ID), role.CodeEQ(targetRoleCode)).
		Only(ctx)
	roleCreated := false
	switch {
	case ent.IsNotFound(err):
		if !dryRun {
			target, err = client.Role.Create().
				SetName(targetRoleName).
				SetCode(targetRoleCode).
				SetDescription(targetRoleDescription).
				SetTenantID(t.ID).
				Save(ctx)
			if err != nil {
				return fmt.Errorf("create %s role: %w", targetRoleCode, err)
			}
			roleCreated = true
		}
	case err != nil:
		return fmt.Errorf("query %s role: %w", targetRoleCode, err)
	default:
		if !dryRun {
			if _, err := target.Update().
				SetName(targetRoleName).
				SetDescription(targetRoleDescription).
				Save(ctx); err != nil {
				return fmt.Errorf("update %s role: %w", targetRoleCode, err)
			}
		}
	}

	// 2) 权限并集：该租户 admin ∪ msp_admin 的 role_permissions。
	union := make(map[int]bool)
	for _, src := range []*ent.Role{adminRole, mspAdminRole} {
		rows, err := client.RolePermission.Query().
			Where(rolepermission.RoleIDEQ(src.ID), rolepermission.TenantIDEQ(t.ID)).
			All(ctx)
		if err != nil {
			return fmt.Errorf("query role_permissions of %s: %w", src.Code, err)
		}
		for _, rp := range rows {
			union[rp.PermissionID] = true
		}
	}

	permsAdded := 0
	if target != nil {
		existing := make(map[int]bool)
		rows, err := client.RolePermission.Query().
			Where(rolepermission.RoleIDEQ(target.ID), rolepermission.TenantIDEQ(t.ID)).
			All(ctx)
		if err != nil {
			return fmt.Errorf("query role_permissions of %s: %w", targetRoleCode, err)
		}
		for _, rp := range rows {
			existing[rp.PermissionID] = true
		}
		missing := make([]int, 0)
		for pid := range union {
			if !existing[pid] {
				missing = append(missing, pid)
			}
		}
		sort.Ints(missing)
		permsAdded = len(missing)
		if !dryRun && len(missing) > 0 {
			builders := make([]*ent.RolePermissionCreate, 0, len(missing))
			for _, pid := range missing {
				builders = append(builders,
					client.RolePermission.Create().
						SetRoleID(target.ID).
						SetPermissionID(pid).
						SetTenantID(t.ID))
			}
			if err := client.RolePermission.CreateBulk(builders...).Exec(ctx); err != nil {
				return fmt.Errorf("bind role_permissions: %w", err)
			}
		}
	} else {
		permsAdded = len(union) // dry-run：角色缺失，全部并集都将写入
	}

	// 3) 首管 membership 重绑（msp_admin → provider_tenant_admin）+ user_roles 边。
	admins, err := client.User.Query().
		Where(user.TenantIDEQ(t.ID), user.IsBootstrapAdminEQ(true)).
		All(ctx)
	if err != nil {
		return fmt.Errorf("query bootstrap admins: %w", err)
	}
	rebound := 0
	for _, admin := range admins {
		m, err := client.UserTenantMembership.Query().
			Where(
				usertenantmembership.UserIDEQ(admin.ID),
				usertenantmembership.TenantIDEQ(t.ID),
				usertenantmembership.DeletedAtIsNil(),
			).
			Only(ctx)
		if err != nil {
			sugar.Warnw("skip admin: no live membership", "tenant_id", t.ID, "user_id", admin.ID, "error", err)
			continue
		}
		currentCode := ""
		if m.RoleID != nil {
			if r, err := client.Role.Get(ctx, *m.RoleID); err == nil {
				currentCode = r.Code
			}
		}
		alreadyDone := currentCode == targetRoleCode
		if !alreadyDone {
			// 2026-10-10 扩展：存量漂移除 msp_admin 外还出现 msp_manager 变体（demo01/MSPB2C
			// 首管），凡"纯 MSP 专属角色"（IsMSPOnlyRole，排除目标角色自身）一律重绑到组合角色；
			// 非 MSP 角色（admin/agent 等）不属于本 CLI 修复面，保持跳过。
			if !isRebindableMSPRole(currentCode) {
				sugar.Warnw("skip admin: membership role is not a rebindable MSP-only role", "tenant_id", t.ID, "user_id", admin.ID, "role", currentCode)
				continue
			}
			rebound++
			if !dryRun {
				if target == nil {
					return fmt.Errorf("target role %s missing while rebinding user %d", targetRoleCode, admin.ID)
				}
				if err := client.UserTenantMembership.UpdateOneID(m.ID).
					SetRoleID(target.ID).
					Exec(ctx); err != nil {
					return fmt.Errorf("rebind membership of user %d: %w", admin.ID, err)
				}
				withRoles, err := client.User.Query().Where(user.IDEQ(admin.ID)).WithRoles().Only(ctx)
				if err != nil {
					return fmt.Errorf("load user %d roles: %w", admin.ID, err)
				}
				hasEdge := false
				for _, r := range withRoles.Edges.Roles {
					if r.ID == target.ID {
						hasEdge = true
						break
					}
				}
				if !hasEdge {
					if err := client.User.UpdateOneID(admin.ID).AddRoleIDs(target.ID).Exec(ctx); err != nil {
						return fmt.Errorf("add user_roles edge for user %d: %w", admin.ID, err)
					}
				}
				if _, err := client.AuditLog.Create().
					SetCreatedAt(time.Now()).
					SetTenantID(t.ID).
					SetTargetTenantID(t.ID).
					SetTargetUserID(admin.ID).
					SetSource(middleware.AuditSourceSystem).
					SetResource("user_tenant_membership").
					SetAction("rbac.provider_tenant_admin_backfill").
					SetPath("cmd:backfill_provider_admin").
					SetMethod("CLI").
					SetStatusCode(200).
					SetActorAccount("cli:backfill_provider_admin").
					Save(ctx); err != nil {
					sugar.Warnw("audit write failed", "tenant_id", t.ID, "user_id", admin.ID, "error", err)
				}
			}
		}
		if dryRun {
			continue
		}
		// 效果验证（G11）：按与 /auth/menus 同一服务计算首管可见菜单，确保
		// 「自身 ITSM + 跨客户 MSP」双面菜单实际可见（幂等复跑时同样输出）。
		menuSvc := service.NewMenuService(client, sugar)
		tree, err := menuSvc.GetUserMenus(ctx, admin.ID, t.ID)
		if err != nil {
			sugar.Warnw("menu probe failed", "tenant_id", t.ID, "user_id", admin.ID, "error", err)
			continue
		}
		names := make([]string, 0, len(tree.Main))
		for _, item := range tree.Main {
			names = append(names, item.Name)
		}
		sugar.Infow("bootstrap admin menus after backfill",
			"tenant_id", t.ID,
			"user_id", admin.ID,
			"main_menus", len(tree.Main),
			"admin_menus", len(tree.Admin),
			"main_names", strings.Join(names, ","),
		)
	}

	sugar.Infow("provider_tenant_admin backfill",
		"tenant_id", t.ID,
		"tenant_code", t.Code,
		"dry_run", dryRun,
		"role_created", roleCreated,
		"perms_added", permsAdded,
		"admins_rebound", rebound,
	)
	return nil
}

func roleByCode(ctx context.Context, client *ent.Client, tenantID int, code string) (*ent.Role, error) {
	return client.Role.Query().
		Where(role.TenantIDEQ(tenantID), role.CodeEQ(code)).
		Only(ctx)
}

func fail(code int, format string, args ...any) {
	fmt.Fprintf(os.Stderr, format+"\n", args...)
	os.Exit(code)
}
