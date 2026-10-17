package project

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"itsm-backend/common"
	"itsm-backend/common/tenantctx"
	"itsm-backend/ent"
	"itsm-backend/middleware"

	"github.com/gin-gonic/gin"
)

// Handler 项目管理 HTTP 处理器（thin：参数解析 + 响应封装；业务在 Service）。
//
// 权威定义：docs/project-management/plan/p0-api-and-pages.md §1.1。
type Handler struct {
	service *Service
	// audit 审计客户端（PE-12；bootstrap 注入，未注入时跳过审计写入）。
	audit *ent.Client
}

// NewHandler 构造处理器。
func NewHandler(service *Service) *Handler {
	return &Handler{service: service}
}

// CreateProject 创建项目
// @Summary 创建项目
// @Description 创建项目（code 缺省自动生成 PRJ-YYYYMMDD-XXXX；新项目从 planning 起步）
// @Tags 项目管理
// @Accept json
// @Produce json
// @Param body body CreateRequest true "创建请求"
// @Success 200 {object} common.Response{data=ProjectResponse}
// @Router /api/v1/projects [post]
func (h *Handler) CreateProject(c *gin.Context) {
	tenantID, ok := middleware.TenantIDOrUnauthorized(c)
	if !ok {
		return
	}
	var req CreateRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		common.Fail(c, common.ParamErrorCode, "参数错误："+err.Error())
		return
	}
	req.TenantID = tenantID
	created, err := h.service.Create(c.Request.Context(), &req)
	if err != nil {
		h.fail(c, err)
		return
	}
	common.Success(c, ToResponseWithOwnerTeams(
		created,
		h.service.OwnerTeamRefs(c.Request.Context(), tenantID, []*ent.Project{created}),
	))
}

// ListProjects 项目列表
// @Summary 项目列表
// @Description 分页 + 过滤：status/type/ownerTeamId/managerId/q
// @Tags 项目管理
// @Produce json
// @Success 200 {object} common.Response{data=common.ListResponse}
// @Router /api/v1/projects [get]
func (h *Handler) ListProjects(c *gin.Context) {
	tenantID, ok := middleware.TenantIDOrUnauthorized(c)
	if !ok {
		return
	}
	page := common.GetPaginationFromQuery(c)
	filter := ListFilter{
		TenantID:    tenantID,
		Status:      strings.TrimSpace(c.Query("status")),
		Type:        strings.TrimSpace(c.Query("type")),
		OwnerTeamID: atoiOrZero(c.Query("ownerTeamId")),
		ManagerID:   atoiOrZero(c.Query("managerId")),
		Keyword:     strings.TrimSpace(c.Query("q")),
		Offset:      page.GetOffset(),
		Limit:       page.GetLimit(),
	}
	items, total, err := h.service.ListForViewer(
		c.Request.Context(), filter,
		c.GetInt("user_id"), isAdminViewer(middleware.GetContextRoles(c)),
	)
	if err != nil {
		h.fail(c, err)
		return
	}
	common.Success(c, common.NewListResponse(
		ToResponseListWithOwnerTeams(items, h.service.OwnerTeamRefs(c.Request.Context(), tenantID, items)),
		common.NewPaginationResponse(page.Page, page.PageSize, total),
	))
}

// GetProject 项目详情
// @Summary 项目详情
// @Tags 项目管理
// @Produce json
// @Param id path int true "项目ID"
// @Success 200 {object} common.Response{data=ProjectResponse}
// @Router /api/v1/projects/{id} [get]
func (h *Handler) GetProject(c *gin.Context) {
	tenantID, ok := middleware.TenantIDOrUnauthorized(c)
	if !ok {
		return
	}
	id, ok := parseID(c, "id")
	if !ok {
		return
	}
	item, err := h.service.GetForViewer(
		c.Request.Context(), tenantID, id,
		c.GetInt("user_id"), isAdminViewer(middleware.GetContextRoles(c)),
	)
	if err != nil {
		h.fail(c, err)
		return
	}
	common.Success(c, ToResponseWithOwnerTeams(
		item,
		h.service.OwnerTeamRefs(c.Request.Context(), tenantID, []*ent.Project{item}),
	))
}

