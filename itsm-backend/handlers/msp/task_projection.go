package msp

import (
	"context"
	"errors"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"

	"itsm-backend/common"
	"itsm-backend/dto"
	"itsm-backend/service"
)

// 跨租户单据流转 P2-②：任务投影 HTTP 层（§7.3 / §8.5）。
// 授权 = Allocation ∩ 客户租户 RBAC（task:read/update）∩ 分配规则；写回在客户租户内执行。

// taskProjectionService 抽象任务投影服务（handler 契约测试注入替身用）。
type taskProjectionService interface {
	ListTasks(ctx context.Context, actor service.MSPWorkbenchActor, req dto.MSPTaskQuery) (*dto.MSPTaskListResponse, error)
	ClaimTask(ctx context.Context, actor service.MSPWorkbenchActor, taskID string, declaredTenantID int) (*dto.MSPTaskItem, error)
	CompleteTask(ctx context.Context, actor service.MSPWorkbenchActor, taskID string, declaredTenantID int, variables map[string]interface{}) error
}

// ListWorkbenchTasks 跨客户"待我处理"任务投影。
// GET /api/v1/msp/workbench/tasks?customerTenantIds=11,12&limit=50
func (h *Handler) ListWorkbenchTasks(c *gin.Context) {
	actor, ok := workbenchActorFrom(c)
	if !ok {
		common.Fail(c, common.ForbiddenCode, "仅 MSP 员工可查看跨租户任务")
		return
	}
	if h.taskProjection == nil {
		common.Fail(c, common.ServiceUnavailableCode, "任务投影服务未配置")
		return
	}
	req := dto.MSPTaskQuery{}
	if raw := strings.TrimSpace(c.Query("customerTenantIds")); raw != "" {
		ids, err := parseTenantIDCSV(raw)
		if err != nil {
			common.Fail(c, common.ParamErrorCode, "customerTenantIds 参数错误")
			return
		}
		req.CustomerTenantIDs = ids
	}
	if raw := strings.TrimSpace(c.Query("limit")); raw != "" {
		limit, err := strconv.Atoi(raw)
		if err != nil || limit < 0 {
			common.Fail(c, common.ParamErrorCode, "limit 参数错误")
			return
		}
		req.Limit = limit
	}
	resp, err := h.taskProjection.ListTasks(c.Request.Context(), actor, req)
	if err != nil {
		failMSPAccess(c, err, "查询跨租户任务失败")
		return
	}
	common.Success(c, resp)
}

// ClaimWorkbenchTask 认领任务（客户租户内写回）。
// POST /api/v1/msp/workbench/tasks/:taskId/claim
func (h *Handler) ClaimWorkbenchTask(c *gin.Context) {
	taskID := strings.TrimSpace(c.Param("taskId"))
	if taskID == "" {
		common.Fail(c, common.ParamErrorCode, "任务 ID 不能为空")
		return
	}
	actor, ok := workbenchActorFrom(c)
	if !ok {
		common.Fail(c, common.ForbiddenCode, "仅 MSP 员工可认领跨租户任务")
		return
	}
	if h.taskProjection == nil {
		common.Fail(c, common.ServiceUnavailableCode, "任务投影服务未配置")
		return
	}
	var req dto.MSPTaskClaimRequest
	if c.Request != nil && c.Request.ContentLength > 0 {
		if err := c.ShouldBindJSON(&req); err != nil {
			common.Fail(c, common.ParamErrorCode, "参数错误: "+err.Error())
			return
		}
	}
	item, err := h.taskProjection.ClaimTask(c.Request.Context(), actor, taskID, req.CustomerTenantID)
	if err != nil {
		if errors.Is(err, service.ErrMSPTaskNotFound) {
			common.Fail(c, common.NotFoundCode, "任务不存在或不在可见范围")
			return
		}
		failMSPAccess(c, err, "认领任务失败")
		return
	}
	common.Success(c, item)
}

// CompleteWorkbenchTask 完成任务并推进客户流程（客户租户内写回）。
// POST /api/v1/msp/workbench/tasks/:taskId/complete
func (h *Handler) CompleteWorkbenchTask(c *gin.Context) {
	taskID := strings.TrimSpace(c.Param("taskId"))
	if taskID == "" {
		common.Fail(c, common.ParamErrorCode, "任务 ID 不能为空")
		return
	}
	actor, ok := workbenchActorFrom(c)
	if !ok {
		common.Fail(c, common.ForbiddenCode, "仅 MSP 员工可完成跨租户任务")
		return
	}
	if h.taskProjection == nil {
		common.Fail(c, common.ServiceUnavailableCode, "任务投影服务未配置")
		return
	}
	var req dto.MSPTaskCompleteRequest
	if c.Request != nil && c.Request.ContentLength > 0 {
		if err := c.ShouldBindJSON(&req); err != nil {
			common.Fail(c, common.ParamErrorCode, "参数错误: "+err.Error())
			return
		}
	}
	if err := h.taskProjection.CompleteTask(c.Request.Context(), actor, taskID, req.CustomerTenantID, req.Variables); err != nil {
		if errors.Is(err, service.ErrMSPTaskNotFound) {
			common.Fail(c, common.NotFoundCode, "任务不存在或不在可见范围")
			return
		}
		failMSPAccess(c, err, "完成任务失败")
		return
	}
	common.Success(c, gin.H{"taskId": taskID, "status": "completed"})
}

// parseTenantIDCSV 解析逗号分隔的客户租户 ID 列表（去空、去重、正整数校验）。
func parseTenantIDCSV(raw string) ([]int, error) {
	parts := strings.Split(raw, ",")
	seen := make(map[int]bool, len(parts))
	out := make([]int, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		id, err := strconv.Atoi(p)
		if err != nil || id <= 0 {
			return nil, errors.New("invalid tenant id")
		}
		if seen[id] {
			continue
		}
		seen[id] = true
		out = append(out, id)
	}
	return out, nil
}
