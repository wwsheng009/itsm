package service

import (
	"context"
	"fmt"
	"strings"
	"time"

	"itsm-backend/dto"
	"itsm-backend/ent"
	entrole "itsm-backend/ent/role"
	"itsm-backend/ent/user"
	"itsm-backend/ent/usertenantmembership"
	"itsm-backend/handlers/pat"
	"itsm-backend/middleware"
	"itsm-backend/pkg/tenantmode"

	"go.uber.org/zap"
	"golang.org/x/crypto/bcrypt"
)

type UserService struct {
	client *ent.Client
	logger *zap.SugaredLogger
	// config 用于读取租户生效的密码策略（system_configs 驱动）。
	// 未注入时回退 DefaultPasswordPolicy，保证既有调用方行为不变。
	config *SystemConfigService
}

func NewUserService(client *ent.Client, logger *zap.SugaredLogger) *UserService {
	return &UserService{
		client: client,
		logger: logger,
	}
}

// SetSystemConfigService 注入系统配置服务，使密码策略可由 /admin/system-config 调整。
func (s *UserService) SetSystemConfigService(config *SystemConfigService) {
	s.config = config
}

// PasswordPolicy 返回租户当前生效的密码策略；未注入配置服务时返回默认策略。
func (s *UserService) PasswordPolicy(ctx context.Context, tenantID int) PasswordPolicy {
	if s == nil || s.config == nil {
		return DefaultPasswordPolicy()
	}
	return s.config.GetPasswordPolicy(ctx, tenantID)
}

// CreateUser 创建用户
func (s *UserService) CreateUser(ctx context.Context, req *dto.CreateUserRequest, tenantID int) (*ent.User, error) {
	s.logger.Infof("创建用户: %s", req.Username)

	// 检查用户名是否已存在
	exists, err := s.client.User.Query().
		Where(user.UsernameEQ(req.Username)).
		Exist(ctx)
	if err != nil {
		return nil, fmt.Errorf("检查用户名失败: %w", err)
	}
	if exists {
		return nil, fmt.Errorf("用户名已存在: %s", req.Username)
	}

	// 检查邮箱是否已存在
	exists, err = s.client.User.Query().
		Where(user.EmailEQ(req.Email)).
		Exist(ctx)
	if err != nil {
		return nil, fmt.Errorf("检查邮箱失败: %w", err)
	}
	if exists {
		return nil, fmt.Errorf("邮箱已存在: %s", req.Email)
	}

	// 加密密码（如果不提供则生成随机密码）
	password := req.Password
	if password == "" {
		password = fmt.Sprintf("P@ssw0rd%08d", time.Now().UnixNano()%100000000)
	}
	if err := s.PasswordPolicy(ctx, tenantID).Validate(password); err != nil {
		return nil, err
	}
	hashedPassword, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return nil, fmt.Errorf("密码加密失败: %w", err)
	}

	// 创建用户
	uc := s.client.User.Create().
		SetUsername(req.Username).
		SetEmail(req.Email).
		SetName(req.Name).
		SetDepartment(req.Department).
		SetPhone(req.Phone).
		SetPasswordHash(string(hashedPassword)).
		SetActive(true).
		SetTenantID(tenantID)
	// 如果请求中提供了角色，则设置角色；否则使用Schema默认值（end_user）
	primaryRole := ""
	if strings.TrimSpace(req.Role) != "" {
		primaryRole = strings.ToLower(strings.TrimSpace(req.Role))
		// 兼容前端传的"user"角色，自动转换为"end_user"
		if primaryRole == "user" {
			primaryRole = "end_user"
		}
		// IP-P2-7 收口：平台角色（super_admin/sysadmin）仅平台根作用域可设置（D4=B：含根租户
		// code=default 兜底），防止客户/服务商租户经用户接口直填主角色获取 RBAC 直通。
		if middleware.IsPlatformRole(primaryRole) {
			kind, code, err := tenantScopeOf(ctx, s.client, tenantID)
			if err != nil {
				return nil, err
			}
			if !isPlatformRootTenant(kind, code) {
				return nil, fmt.Errorf("非平台租户不得设置平台角色: %s", primaryRole)
			}
		}
		uc = uc.SetRole(user.Role(primaryRole))

	}
	// MSP 角色仅服务商租户可写（IP-P2-7 收口：客户租户不得出现 MSP 概念，
	// 服务商员工身份应经建号通道/MSP 治理面维护）。
	if strings.TrimSpace(req.MSPRole) != "" {
		kind, err := tenantTypeOf(ctx, s.client, tenantID)
		if err != nil {
			return nil, err
		}
		if !tenantmode.IsMSPProviderTenantType(kind) {
			return nil, fmt.Errorf("仅服务商租户可设置 MSP 角色")
		}
		uc = uc.SetMspRole(user.MspRole(strings.ToLower(strings.TrimSpace(req.MSPRole))))
	}
	userEntity, err := uc.Save(ctx)
	if err != nil {
		return nil, fmt.Errorf("创建用户失败: %w", err)
	}

	// RBAC 多角色：显式 roleIds 优先；否则把主角色同步到 user_roles 边（存量回填同语义），
	// 使按角色解析审批人/M2M 权限链路能命中新用户。
	roleIDs := req.RoleIDs
	if len(roleIDs) == 0 && primaryRole != "" {
		if id, err := s.resolveRoleID(ctx, tenantID, primaryRole); err == nil {
			roleIDs = []int{id}
		} else {
			// 主角色无对应 roles 表实体（如 legacy "security"），仅告警不阻断建用户。
			s.logger.Warnw("主角色无对应 roles 表实体，user_roles 边未写入",
				"username", req.Username, "role", primaryRole, "error", err)
		}
	}
	if len(roleIDs) > 0 {
		if err := s.SyncUserRoles(ctx, userEntity.ID, tenantID, roleIDs); err != nil {
			return nil, fmt.Errorf("写入用户角色边失败: %w", err)
		}
	}

	s.logger.Infof("用户创建成功: ID=%d, Username=%s", userEntity.ID, userEntity.Username)
	return userEntity, nil
}

