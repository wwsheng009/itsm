package project

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"itsm-backend/ent"
)

// 采购/资产结项登记服务（P2，PE-26）。
//
// 权威定义：docs/project-management/plan/p2-procurement-design.md。
// 职责：登记列表/新增/删除/候选选择器；可见性同项目详情（不可见 404，D-53）；
// cancelled 项目拒绝写；completed 允许补登记（结项后采购/发票后到的真实场景）。

// 登记域哨兵错误（PE-26）。
var (
	// ErrAssetLinkInvalidType 链接类型不在四词表内。
	ErrAssetLinkInvalidType = errors.New("invalid asset link type")
	// ErrAssetLinkInvalidTarget 目标不存在或跨租户（弱引用校验失败）。
	ErrAssetLinkInvalidTarget = errors.New("asset link target not found in tenant")
	// ErrAssetLinkDuplicate 同项目同类型同目标已登记（未删行唯一）。
	ErrAssetLinkDuplicate = errors.New("asset link already exists")
	// ErrAssetLinkNotFound 登记不存在（或已删除）。
	ErrAssetLinkNotFound = errors.New("project asset link not found")
)

// ListAssetLinks 登记列表（可见性谓词；不可见 404）。
func (s *Service) ListAssetLinks(
	ctx context.Context, tenantID, projectID, viewerID int, admin bool,
) (*AssetLinkListResponse, error) {
	if _, err := s.GetForViewer(ctx, tenantID, projectID, viewerID, admin); err != nil {
		return nil, err
	}
	return s.buildAssetLinkList(ctx, tenantID, projectID)
}

// CreateAssetLink 新增登记（D-50 同租户存在性校验 + 重复 409；D-53 写权限与终态）。
func (s *Service) CreateAssetLink(
	ctx context.Context, tenantID, projectID, actorID int, admin bool, req *AssetLinkCreateRequest,
) (*AssetLinkListResponse, error) {
	project, err := s.GetForViewer(ctx, tenantID, projectID, actorID, admin)
	if err != nil {
		return nil, err
	}
	if project.Status == StatusCancelled {
		return nil, fmt.Errorf("%w: 已取消项目不可登记结项资产", ErrValidation)
	}
	req.Normalize()
	if !validAssetLinkTypes[req.LinkType] {
		return nil, fmt.Errorf("%w: %s（可选 asset|asset_license|vendor|contract）", ErrAssetLinkInvalidType, req.LinkType)
	}
	if req.TargetID <= 0 {
		return nil, fmt.Errorf("%w: targetId 必须为正整数", ErrValidation)
	}
	if len([]rune(req.Note)) > assetLinkNoteMaxRunes {
		return nil, fmt.Errorf("%w: 备注长度超过 %d 字符", ErrValidation, assetLinkNoteMaxRunes)
	}

	// 重复判定（未删行唯一；SQLite 测试库无 partial unique，服务层查重为准）。
	if _, err := s.repo.FindActiveAssetLink(ctx, tenantID, projectID, req.LinkType, req.TargetID); err == nil {
		return nil, fmt.Errorf("%w: %s#%d", ErrAssetLinkDuplicate, req.LinkType, req.TargetID)
	} else if !ent.IsNotFound(err) {
		return nil, err
	}

	exists, err := s.repo.AssetLinkTargetExists(ctx, tenantID, req.LinkType, req.TargetID)
	if err != nil {
		return nil, err
	}
	if !exists {
		return nil, fmt.Errorf("%w: %s#%d", ErrAssetLinkInvalidTarget, req.LinkType, req.TargetID)
	}

	if _, err := s.repo.CreateAssetLink(
		ctx, tenantID, projectID, actorID, req.LinkType, req.TargetID, req.Note,
	); err != nil {
		return nil, err
	}
	return s.buildAssetLinkList(ctx, tenantID, projectID)
}

// DeleteAssetLink 软删登记（返回删除后的列表，便于前端一次刷新）。
func (s *Service) DeleteAssetLink(
	ctx context.Context, tenantID, projectID, linkID, actorID int, admin bool,
) (*AssetLinkListResponse, error) {
	project, err := s.GetForViewer(ctx, tenantID, projectID, actorID, admin)
	if err != nil {
		return nil, err
	}
	if project.Status == StatusCancelled {
		return nil, fmt.Errorf("%w: 已取消项目不可调整结项资产登记", ErrValidation)
	}
	if _, err := s.repo.GetAssetLink(ctx, tenantID, projectID, linkID); err != nil {
		if ent.IsNotFound(err) {
			return nil, fmt.Errorf("%w: %d", ErrAssetLinkNotFound, linkID)
		}
		return nil, err
	}
	rows, err := s.repo.SoftDeleteAssetLink(ctx, tenantID, projectID, linkID, actorID, s.now())
	if err != nil {
		return nil, err
	}
	if rows == 0 {
		return nil, fmt.Errorf("%w: %d", ErrAssetLinkNotFound, linkID)
	}
	return s.buildAssetLinkList(ctx, tenantID, projectID)
}

