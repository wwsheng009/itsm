package ai

import (
	"errors"
	"sort"

	"itsm-backend/common"
	"itsm-backend/internal/helpindex"
	"itsm-backend/service"

	"github.com/gin-gonic/gin"
)

// H1-01 文档待办队列（GET /ai/help/todos）。
//
// 数据源（两者合并，均为只读）：
//  1. 账本缺口：helpindex 的 missing/stale 页面（零遥测环境也有基础待办项）；
//  2. 遥测缺口：最近 days 天的 help_gap / doc_draft_created 事件按 pageKey 聚类（O0-02 数据面，
//     前端帮助面板在 missing/stale 时上报 help_gap）。
//
// 排序：文档缺失 > 过时 > 未注册页面 > 仅用户求助缺口；同级按缺口数、草稿数、pageKey。
// 降级：埋点服务未启用 → 200 + telemetry.available=false（仅账本缺口，不 5xx）。

// 待办原因码（稳定取值）。
const (
	helpTodoReasonDocMissing   = "doc_missing"
	helpTodoReasonDocStale     = "doc_stale"
	helpTodoReasonUnregistered = "page_unregistered"
	helpTodoReasonUserGaps     = "user_help_gaps"
)

type helpTodoItem struct {
	PageKey        string `json:"pageKey"`
	Title          string `json:"title,omitempty"`
	DocStatus      string `json:"docStatus"` // missing|stale|ok|unknown
	GapCount       int    `json:"gapCount"`
	DraftCount     int    `json:"draftCount"`
	LastSeen       string `json:"lastSeen,omitempty"`
	Reason         string `json:"reason"`
	State          string `json:"state"` // open|claimed|ignored（缺省 open，H1-04 条件项）
	StateUpdatedBy int    `json:"stateUpdatedBy,omitempty"`
	StateUpdatedAt string `json:"stateUpdatedAt,omitempty"`
}

// GetHelpTodos handles GET /api/v1/ai/help/todos?days=&limit=
func (h *Handler) GetHelpTodos(c *gin.Context) {
	tenantID := c.GetInt("tenant_id")
	if tenantID == 0 {
		common.Fail(c, common.AuthFailedCode, "租户信息缺失")
		return
	}

	days := queryInt(c, "days", 30)
	limit := queryInt(c, "limit", 50)
	if limit > 200 {
		limit = 200
	}

	idx := helpindex.Load()

	var (
		clusters           []service.AssistantHelpGapCluster
		telemetryAvailable = true
	)
	if h.svc == nil {
		telemetryAvailable = false
	} else {
		result, err := h.svc.GetAssistantHelpGapClusters(c.Request.Context(), tenantID, days, limit)
		if err != nil {
			telemetryAvailable = false
			if h.svc.logger != nil {
				h.svc.logger.Warnw("help todos telemetry unavailable", "error", err, "tenantID", tenantID)
			}
		} else {
			clusters = result
		}
	}

	items := buildHelpTodos(idx, clusters, limit)

	// H1-01/H1-03/H1-04 条件项闭环：合并持久化的认领/忽略状态（未装配 → 按 open 缺省渲染，不 5xx）。
	statesAvailable := true
	var states map[string]service.HelpDocTodoState
	if h.svc == nil {
		statesAvailable = false
	} else {
		var err error
		states, err = h.svc.HelpDocTodoStates(c.Request.Context(), tenantID)
		if err != nil {
			statesAvailable = false
			if h.svc.logger != nil {
				h.svc.logger.Warnw("help todo states unavailable", "error", err, "tenantID", tenantID)
			}
		}
	}

	missing, stale, claimed, ignored := 0, 0, 0, 0
	for i := range items {
		switch items[i].DocStatus {
		case "missing":
			missing++
		case "stale":
			stale++
		}
		items[i].State = service.HelpDocTodoStateOpen
		if st, ok := states[items[i].PageKey]; ok {
			items[i].State = st.State
			items[i].StateUpdatedBy = st.UpdatedBy
			items[i].StateUpdatedAt = st.UpdatedAt
		}
		switch items[i].State {
		case service.HelpDocTodoStateClaimed:
			claimed++
		case service.HelpDocTodoStateIgnored:
			ignored++
		}
	}

	common.Success(c, gin.H{
		"days":  days,
		"items": items,
		"totals": gin.H{
			"items":   len(items),
			"missing": missing,
			"stale":   stale,
			"claimed": claimed,
			"ignored": ignored,
		},
		"states":    gin.H{"available": statesAvailable},
		"telemetry": gin.H{"available": telemetryAvailable},
	})
}

