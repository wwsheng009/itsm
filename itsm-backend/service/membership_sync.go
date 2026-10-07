package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"go.uber.org/zap"

	"itsm-backend/common/tenantctx"
	"itsm-backend/ent"
	"itsm-backend/ent/role"
	"itsm-backend/ent/tenant"
	"itsm-backend/ent/usertenantmembership"
	"itsm-backend/middleware"
	"itsm-backend/pkg/tenantmode"
)

// membership_sync.go：membership 写路径收口（MSW-1；权威：canon I3、目标架构 §3.2）。
//
// 背景：读路径（登录/刷新/切换/auth.me/菜单）已是 membership 单源（IP-P1-2），
// 但写路径只有 bootstrap / invitation / 存量回填会写 user_tenant_memberships；
// 建号通道与 MSP 分配通道不写 → 切换后权限空窗（fail-closed）。本服务把
// 「建号 → home membership」「分配 → allocation membership」「撤销 → 软删」收口到单点，
// 幂等、带审计（membership.sync / membership.revoke），供各通道与巡检复用。
//
// 冲突策略（部分唯一索引 uq_membership_live：user_id+tenant_id where deleted_at IS NULL）：
//   - 同源存活行：幂等校正（role_id/msp_role/account_kind/allocation_id/status），仅漂移时更新；
//   - 异源存活行（如已有 home/invite 行）：不覆盖，返回 ErrMembershipSourceConflict 交由巡检/人工处置。

// membership 同步审计动作（resource=membership）。
const (
	MembershipAuditActionSync   = "membership.sync"
	MembershipAuditActionRevoke = "membership.revoke"
)

// ErrMembershipSourceConflict 同租户已存在不同来源的存活成员行（不覆盖）。
var ErrMembershipSourceConflict = errors.New("live membership with different source exists")

// MembershipSyncService 账号↔租户关系（membership）写路径收口服务。
type MembershipSyncService struct {
	client *ent.Client
	logger *zap.SugaredLogger
}

// NewMembershipSyncService 构造 membership 同步服务；logger 为空时使用 Nop。
func NewMembershipSyncService(client *ent.Client, logger *zap.SugaredLogger) *MembershipSyncService {
	if logger == nil {
		logger = zap.NewNop().Sugar()
	}
	return &MembershipSyncService{client: client, logger: logger}
}

// MembershipAuditMeta 同步审计元数据。
type MembershipAuditMeta struct {
	RowTenantID  int    // 审计行归属租户（通常=actor 家租户）；<=0 时回退为作用域租户
	ActorUserID  int    // actor（0=系统/内部触发）
	ActorAccount string // actor 标识（用户名或 user:<id>）
	Source       string // 审计 source（默认 system）
	Trigger      string // 触发动作（user.provision:<channel> / msp.allocation.assign / ...）
}

