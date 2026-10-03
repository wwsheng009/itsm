package middleware

import (
	"context"
	"errors"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"itsm-backend/common/tenantctx"
	"itsm-backend/ent"
	"itsm-backend/ent/permission"
	"itsm-backend/ent/role"
	"itsm-backend/ent/rolepermission"
	"itsm-backend/ent/user"
	"itsm-backend/ent/usertenantmembership"

	"go.uber.org/zap"
)

// =============================================================================
// IP-P1-2 权限单源：membership.role → role_permissions
//
// 目标：登录 / 刷新 / 切换租户 / auth.me / 菜单生成统一以「(user_id, tenant_id)
// 的存活 membership 行 → role_id → 同租户 roles 行 → role_permissions → permissions」
// 作为权限唯一计算路径。静态 RolePermissions 表仅在迁移窗口内、且显式打开
// AUTHZ_STATIC_FALLBACK 时作为回退；默认关闭时无 membership = fail-closed。
//
// 本文件放在 middleware 包的理由：middleware 是现有 RBAC/权限缓存的既有归属层，
// 且 service（menu_service.go）与 handlers 两侧都已依赖 middleware；解析器落在
// 这里可被两端复用而不引入任何循环依赖（middleware 不反向依赖 service/handlers）。
// 请求期 RBACMiddleware 的 4 层回退链（rbac.go/smart_permission.go）仍按 JWT 角色码
// 判定，本次仅收敛「端点权限清单 + 菜单」；请求期单源切换见报告遗留项。
// =============================================================================

// PermissionSource 描述一次权限解析结果的来源，便于日志与测试断言。
type PermissionSource string

const (
	// PermissionSourceMembership：主路径命中（membership → role → role_permissions）。
	PermissionSourceMembership PermissionSource = "membership"
	// PermissionSourceStaticFallback：迁移窗口内显式开启的静态表回退。
	PermissionSourceStaticFallback PermissionSource = "static_fallback"
	// PermissionSourceSuperAdmin：super_admin 硬编码直通（保持现状）。
	PermissionSourceSuperAdmin PermissionSource = "super_admin"
	// PermissionSourceNone：fail-closed（无任何授权）。
	PermissionSourceNone PermissionSource = "none"
)

// authzStaticFallback AUTHZ_STATIC_FALLBACK 开关，默认 false（关闭）。
//
// 关闭（默认）：membership 缺失 / role_id 为空 / 角色行无法解析时 fail-closed；
// 开启：上述场景回退到编译期 RolePermissions（按 users.role 与 msp_role 映射），
// 仅用于 IP-P1-1 回填尚未覆盖的迁移窗口，并在每次回退时记 Warn 日志。
var authzStaticFallback atomic.Bool

// SetAuthzStaticFallback 设置静态权限表回退开关（启动期由 config 注入；测试可直接调用）。
func SetAuthzStaticFallback(enabled bool) {
	authzStaticFallback.Store(enabled)
	InvalidateAllPermissionCaches()
}

// AuthzStaticFallbackEnabled 返回当前静态回退开关状态（默认 false=关闭）。
func AuthzStaticFallbackEnabled() bool {
	return authzStaticFallback.Load()
}

// membershipPermission 用户-租户级权限解析缓存条目（key 含 user_id + tenant_id）。
type membershipPermission struct {
	codes     []string
	expiresAt time.Time
}

var (
	membershipPermissionCache     = make(map[string]*membershipPermission)
	membershipPermissionCacheLock sync.RWMutex
)

func membershipPermissionCacheKey(userID, tenantID int) string {
	return strconv.Itoa(userID) + "_" + strconv.Itoa(tenantID)
}

