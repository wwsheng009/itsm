package bot

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"itsm-backend/ent/enttest"

	_ "github.com/mattn/go-sqlite3"
)

// 业务场景种子测试（S1～S7）：幂等、风险上限矩阵、业务边界（零写入 /
// 无自动改级 / 无自动发布 / 写面最小）。

func newScenarioEnv(t *testing.T) (*TemplateAdmin, context.Context, int) {
	t.Helper()
	ctx := context.Background()
	dsn := "file:bot-scenario-" + t.Name() + "?mode=memory&cache=shared&_fk=1"
	client := enttest.Open(t, "sqlite3", dsn)
	t.Cleanup(func() { _ = client.Close() })
	tenant := client.Tenant.Create().SetCode("sc-1").SetName("场景租户").SaveX(ctx)
	return NewTemplateAdmin(client), ctx, tenant.ID
}

// wantScenarioRisk 是各场景的预期风险上限（最小必要授权，口径见 scenarios.go 文件头）。
var wantScenarioRisk = map[string]string{
	"s1-ticket-assistant":       RiskActLow,
	"s2-incident-oncall":        RiskActLow,
	"s3-knowledge-assistant":    RiskActLow,
	"s4-change-impact-analyst":  RiskPlan,
	"s5-incident-postmortem":    RiskPlan,
	"s6-service-desk-intake":    RiskActLow,
	"s7-ticket-quality-auditor": RiskActMedium,
}

// wantScenarioWrites 是各场景允许的写工具全集（业务边界；其余授权必须为只读/plan）。
var wantScenarioWrites = map[string][]string{
	"s1-ticket-assistant":       {"create_ticket"},
	"s2-incident-oncall":        {"link_ticket_ci"},
	"s3-knowledge-assistant":    {},
	"s4-change-impact-analyst":  {},
	"s5-incident-postmortem":    {},
	"s6-service-desk-intake":    {"create_ticket"},
	"s7-ticket-quality-auditor": {"update_ticket"},
}

// scenarioWriteTools 是内置注册表中的写工具集合（B0-01 标注，见
// service/tool_metadata_test.go）。新增写工具时必须同步本表——否则写面边界断言会
// 因「未识别为写」而漏判。
var scenarioWriteTools = map[string]bool{
	"link_ticket_ci":         true,
	"create_ticket":          true,
	"update_ticket":          true,
	"create_ticket_type":     true,
	"create_ci_relationship": true,
	"delete_ci_relationship": true,
}

func TestScenarioBots_Definitions(t *testing.T) {
	scenarios := ScenarioBots()
	require.Len(t, scenarios, len(wantScenarioRisk))

	bySlug := map[string]ScenarioBot{}
	for _, s := range scenarios {
		bySlug[s.Slug] = s
		require.NotEmpty(t, s.Name, s.Slug)
		require.NotEmpty(t, s.Entrypoints, s.Slug)
		assert.Equal(t, wantScenarioRisk[s.Slug], s.RiskLimit, "%s 风险上限与矩阵不一致", s.Slug)

		var writes []string
		for _, grant := range s.Grants {
			assert.NotEmpty(t, grant.ToolName, s.Slug)
			assert.LessOrEqual(t, RiskRank(grant.RiskLimit), RiskRank(s.RiskLimit),
				"%s: 授权上限 %s 不得超过模板上限 %s", s.Slug, grant.RiskLimit, s.RiskLimit)
			if scenarioWriteTools[grant.ToolName] {
				writes = append(writes, grant.ToolName)
			}
		}
		assert.ElementsMatch(t, wantScenarioWrites[s.Slug], writes, "%s 写面边界漂移", s.Slug)
	}
	assert.Len(t, bySlug, len(wantScenarioRisk), "场景 slug 集合与矩阵不一致")
	if t.Failed() {
		return
	}

	t.Run("S1 含对话创建工单与字段草案", func(t *testing.T) {
		s1 := bySlug["s1-ticket-assistant"]
		assert.True(t, containsGrant(s1, "create_ticket"))
		assert.True(t, containsGrant(s1, "draft_ticket_fields"))
		assert.Contains(t, s1.Entrypoints, EntrypointTicketDetail)
	})

	t.Run("S2 不得包含自动改级/改状态工具", func(t *testing.T) {
		s2 := bySlug["s2-incident-oncall"]
		require.NotEmpty(t, s2.Grants)
		forbidden := []string{"update_incident", "declare_major", "escalate_incident", "resolve_incident", "update_ticket"}
		for _, grant := range s2.Grants {
			assert.NotContainsf(t, forbidden, grant.ToolName, "S2 只能是建议者：%s", grant.ToolName)
		}
		assert.True(t, containsGrant(s2, "link_ticket_ci"), "S2 关联 CI 走确认闭环")
	})

	t.Run("S3 不得包含发布类工具（草稿只落产物）", func(t *testing.T) {
		s3 := bySlug["s3-knowledge-assistant"]
		require.NotEmpty(t, s3.Grants)
		for _, grant := range s3.Grants {
			assert.NotContainsf(t, []string{"publish_kb_article", "publish_article", "create_kb_article"}, grant.ToolName,
				"S3 不得自动发布：%s", grant.ToolName)
		}
		assert.True(t, containsGrant(s3, "draft_kb_article"))
	})

	t.Run("S4/S5 零写入（只读 + plan 产物）", func(t *testing.T) {
		for _, slug := range []string{"s4-change-impact-analyst", "s5-incident-postmortem"} {
			s := bySlug[slug]
			require.NotEmpty(t, s.Grants, slug)
			for _, grant := range s.Grants {
				assert.LessOrEqual(t, RiskRank(grant.RiskLimit), RiskRank(RiskPlan),
					"%s: %s 的授权上限不得超过 plan（零写入）", slug, grant.ToolName)
				assert.False(t, scenarioWriteTools[grant.ToolName], "%s 不得授予写工具 %s", slug, grant.ToolName)
			}
		}
		s4 := bySlug["s4-change-impact-analyst"]
		assert.True(t, containsGrant(s4, "analyze_ci_impact_plan"), "S4 需影响面分析产物")
		assert.True(t, containsGrant(s4, "get_ci_relationships"), "S4 需 CI 拓扑读取")
		assert.Contains(t, s4.Entrypoints, EntrypointCIDetail)

		s5 := bySlug["s5-incident-postmortem"]
		assert.True(t, containsGrant(s5, "draft_kb_article"), "S5 复盘沉淀知识草稿（不发布）")
		assert.False(t, containsGrant(s5, "update_ticket"), "S5 不做工单变更")
		assert.Contains(t, s5.Entrypoints, EntrypointIncidentDetail)
	})

	t.Run("S6 写面仅 create_ticket（受理三入口）", func(t *testing.T) {
		s6 := bySlug["s6-service-desk-intake"]
		assert.True(t, containsGrant(s6, "create_ticket"))
		assert.True(t, containsGrant(s6, "list_kb"), "受理需知识建议")
		assert.False(t, containsGrant(s6, "update_ticket"))
		assert.Contains(t, s6.Entrypoints, EntrypointTicketList)
		assert.Contains(t, s6.Entrypoints, EntrypointIncidentCreate)
	})

	t.Run("S7 写面仅 update_ticket 且不触 act_high", func(t *testing.T) {
		s7 := bySlug["s7-ticket-quality-auditor"]
		assert.True(t, containsGrant(s7, "update_ticket"))
		assert.Equal(t, RiskActMedium, s7.RiskLimit)
		for _, grant := range s7.Grants {
			assert.LessOrEqual(t, RiskRank(grant.RiskLimit), RiskRank(RiskActMedium),
				"S7 不得触及 act_high：%s", grant.ToolName)
			assert.False(t, forbiddenHighRiskTools[grant.ToolName], "S7 不得授予高危工具 %s", grant.ToolName)
		}
	})
}

