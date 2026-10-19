package service

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"itsm-backend/common/tenantctx"
	"itsm-backend/dto"
	"itsm-backend/ent"
	"itsm-backend/middleware"

	"go.uber.org/zap"
)

// P2 MSP 多租户帮助空间与分发授权。
//
// 语义（对齐 workflow_template_distributions / ticket_type_distributions 先例）：
//   - msp_help_docs = provider 自有帮助空间（draft → publish 门禁；仅 published 可分发）；
//   - 分发 = 在客户租户落 copy-in 快照（title/page_key/body_md）+ 来源追踪（source_doc_id/slug/version）；
//   - 授权 = mspguard（allocation + 客户 active）唯一入口；写入按客户租户重绑定 ctx（RLS 同源，零跨租户读）；
//   - 升级 = "提示不强制"：列表返回 upgradeAvailable，显式 upgrade 原位刷新快照到最新已发布版本；
//   - 解绑 = 副本转客户原生内容（保留可见，不再追踪 provider 更新）；
//   - 客户侧消费（帮助面板）= 读取本租户 active/unbound 行（普通租户行，无 MSP 上下文）。

var (
	// ErrMSPHelpDocNotFound 目标文档/分发不存在（同租户口径；不区分"无权"）。
	ErrMSPHelpDocNotFound = errors.New("msp help doc not found")
	// ErrMSPHelpDocConflict 状态冲突（slug 重复 / active 分发已存在 / 无可用升级 / 已解绑）。
	ErrMSPHelpDocConflict = errors.New("msp help doc conflict")
	// ErrMSPHelpDocNotPublished 仅已发布文档可分发/升级（对齐模板分发门禁）。
	ErrMSPHelpDocNotPublished = errors.New("msp help doc not published")
	// ErrMSPHelpDocInvalid 参数不合法。
	ErrMSPHelpDocInvalid = errors.New("msp help doc invalid")
)

const (
	mspHelpDocMaxBodyBytes     = 200 * 1024
	mspHelpSearchDefaultLimit  = 5
	mspHelpSearchMaxLimit      = 20
	mspHelpSnippetRunes        = 120
	mspHelpDocColumns          = "id,tenant_id,slug,title,page_key,body_md,status,version,created_by,updated_by,created_at,updated_at,published_at"
	mspHelpDistributionColumns = "id,tenant_id,provider_tenant_id,source_doc_id,source_slug,source_version,title,page_key,status,distributed_by,distributed_at,unbound_by,unbound_at"
)

// MSPHelpSpaceService 帮助空间与分发服务（provider 写通道 + 客户侧只读消费）。
type MSPHelpSpaceService struct {
	db     *sql.DB
	guard  customerAccessChecker
	client *ent.Client
	logger *zap.SugaredLogger
}

// NewMSPHelpSpaceService 构造帮助空间服务（guard 为 mspguard 唯一授权入口）。
func NewMSPHelpSpaceService(db *sql.DB, guard customerAccessChecker, client *ent.Client, logger *zap.SugaredLogger) *MSPHelpSpaceService {
	return &MSPHelpSpaceService{db: db, guard: guard, client: client, logger: logger}
}

// CustomerHelpDoc 是客户租户侧可见的分发帮助文档（帮助面板消费）。
type CustomerHelpDoc struct {
	Title            string
	PageKey          string
	BodyMD           string
	SourceSlug       string
	SourceVersion    int
	ProviderTenantID int
	// Status = active（跟随 provider）/ unbound（已解绑，转为客户原生内容）。
	Status string
	// Snippet 仅检索路径填充（命中片段）；页面帮助路径为空。
	Snippet string
}

// ===== provider 侧帮助空间（自有资产，msp:read / msp:write）=====

