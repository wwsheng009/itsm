package knowledge

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	"itsm-backend/ent"
	"itsm-backend/ent/knowledgearticle"
	"itsm-backend/ent/knowledgearticleversion"
	"itsm-backend/ent/user"
)

// defaultCategories provides baseline knowledge-base categories so the category
// dropdown is never empty for a freshly provisioned tenant. The list is always
// merged with whatever distinct categories already exist in the article table,
// so adding more categories later does not lose them.
var defaultCategories = []string{
	"故障排查",
	"操作指南",
	"最佳实践",
	"变更发布",
	"安全合规",
	"常见问题",
	"工单模板",
	"流程说明",
}

type EntRepository struct {
	client *ent.Client
}

func NewEntRepository(client *ent.Client) *EntRepository {
	return &EntRepository{client: client}
}

// toDomain maps ent KnowledgeArticle to domain Article
func toDomain(e *ent.KnowledgeArticle) *Article {
	if e == nil {
		return nil
	}
	tags := []string{}
	if e.Tags != "" {
		tags = strings.Split(e.Tags, ",")
	}
	return &Article{
		ID:          e.ID,
		Title:       e.Title,
		Content:     e.Content,
		ContentType: e.ContentType,
		Category:    e.Category,
		Tags:        tags,
		AuthorID:    e.AuthorID,
		TenantID:    e.TenantID,
		IsPublished: e.IsPublished,
		CreatedAt:   e.CreatedAt,
		UpdatedAt:   e.UpdatedAt,

		ValidFrom:          e.ValidFrom,
		ValidUntil:         e.ValidUntil,
		LastReviewedAt:     e.LastReviewedAt,
		ReviewIntervalDays: e.ReviewIntervalDays,
		AuthorityLevel:     e.AuthorityLevel,
	}
}

func (r *EntRepository) Create(ctx context.Context, a *Article) (*Article, error) {
	tagsStr := strings.Join(a.Tags, ",")
	e, err := r.client.KnowledgeArticle.Create().
		SetTitle(a.Title).
		SetContent(a.Content).
		SetContentType(a.ContentType).
		SetCategory(a.Category).
		SetTags(tagsStr).
		SetAuthorID(a.AuthorID).
		SetTenantID(a.TenantID).
		SetIsPublished(a.IsPublished).
		// 时效性（L1）与权威性（L2）：三个时间字段用 Nillable setter，
		// nil 表示不设时效/不设复核，与数据库默认值语义一致。
		SetNillableValidFrom(a.ValidFrom).
		SetNillableValidUntil(a.ValidUntil).
		SetNillableLastReviewedAt(a.LastReviewedAt).
		SetReviewIntervalDays(a.ReviewIntervalDays).
		SetAuthorityLevel(a.AuthorityLevel).
		Save(ctx)
	if err != nil {
		return nil, err
	}
	return toDomain(e), nil
}

func (r *EntRepository) Get(ctx context.Context, id int, tenantID int) (*Article, error) {
	e, err := r.client.KnowledgeArticle.Query().
		Where(knowledgearticle.ID(id), knowledgearticle.TenantID(tenantID), knowledgearticle.DeletedAtIsNil()).
		First(ctx)
	if err != nil {
		return nil, err
	}
	return toDomain(e), nil
}

func (r *EntRepository) List(ctx context.Context, tenantID int, page, size int, category, search, status string) ([]*Article, int, error) {
	q := r.client.KnowledgeArticle.Query().Where(knowledgearticle.TenantID(tenantID), knowledgearticle.DeletedAtIsNil())

	if category != "" {
		q = q.Where(knowledgearticle.Category(category))
	}
	if search != "" {
		q = q.Where(
			knowledgearticle.Or(
				knowledgearticle.TitleContains(search),
				knowledgearticle.ContentContains(search),
			),
		)
	}
	if status != "" {
		if strings.ToLower(status) == "published" {
			q = q.Where(knowledgearticle.IsPublished(true))
		} else if strings.ToLower(status) == "draft" {
			q = q.Where(knowledgearticle.IsPublished(false))
		}
	}

	total, err := q.Count(ctx)
	if err != nil {
		return nil, 0, err
	}

	es, err := q.Limit(size).Offset((page - 1) * size).Order(ent.Desc(knowledgearticle.FieldCreatedAt)).All(ctx)
	if err != nil {
		return nil, 0, err
	}

	var results []*Article
	for _, e := range es {
		results = append(results, toDomain(e))
	}
	return results, total, nil
}