// resolveRoleID 按租户与 code 解析 roles 表实体 ID。
func (s *UserService) resolveRoleID(ctx context.Context, tenantID int, code string) (int, error) {
	r, err := s.client.Role.Query().
		Where(entrole.CodeEQ(code), entrole.TenantIDEQ(tenantID)).
		Only(ctx)
	if err != nil {
		return 0, err
	}
	return r.ID, nil
}

// SyncUserRoles 整体替换用户的 user_roles M2M 边（仅接受本租户内的角色 ID，防跨租户提权）。
func (s *UserService) SyncUserRoles(ctx context.Context, userID, tenantID int, roleIDs []int) error {
	if len(roleIDs) == 0 {
		_, err := s.client.User.UpdateOneID(userID).ClearRoles().Save(ctx)
		return err
	}
	// 校验全部角色属于同一租户，避免恶意 roleIds 跨租户挂角色。
	n, err := s.client.Role.Query().
		Where(entrole.IDIn(roleIDs...), entrole.TenantIDEQ(tenantID)).
		Count(ctx)
	if err != nil {
		return fmt.Errorf("校验角色失败: %w", err)
	}
	if n != len(roleIDs) {
		return fmt.Errorf("存在不属于当前租户的角色: 期望 %d 个，匹配 %d 个", len(roleIDs), n)
	}
	_, err = s.client.User.UpdateOneID(userID).ClearRoles().AddRoleIDs(roleIDs...).Save(ctx)
	return err
}

