package knowledge

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"go.uber.org/zap"
	"itsm-backend/common"
	"itsm-backend/common/knowledgecontent"
	"itsm-backend/dto"
	"itsm-backend/ent"
	"itsm-backend/ent/permission"
	"itsm-backend/handlers/common/knowledgeaccess"
	"itsm-backend/internal/commandbus"
	"itsm-backend/service"
)

type Service struct {
	repo   Repository
	logger *zap.SugaredLogger
	// 向量索引同步（可选注入）：发布→索引，取消发布/软删除→移除向量。
	// nil 时跳过同步，RAG 仍可退化为关键字搜索。
	rag *service.RAGService
	// client 用于访问权限表（knowledgeaccess 的纳管记录存在 permission 表）。
	// 经 SetEntClient 注入；未注入时纳管接口返回明确错误而非静默失败。
	client *ent.Client
	// knowledgeGuard 知识分类可见性守卫（L0 权限边界）。
	knowledgeGuard *knowledgeaccess.Guard
	// freshness 时效性判定器（L1 正确性边界）。
	// 与 RAG 服务内的判定器共用同一套策略，此处用于 RAG 未装配时的检索兜底路径。
	freshness *knowledgeaccess.FreshnessJudger
	// attachmentLifecycle BE-8：文章软删后级联软删其通用附件（保留物理文件待保留期回收）。
	// 由 bootstrap 在 attachment.cleanup_enabled 打开时注入；nil 时行为与改造前一致。
	attachmentLifecycle service.AttachmentLifecycleCascader
}

func NewService(repo Repository, logger *zap.SugaredLogger) *Service {
	return &Service{
		repo:   repo,
		logger: logger,
		// 默认装配时效判定器，保证即便调用方忘记注入，检索也不会引用过期知识。
		freshness: knowledgeaccess.NewFreshnessJudger(knowledgeaccess.DefaultFreshnessPolicy(), nil),
	}
}

// SetEntClient 注入 ent 客户端，启用知识分类纳管能力。
func (s *Service) SetEntClient(client *ent.Client) { s.client = client }

// SetKnowledgeGuard 注入知识分类可见性守卫，用于纳管后失效缓存。
func (s *Service) SetKnowledgeGuard(g *knowledgeaccess.Guard) { s.knowledgeGuard = g }

// SetFreshnessJudger 注入时效性判定器（L1）。传 nil 表示关闭时效过滤。
func (s *Service) SetFreshnessJudger(j *knowledgeaccess.FreshnessJudger) { s.freshness = j }

// SetAttachmentLifecycle 注入附件生命周期级联器（BE-8）。传 nil 表示关闭级联。
func (s *Service) SetAttachmentLifecycle(c service.AttachmentLifecycleCascader) {
	s.attachmentLifecycle = c
}

// MarkArticleReviewed 记录一次内容复核。
//
// L1 时效管控的闭环动作：声明了复核周期的知识一旦逾期，会从 RAG 检索结果中消失，
// 复核是把它恢复回来的唯一途径。只更新复核时间，不改正文，
// 因此复核动作不可能夹带未经审校的内容改动。
func (s *Service) MarkArticleReviewed(ctx context.Context, id int, tenantID int) (*Article, error) {
	a, err := s.repo.MarkReviewed(ctx, id, tenantID)
	if err != nil {
		return nil, err
	}
	s.logger.Infow("知识内容复核完成",
		"article_id", id, "tenant_id", tenantID, "last_reviewed_at", a.LastReviewedAt)
	return a, nil
}