func (r *EntRepository) Update(ctx context.Context, a *Article) (*Article, error) {
	tagsStr := strings.Join(a.Tags, ",")
	u := r.client.KnowledgeArticle.UpdateOneID(a.ID).
		Where(knowledgearticle.TenantID(a.TenantID), knowledgearticle.DeletedAtIsNil()).
		SetTitle(a.Title).
		SetContent(a.Content).
		SetContentType(a.ContentType).
		SetCategory(a.Category).
		SetTags(tagsStr).
		SetIsPublished(a.IsPublished).
		SetReviewIntervalDays(a.ReviewIntervalDays).
		SetAuthorityLevel(a.AuthorityLevel)

	// 时效字段传 nil 表示「解除时效设置」，必须显式 Clear。
	// 注意 SetNillableXxx(nil) 在 ent 里是 no-op（不清除既有值），
	// 若这里偷懒用它，管理员将无法撤销误设的失效时间——
	// 知识会一直被 L1 过滤掉，且界面上看不出原因。
	if a.ValidFrom != nil {
		u = u.SetValidFrom(*a.ValidFrom)
	} else {
		u = u.ClearValidFrom()
	}
	if a.ValidUntil != nil {
		u = u.SetValidUntil(*a.ValidUntil)
	} else {
		u = u.ClearValidUntil()
	}
	if a.LastReviewedAt != nil {
		u = u.SetLastReviewedAt(*a.LastReviewedAt)
	} else {
		u = u.ClearLastReviewedAt()
	}

	e, err := u.Save(ctx)
	if err != nil {
		return nil, err
	}
	return toDomain(e), nil
}

// MarkReviewed 记录一次内容复核。
// 只更新 last_reviewed_at，不触碰正文——复核是对内容「仍然适用」的确认，
// 若与内容更新混在一个入口，复核动作就可能夹带未审校的正文改动。
func (r *EntRepository) MarkReviewed(ctx context.Context, id int, tenantID int) (*Article, error) {
	e, err := r.client.KnowledgeArticle.UpdateOneID(id).
		Where(knowledgearticle.TenantID(tenantID), knowledgearticle.DeletedAtIsNil()).
		SetLastReviewedAt(time.Now()).
		Save(ctx)
	if err != nil {
		return nil, err
	}
	return toDomain(e), nil
}

func (r *EntRepository) Delete(ctx context.Context, id int, tenantID int) error {
	_, err := r.client.KnowledgeArticle.Update().
		Where(knowledgearticle.ID(id), knowledgearticle.TenantID(tenantID), knowledgearticle.DeletedAtIsNil()).
		SetDeletedAt(time.Now()).
		Save(ctx)
	return err
}

func (r *EntRepository) GetCategories(ctx context.Context, tenantID int) ([]string, error) {
	existing, err := r.client.KnowledgeArticle.Query().
		Where(knowledgearticle.TenantID(tenantID), knowledgearticle.DeletedAtIsNil()).
		GroupBy(knowledgearticle.FieldCategory).
		Strings(ctx)
	if err != nil {
		return nil, err
	}

	// Merge defaults with whatever already exists, then dedupe and sort.
	seen := make(map[string]struct{}, len(existing)+len(defaultCategories))
	merged := make([]string, 0, len(existing)+len(defaultCategories))
	for _, c := range defaultCategories {
		c = strings.TrimSpace(c)
		if c == "" {
			continue
		}
		if _, ok := seen[c]; ok {
			continue
		}
		seen[c] = struct{}{}
		merged = append(merged, c)
	}
	for _, c := range existing {
		c = strings.TrimSpace(c)
		if c == "" {
			continue
		}
		if _, ok := seen[c]; ok {
			continue
		}
		seen[c] = struct{}{}
		merged = append(merged, c)
	}
	sort.Slice(merged, func(i, j int) bool { return merged[i] < merged[j] })
	return merged, nil
}

