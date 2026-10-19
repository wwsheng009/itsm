package ai

import (
	"strings"

	"itsm-backend/common"
	"itsm-backend/internal/helpindex"

	"github.com/gin-gonic/gin"
)

// H0-03 帮助检索与页面帮助（Help Agent 数据面）。
//
// 数据来源：构建期内嵌的帮助索引（docs/help 真源 → gen-page-manifest.mjs → internal/helpindex）。
// 权限：路由层挂 ai:read；本层只做参数归一化与降级：
//   - 搜索关键词为空 → 400（避免全量返回）；
//   - 未知 pageKey → status=unknown 的正常响应（面板帮助视图展示"暂无文档"并引导生成）。

// SearchHelp handles GET /api/v1/ai/help/search?q=&pageKey=&limit=
func (h *Handler) SearchHelp(c *gin.Context) {
	tenantID := c.GetInt("tenant_id")
	if tenantID == 0 {
		common.Fail(c, common.AuthFailedCode, "租户信息缺失")
		return
	}
	query := strings.TrimSpace(c.Query("q"))
	if query == "" {
		common.Fail(c, common.ParamErrorCode, "查询关键词不能为空")
		return
	}
	pageKey := strings.TrimSpace(c.Query("pageKey"))
	limit := queryInt(c, "limit", 5)

	results := helpindex.Load().Search(query, pageKey, limit)

	// P2：合并服务商分发帮助检索（客户租户可见；未装配/失败 → 空，不阻塞内嵌检索）。
	distributed := h.svc.SearchCustomerHelpDocs(c.Request.Context(), tenantID, query, pageKey, limit)
	distributedResults := make([]gin.H, 0, len(distributed))
	for _, doc := range distributed {
		distributedResults = append(distributedResults, gin.H{
			"title":            doc.Title,
			"pageKey":          doc.PageKey,
			"snippet":          doc.Snippet,
			"text":             doc.BodyMD,
			"source":           "msp_provider",
			"providerTenantId": doc.ProviderTenantID,
			"sourceSlug":       doc.SourceSlug,
			"sourceVersion":    doc.SourceVersion,
			"status":           doc.Status,
		})
	}
	common.Success(c, gin.H{
		"query":              query,
		"pageKey":            pageKey,
		"results":            results,
		"distributedResults": distributedResults,
	})
}

// GetPageHelp handles GET /api/v1/ai/help/page?pageKey=
//
// 返回该页面的覆盖状态（ok/missing/stale/unknown）与关联文档正文（面板帮助视图直接渲染）。
func (h *Handler) GetPageHelp(c *gin.Context) {
	tenantID := c.GetInt("tenant_id")
	if tenantID == 0 {
		common.Fail(c, common.AuthFailedCode, "租户信息缺失")
		return
	}
	pageKey := strings.TrimSpace(c.Query("pageKey"))
	if pageKey == "" {
		common.Fail(c, common.ParamErrorCode, "pageKey 不能为空")
		return
	}

	idx := helpindex.Load()
	info, docs := idx.DocsForPage(pageKey)
	coverage, totals := idx.Coverage()

	// P2：合并服务商分发帮助（copy-in 快照；active 跟随 provider / unbound 转客户原生）。
	distributed := h.svc.CustomerHelpDocs(c.Request.Context(), tenantID, pageKey)
	distributedDocs := make([]gin.H, 0, len(distributed))
	for _, doc := range distributed {
		distributedDocs = append(distributedDocs, gin.H{
			"title":            doc.Title,
			"pageKey":          doc.PageKey,
			"text":             doc.BodyMD,
			"source":           "msp_provider",
			"providerTenantId": doc.ProviderTenantID,
			"sourceSlug":       doc.SourceSlug,
			"sourceVersion":    doc.SourceVersion,
			"status":           doc.Status,
		})
	}

	common.Success(c, gin.H{
		"pageKey":         pageKey,
		"status":          info.Status,
		"title":           info.Title,
		"pattern":         info.Pattern,
		"entrypoint":      info.Entrypoint,
		"docs":            docs,
		"distributedDocs": distributedDocs,
		"coverage": gin.H{
			"coverage": coverage,
			"totals":   totals,
		},
	})
}

// GetHelpCoverage handles GET /api/v1/ai/help/coverage
//
// H1-04 帮助覆盖率看板数据面：账本覆盖率/总计 + 全量页面清单（status/docs）+ orphan 文档。
// 与 `/ai/help/page` 同一内嵌索引（docs/help 真源 → H0-02 生成器 → helpindex），零额外依赖。
func (h *Handler) GetHelpCoverage(c *gin.Context) {
	if tenantID := c.GetInt("tenant_id"); tenantID == 0 {
		common.Fail(c, common.AuthFailedCode, "租户信息缺失")
		return
	}

	idx := helpindex.Load()
	coverage, totals := idx.Coverage()
	common.Success(c, gin.H{
		"generatedAt": idx.GeneratedAt,
		"coverage":    coverage,
		"totals":      totals,
		"pages":       idx.Pages(),
		"orphanDocs":  idx.OrphanDocs(),
	})
}
