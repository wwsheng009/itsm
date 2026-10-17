package project

import (
	"context"
	"time"

	"itsm-backend/ent"
	"itsm-backend/ent/asset"
	"itsm-backend/ent/assetlicense"
	"itsm-backend/ent/contract"
	"itsm-backend/ent/projectassetlink"
	"itsm-backend/ent/vendor"
)

// 采购/资产结项登记仓储（P2，PE-26）。
//
// 权威定义：docs/project-management/plan/p2-procurement-design.md §3/§4。
// 说明：target_id 为同租户弱引用（D-50）——四域存在性/标签/候选查询均强制 tenant_id 过滤；
// 未删行唯一由迁移 partial unique index 兜底（SQLite 测试库以服务层查重为准）。

// ListAssetLinks 项目登记列表（未删行；最新在前；limit 为硬上限 +1 的截断探测口径）。
func (r *EntRepository) ListAssetLinks(
	ctx context.Context, tenantID, projectID, limit int,
) ([]*ent.ProjectAssetLink, error) {
	return r.client.ProjectAssetLink.Query().
		Where(
			projectassetlink.TenantIDEQ(tenantID),
			projectassetlink.ProjectIDEQ(projectID),
			projectassetlink.DeletedAtIsNil(),
		).
		Order(ent.Desc(projectassetlink.FieldID)).
		Limit(limit).
		All(ctx)
}

// GetAssetLink 取单条登记（未删行；不存在返回 ent.IsNotFound）。
func (r *EntRepository) GetAssetLink(
	ctx context.Context, tenantID, projectID, id int,
) (*ent.ProjectAssetLink, error) {
	return r.client.ProjectAssetLink.Query().
		Where(
			projectassetlink.IDEQ(id),
			projectassetlink.TenantIDEQ(tenantID),
			projectassetlink.ProjectIDEQ(projectID),
			projectassetlink.DeletedAtIsNil(),
		).
		Only(ctx)
}

// CreateAssetLink 新增登记（重复由服务层查重；PostgreSQL 唯一索引冲突由错误冒泡兜底）。
func (r *EntRepository) CreateAssetLink(
	ctx context.Context, tenantID, projectID, actorID int, linkType string, targetID int, note string,
) (*ent.ProjectAssetLink, error) {
	return r.client.ProjectAssetLink.Create().
		SetTenantID(tenantID).
		SetProjectID(projectID).
		SetLinkType(linkType).
		SetTargetID(targetID).
		SetNote(note).
		SetCreatedBy(actorID).
		Save(ctx)
}

// SoftDeleteAssetLink 软删未删行（返回影响行数；0 = 不存在/已删）。
func (r *EntRepository) SoftDeleteAssetLink(
	ctx context.Context, tenantID, projectID, id, actorID int, now time.Time,
) (int, error) {
	return r.client.ProjectAssetLink.Update().
		Where(
			projectassetlink.IDEQ(id),
			projectassetlink.TenantIDEQ(tenantID),
			projectassetlink.ProjectIDEQ(projectID),
			projectassetlink.DeletedAtIsNil(),
		).
		SetDeletedAt(now).
		SetDeletedBy(actorID).
		Save(ctx)
}

// FindActiveAssetLink 查未删行（同项目同类型同目标；用于重复登记 409 判定）。
func (r *EntRepository) FindActiveAssetLink(
	ctx context.Context, tenantID, projectID int, linkType string, targetID int,
) (*ent.ProjectAssetLink, error) {
	return r.client.ProjectAssetLink.Query().
		Where(
			projectassetlink.TenantIDEQ(tenantID),
			projectassetlink.ProjectIDEQ(projectID),
			projectassetlink.LinkTypeEQ(linkType),
			projectassetlink.TargetIDEQ(targetID),
			projectassetlink.DeletedAtIsNil(),
		).
		Only(ctx)
}

// AssetLinkTargetExists 目标存在性（同租户；D-50 弱引用校验）。
func (r *EntRepository) AssetLinkTargetExists(
	ctx context.Context, tenantID int, linkType string, targetID int,
) (bool, error) {
	switch linkType {
	case AssetLinkTypeAsset:
		return r.client.Asset.Query().
			Where(asset.TenantIDEQ(tenantID), asset.IDEQ(targetID)).Exist(ctx)
	case AssetLinkTypeAssetLicense:
		return r.client.AssetLicense.Query().
			Where(assetlicense.TenantIDEQ(tenantID), assetlicense.IDEQ(targetID)).Exist(ctx)
	case AssetLinkTypeVendor:
		return r.client.Vendor.Query().
			Where(vendor.TenantIDEQ(tenantID), vendor.IDEQ(targetID)).Exist(ctx)
	case AssetLinkTypeContract:
		return r.client.Contract.Query().
			Where(contract.TenantIDEQ(tenantID), contract.IDEQ(targetID)).Exist(ctx)
	default:
		return false, nil
	}
}

