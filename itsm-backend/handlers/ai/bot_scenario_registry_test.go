package ai_test

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"itsm-backend/ent"
	"itsm-backend/service"
	"itsm-backend/service/bot"
)

// TestScenarioBots_GrantsExistInRegistryAndDecideAllows 是 S1～S7 业务 Bot 模板的
// 「定义 ↔ 工具注册表 ↔ 策略门」三方交叉校验：
//
//  1. 每条授权工具必须在真实注册表中存在（防种子指向不存在的工具 → 静默空授权）；
//  2. 工具自身风险 ≤ 授权上限 ≤ 模板上限（防定义漂移绕过最小必要口径）；
//  3. 在模板声明的入口下，授权工具经 bot.Decide 全部放行；
//  4. 未授权工具（含所有写工具/高危工具）一律 ReasonToolNotGranted；
//  5. 未声明入口一律 ReasonEntrypointDenied（fail-closed）。
func TestScenarioBots_GrantsExistInRegistryAndDecideAllows(t *testing.T) {
	registry := service.NewToolRegistry(nil, nil, nil, nil)
	byName := make(map[string]service.ToolDefinition)
	for _, td := range registry.ListTools() {
		byName[td.Name] = td
	}
	require.NotEmpty(t, byName, "内置工具注册表不得为空")

	known := make(map[string]bool)
	for _, ep := range bot.KnownEntrypoints() {
		known[ep] = true
	}

	for _, scenario := range bot.ScenarioBots() {
		scenario := scenario
		t.Run(scenario.Slug, func(t *testing.T) {
			raw, err := json.Marshal(scenario.Entrypoints)
			require.NoError(t, err)

			grants := make(map[string]*ent.BotToolGrant, len(scenario.Grants))
			for _, grant := range scenario.Grants {
				td, ok := byName[grant.ToolName]
				require.Truef(t, ok, "授权工具 %s 未在注册表注册（注册表改名/删除时必须同步场景定义）", grant.ToolName)
				assert.LessOrEqual(t, bot.RiskRank(td.Risk), bot.RiskRank(grant.RiskLimit),
					"%s: 工具风险 %s 超过授权上限 %s", grant.ToolName, td.Risk, grant.RiskLimit)
				assert.LessOrEqual(t, bot.RiskRank(grant.RiskLimit), bot.RiskRank(scenario.RiskLimit),
					"%s: 授权上限 %s 超过模板上限 %s", grant.ToolName, grant.RiskLimit, scenario.RiskLimit)
				grants[grant.ToolName] = &ent.BotToolGrant{
					TenantID: 1, BotID: 1, ToolName: grant.ToolName, RiskLimit: grant.RiskLimit,
				}
			}
			require.NotEmpty(t, grants, "场景必须至少配置一条授权")

			snapshot := &bot.Snapshot{
				Template: &ent.BotTemplate{
					ID:              1,
					TenantID:        1,
					Slug:            scenario.Slug,
					Status:          bot.StatusPilot,
					RiskLimit:       scenario.RiskLimit,
					EntrypointsJSON: string(raw),
				},
				Grants: grants,
			}
			meta := func(name string) bot.ToolMeta {
				td := byName[name]
				return bot.ToolMeta{
					Name: td.Name, Provider: "builtin", ReadOnly: td.ReadOnly,
					Resource: td.Resource, Action: td.Action, Risk: td.Risk,
				}
			}

			// 入口声明必须是已知入口。
			for _, ep := range scenario.Entrypoints {
				assert.Truef(t, known[ep], "%s: 未知入口 %s", scenario.Slug, ep)
			}

			// ③ 声明入口下授权工具全部放行。
			for _, ep := range scenario.Entrypoints {
				for name := range grants {
					decision := bot.Decide(bot.CheckInput{Snapshot: snapshot, Tool: meta(name), Entrypoint: ep})
					assert.Truef(t, decision.Allowed, "%s@%s 应放行，实际 reason=%s", name, ep, decision.Reason)
				}
			}

			// ④ 未授权工具一律拒绝（工具未授权，而非风险/入口原因）。
			primary := scenario.Entrypoints[0]
			for name := range byName {
				if _, ok := grants[name]; ok {
					continue
				}
				decision := bot.Decide(bot.CheckInput{Snapshot: snapshot, Tool: meta(name), Entrypoint: primary})
				assert.Falsef(t, decision.Allowed, "未授权工具 %s 不得放行", name)
				assert.Equalf(t, bot.ReasonToolNotGranted, decision.Reason, "未授权工具 %s 的拒绝原因", name)
			}

			// ⑤ 未声明入口一律拒绝（含 chat 之外的任何已知入口）。
			declared := make(map[string]bool, len(scenario.Entrypoints))
			for _, ep := range scenario.Entrypoints {
				declared[ep] = true
			}
			for _, ep := range bot.KnownEntrypoints() {
				if declared[ep] {
					continue
				}
				for name := range grants {
					decision := bot.Decide(bot.CheckInput{Snapshot: snapshot, Tool: meta(name), Entrypoint: ep})
					assert.Falsef(t, decision.Allowed, "%s 在未声明入口 %s 不得放行", name, ep)
					assert.Equal(t, bot.ReasonEntrypointDenied, decision.Reason)
				}
				break // 每个场景验证一个未声明入口即可（矩阵其余部分由 bot 包单测覆盖）
			}
		})
	}
}
