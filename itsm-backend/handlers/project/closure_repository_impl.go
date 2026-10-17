package project

import (
	"context"
	"time"

	"itsm-backend/ent"
	"itsm-backend/ent/projectclosure"
	"itsm-backend/ent/projectmember"
)

// ClosurePatch 结项清单更新补丁（指针 nil = 不修改；PE-15）。
type ClosurePatch struct {
	RetroKbArticleID  *int
	RetroGeneratedBy  *int
	RetroGeneratedAt  *time.Time
	AssetsConfirmedBy *int
	AssetsConfirmedAt *time.Time
	AssetsNote        *string
	AssetsWaivedBy    *int
	AssetsWaivedAt    *time.Time
	AssetsWaiveReason *string
	MembersRecycledBy *int
	MembersRecycledAt *time.Time
	MembersCount      *int
	CSATSurveyID      *int
	CSATTriggeredAt   *time.Time
	// ClearAssetsWaived 豁免 → 确认时清除豁免留痕（ClearAssetsConfirmed 反向）。
	ClearAssetsWaived    bool
	ClearAssetsConfirmed bool
}

// GetClosure 取结项清单（无记录返回 ent.IsNotFound）。
func (r *EntRepository) GetClosure(ctx context.Context, tenantID, projectID int) (*ent.ProjectClosure, error) {
	return r.client.ProjectClosure.Query().
		Where(projectclosure.TenantIDEQ(tenantID), projectclosure.ProjectIDEQ(projectID)).
		Only(ctx)
}

// CreateClosure 创建结项清单（一项目一行；唯一索引冲突由调用方回读处理）。
func (r *EntRepository) CreateClosure(ctx context.Context, tenantID, projectID, createdBy int) (*ent.ProjectClosure, error) {
	return r.client.ProjectClosure.Create().
		SetTenantID(tenantID).
		SetProjectID(projectID).
		SetCreatedBy(createdBy).
		Save(ctx)
}

// UpdateClosureCAS 结项清单更新（version 乐观锁；冲突/不存在返回 CASConflictError）。
func (r *EntRepository) UpdateClosureCAS(
	ctx context.Context, tenantID, id, version int, p ClosurePatch,
) (*ent.ProjectClosure, error) {
	update := r.client.ProjectClosure.Update().
		Where(projectclosure.IDEQ(id), projectclosure.TenantIDEQ(tenantID), projectclosure.VersionEQ(version))
	if p.ClearAssetsWaived {
		update = update.
			ClearAssetsWaivedAt().
			ClearAssetsWaivedBy().
			SetAssetsWaiveReason("")
	}
	if p.ClearAssetsConfirmed {
		update = update.
			ClearAssetsConfirmedAt().
			ClearAssetsConfirmedBy().
			SetAssetsNote("")
	}
	if p.RetroKbArticleID != nil {
		update = update.SetRetroKBArticleID(*p.RetroKbArticleID)
	}
	if p.RetroGeneratedBy != nil {
		update = update.SetRetroGeneratedBy(*p.RetroGeneratedBy)
	}
	if p.RetroGeneratedAt != nil {
		update = update.SetRetroGeneratedAt(*p.RetroGeneratedAt)
	}
	if p.AssetsConfirmedBy != nil {
		update = update.SetAssetsConfirmedBy(*p.AssetsConfirmedBy)
	}
	if p.AssetsConfirmedAt != nil {
		update = update.SetAssetsConfirmedAt(*p.AssetsConfirmedAt)
	}
	if p.AssetsNote != nil {
		update = update.SetAssetsNote(*p.AssetsNote)
	}
	if p.AssetsWaivedBy != nil {
		update = update.SetAssetsWaivedBy(*p.AssetsWaivedBy)
	}
	if p.AssetsWaivedAt != nil {
		update = update.SetAssetsWaivedAt(*p.AssetsWaivedAt)
	}
	if p.AssetsWaiveReason != nil {
		update = update.SetAssetsWaiveReason(*p.AssetsWaiveReason)
	}
	if p.MembersRecycledBy != nil {
		update = update.SetMembersRecycledBy(*p.MembersRecycledBy)
	}
	if p.MembersRecycledAt != nil {
		update = update.SetMembersRecycledAt(*p.MembersRecycledAt)
	}
	if p.MembersCount != nil {
		update = update.SetMembersRecycledCount(*p.MembersCount)
	}
	if p.CSATSurveyID != nil {
		update = update.SetCsatSurveyID(*p.CSATSurveyID)
	}
	if p.CSATTriggeredAt != nil {
		update = update.SetCsatTriggeredAt(*p.CSATTriggeredAt)
	}
	updated, err := update.AddVersion(1).Save(ctx)
	if err != nil {
		return nil, err
	}
	if updated == 0 {
		current, getErr := r.client.ProjectClosure.Query().
			Where(projectclosure.IDEQ(id), projectclosure.TenantIDEQ(tenantID)).
			Only(ctx)
		if getErr != nil {
			return nil, getErr
		}
		return nil, &CASConflictError{CurrentVersion: current.Version}
	}
	return r.client.ProjectClosure.Get(ctx, id)
}

// RecycleProjectMembers 成员回收：将项目内活跃的 member 角色成员置为 removed，
// 返回回收数量（pm/stakeholder 保留——结项后查阅与 CSAT 接收方，见 D-19）。
// managerID > 0 时排除项目经理（防御性：经理行若有 member 角色也不回收）。
func (r *EntRepository) RecycleProjectMembers(ctx context.Context, tenantID, projectID, managerID int) (int, error) {
	update := r.client.ProjectMember.Update().
		Where(
			projectmember.TenantIDEQ(tenantID),
			projectmember.ProjectIDEQ(projectID),
			projectmember.StatusEQ("active"),
			projectmember.ProjectRoleEQ("member"),
		)
	if managerID > 0 {
		update = update.Where(projectmember.UserIDNEQ(managerID))
	}
	return update.SetStatus("removed").Save(ctx)
}