func (r *EntRepository) GetStats(ctx context.Context, tenantID int) (*Stats, error) {
	// Query all articles for this tenant
	query := r.client.KnowledgeArticle.Query().Where(knowledgearticle.TenantID(tenantID), knowledgearticle.DeletedAtIsNil())

	// Get total count
	total, err := query.Count(ctx)
	if err != nil {
		return nil, err
	}

	// Get published count
	published, err := query.Clone().Where(knowledgearticle.IsPublished(true)).Count(ctx)
	if err != nil {
		return nil, err
	}

	// Draft count
	draft, err := query.Clone().Where(knowledgearticle.IsPublished(false)).Count(ctx)
	if err != nil {
		return nil, err
	}

	// Sum of view counts
	type ViewSum struct {
		TotalViews int `json:"total_views"` // matches ent aggregate alias for Scan()
	}
	var viewResult []ViewSum
	err = r.client.KnowledgeArticle.Query().
		Where(knowledgearticle.TenantID(tenantID), knowledgearticle.DeletedAtIsNil()).
		Aggregate(ent.As(ent.Sum(knowledgearticle.FieldViewCount), "total_views")).
		Scan(ctx, &viewResult)
	if err != nil {
		return nil, err
	}
	totalViews := int64(0)
	if len(viewResult) > 0 {
		totalViews = int64(viewResult[0].TotalViews)
	}

	// Sum of like counts
	type LikeSum struct {
		TotalLikes int `json:"total_likes"` // matches ent aggregate alias for Scan()
	}
	var likeResult []LikeSum
	err = r.client.KnowledgeArticle.Query().
		Where(knowledgearticle.TenantID(tenantID), knowledgearticle.DeletedAtIsNil()).
		Aggregate(ent.As(ent.Sum(knowledgearticle.FieldLikeCount), "total_likes")).
		Scan(ctx, &likeResult)
	if err != nil {
		return nil, err
	}
	totalLikes := int64(0)
	if len(likeResult) > 0 {
		totalLikes = int64(likeResult[0].TotalLikes)
	}

	// Get category distribution
	type CategoryGroup struct {
		Category string `json:"category"`
		Count    int    `json:"count"`
	}
	var categoryResults []CategoryGroup
	err = r.client.KnowledgeArticle.Query().
		Where(knowledgearticle.TenantID(tenantID), knowledgearticle.DeletedAtIsNil()).
		GroupBy(knowledgearticle.FieldCategory).
		Aggregate(ent.Count()).
		Scan(ctx, &categoryResults)
	if err != nil {
		return nil, err
	}

	categories := make([]CategoryStat, 0, len(categoryResults))
	for _, cr := range categoryResults {
		categories = append(categories, CategoryStat{
			Name:  cr.Category,
			Count: int64(cr.Count),
		})
	}

	return &Stats{
		Total:      int64(total),
		Published:  int64(published),
		Draft:      int64(draft),
		TotalViews: totalViews,
		TotalLikes: totalLikes,
		Categories: categories,
	}, nil
}