// HoursSummary 项目任务工时汇总（PE-22）
// @Summary 项目任务工时汇总
// @Description 口径：项目全部未删任务的 estimate_hours/actual_hours 求和（不做状态过滤）；可见性同项目详情（不可见 404）
// @Tags 项目管理
// @Produce json
// @Param id path int true "项目ID"
// @Success 200 {object} common.Response{data=HoursSummaryResponse}
// @Router /api/v1/projects/{id}/hours-summary [get]
func (h *Handler) HoursSummary(c *gin.Context) {
	tenantID, ok := middleware.TenantIDOrUnauthorized(c)
	if !ok {
		return
	}
	id, ok := parseID(c, "id")
	if !ok {
		return
	}
	summary, err := h.service.HoursSummary(
		c.Request.Context(), tenantID, id,
		c.GetInt("user_id"), isAdminViewer(middleware.GetContextRoles(c)),
	)
	if err != nil {
		h.fail(c, err)
		return
	}
	common.Success(c, ToHoursSummaryResponse(summary))
}

// UpdateProject 更新项目
// @Summary 更新项目（基础字段；不含状态）
// @Tags 项目管理
// @Accept json
// @Produce json
// @Param id path int true "项目ID"
// @Param body body UpdateRequest true "更新请求（version 可选乐观锁）"
// @Success 200 {object} common.Response{data=ProjectResponse}
// @Router /api/v1/projects/{id} [put]
func (h *Handler) UpdateProject(c *gin.Context) {
	tenantID, ok := middleware.TenantIDOrUnauthorized(c)
	if !ok {
		return
	}
	id, ok := parseID(c, "id")
	if !ok {
		return
	}
	var req UpdateRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		common.Fail(c, common.ParamErrorCode, "参数错误："+err.Error())
		return
	}
	// PE-22：预算变更留痕（仅在本次提交含预算字段时读取旧值，避免额外查询）。
	var oldBudget *ProjectResponse
	if req.BudgetAmount != nil || req.BudgetCurrency != nil {
		if current, err := h.service.Get(c.Request.Context(), tenantID, id); err == nil {
			oldBudget = ToResponse(current)
		}
	}
	updated, err := h.service.Update(c.Request.Context(), tenantID, id, &req)
	if err != nil {
		h.fail(c, err)
		return
	}
	h.writeBudgetAudit(c, tenantID, c.GetInt("user_id"), id, oldBudget, ToResponse(updated))
	common.Success(c, ToResponseWithOwnerTeams(
		updated,
		h.service.OwnerTeamRefs(c.Request.Context(), tenantID, []*ent.Project{updated}),
	))
}

// DeleteProject 删除项目（软删；存在关联任务/单据时 409）
// @Summary 删除项目
// @Tags 项目管理
// @Produce json
// @Param id path int true "项目ID"
// @Success 200 {object} common.Response
// @Router /api/v1/projects/{id} [delete]
func (h *Handler) DeleteProject(c *gin.Context) {
	tenantID, ok := middleware.TenantIDOrUnauthorized(c)
	if !ok {
		return
	}
	id, ok := parseID(c, "id")
	if !ok {
		return
	}
	if err := h.service.Delete(c.Request.Context(), tenantID, id); err != nil {
		h.fail(c, err)
		return
	}
	common.Success(c, gin.H{"deleted": true})
}