// CanGrantRoles 校验调用者是否有权授予指定角色集合（防 roleIds 越权提权）。
// 空 roleIDs 视为无操作，直接返回 nil。
func (s *UserService) CanGrantRoles(ctx context.Context, tenantID int, roleIDs []int, callerRole string) error {
	if len(roleIDs) == 0 {
		return nil
	}
	roles, err := s.client.Role.Query().
		Where(entrole.IDIn(roleIDs...), entrole.TenantIDEQ(tenantID)).
		All(ctx)
	if err != nil {
		return fmt.Errorf("查询角色失败: %w", err)
	}
	if len(roles) != len(roleIDs) {
		return fmt.Errorf("存在不属于当前租户的角色: 期望 %d 个，匹配 %d 个", len(roleIDs), len(roles))
	}
	callerRank := serviceRoleRank(callerRole)
	var tenantKind, tenantCode string
	var tenantScopeLoaded bool
	loadTenantScope := func() (string, string, error) {
		if !tenantScopeLoaded {
			kind, code, err := tenantScopeOf(ctx, s.client, tenantID)
			if err != nil {
				return "", "", err
			}
			tenantKind, tenantCode = kind, code
			tenantScopeLoaded = true
		}
		return tenantKind, tenantCode, nil
	}
	for _, r := range roles {
		if serviceRoleRank(r.Code) > callerRank {
			return fmt.Errorf("无权限分配高于自身角色的用户角色: %s", r.Code)
		}
		// 客户/服务商租户的角色边界（IP-P2-7 收口）：平台角色仅平台根可授予；MSP 专属角色仅服务商侧。
		// 租户作用域懒加载：无命中角色码场景不查库。
		code := strings.ToLower(strings.TrimSpace(r.Code))
		if !middleware.IsMSPOnlyRole(code) && !middleware.IsPlatformRole(code) {
			continue
		}
		kind, tenantCode, err := loadTenantScope()
		if err != nil {
			return err
		}
		if !isHiddenRoleForTenant(kind, tenantCode, code) {
			continue
		}
		if middleware.IsPlatformRole(code) {
			return fmt.Errorf("非平台租户不得授予平台角色: %s", r.Code)
		}
		return fmt.Errorf("客户租户不得授予 MSP 专属角色: %s", r.Code)
	}
	return nil
}

// serviceRoleRank 返回角色权限层级（单一词表：middleware.RoleRank，覆盖 msp_*；IP-P0-9/07:G3）。
func serviceRoleRank(role string) int {
	return middleware.RoleRank(role)
}

// ListUsers 获取用户列表
func (s *UserService) ListUsers(ctx context.Context, req *dto.ListUsersRequest, tenantID int) (*dto.PagedUsersResponse, error) {
	s.logger.Infof("获取用户列表: page=%d, pageSize=%d", req.Page, req.PageSize)

	query := s.client.User.Query().
		Where(user.TenantIDEQ(tenantID))

	// 按状态过滤
	if req.Status != "" {
		active := req.Status == "active"
		query = query.Where(user.ActiveEQ(active))
	}

	// 按部门过滤
	if req.Department != "" {
		query = query.Where(user.DepartmentContainsFold(req.Department))
	}

	// 搜索过滤
	if req.Search != "" {
		search := strings.TrimSpace(req.Search)
		query = query.Where(
			user.Or(
				user.UsernameContainsFold(search),
				user.NameContainsFold(search),
				user.EmailContainsFold(search),
			),
		)
	}

	// 计算总数
	total, err := query.Count(ctx)
	if err != nil {
		return nil, fmt.Errorf("统计用户总数失败: %w", err)
	}

	// 分页查询（预加载 roles 边供前端编辑表单回填）
	users, err := query.
		WithRoles().
		Limit(req.PageSize).
		Offset((req.Page - 1) * req.PageSize).
		Order(ent.Desc(user.FieldCreatedAt)).
		All(ctx)
	if err != nil {
		return nil, fmt.Errorf("查询用户列表失败: %w", err)
	}

	// 转换为响应格式
	userResponses := make([]*dto.UserDetailResponse, 0, len(users))
	for _, u := range users {
		roleIDs := make([]int, 0, len(u.Edges.Roles))
		roleNames := make([]string, 0, len(u.Edges.Roles))
		for _, r := range u.Edges.Roles {
			roleIDs = append(roleIDs, r.ID)
			roleNames = append(roleNames, r.Name)
		}
		userResponses = append(userResponses, &dto.UserDetailResponse{
			ID:         u.ID,
			Username:   u.Username,
			Email:      u.Email,
			Name:       u.Name,
			Department: u.Department,
			Phone:      u.Phone,
			Active:     u.Active,
			TenantID:   u.TenantID,
			Role:       string(u.Role),
			RoleIDs:    roleIDs,
			RoleNames:  roleNames,
			CreatedAt:  u.CreatedAt,
			UpdatedAt:  u.UpdatedAt,
		})
	}

	// Q2=B / MSW-2：客户侧成员列表只读展示服务方成员（独立区块；失败降级不阻断主列表）。
	providerMembers, pmErr := s.listProviderMembers(ctx, req, tenantID)
	if pmErr != nil {
		s.logger.Warnw("查询服务方成员失败（读通道降级：主列表不受影响）", "tenant_id", tenantID, "error", pmErr)
		providerMembers = nil
	} else if len(providerMembers) > 0 {
		seen := make(map[int]bool, len(userResponses))
		for _, u := range userResponses {
			seen[u.ID] = true
		}
		filtered := providerMembers[:0]
		for _, m := range providerMembers {
			if !seen[m.ID] {
				filtered = append(filtered, m)
			}
		}
		providerMembers = filtered
	}

	response := &dto.PagedUsersResponse{
		Users:           userResponses,
		ProviderMembers: providerMembers,
		Pagination: dto.PaginationResponse{
			Page:       req.Page,
			PageSize:   req.PageSize,
			Total:      total,
			TotalPages: (total + req.PageSize - 1) / req.PageSize,
		},
	}

	s.logger.Infof("用户列表查询成功: total=%d, returned=%d", total, len(users))
	return response, nil
}

