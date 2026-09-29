package bot

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"itsm-backend/ent/enttest"

	_ "github.com/mattn/go-sqlite3"
)

// B3-03/B3-04/B3-05 场景种子测试：幂等、边界（无自动改级/无自动发布）、下发矩阵可用。

func newScenarioEnv(t *testing.T) (*TemplateAdmin, context.Context, int) {
	t.Helper()
	ctx := context.Background()
	dsn := "file:bot-scenario-" + t.Name() + "?mode=memory&cache=shared&_fk=1"
	client := enttest.Open(t, "sqlite3", dsn)
	t.Cleanup(func() { _ = client.Close() })
	tenant := client.Tenant.Create().SetCode("sc-1").SetName("场景租户").SaveX(ctx)
	return NewTemplateAdmin(client), ctx, tenant.ID
}

func TestScenarioBots_Definitions(t *testing.T) {
	scenarios := ScenarioBots()
	require.Len(t, scenarios, 3)

	bySlug := map[string]ScenarioBot{}
	for _, s := range scenarios {
		bySlug[s.Slug] = s
		assert.NotEmpty(t, s.Entrypoints, s.Slug)
		assert.Equal(t, RiskActLow, s.RiskLimit, s.Slug)
	}

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

	t.Run("S1 含对话创建工单与字段草案", func(t *testing.T) {
		s1 := bySlug["s1-ticket-assistant"]
		assert.True(t, containsGrant(s1, "create_ticket"))
		assert.True(t, containsGrant(s1, "draft_ticket_fields"))
		assert.Contains(t, s1.Entrypoints, EntrypointTicketDetail)
	})
}

func TestSeedScenarioBots_IdempotentAndNonOverwriting(t *testing.T) {
	admin, ctx, tenantID := newScenarioEnv(t)

	first, err := admin.SeedScenarioBots(ctx, tenantID)
	require.NoError(t, err)
	assert.Equal(t, 3, first.CreatedTemplates)
	assert.Equal(t, 12, first.CreatedGrants) // S1 5 + S2 5 + S3 2
	assert.Zero(t, first.SkippedGrants)

	// 复跑：不重复建模板/授权。
	second, err := admin.SeedScenarioBots(ctx, tenantID)
	require.NoError(t, err)
	assert.Zero(t, second.CreatedTemplates)
	assert.Zero(t, second.CreatedGrants)
	assert.Equal(t, 12, second.SkippedGrants)

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