// ListDocs 列出 provider 帮助空间文档（更新时间倒序）。
func (s *MSPHelpSpaceService) ListDocs(ctx context.Context, actor MSPWorkbenchActor) (*dto.MSPHelpDocListResponse, error) {
	if s == nil || s.db == nil {
		return nil, fmt.Errorf("MSP 帮助空间服务未配置")
	}
	if actor.HomeTenantID <= 0 {
		return nil, fmt.Errorf("%w: 缺少 provider 租户", ErrMSPHelpDocInvalid)
	}
	rctx := tenantctx.WithTenantID(ctx, actor.HomeTenantID)
	rows, err := s.db.QueryContext(rctx, "SELECT "+mspHelpDocColumns+
		" FROM msp_help_docs WHERE tenant_id=$1 ORDER BY updated_at DESC, id DESC", actor.HomeTenantID)
	if err != nil {
		return nil, fmt.Errorf("查询帮助空间文档失败: %w", err)
	}
	defer func() { _ = rows.Close() }()

	items := make([]*dto.MSPHelpDoc, 0)
	for rows.Next() {
		doc, err := scanMSPHelpDoc(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, doc)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("读取帮助空间文档失败: %w", err)
	}
	return &dto.MSPHelpDocListResponse{Items: items, Total: len(items)}, nil
}

// CreateDoc 创建帮助空间草稿（slug 在 provider 内唯一）。
func (s *MSPHelpSpaceService) CreateDoc(ctx context.Context, actor MSPWorkbenchActor, req dto.MSPHelpDocRequest) (*dto.MSPHelpDoc, error) {
	if s == nil || s.db == nil {
		return nil, fmt.Errorf("MSP 帮助空间服务未配置")
	}
	req, err := normalizeMSPHelpDocInput(req)
	if err != nil {
		return nil, err
	}
	if actor.UserID <= 0 || actor.HomeTenantID <= 0 {
		return nil, fmt.Errorf("%w: 缺少操作者", ErrMSPHelpDocInvalid)
	}
	rctx := tenantctx.WithTenantID(ctx, actor.HomeTenantID)

	var exists bool
	if err := s.db.QueryRowContext(rctx,
		"SELECT EXISTS(SELECT 1 FROM msp_help_docs WHERE tenant_id=$1 AND slug=$2)",
		actor.HomeTenantID, req.Slug).Scan(&exists); err != nil {
		return nil, fmt.Errorf("检查帮助空间 slug 失败: %w", err)
	}
	if exists {
		return nil, ErrMSPHelpDocConflict
	}

	return s.upsertDoc(rctx, `INSERT INTO msp_help_docs
		(tenant_id,slug,title,page_key,body_md,status,version,created_by,updated_by,created_at,updated_at)
		VALUES ($1,$2,$3,$4,$5,'draft',1,$6,$6,NOW(),NOW()) RETURNING `+mspHelpDocColumns,
		actor.HomeTenantID, req.Slug, req.Title, req.PageKey, req.BodyMD, actor.UserID)
}

// UpdateDoc 更新文档；已发布文档编辑回到 draft 并推进版本（需重新发布才可再分发）。
func (s *MSPHelpSpaceService) UpdateDoc(ctx context.Context, actor MSPWorkbenchActor, id int, req dto.MSPHelpDocRequest) (*dto.MSPHelpDoc, error) {
	if s == nil || s.db == nil {
		return nil, fmt.Errorf("MSP 帮助空间服务未配置")
	}
	if id <= 0 {
		return nil, fmt.Errorf("%w: 文档 ID 不合法", ErrMSPHelpDocInvalid)
	}
	req.Title = strings.TrimSpace(req.Title)
	req.PageKey = strings.TrimSpace(req.PageKey)
	if req.Title == "" || len(req.Title) > 255 || len(req.PageKey) > 128 {
		return nil, fmt.Errorf("%w: title 必填且 pageKey 长度合规", ErrMSPHelpDocInvalid)
	}
	if len(req.BodyMD) > mspHelpDocMaxBodyBytes {
		return nil, fmt.Errorf("%w: 正文超长", ErrMSPHelpDocInvalid)
	}
	if actor.UserID <= 0 || actor.HomeTenantID <= 0 {
		return nil, fmt.Errorf("%w: 缺少操作者", ErrMSPHelpDocInvalid)
	}
	rctx := tenantctx.WithTenantID(ctx, actor.HomeTenantID)

	return s.upsertDoc(rctx, `UPDATE msp_help_docs SET
		title=$3,
		page_key=$4,
		body_md=$5,
		status=CASE WHEN status='published' THEN 'draft' ELSE status END,
		version=CASE WHEN status='published' THEN version+1 ELSE version END,
		updated_by=$6,
		updated_at=NOW()
		WHERE tenant_id=$1 AND id=$2 RETURNING `+mspHelpDocColumns,
		actor.HomeTenantID, id, req.Title, req.PageKey, req.BodyMD, actor.UserID)
}