// listProviderMembers 客户租户内服务方成员的只读展示数据（MSW-Q2=B：标注"服务方"，不可管理）。
//
// 口径：
//   - 仅客户类型租户返回（provider/platform 租户无此区块）；
//   - 仅存活（deleted_at IS NULL）、status=active、account_kind=provider 的映射；
//   - 复用列表的 status/search/department 过滤语义（作用于账号维度）；
//   - 独立字段返回，不计入分页 total（避免与 users 表的跨表分页口径漂移）。
func (s *UserService) listProviderMembers(ctx context.Context, req *dto.ListUsersRequest, tenantID int) ([]*dto.UserDetailResponse, error) {
	t, err := s.client.Tenant.Get(ctx, tenantID)
	if err != nil {
		return nil, fmt.Errorf("读取租户失败: %w", err)
	}
	if !tenantmode.IsCustomerTenantType(string(t.Type)) {
		return nil, nil
	}

	q := s.client.UserTenantMembership.Query().
		Where(
			usertenantmembership.TenantIDEQ(tenantID),
			usertenantmembership.AccountKindEQ(usertenantmembership.AccountKindProvider),
			usertenantmembership.StatusEQ(usertenantmembership.StatusActive),
			usertenantmembership.DeletedAtIsNil(),
			// IP-P2-9：到期服务方成员不再展示（惰性判定）。
			middleware.MembershipNotExpired(),
		)
	if req.Status != "" {
		q = q.Where(usertenantmembership.HasUserWith(user.ActiveEQ(req.Status == "active")))
	}
	if req.Search != "" {
		search := strings.TrimSpace(req.Search)
		q = q.Where(usertenantmembership.HasUserWith(user.Or(
			user.UsernameContainsFold(search),
			user.NameContainsFold(search),
			user.EmailContainsFold(search),
		)))
	}
	if req.Department != "" {
		q = q.Where(usertenantmembership.HasUserWith(user.DepartmentContainsFold(req.Department)))
	}

	rows, err := q.WithUser().Order(ent.Desc(usertenantmembership.FieldCreatedAt)).All(ctx)
	if err != nil {
		return nil, fmt.Errorf("查询服务方成员失败: %w", err)
	}
	out := make([]*dto.UserDetailResponse, 0, len(rows))
	for _, m := range rows {
		u := m.Edges.User
		if u == nil {
			continue
		}
		mspRole := m.MspRole
		if mspRole == nil && u.MspRole != "" {
			role := string(u.MspRole)
			mspRole = &role
		}
		roleLabel := "服务方成员"
		if mspRole != nil && *mspRole != "" {
			roleLabel = *mspRole
		}
		out = append(out, &dto.UserDetailResponse{
			ID:          u.ID,
			Username:    u.Username,
			Email:       u.Email,
			Name:        u.Name,
			Department:  u.Department,
			Phone:       u.Phone,
			Active:      u.Active,
			TenantID:    tenantID,
			Role:        roleLabel,
			MSPRole:     mspRole,
			CreatedAt:   m.CreatedAt,
			UpdatedAt:   m.UpdatedAt,
			AccountKind: string(usertenantmembership.AccountKindProvider),
			ReadOnly:    true,
		})
	}
	return out, nil
}

