package bot

import (
	"context"
	"fmt"

	"itsm-backend/ent"
	"itsm-backend/ent/bottemplate"
)

// B3 场景 Bot pilot 定义与幂等种子（B3-03/B3-04/B3-05）。
//
// 口径：
//   - 场景 Bot 与「默认助手」并列存在，由管理页/种子装配；种子**只增不改**——
//     管理员对模板与授权的显式修改优先（已存在时不覆盖字段，只补齐缺失授权）。
//   - 场景边界写进定义本身，便于验收断言：
//     S2（事件/值班）**不含**任何"改级/改状态"工具（只是建议，人工执行）；
//     S3（知识/自助）**不含**发布类工具（草稿只落到产物，发布走既有流程）。
//   - 风险上限按场景最小必要授权：pilot 期统一 act_low（plan 类工具风险为 plan，低于 act_low）。

// ScenarioBot 是一个场景 Bot 的种子定义。
type ScenarioBot struct {
	Slug        string
	Name        string
	Audience    string
	RiskLimit   string
	Entrypoints []string
	Grants      []ScenarioGrant
}

// ScenarioGrant 是场景 Bot 的工具授权（riskLimit = 该授权允许的工具风险上限）。
type ScenarioGrant struct {
	ToolName  string
	RiskLimit string
}

// ScenarioBots 返回三个 pilot 场景的定义（稳定顺序：S1/S2/S3）。
func ScenarioBots() []ScenarioBot {
	return []ScenarioBot{
		{
			Slug:        "s1-ticket-assistant",
			Name:        "S1 工单助手",
			Audience:    "internal",
			RiskLimit:   RiskActLow,
			Entrypoints: []string{EntrypointChat, EntrypointTicketDetail, EntrypointTicketList},
			Grants: []ScenarioGrant{
				{"list_tickets", RiskRead},
				{"get_incident_stats", RiskRead},
				{"list_kb", RiskRead},
				{"create_ticket", RiskActLow},
				{"draft_ticket_fields", RiskPlan},
			},
		},
		{
			Slug:        "s2-incident-oncall",
			Name:        "S2 事件值班助手",
			Audience:    "internal",
			RiskLimit:   RiskActLow,
			Entrypoints: []string{EntrypointChat, EntrypointIncidentDetail, EntrypointIncidentCreate},
			Grants: []ScenarioGrant{
				{"list_tickets", RiskRead},
				{"get_incident_stats", RiskRead},
				{"get_ci_impact", RiskPlan},
				{"link_ticket_ci", RiskActLow}, // 关联动作走确认闭环（写工具）
				{"analyze_ci_impact_plan", RiskPlan},
			},
		},
		{
			Slug:        "s3-knowledge-assistant",
			Name:        "S3 知识自助助手",
			Audience:    "all",
			RiskLimit:   RiskActLow,
			Entrypoints: []string{EntrypointChat, EntrypointCIDetail},
			Grants: []ScenarioGrant{
				{"list_kb", RiskRead},
				{"draft_kb_article", RiskPlan},
			},
		},
	}
}

// ScenarioSeedResult 报告一次种子装配的结果（幂等可复跑）。
type ScenarioSeedResult struct {
	CreatedTemplates int
	CreatedGrants    int
	// SkippedGrants 记录因管理员已配置同名授权而跳过的工具（不覆盖既有上限）。
	SkippedGrants int
}

// SeedScenarioBots 幂等装配三个场景 Bot（已存在则只补齐缺失授权）。
func (a *TemplateAdmin) SeedScenarioBots(ctx context.Context, tenantID int) (ScenarioSeedResult, error) {
	var result ScenarioSeedResult
	if a == nil || a.client == nil {
		return result, fmt.Errorf("bot: template admin 未初始化")
	}
	if tenantID <= 0 {
		return result, fmt.Errorf("bot: 场景种子需要租户")
	}

	for _, scenario := range ScenarioBots() {
		tpl, err := a.client.BotTemplate.Query().
			Where(bottemplate.TenantID(tenantID), bottemplate.SlugEQ(scenario.Slug)).
			Only(ctx)
		switch {
		case err == nil:
			// 已存在：不覆盖模板字段（管理员修改优先）。
		case ent.IsNotFound(err):
			created, createErr := a.CreateTemplate(ctx, tenantID, TemplateInput{
				Slug:        scenario.Slug,
				Name:        scenario.Name,
				Audience:    scenario.Audience,
				RiskLimit:   scenario.RiskLimit,
				Status:      StatusPilot,
				Entrypoints: scenario.Entrypoints,
			})
			if createErr != nil {
				return result, fmt.Errorf("bot: 种子场景 %s 失败: %w", scenario.Slug, createErr)
			}
			tpl = created
			result.CreatedTemplates++
		default:
			return result, err
		}

		existing, err := a.ListGrants(ctx, tenantID, tpl.ID)
		if err != nil {
			return result, err
		}
		byName := make(map[string]bool, len(existing))
		for _, grant := range existing {
			byName[grant.ToolName] = true
		}
		for _, grant := range scenario.Grants {
			if byName[grant.ToolName] {
				result.SkippedGrants++
				continue
			}
			if _, err := a.UpsertGrant(ctx, tenantID, tpl.ID, GrantInput{
				ToolName:  grant.ToolName,
				RiskLimit: grant.RiskLimit,
			}); err != nil {
				return result, fmt.Errorf("bot: 场景 %s 授权 %s 失败: %w", scenario.Slug, grant.ToolName, err)
			}
			result.CreatedGrants++
		}
	}
	return result, nil
}
