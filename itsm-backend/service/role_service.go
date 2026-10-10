package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"sort"
	"strings"

	"itsm-backend/common"
	"itsm-backend/dto"
	"itsm-backend/ent"
	"itsm-backend/ent/permission"
	"itsm-backend/ent/role"
	"itsm-backend/ent/rolepermission"
	"itsm-backend/ent/user"
	"itsm-backend/middleware"

	"go.uber.org/zap"
)

// RoleService 角色服务
type RoleService struct {
	client *ent.Client
	logger *zap.SugaredLogger
}

// NewRoleService 创建角色服务
func NewRoleService(client *ent.Client, logger *zap.SugaredLogger) *RoleService {
	return &RoleService{
		client: client,
		logger: logger,
	}
}

// roleBaseQuery 返回租户内角色基础查询；按租户作用域隐藏"非本作用域"角色（IP-P2-7 收口：
// 客户/服务商租户隐藏平台角色，客户租户另隐藏 MSP 专属角色；平台根（含 code=default 兜底，
// D4=B）不隐藏；命中按"不存在"处理，不泄露存在性）。
func (s *RoleService) roleBaseQuery(ctx context.Context, tenantID int) (*ent.RoleQuery, error) {
	kind, code, err := tenantScopeOf(ctx, s.client, tenantID)
	if err != nil {
		return nil, err
	}
	q := s.client.Role.Query().Where(role.TenantID(tenantID))
	if hidden := hiddenRoleCodesForTenant(kind, code); len(hidden) > 0 {
		q = q.Where(role.CodeNotIn(hidden...))
	}
	return q, nil
}

// getRolePermissionsByIDs 批量查询多个角色的权限（按roleID分组，tenantID隔离）
func (s *RoleService) getRolePermissionsByIDs(ctx context.Context, roleIDs []int, tenantID int) (map[int][]dto.PermissionInfo, error) {
	result := make(map[int][]dto.PermissionInfo, len(roleIDs))
	for _, rid := range roleIDs {
		result[rid] = []dto.PermissionInfo{}
	}

	if len(roleIDs) == 0 {
		return result, nil
	}

	// 查询 role_permissions 联表（必须带 TenantID 过滤防止跨租户泄露）
	rps, err := s.client.RolePermission.Query().
		Where(
			rolepermission.RoleIDIn(roleIDs...),
			rolepermission.TenantID(tenantID),
		).
		All(ctx)
	if err != nil {
		return result, fmt.Errorf("查询角色权限关联失败: %w", err)
	}

	// 收集所有 permission IDs
	permIDs := make([]int, 0, len(rps))
	for _, rp := range rps {
		permIDs = append(permIDs, rp.PermissionID)
	}
	if len(permIDs) == 0 {
		return result, nil
	}

	// 批量查询权限详情（带 TenantID 过滤）
	perms, err := s.client.Permission.Query().
		Where(
			permission.IDIn(permIDs...),
			permission.TenantID(tenantID),
		).
		All(ctx)
	if err != nil {
		return result, fmt.Errorf("查询权限详情失败: %w", err)
	}

	permMap := make(map[int]*ent.Permission, len(perms))
	for _, p := range perms {
		permMap[p.ID] = p
	}

	// 按 roleID 分组
	for _, rp := range rps {
		if p, ok := permMap[rp.PermissionID]; ok {
			result[rp.RoleID] = append(result[rp.RoleID], dto.PermissionInfo{
				ID:   p.ID,
				Code: p.Code,
				Name: p.Name,
			})
		}
	}
	return result, nil
}

// buildRoleResponse 构建角色基础响应（不加载权限）
func (s *RoleService) buildRoleResponse(roleEntity *ent.Role) *dto.RoleResponse {
	return &dto.RoleResponse{
		ID:          roleEntity.ID,
		Name:        roleEntity.Name,
		Code:        roleEntity.Code,
		Description: roleEntity.Description,
		IsSystem:    roleEntity.IsSystem,
		Status:      dto.RoleStatusFromActive(roleEntity.IsActive),
		IsActive:    roleEntity.IsActive,
		DataScope:   string(roleEntity.DataScope),
		Permissions: []dto.PermissionInfo{},
		TenantID:    roleEntity.TenantID,
		CreatedAt:   roleEntity.CreatedAt,
		UpdatedAt:   roleEntity.UpdatedAt,
	}
}

