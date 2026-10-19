package msp

import (
	"context"
	"errors"

	"github.com/gin-gonic/gin"

	"itsm-backend/common"
	"itsm-backend/dto"
	"itsm-backend/service"
)

// P2 MSP 多租户帮助空间与分发授权 HTTP 层。
//
// 授权：
//   - 帮助空间自有资产（/msp/help/docs*）= RequireMSPPermission("msp", read|write)（provider 库房）；
//   - 分发（/msp/customers/:id/help/**）= RequireMSPPermission("msp_customer", read|write)
//     ∩ 服务端 mspguard（allocation + 客户 active，唯一入口）；
//   - 写路径审计 source=workbench（service 层 best-effort 落 ai 审计表）。

// helpSpaceService 抽象帮助空间与分发服务（handler 契约测试注入替身用）。
type helpSpaceService interface {
	ListDocs(ctx context.Context, actor service.MSPWorkbenchActor) (*dto.MSPHelpDocListResponse, error)
	CreateDoc(ctx context.Context, actor service.MSPWorkbenchActor, req dto.MSPHelpDocRequest) (*dto.MSPHelpDoc, error)
	UpdateDoc(ctx context.Context, actor service.MSPWorkbenchActor, id int, req dto.MSPHelpDocRequest) (*dto.MSPHelpDoc, error)
	PublishDoc(ctx context.Context, actor service.MSPWorkbenchActor, id int) (*dto.MSPHelpDoc, error)
	DeleteDoc(ctx context.Context, actor service.MSPWorkbenchActor, id int) error
	ListDistributions(ctx context.Context, actor service.MSPWorkbenchActor, customerTenantID int) (*dto.MSPHelpDistributionListResponse, error)
	Distribute(ctx context.Context, actor service.MSPWorkbenchActor, customerTenantID, docID int) (*dto.MSPHelpDistribution, error)
	Upgrade(ctx context.Context, actor service.MSPWorkbenchActor, customerTenantID, distributionID int) (*dto.MSPHelpDistribution, error)
	Unbind(ctx context.Context, actor service.MSPWorkbenchActor, customerTenantID, distributionID int) (*dto.MSPHelpDistribution, error)
}

// respondHelpSpaceError 把帮助空间错误映射为稳定 HTTP 语义（授权错误走 failMSPAccess 统一入口）。
func respondHelpSpaceError(c *gin.Context, err error, publicMsg string) {
	switch {
	case errors.Is(err, service.ErrMSPHelpDocNotFound):
		common.Fail(c, common.NotFoundCode, "帮助文档或分发不存在")
	case errors.Is(err, service.ErrMSPHelpDocConflict):
		common.Fail(c, common.ConflictCode, "状态冲突：请先处理既有草稿/分发或确认版本")
	case errors.Is(err, service.ErrMSPHelpDocNotPublished):
		common.Fail(c, common.ConflictCode, "仅已发布文档可分发/升级")
	case errors.Is(err, service.ErrMSPHelpDocInvalid):
		common.Fail(c, common.ParamErrorCode, err.Error())
	default:
		failMSPAccess(c, err, publicMsg)
	}
}

// ListHelpDocs 帮助空间文档列表。
// GET /api/v1/msp/help/docs
func (h *Handler) ListHelpDocs(c *gin.Context) {
	actor, ok := workbenchActorFrom(c)
	if !ok {
		common.Fail(c, common.ForbiddenCode, "仅 MSP 员工可访问帮助空间")
		return
	}
	if h.helpSpace == nil {
		common.Fail(c, common.ServiceUnavailableCode, "帮助空间服务未配置")
		return
	}
	resp, err := h.helpSpace.ListDocs(c.Request.Context(), actor)
	if err != nil {
		respondHelpSpaceError(c, err, "查询帮助空间失败")
		return
	}
	common.Success(c, resp)
}