// GetUserByID 根据ID获取用户
func (s *UserService) GetUserByID(ctx context.Context, id int, tenantID int) (*ent.User, error) {
	s.logger.Infof("获取用户详情: ID=%d", id)

	userEntity, err := s.client.User.Query().
		Where(
			user.IDEQ(id),
			user.TenantIDEQ(tenantID),
		).
		Only(ctx)
	if err != nil {
		if ent.IsNotFound(err) {
			return nil, fmt.Errorf("用户不存在: ID=%d", id)
		}
		return nil, fmt.Errorf("获取用户失败: %w", err)
	}

	return userEntity, nil
}

// UpdateUser 更新用户信息
func (s *UserService) UpdateUser(ctx context.Context, id int, req *dto.UpdateUserRequest, tenantID int) (*ent.User, error) {
	s.logger.Infof("更新用户: ID=%d", id)

	// 验证用户属于当前租户，防止跨租户访问
	existingUser, err := s.client.User.Query().
		Where(user.IDEQ(id), user.TenantIDEQ(tenantID)).
		Only(ctx)
	if err != nil {
		if ent.IsNotFound(err) {
			return nil, fmt.Errorf("用户不存在: ID=%d", id)
		}
		return nil, fmt.Errorf("获取用户失败: %w", err)
	}

	update := s.client.User.UpdateOneID(id).Where(user.TenantIDEQ(tenantID))

	// 检查用户名是否已被其他用户使用
	if req.Username != "" && req.Username != existingUser.Username {
		exists, err := s.client.User.Query().
			Where(
				user.And(
					user.UsernameEQ(req.Username),
					user.IDNEQ(id),
				),
			).
			Exist(ctx)
		if err != nil {
			return nil, fmt.Errorf("检查用户名失败: %w", err)
		}
		if exists {
			return nil, fmt.Errorf("用户名已存在: %s", req.Username)
		}
		update = update.SetUsername(req.Username)
	}

	// 检查邮箱是否已被其他用户使用
	if req.Email != "" && req.Email != existingUser.Email {
		exists, err := s.client.User.Query().
			Where(
				user.And(
					user.EmailEQ(req.Email),
					user.IDNEQ(id),
				),
			).
			Exist(ctx)
		if err != nil {
			return nil, fmt.Errorf("检查邮箱失败: %w", err)
		}
		if exists {
			return nil, fmt.Errorf("邮箱已存在: %s", req.Email)
		}
		update = update.SetEmail(req.Email)
	}

	// 更新其他字段
	if req.Name != "" {
		update = update.SetName(req.Name)
	}
	if req.Department != "" {
		update = update.SetDepartment(req.Department)
	}
	if req.Phone != "" {
		update = update.SetPhone(req.Phone)
	}
	// 角色更新（仅在提供时设置），管理员权限由RBAC控制
	if strings.TrimSpace(req.Role) != "" {
		role := strings.ToLower(strings.TrimSpace(req.Role))
		// 兼容前端传的"user"角色，自动转换为"end_user"
		if role == "user" {
			role = "end_user"
		}
		// IP-P2-7 收口：平台角色仅平台根作用域可设置（与 CreateUser 同口径；D4=B 含 code=default 兜底）。
		if middleware.IsPlatformRole(role) {
			kind, code, err := tenantScopeOf(ctx, s.client, tenantID)
			if err != nil {
				return nil, err
			}
			if !isPlatformRootTenant(kind, code) {
				return nil, fmt.Errorf("非平台租户不得设置平台角色: %s", role)
			}
		}
		if role != string(existingUser.Role) {
			update = update.SetRole(user.Role(role))
			// P1-2 修复：角色变更后立即吊销该用户全部存量 access token，
			// 防止旧角色权限在 token 有效期内（15 分钟）继续生效。
			if err := middleware.InvalidateUserAccessTokens(ctx, id, time.Now()); err != nil {
				// 吊销失败不阻断角色变更本身，但必须记录：降权延迟窗口存在安全影响。
				s.logger.Errorw("用户角色变更后吊销存量token失败（降权延迟风险）",
					"user_id", id, "old_role", existingUser.Role, "new_role", role, "error", err)
			} else {
				s.logger.Infow("用户角色变更，已吊销存量access token", "user_id", id, "new_role", role)
			}
		}
	}

	userEntity, err := update.Save(ctx)
	if err != nil {
		return nil, fmt.Errorf("更新用户失败: %w", err)
	}

	// RBAC 多角色替换：仅在显式提供 roleIds 时整体替换 user_roles 边
	// （nil=不动；空数组=清空），避免部分更新语义破坏多角色并集。
	if req.RoleIDs != nil {
		if err := s.SyncUserRoles(ctx, id, tenantID, req.RoleIDs); err != nil {
			return nil, fmt.Errorf("替换用户角色边失败: %w", err)
		}
		// 角色集合变更影响权限，与主角色变更同语义：吊销存量 token。
		if err := middleware.InvalidateUserAccessTokens(ctx, id, time.Now()); err != nil {
			s.logger.Errorw("用户多角色变更后吊销存量token失败（降权延迟风险）",
				"user_id", id, "role_ids", req.RoleIDs, "error", err)
		}
	}

	s.logger.Infof("用户更新成功: ID=%d", id)
	return userEntity, nil
}