// filterCitable 按时效性过滤检索结果（L1）。
//
// 注意这里必须与管理端列表区分开：
// ListArticles 要看到已失效、待复核的知识，否则管理员无从治理；
// 而检索是给 LLM 引用的入口，逾期内容一旦进来就会变成带来源的错误答案。
// 本方法只服务「检索」语义，不要把它接到管理端列表上。
func (s *Service) filterCitable(articles []*Article) []*Article {
	if s.freshness == nil || len(articles) == 0 {
		return articles
	}
	kept := make([]*Article, 0, len(articles))
	for _, a := range articles {
		if a == nil {
			continue
		}
		if s.freshness.Citable(knowledgeaccess.FreshnessFields{
			ValidFrom:          a.ValidFrom,
			ValidUntil:         a.ValidUntil,
			LastReviewedAt:     a.LastReviewedAt,
			ReviewIntervalDays: a.ReviewIntervalDays,
		}) {
			kept = append(kept, a)
		}
	}
	return kept
}

// SetRAG wires the optional RAG service for vector index synchronization.
func (s *Service) SetRAG(rag *service.RAGService) {
	s.rag = rag
}

// vectorIndexAction is persisted in the outbox. The consumer reloads the
// article by ID and tenant, so the payload never contains article content.
const (
	vectorIndexSync   = "sync"
	vectorIndexDelete = "delete"
)

func (s *Service) vectorOutboxEnabled() bool { return s.rag != nil && s.client != nil }

func enqueueVectorSyncTx(ctx context.Context, tx *ent.Tx, article *Article, action string) error {
	if article == nil || article.ID <= 0 || article.TenantID <= 0 {
		return fmt.Errorf("knowledge vector outbox requires a tenant-scoped article")
	}
	occurrence := article.UpdatedAt.UnixNano()
	if occurrence <= 0 {
		occurrence = article.CreatedAt.UnixNano()
	}
	returnError := func() error {
		_, err := commandbus.EnqueueTx(ctx, tx, commandbus.EnqueueRequest{
			TenantID: article.TenantID, CommandType: commandbus.CommandSyncKnowledgeVector,
			AggregateType: "knowledge_article", AggregateID: article.ID,
			IdempotencyKey: fmt.Sprintf("knowledge-vector:%d:%d:%s:%d", article.TenantID, article.ID, action, occurrence),
			Payload:        map[string]interface{}{"action": action},
		})
		return err
	}
	return returnError()
}

func (s *Service) CreateArticle(ctx context.Context, a *Article) (*Article, error) {
	// XSS 消毒：Title 走 strict（纯文本），Content 走 UGC（保留富文本白名单，剥离 script/on*/javascript:）
	a.Title = common.SanitizeText(a.Title)
	a.Content = common.SanitizeHTML(a.Content)
	if err := normalizeArticleContentType(a); err != nil {
		return nil, err
	}
	s.validateInlineImageRefs(ctx, a)
	s.logger.Infow("Creating Knowledge Article", "title", a.Title, "category", a.Category)
	if s.vectorOutboxEnabled() {
		created, err := s.createWithVectorOutbox(ctx, a)
		if err != nil {
			return nil, err
		}
		s.snapshotVersion(ctx, created, "初始版本")
		return created, nil
	}
	created, err := s.repo.Create(ctx, a)
	if err != nil {
		return nil, err
	}
	s.snapshotVersion(ctx, created, "初始版本")
	return created, nil
}

func (s *Service) GetArticle(ctx context.Context, id int, tenantID int) (*Article, error) {
	return s.repo.Get(ctx, id, tenantID)
}

func (s *Service) ListArticles(ctx context.Context, tenantID int, page, size int, category, search, status string) ([]*Article, int, error) {
	return s.repo.List(ctx, tenantID, page, size, category, search, status)
}

func (s *Service) UpdateArticle(ctx context.Context, a *Article) (*Article, error) {
	// XSS 消毒
	a.Title = common.SanitizeText(a.Title)
	a.Content = common.SanitizeHTML(a.Content)
	if err := normalizeArticleContentType(a); err != nil {
		return nil, err
	}
	s.validateInlineImageRefs(ctx, a)
	s.logger.Infow("Updating Knowledge Article", "id", a.ID, "title", a.Title)

	// 版本历史写入侧：更新成功后把「新正文」记一条版本，保证最新版本 = 当前正文。
	// 快照失败不阻断更新，只告警（正文一致性优先）。
	if s.vectorOutboxEnabled() {
		updated, err := s.updateWithVectorOutbox(ctx, a)
		if err != nil {
			return nil, err
		}
		s.snapshotVersion(ctx, updated, "文章更新")
		return updated, nil
	}
	updated, err := s.repo.Update(ctx, a)
	if err != nil {
		return nil, err
	}
	s.snapshotVersion(ctx, updated, "文章更新")
	return updated, nil
}