// loadMembershipPermissionFromCache 命中且未过期时返回缓存副本与 true。
// 缓存 key 含 user_id + tenant_id：同一账号在 A/B 租户各自独立，不会串租户。
func loadMembershipPermissionFromCache(userID, tenantID int) ([]string, bool) {
	if !PermissionConfig.EnableCache {
		return nil, false
	}
	key := membershipPermissionCacheKey(userID, tenantID)
	membershipPermissionCacheLock.RLock()
	entry, ok := membershipPermissionCache[key]
	if ok && time.Now().Before(entry.expiresAt) {
		codes := append([]string(nil), entry.codes...)
		membershipPermissionCacheLock.RUnlock()
		return codes, true
	}
	membershipPermissionCacheLock.RUnlock()
	return nil, false
}

func storeMembershipPermissionToCache(userID, tenantID int, codes []string) {
	if !PermissionConfig.EnableCache {
		return
	}
	key := membershipPermissionCacheKey(userID, tenantID)
	copied := append([]string(nil), codes...)
	membershipPermissionCacheLock.Lock()
	membershipPermissionCache[key] = &membershipPermission{
		codes:     copied,
		expiresAt: time.Now().Add(permissionCacheTTL),
	}
	membershipPermissionCacheLock.Unlock()
}

// invalidateMembershipPermissionCacheForTenant 清理某租户全部用户级权限缓存。
// 角色/权限变更走既有 InvalidateRolePermissionCache(role, tenant) 链路时调用：
// 现有表结构无法由 role 反查受影响用户，故按租户整体失效（保守但正确）。
func invalidateMembershipPermissionCacheForTenant(tenantID int) {
	suffix := "_" + strconv.Itoa(tenantID)
	membershipPermissionCacheLock.Lock()
	for key := range membershipPermissionCache {
		if strings.HasSuffix(key, suffix) {
			delete(membershipPermissionCache, key)
		}
	}
	membershipPermissionCacheLock.Unlock()
}

// clearMembershipPermissionCache 清理全部用户级权限缓存。
func clearMembershipPermissionCache() {
	membershipPermissionCacheLock.Lock()
	clear(membershipPermissionCache)
	membershipPermissionCacheLock.Unlock()
}