// DeleteUser 删除用户
func (s *UserService) DeleteUser(ctx context.Context, id int, tenantID int) error {
	s.logger.Infof("删除用户: ID=%d", id)

	// 检查用户是否存在
	_, err := s.GetUserByID(ctx, id, tenantID)
	if err != nil {
		return err
	}

	// 软删除 - 设置为非激活状态
	err = s.client.User.UpdateOneID(id).
		Where(user.TenantIDEQ(tenantID)).
		SetActive(false).
		Exec(ctx)
	if err != nil {
		return fmt.Errorf("删除用户失败: %w", err)
	}

	// IP-P2-8：软删=停用，同样立即吊销存量会话（与 ChangeUserStatus 口径一致）。
	now := time.Now()
	if revokeErr := middleware.InvalidateUserAccessTokens(ctx, id, now); revokeErr != nil {
		s.logger.Errorw("删除用户后吊销存量access token失败", "user_id", id, "error", revokeErr)
	}
	if revokeErr := middleware.InvalidateUserRefreshTokens(ctx, id, now); revokeErr != nil {
		s.logger.Errorw("删除用户后吊销存量refresh token失败", "user_id", id, "error", revokeErr)
	}
	// PAT（D-PAT-6/R3）：删除用户 → 全部个人访问令牌吊销（security_event）。
	pat.RevokeAllForUserByClient(ctx, s.client, s.logger, id)

	s.logger.Infof("用户删除成功: ID=%d", id)
	return nil
}

// ChangeUserStatus 更改用户状态
// currentUserID 是当前操作的用户ID，用于防止用户停用自己
func (s *UserService) ChangeUserStatus(ctx context.Context, id int, active bool, currentUserID int, tenantID int) error {
	s.logger.Infof("更改用户状态: ID=%d, active=%t, currentUserID=%d", id, active, currentUserID)

	// 检查用户是否存在
	_, err := s.GetUserByID(ctx, id, tenantID)
	if err != nil {
		return err
	}

	// 防止用户停用自己
	if id == currentUserID && !active {
		return fmt.Errorf("不能停用当前登录用户")
	}

	err = s.client.User.UpdateOneID(id).
		Where(user.TenantIDEQ(tenantID)).
		SetActive(active).
		Exec(ctx)
	if err != nil {
		return fmt.Errorf("更改用户状态失败: %w", err)
	}

	// 停用账户时立即吊销其全部存量 access token（P1-2 延伸：停用不应等 token 自然过期）。
	if !active {
		if revokeErr := middleware.InvalidateUserAccessTokens(ctx, id, time.Now()); revokeErr != nil {
			s.logger.Errorw("停用用户后吊销存量token失败", "user_id", id, "error", revokeErr)
		} else {
			s.logger.Infow("用户已停用，存量access token已吊销", "user_id", id)
			// PAT（D-PAT-6/R3）：停用 → 全部个人访问令牌吊销（security_event）。
			pat.RevokeAllForUserByClient(ctx, s.client, s.logger, id)
		}
	}

	s.logger.Infof("用户状态更改成功: ID=%d, active=%t", id, active)
	return nil
}