// CreateRole 创建角色
func (s *RoleService) CreateRole(ctx context.Context, req *dto.CreateRoleRequest, tenantID int) (*dto.RoleResponse, error) {
	s.logger.Infow("Creating role", "name", req.Name, "tenant_id", tenantID)

	code := req.Code
	if code == "" {
		code = generateCodeFromName(req.Name)
	}
	if isReservedRoleCode(code) {
		return nil, fmt.Errorf("%w: %s", ErrReservedRoleCode, strings.TrimSpace(code))
	}

	roleEntity, err := s.client.Role.Create().
		SetName(req.Name).
		SetDescription(req.Description).
		SetCode(code).
		SetTenantID(tenantID).
		SetIsSystem(req.IsSystem).
		SetIsActive(req.Status != dto.RoleStatusInactive). // 默认启用，仅显式 inactive 时禁用
		Save(ctx)
	if err != nil {
		return nil, fmt.Errorf("创建角色失败: %w", err)
	}

	if len(req.Permissions) > 0 {
		permissionIDs, err := s.permissionIDsByCodes(ctx, req.Permissions, tenantID)
		if err != nil {
			return nil, err
		}
		if err := s.AssignPermissions(ctx, roleEntity.ID, permissionIDs, tenantID); err != nil {
			return nil, err
		}
	}

	return s.GetRole(ctx, roleEntity.ID, tenantID)
}

// GetRole 获取角色详情
func (s *RoleService) GetRole(ctx context.Context, id int, tenantID int) (*dto.RoleResponse, error) {
	query, err := s.roleBaseQuery(ctx, tenantID)
	if err != nil {
		return nil, err
	}
	roleEntity, err := query.
		Where(role.IDEQ(id)).
		Only(ctx)
	if err != nil {
		if ent.IsNotFound(err) {
			return nil, fmt.Errorf("角色不存在")
		}
		return nil, fmt.Errorf("查询角色失败: %w", err)
	}

	resp := s.buildRoleResponse(roleEntity)

	perms, err := s.getRolePermissionsByIDs(ctx, []int{id}, tenantID)
	if err != nil {
		s.logger.Warnw("Failed to load role permissions", "role_id", id, "error", err)
	} else {
		resp.Permissions = perms[id]
	}

	return resp, nil
}

// ListRoles 获取角色列表
func (s *RoleService) ListRoles(ctx context.Context, tenantID int, params *dto.GetRolesParams) ([]*dto.RoleResponse, int, error) {
	page, pageSize := common.ValidatePagination(params.Page, params.PageSize)

	query, err := s.roleBaseQuery(ctx, tenantID)
	if err != nil {
		return nil, 0, err
	}

	if strings.TrimSpace(params.Search) != "" {
		keyword := strings.TrimSpace(params.Search)
		query = query.Where(role.Or(
			role.NameContainsFold(keyword),
			role.CodeContainsFold(keyword),
			role.DescriptionContainsFold(keyword),
		))
	}

	// status 取值已由 handler 用 dto.RoleActiveFromStatus 校验，非法值不会到这里
	if active, ok := dto.RoleActiveFromStatus(params.Status); ok {
		query = query.Where(role.IsActive(active))
	}

	total, err := query.Clone().Count(ctx)
	if err != nil {
		return nil, 0, fmt.Errorf("统计角色数量失败: %w", err)
	}

	roles, err := query.
		Offset((page - 1) * pageSize).
		Limit(pageSize).
		All(ctx)
	if err != nil {
		return nil, 0, fmt.Errorf("查询角色列表失败: %w", err)
	}

	roleIDs := make([]int, len(roles))
	for i, r := range roles {
		roleIDs[i] = r.ID
	}

	permMap := make(map[int][]dto.PermissionInfo)
	if len(roleIDs) > 0 {
		permMap, err = s.getRolePermissionsByIDs(ctx, roleIDs, tenantID)
		if err != nil {
			s.logger.Warnw("Failed to batch load role permissions", "error", err)
		}
	}

	responses := make([]*dto.RoleResponse, len(roles))
	callerRank := serviceRoleRank(params.CallerRole)
	for i, r := range roles {
		resp := s.buildRoleResponse(r)
		resp.Permissions = permMap[r.ID]
		// Grantable：调用者能否把该角色授予用户（rank ≤ 调用者；与写面 CanGrantRoles 同口径，IP-P2-7 补丁 9）。
		resp.Grantable = serviceRoleRank(r.Code) <= callerRank
		responses[i] = resp
	}

	return responses, total, nil
}