// SyncHomeMembership 幂等写入/校正 home membership（建号/首管等 home 场景）。
//
// account_kind 按「账号类型」快照（迁移 20260504 回填 1 同口径）：
// provider 租户或 msp_role=provider_* → provider；客户租户 → customer；其余 → platform。
// roleCode 解析不到对应角色行时写 NULL 并告警（进入巡检清单，不得静默）。
func (s *MembershipSyncService) SyncHomeMembership(
	ctx context.Context,
	userID, tenantID int,
	roleCode, mspRole string,
	meta MembershipAuditMeta,
) (*ent.UserTenantMembership, error) {
	if s == nil || s.client == nil {
		return nil, errors.New("membership sync service unavailable")
	}
	if userID <= 0 || tenantID <= 0 {
		return nil, fmt.Errorf("invalid membership scope: user=%d tenant=%d", userID, tenantID)
	}
	scopeCtx := tenantctx.WithTenantID(ctx, tenantID)

	roleID, roleResolved := s.resolveRoleID(scopeCtx, tenantID, roleCode)
	if strings.TrimSpace(roleCode) != "" && !roleResolved {
		s.logger.Warnw("membership sync: role code unresolved, role_id stays NULL (巡检清单)",
			"user_id", userID, "tenant_id", tenantID, "role_code", roleCode)
	}
	accountKind, err := s.accountKindForHome(scopeCtx, tenantID, mspRole)
	if err != nil {
		return nil, err
	}

	live, err := s.liveMembership(scopeCtx, userID, tenantID)
	if err != nil {
		return nil, err
	}
	if live == nil {
		builder := s.client.UserTenantMembership.Create().
			SetUserID(userID).
			SetTenantID(tenantID).
			SetAccountKind(accountKind).
			SetSource(usertenantmembership.SourceHome).
			SetIsDefault(true).
			SetStatus(usertenantmembership.StatusActive)
		if roleID > 0 {
			builder = builder.SetRoleID(roleID)
		}
		if strings.TrimSpace(mspRole) != "" {
			builder = builder.SetMspRole(mspRole)
		}
		saved, err := builder.Save(scopeCtx)
		if err != nil {
			return nil, fmt.Errorf("create home membership user=%d tenant=%d: %w", userID, tenantID, err)
		}
		s.writeSyncAudit(scopeCtx, meta, MembershipAuditActionSync, "created", saved, roleCode)
		return saved, nil
	}
	if live.Source != usertenantmembership.SourceHome {
		return nil, fmt.Errorf("%w: user=%d tenant=%d source=%s",
			ErrMembershipSourceConflict, userID, tenantID, live.Source)
	}

	// 同源幂等校正：仅字段漂移时更新（避免审计噪音）。
	upd := live.Update()
	changed := false
	if roleID > 0 && (live.RoleID == nil || *live.RoleID != roleID) {
		upd = upd.SetRoleID(roleID)
		changed = true
	}
	if mspRole != "" && (live.MspRole == nil || *live.MspRole != mspRole) {
		upd = upd.SetMspRole(mspRole)
		changed = true
	}
	if live.AccountKind != accountKind {
		upd = upd.SetAccountKind(accountKind)
		changed = true
	}
	if live.Status != usertenantmembership.StatusActive {
		upd = upd.SetStatus(usertenantmembership.StatusActive)
		changed = true
	}
	if !changed {
		return live, nil
	}
	saved, err := upd.Save(scopeCtx)
	if err != nil {
		return nil, fmt.Errorf("update home membership user=%d tenant=%d: %w", userID, tenantID, err)
	}
	s.writeSyncAudit(scopeCtx, meta, MembershipAuditActionSync, "updated", saved, roleCode)
	return saved, nil
}

// SyncAllocationMembership 分配 → 客户作用域 membership（source=allocation；迁移 20260504 回填 2 同口径）。
//
// 词表映射（20260504 预设）：specialist→msp_specialist；primary/backup/其他→msp_tech。
// 注意：目标架构 §5.3 表格写 primary→msp_manager，与既有回填数据口径存在差异，
// 本实现按回填口径（最小权限）执行，差异登记为方案 MSW-Q5 待决。
func (s *MembershipSyncService) SyncAllocationMembership(
	ctx context.Context,
	alloc *ent.MSPAllocation,
	meta MembershipAuditMeta,
) (*ent.UserTenantMembership, error) {
	if s == nil || s.client == nil {
		return nil, errors.New("membership sync service unavailable")
	}
	if alloc == nil || alloc.ID <= 0 || alloc.MspUserID <= 0 || alloc.CustomerTenantID <= 0 {
		return nil, fmt.Errorf("invalid allocation for membership sync")
	}
	scopeCtx := tenantctx.WithTenantID(ctx, alloc.CustomerTenantID)

	roleCode, mspRole := allocationMembershipRoles(alloc.Role)
	roleID, roleResolved := s.resolveRoleID(scopeCtx, alloc.CustomerTenantID, roleCode)
	if !roleResolved {
		s.logger.Warnw("membership sync: allocation role unresolved, role_id stays NULL (巡检清单)",
			"allocation_id", alloc.ID, "tenant_id", alloc.CustomerTenantID, "role_code", roleCode)
	}

	live, err := s.liveMembership(scopeCtx, alloc.MspUserID, alloc.CustomerTenantID)
	if err != nil {
		return nil, err
	}
	if live == nil {
		builder := s.client.UserTenantMembership.Create().
			SetUserID(alloc.MspUserID).
			SetTenantID(alloc.CustomerTenantID).
			SetAccountKind(usertenantmembership.AccountKindProvider).
			SetSource(usertenantmembership.SourceAllocation).
			SetAllocationID(alloc.ID).
			SetIsDefault(false).
			SetStatus(usertenantmembership.StatusActive).
			SetJoinedAt(alloc.AssignedAt)
		if roleID > 0 {
			builder = builder.SetRoleID(roleID)
		}
		builder = builder.SetMspRole(mspRole)
		saved, err := builder.Save(scopeCtx)
		if err != nil {
			return nil, fmt.Errorf("create allocation membership allocation=%d: %w", alloc.ID, err)
		}
		s.writeSyncAudit(scopeCtx, meta, MembershipAuditActionSync, "created", saved, roleCode)
		return saved, nil
	}
	if live.Source != usertenantmembership.SourceAllocation {
		return nil, fmt.Errorf("%w: user=%d tenant=%d source=%s",
			ErrMembershipSourceConflict, alloc.MspUserID, alloc.CustomerTenantID, live.Source)
	}

	// 同源幂等校正（含重新分配后的 allocation_id 切换与撤销态复活）。
	upd := live.Update()
	changed := false
	if live.AllocationID == nil || *live.AllocationID != alloc.ID {
		upd = upd.SetAllocationID(alloc.ID)
		changed = true
	}
	if roleID > 0 && (live.RoleID == nil || *live.RoleID != roleID) {
		upd = upd.SetRoleID(roleID)
		changed = true
	}
	if live.MspRole == nil || *live.MspRole != mspRole {
		upd = upd.SetMspRole(mspRole)
		changed = true
	}
	if live.Status != usertenantmembership.StatusActive {
		upd = upd.SetStatus(usertenantmembership.StatusActive)
		changed = true
	}
	if live.DeassignedAt != nil {
		upd = upd.ClearDeassignedAt()
		changed = true
	}
	if !changed {
		return live, nil
	}
	saved, err := upd.Save(scopeCtx)
	if err != nil {
		return nil, fmt.Errorf("update allocation membership allocation=%d: %w", alloc.ID, err)
	}
	s.writeSyncAudit(scopeCtx, meta, MembershipAuditActionSync, "updated", saved, roleCode)
	return saved, nil
}