// TransitionStatus 项目状态流转
// @Summary 状态流转（activate|suspend|resume|complete|cancel）
// @Description complete 走结项门禁（未终结任务/单据返回 409 + blockers）；cancel 需 reason
// @Tags 项目管理
// @Accept json
// @Produce json
// @Param id path int true "项目ID"
// @Param body body StatusRequest true "流转请求"
// @Success 200 {object} common.Response{data=ProjectResponse}
// @Router /api/v1/projects/{id}/status [post]
func (h *Handler) TransitionStatus(c *gin.Context) {
	tenantID, ok := middleware.TenantIDOrUnauthorized(c)
	if !ok {
		return
	}
	id, ok := parseID(c, "id")
	if !ok {
		return
	}
	var req StatusRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		common.Fail(c, common.ParamErrorCode, "参数错误："+err.Error())
		return
	}
	updated, err := h.service.Transition(c.Request.Context(), tenantID, id, &req)
	if err != nil {
		// PE-15 结项门禁拒绝留痕（含任务/单据/阶段门/结项清单阻塞明细）。
		var closure *ClosureBlockedError
		if errors.As(err, &closure) && req.Action == ActionComplete {
			h.writeClosureAudit(c, tenantID, c.GetInt("user_id"), id, "closure_blocked", map[string]any{
				"reason":   "closure_blocked",
				"blockers": simplifyClosureBlockersForAudit(closure.Blockers),
			})
		}
		h.fail(c, err)
		return
	}
	common.Success(c, ToResponse(updated))
}

// ---------------------------------------------------------------------------
// 内部工具
// ---------------------------------------------------------------------------

func parseID(c *gin.Context, param string) (int, bool) {
	id, err := strconv.Atoi(c.Param(param))
	if err != nil || id <= 0 {
		common.Fail(c, common.ParamErrorCode, "无效的ID")
		return 0, false
	}
	return id, true
}

func atoiOrZero(raw string) int {
	if strings.TrimSpace(raw) == "" {
		return 0
	}
	v, err := strconv.Atoi(strings.TrimSpace(raw))
	if err != nil || v <= 0 {
		return 0
	}
	return v
}

