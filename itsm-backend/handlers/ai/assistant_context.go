package ai

import (
	"errors"
	"strings"

	"itsm-backend/common"
	"itsm-backend/internal/assistantcontext"
	"itsm-backend/middleware"
	"itsm-backend/service"
	"itsm-backend/service/bot"

	"github.com/gin-gonic/gin"
)

// D1-01 /ai/assistant/context：页面助手上下文的**服务端过滤**数据面。
//
// 数据源：internal/assistantcontext（由前端 page-context/registry.ts 经
// gen-page-manifest.mjs 生成的内嵌副本；前端注册表是唯一真源）。
//
// 过滤口径（方案 §4.1 L1 / P1-D1-01）：
//  1. 未知 pageKey → known=false（前端回退静态基线；不 400——与 D0-04 提示层同口径）；
//  2. 场景 Bot 可用性（bot.SceneEligibility）：模板缺失/draft/角色不可见/入口不允许
//     → 该页场景建议全部过滤（fail-closed）；Bot 策略层未装配（bot.enabled=false 兼容态）
//     → 不做过滤（filtering=disabled），与静态基线行为一致；
//  3. 建议声明的 tools：逐工具按 bot.Decide（授权 ∩ RBAC ∩ 风险上限 ∩ 入口）判定，
//     任一工具不通过 → 该建议过滤（reason 取判定原因码；未知工具 → unknown_tool）；
//  4. 未声明 tools 的建议只受场景可用性约束。
//
// 响应 suggestions 保留注册表顺序（rank = 注册表序号）；filtered 记录被过滤项与原因
// （可观测/可测试；不改变对话链路的授权判定——执行面仍走既有 Gate2/Gate3）。

// 工具门禁的兜底原因码（稳定取值）。
const (
	reasonToolsUnavailable = "tools_unavailable"
	reasonUnknownTool      = "unknown_tool"
)

// assistantContextSuggestion 是下发的一条建议。
type assistantContextSuggestion struct {
	ID       string `json:"id"`
	Kind     string `json:"kind"`
	Label    string `json:"label"`
	Prompt   string `json:"prompt"`
	ReadOnly bool   `json:"readOnly"`
	Rank     int    `json:"rank"`
	// O2-01：规则打分与反馈（原始计数 + 衰减有效计数；ranking=weighted 时携带；无反馈的卡 Feedback 为 nil）。
	Score    float64                   `json:"score,omitempty"`
	Feedback *service.SuggestionWeight `json:"feedback,omitempty"`
}

// assistantContextFiltered 是被过滤的建议与原因码。
type assistantContextFiltered struct {
	ID     string `json:"id"`
	Reason string `json:"reason"`
}