// RevokeAllocationMembership 分配撤销 → 软删对应 allocation membership（deassigned_at + deleted_at；回收即失效）。
// 幂等：无存活 allocation 成员行时静默返回 nil。
func (s *MembershipSyncService) RevokeAllocationMembership(
	ctx context.Context,
	mspUserID, customerTenantID int,
	meta MembershipAuditMeta,
) error {
	if s == nil || s.client == nil {
		return errors.New("membership sync service unavailable")
	}
	if mspUserID <= 0 || customerTenantID <= 0 {
		return fmt.Errorf("invalid revoke scope: user=%d tenant=%d", mspUserID, customerTenantID)
	}
	scopeCtx := tenantctx.WithTenantID(ctx, customerTenantID)
	live, err := s.client.UserTenantMembership.Query().
		Where(
			usertenantmembership.UserID(mspUserID),
			usertenantmembership.TenantID(customerTenantID),
			usertenantmembership.SourceEQ(usertenantmembership.SourceAllocation),
			usertenantmembership.DeletedAtIsNil(),
		).
		Only(scopeCtx)
	if err != nil {
		if ent.IsNotFound(err) {
			return nil
		}
		return fmt.Errorf("load allocation membership user=%d tenant=%d: %w", mspUserID, customerTenantID, err)
	}
	now := time.Now()
	saved, err := live.Update().
		SetDeassignedAt(now).
		SetDeletedAt(now).
		Save(scopeCtx)
	if err != nil {
		return fmt.Errorf("revoke allocation membership user=%d tenant=%d: %w", mspUserID, customerTenantID, err)
	}
	s.writeSyncAudit(scopeCtx, meta, MembershipAuditActionRevoke, "revoked", saved, "")
	return nil
}

