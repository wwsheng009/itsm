package dto

import (
	"encoding/json"
	"time"
)

// 创建知识库文章请求
type CreateKnowledgeArticleRequest struct {
	Title    string   `json:"title" binding:"required"`
	// max=200000：新建页既承接人工写作，也承接「AI 会话 → 整段整理成文章」的预填
	// （多轮会话可达数万字，见前端 ARTICLE_CONTENT_MAX_LENGTH；更新路径本就不设上限，
	// 这里放宽到同一量级，避免长会话保存被 400 拦下）。全局请求体上限 10MB 仍是硬边界。
	Content  string   `json:"content" binding:"required,min=1,max=200000"`
	Category string   `json:"category" binding:"required,max=50"`
	Tags     []string `json:"tags"`

	// ContentType 正文类型：text / markdown / html / rich_text。
	// 留空表示由服务端按内容形态自动判定（历史客户端兼容）。
	ContentType string `json:"contentType"`

	// 知识可引用性 L1：时效性。均为可选，留空表示「长期有效、不设复核」。
	ValidFrom          *time.Time `json:"validFrom"`
	ValidUntil         *time.Time `json:"validUntil"`
	ReviewIntervalDays *int       `json:"reviewIntervalDays" binding:"omitempty,min=0,max=3650"`

	// 知识可引用性 L2：权威性。0=普通 10=部门推荐 20=官方标准 30=唯一真相源。
	AuthorityLevel *int `json:"authorityLevel" binding:"omitempty,min=0,max=30"`
}

// 更新知识库文章请求
type UpdateKnowledgeArticleRequest struct {
	Title    *string  `json:"title"`
	Content  *string  `json:"content"`
	Category *string  `json:"category"`
	// Status 已废弃：更新接口刻意忽略该字段。发布态不是编辑字段，
	// 只能由 POST /knowledge/articles/{id}/publish|unpublish 两个动作变更，
	// 否则一次保存即可绕过版本记录把文章发布出去。
	Status   *string  `json:"status"`
	Tags     []string `json:"tags"`

	// ContentType 正文类型：text / markdown / html / rich_text。
	// 传 nil 表示「不显式指定」——服务端会保留已存类型，缺失时按内容判定。
	ContentType *string `json:"contentType"`

	// 知识可引用性 L1：时效性。指针语义即「不传=保持不变」。
	// 传 nil 指针本身在部分更新里就是「不改」，要解除时效需显式传空字符串等哨兵，
	// 当前版本请通过复核接口与创建时的声明管理时效，暂不支持把已设时效改回永久。
	ValidFrom          *time.Time `json:"validFrom"`
	ValidUntil         *time.Time `json:"validUntil"`
	ReviewIntervalDays *int       `json:"reviewIntervalDays" binding:"omitempty,min=0,max=3650"`

	// 知识可引用性 L2：权威性。
	AuthorityLevel *int `json:"authorityLevel" binding:"omitempty,min=0,max=30"`

	// 注意：这里刻意不暴露 LastReviewedAt。
	// 复核时间是可引用性的判定依据，一旦允许客户端直接写入，
	// 就能随便把时间改成未来值来消除逾期提醒，L1 会形同虚设。
	// 复核请走 POST /api/v1/knowledge/articles/:id/review。
}

// KnowledgeFreshness 时效状态，随文章响应返回，供前端提示「内容可能已过期」。
type KnowledgeFreshness struct {
	// Verdict 时效判定结果：ok / not_yet_effective / expired / review_overdue
	Verdict string `json:"verdict"`
	// Citable 当前是否可被 RAG 引用
	Citable bool `json:"citable"`
	// ReviewDueAt 下次复核到期时间，未设复核周期时为空
	ReviewDueAt *time.Time `json:"reviewDueAt,omitempty"`
}

// 知识库文章响应
type KnowledgeArticleResponse struct {
	ID        int       `json:"id"`
	Title     string    `json:"title"`
	Content   string    `json:"content"`
	// ContentType 是解析后的生效类型（历史数据由内容形态兜底判定），
	// 前端据此选择渲染器：text / markdown / html / rich_text。
	ContentType string   `json:"contentType"`
	Category  string    `json:"category"`
	Status    string    `json:"status"`
	Author    string    `json:"author"`
	Views     int       `json:"views"`
	Tags      []string  `json:"tags"`
	TenantID  int       `json:"tenantId"`
	CreatedAt time.Time `json:"createdAt"`
	UpdatedAt time.Time `json:"updatedAt"`

	// 知识可引用性 L1：时效性
	ValidFrom          *time.Time          `json:"validFrom,omitempty"`
	ValidUntil         *time.Time          `json:"validUntil,omitempty"`
	LastReviewedAt     *time.Time          `json:"lastReviewedAt,omitempty"`
	ReviewIntervalDays int                 `json:"reviewIntervalDays"`
	Freshness          *KnowledgeFreshness `json:"freshness,omitempty"`

	// 知识可引用性 L2：权威性
	AuthorityLevel int `json:"authorityLevel"`
}

// PublishKnowledgeArticleRequest 发布文章请求。
//
// 请求体可整体省略：changeLog 为本次发布说明，缺省时由服务端生成
// （首次发布 / 发布更新）。发布动作本身不携带正文——正文以当前草稿为准，
// 避免「用旧快照发布」覆盖他人并发修改。
type PublishKnowledgeArticleRequest struct {
	ChangeLog string `json:"changeLog" binding:"omitempty,max=200"`
}

