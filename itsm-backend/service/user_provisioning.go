package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"sort"
	"strings"
	"time"

	"go.uber.org/zap"

	"itsm-backend/common/tenantctx"
	"itsm-backend/dto"
	"itsm-backend/ent"
	entrole "itsm-backend/ent/role"
	"itsm-backend/ent/tenant"
	"itsm-backend/middleware"
	"itsm-backend/pkg/mspguard"
	"itsm-backend/pkg/tenantmode"
	"itsm-backend/pkg/tenantquota"
)

// provisioning.go：三通道建号收口（IP-P0-5；canon K4、ADR-004:A2/A3、F1/F3/F14）。
//
// 设计约束（目标架构 §6.1）：
//  1. handler 不得自行拼装 bypass；唯一入口 = UserProvisioningService.ProvisionUser
//     （注意与"服务请求交付任务"的 ProvisioningService 区分，同名冲突已由本命名规避）；
//  2. 通道矩阵 platform / msp / tenant（invite 属 P1）；
//  3. 角色白名单 + 通道内 rank 上限；msp_role 仅平台/provider 面可写；
//  4. 跨租户写使用 tenantctx.WithProvisioningBypass(actor, channel, target)（带审计），
//     并且 ctx 的 tenant 一律指向目标租户，保证 RLS/写守卫按目标租户收窄。

// 建号通道（权威：配套方案 §2 通道矩阵）。
const (
	ProvisionChannelPlatform = "platform"
	ProvisionChannelMSP      = "msp"
	ProvisionChannelTenant   = "tenant"
)

// 建号错误码（权威：配套方案 §2）。
const (
	ProvisionCodeCrossTenantForbidden = "CROSS_TENANT_FORBIDDEN"
	ProvisionCodeRoleNotGrantable     = "ROLE_NOT_GRANTABLE"
	ProvisionCodeMSPRoleNotAllowed    = "MSP_ROLE_NOT_ALLOWED"
	ProvisionCodeUsernameExists       = "USERNAME_EXISTS"
	ProvisionCodeEmailExists          = "EMAIL_EXISTS"
	ProvisionCodeTenantNotFound       = "TENANT_NOT_FOUND"
	ProvisionCodeTenantSuspended      = "TENANT_SUSPENDED"
	ProvisionCodeChannelsDisabled     = "PROVISIONING_CHANNELS_DISABLED"
	// ProvisionCodeTenantQuotaExceeded IP-P2-6：目标租户 maxUsers 配额超限（HTTP 422）。
	ProvisionCodeTenantQuotaExceeded = tenantquota.CodeTenantQuotaExceeded
)

// userRoleEnumCompatible users.role 枚举词表（ent/schema/user.go）：
// 仅这些 code 可回写主角色列；自定义/MSP 角色只能经 user_roles 边（roleIds）授予。
var userRoleEnumCompatible = map[string]bool{
	"super_admin": true, "admin": true, "manager": true, "it_admin": true,
	"security_admin": true, "sysadmin": true, "agent": true, "technician": true,
	"security": true, "end_user": true,
}

// ProvisionError 是建号通道的稳定拒绝类型：handler 依据 Code/Status 映射响应。
type ProvisionError struct {
	Code    string
	Message string
	Status  int
}

func (e *ProvisionError) Error() string { return e.Code + ": " + e.Message }

func newProvisionError(code string, status int, format string, args ...interface{}) *ProvisionError {
	return &ProvisionError{Code: code, Message: fmt.Sprintf(format, args...), Status: status}
}

// AsProvisionError 提取稳定错误码；非建号类错误返回 ok=false。
func AsProvisionError(err error) (*ProvisionError, bool) {
	var target *ProvisionError
	if errors.As(err, &target) {
		return target, true
	}
	return nil, false
}

// ProvisionActor 是调用方身份快照（来自 JWT claims / gin context，不含敏感信息）。
type ProvisionActor struct {
	UserID       int
	HomeTenantID int
	Role         string // 主角色（users.role / JWT role）
	MSPRole      string // provider_admin / provider_agent（可空）
	Username     string
}

