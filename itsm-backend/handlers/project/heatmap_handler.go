package project

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"

	"itsm-backend/common"
	"itsm-backend/common/tenantctx"
	"itsm-backend/middleware"

	"github.com/gin-gonic/gin"
)

// 资源负荷（周容量配置 + 只读热图）HTTP 端点（P2，PE-23）。
// 权威设计：docs/project-management/plan/p2-resource-heatmap-design.md。

// UpdateCapacity 容量配置
// @Summary 周容量配置（项目默认 + 成员覆盖；缺省=不修改，null/0=清空）
// @Description 成员须为项目存活成员；取值 (0,168] 小时；审计 capacity_update（未变化不留痕）
// @Tags 项目管理
// @Accept json
// @Produce json
// @Param id path int true "项目ID"
// @Param body body CapacityUpdateRequest true "容量请求"
// @Success 200 {object} common.Response{data=CapacityResponse}
// @Router /api/v1/projects/{id}/capacity [put]
func (h *Handler) UpdateCapacity(c *gin.Context) {
	tenantID, ok := middleware.TenantIDOrUnauthorized(c)
	if !ok {
		return
	}
	id, ok := parseID(c, "id")
	if !ok {
		return
	}
	var req CapacityUpdateRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		common.Fail(c, common.ParamErrorCode, "参数错误："+err.Error())
		return
	}
	before, err := h.service.Capacity(c.Request.Context(), tenantID, id)
	if err != nil {
		h.fail(c, err)
		return
	}
	after, err := h.service.UpdateCapacity(c.Request.Context(), tenantID, id, &req)
	if err != nil {
		h.fail(c, err)
		return
	}
	if diff := capacityDiff(before, after); len(diff) > 0 {
		h.writeCapacityAudit(c, tenantID, c.GetInt("user_id"), id, "capacity_update", diff)
	}
	common.Success(c, after)
}

// ResourceHeatmap 资源负荷热图
// @Summary 资源负荷只读热图（周维度）
// @Description 口径：任务估算按计划日期自然日均摊、剩余=estimate−actual 均摊（逾期落本周）；周对齐 ISO 周；可见性同详情（不可见 404）
// @Tags 项目管理
// @Produce json
// @Param id path int true "项目ID"
// @Param start query string false "窗口起始（YYYY-MM-DD 或 RFC3339；缺省=本周，snap 到周一）"
// @Param weeks query int false "窗口周数 1..12（缺省 6）"
// @Success 200 {object} common.Response{data=ResourceHeatmap}
// @Router /api/v1/projects/{id}/resource-heatmap [get]
func (h *Handler) ResourceHeatmap(c *gin.Context) {
	tenantID, ok := middleware.TenantIDOrUnauthorized(c)
	if !ok {
		return
	}
	id, ok := parseID(c, "id")
	if !ok {
		return
	}
	start, err := parseHeatmapStart(c.Query("start"))
	if err != nil {
		common.Fail(c, common.ParamErrorCode, err.Error())
		return
	}
	weeks := 0
	if raw := strings.TrimSpace(c.Query("weeks")); raw != "" {
		v, err := strconv.Atoi(raw)
		if err != nil || v < HeatmapMinWeeks || v > HeatmapMaxWeeks {
			common.Fail(c, common.ParamErrorCode, fmt.Sprintf("参数错误：weeks 应在 %d~%d", HeatmapMinWeeks, HeatmapMaxWeeks))
			return
		}
		weeks = v
	}
	result, err := h.service.ResourceHeatmap(
		c.Request.Context(), tenantID, id,
		c.GetInt("user_id"), isAdminViewer(middleware.GetContextRoles(c)),
		start, weeks,
	)
	if err != nil {
		h.fail(c, err)
		return
	}
	common.Success(c, result)
}

// parseHeatmapStart 解析窗口起始（空=本周；支持 YYYY-MM-DD 与 RFC3339）。
func parseHeatmapStart(raw string) (*time.Time, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, nil
	}
	if d, err := time.Parse("2006-01-02", raw); err == nil {
		return &d, nil
	}
	if d, err := time.Parse(time.RFC3339, raw); err == nil {
		return &d, nil
	}
	return nil, fmt.Errorf("参数错误：start 需为 YYYY-MM-DD 或 RFC3339")
}

// capacityDiff 容量变更明细（old/new；无变化返回空 map → 不写审计）。
func capacityDiff(before, after *CapacityResponse) map[string]any {
	out := map[string]any{}
	if !floatPtrEqual(before.DefaultWeeklyCapacityHours, after.DefaultWeeklyCapacityHours) {
		out["defaultWeeklyCapacityHours"] = gin.H{
			"old": before.DefaultWeeklyCapacityHours,
			"new": after.DefaultWeeklyCapacityHours,
		}
	}
	beforeByUser := make(map[int]*float64, len(before.Members))
	for _, m := range before.Members {
		beforeByUser[m.UserID] = m.WeeklyCapacityHours
	}
	changes := make([]gin.H, 0, len(after.Members))
	for _, m := range after.Members {
		if floatPtrEqual(beforeByUser[m.UserID], m.WeeklyCapacityHours) {
			continue
		}
		changes = append(changes, gin.H{"userId": m.UserID, "old": beforeByUser[m.UserID], "new": m.WeeklyCapacityHours})
	}
	if len(changes) > 0 {
		out["members"] = changes
	}
	return out
}

// writeCapacityAudit 容量审计（resource=project，action=capacity_update；2s 超时、失败不阻断业务）。
func (h *Handler) writeCapacityAudit(c *gin.Context, tenantID, userID, projectID int, action string, metadata map[string]any) {
	if h.audit == nil {
		return
	}
	payload, _ := json.Marshal(metadata)
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
		SetAction(action).
		SetPath(fmt.Sprintf("/api/v1/projects/%d/capacity", projectID)).
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