// PublishDoc 发布草稿（仅 published 可分发）。
func (s *MSPHelpSpaceService) PublishDoc(ctx context.Context, actor MSPWorkbenchActor, id int) (*dto.MSPHelpDoc, error) {
	if s == nil || s.db == nil {
		return nil, fmt.Errorf("MSP 帮助空间服务未配置")
	}
	if id <= 0 || actor.UserID <= 0 || actor.HomeTenantID <= 0 {
		return nil, fmt.Errorf("%w: 参数不完整", ErrMSPHelpDocInvalid)
	}
	rctx := tenantctx.WithTenantID(ctx, actor.HomeTenantID)
	return s.upsertDoc(rctx, `UPDATE msp_help_docs SET
		status='published', published_at=NOW(), updated_by=$3, updated_at=NOW()
		WHERE tenant_id=$1 AND id=$2 RETURNING `+mspHelpDocColumns,
		actor.HomeTenantID, id, actor.UserID)
}

// DeleteDoc 删除草稿（已发布文档请先在其他入口下线；本接口仅允许草稿，避免误删已分发来源）。
func (s *MSPHelpSpaceService) DeleteDoc(ctx context.Context, actor MSPWorkbenchActor, id int) error {
	if s == nil || s.db == nil {
		return fmt.Errorf("MSP 帮助空间服务未配置")
	}
	if id <= 0 || actor.HomeTenantID <= 0 {
		return fmt.Errorf("%w: 参数不完整", ErrMSPHelpDocInvalid)
	}
	rctx := tenantctx.WithTenantID(ctx, actor.HomeTenantID)
	res, err := s.db.ExecContext(rctx,
		"DELETE FROM msp_help_docs WHERE tenant_id=$1 AND id=$2 AND status='draft'", actor.HomeTenantID, id)
	if err != nil {
		return fmt.Errorf("删除帮助空间草稿失败: %w", err)
	}
	if affected, err := res.RowsAffected(); err == nil && affected == 0 {
		// 不存在或非草稿：统一冲突语义（不区分，避免探测）。
		return ErrMSPHelpDocConflict
	}
	return nil
}

// ===== 分发授权（provider 对客户；msp_customer read/write ∩ mspguard）=====