// UpdateRole 更新角色
func (s *RoleService) UpdateRole(ctx context.Context, id int, req *dto.UpdateRoleRequest, tenantID int) (*dto.RoleResponse, error) {
	s.logger.Infow("Updating role", "id", id, "tenant_id", tenantID)

	query, err := s.roleBaseQuery(ctx, tenantID)
	if err != nil {
		return nil, err
	}
	roleEntity, err := query.
		Where(role.IDEQ(id)).
		Only(ctx)
	if err != nil {
		if ent.IsNotFound(err) {
			return nil, fmt.Errorf("角色不存在或无权访问")
		}
		return nil, fmt.Errorf("查询角色失败: %w", err)
	}

	// 保留角色码护栏（IP-P2-7 收口）：防止把自有角色重命名为 super_admin 等获取 RBAC 直通。
	if req.Code != nil && strings.TrimSpace(*req.Code) != "" && isReservedRoleCode(*req.Code) {
		return nil, fmt.Errorf("%w: %s", ErrReservedRoleCode, strings.TrimSpace(*req.Code))
	}

	update := s.client.Role.Update().
		Where(
			role.IDEQ(id),
			role.TenantID(tenantID),
		)

	if req.Name != nil {
		update = update.SetName(*req.Name)
	}
	if req.Description != nil {
		update = update.SetDescription(*req.Description)
	}
	if req.Code != nil && strings.TrimSpace(*req.Code) != "" {
		update = update.SetCode(strings.TrimSpace(*req.Code))
	}
	// 启用状态：线上契约 status(active/inactive) → 持久层 is_active(bool)
	// handler 已用 dto.RoleActiveFromStatus 校验取值，此处只需按契约映射
	if req.Status != nil {
		update = update.SetIsActive(*req.Status == dto.RoleStatusActive)
	}

	_, err = update.Save(ctx)
	if err != nil {
		return nil, fmt.Errorf("更新角色失败: %w", err)
	}

	if req.Permissions != nil {
		permissionIDs, err := s.permissionIDsByCodes(ctx, req.Permissions, tenantID)
		if err != nil {
			return nil, err
		}
		if err := s.AssignPermissions(ctx, id, permissionIDs, tenantID); err != nil {
			return nil, err
		}
	}

	middleware.InvalidateRolePermissionCache(roleEntity.Code, tenantID)

	return s.GetRole(ctx, id, tenantID)
}

// isReservedRoleCode 保留角色码（平台治理角色 + MSP 专属角色）：不可经角色配置面创建或重命名。
// 作用：防止把自有角色改名成 super_admin 获取 RBAC 直通（IP-P2-7 收口）。
func isReservedRoleCode(code string) bool {
	return middleware.IsPlatformRole(code) || middleware.IsMSPOnlyRole(code)
}

// ensureAssignablePermissionsForNonPlatformRoot 非平台根租户的「可分配权限」护栏（IP-P2-7 收口）：
//  1. 通配权限（resource/action 含 *）——挂到自有角色即可绕过全部权限判定；
//  2. 平台治理权限（platformGovernedPermissionResources 集合，如 system:write 掌管 LLM 实例/运维命令/RLS/定时器）——
//     权限码可被克隆目录带到租户，若允许再分配，租户即以其为能力穿过仅按码粗筛的平台路由。
//
// 覆盖创建角色（Permissions 码路径）/更新角色（码路径）/AssignPermissions（ID 路径）三条路径。
// 平台根（internal/standard 或根租户 code=default 兜底，D4=B）不受限。
func (s *RoleService) ensureAssignablePermissionsForNonPlatformRoot(ctx context.Context, tenantID int, perms []*ent.Permission) error {
	violationErr := error(nil)
	violation := ""
	for _, p := range perms {
		if isWildcardPermission(p.Resource, p.Action) {
			violationErr = ErrWildcardPermissionForbidden
			violation = fmt.Sprintf("%s:%s", p.Resource, p.Action)
			break
		}
		if isPlatformGovernedPermissionResource(p.Resource) {
			violationErr = ErrPlatformPermissionForbidden
			violation = fmt.Sprintf("%s:%s", p.Resource, p.Action)
			break
		}
	}
	if violationErr == nil {
		return nil
	}
	kind, code, err := tenantScopeOf(ctx, s.client, tenantID)
	if err != nil {
		return err
	}
	if !isPlatformRootTenant(kind, code) {
		if errors.Is(violationErr, ErrWildcardPermissionForbidden) {
			return fmt.Errorf("%w（%s）", violationErr, violation)
		}
		return fmt.Errorf("%w: %s", violationErr, violation)
	}
	return nil
}