// GetByIDs returns published, non-deleted articles by ID for a given tenant.
// The returned slice preserves the order of the input ids slice.
func (r *EntRepository) GetByIDs(ctx context.Context, tenantID int, ids []int) ([]*Article, error) {
	if len(ids) == 0 {
		return nil, nil
	}

	es, err := r.client.KnowledgeArticle.Query().
		Where(
			knowledgearticle.IDIn(ids...),
			knowledgearticle.TenantID(tenantID),
			knowledgearticle.DeletedAtIsNil(),
			knowledgearticle.IsPublished(true),
		).
		All(ctx)
	if err != nil {
		return nil, err
	}

	// Build a map for quick lookup
	domainMap := make(map[int]*Article, len(es))
	for _, e := range es {
		domainMap[e.ID] = toDomain(e)
	}

	// Preserve input order
	result := make([]*Article, 0, len(ids))
	for _, id := range ids {
		if a, ok := domainMap[id]; ok {
			result = append(result, a)
		}
	}
	return result, nil
}

// ==================== 版本历史 ====================

// toVersionDomain maps ent KnowledgeArticleVersion to domain ArticleVersion.
func toVersionDomain(e *ent.KnowledgeArticleVersion) *ArticleVersion {
	if e == nil {
		return nil
	}
	tags := []string{}
	if e.Tags != "" {
		tags = strings.Split(e.Tags, ",")
	}
	return &ArticleVersion{
		ID:            e.ID,
		ArticleID:     e.ArticleID,
		Version:       e.Version,
		Title:         e.Title,
		Content:       e.Content,
		Category:      e.Category,
		Tags:          tags,
		AuthorID:      e.AuthorID,
		ChangeSummary: e.ChangeSummary,
		CreatedAt:     e.CreatedAt,
	}
}

// nextVersionNumber 返回文章下一个可用的版本号（无历史时为 1）。
// 同时适用于 ent.Client 与 ent.Tx 上的查询。
func nextVersionNumber(ctx context.Context, q *ent.KnowledgeArticleVersionQuery) (int, error) {
	latest, err := q.Order(ent.Desc(knowledgearticleversion.FieldVersion)).First(ctx)
	if err != nil {
		if ent.IsNotFound(err) {
			return 1, nil
		}
		return 0, err
	}
	return latest.Version + 1, nil
}

// fillAuthorNames 批量回填版本创建者姓名。仅用于展示：
// 用户查询失败时保持姓名为空，不影响版本列表本身可用。
func (r *EntRepository) fillAuthorNames(ctx context.Context, versions []*ArticleVersion) {
	ids := make([]int, 0, len(versions))
	seen := make(map[int]struct{}, len(versions))
	for _, v := range versions {
		if v.AuthorID <= 0 {
			continue
		}
		if _, ok := seen[v.AuthorID]; ok {
			continue
		}
		seen[v.AuthorID] = struct{}{}
		ids = append(ids, v.AuthorID)
	}
	if len(ids) == 0 {
		return
	}

	users, err := r.client.User.Query().Where(user.IDIn(ids...)).All(ctx)
	if err != nil {
		return
	}
	names := make(map[int]string, len(users))
	for _, u := range users {
		names[u.ID] = u.Name
	}
	for _, v := range versions {
		if name, ok := names[v.AuthorID]; ok {
			v.AuthorName = name
		}
	}
}

func (r *EntRepository) ListVersions(ctx context.Context, articleID int, tenantID int) ([]*ArticleVersion, error) {
	// 先校验文章归属：避免跨租户通过文章 ID 探测版本历史。
	if _, err := r.client.KnowledgeArticle.Query().
		Where(knowledgearticle.ID(articleID), knowledgearticle.TenantID(tenantID), knowledgearticle.DeletedAtIsNil()).
		Only(ctx); err != nil {
		return nil, err
	}

	es, err := r.client.KnowledgeArticleVersion.Query().
		Where(knowledgearticleversion.ArticleID(articleID)).
		Order(ent.Desc(knowledgearticleversion.FieldVersion)).
		All(ctx)
	if err != nil {
		return nil, err
	}

	versions := make([]*ArticleVersion, 0, len(es))
	for _, e := range es {
		versions = append(versions, toVersionDomain(e))
	}
	r.fillAuthorNames(ctx, versions)
	return versions, nil
}