// UserProvisioningService 收口建号通道。零值不可用；用 NewUserProvisioningService 构造。
type UserProvisioningService struct {
	client          *ent.Client
	users           *UserService
	checker         *mspguard.Checker
	logger          *zap.SugaredLogger
	channelsEnabled bool
	quotaSvc        *TenantQuotaService    // IP-P2-6：租户硬配额（maxUsers），nil 时跳过
	membershipSync  *MembershipSyncService // MSW-1：建号后补齐 home membership（幂等）
}

// NewUserProvisioningService 构造建号收口服务。
// 开关：USER_PROVISIONING_CHANNELS_ENABLED **默认开启**（未设置/空/无法识别视为开启，
// 2026-10-08 起与手册/部署文档一致）；显式 false/0/no/off 可回滚关闭；
// 也可用 SetChannelsEnabled 显式覆盖（测试/按租户灰度）。
func NewUserProvisioningService(client *ent.Client, users *UserService, logger *zap.SugaredLogger) *UserProvisioningService {
	return &UserProvisioningService{
		client:          client,
		users:           users,
		checker:         mspguard.New(client),
		logger:          logger,
		channelsEnabled: channelsEnabledFromEnv(os.Getenv("USER_PROVISIONING_CHANNELS_ENABLED")),
		membershipSync:  NewMembershipSyncService(client, logger),
	}
}

// channelsEnabledFromEnv 解析建号通道开关（默认开）：
//   - 未设置 / 空 / 无法识别 → true（开箱可用）；
//   - false / 0 / no / off（大小写不敏感，容忍空白）→ false（显式回滚关闭）。
func channelsEnabledFromEnv(raw string) bool {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "false", "0", "no", "off":
		return false
	default:
		return true
	}
}

// SetChannelsEnabled 显式设置灰度开关（测试与按租户灰度使用）。
func (s *UserProvisioningService) SetChannelsEnabled(enabled bool) { s.channelsEnabled = enabled }

// SetTenantQuotaService 注入租户配额服务（IP-P2-6；nil 关闭校验）。
func (s *UserProvisioningService) SetTenantQuotaService(q *TenantQuotaService) { s.quotaSvc = q }

// ChannelsEnabled 报告建号通道是否开启。
func (s *UserProvisioningService) ChannelsEnabled() bool { return s != nil && s.channelsEnabled }