// platformGovernedPermissionResources 平台治理权限资源段集合（非平台根租户不可分配，IP-P2-7 收口补丁 4；
// 补丁 6 由单常量改为显式集合）。
//
// 逐家族评估结论（2026-10-08，补丁 6）——判定标准"是否被平台面（L1 路由 / 全局资源）独占依赖"：
//   - system：纳入（LLM 实例管理/运维命令/RLS 指标/定时器均挂 L1，租户无合法用途）；
//   - system_config / mcp / connector：租户管理面显式依赖（角色基线与租户路由），不纳入——
//     其平台面（若有）由 L1 身份校验承载；
//   - menu / federation：码空间不存在该家族（菜单管理复用 system_config；federation 无引用）。
//
// 新增/调整家族必须先完成评估并更新《新增角色/权限/路由评审清单》§2.2，再改此处。
var platformGovernedPermissionResources = map[string]struct{}{
	"system": {},
	// 2026-10-10 治理域收口（设计 §8 D7）：tenant 家族=租户治理属性（plan/billing/status，
	// 路由均挂 L1）；mcp 家族由"租户面"改判平台资产（覆盖补丁 6 评估）。评估结论与
	// 变更纪律见 docs/multi-tenant/plan/role-permission-extension-checklist.md §2.2。
	"tenant": {},
	"mcp":    {},
}

// isWildcardPermission 通配权限（resource/action 含 *）——写面护栏与目录标注共用的单一源。
func isWildcardPermission(resource, action string) bool { return resource == "*" || action == "*" }

// isPlatformGovernedPermissionResource 平台治理家族成员判定。
func isPlatformGovernedPermissionResource(resource string) bool {
	_, governed := platformGovernedPermissionResources[strings.TrimSpace(resource)]
	return governed
}

// IsPlatformGovernedPermissionResource 平台治理家族判定的导出包装：供开通克隆链
// （pkg/seeder tenant_provisioner）等消费方复用单一源（设计 §8 D1=A 防再生）。
func IsPlatformGovernedPermissionResource(resource string) bool {
	return isPlatformGovernedPermissionResource(resource)
}

// isNonAssignablePermission 非平台根不可分配（通配 + 平台治理家族）——与写面护栏同口径，
// 供权限目录 assignable 标注使用（IP-P2-7 收口补丁 8 / R5）。
func isNonAssignablePermission(resource, action string) bool {
	return isWildcardPermission(resource, action) || isPlatformGovernedPermissionResource(resource)
}

func (s *RoleService) permissionIDsByCodes(ctx context.Context, permissionCodes []string, tenantID int) ([]int, error) {
	normalizedCodes := make([]string, 0, len(permissionCodes))
	seen := make(map[string]struct{}, len(permissionCodes))
	for _, code := range permissionCodes {
		trimmed := strings.TrimSpace(code)
		if trimmed == "" {
			continue
		}
		if _, ok := seen[trimmed]; ok {
			continue
		}
		seen[trimmed] = struct{}{}
		normalizedCodes = append(normalizedCodes, trimmed)
	}
	if len(normalizedCodes) == 0 {
		return []int{}, nil
	}

	perms, err := s.client.Permission.Query().
		Where(
			permission.TenantID(tenantID),
			permission.CodeIn(normalizedCodes...),
		).
		All(ctx)
	if err != nil {
		return nil, fmt.Errorf("查询权限失败: %w", err)
	}
	if len(perms) != len(normalizedCodes) {
		found := make(map[string]struct{}, len(perms))
		for _, permEntity := range perms {
			found[permEntity.Code] = struct{}{}
		}
		missing := make([]string, 0)
		for _, code := range normalizedCodes {
			if _, ok := found[code]; !ok {
				missing = append(missing, code)
			}
		}
		return nil, fmt.Errorf("权限不存在，请先初始化权限字典: %s", strings.Join(missing, ", "))
	}
	if err := s.ensureAssignablePermissionsForNonPlatformRoot(ctx, tenantID, perms); err != nil {
		return nil, err
	}

	ids := make([]int, 0, len(perms))
	for _, permEntity := range perms {
		ids = append(ids, permEntity.ID)
	}
	return ids, nil
}

