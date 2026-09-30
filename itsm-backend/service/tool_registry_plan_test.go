package service

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"itsm-backend/ent/botartifact"
	"itsm-backend/ent/enttest"
	"itsm-backend/ent/knowledgearticle"
	"itsm-backend/ent/ticket"
	"itsm-backend/service/bot"
)

// B3-06 plan/analysis/draft 工具集成测试：
// 产物归属 + 租户隔离 + **零业务写入** + 证据引用。
func TestPlanTools_ArtifactsAndZeroBusinessWrites(t *testing.T) {
	ctx := context.Background()
	dsn := "file:plan-tools-" + t.Name() + "?mode=memory&cache=shared&_fk=1"
	client := enttest.Open(t, "sqlite3", dsn)
	t.Cleanup(func() { _ = client.Close() })

	tenantA := client.Tenant.Create().SetCode("plan-a").SetName("A").SaveX(ctx)
	tenantB := client.Tenant.Create().SetCode("plan-b").SetName("B").SaveX(ctx)
	userA := client.User.Create().
		SetUsername("plan-a-user").SetEmail("plan-a@example.com").SetName("A User").
		SetPasswordHash("x").SetTenantID(tenantA.ID).SaveX(ctx)
	// ci_type_id 是外键：先落一条 CI 类型，避免 FK 约束失败。
	ciType := client.CIType.Create().
		SetName("服务器").SetTenantID(tenantA.ID).SaveX(ctx)
	ciA := client.ConfigurationItem.Create().
		SetTenantID(tenantA.ID).SetCiNumber("CI-PLAN-1").SetName("核心数据库").
		SetCiTypeID(ciType.ID).SetCiType("server").SaveX(ctx)

	registry := NewToolRegistry(nil, nil, nil, client)
	registry.SetArtifactStore(bot.NewArtifactStore(client))

	runCtx := WithToolActor(WithToolConversation(ctx, 55), userA.ID)

	t.Run("draft_ticket_fields 产出 plan 产物", func(t *testing.T) {
		res, err := registry.Execute(runCtx, tenantA.ID, "draft_ticket_fields", map[string]interface{}{
			"description": "打印机宕机，整个楼层无法打印",
		})
		require.NoError(t, err)
		payload, ok := res.(map[string]interface{})
		require.True(t, ok)
		assert.Equal(t, bot.ArtifactKindPlan, payload["kind"])
		assert.NotZero(t, payload["artifactId"])

		artifact, err := client.BotArtifact.Query().Where(botartifact.ID(payload["artifactId"].(int))).Only(ctx)
		require.NoError(t, err)
		assert.Equal(t, userA.ID, artifact.OwnerUserID)
		assert.Equal(t, 55, artifact.ConversationID)
		assert.Contains(t, artifact.ContentJSON, "high") // 宕机 → 建议高优先级
	})

	t.Run("analyze_ci_impact_plan 只读本租户 CI 且带证据", func(t *testing.T) {
		res, err := registry.Execute(runCtx, tenantA.ID, "analyze_ci_impact_plan", map[string]interface{}{
			"ciId": float64(ciA.ID), "change": "扩容存储",
		})
		require.NoError(t, err)
		payload := res.(map[string]interface{})
		assert.Equal(t, bot.ArtifactKindAnalysis, payload["kind"])
		artifact, err := client.BotArtifact.Query().Where(botartifact.ID(payload["artifactId"].(int))).Only(ctx)
		require.NoError(t, err)
		assert.Contains(t, artifact.EvidenceJSON, "核心数据库")

		// 跨租户 CI：不可用且**不产生**新产物。
		before, err := client.BotArtifact.Query().Count(ctx)
		require.NoError(t, err)
		_, err = registry.Execute(runCtx, tenantB.ID, "analyze_ci_impact_plan", map[string]interface{}{"ciId": float64(ciA.ID)})
		require.ErrorContains(t, err, "配置项不可用")
		after, err := client.BotArtifact.Query().Count(ctx)
		require.NoError(t, err)
		assert.Equal(t, before, after)
	})

	t.Run("draft_kb_article 产出 draft 产物且不发布", func(t *testing.T) {
		res, err := registry.Execute(runCtx, tenantA.ID, "draft_kb_article", map[string]interface{}{
			"title": "打印机故障处置", "content": "1. 检查队列；2. 重启服务",
		})
		require.NoError(t, err)
		payload := res.(map[string]interface{})
		assert.Equal(t, bot.ArtifactKindDraft, payload["kind"])
	})

	t.Run("零业务写入", func(t *testing.T) {
		tickets, err := client.Ticket.Query().Where(ticket.TenantID(tenantA.ID)).Count(ctx)
		require.NoError(t, err)
		assert.Zero(t, tickets, "plan/analysis/draft 工具不得写工单")
		articles, err := client.KnowledgeArticle.Query().Where(knowledgearticle.TenantID(tenantA.ID)).Count(ctx)
		require.NoError(t, err)
		assert.Zero(t, articles, "draft 工具不得直接创建知识文章（草稿只在产物中）")
	})

	t.Run("缺少发起人上下文时拒绝且不产生产物", func(t *testing.T) {
		before, err := client.BotArtifact.Query().Count(ctx)
		require.NoError(t, err)
		_, err = registry.Execute(ctx, tenantA.ID, "draft_kb_article", map[string]interface{}{"title": "x", "content": "y"})
		require.ErrorContains(t, err, "发起人上下文")
		after, err := client.BotArtifact.Query().Count(ctx)
		require.NoError(t, err)
		assert.Equal(t, before, after)
	})

	t.Run("能力未注入时明确报错", func(t *testing.T) {
		bare := NewToolRegistry(nil, nil, nil, client)
		_, err := bare.Execute(runCtx, tenantA.ID, "draft_ticket_fields", map[string]interface{}{"description": "x"})
		require.ErrorContains(t, err, "能力未启用")
	})
}