// fail 统一错误映射（409 家族带 data 明细，供前端展示阻塞项）。
func (h *Handler) fail(c *gin.Context, err error) {
	var closure *ClosureBlockedError
	if errors.As(err, &closure) {
		common.FailWithData(c, common.ConflictCode, "结项门禁未通过（任务/单据/阶段门/结项清单存在阻塞项）", gin.H{
			"blockers": closure.Blockers,
			"total":    len(closure.Blockers),
		})
		return
	}
	var del *DeleteBlockedError
	if errors.As(err, &del) {
		common.FailWithData(c, common.ConflictCode, "存在关联任务或单据，无法删除项目", gin.H{
			"blockers": del.Blockers,
			"total":    len(del.Blockers),
		})
		return
	}
	var cas *CASConflictError
	if errors.As(err, &cas) {
		common.FailWithData(c, common.ConflictCode, "项目已被其他操作更新，请刷新后重试", gin.H{
			"currentVersion": cas.CurrentVersion,
		})
		return
	}
	var templateApplied *TemplateAlreadyAppliedError
	if errors.As(err, &templateApplied) {
		common.FailWithData(c, common.ConflictCode, "该项目已应用模板，不可重复应用（幂等：未产生重复数据）", gin.H{
			"templateId":   templateApplied.TemplateID,
			"templateName": templateApplied.TemplateName,
			"appliedAt":    templateApplied.AppliedAt,
			"hint":         "apply_once_per_project",
		})
		return
	}
	var templateCAS *TemplateVersionConflictError
	if errors.As(err, &templateCAS) {
		common.FailWithData(c, common.ConflictCode, "模板已被其他操作更新，请刷新后重试", gin.H{
			"currentVersion": templateCAS.CurrentVersion,
		})
		return
	}
	var gateBlocked *GateBlockedError
	if errors.As(err, &gateBlocked) {
		common.FailWithData(c, common.ConflictCode, "阶段门门禁未通过", gin.H{
			"gate":     gateBlocked.Gate,
			"blockers": gateBlocked.Blockers,
			"total":    len(gateBlocked.Blockers),
		})
		return
	}
	var reportCAS *ReportTemplateCASError
	if errors.As(err, &reportCAS) {
		common.FailWithData(c, common.ConflictCode, "报告模板已被其他操作更新，请刷新后重试", gin.H{
			"currentVersion": reportCAS.CurrentVersion,
		})
		return
	}
	var riskConverted *RiskAlreadyConvertedError
	if errors.As(err, &riskConverted) {
		common.FailWithData(c, common.ConflictCode, "该风险/问题已转换，不可重复转换", gin.H{
			"target":     riskConverted.Target,
			"existingId": riskConverted.ExistingID,
		})
		return
	}
	var riskPartial *RiskConvertPartialError
	if errors.As(err, &riskPartial) {
		common.FailWithData(c, common.InternalErrorCode, "单据已创建但关联回写失败，请人工关联", gin.H{
			"target":    riskPartial.Target,
			"createdId": riskPartial.CreatedID,
		})
		return
	}
	switch {
	case errors.Is(err, ErrNotFound):
		common.Fail(c, common.NotFoundCode, "项目不存在")
	case errors.Is(err, ErrCodeExists):
		common.Fail(c, common.ConflictCode, "项目代码已存在")
	case errors.Is(err, ErrInvalidTransition):
		common.Fail(c, common.ConflictCode, "非法状态流转："+err.Error())
	case errors.Is(err, ErrValidation):
		common.Fail(c, common.ParamErrorCode, err.Error())
	case errors.Is(err, ErrReasonRequired):
		common.Fail(c, common.ParamErrorCode, "取消项目需要填写原因")
	case errors.Is(err, ErrNoUpdateFields):
		common.Fail(c, common.ParamErrorCode, "没有可更新的字段")
	case errors.Is(err, ErrMemberExists):
		common.Fail(c, common.ConflictCode, "成员已存在")
	case errors.Is(err, ErrMemberNotFound):
		common.Fail(c, common.NotFoundCode, "成员不存在")
	case errors.Is(err, ErrMemberNotInTenant):
		common.Fail(c, common.ConflictCode, "该用户在本租户无有效成员身份，无法加入项目")
	case errors.Is(err, ErrManagerCannotBeRemoved):
		common.FailWithData(c, common.ConflictCode, "项目经理不可从成员中移除，请先变更项目经理", gin.H{
			"hint": "change_manager_first",
		})
	case errors.Is(err, ErrInvalidProjectRole):
		common.Fail(c, common.ParamErrorCode, "无效的项目角色（pm|member|stakeholder）")
	case errors.Is(err, ErrOwnerTeamUnavailable):
		common.Fail(c, common.ParamErrorCode, "归口团队不可用（不存在/已删除/已停用）")
	case errors.Is(err, ErrReportTemplateNotFound):
		common.Fail(c, common.NotFoundCode, "报告模板不存在")
	case errors.Is(err, ErrReportTemplateExists):
		common.Fail(c, common.ConflictCode, "报告模板名称已存在")
	case errors.Is(err, ErrReportTemplateArchived):
		common.Fail(c, common.ConflictCode, "报告模板未启用（仅 active 模板可生成报告）")
	case errors.Is(err, ErrTaskNotFound):
		common.Fail(c, common.NotFoundCode, "项目任务不存在")
	case errors.Is(err, ErrTaskDepthExceeded):
		common.Fail(c, common.ParamErrorCode, "任务层级超过上限（≤3 层）")
	case errors.Is(err, ErrTaskParentCrossProject):
		common.Fail(c, common.ParamErrorCode, "父任务不属于当前项目")
	case errors.Is(err, ErrTaskCycle):
		common.Fail(c, common.ConflictCode, "任务层级存在环")
	case errors.Is(err, ErrTaskHasChildren):
		common.Fail(c, common.ConflictCode, "存在子任务，无法删除")
	case errors.Is(err, ErrTaskInvalidTransition):
		common.Fail(c, common.ConflictCode, "非法任务状态流转："+err.Error())
	case errors.Is(err, ErrAssigneeNotMember):
		common.Fail(c, common.ConflictCode, "负责人不是项目活跃成员")
	case errors.Is(err, ErrRecordNotFound):
		common.Fail(c, common.NotFoundCode, "单据不存在")
	case errors.Is(err, ErrRecordDomainInvalid):
		common.Fail(c, common.ParamErrorCode, "不支持的单据类型")
	case errors.Is(err, ErrProjectWriteRequired):
		common.Fail(c, common.ForbiddenCode, "缺少项目写权限")
	case errors.Is(err, ErrTaskTicketExists):
		common.Fail(c, common.ConflictCode, "任务已关联工单，请先解绑")
	case errors.Is(err, ErrTaskTicketNotLinked):
		common.Fail(c, common.ConflictCode, "任务未关联工单")
	case errors.Is(err, ErrTicketGeneratorNotConfigured):
		common.Fail(c, common.ServiceUnavailableCode, "工单生成服务未配置")
	case errors.Is(err, ErrClosurePortUnavailable):
		common.Fail(c, common.ServiceUnavailableCode, "复盘知识库/满意度调研服务未配置")
	case errors.Is(err, ErrAssetLinkInvalidType):
		common.Fail(c, common.ParamErrorCode, "无效的登记类型（asset|asset_license|vendor|contract）")
	case errors.Is(err, ErrAssetLinkInvalidTarget):
		common.Fail(c, common.ParamErrorCode, "登记目标不存在（或不属于当前租户）")
	case errors.Is(err, ErrAssetLinkDuplicate):
		common.FailWithData(c, common.ConflictCode, "该目标已登记，请勿重复登记", gin.H{
			"hint": "duplicate_link",
		})
	case errors.Is(err, ErrAssetLinkNotFound):
		common.Fail(c, common.NotFoundCode, "登记不存在或已删除")
	case errors.Is(err, ErrMilestoneNotFound):
		common.Fail(c, common.NotFoundCode, "里程碑不存在")
	case errors.Is(err, ErrInvalidMilestoneState):
		common.Fail(c, common.ConflictCode, "非法里程碑状态流转："+err.Error())
	case errors.Is(err, ErrMilestoneOwnerNotMember):
		common.Fail(c, common.ConflictCode, "里程碑负责人不是项目活跃成员")
	case errors.Is(err, ErrDependencySelf):
		common.Fail(c, common.ParamErrorCode, "任务不能依赖自身")
	case errors.Is(err, ErrDependencyExists):
		common.Fail(c, common.ConflictCode, "依赖已存在")
	case errors.Is(err, ErrDependencyCycle):
		common.Fail(c, common.ConflictCode, "依赖存在环")
	case errors.Is(err, ErrDependencyNotFound):
		common.Fail(c, common.NotFoundCode, "依赖不存在")
	case errors.Is(err, ErrBaselineNotFound):
		common.Fail(c, common.NotFoundCode, "项目尚无计划基线")
	case errors.Is(err, ErrBaselineVersionConflict):
		common.Fail(c, common.ConflictCode, "基线版本冲突，请刷新后重试")
	case errors.Is(err, ErrTemplateNotFound):
		common.Fail(c, common.NotFoundCode, "项目模板不存在")
	case errors.Is(err, ErrTemplateNameExists):
		common.Fail(c, common.ConflictCode, "模板名称已存在")
	case errors.Is(err, ErrTemplateNotActive):
		common.Fail(c, common.ConflictCode, "模板未激活，无法应用（请先激活模板）")
	case errors.Is(err, ErrTemplateInUse):
		common.FailWithData(c, common.ConflictCode, "模板已被项目应用，无法删除（可改为归档）", gin.H{
			"hint": "archive_instead",
		})
	case errors.Is(err, ErrGateAlreadyPassed):
		common.Fail(c, common.ConflictCode, "阶段门已通过，无需重复提交")
	case errors.Is(err, ErrGateInReview):
		common.Fail(c, common.ConflictCode, "阶段门审批中，请等待裁决")
	case errors.Is(err, ErrGateNoApproval):
		common.Fail(c, common.ConflictCode, "阶段门不在审批中，无法裁决")
	case errors.Is(err, ErrGateApprovalFailed):
		common.Fail(c, common.ConflictCode, "流程审批推进失败："+err.Error())
	case errors.Is(err, ErrRiskNotFound):
		common.Fail(c, common.NotFoundCode, "风险/问题不存在")
	case errors.Is(err, ErrInvalidRiskState):
		common.Fail(c, common.ConflictCode, "非法风险状态流转："+err.Error())
	case errors.Is(err, ErrRiskResolutionRequired):
		common.Fail(c, common.ParamErrorCode, "关闭风险/问题必须填写结论（resolution）")
	case errors.Is(err, ErrRiskOwnerNotMember):
		common.Fail(c, common.ConflictCode, "风险负责人不是项目活跃成员")
	case errors.Is(err, ErrRiskTicketNotFound):
		common.Fail(c, common.ParamErrorCode, "关联工单不存在（须为同租户工单）")
	case errors.Is(err, ErrRiskPortUnavailable):
		common.Fail(c, common.ServiceUnavailableCode, "问题单/变更单服务未配置")
	case errors.Is(err, ErrRiskConversionFailed):
		common.Fail(c, common.InternalErrorCode, "转换失败："+err.Error())
	default:
		common.Fail(c, common.InternalErrorCode, err.Error())
	}
}

