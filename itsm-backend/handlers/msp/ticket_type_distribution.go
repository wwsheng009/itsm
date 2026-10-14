package msp

import (
	"context"
	"errors"
	"strconv"

	"github.com/gin-gonic/gin"

	"itsm-backend/common"
	"itsm-backend/dto"
	"itsm-backend/service"
)

// D-TT-1 工单类型受控下发（方案 §4.5a，P2-3）HTTP 层。
// 授权 = 端点级 RequireMSPPermission（msp_customer read/write）∩ 服务端 mspguard
// （allocation + 客户 active，唯一入口）；写路径审计 source=workbench。

// ticketTypeDistributionService 抽象受控下发服务（handler 契约测试注入替身用）。
type ticketTypeDistributionService interface {
	Distribute(ctx context.Context, actor service.MSPWorkbenchActor, customerTenantID, sourceTypeID int) (*dto.DistributeTicketTypeResponse, error)
	ListDistributions(ctx context.Context, actor service.MSPWorkbenchActor, customerTenantID int) ([]*dto.TicketTypeDistributionItem, error)
	Preview(ctx context.Context, actor service.MSPWorkbenchActor, customerTenantID, distributionID int) (*dto.TicketTypeDistributionPreview, error)
	Publish(ctx context.Context, actor service.MSPWorkbenchActor, customerTenantID, distributionID int) (*dto.PublishTicketTypeDistributionResponse, error)
	Unbind(ctx context.Context, actor service.MSPWorkbenchActor, customerTenantID, distributionID int) (*dto.UnbindTicketTypeDistributionResponse, error)
}

// DistributeTicketType 下发源类型到客户（copy-in 物化 + SLA binding 联动）。
// POST /api/v1/msp/customers/:customer_tenant_id/ticket-types/:id/distribute
func (h *Handler) DistributeTicketType(c *gin.Context) {
	customerID, ok := mspIntPathParam(c, "customer_tenant_id")
	if !ok {
		return
	}
	sourceTypeID, ok := mspIntPathParam(c, "id")
	if !ok {
		return
	}
	actor, ok := workbenchActorFrom(c)
	if !ok {
		common.Fail(c, common.ForbiddenCode, "仅 MSP 员工可下发工单类型")
		return
	}
	if h.ticketTypeDistribution == nil {
		common.Fail(c, common.ServiceUnavailableCode, "类型下发服务未配置")
		return
	}
	resp, err := h.ticketTypeDistribution.Distribute(c.Request.Context(), actor, customerID, sourceTypeID)
	if err != nil {
		respondTicketTypeDistributionError(c, err, "类型下发失败")
		return
	}
	common.Success(c, resp)
}

// ListTicketTypeDistributions 客户维度的分发列表（provider 视角，"有可用更新"提示）。
// GET /api/v1/msp/customers/:customer_tenant_id/ticket-types/distributions
func (h *Handler) ListTicketTypeDistributions(c *gin.Context) {
	customerID, ok := mspIntPathParam(c, "customer_tenant_id")
	if !ok {
		return
	}
	actor, ok := workbenchActorFrom(c)
	if !ok {
		common.Fail(c, common.ForbiddenCode, "仅 MSP 员工可查看类型分发")
		return
	}
	if h.ticketTypeDistribution == nil {
		common.Fail(c, common.ServiceUnavailableCode, "类型下发服务未配置")
		return
	}
	items, err := h.ticketTypeDistribution.ListDistributions(c.Request.Context(), actor, customerID)
	if err != nil {
		respondTicketTypeDistributionError(c, err, "查询类型分发失败")
		return
	}
	common.Success(c, gin.H{"items": items, "total": len(items)})
}

