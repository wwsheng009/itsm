package service

import (
	"context"
	"errors"
	"fmt"
	"time"

	"itsm-backend/ent"
	"itsm-backend/ent/usertenantmembership"
	"itsm-backend/ent/usertenantmembershiporg"

	"go.uber.org/zap"
)

// =============================================================================
// IP-P1-3：组织成员关系迁 membership（user_tenant_membership_orgs 子表）
//
// 「组织归属以 membership 为唯一载体」：
//   - 写入：本服务校验 org.tenant_id == membership.tenant_id（应用层），
//     并由迁移 20260505 的复合 FK (membership_id, tenant_id) →
//     user_tenant_memberships(id, tenant_id) 在 DB 层二次拒绝跨租户配对；
//   - 多态：org_type ∈ {department,team,group,project}，指向四张组织表，
//     无法建单一物理 FK → 应用层校验 + guard 扫描（IP-P2-5）；
//   - 软删：detach = deleted_at 置位；重复挂接复活原行（unique 约束含历史行）。
// =============================================================================

var (
	// ErrMembershipOrgCrossTenant：组织与 membership 不同租户（必须拒绝）。
	ErrMembershipOrgCrossTenant = errors.New("membership org: organization and membership belong to different tenants")
	// ErrMembershipOrgNotFound：组织不存在（或 ID 无效）。
	ErrMembershipOrgNotFound = errors.New("membership org: organization not found")
	// ErrMembershipOrgTypeInvalid：org_type 不在冻结枚举内。
	ErrMembershipOrgTypeInvalid = errors.New("membership org: invalid org type")
	// ErrMembershipOrgMembershipInactive：membership 不存在/已软删/非 active。
	ErrMembershipOrgMembershipInactive = errors.New("membership org: membership not found or inactive")
)

// MembershipOrgService 组织归属写入/读取服务（IP-P1-3）。
type MembershipOrgService struct {
	client *ent.Client
	logger *zap.SugaredLogger
}

// NewMembershipOrgService 构造组织归属服务。
func NewMembershipOrgService(client *ent.Client, logger *zap.SugaredLogger) *MembershipOrgService {
	return &MembershipOrgService{client: client, logger: logger}
}

// validOrgTypes 冻结枚举（§4.0-A）。
var validOrgTypes = map[string]usertenantmembershiporg.OrgType{
	"department": usertenantmembershiporg.OrgTypeDepartment,
	"team":       usertenantmembershiporg.OrgTypeTeam,
	"group":      usertenantmembershiporg.OrgTypeGroup,
	"project":    usertenantmembershiporg.OrgTypeProject,
}

// orgTenantID 加载多态组织并返回其租户 ID；不存在 → ErrMembershipOrgNotFound。
func (s *MembershipOrgService) orgTenantID(ctx context.Context, orgType string, orgID int) (int, error) {
	switch orgType {
	case "department":
		entity, err := s.client.Department.Get(ctx, orgID)
		if err != nil {
			if ent.IsNotFound(err) {
				return 0, fmt.Errorf("%w: department %d", ErrMembershipOrgNotFound, orgID)
			}
			return 0, err
		}
		return entity.TenantID, nil
	case "team":
		entity, err := s.client.Team.Get(ctx, orgID)
		if err != nil {
			if ent.IsNotFound(err) {
				return 0, fmt.Errorf("%w: team %d", ErrMembershipOrgNotFound, orgID)
			}
			return 0, err
		}
		return entity.TenantID, nil
	case "group":
		entity, err := s.client.Group.Get(ctx, orgID)
		if err != nil {
			if ent.IsNotFound(err) {
				return 0, fmt.Errorf("%w: group %d", ErrMembershipOrgNotFound, orgID)
			}
			return 0, err
		}
		return entity.TenantID, nil
	case "project":
		entity, err := s.client.Project.Get(ctx, orgID)
		if err != nil {
			if ent.IsNotFound(err) {
				return 0, fmt.Errorf("%w: project %d", ErrMembershipOrgNotFound, orgID)
			}
			return 0, err
		}
		return entity.TenantID, nil
	default:
		return 0, fmt.Errorf("%w: %q", ErrMembershipOrgTypeInvalid, orgType)
	}
}