// Distribute 将 provider 已发布文档分发到客户租户（copy-in 快照 + 来源追踪）。
func (s *MSPHelpSpaceService) Distribute(ctx context.Context, actor MSPWorkbenchActor, customerTenantID, docID int) (*dto.MSPHelpDistribution, error) {
	if s == nil || s.db == nil {
		return nil, fmt.Errorf("MSP 帮助空间服务未配置")
	}
	if actor.UserID <= 0 || actor.HomeTenantID <= 0 || customerTenantID <= 0 || docID <= 0 {
		return nil, fmt.Errorf("%w: 分发参数不完整", ErrMSPHelpDocInvalid)
	}
	if s.guard == nil {
		return nil, fmt.Errorf("帮助空间分发授权校验未配置")
	}
	// 授权唯一入口：allocation + 客户归属 + 客户 active（mspguard）。
	if err := s.guard.CanAccessCustomer(ctx, actor.UserID, customerTenantID); err != nil {
		return nil, err
	}

	src, err := s.loadPublishedDocByID(tenantctx.WithTenantID(ctx, actor.HomeTenantID), actor.HomeTenantID, docID)
	if err != nil {
		return nil, err
	}

	wctx := tenantctx.WithTenantID(ctx, customerTenantID)
	var activeExists bool
	if err := s.db.QueryRowContext(wctx,
		"SELECT EXISTS(SELECT 1 FROM msp_help_distributions WHERE tenant_id=$1 AND source_slug=$2 AND status='active')",
		customerTenantID, src.Slug).Scan(&activeExists); err != nil {
		return nil, fmt.Errorf("检查客户分发状态失败: %w", err)
	}
	if activeExists {
		// 已有 active 分发：要求先升级或解绑，避免双份副本（对齐模板分发"不覆盖未决内容"）。
		return nil, ErrMSPHelpDocConflict
	}

	var dist dto.MSPHelpDistribution
	if err := s.db.QueryRowContext(wctx, `INSERT INTO msp_help_distributions
		(tenant_id,provider_tenant_id,source_doc_id,source_slug,source_version,source_updated_at,title,page_key,body_md,status,distributed_by,distributed_at,updated_at)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,'active',$10,NOW(),NOW())
		RETURNING id, distributed_at`,
		customerTenantID, actor.HomeTenantID, src.ID, src.Slug, src.Version, src.UpdatedAt,
		src.Title, src.PageKey, src.BodyMD, actor.UserID).Scan(&dist.ID, &dist.DistributedAt); err != nil {
		return nil, fmt.Errorf("创建客户帮助副本失败: %w", err)
	}

	dist.CustomerTenantID = customerTenantID
	dist.ProviderTenantID = actor.HomeTenantID
	dist.SourceDocID = src.ID
	dist.SourceSlug = src.Slug
	dist.SourceVersion = src.Version
	dist.LatestProviderVersion = src.Version
	dist.Title = src.Title
	dist.PageKey = src.PageKey
	dist.Status = "active"
	dist.DistributedBy = actor.UserID

	s.recordHelpAudit(ctx, actor, customerTenantID, "distribute", src.Slug, src.Version, "success")
	return &dist, nil
}

// ListDistributions 客户维度的分发列表（provider 视角 + 升级提示，提示不强制）。
func (s *MSPHelpSpaceService) ListDistributions(ctx context.Context, actor MSPWorkbenchActor, customerTenantID int) (*dto.MSPHelpDistributionListResponse, error) {
	if s == nil || s.db == nil {
		return nil, fmt.Errorf("MSP 帮助空间服务未配置")
	}
	if actor.UserID <= 0 || actor.HomeTenantID <= 0 || customerTenantID <= 0 {
		return nil, fmt.Errorf("%w: 查询参数不完整", ErrMSPHelpDocInvalid)
	}
	if s.guard == nil {
		return nil, fmt.Errorf("帮助空间分发授权校验未配置")
	}
	if err := s.guard.CanAccessCustomer(ctx, actor.UserID, customerTenantID); err != nil {
		return nil, err
	}

	rctx := tenantctx.WithTenantID(ctx, customerTenantID)
	rows, err := s.db.QueryContext(rctx, "SELECT "+mspHelpDistributionColumns+
		" FROM msp_help_distributions WHERE tenant_id=$1 AND provider_tenant_id=$2"+
		" ORDER BY (status='active') DESC, distributed_at DESC, id DESC",
		customerTenantID, actor.HomeTenantID)
	if err != nil {
		return nil, fmt.Errorf("查询分发记录失败: %w", err)
	}
	defer func() { _ = rows.Close() }()

	items := make([]*dto.MSPHelpDistribution, 0)
	latestCache := map[int]int{}
	for rows.Next() {
		item, err := scanMSPHelpDistribution(rows)
		if err != nil {
			return nil, err
		}
		item.CustomerTenantID = customerTenantID
		item.ProviderTenantID = actor.HomeTenantID

		latest, ok := latestCache[item.SourceDocID]
		if !ok {
			var v int
			perr := s.db.QueryRowContext(tenantctx.WithTenantID(ctx, actor.HomeTenantID),
				"SELECT version FROM msp_help_docs WHERE tenant_id=$1 AND id=$2 AND status='published'",
				actor.HomeTenantID, item.SourceDocID).Scan(&v)
			if perr != nil && !errors.Is(perr, sql.ErrNoRows) {
				return nil, fmt.Errorf("读取 provider 最新版本失败: %w", perr)
			}
			latest = v
			latestCache[item.SourceDocID] = v
		}
		item.LatestProviderVersion = latest
		item.UpgradeAvailable = item.Status == "active" && latest > item.SourceVersion

		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("读取分发记录失败: %w", err)
	}
	return &dto.MSPHelpDistributionListResponse{Items: items, Total: len(items)}, nil
}