// forbiddenHighRiskTools 是「任何预置场景都不得授予」的管理/高危工具（act_high 与删除类）。
var forbiddenHighRiskTools = map[string]bool{
	"create_ticket_type":     true,
	"create_ci_relationship": true,
	"delete_ci_relationship": true,
}

func TestSeedScenarioBots_IdempotentAndNonOverwriting(t *testing.T) {
	admin, ctx, tenantID := newScenarioEnv(t)

	wantTemplates := len(wantScenarioRisk)
	wantGrants := 0
	for _, scenario := range ScenarioBots() {
		wantGrants += len(scenario.Grants)
	}

	first, err := admin.SeedScenarioBots(ctx, tenantID)
	require.NoError(t, err)
	assert.Equal(t, wantTemplates, first.CreatedTemplates)
	assert.Equal(t, wantGrants, first.CreatedGrants)
	assert.Zero(t, first.SkippedGrants)

	// 复跑：不重复建模板/授权。
	second, err := admin.SeedScenarioBots(ctx, tenantID)
	require.NoError(t, err)
	assert.Zero(t, second.CreatedTemplates)
	assert.Zero(t, second.CreatedGrants)
	assert.Equal(t, wantGrants, second.SkippedGrants)

	// 管理员改动不被覆盖：改 S1 名称与风险上限后复跑，字段保持管理员版本。
	templates, err := admin.ListTemplates(ctx, tenantID)
	require.NoError(t, err)
	var s1ID int
	for _, tpl := range templates {
		if tpl.Slug == "s1-ticket-assistant" {
			s1ID = tpl.ID
		}
	}
	require.NotZero(t, s1ID)
	_, err = admin.UpdateTemplate(ctx, tenantID, s1ID, TemplateInput{Name: "工单助手（管理员改名）", RiskLimit: RiskActMedium})
	require.NoError(t, err)

	_, err = admin.SeedScenarioBots(ctx, tenantID)
	require.NoError(t, err)
	updated, err := admin.GetTemplate(ctx, tenantID, s1ID)
	require.NoError(t, err)
	assert.Equal(t, "工单助手（管理员改名）", updated.Name)
	assert.Equal(t, RiskActMedium, updated.RiskLimit)

	// 场景模板状态为 pilot（不参与 GA 严格交集，走兼容默认；上线由管理员显式改为 ga）。
	for _, tpl := range templates {
		if tpl.Slug == "s1-ticket-assistant" {
			assert.Equal(t, StatusPilot, tpl.Status)
		}
	}
}

func containsGrant(s ScenarioBot, toolName string) bool {
	for _, grant := range s.Grants {
		if grant.ToolName == toolName {
			return true
		}
	}
	return false
}