func (r *EntRepository) GetVersion(ctx context.Context, articleID int, version int, tenantID int) (*ArticleVersion, error) {
	if _, err := r.client.KnowledgeArticle.Query().
		Where(knowledgearticle.ID(articleID), knowledgearticle.TenantID(tenantID), knowledgearticle.DeletedAtIsNil()).
		Only(ctx); err != nil {
		return nil, err
	}

	e, err := r.client.KnowledgeArticleVersion.Query().
		Where(knowledgearticleversion.ArticleID(articleID), knowledgearticleversion.Version(version)).
		Only(ctx)
	if err != nil {
		return nil, err
	}
	out := toVersionDomain(e)
	r.fillAuthorNames(ctx, []*ArticleVersion{out})
	return out, nil
}

func (r *EntRepository) CreateVersion(ctx context.Context, articleID int, tenantID int, changeSummary string) (*ArticleVersion, error) {
	article, err := r.client.KnowledgeArticle.Query().
		Where(knowledgearticle.ID(articleID), knowledgearticle.TenantID(tenantID), knowledgearticle.DeletedAtIsNil()).
		Only(ctx)
	if err != nil {
		return nil, err
	}

	next, err := nextVersionNumber(ctx, r.client.KnowledgeArticleVersion.Query().
		Where(knowledgearticleversion.ArticleID(articleID)))
	if err != nil {
		return nil, err
	}

	e, err := r.client.KnowledgeArticleVersion.Create().
		SetArticleID(article.ID).
		SetVersion(next).
		SetTitle(article.Title).
		SetContent(article.Content).
		SetCategory(article.Category).
		SetTags(article.Tags).
		SetAuthorID(article.AuthorID).
		SetChangeSummary(changeSummary).
		Save(ctx)
	if err != nil {
		return nil, err
	}
	return toVersionDomain(e), nil
}

func (r *EntRepository) RestoreVersion(ctx context.Context, articleID int, version int, tenantID int) (*Article, error) {
	tx, err := r.client.Tx(ctx)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback() }()

	// 文章归属校验：租户不匹配 / 已软删时返回 ent.NotFoundError。
	if _, err := tx.KnowledgeArticle.Query().
		Where(knowledgearticle.ID(articleID), knowledgearticle.TenantID(tenantID), knowledgearticle.DeletedAtIsNil()).
		Only(ctx); err != nil {
		return nil, err
	}

	target, err := tx.KnowledgeArticleVersion.Query().
		Where(knowledgearticleversion.ArticleID(articleID), knowledgearticleversion.Version(version)).
		Only(ctx)
	if err != nil {
		return nil, err
	}

	// 仅回写版本快照覆盖的字段；content_type / 时效等元数据保持当前值。
	updated, err := tx.KnowledgeArticle.UpdateOneID(articleID).
		Where(knowledgearticle.TenantID(tenantID), knowledgearticle.DeletedAtIsNil()).
		SetTitle(target.Title).
		SetContent(target.Content).
		SetCategory(target.Category).
		SetTags(target.Tags).
		Save(ctx)
	if err != nil {
		return nil, err
	}

	// 恢复结果本身记为一条新版本：保证「最新版本 = 当前正文」，且恢复动作可追溯。
	next, err := nextVersionNumber(ctx, tx.KnowledgeArticleVersion.Query().
		Where(knowledgearticleversion.ArticleID(articleID)))
	if err != nil {
		return nil, err
	}
	if _, err := tx.KnowledgeArticleVersion.Create().
		SetArticleID(updated.ID).
		SetVersion(next).
		SetTitle(updated.Title).
		SetContent(updated.Content).
		SetCategory(updated.Category).
		SetTags(updated.Tags).
		SetAuthorID(updated.AuthorID).
		SetChangeSummary(fmt.Sprintf("恢复到 v%d", version)).
		Save(ctx); err != nil {
		return nil, err
	}

	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return toDomain(updated), nil
}