// Upgrade 将 active 分发原位刷新到 provider 最新已发布版本（显式动作，不自动同步）。
func (s *MSPHelpSpaceService) Upgrade(ctx context.Context, actor MSPWorkbenchActor, customerTenantID, distributionID int) (*dto.MSPHelpDistribution, error) {
	dist, src, err := s.loadActiveDistribution(ctx, actor, customerTenantID, distributionID)
	if err != nil {
		return nil, err
	}
	if src.Version <= dist.SourceVersion {
		return nil, ErrMSPHelpDocConflict
	}

	wctx := tenantctx.WithTenantID(ctx, customerTenantID)
	if _, err := s.db.ExecContext(wctx, `UPDATE msp_help_distributions SET
		source_doc_id=$3, source_version=$4, source_updated_at=$5, title=$6, page_key=$7, body_md=$8, updated_at=NOW()
		WHERE tenant_id=$1 AND id=$2 AND status='active'`,
		customerTenantID, distributionID, src.ID, src.Version, src.UpdatedAt, src.Title, src.PageKey, src.BodyMD); err != nil {
		return nil, fmt.Errorf("升级客户帮助副本失败: %w", err)
	}

	dist.SourceDocID = src.ID
	dist.SourceVersion = src.Version
	dist.LatestProviderVersion = src.Version
	dist.UpgradeAvailable = false
	dist.Title = src.Title
	dist.PageKey = src.PageKey

	s.recordHelpAudit(ctx, actor, customerTenantID, "upgrade", src.Slug, src.Version, "success")
	return dist, nil
}

// Unbind 解绑分发：副本转为客户原生内容（保留可见，停止追踪 provider 更新）。
func (s *MSPHelpSpaceService) Unbind(ctx context.Context, actor MSPWorkbenchActor, customerTenantID, distributionID int) (*dto.MSPHelpDistribution, error) {
	dist, _, err := s.loadActiveDistribution(ctx, actor, customerTenantID, distributionID)
	if err != nil {
		return nil, err
	}

	wctx := tenantctx.WithTenantID(ctx, customerTenantID)
	var unboundAt time.Time
	if err := s.db.QueryRowContext(wctx, `UPDATE msp_help_distributions SET
		status='unbound', unbound_by=$3, unbound_at=NOW(), updated_at=NOW()
		WHERE tenant_id=$1 AND id=$2 AND status='active'
		RETURNING unbound_at`,
		customerTenantID, distributionID, actor.UserID).Scan(&unboundAt); err != nil {
		return nil, fmt.Errorf("解绑客户帮助副本失败: %w", err)
	}

	dist.Status = "unbound"
	dist.UnboundBy = actor.UserID
	dist.UnboundAt = &unboundAt
	dist.UpgradeAvailable = false

	s.recordHelpAudit(ctx, actor, customerTenantID, "unbind", dist.SourceSlug, dist.SourceVersion, "success")
	return dist, nil
}