// Attach 将组织挂到 membership 上（同租户强校验；isPrimary 时同类型旧主组织自动降级）。
// 幂等：同一 (membership, org_type, org_id) 重复调用复用原行（含软删复活）。
func (s *MembershipOrgService) Attach(ctx context.Context, membershipID int, orgType string, orgID int64, isPrimary bool) (*ent.UserTenantMembershipOrg, error) {
	if _, ok := validOrgTypes[orgType]; !ok {
		return nil, fmt.Errorf("%w: %q", ErrMembershipOrgTypeInvalid, orgType)
	}
	if orgID <= 0 {
		return nil, fmt.Errorf("%w: org_id %d", ErrMembershipOrgNotFound, orgID)
	}

	membershipEntity, err := s.client.UserTenantMembership.Query().
		Where(
			usertenantmembership.IDEQ(membershipID),
			usertenantmembership.DeletedAtIsNil(),
			usertenantmembership.StatusEQ(usertenantmembership.StatusActive),
		).
		Only(ctx)
	if err != nil {
		if ent.IsNotFound(err) {
			return nil, fmt.Errorf("%w: membership %d", ErrMembershipOrgMembershipInactive, membershipID)
		}
		return nil, err
	}

	orgTenantID, err := s.orgTenantID(ctx, orgType, int(orgID))
	if err != nil {
		return nil, err
	}
	if orgTenantID != membershipEntity.TenantID {
		return nil, fmt.Errorf("%w: org tenant %d, membership tenant %d",
			ErrMembershipOrgCrossTenant, orgTenantID, membershipEntity.TenantID)
	}

	tx, err := s.client.Tx(ctx)
	if err != nil {
		return nil, err
	}
	defer func() {
		if err != nil {
			_ = tx.Rollback()
		}
	}()

	existing, err := tx.UserTenantMembershipOrg.Query().
		Where(
			usertenantmembershiporg.MembershipIDEQ(membershipID),
			usertenantmembershiporg.OrgTypeEQ(validOrgTypes[orgType]),
			usertenantmembershiporg.OrgIDEQ(orgID),
		).
		Only(ctx)
	if err != nil && !ent.IsNotFound(err) {
		return nil, err
	}

	// 主组织唯一：同 membership 同类型仅一个存活主组织（部分唯一索引 uq_membership_org_primary）。
	if isPrimary {
		unset := tx.UserTenantMembershipOrg.Update().
			Where(
				usertenantmembershiporg.MembershipIDEQ(membershipID),
				usertenantmembershiporg.OrgTypeEQ(validOrgTypes[orgType]),
				usertenantmembershiporg.DeletedAtIsNil(),
				usertenantmembershiporg.IsPrimaryEQ(true),
			)
		if existing != nil {
			unset = unset.Where(usertenantmembershiporg.IDNEQ(existing.ID))
		}
		if err = unset.SetIsPrimary(false).Exec(ctx); err != nil {
			return nil, err
		}
	}

	var result *ent.UserTenantMembershipOrg
	if existing != nil {
		update := tx.UserTenantMembershipOrg.UpdateOneID(existing.ID).
			SetStatus(usertenantmembershiporg.StatusActive).
			SetIsPrimary(isPrimary).
			ClearDeletedAt()
		result, err = update.Save(ctx)
	} else {
		result, err = tx.UserTenantMembershipOrg.Create().
			SetMembershipID(membershipID).
			SetTenantID(membershipEntity.TenantID).
			SetOrgType(validOrgTypes[orgType]).
			SetOrgID(orgID).
			SetIsPrimary(isPrimary).
			SetStatus(usertenantmembershiporg.StatusActive).
			Save(ctx)
	}
	if err != nil {
		return nil, err
	}
	if err = tx.Commit(); err != nil {
		return nil, err
	}

	if s.logger != nil {
		s.logger.Infow("membership org attached",
			"membership_id", membershipID, "tenant_id", membershipEntity.TenantID,
			"org_type", orgType, "org_id", orgID, "is_primary", isPrimary)
	}
	return result, nil
}

// Detach 软删组织归属（deleted_at 置位 + status=suspended）。返回是否存在存活行。
func (s *MembershipOrgService) Detach(ctx context.Context, membershipID int, orgType string, orgID int64) (bool, error) {
	if _, ok := validOrgTypes[orgType]; !ok {
		return false, fmt.Errorf("%w: %q", ErrMembershipOrgTypeInvalid, orgType)
	}
	affected, err := s.client.UserTenantMembershipOrg.Update().
		Where(
			usertenantmembershiporg.MembershipIDEQ(membershipID),
			usertenantmembershiporg.OrgTypeEQ(validOrgTypes[orgType]),
			usertenantmembershiporg.OrgIDEQ(orgID),
			usertenantmembershiporg.DeletedAtIsNil(),
		).
		SetDeletedAt(time.Now()).
		SetStatus(usertenantmembershiporg.StatusSuspended).
		Save(ctx)
	if err != nil {
		return false, err
	}
	return affected > 0, nil
}

// ListForMembership 返回 membership 下存活且未过期的组织归属（按类型/主组织优先排序）。
func (s *MembershipOrgService) ListForMembership(ctx context.Context, membershipID int) ([]*ent.UserTenantMembershipOrg, error) {
	now := time.Now()
	return s.client.UserTenantMembershipOrg.Query().
		Where(
			usertenantmembershiporg.MembershipIDEQ(membershipID),
			usertenantmembershiporg.DeletedAtIsNil(),
			usertenantmembershiporg.StatusEQ(usertenantmembershiporg.StatusActive),
			usertenantmembershiporg.Or(
				usertenantmembershiporg.ExpiresAtIsNil(),
				usertenantmembershiporg.ExpiresAtGT(now),
			),
		).
		Order(ent.Asc(usertenantmembershiporg.FieldOrgType), ent.Desc(usertenantmembershiporg.FieldIsPrimary)).
		All(ctx)
}