// ResetPassword 重置用户密码
func (s *UserService) ResetPassword(ctx context.Context, id int, newPassword string, tenantID int) error {
	s.logger.Infof("重置用户密码: ID=%d", id)

	// 检查用户是否存在
	_, err := s.GetUserByID(ctx, id, tenantID)
	if err != nil {
		return err
	}
	if err := s.PasswordPolicy(ctx, tenantID).Validate(newPassword); err != nil {
		return err
	}

	// 加密新密码
	hashedPassword, err := bcrypt.GenerateFromPassword([]byte(newPassword), bcrypt.DefaultCost)
	if err != nil {
		return fmt.Errorf("密码加密失败: %w", err)
	}

	err = s.client.User.UpdateOneID(id).
		Where(user.TenantIDEQ(tenantID)).
		SetPasswordHash(string(hashedPassword)).
		Exec(ctx)
	if err != nil {
		return fmt.Errorf("重置密码失败: %w", err)
	}

	// 密码重置后立即吊销该用户全部存量 access token（防旧会话存活）。
	if revokeErr := middleware.InvalidateUserAccessTokens(ctx, id, time.Now()); revokeErr != nil {
		s.logger.Errorw("密码重置后吊销存量token失败", "user_id", id, "error", revokeErr)
	}
	// PAT（D-PAT-6/R3）：密码重置 → 全部个人访问令牌吊销（security_event，Q1 决策）。
	pat.RevokeAllForUserByClient(ctx, s.client, s.logger, id)

	s.logger.Infof("用户密码重置成功: ID=%d", id)
	return nil
}

// GetUserStats 获取用户统计信息
func (s *UserService) GetUserStats(ctx context.Context, tenantID int) (*dto.UserStatsResponse, error) {
	s.logger.Infof("获取用户统计: tenantID=%d", tenantID)

	if tenantID <= 0 {
		return nil, fmt.Errorf("租户信息无效")
	}
	query := s.client.User.Query().Where(user.TenantIDEQ(tenantID))

	// 总用户数
	total, err := query.Count(ctx)
	if err != nil {
		return nil, fmt.Errorf("统计总用户数失败: %w", err)
	}

	// 活跃用户数
	active, err := query.Clone().Where(user.ActiveEQ(true)).Count(ctx)
	if err != nil {
		return nil, fmt.Errorf("统计活跃用户数失败: %w", err)
	}

	monthStart := time.Now().In(time.Local)
	monthStart = time.Date(monthStart.Year(), monthStart.Month(), 1, 0, 0, 0, 0, monthStart.Location())
	newThisMonth, err := query.Clone().Where(user.CreatedAtGTE(monthStart)).Count(ctx)
	if err != nil {
		return nil, fmt.Errorf("统计本月新增用户失败: %w", err)
	}

	response := &dto.UserStatsResponse{
		Total:  total,
		Active: active,
		// 当前模型没有登录会话/心跳数据，返回0，避免把“启用”误报为“在线”。
		Online:       0,
		ByRole:       map[string]int{},
		ByDepartment: map[string]int{},
		NewThisMonth: newThisMonth,
	}

	s.logger.Infow("用户统计获取成功", "total", total, "active", active, "online", 0, "new_this_month", newThisMonth)
	return response, nil
}

