package schema_test

import (
	"context"
	"testing"

	"itsm-backend/ent/bottemplate"
	"itsm-backend/ent/bottoolgrant"
	"itsm-backend/ent/enttest"
	_ "itsm-backend/ent/runtime"

	_ "github.com/mattn/go-sqlite3"
	"github.com/stretchr/testify/require"
)

// B2-01 迁移证据：表/列齐备、租户内 slug 唯一、同模板同工具授权唯一、迁移幂等。
// 说明：与 mcp_migration_test.go / bot_migration_test.go 同包（外部测试包），
// `ent generate ./schema` 不读取 _test.go。

func TestBotTemplateSchemaMigration_CreatesTablesAndIndexes(t *testing.T) {
	ctx := context.Background()
	dsn := mcpSchemaDSN(t)
	client := enttest.Open(t, "sqlite3", dsn)
	defer client.Close()
	db := openMCPSchemaDB(t, dsn)

	for _, table := range []string{"bot_templates", "bot_tool_grants"} {
		require.True(t, mcpTableExists(t, db, table), "缺少 %s 表", table)
	}

	templateColumns := mcpTableColumns(t, db, "bot_templates")
	for _, column := range []string{
		"tenant_id", "slug", "name", "audience", "risk_limit",
		"entrypoints_json", "system_prompt_ref", "status", "created_at", "updated_at",
	} {
		require.True(t, templateColumns[column], "bot_templates 缺少列 %s", column)
	}
	grantColumns := mcpTableColumns(t, db, "bot_tool_grants")
	for _, column := range []string{
		"tenant_id", "bot_id", "tool_name", "risk_limit", "args_policy_json", "created_at", "updated_at",
	} {
		require.True(t, grantColumns[column], "bot_tool_grants 缺少列 %s", column)
	}

	// 迁移幂等：同一 schema 再次 Create 不报错。
	require.NoError(t, client.Schema.Create(ctx))
}

func TestBotTemplateSchema_TenantScopedUniqueAndGrants(t *testing.T) {
	ctx := context.Background()
	client := enttest.Open(t, "sqlite3", mcpSchemaDSN(t))
	defer client.Close()

	// 同租户 slug 唯一。
	client.BotTemplate.Create().
		SetTenantID(1).SetSlug("default-assistant").SetName("默认助手").SaveX(ctx)
	_, err := client.BotTemplate.Create().
		SetTenantID(1).SetSlug("default-assistant").SetName("重复").Save(ctx)
	require.Error(t, err, "同租户重复 slug 必须被唯一约束拒绝")

	// 不同租户可同名（多租户各自自定义）。
	client.BotTemplate.Create().
		SetTenantID(2).SetSlug("default-assistant").SetName("默认助手（租户2）").SaveX(ctx)

	// 默认值：status=draft、entrypoints_json=[]、audience=internal。
	created, err := client.BotTemplate.Query().
		Where(bottemplate.TenantID(1), bottemplate.SlugEQ("default-assistant")).Only(ctx)
	require.NoError(t, err)
	require.Equal(t, "draft", created.Status)
	require.Equal(t, "internal", created.Audience)
	require.Equal(t, "[]", created.EntrypointsJSON)
	require.Equal(t, "act_low", created.RiskLimit)

	// 同模板同工具授权唯一；不同工具/不同模板可并存。
	client.BotToolGrant.Create().
		SetTenantID(1).SetBotID(created.ID).SetToolName("stub__create_note").SaveX(ctx)
	_, err = client.BotToolGrant.Create().
		SetTenantID(1).SetBotID(created.ID).SetToolName("stub__create_note").Save(ctx)
	require.Error(t, err, "同模板同工具重复授权必须被唯一约束拒绝")
	client.BotToolGrant.Create().
		SetTenantID(1).SetBotID(created.ID).SetToolName("stub__list_notes").SaveX(ctx)
	other, err := client.BotTemplate.Query().Where(bottemplate.TenantID(2)).Only(ctx)
	require.NoError(t, err)
	client.BotToolGrant.Create().
		SetTenantID(2).SetBotID(other.ID).SetToolName("stub__create_note").SaveX(ctx)

	// 按工具反查（租户维度索引）：跨模板命中两条。
	rows, err := client.BotToolGrant.Query().
		Where(bottoolgrant.ToolName("stub__create_note")).All(ctx)
	require.NoError(t, err)
	require.Len(t, rows, 2)

	// edge 反向查询：模板 → 授权。
	grants, err := created.QueryGrants().All(ctx)
	require.NoError(t, err)
	require.Len(t, grants, 2)
}