// loadActiveDistribution 读取 active 分发行 + provider 最新已发布文档（授权经 mspguard）。
func (s *MSPHelpSpaceService) loadActiveDistribution(
	ctx context.Context,
	actor MSPWorkbenchActor,
	customerTenantID, distributionID int,
) (*dto.MSPHelpDistribution, *dto.MSPHelpDoc, error) {
	if s == nil || s.db == nil {
		return nil, nil, fmt.Errorf("MSP 帮助空间服务未配置")
	}
	if actor.UserID <= 0 || actor.HomeTenantID <= 0 || customerTenantID <= 0 || distributionID <= 0 {
		return nil, nil, fmt.Errorf("%w: 参数不完整", ErrMSPHelpDocInvalid)
	}
	if s.guard == nil {
		return nil, nil, fmt.Errorf("帮助空间分发授权校验未配置")
	}
	if err := s.guard.CanAccessCustomer(ctx, actor.UserID, customerTenantID); err != nil {
		return nil, nil, err
	}

	rctx := tenantctx.WithTenantID(ctx, customerTenantID)
	row := s.db.QueryRowContext(rctx, "SELECT "+mspHelpDistributionColumns+
		" FROM msp_help_distributions WHERE tenant_id=$1 AND id=$2 AND provider_tenant_id=$3",
		customerTenantID, distributionID, actor.HomeTenantID)
	dist, err := scanMSPHelpDistribution(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil, ErrMSPHelpDocNotFound
	}
	if err != nil {
		return nil, nil, err
	}
	dist.CustomerTenantID = customerTenantID
	dist.ProviderTenantID = actor.HomeTenantID
	if dist.Status != "active" {
		return nil, nil, ErrMSPHelpDocConflict
	}

	src, err := s.loadPublishedDocByID(tenantctx.WithTenantID(ctx, actor.HomeTenantID), actor.HomeTenantID, dist.SourceDocID)
	if err != nil {
		return nil, nil, err
	}
	return dist, src, nil
}

// ===== 客户侧消费（普通租户行，帮助面板只读）=====

// CustomerHelpDocs 读取本租户可见的分发帮助（active 跟随 + unbound 原生副本）；不为空时按 pageKey 过滤。
func (s *MSPHelpSpaceService) CustomerHelpDocs(ctx context.Context, tenantID int, pageKey string) ([]CustomerHelpDoc, error) {
	if s == nil || s.db == nil || tenantID <= 0 {
		return nil, nil
	}
	rctx := tenantctx.WithTenantID(ctx, tenantID)
	query := "SELECT title,page_key,body_md,source_slug,source_version,provider_tenant_id,status" +
		" FROM msp_help_distributions WHERE tenant_id=$1 AND status IN ('active','unbound')"
	args := []interface{}{tenantID}
	if strings.TrimSpace(pageKey) != "" {
		query += " AND page_key=$2"
		args = append(args, strings.TrimSpace(pageKey))
	}
	query += " ORDER BY (status='active') DESC, id DESC"

	rows, err := s.db.QueryContext(rctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("查询客户帮助副本失败: %w", err)
	}
	defer func() { _ = rows.Close() }()

	docs := make([]CustomerHelpDoc, 0)
	for rows.Next() {
		var doc CustomerHelpDoc
		if err := rows.Scan(&doc.Title, &doc.PageKey, &doc.BodyMD, &doc.SourceSlug,
			&doc.SourceVersion, &doc.ProviderTenantID, &doc.Status); err != nil {
			return nil, fmt.Errorf("读取客户帮助副本失败: %w", err)
		}
		docs = append(docs, doc)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("读取客户帮助副本失败: %w", err)
	}
	return docs, nil
}

