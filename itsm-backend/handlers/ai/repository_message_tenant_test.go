package ai_test

// IP-P2-3 收尾（2026-10-03）：CreateMessage 的 tenant_id 解析契约——
// ctx 租户优先；ctx 缺失时由会话派生；跨租户冲突与（会话无租户且 ctx 无租户）均 fail-closed。

import (
	"context"
	"testing"

	"itsm-backend/common/tenantctx"
	"itsm-backend/ent/enttest"
	"itsm-backend/handlers/ai"

	_ "github.com/mattn/go-sqlite3"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestEntRepository_CreateMessageTenantResolution(t *testing.T) {
	client := enttest.Open(t, "sqlite3", "file:ai_repo_msg_tenant?mode=memory&cache=shared&_fk=1")
	t.Cleanup(func() { _ = client.Close() })
	repo := ai.NewEntRepository(client)
	ctx := context.Background()

	tenantA := client.Tenant.Create().SetCode("msg-tenant-a").SetName("Msg Tenant A").SaveX(ctx)
	tenantB := client.Tenant.Create().SetCode("msg-tenant-b").SetName("Msg Tenant B").SaveX(ctx)
	convA := client.Conversation.Create().SetTenantID(tenantA.ID).SetTitle("A 会话").SaveX(ctx)
	convNoTenant := client.Conversation.Create().SetTitle("无租户会话").SaveX(ctx)

	// ① ctx 无租户 → 由会话派生并落库。
	m1, err := repo.CreateMessage(context.Background(), &ai.Message{
		ConversationID: convA.ID, Role: "user", Content: "no-ctx",
	})
	require.NoError(t, err)
	e1, err := client.Message.Get(ctx, m1.ID)
	require.NoError(t, err)
	assert.Equal(t, tenantA.ID, e1.TenantID)

	// ② ctx 租户与会话一致 → 正常写入。
	m2, err := repo.CreateMessage(tenantctx.WithTenantID(ctx, tenantA.ID), &ai.Message{
		ConversationID: convA.ID, Role: "assistant", Content: "same-tenant",
	})
	require.NoError(t, err)
	e2, err := client.Message.Get(ctx, m2.ID)
	require.NoError(t, err)
	assert.Equal(t, tenantA.ID, e2.TenantID)

	// ③ ctx 与会话跨租户 → 冲突 fail-closed，不落库。
	before, err := client.Message.Query().Count(ctx)
	require.NoError(t, err)
	_, err = repo.CreateMessage(tenantctx.WithTenantID(ctx, tenantB.ID), &ai.Message{
		ConversationID: convA.ID, Role: "user", Content: "cross-tenant",
	})
	require.ErrorContains(t, err, "tenant mismatch")
	after, err := client.Message.Query().Count(ctx)
	require.NoError(t, err)
	assert.Equal(t, before, after, "冲突请求不得写入")

	// ④ 会话无租户且 ctx 无租户 → unresolved fail-closed。
	_, err = repo.CreateMessage(context.Background(), &ai.Message{
		ConversationID: convNoTenant.ID, Role: "user", Content: "unresolved",
	})
	require.ErrorContains(t, err, "tenant unresolved")

	// ⑤ 会话无租户但 ctx 有租户（历史会话被新请求接管）→ 以 ctx 租户落库。
	m3, err := repo.CreateMessage(tenantctx.WithTenantID(ctx, tenantB.ID), &ai.Message{
		ConversationID: convNoTenant.ID, Role: "user", Content: "ctx-wins",
	})
	require.NoError(t, err)
	e3, err := client.Message.Get(ctx, m3.ID)
	require.NoError(t, err)
	assert.Equal(t, tenantB.ID, e3.TenantID)
}