// ProvisionUser 在目标租户建号：CanAccessTenant → 通道授权 + 角色白名单 →
// WithProvisioningBypass(actor, channel) → WithTenantID(target) → UserService.CreateUser。
func (s *UserProvisioningService) ProvisionUser(ctx context.Context, actor ProvisionActor, targetTenantID int, req *dto.CreateUserRequest) (*ent.User, error) {
	if s == nil || s.client == nil || s.users == nil {
		return nil, fmt.Errorf("provisioning service unavailable")
	}
	if !s.channelsEnabled {
		return nil, newProvisionError(ProvisionCodeChannelsDisabled, http.StatusNotFound,
			"建号通道未开启（USER_PROVISIONING_CHANNELS_ENABLED）")
	}
	if req == nil {
		return nil, newProvisionError("BAD_REQUEST", http.StatusBadRequest, "请求体不能为空")
	}

	target, err := s.client.Tenant.Query().Where(tenant.IDEQ(targetTenantID)).Only(ctx)
	if err != nil {
		if ent.IsNotFound(err) {
			return nil, newProvisionError(ProvisionCodeTenantNotFound, http.StatusNotFound, "目标租户不存在: %d", targetTenantID)
		}
		return nil, fmt.Errorf("查询目标租户失败: %w", err)
	}
	if target.Status != "active" {
		return nil, newProvisionError(ProvisionCodeTenantSuspended, http.StatusForbidden,
			"目标租户不可用（status=%s）", target.Status)
	}

	channel, err := s.resolveChannel(ctx, actor, target)
	if err != nil {
		return nil, err
	}
	selectedRoles, err := s.validateRole(ctx, actor, channel, target, req)
	if err != nil {
		return nil, err
	}
	// IP-P2-7/D3：roleIds → 主角色派生（仅枚举兼容角色回写 users.role；自定义角色保持默认 end_user，
	// 权限以 user_roles 边为准）。
	s.applyDerivedPrimaryRole(target.ID, req, selectedRoles)

	// IP-P2-6：租户硬配额（maxUsers）校验；bootstrap/break-glass 通道不在本服务范围内。
	if s.quotaSvc != nil {
		if err := s.quotaSvc.CheckUserCreate(ctx, target.ID); err != nil {
			if _, ok := tenantquota.AsExceeded(err); ok {
				return nil, newProvisionError(ProvisionCodeTenantQuotaExceeded, http.StatusUnprocessableEntity, "%s", err.Error())
			}
			return nil, err
		}
	}

	provisionCtx := tenantctx.WithTenantID(ctx, target.ID)
	actorLabel := strings.TrimSpace(actor.Username)
	if actorLabel == "" {
		actorLabel = fmt.Sprintf("user:%d", actor.UserID)
	}
	if channel != ProvisionChannelTenant {
		provisionCtx = tenantctx.WithProvisioningBypass(provisionCtx, actorLabel, channel, target.ID)
	}

	created, err := s.users.CreateUser(provisionCtx, req, target.ID)
	if err != nil {
		switch {
		case strings.Contains(err.Error(), "用户名已存在"):
			return nil, newProvisionError(ProvisionCodeUsernameExists, http.StatusConflict, "%s", err.Error())
		case strings.Contains(err.Error(), "邮箱已存在"):
			return nil, newProvisionError(ProvisionCodeEmailExists, http.StatusConflict, "%s", err.Error())
		default:
			return nil, err
		}
	}

	s.logger.Infow("user provisioned",
		"channel", channel,
		"actor", actorLabel,
		"actor_user_id", actor.UserID,
		"target_tenant_id", target.ID,
		"user_id", created.ID,
		"role", created.Role,
	)
	// MSW-1：建号即写 home membership（幂等；失败不阻断建号，靠巡检兜底修复）。
	// IP-P2-7/D3：roleIds 场景下 membership 角色按所选最高 rank 角色解析（msp_role 映射优先级不变）。
	s.syncHomeMembershipAfterProvision(provisionCtx, actor, actorLabel, channel, target, created, highestRankRoleCode(selectedRoles))
	s.recordProvisionAudit(ctx, actor, channel, target.ID, created.ID)
	return created, nil
}

// syncHomeMembershipAfterProvision 建号后补齐 home membership（MSW-1）。
// account_kind 按账号类型快照（provider/customer/platform）；角色 code 按
// msp_role 映射（provider_admin→msp_manager、provider_agent→msp_tech），否则取 users.role。
func (s *UserProvisioningService) syncHomeMembershipAfterProvision(
	ctx context.Context,
	actor ProvisionActor,
	actorLabel, channel string,
	target *ent.Tenant,
	created *ent.User,
	roleIDsTopCode string,
) {
	if s.membershipSync == nil || target == nil || created == nil {
		return
	}
	roleCode, mspRole := provisionMembershipRoles(created)
	// IP-P2-7/D3：roleIds 场景按所选最高 rank 角色解析 membership 角色（含自定义/MSP 角色）；
	// msp_role 映射优先级不变（provider_agent → msp_tech；provider_admin 员工 → msp_manager；
	// provider 首管例外 → provider_tenant_admin/msp_admin，见 provisionMembershipRoles）。
	if mspRole == "" && strings.TrimSpace(roleIDsTopCode) != "" {
		roleCode = roleIDsTopCode
	}
	rowTenant := actor.HomeTenantID
	if rowTenant <= 0 {
		rowTenant = target.ID
	}
	source := middleware.AuditSourceHeader
	switch channel {
	case ProvisionChannelPlatform:
		source = middleware.AuditSourcePlatformSelected
	case ProvisionChannelTenant:
		source = middleware.AuditSourceLogin
	}
	meta := MembershipAuditMeta{
		RowTenantID:  rowTenant,
		ActorUserID:  actor.UserID,
		ActorAccount: actorLabel,
		Source:       source,
		Trigger:      "user.provision:" + channel,
	}
	if _, err := s.membershipSync.SyncHomeMembership(ctx, created.ID, target.ID, roleCode, mspRole, meta); err != nil {
		s.logger.Warnw("membership sync after provision failed (account created; repair via inspection)",
			"error", err, "user_id", created.ID, "target_tenant_id", target.ID)
	}
}