// GetAssistantContext handles GET /api/v1/ai/assistant/context?pageKey=&route=
func (h *Handler) GetAssistantContext(c *gin.Context) {
	tenantID := c.GetInt("tenant_id")
	userID := c.GetInt("user_id")
	role := c.GetString("role")
	if tenantID <= 0 || userID <= 0 || role == "" {
		common.AuthFailed(c, "缺少有效身份上下文")
		return
	}

	pageKey := strings.TrimSpace(c.Query("pageKey"))
	if pageKey == "" {
		common.Fail(c, common.ParamErrorCode, "pageKey 不能为空")
		return
	}
	route := strings.TrimSpace(c.Query("route"))

	// suggestions/filtered 恒为数组（JSON 空数组而非 null）。
	suggestions := make([]assistantContextSuggestion, 0)
	filtered := make([]assistantContextFiltered, 0)

	decl := assistantcontext.Load().Declaration(pageKey)
	if decl == nil {
		// 未知页面键：正常降级（known=false），前端回退静态基线。
		common.Success(c, gin.H{
			"pageKey":     pageKey,
			"route":       route,
			"known":       false,
			"suggestions": suggestions,
			"filtered":    filtered,
		})
		return
	}

	filtering := "none"
	sceneEligible := true
	sceneReason := ""
	var snapshot *bot.Snapshot
	var sceneView gin.H

	if decl.Scene != "" {
		sceneView = gin.H{"slug": decl.Scene, "eligible": true, "reason": "", "botId": 0}
		filtering = "disabled"
		sceneReason = bot.ReasonScenePolicyDisabled

		// Bot 策略层生效时才做场景/工具过滤（bot.enabled=false 兼容态与静态基线一致）。
		if h.svc.botEnabled(c.Request.Context(), tenantID) && h.svc.entClient != nil {
			filtering = "enabled"
			snap, err := h.svc.botPolicy.SnapshotForSlug(c.Request.Context(), tenantID, decl.Scene)
			if err != nil {
				// 快照读取失败 → fail-closed（与 bot.Decide 的 snapshot_error 同口径）。
				h.svc.logger.Warnw("assistant context scene snapshot failed",
					"error", err, "scene", decl.Scene, "tenantID", tenantID)
				sceneEligible, sceneReason = false, bot.ReasonSnapshotError
			} else {
				snapshot = snap
				if snap != nil {
					sceneEligible, sceneReason = bot.SceneEligibility(snap.Template, role, decl.Entrypoint)
					if snap.Template != nil {
						sceneView["botId"] = snap.Template.ID
						sceneView["status"] = snap.Template.Status
					}
				} else {
					sceneEligible, sceneReason = bot.SceneEligibility(nil, role, decl.Entrypoint)
				}
			}
		}

		sceneView["eligible"] = sceneEligible
		sceneView["reason"] = sceneReason
	}

	entrypoint := decl.Entrypoint
	if strings.TrimSpace(entrypoint) == "" {
		entrypoint = bot.EntrypointChat
	}

	for idx, action := range decl.Suggestions {
		if decl.Scene != "" && !sceneEligible {
			filtered = append(filtered, assistantContextFiltered{ID: action.ID, Reason: sceneReason})
			continue
		}
		if filtering == "enabled" && len(action.Tools) > 0 {
			reason := ""
			for _, toolName := range action.Tools {
				if denied := h.assistantToolGateReason(c, tenantID, role, snapshot, entrypoint, toolName); denied != "" {
					reason = denied
					break
				}
			}
			if reason != "" {
				filtered = append(filtered, assistantContextFiltered{ID: action.ID, Reason: reason})
				continue
			}
		}
		suggestions = append(suggestions, assistantContextSuggestion{
			ID:       action.ID,
			Kind:     action.Kind,
			Label:    action.Label,
			Prompt:   action.Prompt,
			ReadOnly: action.ReadOnly,
			Rank:     idx,
		})
	}

	// O2-01 建议排序权重（先规则）：埋点服务可用时按组内反馈（半衰期衰减 + 忽略负样本）重排；
	// 不可用则保持注册表顺序。
	weighted := false
	if len(suggestions) > 0 && h.svc.aiTelemetryService != nil {
		weights, err := h.svc.SuggestionWeights(c.Request.Context(), tenantID, service.SuggestionWeightsDefaultDays)
		switch {
		case err == nil:
			suggestions = rankAssistantSuggestions(suggestions, weights)
			weighted = true
		case errors.Is(err, service.ErrAssistantTelemetryUnavailable):
			// 关闭态兼容：不重排、不告警（与既有静态基线行为一致）。
		default:
			h.svc.logger.Warnw("assistant context suggestion weights unavailable",
				"error", err, "tenantID", tenantID)
		}
	}

	resp := gin.H{
		"pageKey":     pageKey,
		"route":       route,
		"known":       true,
		"title":       decl.Title,
		"entrypoint":  entrypoint,
		"suggestions": suggestions,
		"filtered":    filtered,
		"filtering":   filtering,
		"ranking":     suggestionRankingMode(weighted),
	}
	if sceneView != nil {
		resp["scene"] = sceneView
	}
	common.Success(c, resp)
}

// assistantToolGateReason 判定单条建议声明的工具是否可下发（"" = 放行）。
//
// 复用 bot.Decide（B2-02 策略门禁）：未配置授权的场景 Bot 走兼容默认（只读 + 遗留白名单），
// 配置了授权则要求授权 ∩ RBAC ∩ 风险上限 ∩ 入口全部通过。
func (h *Handler) assistantToolGateReason(
	c *gin.Context,
	tenantID int,
	role string,
	snapshot *bot.Snapshot,
	entrypoint string,
	toolName string,
) string {
	if h.svc.tools == nil {
		return reasonToolsUnavailable
	}
	def := h.svc.tools.GetToolForTenant(c.Request.Context(), tenantID, toolName)
	if def == nil {
		return reasonUnknownTool
	}
	decision := bot.Decide(bot.CheckInput{
		Snapshot: snapshot,
		Tool: bot.ToolMeta{
			Name:     def.Name,
			Provider: def.Provider,
			ReadOnly: def.ReadOnly,
			Resource: def.Resource,
			Action:   def.Action,
			Risk:     def.Risk,
		},
		Entrypoint: entrypoint,
		RBACAllowed: func(resource, action string) bool {
			return middleware.HasResourcePermission(c.Request.Context(), h.svc.entClient, role, resource, action, tenantID)
		},
	})
	if decision.Allowed {
		return ""
	}
	return decision.Reason
}