// PreviewTicketTypeDistribution 发布预览（受控字段 diff + SLA 变更）。
// GET /api/v1/msp/customers/:customer_tenant_id/ticket-types/distributions/:distId/preview
func (h *Handler) PreviewTicketTypeDistribution(c *gin.Context) {
	customerID, distID, actor, ok := h.ticketTypeDistributionContext(c)
	if !ok {
		return
	}
	resp, err := h.ticketTypeDistribution.Preview(c.Request.Context(), actor, customerID, distID)
	if err != nil {
		respondTicketTypeDistributionError(c, err, "预览发布失败")
		return
	}
	common.Success(c, resp)
}

// PublishTicketTypeDistribution 执行发布（INV-2 守卫 + INV-3 白名单；失败可重试）。
// POST /api/v1/msp/customers/:customer_tenant_id/ticket-types/distributions/:distId/publish
func (h *Handler) PublishTicketTypeDistribution(c *gin.Context) {
	customerID, distID, actor, ok := h.ticketTypeDistributionContext(c)
	if !ok {
		return
	}
	resp, err := h.ticketTypeDistribution.Publish(c.Request.Context(), actor, customerID, distID)
	if err != nil {
		respondTicketTypeDistributionError(c, err, "发布失败")
		return
	}
	common.Success(c, resp)
}

// UnbindTicketTypeDistribution 解绑：副本转 native（保留治理字段，受控字段解锁）。
// POST /api/v1/msp/customers/:customer_tenant_id/ticket-types/distributions/:distId/unbind
func (h *Handler) UnbindTicketTypeDistribution(c *gin.Context) {
	customerID, distID, actor, ok := h.ticketTypeDistributionContext(c)
	if !ok {
		return
	}
	resp, err := h.ticketTypeDistribution.Unbind(c.Request.Context(), actor, customerID, distID)
	if err != nil {
		respondTicketTypeDistributionError(c, err, "解绑失败")
		return
	}
	common.Success(c, resp)
}

// ticketTypeDistributionContext 解析 {customer_tenant_id, distId} + msp actor；任一失败已写响应。
func (h *Handler) ticketTypeDistributionContext(c *gin.Context) (customerID, distID int, actor service.MSPWorkbenchActor, ok bool) {
	customerID, ok = mspIntPathParam(c, "customer_tenant_id")
	if !ok {
		return
	}
	distID, ok = mspIntPathParam(c, "distId")
	if !ok {
		return
	}
	actor, ok = workbenchActorFrom(c)
	if !ok {
		common.Fail(c, common.ForbiddenCode, "仅 MSP 员工可操作类型分发")
		return
	}
	if h.ticketTypeDistribution == nil {
		common.Fail(c, common.ServiceUnavailableCode, "类型下发服务未配置")
		ok = false
		return
	}
	return customerID, distID, actor, true
}

// mspIntPathParam 解析正整数路径参数（失败响应并返回 false）。
func mspIntPathParam(c *gin.Context, name string) (int, bool) {
	v := c.Param(name)
	id, err := strconv.Atoi(v)
	if err != nil || id <= 0 {
		common.Fail(c, common.ParamErrorCode, "无效的参数: "+name)
		return 0, false
	}
	return id, true
}

// respondTicketTypeDistributionError 领域错误 → HTTP 映射（其余走 failMSPAccess 归因链）。
func respondTicketTypeDistributionError(c *gin.Context, err error, publicMsg string) {
	switch {
	case errors.Is(err, service.ErrTicketTypeDistributionConflict):
		common.Fail(c, common.ConflictCode, "该客户已存在同源类型的活跃分发；如需更新请发布，或先解绑后重新下发")
	case errors.Is(err, service.ErrTicketTypeSourceInvalid):
		common.Fail(c, common.ParamErrorCode, "源类型无效：需为当前服务商自有且非受控副本")
	case errors.Is(err, service.ErrTicketTypeDistributionInvalid):
		common.Fail(c, common.ParamErrorCode, "参数无效或状态不允许该操作")
	case errors.Is(err, service.ErrTicketTypeDistributionNotFound):
		common.Fail(c, common.NotFoundCode, "分发记录不存在")
	default:
		failMSPAccess(c, err, publicMsg)
	}
}
