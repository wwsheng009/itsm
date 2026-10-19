package bot

import (
	"strings"

	"itsm-backend/ent"
)

// D1-01：页面场景 Bot 的可用性判定（/ai/assistant/context 建议过滤的「可见 Bot」段）。
//
// 口径与运行时可见性同源（ListVisibleForChat / Decide 的 status/audience/entrypoint 段）：
// 未发布的（draft）/ 角色不可见的 / 入口不允许的 Bot，其场景建议一律不下发（fail-closed）。

// 场景可用性原因码（稳定取值，随 /ai/assistant/context 下发；与 Decide 原因码同源复用）。
const (
	// ReasonTemplateMissing 租户未装配该场景 Bot（种子未跑/被删除）→ 不可见（fail-closed）。
	ReasonTemplateMissing = "template_missing"
	// ReasonAudienceDenied 模板 audience 对当前角色不可见（同 VisibleForRole 口径）。
	ReasonAudienceDenied = "audience_denied"
	// ReasonScenePolicyDisabled Bot 策略层未装配（bot.enabled=false 兼容态）→ 不做场景过滤。
	ReasonScenePolicyDisabled = "policy_disabled"
)

// SceneEligibility 判定场景 Bot 对「角色 + 入口」是否可用。
//
//   - tpl == nil（模板缺失）→ false + template_missing；
//   - draft → false + status_draft（复用 ReasonStatusDraft）；
//   - audience 不可见 → false + audience_denied；
//   - 入口不在 entrypoints → false + entrypoint_denied（复用 ReasonEntrypointDenied，空列表 = 全拒）。
func SceneEligibility(tpl *ent.BotTemplate, role, entrypoint string) (bool, string) {
	if tpl == nil {
		return false, ReasonTemplateMissing
	}
	if strings.EqualFold(strings.TrimSpace(tpl.Status), StatusDraft) {
		return false, ReasonStatusDraft
	}
	if !VisibleForRole(tpl.Audience, role) {
		return false, ReasonAudienceDenied
	}
	if !entrypointAllowed(tpl.EntrypointsJSON, entrypoint) {
		return false, ReasonEntrypointDenied
	}
	return true, ""
}
