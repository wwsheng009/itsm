package knowledge

import (
	"time"
)

// Article representing a knowledge base article
type Article struct {
	ID      int    `json:"id"`
	Title   string `json:"title"`
	Content string `json:"content"`
	// ContentType 正文类型：text / markdown / html / rich_text。
	// 空串 = 历史数据，由 knowledgecontent.Resolve 在读写两端按内容兜底判定。
	ContentType string    `json:"contentType"`
	Category    string    `json:"category"`
	Tags        []string  `json:"tags"`
	AuthorID    int       `json:"authorId"`
	TenantID    int       `json:"tenantId"`
	IsPublished bool      `json:"isPublished"`
	CreatedAt   time.Time `json:"createdAt"`
	UpdatedAt   time.Time `json:"updatedAt"`
	// RelevanceScore is set by SearchArticles when returning RAG results (0 if not from RAG).
	// Values are in [0, 1]; higher means more relevant.
	RelevanceScore float64 `json:"relevanceScore,omitempty"`

	// 知识可引用性 L1：时效性。三个时间字段为 nil 表示未设置，
	// 语义是「长期有效 / 不设复核」，与本次改动前的行为一致。
	ValidFrom          *time.Time `json:"validFrom,omitempty"`
	ValidUntil         *time.Time `json:"validUntil,omitempty"`
	LastReviewedAt     *time.Time `json:"lastReviewedAt,omitempty"`
	ReviewIntervalDays int        `json:"reviewIntervalDays,omitempty"`

	// 知识可引用性 L2：权威性。仅影响检索排序，不影响准入。
	AuthorityLevel int `json:"authorityLevel,omitempty"`

	// Freshness 为派生字段，仅在检索/详情响应中填充，用于前端提示「该内容已逾期未复核」。
	// 不参与写入，写入请以 ValidUntil / LastReviewedAt 等原始字段为准。
	Freshness string `json:"freshness,omitempty"`
}

// ArticleVersion 文章历史版本快照。
//
// 快照在文章写入成功后生成：v1 为创建时的「初始版本」，此后每次更新把
// 写入后的正文追加为新版本（恢复同样只追加新版本），因此列表倒序第一条
// 始终等于文章当前正文；历史版本只增不改，回放即各时点的历史状态。
type ArticleVersion struct {
	ID            int
	ArticleID     int
	Version       int
	Title         string
	Content       string
	Category      string
	Tags          []string
	AuthorID      int
	AuthorName    string
	ChangeSummary string
	CreatedAt     time.Time
}

// Category represents a knowledge category
type Category struct {
	Name string `json:"name"`
}