// CreateHelpDoc 创建帮助空间草稿。
// POST /api/v1/msp/help/docs
func (h *Handler) CreateHelpDoc(c *gin.Context) {
	actor, ok := workbenchActorFrom(c)
	if !ok {
		common.Fail(c, common.ForbiddenCode, "仅 MSP 员工可编辑帮助空间")
		return
	}
	if h.helpSpace == nil {
		common.Fail(c, common.ServiceUnavailableCode, "帮助空间服务未配置")
		return
	}
	var req dto.MSPHelpDocRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		common.ParamErrorWithErr(c, err, "请求参数错误")
		return
	}
	doc, err := h.helpSpace.CreateDoc(c.Request.Context(), actor, req)
	if err != nil {
		respondHelpSpaceError(c, err, "创建帮助文档失败")
		return
	}
	common.Success(c, doc)
}

// UpdateHelpDoc 更新帮助文档（已发布文档编辑回到 draft 并推进版本）。
// PUT /api/v1/msp/help/docs/:id
func (h *Handler) UpdateHelpDoc(c *gin.Context) {
	actor, ok := workbenchActorFrom(c)
	if !ok {
		common.Fail(c, common.ForbiddenCode, "仅 MSP 员工可编辑帮助空间")
		return
	}
	id, ok := mspIntPathParam(c, "id")
	if !ok {
		return
	}
	if h.helpSpace == nil {
		common.Fail(c, common.ServiceUnavailableCode, "帮助空间服务未配置")
		return
	}
	var req dto.MSPHelpDocRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		common.ParamErrorWithErr(c, err, "请求参数错误")
		return
	}
	doc, err := h.helpSpace.UpdateDoc(c.Request.Context(), actor, id, req)
	if err != nil {
		respondHelpSpaceError(c, err, "更新帮助文档失败")
		return
	}
	common.Success(c, doc)
}

// PublishHelpDoc 发布帮助文档（仅 published 可分发）。
// POST /api/v1/msp/help/docs/:id/publish
func (h *Handler) PublishHelpDoc(c *gin.Context) {
	actor, ok := workbenchActorFrom(c)
	if !ok {
		common.Fail(c, common.ForbiddenCode, "仅 MSP 员工可发布帮助文档")
		return
	}
	id, ok := mspIntPathParam(c, "id")
	if !ok {
		return
	}
	if h.helpSpace == nil {
		common.Fail(c, common.ServiceUnavailableCode, "帮助空间服务未配置")
		return
	}
	doc, err := h.helpSpace.PublishDoc(c.Request.Context(), actor, id)
	if err != nil {
		respondHelpSpaceError(c, err, "发布帮助文档失败")
		return
	}
	common.Success(c, doc)
}

// DeleteHelpDoc 删除帮助空间草稿（已发布文档不可删，避免误删分发来源）。
// DELETE /api/v1/msp/help/docs/:id
func (h *Handler) DeleteHelpDoc(c *gin.Context) {
	actor, ok := workbenchActorFrom(c)
	if !ok {
		common.Fail(c, common.ForbiddenCode, "仅 MSP 员工可删除帮助草稿")
		return
	}
	id, ok := mspIntPathParam(c, "id")
	if !ok {
		return
	}
	if h.helpSpace == nil {
		common.Fail(c, common.ServiceUnavailableCode, "帮助空间服务未配置")
		return
	}
	if err := h.helpSpace.DeleteDoc(c.Request.Context(), actor, id); err != nil {
		respondHelpSpaceError(c, err, "删除帮助草稿失败")
		return
	}
	common.Success(c, gin.H{"deleted": true})
}

// ListHelpDistributions 客户维度的帮助分发列表（provider 视角 + 升级提示）。
// GET /api/v1/msp/customers/:customer_tenant_id/help/distributions
func (h *Handler) ListHelpDistributions(c *gin.Context) {
	customerID, ok := mspIntPathParam(c, "customer_tenant_id")
	if !ok {
		return
	}
	actor, ok := workbenchActorFrom(c)
	if !ok {
		common.Fail(c, common.ForbiddenCode, "仅 MSP 员工可查看帮助分发")
		return
	}
	if h.helpSpace == nil {
		common.Fail(c, common.ServiceUnavailableCode, "帮助空间服务未配置")
		return
	}
	resp, err := h.helpSpace.ListDistributions(c.Request.Context(), actor, customerID)
	if err != nil {
		respondHelpSpaceError(c, err, "查询帮助分发失败")
		return
	}
	common.Success(c, resp)
}