// 知识库文章列表请求
type ListKnowledgeArticlesRequest struct {
	Page     int    `form:"page" binding:"min=1"`
	PageSize int    `form:"pageSize" binding:"min=1,max=100"`
	Category string `form:"category"`
	Status   string `form:"status"`
	Search   string `form:"search"`
}

// 知识库文章列表响应
type KnowledgeArticleListResponse struct {
	Items    []KnowledgeArticleResponse `json:"items"`
	Articles []KnowledgeArticleResponse `json:"articles"` // v1.1 回归：前端别名
	Total    int                        `json:"total"`
	Page     int                        `json:"page"`
	PageSize int                        `json:"pageSize"`
}

// MarshalJSON 保证 articles 别名至少与 items 一致，避免前端列表为空。
func (r KnowledgeArticleListResponse) MarshalJSON() ([]byte, error) {
	if r.Articles == nil {
		r.Articles = r.Items
	}
	type alias KnowledgeArticleListResponse
	return json.Marshal(alias(r))
}

// KnowledgeStatsResponse 知识库统计响应
type KnowledgeStatsResponse struct {
	Total      int             `json:"total"`      // 总文章数
	Published  int             `json:"published"`  // 已发布文章数
	Draft      int             `json:"draft"`      // 草稿数
	Views      int64           `json:"views"`      // 总浏览次数
	Rating     float64         `json:"rating"`     // 平均评分 (基于点赞数)
	Categories []CategoryStats `json:"categories"` // 按分类统计
}

// CategoryStats 分类统计
type CategoryStats struct {
	Name  string `json:"name"`  // 分类名称
	Count int    `json:"count"` // 文章数量
}

// KnowledgeArticleVersionResponse 文章版本历史响应。
//
// 字段名与前端 ArticleVersion 契约保持一致（changeLog / createdByName / contentType），
// 避免前端版本列表把「变更说明」「创建者」渲染成空值。
type KnowledgeArticleVersionResponse struct {
	ID            int       `json:"id"`
	ArticleID     int       `json:"articleId"`
	Version       int       `json:"version"`
	Title         string    `json:"title"`
	Content       string    `json:"content"`
	ContentType   string    `json:"contentType,omitempty"`
	Category      string    `json:"category"`
	Tags          []string  `json:"tags"`
	AuthorID      int       `json:"createdBy"`
	CreatedByName string    `json:"createdByName"`
	ChangeLog     string    `json:"changeLog"`
	CreatedAt     time.Time `json:"createdAt"`
}

// ListArticleVersionsRequest 列出版本请求
type ListArticleVersionsRequest struct {
	Page     int `form:"page" binding:"min=1"`
	PageSize int `form:"pageSize" binding:"min=1,max=50"`
}

// KnowledgeArticleVersionListResponse 文章版本列表响应
type KnowledgeArticleVersionListResponse struct {
	Versions []KnowledgeArticleVersionResponse `json:"versions"`
	Total    int                               `json:"total"`
	Page     int                               `json:"page"`
	PageSize int                               `json:"pageSize"`
}

// KnowledgeArticleVersionChange 单个版本差异项
type KnowledgeArticleVersionChange struct {
	Type    string `json:"type"` // added / removed
	Content string `json:"content"`
}

// KnowledgeArticleVersionCompareResponse 版本比较响应
type KnowledgeArticleVersionCompareResponse struct {
	FromVersion int                             `json:"fromVersion"`
	ToVersion   int                             `json:"toVersion"`
	Diff        string                          `json:"diff"`
	Changes     []KnowledgeArticleVersionChange `json:"changes"`
}

// RestoreArticleVersionRequest 恢复版本请求
type RestoreArticleVersionRequest struct {
	Version int `json:"version" binding:"required,min=1"`
}

// ArticleSessionResponse 协作会话响应
type ArticleSessionResponse struct {
	SessionID     int       `json:"sessionId"`
	ArticleID     int       `json:"articleId"`
	UserID        int       `json:"userId"`
	UserName      string    `json:"userName"`
	SessionToken  string    `json:"sessionToken"`
	Status        string    `json:"status"`
	LastHeartbeat time.Time `json:"lastHeartbeat"`
	CreatedAt     time.Time `json:"createdAt"`
}

// CreateSessionRequest 创建会话请求
type CreateSessionRequest struct {
	ArticleID int `json:"articleId" binding:"required,min=1"`
}

// SessionHeartbeatRequest 心跳请求
type SessionHeartbeatRequest struct {
	SessionToken string `json:"sessionToken" binding:"required"`
	CursorPos    *int   `json:"cursorPosition"`
}

// ArticleParticipantResponse 参与者响应
type ArticleParticipantResponse struct {
	UserID       int       `json:"userId"`
	UserName     string    `json:"userName"`
	Avatar       string    `json:"avatar"`
	CursorPos    int       `json:"cursorPosition"`
	IsActive     bool      `json:"isActive"`
	JoinedAt     time.Time `json:"joinedAt"`
	LastActivity time.Time `json:"lastActivity"`
}

// ListParticipantsResponse 参与者列表响应
type ListParticipantsResponse struct {
	Participants []ArticleParticipantResponse `json:"participants"`
	Total        int                          `json:"total"`
}