// ResolvePermissions 计算 (userID, tenantID) 的权限码集合（resource:action，已排序去重）。
// 调用方：Login / RefreshToken / SwitchTenant / GetMe / 菜单生成（IP-P1-2）。
//
// 解析顺序（IP-P1-2 单一真源）：
//  1. super_admin 硬编码直通 → ["*"]（保持既有语义）；
//  2. 存活 membership（deleted_at IS NULL 且 status=active 且 tenant_id=目标租户）
//     → role_id → roles(id=role_id 且 tenant_id=目标租户) → role_permissions
//     → permissions（同租户）→ 权限码；
//  3. membership 缺失 / role_id 为空 / 角色行无法解析：
//     - AUTHZ_STATIC_FALLBACK=true → 回退静态 RolePermissions（users.role + msp_role 映射并集），
//     source=static_fallback，并记 Warn；
//     - 默认 false → 返回空集合 + source=none，并记 Warn（fail-closed）。
//  4. 缓存：仅缓存主路径（membership）结果，key=user_id+tenant_id；失效见 SetAuthzStaticFallback /
//     InvalidateAllPermissionCaches / InvalidateRolePermissionCache（按租户清理）。
//
// 错误语义：只有 DB 查询/客户端不可用等基础设施错误才返回 error（此时同样返回空集合）；
// "无 membership" 属于业务态，不返回 error。调用方可直接使用返回值，error 仅用于日志。
func ResolvePermissions(ctx context.Context, client *ent.Client, userID, tenantID int) ([]string, PermissionSource, error) {
	if client == nil {
		zap.S().Warnw("ResolvePermissions: ent client unavailable, fail-closed",
			"user_id", userID, "tenant_id", tenantID, "reason", "client_nil")
		return nil, PermissionSourceNone, errors.New("ent client is nil")
	}
	if userID <= 0 || tenantID <= 0 {
		zap.S().Warnw("ResolvePermissions: invalid scope, fail-closed",
			"user_id", userID, "tenant_id", tenantID, "reason", "invalid_scope")
		return nil, PermissionSourceNone, errors.New("invalid user/tenant scope")
	}

	// R2B 阴影观察（2026-10-03）：调用方（RBAC 预检）可能先于租户中间件执行；
	// 本函数全部查询按 tenantID 收窄，显式补齐租户 ctx（enforce 前置）。
	ctx = tenantctx.WithTenantID(ctx, tenantID)

	userEntity, err := client.User.Get(ctx, userID)
	if err != nil {
		zap.S().Warnw("ResolvePermissions: user lookup failed, fail-closed",
			"user_id", userID, "tenant_id", tenantID, "reason", "user_lookup_failed", "error", err)
		return nil, PermissionSourceNone, err
	}
	// super_admin 硬编码直通（保持现状；不依赖 membership 与租户内角色行）。
	if userEntity.Role == user.RoleSuperAdmin {
		return []string{"*"}, PermissionSourceSuperAdmin, nil
	}

	if codes, ok := loadMembershipPermissionFromCache(userID, tenantID); ok {
		return codes, PermissionSourceMembership, nil
	}

	membership, err := client.UserTenantMembership.Query().
		Where(
			usertenantmembership.UserID(userID),
			usertenantmembership.TenantID(tenantID),
			usertenantmembership.DeletedAtIsNil(),
			usertenantmembership.StatusEQ(usertenantmembership.StatusActive),
		).
		Only(ctx)
	if err != nil && !ent.IsNotFound(err) {
		zap.S().Warnw("ResolvePermissions: membership lookup failed, fail-closed",
			"user_id", userID, "tenant_id", tenantID, "reason", "membership_lookup_failed", "error", err)
		return nil, PermissionSourceNone, err
	}
	if membership == nil {
		return resolveViaStaticFallbackOrDeny(userEntity, nil, userID, tenantID, "membership_missing")
	}
	if membership.RoleID == nil {
		return resolveViaStaticFallbackOrDeny(userEntity, membership, userID, tenantID, "membership_role_id_missing")
	}

	roleEntity, err := client.Role.Query().
		Where(
			role.IDEQ(*membership.RoleID),
			role.TenantID(tenantID),
		).
		Only(ctx)
	if err != nil || roleEntity == nil {
		reason := "membership_role_unresolved"
		if err != nil && !ent.IsNotFound(err) {
			zap.S().Warnw("ResolvePermissions: role lookup failed",
				"user_id", userID, "tenant_id", tenantID, "role_id", *membership.RoleID, "error", err)
		}
		return resolveViaStaticFallbackOrDeny(userEntity, membership, userID, tenantID, reason)
	}
	if roleEntity.Code == "super_admin" {
		return []string{"*"}, PermissionSourceSuperAdmin, nil
	}

	codes, err := loadPermissionCodesByRoleID(ctx, client, roleEntity.ID, tenantID)
	if err != nil {
		zap.S().Warnw("ResolvePermissions: role_permissions load failed, fail-closed",
			"user_id", userID, "tenant_id", tenantID, "role_id", roleEntity.ID,
			"reason", "role_permissions_load_failed", "error", err)
		return nil, PermissionSourceNone, err
	}

	// 角色行存在即视为已配置：空集 = 显式撤销，保持 fail-closed，不做静态回退。
	storeMembershipPermissionToCache(userID, tenantID, codes)
	return codes, PermissionSourceMembership, nil
}

