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

// 跨租户单据流转 P2-①：模板分发（copy-in）HTTP 层。
// 契约见 docs/multi-tenant/plan/msp-cross-tenant-record-flow-architecture.md §7.2。

// workflowDistributionService 抽象模板分发服务（handler 契约测试注入替身用）。
type workflowDistributionService interface {
	Distribute(ctx context.Context, actor service.MSPWorkbenchActor, customerTenantID int, key, version string) (*dto.DistributeWorkflowTemplateResponse, error)
	ListDistributions(ctx context.Context, actor service.MSPWorkbenchActor, customerTenantID int) (*dto.WorkflowTemplateDistributionListResponse, error)
}

// DistributeWorkflowTemplate 将 provider 已发布模板分发到客户租户（创建草稿副本 + 来源追踪）。
// POST /api/v1/msp/customers/:customer_tenant_id/workflow-templates/:key/distribute
func (h *Handler) DistributeWorkflowTemplate(c *gin.Context) {
	customerID, err := strconv.Atoi(c.Param("customer_tenant_id"))
	if err != nil || customerID <= 0 {
		common.Fail(c, common.ParamErrorCode, "无效的客户租户 ID")
		return
	}
	actor, ok := workbenchActorFrom(c)
	if !ok {
		common.Fail(c, common.ForbiddenCode, "仅 MSP 员工可分发模板")
		return
	}
	key := c.Param("key")
	if key == "" {
		common.Fail(c, common.ParamErrorCode, "模板 key 不能为空")
		return
	}
	var req dto.DistributeWorkflowTemplateRequest
	if c.Request != nil && c.Request.ContentLength > 0 {
		if err := c.ShouldBindJSON(&req); err != nil {
			common.Fail(c, common.ParamErrorCode, "参数错误: "+err.Error())
			return
		}
	}
	if h.workflowDistribution == nil {
		common.Fail(c, common.ServiceUnavailableCode, "模板分发服务未配置")
		return
	}
	resp, err := h.workflowDistribution.Distribute(c.Request.Context(), actor, customerID, key, req.Version)
	if err != nil {
		switch {
		case errors.Is(err, service.ErrWorkflowTemplateNotFound):
			common.Fail(c, common.NotFoundCode, "模板不存在或尚未发布")
		case errors.Is(err, service.ErrWorkflowTemplateConflict):
			common.Fail(c, common.ConflictCode, "客户租户已存在同 key 草稿，请先在客户租户内处理")
		case errors.Is(err, service.ErrWorkflowTemplateInvalid):
			common.Fail(c, common.ParamErrorCode, "分发参数无效")
		default:
			failMSPAccess(c, err, "模板分发失败")
		}
		return
	}
	common.Success(c, resp)
}

// ListCustomerWorkflowTemplates 分发状态（provider 视角：来源追踪 + 升级提示）。
// GET /api/v1/msp/customers/:customer_tenant_id/workflow-templates
func (h *Handler) ListCustomerWorkflowTemplates(c *gin.Context) {
	customerID, err := strconv.Atoi(c.Param("customer_tenant_id"))
	if err != nil || customerID <= 0 {
		common.Fail(c, common.ParamErrorCode, "无效的客户租户 ID")
		return
	}
	actor, ok := workbenchActorFrom(c)
	if !ok {
		common.Fail(c, common.ForbiddenCode, "仅 MSP 员工可查看分发状态")
		return
	}
	if h.workflowDistribution == nil {
		common.Fail(c, common.ServiceUnavailableCode, "模板分发服务未配置")
		return
	}
	resp, err := h.workflowDistribution.ListDistributions(c.Request.Context(), actor, customerID)
	if err != nil {
		failMSPAccess(c, err, "查询分发状态失败")
		return
	}
	common.Success(c, resp)
}