// snapshotVersion 记录一次版本快照（在文章写入成功后调用，快照内容即当时正文）；
// 版本历史属辅助能力，失败只告警、不阻断主流程。
func (s *Service) snapshotVersion(ctx context.Context, a *Article, summary string) {
	if a == nil || a.ID <= 0 || a.TenantID <= 0 {
		return
	}
	if _, err := s.repo.CreateVersion(ctx, a.ID, a.TenantID, summary); err != nil {
		s.logger.Warnw("保存文章版本快照失败", "error", err, "id", a.ID, "tenant_id", a.TenantID)
	}
}

// ListArticleVersions 返回文章的全部历史版本（按版本号倒序）。
func (s *Service) ListArticleVersions(ctx context.Context, articleID, tenantID int) ([]*ArticleVersion, error) {
	return s.repo.ListVersions(ctx, articleID, tenantID)
}

// RestoreArticleVersion 将文章恢复到指定历史版本（恢复动作本身也会留一份快照）。
func (s *Service) RestoreArticleVersion(ctx context.Context, articleID, version, tenantID int) (*Article, error) {
	restored, err := s.repo.RestoreVersion(ctx, articleID, version, tenantID)
	if err != nil {
		return nil, err
	}
	s.logger.Infow("知识文章已恢复到历史版本", "article_id", articleID, "version", version, "tenant_id", tenantID)
	return restored, nil
}

// CompareArticleVersions 比较两个历史版本的正文差异。
//
// 差异口径为「按行的多重集合差」：目标版本多出的行即新增，源版本独有行即删除。
// 不做行对齐/相似度匹配（不产出 modified），对版本审阅而言信息足够且无额外依赖。
func (s *Service) CompareArticleVersions(ctx context.Context, articleID, fromVersion, toVersion, tenantID int) (*dto.KnowledgeArticleVersionCompareResponse, error) {
	from, err := s.repo.GetVersion(ctx, articleID, fromVersion, tenantID)
	if err != nil {
		return nil, err
	}
	to, err := s.repo.GetVersion(ctx, articleID, toVersion, tenantID)
	if err != nil {
		return nil, err
	}

	removed, added := diffLines(from.Content, to.Content)

	result := &dto.KnowledgeArticleVersionCompareResponse{
		FromVersion: fromVersion,
		ToVersion:   toVersion,
		Changes:     make([]dto.KnowledgeArticleVersionChange, 0, len(removed)+len(added)),
	}
	var b strings.Builder
	fmt.Fprintf(&b, "--- v%d\n+++ v%d\n", fromVersion, toVersion)
	for _, line := range removed {
		result.Changes = append(result.Changes, dto.KnowledgeArticleVersionChange{Type: "removed", Content: line})
		b.WriteString("- ")
		b.WriteString(line)
		b.WriteString("\n")
	}
	for _, line := range added {
		result.Changes = append(result.Changes, dto.KnowledgeArticleVersionChange{Type: "added", Content: line})
		b.WriteString("+ ")
		b.WriteString(line)
		b.WriteString("\n")
	}
	result.Diff = b.String()
	return result, nil
}

// diffLines 计算两个文本按行比较的新增/删除行，保持各自原始行序。
func diffLines(from, to string) (removed []string, added []string) {
	fromLines := splitContentLines(from)
	toLines := splitContentLines(to)

	// 统计源文本行出现次数，用于抵消目标文本中的重复行。
	remaining := make(map[string]int, len(fromLines))
	for _, line := range fromLines {
		remaining[line]++
	}
	for _, line := range toLines {
		if remaining[line] > 0 {
			remaining[line]--
			continue
		}
		added = append(added, line)
	}

	// 反向抵消，得到源文本中独有的行（删除）。
	matched := make(map[string]int, len(toLines))
	for _, line := range toLines {
		matched[line]++
	}
	for _, line := range fromLines {
		if matched[line] > 0 {
			matched[line]--
			continue
		}
		removed = append(removed, line)
	}
	return removed, added
}