// DistributeHelpDoc 分发帮助文档到客户租户（copy-in 快照 + 来源追踪）。
// POST /api/v1/msp/customers/:customer_tenant_id/help/docs/:id/distribute
func (h *Handler) DistributeHelpDoc(c *gin.Context) {
	customerID, ok := mspIntPathParam(c, "customer_tenant_id")
	if !ok {
		return
	}
	docID, ok := mspIntPathParam(c, "id")
	if !ok {
		return
	}
	actor, ok := workbenchActorFrom(c)
	if !ok {
		common.Fail(c, common.ForbiddenCode, "仅 MSP 员工可分发帮助文档")
		return
	}
	if h.helpSpace == nil {
		common.Fail(c, common.ServiceUnavailableCode, "帮助空间服务未配置")
		return
	}
	dist, err := h.helpSpace.Distribute(c.Request.Context(), actor, customerID, docID)
	if err != nil {
		respondHelpSpaceError(c, err, "帮助文档分发失败")
		return
	}
	common.Success(c, dto.DistributeMSPHelpDocResponse{Distribution: dist})
}

// UpgradeHelpDistribution 升级客户帮助副本到 provider 最新已发布版本（显式动作）。
// POST /api/v1/msp/customers/:customer_tenant_id/help/distributions/:distId/upgrade
func (h *Handler) UpgradeHelpDistribution(c *gin.Context) {
	customerID, distID, actor, ok := h.helpDistributionContext(c)
	if !ok {
		return
	}
	dist, err := h.helpSpace.Upgrade(c.Request.Context(), actor, customerID, distID)
	if err != nil {
		respondHelpSpaceError(c, err, "帮助副本升级失败")
		return
	}
	common.Success(c, dto.DistributeMSPHelpDocResponse{Distribution: dist})
}

// UnbindHelpDistribution 解绑客户帮助副本（转为客户原生内容，停止追踪 provider 更新）。
// POST /api/v1/msp/customers/:customer_tenant_id/help/distributions/:distId/unbind
func (h *Handler) UnbindHelpDistribution(c *gin.Context) {
	customerID, distID, actor, ok := h.helpDistributionContext(c)
	if !ok {
		return
	}
	dist, err := h.helpSpace.Unbind(c.Request.Context(), actor, customerID, distID)
	if err != nil {
		respondHelpSpaceError(c, err, "帮助副本解绑失败")
		return
	}
	common.Success(c, dto.DistributeMSPHelpDocResponse{Distribution: dist})
}

// helpDistributionContext 解析客户/分发行/操作者（升级与解绑共用前置）。
func (h *Handler) helpDistributionContext(c *gin.Context) (int, int, service.MSPWorkbenchActor, bool) {
	customerID, ok := mspIntPathParam(c, "customer_tenant_id")
	if !ok {
		return 0, 0, service.MSPWorkbenchActor{}, false
	}
	distID, ok := mspIntPathParam(c, "distId")
	if !ok {
		return 0, 0, service.MSPWorkbenchActor{}, false
	}
	actor, ok := workbenchActorFrom(c)
	if !ok {
		common.Fail(c, common.ForbiddenCode, "仅 MSP 员工可操作帮助分发")
		return 0, 0, service.MSPWorkbenchActor{}, false
	}
	if h.helpSpace == nil {
		common.Fail(c, common.ServiceUnavailableCode, "帮助空间服务未配置")
		return 0, 0, service.MSPWorkbenchActor{}, false
	}
	return customerID, distID, actor, true
}