// ListAssetLinkCandidates 候选选择器（D-52；type 四词表 + 关键词 + 限额）。
func (s *Service) ListAssetLinkCandidates(
	ctx context.Context, tenantID, projectID, viewerID int, admin bool,
	linkType, keyword string, limit int,
) (*AssetLinkCandidateListResponse, error) {
	if _, err := s.GetForViewer(ctx, tenantID, projectID, viewerID, admin); err != nil {
		return nil, err
	}
	linkType = strings.ToLower(strings.TrimSpace(linkType))
	if !validAssetLinkTypes[linkType] {
		return nil, fmt.Errorf("%w: %s（可选 asset|asset_license|vendor|contract）", ErrAssetLinkInvalidType, linkType)
	}
	keyword = strings.TrimSpace(keyword)
	if limit <= 0 {
		limit = assetLinkCandidatesDefaultLimit
	}
	if limit > assetLinkCandidatesMaxLimit {
		limit = assetLinkCandidatesMaxLimit
	}
	items, truncated, err := s.repo.SearchAssetLinkTargets(ctx, tenantID, linkType, keyword, limit)
	if err != nil {
		return nil, err
	}
	if items == nil {
		items = []AssetLinkCandidate{}
	}
	return &AssetLinkCandidateListResponse{Items: items, Truncated: truncated}, nil
}

// buildAssetLinkList 组装登记列表（硬上限 200 + 显式 truncated；按类型批量富化标签，避免 N+1）。
func (s *Service) buildAssetLinkList(
	ctx context.Context, tenantID, projectID int,
) (*AssetLinkListResponse, error) {
	rows, err := s.repo.ListAssetLinks(ctx, tenantID, projectID, assetLinkMaxItems+1)
	if err != nil {
		return nil, err
	}
	truncated := len(rows) > assetLinkMaxItems
	if truncated {
		rows = rows[:assetLinkMaxItems]
	}

	idsByType := make(map[string][]int, len(assetLinkTypeOrder))
	for _, row := range rows {
		idsByType[row.LinkType] = append(idsByType[row.LinkType], row.TargetID)
	}
	infosByType := make(map[string]map[int]AssetLinkTargetInfo, len(idsByType))
	for linkType, ids := range idsByType {
		infos, err := s.repo.AssetLinkTargetInfos(ctx, tenantID, linkType, ids)
		if err != nil {
			return nil, err
		}
		infosByType[linkType] = infos
	}

	resp := &AssetLinkListResponse{
		Items:     make([]AssetLinkItem, 0, len(rows)),
		Summary:   newAssetLinkSummary(),
		Total:     len(rows),
		Truncated: truncated,
	}
	for _, row := range rows {
		info, ok := infosByType[row.LinkType][row.TargetID]
		resp.Items = append(resp.Items, AssetLinkItem{
			ID:            row.ID,
			LinkType:      row.LinkType,
			TargetID:      row.TargetID,
			Label:         info.Label,
			Sublabel:      info.Sublabel,
			Note:          row.Note,
			TargetMissing: !ok,
			CreatedBy:     row.CreatedBy,
			CreatedAt:     row.CreatedAt,
		})
		if _, known := resp.Summary[row.LinkType]; known {
			resp.Summary[row.LinkType]++
		}
	}
	return resp, nil
}

// assetLinkSummaryText 清单项 detail 文案（如「已登记 3 条（asset×2, asset_license×1）」；0 条返回空）。
func assetLinkSummaryText(summary map[string]int) string {
	total := 0
	parts := make([]string, 0, len(assetLinkTypeOrder))
	for _, t := range assetLinkTypeOrder {
		if n := summary[t]; n > 0 {
			total += n
			parts = append(parts, fmt.Sprintf("%s×%d", t, n))
		}
	}
	if total == 0 {
		return ""
	}
	return fmt.Sprintf("已登记 %d 条（%s）", total, strings.Join(parts, ", "))
}