func splitContentLines(content string) []string {
	if content == "" {
		return nil
	}
	return strings.Split(strings.ReplaceAll(content, "\r\n", "\n"), "\n")
}

// normalizeArticleContentType 校验显式传入的正文类型并落到最终生效值。
//
// 规则：
//   - 显式值非法 → 报错（避免脏值落库后在渲染端变成未知分支）；
//   - 显式值合法 → 直接采用（用户明确声明优先，哪怕与内容形态不符）；
//   - 留空     → 按内容形态兜底判定，并把结果写回，使存量文章在编辑后逐步带上类型。
//
// 判定口径与前端 lib/knowledge/article-content-type.ts 保持一致。
func normalizeArticleContentType(a *Article) error {
	explicit, err := knowledgecontent.Parse(a.ContentType)
	if err != nil {
		return err
	}
	a.ContentType = knowledgecontent.Resolve(explicit, a.Content)
	return nil
}

// validateInlineImageRefs BE-7：知识库正文的内嵌图片引用必须归属当前文章
// （新建时 a.ID=0，任何能解析到通用附件记录的引用都属于他人附件）；
// ent 客户端未注入或查询失败时保持原内容，不阻塞写入。
func (s *Service) validateInlineImageRefs(ctx context.Context, a *Article) {
	if s.client == nil || a == nil {
		return
	}
	validated, violations, err := service.ValidateRichTextInlineRefs(
		ctx, s.client, a.TenantID, service.AttachmentBizTypeKnowledgeArticle, a.ID, a.Content)
	if err != nil {
		s.logger.Warnw("内嵌图片引用校验失败，按清洗结果落库",
			"error", err, "tenant_id", a.TenantID, "article_id", a.ID)
		return
	}
	a.Content = validated
	for _, v := range violations {
		s.logger.Warnw("剥离越权内嵌图片引用",
			"tenant_id", a.TenantID,
			"biz_type", service.AttachmentBizTypeKnowledgeArticle,
			"biz_id", a.ID,
			"attachment_id", v.AttachmentID,
			"reason", v.Reason,
			"src", v.Src,
		)
	}
}

func (s *Service) DeleteArticle(ctx context.Context, id int, tenantID int) error {
	s.logger.Infow("Deleting Knowledge Article", "id", id)
	var err error
	if s.vectorOutboxEnabled() {
		err = s.deleteWithVectorOutbox(ctx, id, tenantID)
	} else {
		err = s.repo.Delete(ctx, id, tenantID)
	}
	if err != nil {
		return err
	}
	s.cascadeAttachments(ctx, id, tenantID)
	return nil
}

// cascadeAttachments BE-8：文章软删成功后级联软删其通用附件。
// 级联器未注入（灰度关闭）时跳过；级联失败只告警，不影响文章删除结果。
func (s *Service) cascadeAttachments(ctx context.Context, id, tenantID int) {
	if s.attachmentLifecycle == nil {
		return
	}
	n, err := s.attachmentLifecycle.CascadeHostDeletion(ctx, tenantID, service.AttachmentBizTypeKnowledgeArticle, id)
	if err != nil {
		s.logger.Warnw("knowledge attachment cascade failed",
			"article_id", id, "tenant_id", tenantID, "error", err)
		return
	}
	if n > 0 {
		s.logger.Infow("knowledge attachments cascaded",
			"article_id", id, "tenant_id", tenantID, "cascaded", n)
	}
}

func (s *Service) GetCategories(ctx context.Context, tenantID int) ([]string, error) {
	return s.repo.GetCategories(ctx, tenantID)
}