// DeleteRole 删除角色
func (s *RoleService) DeleteRole(ctx context.Context, id int, tenantID int) error {
	s.logger.Infow("Deleting role", "id", id, "tenant_id", tenantID)

	query, err := s.roleBaseQuery(ctx, tenantID)
	if err != nil {
		return err
	}
	roleEntity, err := query.
		Where(role.IDEQ(id)).
		Only(ctx)
	if err != nil {
		return fmt.Errorf("角色不存在: %w", err)
	}

	if roleEntity.IsSystem {
		return fmt.Errorf("系统角色不能删除")
	}

	count, err := s.client.User.Query().
		Where(
			user.HasRolesWith(role.IDEQ(roleEntity.ID)),
			user.TenantID(tenantID),
		).
		Count(ctx)
	if err != nil {
		return fmt.Errorf("检查用户失败: %w", err)
	}
	if count > 0 {
		return fmt.Errorf("有%d个用户使用此角色，无法删除", count)
	}

	roleCode := roleEntity.Code

	// 删除角色权限关联
	_, err = s.client.RolePermission.Delete().
		Where(rolepermission.RoleID(id)).
		Exec(ctx)
	if err != nil {
		s.logger.Warnw("Failed to delete role permissions", "role_id", id, "error", err)
	}

	_, err = s.client.Role.Delete().
		Where(
			role.IDEQ(id),
			role.TenantID(tenantID),
		).
		Exec(ctx)
	if err != nil {
		return fmt.Errorf("删除角色失败: %w", err)
	}

	middleware.InvalidateRolePermissionCache(roleCode, tenantID)

	return nil
}

// AssignPermissions 给角色分配权限（直接操作 role_permissions 联表）
// C-4 修复：使用事务包裹 Delete+CreateBulk，循环内 err 必须 return 触发 Rollback，
// 防止"旧权限已清空、新权限只写入部分"的越权/失权静默故障。
// ErrPermissionNotInTenant 表示请求绑定的权限中存在不属于当前租户（或不存在）的项。
// 属于调用方输入错误（应映射为 400），而非服务端内部错误（500）。
var ErrPermissionNotInTenant = errors.New("部分权限不存在或不属于当前租户")

// ErrReservedRoleCode 保留角色码（平台治理角色 + MSP 专属角色）不可创建或重命名（IP-P2-7 收口）。
var ErrReservedRoleCode = errors.New("角色码为系统保留（平台/MSP 专属），不可创建或重命名")

// ErrWildcardPermissionForbidden 通配权限（resource/action 含 *）仅平台根租户可用（IP-P2-7 收口）。
var ErrWildcardPermissionForbidden = errors.New("通配权限仅平台根租户可用")

// ErrPlatformPermissionForbidden 平台治理权限码（resource=system）仅平台根租户可分配（IP-P2-7 收口补丁 4）。
var ErrPlatformPermissionForbidden = errors.New("平台治理权限码仅平台根租户可分配")