// loadPermissionCodesByRoleID 按 role_id + tenant_id 读取 role_permissions 并联表
// permissions，返回排序去重后的 resource:action 权限码。
func loadPermissionCodesByRoleID(ctx context.Context, client *ent.Client, roleID, tenantID int) ([]string, error) {
	rolePerms, err := client.RolePermission.Query().
		Where(
			rolepermission.RoleIDEQ(roleID),
			rolepermission.TenantID(tenantID),
		).
		All(ctx)
	if err != nil {
		return nil, err
	}
	if len(rolePerms) == 0 {
		return []string{}, nil
	}

	permissionIDs := make([]int, 0, len(rolePerms))
	seen := make(map[int]bool, len(rolePerms))
	for _, rp := range rolePerms {
		if rp.PermissionID == 0 || seen[rp.PermissionID] {
			continue
		}
		seen[rp.PermissionID] = true
		permissionIDs = append(permissionIDs, rp.PermissionID)
	}
	if len(permissionIDs) == 0 {
		return []string{}, nil
	}

	perms, err := client.Permission.Query().
		Where(
			permission.IDIn(permissionIDs...),
			permission.TenantID(tenantID),
		).
		All(ctx)
	if err != nil {
		return nil, err
	}

	codeSet := make(map[string]bool, len(perms))
	for _, p := range perms {
		if p.Resource == "" || p.Action == "" {
			continue
		}
		codeSet[p.Resource+":"+p.Action] = true
	}
	codes := make([]string, 0, len(codeSet))
	for code := range codeSet {
		codes = append(codes, code)
	}
	sort.Strings(codes)
	return codes, nil
}

// resolveViaStaticFallbackOrDeny 处理 membership 缺失 / role_id 为空 / 角色行无法解析：
// 开关开启 → 静态表回退（Warn 日志）；默认关闭 → fail-closed（Warn 日志）。
func resolveViaStaticFallbackOrDeny(userEntity *ent.User, membership *ent.UserTenantMembership, userID, tenantID int, reason string) ([]string, PermissionSource, error) {
	if !AuthzStaticFallbackEnabled() {
		zap.S().Warnw("ResolvePermissions: no membership, fail-closed (static fallback disabled)",
			"user_id", userID, "tenant_id", tenantID, "reason", reason,
			"static_fallback_enabled", false)
		return nil, PermissionSourceNone, nil
	}

	roles := staticFallbackRoles(userEntity, membership)
	codes := staticPermissionCodesForRoles(roles)
	zap.S().Warnw("ResolvePermissions: static permission fallback used (migration window)",
		"user_id", userID, "tenant_id", tenantID, "reason", reason,
		"roles", roles, "permission_count", len(codes),
		"static_fallback_enabled", true)
	return codes, PermissionSourceStaticFallback, nil
}

// staticFallbackRoles 组装静态回退使用的角色集合：users.role + msp_role 映射。
// msp_role 优先取 membership.msp_role（目标租户内的服务方角色快照），
// membership 缺失时退回 users.msp_role（与旧 Login/SwitchTenant 静态计算一致）。
func staticFallbackRoles(userEntity *ent.User, membership *ent.UserTenantMembership) []string {
	roles := make([]string, 0, 2)
	seen := make(map[string]bool, 2)
	add := func(r string) {
		r = strings.TrimSpace(r)
		if r == "" || seen[r] {
			return
		}
		seen[r] = true
		roles = append(roles, r)
	}

	if userEntity != nil {
		add(string(userEntity.Role))
	}
	mspRole := ""
	if membership != nil && membership.MspRole != nil {
		mspRole = strings.TrimSpace(*membership.MspRole)
	}
	if mspRole == "" && userEntity != nil {
		mspRole = strings.TrimSpace(string(userEntity.MspRole))
	}
	if mspRole != "" {
		add(GetMSPRBACRole(mspRole))
	}
	return roles
}

// staticPermissionCodesForRoles 合并静态 RolePermissions 表并去重排序。
func staticPermissionCodesForRoles(roles []string) []string {
	codeSet := make(map[string]bool)
	for _, roleCode := range roles {
		for _, p := range RolePermissions[roleCode] {
			if p.Resource == "" || p.Action == "" {
				continue
			}
			codeSet[p.Resource+":"+p.Action] = true
		}
	}
	codes := make([]string, 0, len(codeSet))
	for code := range codeSet {
		codes = append(codes, code)
	}
	sort.Strings(codes)
	return codes
}