// provisionMembershipRoles 账号 → membership 词表（home 作用域；迁移 20260504 回填 1 同口径）。
//
// 07:G11 例外（2026-10-10 回归修复）：provider 首管（is_bootstrap_admin）绑定组合角色
// provider_tenant_admin + msp_admin，与 pkg/bootstrap 首管策略（RBACRoleCode=
// provider_tenant_admin / MembershipMSPRole=msp_admin）单源对齐。否则建号/回填/对账
// 会把首管 membership 校正回纯 MSP 角色 msp_manager，再次触发「自身 ITSM 菜单全被
// 权限过滤」（只见 MSP 菜单）。
func provisionMembershipRoles(u *ent.User) (roleCode, mspRole string) {
	switch strings.ToLower(strings.TrimSpace(string(u.MspRole))) {
	case "provider_admin":
		if u.IsBootstrapAdmin {
			return "provider_tenant_admin", "msp_admin"
		}
		return "msp_manager", "msp_manager"
	case "provider_agent":
		return "msp_tech", "msp_tech"
	}
	return strings.ToLower(strings.TrimSpace(string(u.Role))), ""
}

// recordProvisionAudit 审计事件 user.provision（IP-P0-10）：source 按通道映射，
// 审计行归属 actor 家租户，target_tenant_id 指向目标租户。
// ctx 必须携带 actor 家租户（enforce 下审计行 tenant_id = 当前租户作用域，fail-closed）。
func (s *UserProvisioningService) recordProvisionAudit(ctx context.Context, actor ProvisionActor, channel string, targetTenantID, createdUserID int) {
	if s.client == nil {
		return
	}
	source := middleware.AuditSourceHeader
	switch channel {
	case ProvisionChannelPlatform:
		source = middleware.AuditSourcePlatformSelected
	case ProvisionChannelTenant:
		source = middleware.AuditSourceLogin
	}
	rowTenant := actor.HomeTenantID
	if rowTenant <= 0 {
		rowTenant = targetTenantID
	}
	payload, _ := json.Marshal(map[string]any{
		"channel":              channel,
		"actor_user_id":        actor.UserID,
		"actor_home_tenant_id": actor.HomeTenantID,
		"target_tenant_id":     targetTenantID,
		"created_user_id":      createdUserID,
	})
	auditCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	if _, err := s.client.AuditLog.Create().
		SetCreatedAt(time.Now()).
		SetTenantID(rowTenant).
		SetUserID(actor.UserID).
		SetActorAccount(actor.Username).
		SetTargetTenantID(targetTenantID).
		SetSource(source).
		SetResource("user").
		SetAction("user.provision").
		SetPath("/api/v1/msp/customers/:id/users").
		SetMethod("POST").
		SetStatusCode(201).
		SetRequestBody(string(payload)).
		Save(auditCtx); err != nil && s.logger != nil {
		s.logger.Warnw("provision audit write failed", "error", err, "target_tenant_id", targetTenantID)
	}
}