// SearchArticles performs a RAG-powered search over published knowledge articles.
// It first retrieves articles via the RAG service (vector + keyword hybrid search),
// then fetches the full article records from the database to return complete content.
// Results are sorted by relevance score descending.
func (s *Service) SearchArticles(ctx context.Context, tenantID int, query string, category string, limit int) ([]*Article, error) {
	if limit <= 0 {
		limit = 20
	}
	if limit > 50 {
		limit = 50
	}

	// If RAG service is wired, use it for real vector + keyword retrieval.
	if s.rag != nil {
		rawResults, err := s.rag.Ask(ctx, tenantID, query, limit)
		if err != nil {
			s.logger.Warnw("KnowledgeService: RAG Ask failed, falling back to keyword", "query", query, "error", err)
			// Fall through to keyword-only path via repo
		} else {
			if len(rawResults) == 0 {
				return []*Article{}, nil
			}

			// Collect article IDs from RAG results, preserving order (by relevance).
			scored := make([]struct {
				id    int
				score float64
			}, 0, len(rawResults))
			for _, r := range rawResults {
				if id, ok := r["id"].(int); ok {
					score, _ := r["score"].(float64)
					scored = append(scored, struct {
						id    int
						score float64
					}{id: id, score: score})
				}
			}

			if len(scored) == 0 {
				return []*Article{}, nil
			}

			// Fetch full articles from DB in a single call.
			articles, err := s.repo.GetByIDs(ctx, tenantID, scoredIDsToIDs(scored))
			if err != nil {
				s.logger.Warnw("KnowledgeService: failed to fetch articles by IDs", "error", err)
				return nil, err
			}

			// Re-order by RAG score descending and attach scores.
			scoreMap := make(map[int]float64, len(scored))
			for _, s := range scored {
				scoreMap[s.id] = s.score
			}
			sorted := make([]*Article, 0, len(articles))
			for _, a := range articles {
				if score, ok := scoreMap[a.ID]; ok {
					a.RelevanceScore = score
					sorted = append(sorted, a)
				}
			}
			// Sort by score descending
			for i := 0; i < len(sorted)-1; i++ {
				for j := i + 1; j < len(sorted); j++ {
					if sorted[j].RelevanceScore > sorted[i].RelevanceScore {
						sorted[i], sorted[j] = sorted[j], sorted[i]
					}
				}
			}
			return sorted, nil
		}
	}

	// No RAG service: fall back to plain ListArticles with keyword search.
	articles, _, err := s.repo.List(ctx, tenantID, 1, limit, category, query, "published")
	if err != nil {
		return nil, err
	}
	// RAG 未装配时，这条路径就是知识检索的唯一入口，时效性必须在这里拦住，
	// 否则未部署向量库的环境会整体绕过 L1。
	return s.filterCitable(articles), nil
}

func scoredIDsToIDs(scored []struct {
	id    int
	score float64
},
) []int {
	ids := make([]int, len(scored))
	for i, s := range scored {
		ids[i] = s.id
	}
	return ids
}

func (s *Service) GetStats(ctx context.Context, tenantID int) (*dto.KnowledgeStatsResponse, error) {
	stats, err := s.repo.GetStats(ctx, tenantID)
	if err != nil {
		return nil, err
	}

	// Calculate average rating based on total likes / total articles
	// Since we only have likes (not a 1-5 star rating), we'll use likes as a proxy for rating
	var avgRating float64
	if stats.Total > 0 {
		avgRating = float64(stats.TotalLikes) / float64(stats.Total)
	}

	// Convert categories to DTO format
	categoryStats := make([]dto.CategoryStats, 0, len(stats.Categories))
	for _, cat := range stats.Categories {
		categoryStats = append(categoryStats, dto.CategoryStats{
			Name:  cat.Name,
			Count: int(cat.Count),
		})
	}

	return &dto.KnowledgeStatsResponse{
		Total:      int(stats.Total),
		Published:  int(stats.Published),
		Draft:      int(stats.Draft),
		Views:      stats.TotalViews,
		Rating:     avgRating,
		Categories: categoryStats,
	}, nil
}

