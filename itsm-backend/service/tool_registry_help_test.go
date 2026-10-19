package service

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"itsm-backend/ent/botartifact"
	"itsm-backend/ent/enttest"
	"itsm-backend/service/bot"
)

// H0-03 Help Agent 工具执行面：只读、无 DB 依赖、参数缺失 fail-visible。

func execHelp(t *testing.T, reg *ToolRegistry, name string, args map[string]interface{}) map[string]interface{} {
	t.Helper()
	raw, err := reg.Execute(context.Background(), 1, name, args)
	require.NoError(t, err)
	// Execute 可能带元数据包装 → JSON 往返后再断言（对形状保持宽容）。
	buf, err := json.Marshal(raw)
	require.NoError(t, err)
	var payload map[string]interface{}
	require.NoError(t, json.Unmarshal(buf, &payload))
	return payload
}

func TestHelpTools_ExecuteBuiltin(t *testing.T) {
	reg := NewToolRegistry(nil, nil, nil, nil)

	// help_search：SLA 命中帮助文档。
	search := execHelp(t, reg, "help_search", map[string]interface{}{"q": "SLA", "limit": float64(5)})
	assert.Greater(t, search["count"].(float64), float64(0))
	results, ok := search["results"].([]interface{})
	require.True(t, ok)
	require.NotEmpty(t, results)

	// help_get_page：页面状态与正文。
	page := execHelp(t, reg, "help_get_page", map[string]interface{}{"pageKey": "ticket.detail"})
	assert.Equal(t, "ok", page["status"])
	assert.Equal(t, "ticket.detail", page["pageKey"])
	assert.NotEmpty(t, page["docs"])

	// help_read_doc：按路径读取全文。
	doc := execHelp(t, reg, "help_read_doc", map[string]interface{}{"path": "docs/help/ai/chat.md"})
	assert.Equal(t, "AI 助手（对话工作区）", doc["title"])
	assert.Contains(t, doc["text"], "确认卡片")

	// help_list_gaps：当前种子文档 100% 覆盖 → gaps 为空（coverage=1.0）。
	gaps := execHelp(t, reg, "help_list_gaps", map[string]interface{}{})
	assert.Equal(t, float64(1), gaps["coverage"].(float64))
	assert.Empty(t, gaps["gaps"])
}

func TestHelpTools_MissingArgsAreVisibleErrors(t *testing.T) {
	reg := NewToolRegistry(nil, nil, nil, nil)

	_, err := reg.Execute(context.Background(), 1, "help_get_page", map[string]interface{}{})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "pageKey")

	_, err = reg.Execute(context.Background(), 1, "help_read_doc", map[string]interface{}{})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "path")

	_, err = reg.Execute(context.Background(), 1, "help_read_doc", map[string]interface{}{"path": "docs/help/none.md"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not found")

	// 空查询 → 空结果（不报错，模型可自行改写查询）。
	res := execHelp(t, reg, "help_search", map[string]interface{}{"q": "   "})
	assert.Equal(t, float64(0), res["count"].(float64))
}

// TestHelpCreateDraft_Artifact：H0-04 草稿通道——只落 bot_artifacts（plan）、归属隔离、fail-visible。
func TestHelpCreateDraft_Artifact(t *testing.T) {
	ctx := context.Background()
	dsn := "file:help-draft-" + t.Name() + "?mode=memory&cache=shared&_fk=1"
	client := enttest.Open(t, "sqlite3", dsn)
	t.Cleanup(func() { _ = client.Close() })

	tenantA := client.Tenant.Create().SetCode("help-a").SetName("A").SaveX(ctx)
	tenantB := client.Tenant.Create().SetCode("help-b").SetName("B").SaveX(ctx)
	userA := client.User.Create().
		SetUsername("help-a-user").SetEmail("help-a@example.com").SetName("A User").
		SetPasswordHash("x").SetTenantID(tenantA.ID).SaveX(ctx)

	registry := NewToolRegistry(nil, nil, nil, client)
	registry.SetArtifactStore(bot.NewArtifactStore(client))
	runCtx := WithToolActor(WithToolConversation(ctx, 77), userA.ID)

	res, err := registry.Execute(runCtx, tenantA.ID, "help_create_draft", map[string]interface{}{
		"pageKey":    "ticket.detail",
		"content":    "## 适用场景\n- 草稿内容（不发布）",
		"sourceDocs": []interface{}{"docs/help/tickets/list.md"},
	})
	require.NoError(t, err)
	payload, ok := res.(map[string]interface{})
	require.True(t, ok)
	assert.Equal(t, bot.ArtifactKindDraft, payload["kind"])
	assert.Equal(t, "帮助文档草稿：工单详情", payload["title"])

	artifactId, _ := payload["artifactId"].(int)
	require.NotZero(t, artifactId)
	artifact, err := client.BotArtifact.Query().Where(botartifact.ID(artifactId)).Only(ctx)
	require.NoError(t, err)
	assert.Equal(t, userA.ID, artifact.OwnerUserID)
	assert.Equal(t, tenantA.ID, artifact.TenantID)
	assert.Contains(t, artifact.ContentJSON, "ticket.detail")
	assert.Contains(t, artifact.EvidenceJSON, "docs/help/tickets/list.md")
	assert.Contains(t, artifact.EvidenceJSON, "\"pageStatus\":\"ok\"")

	// 归属/租户隔离：以租户 B 查询同一产物 → 不存在（ArtifactStore 双条件语义）。
	other, err := client.BotArtifact.Query().
		Where(botartifact.ID(artifactId), botartifact.TenantID(tenantB.ID)).
		Only(ctx)
	require.Error(t, err)
	require.Nil(t, other)

	// 未注入产物存储 → 明确失败（fail-visible）。
	bare := NewToolRegistry(nil, nil, nil, nil)
	_, err = bare.Execute(runCtx, tenantA.ID, "help_create_draft", map[string]interface{}{
		"pageKey": "ticket.detail", "content": "x",
	})
	require.ErrorContains(t, err, "未启用")

	// 参数缺失 → 明确错误。
	_, err = registry.Execute(runCtx, tenantA.ID, "help_create_draft", map[string]interface{}{"pageKey": "x"})
	require.ErrorContains(t, err, "content")
}
