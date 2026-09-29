package ai_test

import (
	"context"
	"testing"

	"itsm-backend/ent/enttest"
	"itsm-backend/handlers/ai"

	_ "github.com/mattn/go-sqlite3"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// B2-04：会话 → Bot 归属字段（bot_id）读写契约。
//
// 语义：0 = 未绑定 = 内置默认助手（兼容默认）；新会话创建时按选择器写入，
// 读路径必须保真回读（历史会话归属由此还原，不随选择器变更而改写）。
func TestEntRepository_ConversationBotBinding(t *testing.T) {
	client := enttest.Open(t, "sqlite3", "file:ai_repo_b2_conversation?mode=memory&cache=shared&_fk=1")
	t.Cleanup(func() { _ = client.Close() })

	repo := ai.NewEntRepository(client)
	ctx := context.Background()

	tenant, err := client.Tenant.Create().SetCode("bot-binding").SetName("Bot Binding").Save(ctx)
	require.NoError(t, err)

	// ① 未指定（0）→ 落 bot_id=0，读回 0（默认助手）。
	plain, err := repo.CreateConversation(ctx, &ai.Conversation{
		Title: "默认助手会话", UserID: 0, TenantID: tenant.ID,
	})
	require.NoError(t, err)
	assert.Equal(t, 0, plain.BotID)

	read, err := repo.GetConversation(ctx, plain.ID, tenant.ID)
	require.NoError(t, err)
	assert.Equal(t, 0, read.BotID)

	// ② 指定 Bot → 往返一致。
	bound, err := repo.CreateConversation(ctx, &ai.Conversation{
		Title: "运维助手会话", TenantID: tenant.ID, BotID: 42,
	})
	require.NoError(t, err)
	assert.Equal(t, 42, bound.BotID)

	read, err = repo.GetConversation(ctx, bound.ID, tenant.ID)
	require.NoError(t, err)
	assert.Equal(t, 42, read.BotID, "会话归属必须原样读回")

	// ③ 列表路径同样带出归属（工作区会话列表按会话展示 Bot）。
	rows, err := repo.ListConversations(ctx, tenant.ID, 0)
	require.NoError(t, err)
	byID := map[int]int{}
	for _, row := range rows {
		byID[row.ID] = row.BotID
	}
	assert.Equal(t, 0, byID[plain.ID])
	assert.Equal(t, 42, byID[bound.ID])

	// ④ 跨租户读取不泄露（既有租户隔离语义保持）。
	other, err := client.Tenant.Create().SetCode("bot-binding-2").SetName("Bot Binding 2").Save(ctx)
	require.NoError(t, err)
	_, err = repo.GetConversation(ctx, bound.ID, other.ID)
	require.Error(t, err)
}