// BatchUpdateUsers 批量更新用户
func (s *UserService) BatchUpdateUsers(ctx context.Context, req *dto.BatchUpdateUsersRequest, tenantID int) error {
	s.logger.Infof("批量更新用户: count=%d", len(req.UserIDs))

	if len(req.UserIDs) == 0 {
		return fmt.Errorf("用户ID列表不能为空")
	}

	ids := uniquePositiveIDs(req.UserIDs)
	if len(ids) == 0 {
		return fmt.Errorf("用户ID列表不能为空")
	}
	if req.Action == "deactivate" && req.OperatorID > 0 {
		for _, id := range ids {
			if id == req.OperatorID {
				return fmt.Errorf("批量停用不能包含当前登录用户")
			}
		}
	}
	matched, err := s.client.User.Query().Where(user.IDIn(ids...), user.TenantIDEQ(tenantID)).Count(ctx)
	if err != nil {
		return fmt.Errorf("校验批量用户失败: %w", err)
	}
	if matched != len(ids) {
		return fmt.Errorf("部分用户不存在或不属于当前租户，请刷新列表后重试")
	}

	update := s.client.User.Update().
		Where(
			user.IDIn(ids...),
			user.TenantIDEQ(tenantID),
		)

	// 根据操作类型更新
	switch req.Action {
	case "activate":
		update = update.SetActive(true)
	case "deactivate":
		update = update.SetActive(false)
	case "department":
		if req.Department == "" {
			return fmt.Errorf("部门不能为空")
		}
		update = update.SetDepartment(req.Department)
	default:
		return fmt.Errorf("不支持的操作类型: %s", req.Action)
	}

	count, err := update.Save(ctx)
	if err != nil {
		return fmt.Errorf("批量更新用户失败: %w", err)
	}

	// IP-P2-8：批量停用同样立即吊销存量会话（与单条停用口径一致）。
	if req.Action == "deactivate" {
		now := time.Now()
		for _, id := range ids {
			if revokeErr := middleware.InvalidateUserAccessTokens(ctx, id, now); revokeErr != nil {
				s.logger.Errorw("批量停用后吊销存量access token失败", "user_id", id, "error", revokeErr)
			}
			// PAT（D-PAT-6/R3）：批量停用 → 全部个人访问令牌吊销（security_event）。
			pat.RevokeAllForUserByClient(ctx, s.client, s.logger, id)
			if revokeErr := middleware.InvalidateUserRefreshTokens(ctx, id, now); revokeErr != nil {
				s.logger.Errorw("批量停用后吊销存量refresh token失败", "user_id", id, "error", revokeErr)
			}
		}
	}

	s.logger.Infof("批量更新用户成功: updated=%d", count)
	return nil
}

// validatePassword 保留包内默认策略入口（等价于 DefaultPasswordPolicy().Validate），
// 供不持有租户上下文的调用方使用；业务路径请优先走 (*UserService).PasswordPolicy。
//
//lint:ignore U1000 预留入口：包内默认策略便捷函数，当前无调用方（CI staticcheck v0.6.1 会报 U1000）
func validatePassword(password string) error {
	return DefaultPasswordPolicy().Validate(password)
}

// SearchUsers 搜索用户
func (s *UserService) SearchUsers(ctx context.Context, req *dto.SearchUsersRequest, tenantID int) ([]*dto.UserDetailResponse, error) {
	s.logger.Infof("搜索用户: keyword=%s", req.Keyword)

	if req.Keyword == "" {
		return []*dto.UserDetailResponse{}, nil
	}

	query := s.client.User.Query().
		Where(
			user.Or(
				user.UsernameContainsFold(req.Keyword),
				user.NameContainsFold(req.Keyword),
				user.EmailContainsFold(req.Keyword),
			),
		)

	query = query.Where(user.TenantIDEQ(tenantID))

	// 只返回活跃用户
	query = query.Where(user.ActiveEQ(true))

	users, err := query.
		Limit(req.Limit).
		Order(ent.Asc(user.FieldName)).
		All(ctx)
	if err != nil {
		return nil, fmt.Errorf("搜索用户失败: %w", err)
	}

	// 转换为响应格式
	userResponses := make([]*dto.UserDetailResponse, 0, len(users))
	for _, u := range users {
		userResponses = append(userResponses, &dto.UserDetailResponse{
			ID:         u.ID,
			Username:   u.Username,
			Email:      u.Email,
			Name:       u.Name,
			Department: u.Department,
			Phone:      u.Phone,
			Active:     u.Active,
			TenantID:   u.TenantID,
			Role:       string(u.Role),
			CreatedAt:  u.CreatedAt,
			UpdatedAt:  u.UpdatedAt,
		})
	}

	s.logger.Infof("用户搜索成功: found=%d", len(users))
	return userResponses, nil
}