// resolveChannel 依调用方与目标租户解析通道；不满足任何通道 → CROSS_TENANT_FORBIDDEN。
func (s *UserProvisioningService) resolveChannel(ctx context.Context, actor ProvisionActor, target *ent.Tenant) (string, error) {
	// 平台通道：平台角色 → 任意 active 租户（目标状态已在上面校验）。
	if isPlatformActorRole(actor.Role) {
		return ProvisionChannelPlatform, nil
	}
	// 租户通道：本租户内建号。
	if actor.HomeTenantID > 0 && actor.HomeTenantID == target.ID {
		return ProvisionChannelTenant, nil
	}
	// MSP 通道：provider 管理员 + 目标为客户租户 + 有效 allocation（含归属校验，R2/R9）。
	if isProviderAdminActor(actor) && tenantmode.IsCustomerTenantType(string(target.Type)) {
		if err := s.checker.CanAccessCustomer(ctx, actor.UserID, target.ID); err != nil {
			if ae, ok := mspguard.AsAccessError(err); ok {
				status := http.StatusForbidden
				if ae.Code == mspguard.CodeCustomerTenantNotFound {
					status = http.StatusNotFound
				}
				return "", newProvisionError(ae.Code, status, "%s", ae.Message)
			}
			return "", err
		}
		return ProvisionChannelMSP, nil
	}
	return "", newProvisionError(ProvisionCodeCrossTenantForbidden, http.StatusForbidden,
		"无权限在目标租户建号（需要平台角色、该租户内管理员身份或目标客户的有效 MSP 分配）")
}

// validateRole 角色白名单 + 通道内 rank 上限 + msp_role 写入口校验。
// 返回 roleIds 对应的目标租户角色行（IP-P2-7：供主角色派生/membership 解析复用，单次查询）。
func (s *UserProvisioningService) validateRole(ctx context.Context, actor ProvisionActor, channel string, target *ent.Tenant, req *dto.CreateUserRequest) ([]*ent.Role, error) {
	// msp_role：白名单 provider_admin/provider_agent（customer_user 仅 legacy 读映射）。
	if mspRole := strings.ToLower(strings.TrimSpace(req.MSPRole)); mspRole != "" {
		if mspRole != "provider_admin" && mspRole != "provider_agent" {
			return nil, newProvisionError(ProvisionCodeMSPRoleNotAllowed, http.StatusUnprocessableEntity,
				"mspRole=%s 不允许写入（白名单 provider_admin/provider_agent）", req.MSPRole)
		}
		allowed := channel == ProvisionChannelPlatform || channel == ProvisionChannelMSP ||
			(channel == ProvisionChannelTenant &&
				tenantmode.IsMSPProviderTenantType(string(target.Type)) &&
				actor.HomeTenantID == target.ID)
		if !allowed {
			return nil, newProvisionError(ProvisionCodeMSPRoleNotAllowed, http.StatusUnprocessableEntity,
				"当前通道不允许设置 mspRole（仅平台通道、MSP 通道或 provider 本租户）")
		}
	}

	// 主角色：显式拒绝平台角色；其余按通道 rank 上限。
	role := normalizeProvisionRole(req.Role)
	if role != "" {
		if middleware.IsPlatformRole(role) {
			return nil, newProvisionError(ProvisionCodeRoleNotGrantable, http.StatusUnprocessableEntity,
				"不允许授予平台角色: %s", role)
		}
		if serviceRoleRank(role) > provisionCallerRank(actor, channel) {
			return nil, newProvisionError(ProvisionCodeRoleNotGrantable, http.StatusUnprocessableEntity,
				"无权限授予高于当前通道上限的角色: %s", role)
		}
	}

	// roleIds 上限：复用既有 CanGrantRoles（跨租户角色 ID 与 rank 校验）。
	if len(req.RoleIDs) > 0 {
		// RLS：角色行按目标租户作用域读取（平台/ MSP 跨租户场景在 caller 上下文会读空）。
		roleCtx := tenantctx.WithTenantID(ctx, target.ID)
		if err := s.users.CanGrantRoles(roleCtx, target.ID, req.RoleIDs, provisionCallerRole(actor, channel)); err != nil {
			return nil, newProvisionError(ProvisionCodeRoleNotGrantable, http.StatusUnprocessableEntity, "%s", err.Error())
		}
		roles, err := s.client.Role.Query().
			Where(entrole.IDIn(req.RoleIDs...), entrole.TenantIDEQ(target.ID)).
			All(roleCtx)
		if err != nil {
			return nil, fmt.Errorf("查询目标租户角色失败: %w", err)
		}
		// 平台角色防护：super_admin/sysadmin 与 req.Role 分支同口径（词表单一源 middleware），不允许经 roleIds 绕过。
		for _, r := range roles {
			if middleware.IsPlatformRole(r.Code) {
				return nil, newProvisionError(ProvisionCodeRoleNotGrantable, http.StatusUnprocessableEntity,
					"不允许授予平台角色: %s", r.Code)
			}
		}
		return roles, nil
	}
	return nil, nil
}

