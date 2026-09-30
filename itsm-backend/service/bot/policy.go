package bot

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"itsm-backend/ent"
	"itsm-backend/ent/bottemplate"
	"itsm-backend/ent/bottoolgrant"
)

// Package bot 的策略门禁（B2-02）：工具**下发与执行**前的四重交集
//
//	授权（bot_tool_grants）∩ RBAC（调用方回调）∩ 风险上限（模板/授权）∩ 入口（entrypoints）
//
// 与 B2-01 的分工：B2-01 只管管理面 CRUD 与「授权风险 ≤ 模板上限」的写入校验；
// 本文件负责运行时的**读侧判定**，并同时服务两个调用点：
//   - 下发面：聊天工具面装配（handlers/ai ChatStream）按本判定过滤，未授权工具不可见；
//   - 执行面：ExecuteTool 的 Gate2 之后**二次校验**（不可只靠下发过滤——工具面可能过期，
//     授权可能被回收，调用也可能绕过聊天链路直接触发）。
//
// 兼容默认（灰度前提）：**未配置任何授权**的 Bot 等价现状行为（只读 + 遗留写白名单），
// 管理员一旦为该 Bot 配第一条授权，该 Bot 即切换到严格交集；`bot.enabled=false` 时
// 调用方根本不装配 Policy，行为逐字节不变。

// EntrypointChat 是聊天入口标识（模板 entrypoints_json 中的一个取值）。
const EntrypointChat = "chat"

// LegacyChatWritableTools 是迁移自 handlers/ai 的聊天写工具白名单（兼容默认，见文件头）。
//
// 这些工具经 ExecuteTool 进入待审批队列（Gate3），不在聊天链路内直接落库。
// handlers/ai 的 `chatWritableTools` 已改为本表的别名（单一来源，防止两处漂移）。
var LegacyChatWritableTools = map[string]bool{
	"create_ticket":      true,
	"update_ticket":      true,
	"create_ticket_type": true,
	// CMDB 本体链路：把工单挂到配置项（走同一审批流）
	"link_ticket_ci": true,
	// CMDB 关系写操作：与上面同一审批流 + Gate2 RBAC（resource=ci_relationship, action=write）
	"create_ci_relationship": true,
	"delete_ci_relationship": true,
}

// ToolMeta 是策略判定所需的最小工具视图（内置与 MCP 同构；MCP 由 M1-01 标注 risk）。
type ToolMeta struct {
	Name     string
	Provider string
	ReadOnly bool
	Resource string
	Action   string
	Risk     string
}

// Snapshot 是「某 Bot 的当前策略快照」（一次会话/一次执行内复用，避免逐工具打库）。
type Snapshot struct {
	Template *ent.BotTemplate
	Grants   map[string]*ent.BotToolGrant // key = tool_name
}

// 判定原因码（稳定取值，写审计 permission_reason / 日志 / SSE 事件）。
const (
	// ReasonLegacyDefault 未配置任何授权 → 兼容默认（只读 + 遗留写白名单）。
	ReasonLegacyDefault = "legacy_default"
	// ReasonStatusDraft 模板未发布（draft）→ 不下发（pilot/ga 均视为已发布）。
	ReasonStatusDraft = "status_draft"
	// ReasonEntrypointDenied 入口不在模板 entrypoints 内（空列表 = 全拒，fail-closed）。
	ReasonEntrypointDenied = "entrypoint_denied"
	// ReasonToolNotGranted 未授权该工具。
	ReasonToolNotGranted = "tool_not_granted"
	// ReasonRiskUnknown 工具风险未标注 → fail-closed 拒绝（B0-01 口径：缺失按最保守处理）。
	ReasonRiskUnknown = "risk_unknown"
	// ReasonRiskExceeded 工具风险超过授权上限（或授权上限超过模板上限，双保险）。
	ReasonRiskExceeded = "risk_exceeded"
	// ReasonRBACDenied RBAC 判定拒绝（回调返回 false）。
	ReasonRBACDenied = "rbac_denied"
	// ReasonSnapshotError 策略快照读取失败 → fail-closed 拒绝。
	ReasonSnapshotError = "snapshot_error"
)

// Decision 是单工具的判定结果。
type Decision struct {
	Allowed bool
	Reason  string
	// Legacy 为 true 表示走了兼容默认（未配置授权），此时 Reason=ReasonLegacyDefault。
	Legacy bool
}

// CheckInput 是判定入参。
type CheckInput struct {
	Snapshot   *Snapshot
	Tool       ToolMeta
	Entrypoint string
	// RBACAllowed 为 RBAC 判定回调（nil = 本层不判定，由调用方保证）。
	// 调用方应传入与 Gate2 同源的判定，避免两处口径漂移。
	RBACAllowed func(resource, action string) bool
}

// Policy 是策略门禁（持有 ent client 以读取模板/授权）。
type Policy struct {
	client *ent.Client
}

func NewPolicy(client *ent.Client) *Policy {
	return &Policy{client: client}
}