type helpTodoStateRequest struct {
	PageKey string `json:"pageKey"`
	State   string `json:"state"`
}

// SetHelpTodoState handles PUT /api/v1/ai/help/todos/state（H1-04 条件项闭环：认领/忽略/恢复）。
//
// 语义：claimed（认领）/ ignored（忽略）持久化到 ai_doc_todo_states；open（恢复）删除行回到缺省。
// 幂等：重复同一状态原地刷新（操作人/时间）；重复恢复为空操作。
// 降级：状态服务未装配 → 503（写路径可见地失败；GET 侧仅 states.available=false）。
func (h *Handler) SetHelpTodoState(c *gin.Context) {
	tenantID := c.GetInt("tenant_id")
	userID := c.GetInt("user_id")
	if tenantID == 0 || userID == 0 {
		common.Fail(c, common.AuthFailedCode, "租户/用户信息缺失")
		return
	}

	var req helpTodoStateRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		common.Fail(c, common.ParamErrorCode, "请求体格式错误")
		return
	}
	if h.svc == nil {
		common.Fail(c, common.ServiceUnavailableCode, "待办状态服务未就绪")
		return
	}

	result, err := h.svc.SetHelpDocTodoState(c.Request.Context(), tenantID, userID, req.PageKey, req.State)
	if err != nil {
		switch {
		case errors.Is(err, service.ErrHelpDocTodoStateUnavailable):
			common.Fail(c, common.ServiceUnavailableCode, "待办状态服务未就绪")
		case errors.Is(err, service.ErrHelpDocTodoStateInvalid):
			common.Fail(c, common.ParamErrorCode, "pageKey/state 非法（state 取 claimed|ignored|open）")
		default:
			if h.svc.logger != nil {
				h.svc.logger.Warnw("设置待办状态失败", "error", err, "tenantID", tenantID, "pageKey", req.PageKey)
			}
			common.Fail(c, common.InternalErrorCode, "设置待办状态失败")
		}
		return
	}

	common.Success(c, result)
}

func helpDocStatusRank(status string) int {
	switch status {
	case "missing":
		return 0
	case "stale":
		return 1
	case "unknown":
		return 2
	default:
		return 3 // ok / 其他
	}
}

// buildHelpTodos 合并账本缺口与遥测聚类（纯函数，便于单测）。
func buildHelpTodos(idx *helpindex.Index, clusters []service.AssistantHelpGapCluster, limit int) []helpTodoItem {
	byKey := map[string]*helpTodoItem{}
	get := func(pageKey string) *helpTodoItem {
		if item, ok := byKey[pageKey]; ok {
			return item
		}
		item := &helpTodoItem{PageKey: pageKey}
		byKey[pageKey] = item
		return item
	}

	// ① 账本缺口（missing/stale）：无需遥测即为待办项。
	for _, gap := range idx.Gaps() {
		item := get(gap.PageKey)
		item.Title = gap.Title
		item.DocStatus = gap.Status
	}

	// ② 遥测缺口聚类（含未注册页面：注册表/账本均无 → unknown）。
	for _, cluster := range clusters {
		if cluster.PageKey == "" {
			continue
		}
		item := get(cluster.PageKey)
		item.GapCount = cluster.GapCount
		item.DraftCount = cluster.DraftCount
		item.LastSeen = cluster.LastSeen
		if item.DocStatus == "" {
			info, _ := idx.DocsForPage(cluster.PageKey)
			item.DocStatus = info.Status
			if item.Title == "" {
				item.Title = info.Title
			}
		}
	}

	items := make([]helpTodoItem, 0, len(byKey))
	for _, item := range byKey {
		switch item.DocStatus {
		case "missing":
			item.Reason = helpTodoReasonDocMissing
		case "stale":
			item.Reason = helpTodoReasonDocStale
		case "unknown":
			item.Reason = helpTodoReasonUnregistered
		default:
			item.DocStatus = "ok"
			item.Reason = helpTodoReasonUserGaps
		}
		items = append(items, *item)
	}

	sort.SliceStable(items, func(i, j int) bool {
		ri, rj := helpDocStatusRank(items[i].DocStatus), helpDocStatusRank(items[j].DocStatus)
		if ri != rj {
			return ri < rj
		}
		if items[i].GapCount != items[j].GapCount {
			return items[i].GapCount > items[j].GapCount
		}
		if items[i].DraftCount != items[j].DraftCount {
			return items[i].DraftCount > items[j].DraftCount
		}
		return items[i].PageKey < items[j].PageKey
	})
	if limit > 0 && len(items) > limit {
		items = items[:limit]
	}
	return items
}
