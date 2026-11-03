package knowledge

import (
	"context"
	"time"
)

// Repository interface for Knowledge domain
type Repository interface {
	Create(ctx context.Context, a *Article) (*Article, error)
	Get(ctx context.Context, id int, tenantID int) (*Article, error)
	// List 列表/增量查询。updatedSince 非空时按 updated_at 升序返回该时间之后的变化，
	// 供客户端做游标增量下行；为空时保持原有的创建时间倒序列表语义。
	List(ctx context.Context, tenantID int, page, size int, category, search, status string, updatedSince *time.Time) ([]*Article, int, error)
	// ListDeleted 返回 since 之后被软删除的文章墓碑，供增量下行删除传播。
	ListDeleted(ctx context.Context, tenantID int, since *time.Time, page, size int) ([]DeletedArticle, int, error)
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

	// ListVersions 返回文章的全部发布版本（按版本号倒序，最新在前）。
	// 文章不存在 / 不属于当前租户 / 已软删时返回 ent.NotFoundError。
	ListVersions(ctx context.Context, articleID int, tenantID int) ([]*ArticleVersion, error)
	// GetVersion 返回指定版本；文章或版本不存在时返回 ent.NotFoundError。
	GetVersion(ctx context.Context, articleID int, version int, tenantID int) (*ArticleVersion, error)
	// GetLatestVersion 返回文章最近一个发布版本；从未发布过时返回 (nil, nil)。
	// 发布幂等判定依赖它：正文与最近发布版本一致时不重复产生版本。
	GetLatestVersion(ctx context.Context, articleID int, tenantID int) (*ArticleVersion, error)
	// Publish 把文章置为已发布；createRelease 为 true 时在同一事务内追加一条版本快照
	// （版本号 = 当前最大版本号 + 1，作者记为 publisherID）。
	// 版本快照与发布态在同事务内落库，避免出现「已发布但没有发布记录」的中间态。
	Publish(ctx context.Context, id int, tenantID int, publisherID int, changeSummary string, createRelease bool) (*Article, error)
	// SetPublished 仅切换发布可见性（下架 / 重新上架），不产生版本记录。
	SetPublished(ctx context.Context, id int, tenantID int, published bool) (*Article, error)
}

// DeletedArticle 是软删除文章的墓碑（只含下行需要的字段）。
type DeletedArticle struct {
	ID        int
	DeletedAt time.Time
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