// AssetLinkTargetInfos 批量富化目标标签（同租户；缺失的 id 不出现在结果 map）。
func (r *EntRepository) AssetLinkTargetInfos(
	ctx context.Context, tenantID int, linkType string, ids []int,
) (map[int]AssetLinkTargetInfo, error) {
	infos := make(map[int]AssetLinkTargetInfo, len(ids))
	if len(ids) == 0 {
		return infos, nil
	}
	switch linkType {
	case AssetLinkTypeAsset:
		rows, err := r.client.Asset.Query().
			Where(asset.TenantIDEQ(tenantID), asset.IDIn(ids...)).All(ctx)
		if err != nil {
			return nil, err
		}
		for _, row := range rows {
			infos[row.ID] = AssetLinkTargetInfo{Label: row.AssetNumber, Sublabel: joinLabel(row.Name, row.Status)}
		}
	case AssetLinkTypeAssetLicense:
		rows, err := r.client.AssetLicense.Query().
			Where(assetlicense.TenantIDEQ(tenantID), assetlicense.IDIn(ids...)).All(ctx)
		if err != nil {
			return nil, err
		}
		for _, row := range rows {
			infos[row.ID] = AssetLinkTargetInfo{Label: row.Name, Sublabel: row.LicenseType}
		}
	case AssetLinkTypeVendor:
		rows, err := r.client.Vendor.Query().
			Where(vendor.TenantIDEQ(tenantID), vendor.IDIn(ids...)).All(ctx)
		if err != nil {
			return nil, err
		}
		for _, row := range rows {
			infos[row.ID] = AssetLinkTargetInfo{Label: row.Name, Sublabel: row.Code}
		}
	case AssetLinkTypeContract:
		rows, err := r.client.Contract.Query().
			Where(contract.TenantIDEQ(tenantID), contract.IDIn(ids...)).All(ctx)
		if err != nil {
			return nil, err
		}
		for _, row := range rows {
			infos[row.ID] = AssetLinkTargetInfo{Label: row.ContractNumber, Sublabel: row.Title}
		}
	}
	return infos, nil
}

// SearchAssetLinkTargets 候选查询（同租户 + 关键词 ContainsFold + limit+1 截断探测；D-52 最小字段）。
func (r *EntRepository) SearchAssetLinkTargets(
	ctx context.Context, tenantID int, linkType, keyword string, limit int,
) ([]AssetLinkCandidate, bool, error) {
	items := make([]AssetLinkCandidate, 0, limit)
	probe := limit + 1

	switch linkType {
	case AssetLinkTypeAsset:
		query := r.client.Asset.Query().Where(asset.TenantIDEQ(tenantID))
		if keyword != "" {
			query = query.Where(asset.Or(
				asset.AssetNumberContainsFold(keyword),
				asset.NameContainsFold(keyword),
			))
		}
		rows, err := query.Order(ent.Desc(asset.FieldID)).Limit(probe).All(ctx)
		if err != nil {
			return nil, false, err
		}
		for _, row := range rows {
			items = append(items, AssetLinkCandidate{ID: row.ID, Label: row.AssetNumber, Sublabel: joinLabel(row.Name, row.Status)})
		}
	case AssetLinkTypeAssetLicense:
		query := r.client.AssetLicense.Query().Where(assetlicense.TenantIDEQ(tenantID))
		if keyword != "" {
			query = query.Where(assetlicense.NameContainsFold(keyword))
		}
		rows, err := query.Order(ent.Desc(assetlicense.FieldID)).Limit(probe).All(ctx)
		if err != nil {
			return nil, false, err
		}
		for _, row := range rows {
			items = append(items, AssetLinkCandidate{ID: row.ID, Label: row.Name, Sublabel: row.LicenseType})
		}
	case AssetLinkTypeVendor:
		query := r.client.Vendor.Query().Where(vendor.TenantIDEQ(tenantID))
		if keyword != "" {
			query = query.Where(vendor.Or(
				vendor.NameContainsFold(keyword),
				vendor.CodeContainsFold(keyword),
			))
		}
		rows, err := query.Order(ent.Desc(vendor.FieldID)).Limit(probe).All(ctx)
		if err != nil {
			return nil, false, err
		}
		for _, row := range rows {
			items = append(items, AssetLinkCandidate{ID: row.ID, Label: row.Name, Sublabel: row.Code})
		}
	case AssetLinkTypeContract:
		query := r.client.Contract.Query().Where(contract.TenantIDEQ(tenantID))
		if keyword != "" {
			query = query.Where(contract.Or(
				contract.ContractNumberContainsFold(keyword),
				contract.TitleContainsFold(keyword),
			))
		}
		rows, err := query.Order(ent.Desc(contract.FieldID)).Limit(probe).All(ctx)
		if err != nil {
			return nil, false, err
		}
		for _, row := range rows {
			items = append(items, AssetLinkCandidate{ID: row.ID, Label: row.ContractNumber, Sublabel: row.Title})
		}
	}

	truncated := len(items) > limit
	if truncated {
		items = items[:limit]
	}
	return items, truncated, nil
}

// joinLabel 组合「主 + 辅」标签（空段跳过）。
func joinLabel(parts ...string) string {
	out := ""
	for _, p := range parts {
		if p == "" {
			continue
		}
		if out != "" {
			out += " / "
		}
		out += p
	}
	return out
}