// liveMembership 读取 (user, tenant) 的存活成员行（deleted_at IS NULL）。
func (s *MembershipSyncService) liveMembership(ctx context.Context, userID, tenantID int) (*ent.UserTenantMembership, error) {
	live, err := s.client.UserTenantMembership.Query().
		Where(
			usertenantmembership.UserID(userID),
			usertenantmembership.TenantID(tenantID),
			usertenantmembership.DeletedAtIsNil(),
		).
		Only(ctx)
	if err != nil {
		if ent.IsNotFound(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("load live membership user=%d tenant=%d: %w", userID, tenantID, err)
	}
	return live, nil
}

// resolveRoleID 按租户 + code 解析角色行 ID；未命中返回 (0,false)（由调用方登记巡检）。
func (s *MembershipSyncService) resolveRoleID(ctx context.Context, tenantID int, code string) (int, bool) {
	code = strings.ToLower(strings.TrimSpace(code))
	if code == "" {
		return 0, false
	}
	r, err := s.client.Role.Query().
		Where(role.TenantIDEQ(tenantID), role.CodeEQ(code)).
		Order(ent.Asc(role.FieldID)).
		First(ctx)
	if err != nil {
		if !ent.IsNotFound(err) {
			s.logger.Warnw("membership sync: role lookup failed",
				"tenant_id", tenantID, "role_code", code, "error", err)
		}
		return 0, false
	}
	return r.ID, true
}

// accountKindForHome 账号类型快照（迁移 20260504 回填 1 同口径，不按目标租户类型倒置）。
func (s *MembershipSyncService) accountKindForHome(ctx context.Context, tenantID int, mspRole string) (usertenantmembership.AccountKind, error) {
	t, err := s.client.Tenant.Query().Where(tenant.IDEQ(tenantID)).Only(ctx)
	if err != nil {
		return "", fmt.Errorf("load tenant %d for account_kind: %w", tenantID, err)
	}
	switch {
	case tenantmode.IsMSPProviderTenantType(string(t.Type)) || isProviderMSPRole(mspRole):
		return usertenantmembership.AccountKindProvider, nil
	case tenantmode.IsCustomerTenantType(string(t.Type)):
		return usertenantmembership.AccountKindCustomer, nil
	default:
		return usertenantmembership.AccountKindPlatform, nil
	}
}

// writeSyncAudit 写 membership 同步审计（resource=membership）。
// 审计行归属 meta.RowTenantID（默认作用域租户），target_tenant_id=作用域租户；
// 写前显式重绑 ctx 租户，满足写路径租户守卫 fail-closed 口径。
func (s *MembershipSyncService) writeSyncAudit(
	ctx context.Context,
	meta MembershipAuditMeta,
	action, outcome string,
	m *ent.UserTenantMembership,
	roleCode string,
) {
	if s == nil || s.client == nil || m == nil {
		return
	}
	rowTenant := meta.RowTenantID
	if rowTenant <= 0 {
		rowTenant = m.TenantID
	}
	source := meta.Source
	if strings.TrimSpace(source) == "" {
		source = middleware.AuditSourceSystem
	}
	payload, _ := json.Marshal(map[string]any{
		"outcome":           outcome,
		"trigger":           meta.Trigger,
		"scope_tenant_id":   m.TenantID,
		"membership_id":     m.ID,
		"membership_source": string(m.Source),
		"role_code":         roleCode,
		"msp_role":          derefString(m.MspRole),
		"allocation_id":     derefInt(m.AllocationID),
		"account_kind":      string(m.AccountKind),
	})
	auditCtx := tenantctx.WithTenantID(ctx, rowTenant)
	auditCtx, cancel := context.WithTimeout(auditCtx, 2*time.Second)
	defer cancel()
	if _, err := s.client.AuditLog.Create().
		SetCreatedAt(time.Now()).
		SetTenantID(rowTenant).
		SetUserID(meta.ActorUserID).
		SetActorAccount(meta.ActorAccount).
		SetTargetTenantID(m.TenantID).
		SetSource(source).
		SetResource("membership").
		SetAction(action).
		SetPath("service:membership_sync").
		SetMethod("INTERNAL").
		SetStatusCode(200).
		SetRequestBody(string(payload)).
		Save(auditCtx); err != nil {
		s.logger.Warnw("membership sync audit write failed",
			"error", err, "action", action, "scope_tenant_id", m.TenantID, "membership_id", m.ID)
	}
}

// allocationMembershipRoles 分配角色 → membership 词表（20260504 回填 2 预设）。
func allocationMembershipRoles(allocRole string) (roleCode, mspRole string) {
	if strings.EqualFold(strings.TrimSpace(allocRole), "specialist") {
		return "msp_specialist", "msp_specialist"
	}
	return "msp_tech", "msp_tech"
}

// isProviderMSPRole 服务方账号角色（users.msp_role 词表）。
func isProviderMSPRole(mspRole string) bool {
	switch strings.ToLower(strings.TrimSpace(mspRole)) {
	case "provider_admin", "provider_agent":
		return true
	}
	return false
}