// SearchCustomerHelpDocs 在客户可见的分发帮助中做子串检索（title/body，ILIKE；返回片段）。
func (s *MSPHelpSpaceService) SearchCustomerHelpDocs(ctx context.Context, tenantID int, query, pageKey string, limit int) ([]CustomerHelpDoc, error) {
	if s == nil || s.db == nil || tenantID <= 0 {
		return nil, nil
	}
	query = strings.TrimSpace(query)
	if query == "" {
		return nil, nil
	}
	if limit <= 0 {
		limit = mspHelpSearchDefaultLimit
	}
	if limit > mspHelpSearchMaxLimit {
		limit = mspHelpSearchMaxLimit
	}

	pattern := "%" + escapeILIKEPattern(query) + "%"
	rctx := tenantctx.WithTenantID(ctx, tenantID)
	sqlQuery := "SELECT title,page_key,body_md,source_slug,source_version,provider_tenant_id,status" +
		" FROM msp_help_distributions WHERE tenant_id=$1 AND status IN ('active','unbound')" +
		" AND (title ILIKE $2 ESCAPE '\\' OR body_md ILIKE $2 ESCAPE '\\')"
	args := []interface{}{tenantID, pattern}
	if strings.TrimSpace(pageKey) != "" {
		sqlQuery += " AND page_key=$3"
		args = append(args, strings.TrimSpace(pageKey))
	}
	sqlQuery += fmt.Sprintf(" ORDER BY (status='active') DESC, id DESC LIMIT $%d", len(args)+1)
	args = append(args, limit)

	rows, err := s.db.QueryContext(rctx, sqlQuery, args...)
	if err != nil {
		return nil, fmt.Errorf("检索客户帮助副本失败: %w", err)
	}
	defer func() { _ = rows.Close() }()

	docs := make([]CustomerHelpDoc, 0)
	for rows.Next() {
		var doc CustomerHelpDoc
		if err := rows.Scan(&doc.Title, &doc.PageKey, &doc.BodyMD, &doc.SourceSlug,
			&doc.SourceVersion, &doc.ProviderTenantID, &doc.Status); err != nil {
			return nil, fmt.Errorf("读取客户帮助副本失败: %w", err)
		}
		doc.Snippet = helpMatchSnippet(doc.BodyMD, query)
		docs = append(docs, doc)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("读取客户帮助副本失败: %w", err)
	}
	return docs, nil
}

// ===== 内部工具 =====

func (s *MSPHelpSpaceService) upsertDoc(ctx context.Context, query string, args ...interface{}) (*dto.MSPHelpDoc, error) {
	row := s.db.QueryRowContext(ctx, query, args...)
	doc, err := scanMSPHelpDoc(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrMSPHelpDocNotFound
	}
	if err != nil {
		return nil, err
	}
	return doc, nil
}

// loadPublishedDocByID 读取 provider 已发布文档（非已发布 → ErrMSPHelpDocNotPublished）。
func (s *MSPHelpSpaceService) loadPublishedDocByID(ctx context.Context, providerTenantID, docID int) (*dto.MSPHelpDoc, error) {
	row := s.db.QueryRowContext(ctx, "SELECT "+mspHelpDocColumns+
		" FROM msp_help_docs WHERE tenant_id=$1 AND id=$2", providerTenantID, docID)
	doc, err := scanMSPHelpDoc(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrMSPHelpDocNotFound
	}
	if err != nil {
		return nil, err
	}
	if doc.Status != "published" {
		return nil, ErrMSPHelpDocNotPublished
	}
	return doc, nil
}

// rowScanner 统一 *sql.Row / *sql.Rows 的扫描入口。
type rowScanner interface {
	Scan(dest ...interface{}) error
}

func scanMSPHelpDoc(row rowScanner) (*dto.MSPHelpDoc, error) {
	var (
		doc         dto.MSPHelpDoc
		tenantID    int
		publishedAt sql.NullTime
	)
	if err := row.Scan(&doc.ID, &tenantID, &doc.Slug, &doc.Title, &doc.PageKey, &doc.BodyMD,
		&doc.Status, &doc.Version, &doc.CreatedBy, &doc.UpdatedBy,
		&doc.CreatedAt, &doc.UpdatedAt, &publishedAt); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, sql.ErrNoRows
		}
		return nil, fmt.Errorf("读取帮助空间文档失败: %w", err)
	}
	if publishedAt.Valid {
		t := publishedAt.Time
		doc.PublishedAt = &t
	}
	return &doc, nil
}

func scanMSPHelpDistribution(row rowScanner) (*dto.MSPHelpDistribution, error) {
	var (
		dist      dto.MSPHelpDistribution
		tenantID  int
		unboundAt sql.NullTime
	)
	if err := row.Scan(&dist.ID, &tenantID, &dist.ProviderTenantID, &dist.SourceDocID, &dist.SourceSlug,
		&dist.SourceVersion, &dist.Title, &dist.PageKey, &dist.Status, &dist.DistributedBy,
		&dist.DistributedAt, &dist.UnboundBy, &unboundAt); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, sql.ErrNoRows
		}
		return nil, fmt.Errorf("读取分发记录失败: %w", err)
	}
	dist.CustomerTenantID = tenantID
	if unboundAt.Valid {
		t := unboundAt.Time
		dist.UnboundAt = &t
	}
	return &dist, nil
}