// ===== 知识分类可见性纳管（L0 权限边界） =====
//
// 纳管语义：为某分类在 RBAC permission 表写入一条
// resource="knowledge_category" / action="read:<分类>" 记录。
// 写入即纳管——此后该分类下的知识仅对持有该权限的角色可见。
// 这是「企业内部知识可引用性」的地基：没有它，上层任何检索优化
// 都是在给越权内容做更好的曝光。

// ListRestrictedCategories 返回当前租户已被纳管的知识分类。
func (s *Service) ListRestrictedCategories(ctx context.Context, tenantID int) ([]string, error) {
	if s.client == nil {
		return nil, fmt.Errorf("知识分类纳管能力未装配（ent client 缺失）")
	}
	perms, err := s.client.Permission.Query().
		Where(permission.TenantIDEQ(tenantID), permission.ResourceEQ(knowledgeaccess.CategoryResource)).
		All(ctx)
	if err != nil {
		return nil, fmt.Errorf("查询受限分类失败: %w", err)
	}
	cats := make([]string, 0, len(perms))
	for _, p := range perms {
		if !strings.HasPrefix(p.Action, knowledgeaccess.ActionPrefix) {
			continue
		}
		if c := strings.TrimSpace(p.Action[len(knowledgeaccess.ActionPrefix):]); c != "" {
			cats = append(cats, c)
		}
	}
	sort.Strings(cats)
	return cats, nil
}

// SetCategoryRestriction 纳管（restricted=true）或解除纳管（false）某个知识分类。
//
// 纳管时若该分类无任何文章，仍然允许——管理员可提前规划分类权限模型。
// 解除纳管即删除对应 permission 记录，该分类恢复为全租户可读。
func (s *Service) SetCategoryRestriction(ctx context.Context, tenantID int, category string, restricted bool) error {
	if s.client == nil {
		return fmt.Errorf("知识分类纳管能力未装配（ent client 缺失）")
	}
	category = strings.TrimSpace(category)
	if category == "" {
		return fmt.Errorf("分类名不能为空")
	}

	action := knowledgeaccess.ActionForCategory(category)

	if !restricted {
		n, err := s.client.Permission.Delete().
			Where(
				permission.TenantIDEQ(tenantID),
				permission.ResourceEQ(knowledgeaccess.CategoryResource),
				permission.ActionEQ(action),
			).Exec(ctx)
		if err != nil {
			return fmt.Errorf("解除纳管失败: %w", err)
		}
		s.logger.Infow("知识分类已解除纳管", "tenant_id", tenantID, "category", category, "deleted", n)
		if s.knowledgeGuard != nil {
			s.knowledgeGuard.Invalidate(tenantID)
		}
		return nil
	}

	// 纳管：已存在则幂等跳过，避免重复记录干扰权限判定
	exists, err := s.client.Permission.Query().
		Where(
			permission.TenantIDEQ(tenantID),
			permission.ResourceEQ(knowledgeaccess.CategoryResource),
			permission.ActionEQ(action),
		).Exist(ctx)
	if err != nil {
		return fmt.Errorf("校验纳管状态失败: %w", err)
	}
	if !exists {
		_, err = s.client.Permission.Create().
			SetCode(knowledgeaccess.CategoryResource + "_" + category).
			SetName("知识分类-" + category).
			SetDescription("知识分类可见性：仅授权角色可读该分类下的知识（AI 检索同样受控）").
			SetResource(knowledgeaccess.CategoryResource).
			SetAction(action).
			SetTenantID(tenantID).Save(ctx)
		if err != nil {
			return fmt.Errorf("纳管分类失败: %w", err)
		}
		s.logger.Infow("知识分类已纳管", "tenant_id", tenantID, "category", category)
	}

	if s.knowledgeGuard != nil {
		s.knowledgeGuard.Invalidate(tenantID)
	}
	return nil
}