func (s *RoleService) AssignPermissions(ctx context.Context, roleID int, permissionIDs []int, tenantID int) error {
	s.logger.Infow("Assigning permissions to role", "role_id", roleID, "permission_count", len(permissionIDs))

	query, err := s.roleBaseQuery(ctx, tenantID)
	if err != nil {
		return err
	}
	roleEntity, err := query.
		Where(role.IDEQ(roleID)).
		Only(ctx)
	if err != nil {
		return fmt.Errorf("角色不存在: %w", err)
	}

	// P1-3 权限变更审计：事务前快照旧权限（code 列表），用于 after 提交后写 diff。
	beforeCodes, err := s.rolePermissionCodes(ctx, roleID, tenantID)
	if err != nil {
		// 快照失败不阻断授权，但记录（审计降级而非业务失败）
		s.logger.Warnw("权限变更前快照失败，审计diff将缺失before", "role_id", roleID, "error", err)
		beforeCodes = nil
	}

	if len(permissionIDs) > 0 {
		// R1 修复：权限是租户级实体，绑定角色时必须按本租户过滤。
		// 否则可传入他租户 permission ID 通过 len 校验，造成跨租户提权。
		perms, err := s.client.Permission.Query().
			Where(
				permission.IDIn(permissionIDs...),
				permission.TenantID(tenantID),
			).
			All(ctx)
		if err != nil {
			return fmt.Errorf("查询权限失败: %w", err)
		}
		if len(perms) != len(permissionIDs) {
			return ErrPermissionNotInTenant
		}
		if err := s.ensureAssignablePermissionsForNonPlatformRoot(ctx, tenantID, perms); err != nil {
			return err
		}
	}

	// 开启事务：DELETE + CREATE BULK 必须原子化
	tx, err := s.client.Tx(ctx)
	if err != nil {
		return fmt.Errorf("开启事务失败: %w", err)
	}
	defer tx.Rollback()

	// 清除现有权限关联
	_, err = tx.RolePermission.Delete().
		Where(
			rolepermission.RoleID(roleID),
			rolepermission.TenantID(tenantID),
		).
		Exec(ctx)
	if err != nil {
		return fmt.Errorf("清除权限关联失败: %w", err)
	}

	// 批量添加新权限关联（使用 CreateBulk 避免循环）
	if len(permissionIDs) > 0 {
		bulk := make([]*ent.RolePermissionCreate, 0, len(permissionIDs))
		for _, pid := range permissionIDs {
			bulk = append(bulk, tx.RolePermission.Create().
				SetRoleID(roleID).
				SetPermissionID(pid).
				SetTenantID(tenantID),
			)
		}
		if err := tx.RolePermission.CreateBulk(bulk...).Exec(ctx); err != nil {
			// CreateBulk 任何一条失败全部回滚，避免部分写入
			return fmt.Errorf("创建权限关联失败: %w", err)
		}
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("提交事务失败: %w", err)
	}

	middleware.InvalidateRolePermissionCache(roleEntity.Code, tenantID)

	// P1-3 权限变更审计：提交后快照新权限并写 diff（before/after code 列表）。
	s.writePermissionChangeAudit(ctx, roleEntity, tenantID, beforeCodes, permissionIDs)

	return nil
}

// rolePermissionCodes 返回角色当前权限的 code 列表（排序后，供审计 diff）。
func (s *RoleService) rolePermissionCodes(ctx context.Context, roleID, tenantID int) ([]string, error) {
	// RolePermission 无 ent 边（schema Edges 为 nil），按既有模式两步查询：
	// 先取 role_permissions 的 permission_id，再按租户过滤查 permission.code。
	rolePerms, err := s.client.RolePermission.Query().
		Where(rolepermission.RoleIDEQ(roleID), rolepermission.TenantID(tenantID)).
		All(ctx)
	if err != nil {
		return nil, err
	}
	if len(rolePerms) == 0 {
		return []string{}, nil
	}
	permIDs := make([]int, 0, len(rolePerms))
	for _, rp := range rolePerms {
		permIDs = append(permIDs, rp.PermissionID)
	}
	permEntities, err := s.client.Permission.Query().
		Where(permission.IDIn(permIDs...), permission.TenantID(tenantID)).
		All(ctx)
	if err != nil {
		return nil, err
	}
	codes := make([]string, 0, len(permEntities))
	for _, p := range permEntities {
		codes = append(codes, p.Code)
	}
	sort.Strings(codes)
	return codes, nil
}

