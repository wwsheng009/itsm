package project

import (
	"context"
	"time"

	"itsm-backend/dto"
	knowledgeHandler "itsm-backend/handlers/knowledge"
	"itsm-backend/service"
)

// 结项清单外呼端口（PE-15）：复盘 KB 草稿与 CSAT 调研。
//
// 端口定义在域内、适配器在本包（与 GateProcessAdapter 同先例），
// bootstrap 负责装配；未装配时对应动作返回 503（ErrClosurePortUnavailable），
// 但 CSAT 的结项自动触发为尽力而为（不阻塞结项）。

// RetroDraft 复盘草稿内容。
type RetroDraft struct {
	Title    string
	Content  string
	Category string
	Tags     []string
}

// CSATSurveyInput CSAT 调研创建入参。
type CSATSurveyInput struct {
	Title       string
	Description string
}

// ClosureKnowledgePort 复盘 KB 草稿端口。
type ClosureKnowledgePort interface {
	// CreateRetroDraft 创建草稿文章（未发布），返回文章ID。
	CreateRetroDraft(ctx context.Context, tenantID, authorID int, draft RetroDraft) (int, error)
}

// ClosureSurveyPort CSAT 调研端口。
type ClosureSurveyPort interface {
	// CreateCSATSurvey 创建 CSAT 调研，返回 surveyID。
	CreateCSATSurvey(ctx context.Context, tenantID int, input CSATSurveyInput) (int, error)
}

// SetClosurePorts 注入结项清单端口（bootstrap；测试可注入假实现）。
func (s *Service) SetClosurePorts(knowledge ClosureKnowledgePort, survey ClosureSurveyPort) {
	if knowledge != nil {
		s.closureKnowledge = knowledge
	}
	if survey != nil {
		s.closureSurvey = survey
	}
}

// ClosureKnowledgeAdapter 复盘 KB 草稿适配器（复用知识库域服务知识库域服务，
// 走生产链路：分类校验 + 消毒 + 新建即草稿）。
type ClosureKnowledgeAdapter struct {
	svc *knowledgeHandler.Service
}

// NewClosureKnowledgeAdapter 构造适配器。
func NewClosureKnowledgeAdapter(svc *knowledgeHandler.Service) *ClosureKnowledgeAdapter {
	return &ClosureKnowledgeAdapter{svc: svc}
}

// CreateRetroDraft 创建知识库草稿文章（is_published=false，人工确认后发布）。
func (a *ClosureKnowledgeAdapter) CreateRetroDraft(
	ctx context.Context, tenantID, authorID int, draft RetroDraft,
) (int, error) {
	if err := a.svc.ValidateCategory(ctx, tenantID, draft.Category); err != nil {
		return 0, err
	}
	article, err := a.svc.CreateArticle(ctx, &knowledgeHandler.Article{
		Title:       draft.Title,
		Content:     draft.Content,
		ContentType: "markdown",
		Category:    draft.Category,
		Tags:        draft.Tags,
		AuthorID:    authorID,
		TenantID:    tenantID,
	})
	if err != nil {
		return 0, err
	}
	return article.ID, nil
}

// ClosureSurveyAdapter CSAT 调研适配器（复用 service.SurveyService）。
type ClosureSurveyAdapter struct {
	svc *service.SurveyService
}

// NewClosureSurveyAdapter 构造适配器。
func NewClosureSurveyAdapter(svc *service.SurveyService) *ClosureSurveyAdapter {
	return &ClosureSurveyAdapter{svc: svc}
}

// CreateCSATSurvey 创建 CSAT 调研（1-5 评分 + 开放式意见，立即生效，30 天窗口）。
func (a *ClosureSurveyAdapter) CreateCSATSurvey(
	ctx context.Context, tenantID int, input CSATSurveyInput,
) (int, error) {
	start := time.Now()
	end := start.Add(30 * 24 * time.Hour)
	survey, err := a.svc.CreateSurvey(ctx, &dto.CreateSurveyRequest{
		Title:       input.Title,
		Description: input.Description,
		SurveyType:  "CSAT",
		IsActive:    true,
		StartDate:   &start,
		EndDate:     &end,
		Questions: []dto.Question{
			{Question: "您对本次项目交付的总体满意度？", Type: "rating", Required: true},
			{Question: "其他意见与建议（可选）", Type: "text", Required: false},
		},
	}, tenantID)
	if err != nil {
		return 0, err
	}
	return survey.ID, nil
}