// writeBudgetAudit 预算变更审计（PE-22；resource=project, action=budget_update；2s 超时、失败不阻断业务）。
// 仅在金额/币种实际变化时写入；before/after 任一缺失（读取失败）时跳过。
func (h *Handler) writeBudgetAudit(c *gin.Context, tenantID, userID, projectID int, before, after *ProjectResponse) {
	if h.audit == nil || before == nil || after == nil {
		return
	}
	if floatPtrEqual(before.BudgetAmount, after.BudgetAmount) && stringPtrEqual(before.BudgetCurrency, after.BudgetCurrency) {
		return
	}
	payload, _ := json.Marshal(map[string]any{
		"projectId":   projectID,
		"oldAmount":   before.BudgetAmount,
		"oldCurrency": before.BudgetCurrency,
		"newAmount":   after.BudgetAmount,
		"newCurrency": after.BudgetCurrency,
	})

	auditCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if tenantID > 0 {
		auditCtx = tenantctx.WithTenantID(auditCtx, tenantID)
	} else {
		auditCtx = tenantctx.WithSystemBypass(auditCtx)
	}
	create := h.audit.AuditLog.Create().
		SetCreatedAt(time.Now()).
		SetResource("project").
		SetAction("budget_update").
		SetPath(fmt.Sprintf("/api/v1/projects/%d", projectID)).
		SetMethod(c.Request.Method).
		SetStatusCode(200).
		SetRequestBody(string(payload)).
		SetSource("workbench").
		SetIP(c.ClientIP()).
		SetRequestID(c.GetString("request_id"))
	if userID > 0 {
		create = create.SetUserID(userID)
	}
	if tenantID > 0 {
		create = create.SetTenantID(tenantID)
	}
	if account := c.GetString("username"); account != "" {
		create = create.SetActorAccount(account)
	}
	_ = create.Exec(auditCtx)
}

// floatPtrEqual 指针浮点等值（nil 与 nil 相等；nil 与非 nil 不等）。
func floatPtrEqual(a, b *float64) bool {
	if a == nil || b == nil {
		return a == nil && b == nil
	}
	return *a == *b
}

// stringPtrEqual 指针字符串等值（nil 与 nil 相等；nil 与非 nil 不等）。
func stringPtrEqual(a, b *string) bool {
	if a == nil || b == nil {
		return a == nil && b == nil
	}
	return *a == *b
}