// writePermissionChangeAudit 写权限变更 diff 审计（P1-3）。
// after 由提交后的重新快照获得（而非信任入参），确保审计与库内终态一致。
func (s *RoleService) writePermissionChangeAudit(ctx context.Context, roleEntity *ent.Role, tenantID int, before []string, _ []int) {
	after, err := s.rolePermissionCodes(ctx, roleEntity.ID, tenantID)
	if err != nil {
		s.logger.Warnw("权限变更后快照失败，审计diff缺失after", "role_id", roleEntity.ID, "error", err)
		return
	}
	sort.Strings(before)

	diff := map[string]any{
		"role_id":   roleEntity.ID,
		"role_code": roleEntity.Code,
		"before":    before,
		"after":     after,
	}
	payload, err := json.Marshal(diff)
	if err != nil {
		s.logger.Warnw("权限变更diff序列化失败", "role_id", roleEntity.ID, "error", err)
		return
	}

	if _, err := s.client.AuditLog.Create().
		SetTenantID(tenantID).
		SetResource("role_permissions").
		SetAction("assign").
		SetPath(fmt.Sprintf("/api/v1/roles/%d/permissions", roleEntity.ID)).
		SetMethod("POST").
		SetStatusCode(200).
		SetRequestBody(string(payload)).
		Save(ctx); err != nil {
		// 审计写失败不回滚业务（权限已生效），但必须告警。
		s.logger.Errorw("权限变更审计写入失败", "role_id", roleEntity.ID, "error", err)
		return
	}
	s.logger.Infow("权限变更审计已记录", "role_id", roleEntity.ID, "before_count", len(before), "after_count", len(after))
}

// PermissionService 权限服务
type PermissionService struct {
	client *ent.Client
	logger *zap.SugaredLogger
}

// NewPermissionService 创建权限服务
func NewPermissionService(client *ent.Client, logger *zap.SugaredLogger) *PermissionService {
	return &PermissionService{
		client: client,
		logger: logger,
	}
}

// CreatePermission 创建权限
func (s *PermissionService) CreatePermission(ctx context.Context, req *dto.CreatePermissionRequest, tenantID int) (*dto.PermissionResponse, error) {
	s.logger.Infow("Creating permission", "code", req.Code, "tenant_id", tenantID)

	// 通配权限仅平台根作用域可创建（IP-P2-7 收口：防止非平台租户自造 `*:*` 绕过权限判定；
	// D4=B：平台根含根租户 code=default 兜底）。
	if req.Resource == "*" || req.Action == "*" {
		kind, code, err := tenantScopeOf(ctx, s.client, tenantID)
		if err != nil {
			return nil, err
		}
		if !isPlatformRootTenant(kind, code) {
			return nil, ErrWildcardPermissionForbidden
		}
	}

	permEntity, err := s.client.Permission.Create().
		SetCode(req.Code).
		SetName(req.Name).
		SetDescription(req.Description).
		SetResource(req.Resource).
		SetAction(req.Action).
		SetTenantID(tenantID).
		Save(ctx)
	if err != nil {
		return nil, fmt.Errorf("创建权限失败: %w", err)
	}

	return s.toPermissionResponse(permEntity), nil
}

// ListPermissions 获取权限列表
func (s *PermissionService) ListPermissions(ctx context.Context, tenantID int, resource string) ([]*dto.PermissionResponse, error) {
	query := s.client.Permission.Query().
		Where(permission.TenantID(tenantID))

	if resource != "" {
		query = query.Where(permission.Resource(resource))
	}

	perms, err := query.All(ctx)
	if err != nil {
		return nil, fmt.Errorf("查询权限列表失败: %w", err)
	}

	// assignable 标注（IP-P2-7 收口补丁 8 / R5）：与写面护栏 ensureAssignablePermissionsForNonPlatformRoot
	// 同口径——非平台根租户的通配/平台治理家族码标注为不可分配，供前端隐藏选择项（减少无谓 400）。
	kind, code, err := tenantScopeOf(ctx, s.client, tenantID)
	if err != nil {
		return nil, fmt.Errorf("查询租户作用域失败: %w", err)
	}
	platformRoot := isPlatformRootTenant(kind, code)

	responses := make([]*dto.PermissionResponse, 0, len(perms))
	for _, p := range perms {
		resp := s.toPermissionResponse(p)
		resp.Assignable = platformRoot || !isNonAssignablePermission(p.Resource, p.Action)
		// 2026-10-10 治理域收口（设计 §8 D6）：非平台根目录不下发平台治理家族码——
		// 前端"租户域授权"只见租户面码；存量已收回 + 开通克隆链跳过（防再生），本过滤为兜底。
		if !platformRoot && isPlatformGovernedPermissionResource(p.Resource) {
			continue
		}
		responses = append(responses, resp)
	}

	return responses, nil
}

