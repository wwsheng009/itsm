package ai

import (
	"itsm-backend/common"
	"itsm-backend/internal/helpindex"
	"itsm-backend/service"

	"github.com/gin-gonic/gin"
)

// P2 文档命中率/解决率运营报表（GET /api/v1/ai/observability/doc-metrics）。
//
// 契约（方案 §9 帮助度量 / §13.3 运营层 / §13.6）：
//  1. 遥测聚合（service.GetAssistantDocMetrics）：文档命中率（未产生 help_gap 的会话占比）、
//     解决率（未升级人工率）、反馈解决率、按页 Top（缺口优先）、按天缺口收敛趋势；
//  2. 账本覆盖率（helpindex，零遥测环境也有基础面）。
//
// 降级：埋点服务未装配/查询失败 → 200 + telemetry.available=false（覆盖率照常渲染，不 5xx）。
func (h *Handler) GetDocMetrics(c *gin.Context) {
	tenantID := c.GetInt("tenant_id")
	if tenantID == 0 {
		common.Fail(c, common.AuthFailedCode, "租户信息缺失")
		return
	}

	days := queryInt(c, "days", 30)

	report := &service.AssistantDocMetricsReport{
		Days:   days,
		ByPage: []service.AssistantDocMetricsPage{},
		Trend:  []service.AssistantDocMetricsTrendPoint{},
	}
	telemetryAvailable := true
	if h.svc == nil {
		telemetryAvailable = false
	} else {
		result, err := h.svc.GetAssistantDocMetrics(c.Request.Context(), tenantID, days)
		if err != nil {
			telemetryAvailable = false
			if h.svc.logger != nil {
				h.svc.logger.Warnw("doc metrics telemetry unavailable", "error", err, "tenantID", tenantID)
			}
		} else {
			report = result
		}
	}

	idx := helpindex.Load()
	coverage, coverageTotals := idx.Coverage()

	common.Success(c, gin.H{
		"days":        report.Days,
		"totals":      report.Totals,
		"rates":       report.Rates,
		"byPage":      report.ByPage,
		"trend":       report.Trend,
		"generatedAt": report.GeneratedAt,
		"coverage": gin.H{
			"coverage": coverage,
			"totals":   coverageTotals,
		},
		"telemetry": gin.H{"available": telemetryAvailable},
	})
}