// GrantableRoles 返回目标租户内、当前调用者/通道可授予的角色候选（IP-P2-7）。
//
// 与建号写入共享 resolveChannel + 同一 rank/平台角色判定，保证 UI 候选与 validateRole 同源；
// 供建号弹窗多选使用（只读，不改变任何写入语义）。
func (s *UserProvisioningService) GrantableRoles(ctx context.Context, actor ProvisionActor, targetTenantID int) ([]dto.RoleOption, error) {
	if s == nil || s.client == nil {
		return nil, fmt.Errorf("provisioning service unavailable")
	}
	if !s.channelsEnabled {
		return nil, newProvisionError(ProvisionCodeChannelsDisabled, http.StatusNotFound,
			"建号通道未开启（USER_PROVISIONING_CHANNELS_ENABLED）")
	}
	if targetTenantID <= 0 {
		return nil, newProvisionError("BAD_REQUEST", http.StatusBadRequest, "无效的租户ID")
	}
	target, err := s.client.Tenant.Query().Where(tenant.IDEQ(targetTenantID)).Only(ctx)
	if err != nil {
		if ent.IsNotFound(err) {
			return nil, newProvisionError(ProvisionCodeTenantNotFound, http.StatusNotFound, "目标租户不存在: %d", targetTenantID)
		}
		return nil, fmt.Errorf("查询目标租户失败: %w", err)
	}
	if target.Status != "active" {
		return nil, newProvisionError(ProvisionCodeTenantSuspended, http.StatusForbidden,
			"目标租户不可用（status=%s）", target.Status)
	}
	channel, err := s.resolveChannel(ctx, actor, target)
	if err != nil {
		return nil, err
	}

	// RLS：按目标租户作用域只读角色表（平台/ MSP 跨租户场景在 caller 上下文会读空）。
	readCtx := tenantctx.WithTenantID(ctx, target.ID)
	roles, err := s.client.Role.Query().
		Where(entrole.TenantIDEQ(target.ID), entrole.IsActiveEQ(true)).
		All(readCtx)
	if err != nil {
		return nil, fmt.Errorf("查询目标租户角色失败: %w", err)
	}

	rankCap := provisionCallerRank(actor, channel)
	customerScope := tenantmode.IsCustomerTenantType(string(target.Type))
	out := make([]dto.RoleOption, 0, len(roles))
	for _, r := range roles {
		code := strings.ToLower(strings.TrimSpace(r.Code))
		if code == "" || middleware.IsPlatformRole(code) {
			continue
		}
		// 客户类型租户：MSP 专属角色不由客户员工持有（IP-P2-7 收口；词表单一源 middleware.MSPOnlyRoleCodes）。
		if customerScope && middleware.IsMSPOnlyRole(code) {
			continue
		}
		rank := serviceRoleRank(code)
		if rank > rankCap {
			continue
		}
		out = append(out, dto.RoleOption{
			ID: r.ID, Code: r.Code, Name: r.Name, Description: r.Description,
			IsSystem: r.IsSystem, Rank: rank,
		})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Rank != out[j].Rank {
			return out[i].Rank < out[j].Rank
		}
		return out[i].Code < out[j].Code
	})
	return out, nil
}

