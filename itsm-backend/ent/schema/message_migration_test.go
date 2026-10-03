package schema_test

import (
	"context"
	"testing"

	"itsm-backend/ent/enttest"
	"itsm-backend/ent/message"

	_ "github.com/mattn/go-sqlite3"
	"github.com/stretchr/testify/require"
)

// TestMessageSchemaMigration_TenantID（IP-P2-3 收尾，2026-10-03）：messages 租户化取证——
// 列存在；tenant_id 必填（无租户写入被拒）；带租户写入可按租户谓词查询；Schema.Create 幂等。
// 对应磁盘迁移 migrations/20261003_messages_tenant_not_null.sql。
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

	got, err := client.Message.Query().Where(message.TenantID(7)).All(ctx)
	require.NoError(t, err)
	require.Len(t, got, 1)
	require.Equal(t, scoped.ID, got[0].ID)

	// 收尾后租户必填：缺 tenant_id 的写入必须失败（写入侧由 repository 派生，DB/ent 兜底）。
	_, err = client.Message.Create().
		SetConversationID(conv.ID).SetRole("assistant").SetContent("hello").Save(ctx)
	require.Error(t, err, "tenant_id 缺失时应拒绝写入（收尾后必填）")

	// 迁移幂等：同一 schema 再次 Create 不应报错。
	require.NoError(t, client.Schema.Create(ctx))
}
