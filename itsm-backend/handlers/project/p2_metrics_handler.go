package project

import (
	"fmt"
	"strings"
	"time"

	"itsm-backend/common"
	"itsm-backend/middleware"

	"github.com/gin-gonic/gin"
)

// ---------------------------------------------------------------------------
// P2 PE-24 组合视图与度量 Handler（thin：参数解析 + 响应封装；业务在 Service）
// ---------------------------------------------------------------------------

// portfolioListFilter 组合/度量共用的项目过滤（与列表同参数，D-40 口径同源）。
func portfolioListFilter(c *gin.Context, tenantID int) ListFilter {
	return ListFilter{
		TenantID:    tenantID,
		Status:      strings.TrimSpace(c.Query("status")),
		Type:        strings.TrimSpace(c.Query("type")),
		OwnerTeamID: atoiOrZero(c.Query("ownerTeamId")),
		ManagerID:   atoiOrZero(c.Query("managerId")),
		Keyword:     strings.TrimSpace(c.Query("q")),
	}
}

// Portfolio 项目组合视图
// @Summary 项目组合视图（按状态/健康度/负责人/部门/类型聚合 + 与列表一致的项目行）
// @Description 与 GET /projects 同过滤、同可见性、同行结构（口径同源，供对账）；projects 行上限 500，超过 truncated=true。
// @Tags 项目管理
// @Produce json
// @Success 200 {object} common.Response{data=PortfolioResponse}
// @Router /api/v1/projects/portfolio [get]
func (h *Handler) Portfolio(c *gin.Context) {
	tenantID, ok := middleware.TenantIDOrUnauthorized(c)
	if !ok {
		return
	}
	resp, err := h.service.Portfolio(
		c.Request.Context(), tenantID,
		c.GetInt("user_id"), isAdminViewer(middleware.GetContextRoles(c)),
		portfolioListFilter(c, tenantID),
	)
	if err != nil {
		h.fail(c, err)
		return
	}
	common.Success(c, resp)
}

// Metrics 项目组合度量
// @Summary 项目组合度量（按时交付率/里程碑达成率/阶段门一次通过率/资源利用率 + 明细）
// @Description 与 GET /projects 同过滤、同可见性；from/to 为口径日期时间窗（可选），start/weeks 为资源窗口（缺省本周/6 周，1..12）；明细上限 200/组（分子分母恒为全量）。
// @Tags 项目管理
// @Produce json
// @Success 200 {object} common.Response{data=MetricsResponse}
// @Router /api/v1/projects/metrics [get]
func (h *Handler) Metrics(c *gin.Context) {
	tenantID, ok := middleware.TenantIDOrUnauthorized(c)
	if !ok {
		return
	}
	from, err := parseDayParam(c.Query("from"), "from")
	if err != nil {
		common.Fail(c, common.ParamErrorCode, err.Error())
		return
	}
	to, err := parseDayParam(c.Query("to"), "to")
	if err != nil {
		common.Fail(c, common.ParamErrorCode, err.Error())
		return
	}
	start, err := parseHeatmapStart(c.Query("start"))
	if err != nil {
		common.Fail(c, common.ParamErrorCode, err.Error())
		return
	}
	resp, err := h.service.Metrics(
		c.Request.Context(), tenantID,
		c.GetInt("user_id"), isAdminViewer(middleware.GetContextRoles(c)),
		portfolioListFilter(c, tenantID),
		from, to, start, atoiOrZero(c.Query("weeks")),
	)
	if err != nil {
		h.fail(c, err)
		return
	}
	common.Success(c, resp)
}

// parseDayParam 解析 YYYY-MM-DD（空 = 不限）。
func parseDayParam(raw, name string) (*time.Time, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, nil
	}
	d, err := time.Parse("2006-01-02", raw)
	if err != nil {
		return nil, fmt.Errorf("参数错误：%s 需为 YYYY-MM-DD", name)
	}
	return &d, nil
}