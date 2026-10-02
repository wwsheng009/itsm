package schema_test

import (
	"context"
	"testing"

	"itsm-backend/ent/enttest"
	"itsm-backend/ent/message"

	_ "github.com/mattn/go-sqlite3"
	"github.com/stretchr/testify/require"
)

// TestMessageSchemaMigration_TenantID（IP-P2-3）：messages 租户化取证——
// 列存在、带租户写入可按租户谓词查询、无租户写入保持可空（历史兼容）。
func TestMessageSchemaMigration_TenantID(t *testing.T) {
	ctx := context.Background()
	dsn := mcpSchemaDSN(t)
	client := enttest.Open(t, "sqlite3", dsn)
	defer client.Close()
	db := openMCPSchemaDB(t, dsn)

	cols := mcpTableColumns(t, db, "messages")
	require.True(t, cols["tenant_id"], "messages 缺少 tenant_id 列")

	conv := client.Conversation.Create().SetTitle("会话").SetTenantID(7).SaveX(ctx)

	scoped := client.Message.Create().
		SetConversationID(conv.ID).SetRole("user").SetContent("hi").SetTenantID(7).SaveX(ctx)
	legacy := client.Message.Create().
		SetConversationID(conv.ID).SetRole("assistant").SetContent("hello").SaveX(ctx)

	got, err := client.Message.Query().Where(message.TenantID(7)).All(ctx)
	require.NoError(t, err)
	require.Len(t, got, 1)
	require.Equal(t, scoped.ID, got[0].ID)

	legacyRow, err := client.Message.Get(ctx, legacy.ID)
	require.NoError(t, err)
	require.Zero(t, legacyRow.TenantID, "无租户上下文写入保持可空（历史兼容，由回填/巡检兜底）")

	// 迁移幂等：同一 schema 再次 Create 不应报错。
	require.NoError(t, client.Schema.Create(ctx))
}
