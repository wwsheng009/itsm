// role_permission_sync.go：内置角色权限跨租户同步（P1 workflow:* 缺口修复配套）。
//
// 背景：DBOnly 模式下 role_permissions 是授权唯一真源；启动 seed 仅同步 default 租户，
// 存量租户（含 provider/客户租户）在词表新增权限码后不会自动获得。
// 本文件提供显式、幂等的对齐能力（增缺 + 删除词表管理范围内的过期授予；词表外自定义权限保留），
// 供 cmd/sync_role_permissions 使用；新租户开通仍由 tenant_provisioner 从源租户克隆（无需本工具）。
package seeder

import (
	"context"
	"fmt"

	"itsm-backend/ent"
	"itsm-backend/ent/permission"
	"itsm-backend/ent/role"
	"itsm-backend/ent/rolepermission"
	"itsm-backend/internal/authz"
)

// RolePermissionSyncResult 单租户同步结果（dry-run 时为待增/待删数）。
type RolePermissionSyncResult struct {
	TenantID   int
	TenantCode string
	Roles      int // 命中词表的内置角色数
	Created    int // 已增/待增授权数
	Deleted    int // 已删/待删授权数
}

// SyncBuiltinRolePermissionsForTenant 把单个租户的内置角色权限对齐到 authz 词表。
// dryRun=true 时只计算差异不落库。幂等：复跑 created/deleted 均为 0。
func SyncBuiltinRolePermissionsForTenant(ctx context.Context, client *ent.Client, tenantID int, dryRun bool) (RolePermissionSyncResult, error) {
	res := RolePermissionSyncResult{TenantID: tenantID}
	if client == nil {
		return res, fmt.Errorf("ent client is nil")
	}
	t, err := client.Tenant.Get(ctx, tenantID)
	if err != nil {
		return res, fmt.Errorf("load tenant %d: %w", tenantID, err)
	}
	res.TenantCode = t.Code

	perms, err := client.Permission.Query().Where(permission.TenantIDEQ(tenantID)).All(ctx)
	if err != nil {
		return res, fmt.Errorf("list permissions of tenant %d: %w", tenantID, err)
	}
	permByCode := make(map[string]int, len(perms))
	for _, p := range perms {
		permByCode[p.Code] = p.ID
	}

	rolePermissionMap := authz.BuiltinRolePermissionCodes()

	// 词表管理范围：任何内置角色引用到的权限码（范围外 = 租户自定义，不动）。
	managed := make(map[int]bool)
	for _, codes := range rolePermissionMap {
		for _, code := range codes {
			if id, ok := permByCode[code]; ok {
				managed[id] = true
			}
		}
	}

	roles, err := client.Role.Query().Where(role.TenantIDEQ(tenantID)).All(ctx)
	if err != nil {
		return res, fmt.Errorf("list roles of tenant %d: %w", tenantID, err)
	}

	for _, r := range roles {
		codes, ok := rolePermissionMap[r.Code]
		if !ok {
			continue // 非内置角色跳过
		}
		want := make(map[int]bool, len(codes))
		for _, code := range codes {
			if id, ok := permByCode[code]; ok {
				want[id] = true
			}
		}
		if len(want) == 0 {
			continue
		}
		res.Roles++

		existing, err := client.RolePermission.Query().
			Where(rolepermission.RoleIDEQ(r.ID), rolepermission.TenantIDEQ(tenantID)).
			All(ctx)
		if err != nil {
			return res, fmt.Errorf("list grants of role %s: %w", r.Code, err)
		}
		have := make(map[int]bool, len(existing))
		toDelete := make([]int, 0)
		for _, g := range existing {
			have[g.PermissionID] = true
			if managed[g.PermissionID] && !want[g.PermissionID] {
				toDelete = append(toDelete, g.PermissionID)
			}
		}
		toCreate := make([]int, 0)
		for id := range want {
			if !have[id] {
				toCreate = append(toCreate, id)
			}
		}
		res.Created += len(toCreate)
		res.Deleted += len(toDelete)

		if dryRun {
			continue
		}
		if len(toDelete) > 0 {
			if _, err := client.RolePermission.Delete().
				Where(
					rolepermission.RoleIDEQ(r.ID),
					rolepermission.TenantIDEQ(tenantID),
					rolepermission.PermissionIDIn(toDelete...),
				).
				Exec(ctx); err != nil {
				return res, fmt.Errorf("delete obsolete grants of role %s: %w", r.Code, err)
			}
		}
		if len(toCreate) > 0 {
			builders := make([]*ent.RolePermissionCreate, 0, len(toCreate))
			for _, pid := range toCreate {
				builders = append(builders, client.RolePermission.Create().
					SetRoleID(r.ID).
					SetPermissionID(pid).
					SetTenantID(tenantID))
			}
			if _, err := client.RolePermission.CreateBulk(builders...).Save(ctx); err != nil {
				return res, fmt.Errorf("create missing grants of role %s: %w", r.Code, err)
			}
		}
	}
	return res, nil
}

// SyncBuiltinRolePermissionsAllTenants 对全部租户执行同步（按 id 升序，逐租户返回结果）。
func SyncBuiltinRolePermissionsAllTenants(ctx context.Context, client *ent.Client, dryRun bool) ([]RolePermissionSyncResult, error) {
	tenants, err := client.Tenant.Query().Order(ent.Asc("id")).All(ctx)
	if err != nil {
		return nil, fmt.Errorf("list tenants: %w", err)
	}
	out := make([]RolePermissionSyncResult, 0, len(tenants))
	for _, t := range tenants {
		res, err := SyncBuiltinRolePermissionsForTenant(ctx, client, t.ID, dryRun)
		if err != nil {
			return out, err
		}
		out = append(out, res)
	}
	return out, nil
}