func normalizeMSPHelpDocInput(req dto.MSPHelpDocRequest) (dto.MSPHelpDocRequest, error) {
	req.Slug = strings.TrimSpace(req.Slug)
	req.Title = strings.TrimSpace(req.Title)
	req.PageKey = strings.TrimSpace(req.PageKey)
	if req.Slug == "" || len(req.Slug) > 64 {
		return req, fmt.Errorf("%w: slug 必填且 ≤64 字符", ErrMSPHelpDocInvalid)
	}
	for _, r := range req.Slug {
		ok := (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '-' || r == '_' || r == '.'
		if !ok {
			return req, fmt.Errorf("%w: slug 仅允许小写字母/数字/._-", ErrMSPHelpDocInvalid)
		}
	}
	if req.Title == "" || len(req.Title) > 255 {
		return req, fmt.Errorf("%w: title 必填且 ≤255 字符", ErrMSPHelpDocInvalid)
	}
	if len(req.PageKey) > 128 {
		return req, fmt.Errorf("%w: pageKey 超长", ErrMSPHelpDocInvalid)
	}
	if len(req.BodyMD) > mspHelpDocMaxBodyBytes {
		return req, fmt.Errorf("%w: 正文超长", ErrMSPHelpDocInvalid)
	}
	return req, nil
}

// escapeILIKEPattern 转义 LIKE 通配符（配合 ESCAPE '\'）。
func escapeILIKEPattern(value string) string {
	replacer := strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`)
	return replacer.Replace(value)
}

// helpMatchSnippet 截取首个命中附近的片段（rune 安全；未命中时取头部）。
func helpMatchSnippet(body, query string) string {
	runes := []rune(body)
	lowerBody := strings.ToLower(body)
	lowerQuery := strings.ToLower(query)
	start := 0
	if idx := strings.Index(lowerBody, lowerQuery); idx >= 0 {
		// 字节位置 → rune 位置；向前留 20 字上下文。
		runeIdx := len([]rune(lowerBody[:idx]))
		start = runeIdx - 20
		if start < 0 {
			start = 0
		}
	}
	end := start + mspHelpSnippetRunes
	if end > len(runes) {
		end = len(runes)
	}
	snippet := strings.TrimSpace(string(runes[start:end]))
	if end < len(runes) {
		snippet += "…"
	}
	return snippet
}

// recordHelpAudit 分发授权审计（best-effort；resource=msp_help_doc，source=workbench）。
func (s *MSPHelpSpaceService) recordHelpAudit(
	ctx context.Context,
	actor MSPWorkbenchActor,
	customerTenantID int,
	op, slug string,
	version int,
	outcome string,
) {
	if s.client == nil {
		return
	}
	payload, _ := json.Marshal(map[string]any{
		"source":               "workbench",
		"op":                   op,
		"actor_user_id":        actor.UserID,
		"actor_home_tenant_id": actor.HomeTenantID,
		"target_tenant_id":     customerTenantID,
		"source_slug":          slug,
		"source_version":       version,
		"outcome":              outcome,
	})
	rowTenant := actor.HomeTenantID
	if rowTenant <= 0 {
		rowTenant = customerTenantID
	}
	auditCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	if _, err := s.client.AuditLog.Create().
		SetCreatedAt(time.Now()).
		SetTenantID(rowTenant).
		SetUserID(actor.UserID).
		SetActorAccount(actor.Username).
		SetTargetTenantID(customerTenantID).
		SetSource(middleware.AuditSourceWorkbench).
		SetResource("msp_help_doc").
		SetAction("msp_help_doc." + op).
		SetPath("/api/v1/msp/help").
		SetMethod("POST").
		SetStatusCode(map[bool]int{true: 200, false: 403}[outcome == "success"]).
		SetRequestBody(string(payload)).
		Save(auditCtx); err != nil && s.logger != nil {
		s.logger.Warnw("msp help audit write failed", "error", err, "op", op, "slug", slug, "customer_tenant_id", customerTenantID)
	}
}