// SnapshotForBot 解析某 Bot 的策略快照。
//
//   - botID > 0：按租户 + 模板 ID 解析；不存在返回 (nil, nil) → 调用方按兼容默认处理；
//   - botID <= 0：解析内置「默认助手」（slug=default-assistant）；不存在同样返回 (nil, nil)。
//
// 「已配置授权」的判定：快照带 ≥1 条授权。0 授权的模板 = 未配置策略 → 兼容默认。
func (p *Policy) SnapshotForBot(ctx context.Context, tenantID, botID int) (*Snapshot, error) {
	if p == nil || p.client == nil {
		return nil, fmt.Errorf("bot policy: ent client 未注入")
	}
	query := p.client.BotTemplate.Query().Where(bottemplate.TenantID(tenantID))
	if botID > 0 {
		query = query.Where(bottemplate.ID(botID))
	} else {
		query = query.Where(bottemplate.SlugEQ(DefaultTemplateSlug))
	}
	tpl, err := query.Only(ctx)
	if ent.IsNotFound(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}

	grants, err := p.client.BotToolGrant.Query().
		Where(bottoolgrant.TenantID(tenantID), bottoolgrant.BotID(tpl.ID)).
		All(ctx)
	if err != nil {
		return nil, err
	}
	byName := make(map[string]*ent.BotToolGrant, len(grants))
	for _, grant := range grants {
		byName[grant.ToolName] = grant
	}
	return &Snapshot{Template: tpl, Grants: byName}, nil
}

// CheckTool 是执行面的单次判定便捷入口（读取快照 + Decide 合并为一步）。
//
// 快照读取失败 → fail-closed（ReasonSnapshotError）。
func (p *Policy) CheckTool(ctx context.Context, tenantID, botID int, entrypoint string, tool ToolMeta, rbac func(string, string) bool) Decision {
	snapshot, err := p.SnapshotForBot(ctx, tenantID, botID)
	if err != nil {
		return Decision{Allowed: false, Reason: ReasonSnapshotError}
	}
	return Decide(CheckInput{Snapshot: snapshot, Tool: tool, Entrypoint: entrypoint, RBACAllowed: rbac})
}

// Decide 是纯函数判定（便于表驱动测试与复用）。
func Decide(in CheckInput) Decision {
	// ⓪ 黑名单（B2-05）：命中即拒绝，与授权/兼容默认无关——管理员显式授权也不放行。
	if rule := BlacklistRule(in.Tool.Name, in.Tool.Provider, in.Tool.Resource); rule != "" {
		return Decision{Allowed: false, Reason: ReasonToolBlacklisted + ":" + rule}
	}

	// ① 兼容默认：无模板 或 模板未配置任何授权 → 等价现状（只读 + 遗留写白名单）。
	if in.Snapshot == nil || in.Snapshot.Template == nil || len(in.Snapshot.Grants) == 0 {
		if !in.Tool.ReadOnly && !LegacyChatWritableTools[in.Tool.Name] {
			return Decision{Allowed: false, Reason: ReasonToolNotGranted, Legacy: true}
		}
		if in.RBACAllowed != nil && !in.RBACAllowed(in.Tool.Resource, in.Tool.Action) {
			return Decision{Allowed: false, Reason: ReasonRBACDenied, Legacy: true}
		}
		return Decision{Allowed: true, Reason: ReasonLegacyDefault, Legacy: true}
	}

	// ② 严格交集（该 Bot 已配置授权）。
	tpl := in.Snapshot.Template
	if strings.EqualFold(strings.TrimSpace(tpl.Status), StatusDraft) {
		return Decision{Allowed: false, Reason: ReasonStatusDraft}
	}
	if !entrypointAllowed(tpl.EntrypointsJSON, in.Entrypoint) {
		return Decision{Allowed: false, Reason: ReasonEntrypointDenied}
	}
	grant := in.Snapshot.Grants[in.Tool.Name]
	if grant == nil {
		return Decision{Allowed: false, Reason: ReasonToolNotGranted}
	}
	toolRank := RiskRank(in.Tool.Risk)
	if toolRank < 0 {
		// 未标注风险 → 最保守拒绝（与 NormalizeToolMetadata 的兜底口径一致）。
		return Decision{Allowed: false, Reason: ReasonRiskUnknown}
	}
	// 授权上限 ≤ 模板上限（写入侧已校验，这里读侧双保险——历史数据或直改库都拦得住）。
	if grantRank := RiskRank(grant.RiskLimit); grantRank < 0 || grantRank > RiskRank(tpl.RiskLimit) {
		return Decision{Allowed: false, Reason: ReasonRiskExceeded}
	}
	if toolRank > RiskRank(grant.RiskLimit) {
		return Decision{Allowed: false, Reason: ReasonRiskExceeded}
	}
	if in.RBACAllowed != nil && !in.RBACAllowed(in.Tool.Resource, in.Tool.Action) {
		return Decision{Allowed: false, Reason: ReasonRBACDenied}
	}
	return Decision{Allowed: true}
}

// entrypointAllowed 判定入口是否被模板允许；entrypoints 为空/无法解析 → 拒绝（fail-closed）。
func entrypointAllowed(raw, entrypoint string) bool {
	entrypoint = strings.TrimSpace(entrypoint)
	if entrypoint == "" {
		return false
	}
	var allowed []string
	if err := json.Unmarshal([]byte(strings.TrimSpace(raw)), &allowed); err != nil {
		return false
	}
	for _, item := range allowed {
		if strings.EqualFold(strings.TrimSpace(item), entrypoint) {
			return true
		}
	}
	return false
}
