package knowledge

import (
	"context"
)

// Repository interface for Knowledge domain
type Repository interface {
	Create(ctx context.Context, a *Article) (*Article, error)
	Get(ctx context.Context, id int, tenantID int) (*Article, error)
	List(ctx context.Context, tenantID int, page, size int, category, search, status string) ([]*Article, int, error)
	Update(ctx context.Context, a *Article) (*Article, error)
	Delete(ctx context.Context, id int, tenantID int) error
	// MarkReviewed 记录一次内容复核：把 last_reviewed_at 置为当前时间。
	// 这是 L1 时效管控的闭环动作——声明了复核周期的知识逾期后会被 RAG 过滤，
	// 复核是把它恢复回检索结果的唯一途径。只改复核时间，不触碰正文。
	MarkReviewed(ctx context.Context, id int, tenantID int) (*Article, error)
	GetCategories(ctx context.Context, tenantID int) ([]string, error)
	GetStats(ctx context.Context, tenantID int) (*Stats, error)
	// GetByIDs returns articles by a list of IDs, preserving the order of IDs.
	// It filters by tenant and only returns published, non-deleted articles.
	GetByIDs(ctx context.Context, tenantID int, ids []int) ([]*Article, error)

	// ListVersions 返回文章的全部历史版本（按版本号倒序，最新在前）。
	// 文章不存在 / 不属于当前租户 / 已软删时返回 ent.NotFoundError。
	ListVersions(ctx context.Context, articleID int, tenantID int) ([]*ArticleVersion, error)
	// GetVersion 返回指定版本；文章或版本不存在时返回 ent.NotFoundError。
	GetVersion(ctx context.Context, articleID int, version int, tenantID int) (*ArticleVersion, error)
	// CreateVersion 保存文章当前状态为新的版本快照（version = 当前最大版本号 + 1）。
	CreateVersion(ctx context.Context, articleID int, tenantID int, changeSummary string) (*ArticleVersion, error)
	// RestoreVersion 将文章恢复到指定版本：先保存恢复前快照，再回写标题/正文/分类/标签。
	// 整个过程在同一事务中完成，避免出现「快照写了但正文没回滚」的半成品状态。
	RestoreVersion(ctx context.Context, articleID int, version int, tenantID int) (*Article, error)
}

// Stats represents knowledge base statistics
type Stats struct {
	Total      int64
	Published  int64
	Draft      int64
	TotalViews int64
	TotalLikes int64
	Categories []CategoryStat
}

// CategoryStat represents category statistics
type CategoryStat struct {
	Name  string
	Count int64
}
