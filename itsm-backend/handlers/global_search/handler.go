package global_search

import (
	"strings"

	"itsm-backend/common"
	"itsm-backend/domain/role"
	"itsm-backend/middleware"

	"github.com/gin-gonic/gin"
)

// Handler 全局搜索HTTP处理器
type Handler struct {
	service *Service
}

// NewHandler creates a new global search handler
func NewHandler(service *Service) *Handler {
	return &Handler{service: service}
}

// SearchResult 搜索结果
type SearchResult struct {
	ID           int    `json:"id"`
	Type         string `json:"type"`
	Title        string `json:"title"`
	Description  string `json:"description,omitempty"`
	Status       string `json:"status,omitempty"`
	TicketNumber string `json:"ticketNumber,omitempty"`
	// Code 项目编号（type=project/project_task 时返回）。
	Code string `json:"code,omitempty"`
	// ProjectID/ProjectName 所属项目（type=project_task 时返回，供前端跳转）。
	ProjectID   int    `json:"projectId,omitempty"`
	ProjectName string `json:"projectName,omitempty"`
}

// SearchResponse 搜索响应
type SearchResponse struct {
	Results []*SearchResult `json:"results"`
	Total   int             `json:"total"`
}

// Search 全局搜索
// @Summary 全局搜索
// @Description 在工单、事件、问题、变更、知识库、项目、项目任务中搜索（项目/任务受可见性谓词约束）
// @Tags 全局搜索
// @Produce json
// @Param q query string true "搜索关键词"
// @Success 200 {object} common.Response
// @Router /api/v1/global-search [get]
func (h *Handler) Search(ctx *gin.Context) {
	keyword := strings.TrimSpace(ctx.Query("q"))
	if keyword == "" {
		common.Success(ctx, &SearchResponse{Results: []*SearchResult{}, Total: 0})
		return
	}

	tenantID, ok := middleware.TenantIDOrUnauthorized(ctx)
	if !ok {
		return
	}
	if tenantID == 0 {
		common.Fail(ctx, common.AuthFailedCode, "租户信息缺失")
		return
	}

	response, err := h.service.Search(
		ctx.Request.Context(), tenantID,
		ctx.GetInt("user_id"), isAdminLike(middleware.GetContextRoles(ctx)),
		keyword,
	)
	if err != nil {
		common.Fail(ctx, common.InternalErrorCode, "搜索失败")
		return
	}

	common.Success(ctx, response)
}

// isAdminLike 管理角色判定（单一源：domain/role.IsAdminLike，与项目域可见性口径一致）。
func isAdminLike(roles []string) bool {
	for _, r := range roles {
		if role.IsAdminLike(r) {
			return true
		}
	}
	return false
}

// RegisterRoutes 注册路由
func (h *Handler) RegisterRoutes(r *gin.RouterGroup) {
	search := r.Group("/global-search")
	{
		search.GET("", h.Search)
	}
}
