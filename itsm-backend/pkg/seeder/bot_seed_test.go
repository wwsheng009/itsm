package seeder

import (
	"testing"

	"itsm-backend/ent/bottemplate"
	"itsm-backend/ent/bottoolgrant"
	"itsm-backend/ent/tenant"
	"itsm-backend/pkg/tenantmode"
	servicebot "itsm-backend/service/bot"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestSeedBotTemplatesSeedsAssistantAndScenarios 校验 Bot 种子：
// 内置默认助手 + 三个场景 pilot 模板与授权齐备，且复跑幂等。
func TestSeedBotTemplatesSeedsAssistantAndScenarios(t *testing.T) {
	seeder, ctx := newTestSeeder(t, tenantmode.DeploymentModePrivate)
	root := seeder.seedDefaultTenant(ctx)
	require.NotNil(t, root)

	seeder.seedBotTemplates(ctx)

	templates, err := seeder.client.BotTemplate.Query().Where(bottemplate.TenantIDEQ(root.ID)).All(ctx)
	require.NoError(t, err)
	require.Len(t, templates, 1+len(servicebot.ScenarioBots()))

	statusBySlug := make(map[string]string, len(templates))
	for _, tpl := range templates {
		statusBySlug[tpl.Slug] = tpl.Status
	}
	assert.Equal(t, servicebot.StatusGA, statusBySlug[servicebot.DefaultTemplateSlug], "默认助手应为 ga")
	for _, scenario := range servicebot.ScenarioBots() {
		assert.Equal(t, servicebot.StatusPilot, statusBySlug[scenario.Slug], "场景 %s 应为 pilot", scenario.Slug)
	}

	wantGrants := 0
	for _, scenario := range servicebot.ScenarioBots() {
		wantGrants += len(scenario.Grants)
	}
	grants, err := seeder.client.BotToolGrant.Query().Where(bottoolgrant.TenantIDEQ(root.ID)).All(ctx)
	require.NoError(t, err)
	assert.Len(t, grants, wantGrants)

	// 复跑幂等：不新增模板/授权（管理员修改优先，种子只增不改）。
	seeder.seedBotTemplates(ctx)
	templatesAgain, err := seeder.client.BotTemplate.Query().Where(bottemplate.TenantIDEQ(root.ID)).Count(ctx)
	require.NoError(t, err)
	assert.Equal(t, 1+len(servicebot.ScenarioBots()), templatesAgain)
	grantsAgain, err := seeder.client.BotToolGrant.Query().Where(bottoolgrant.TenantIDEQ(root.ID)).Count(ctx)
	require.NoError(t, err)
	assert.Equal(t, wantGrants, grantsAgain)

	require.NoError(t, seeder.verifyBotTemplates(ctx))
}

// TestCloneTenantTemplatesCopiesBotTemplates 校验租户开通克隆（cloneTenantTemplates）：
// 目标租户继承默认租户的 Bot 模板与授权，且复跑幂等。
//
// 直测克隆函数而非 ProvisionTenant：保持用例聚焦（避开完整链路的角色/组织前置换数据）。
// 说明：此前 `incident_emergency_flow: process_definition not found`（B2-03 O-4 / M0-10）
// 的根因是 embed.FS 路径在 Windows 下被 filepath.Join 拼接出反斜杠，导致全部内置模板
// 部署失败、默认租户 0 流程定义；已修复（2026-10-03），完整链路由
// TestProvisionTenantReadinessAcrossDeploymentModes 覆盖。
func TestCloneTenantTemplatesCopiesBotTemplates(t *testing.T) {
	seeder, ctx := newTestSeeder(t, tenantmode.DeploymentModePrivate)
	root := seeder.seedDefaultTenant(ctx)
	require.NotNil(t, root)
	seeder.seedBotTemplates(ctx)

	sourceTemplates, err := seeder.client.BotTemplate.Query().Where(bottemplate.TenantIDEQ(root.ID)).Count(ctx)
	require.NoError(t, err)
	require.Equal(t, 1+len(servicebot.ScenarioBots()), sourceTemplates)
	sourceGrants, err := seeder.client.BotToolGrant.Query().Where(bottoolgrant.TenantIDEQ(root.ID)).Count(ctx)
	require.NoError(t, err)
	require.Positive(t, sourceGrants)

	target, err := seeder.client.Tenant.Create().
		SetName("Bot Clone Target").SetCode("bot-clone-target").
		SetType(tenant.TypeSaasCustomer).Save(ctx)
	require.NoError(t, err)

	require.NoError(t, cloneTenantTemplates(ctx, seeder.client, root.ID, target.ID))
	targetTemplates, err := seeder.client.BotTemplate.Query().Where(bottemplate.TenantIDEQ(target.ID)).Count(ctx)
	require.NoError(t, err)
	assert.Equal(t, sourceTemplates, targetTemplates)
	targetGrants, err := seeder.client.BotToolGrant.Query().Where(bottoolgrant.TenantIDEQ(target.ID)).Count(ctx)
	require.NoError(t, err)
	assert.Equal(t, sourceGrants, targetGrants)

	// 幂等复跑：不重复克隆。
	require.NoError(t, cloneTenantTemplates(ctx, seeder.client, root.ID, target.ID))
	targetTemplatesAgain, err := seeder.client.BotTemplate.Query().Where(bottemplate.TenantIDEQ(target.ID)).Count(ctx)
	require.NoError(t, err)
	assert.Equal(t, sourceTemplates, targetTemplatesAgain)
	targetGrantsAgain, err := seeder.client.BotToolGrant.Query().Where(bottoolgrant.TenantIDEQ(target.ID)).Count(ctx)
	require.NoError(t, err)
	assert.Equal(t, sourceGrants, targetGrantsAgain)
}