// InitDefaultPermissions 初始化默认权限
func (s *PermissionService) InitDefaultPermissions(ctx context.Context, tenantID int) error {
	defaultPermissions := []dto.CreatePermissionRequest{}
	resourceActions := map[string][]string{
		"dashboard":       {"read", "view"},
		"ticket":          {"create", "read", "write", "update", "delete", "assign", "export"},
		"ticket_category": {"create", "read", "write", "update", "delete"},
		"ticket_type":     {"create", "read", "write", "update", "delete", "manage", "archive"},
		"incident":        {"create", "read", "write", "update", "delete", "assign", "export"},
		"problem":         {"create", "read", "write", "update", "delete", "export"},
		"change":          {"create", "read", "write", "update", "delete", "approve", "export"},
		"service_catalog": {"create", "read", "write", "update", "delete"},
		"service_request": {"create", "read", "write", "update", "delete", "approve"},
		"knowledge":       {"create", "read", "write", "update", "delete"},
		"cmdb":            {"create", "read", "write", "update", "delete", "export"},
		"asset":           {"create", "read", "write", "update", "delete", "assign"},
		"license":         {"create", "read", "write", "update", "delete", "assign"},
		"release":         {"create", "read", "write", "update", "delete"},
		"report":          {"read", "view", "export"},
		"user":            {"read", "write", "delete", "manage"},
		"role":            {"read", "write", "delete", "manage"},
		"groups":          {"read", "write", "delete"},
		"org":             {"read", "write"},
		"bpmn":            {"read", "write", "delete"},
		"workflow":        {"read", "write", "delete"},
		"system_config":   {"read", "write", "update"},
		"admin":           {"read", "write", "all"},
		"ai":              {"read", "write"},
	}

	for resource, actions := range resourceActions {
		for _, action := range actions {
			defaultPermissions = append(defaultPermissions, dto.CreatePermissionRequest{
				Code:        fmt.Sprintf("%s:%s", resource, action),
				Name:        fmt.Sprintf("%s %s", resource, action),
				Description: fmt.Sprintf("允许对 %s 执行 %s 操作", resource, action),
				Resource:    resource,
				Action:      action,
			})
		}
	}
	defaultPermissions = append(defaultPermissions, dto.CreatePermissionRequest{
		Code:        "admin:all",
		Name:        "完全管理",
		Description: "平台级全部管理权限",
		Resource:    "admin",
		Action:      "all",
	})

	for _, req := range defaultPermissions {
		exists, _ := s.client.Permission.Query().
			Where(
				permission.Code(req.Code),
				permission.TenantID(tenantID),
			).
			Exist(ctx)
		if exists {
			continue
		}

		_, err := s.CreatePermission(ctx, &req, tenantID)
		if err != nil {
			s.logger.Warnw("Failed to create default permission", "code", req.Code, "error", err)
		}
	}

	return nil
}

func (s *PermissionService) toPermissionResponse(permEntity *ent.Permission) *dto.PermissionResponse {
	return &dto.PermissionResponse{
		ID:          permEntity.ID,
		Code:        permEntity.Code,
		Name:        permEntity.Name,
		Description: permEntity.Description,
		Resource:    permEntity.Resource,
		Action:      permEntity.Action,
		Assignable:  true, // 默认可分配；ListPermissions 按租户作用域覆写（补丁 8）
		TenantID:    permEntity.TenantID,
		CreatedAt:   permEntity.CreatedAt,
		UpdatedAt:   permEntity.UpdatedAt,
	}
}

// generateCodeFromName 从名称生成代码
func generateCodeFromName(name string) string {
	code := strings.ToLower(name)
	code = regexp.MustCompile(`[^a-z0-9\x{4e00}-\x{9fa5}]`).ReplaceAllString(code, "_")
	code = regexp.MustCompile(`_+`).ReplaceAllString(code, "_")
	code = strings.Trim(code, "_")
	if len(code) > 20 {
		code = code[:20]
	}
	return code
}