// applyDerivedPrimaryRole roleIds 非空且未显式指定主角色时，取最高 rank 的枚举兼容角色回写 req.Role（D3）。
// 自定义/MSP 角色无 users.role 枚举映射时保持默认 end_user，权限以 user_roles 边为准。
func (s *UserProvisioningService) applyDerivedPrimaryRole(targetTenantID int, req *dto.CreateUserRequest, roles []*ent.Role) {
	if req == nil || strings.TrimSpace(req.Role) != "" || len(roles) == 0 {
		return
	}
	bestCode, bestRank := "", -1
	for _, r := range roles {
		code := strings.ToLower(strings.TrimSpace(r.Code))
		if !userRoleEnumCompatible[code] {
			continue
		}
		if rank := serviceRoleRank(code); rank > bestRank {
			bestCode, bestRank = code, rank
		}
	}
	if bestCode == "" {
		if s.logger != nil {
			s.logger.Infow("derive primary role: no enum-compatible role among roleIds; users.role stays default",
				"target_tenant_id", targetTenantID, "role_count", len(roles))
		}
		return
	}
	req.Role = bestCode
}

// highestRankRoleCode 取角色集中 rank 最高的 code（空集返回 ""；用于 membership 角色解析）。
func highestRankRoleCode(roles []*ent.Role) string {
	bestCode, bestRank := "", -1
	for _, r := range roles {
		code := strings.ToLower(strings.TrimSpace(r.Code))
		if code == "" {
			continue
		}
		if rank := serviceRoleRank(code); rank > bestRank {
			bestCode, bestRank = code, rank
		}
	}
	return bestCode
}

// provisionCallerRank 各通道可授予角色的 rank 上限：
// platform=5（平台治理，super_admin/sysadmin 已在上面显式拒绝）；
// msp=4（provider 管理员为分配客户建首个客户管理员，天花板 admin）；
// tenant=调用方有效角色 rank（provider 侧按 msp_role 映射计入）。
func provisionCallerRank(actor ProvisionActor, channel string) int {
	switch channel {
	case ProvisionChannelPlatform:
		return 5
	case ProvisionChannelMSP:
		return 4
	default:
		rank := serviceRoleRank(actor.Role)
		if mapped := serviceRoleRank(mappedMSPRole(actor.MSPRole)); mapped > rank {
			rank = mapped
		}
		return rank
	}
}

// provisionCallerRole 供 CanGrantRoles 比较的角色字符串。
func provisionCallerRole(actor ProvisionActor, channel string) string {
	switch channel {
	case ProvisionChannelPlatform:
		return "super_admin"
	case ProvisionChannelMSP:
		return "admin"
	default:
		if strings.TrimSpace(actor.Role) != "" {
			return actor.Role
		}
		return mappedMSPRole(actor.MSPRole)
	}
}

func normalizeProvisionRole(role string) string {
	r := strings.ToLower(strings.TrimSpace(role))
	if r == "user" {
		return "end_user"
	}
	return r
}

func mappedMSPRole(mspRole string) string {
	switch strings.ToLower(strings.TrimSpace(mspRole)) {
	case "provider_admin":
		return "msp_manager"
	case "provider_agent":
		return "msp_tech"
	default:
		return strings.ToLower(strings.TrimSpace(mspRole))
	}
}

func isPlatformActorRole(role string) bool {
	switch strings.ToLower(strings.TrimSpace(role)) {
	case "super_admin", "sysadmin":
		return true
	}
	return false
}

func isProviderAdminActor(actor ProvisionActor) bool {
	switch strings.ToLower(strings.TrimSpace(actor.MSPRole)) {
	case "provider_admin":
		return true
	}
	switch strings.ToLower(strings.TrimSpace(actor.Role)) {
	case "msp_admin", "msp_manager":
		return true
	}
	return false
}
