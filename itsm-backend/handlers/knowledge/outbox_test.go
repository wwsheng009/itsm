package knowledge

import (
	"context"
	"testing"

	"itsm-backend/ent/enttest"
	"itsm-backend/ent/operationalcommand"
	"itsm-backend/internal/commandbus"
	"itsm-backend/service"

	_ "github.com/mattn/go-sqlite3"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap/zaptest"
)

// TestPublishArticle_EnqueuesVectorSyncInSameTransaction 验证新的发布口径下
// 向量索引与发布态严格对齐：草稿不索引，发布才入队同步，下架入队移除，
// 且向量命令与发布动作落在同一事务中。
func TestPublishArticle_EnqueuesVectorSyncInSameTransaction(t *testing.T) {
	ctx := context.Background()
	client := enttest.Open(t, "sqlite3", "file:knowledge_outbox?mode=memory&cache=shared&_fk=1")
	defer client.Close()

	svc := NewService(NewEntRepository(client), zaptest.NewLogger(t).Sugar())
	svc.SetEntClient(client)
	// A non-nil RAG service activates the production outbox path. Its vector
	// dependencies are intentionally absent: this test verifies persistence,
	// not a provider call.
	svc.SetRAG(service.NewRAGService(nil, nil, nil, zaptest.NewLogger(t).Sugar(), service.RAGConfig{}))

	countCommands := func(articleID int) int {
		n, err := client.OperationalCommand.Query().Where(
			operationalcommand.TenantIDEQ(7),
			operationalcommand.CommandTypeEQ(commandbus.CommandSyncKnowledgeVector),
			operationalcommand.AggregateIDEQ(articleID),
		).Count(ctx)
		require.NoError(t, err)
		return n
	}

	// 即使请求显式传 IsPublished，创建也必须落在草稿态且不产生向量命令。
	created, err := svc.CreateArticle(ctx, &Article{
		Title: "runbook", Content: "restart safely", Category: "操作指南",
		AuthorID: 1, TenantID: 7, IsPublished: true,
	})
	require.NoError(t, err)
	require.False(t, created.IsPublished, "创建一律为草稿")
	require.Zero(t, countCommands(created.ID), "草稿不得进入向量索引")

	// 发布：产生同步命令。
	published, err := svc.PublishArticle(ctx, created.ID, 7, 1, "")
	require.NoError(t, err)
	require.True(t, published.IsPublished)

	cmd, err := client.OperationalCommand.Query().Where(
		operationalcommand.TenantIDEQ(7),
		operationalcommand.CommandTypeEQ(commandbus.CommandSyncKnowledgeVector),
		operationalcommand.AggregateIDEQ(created.ID),
	).Only(ctx)
	require.NoError(t, err)
	require.Equal(t, "knowledge_article", cmd.AggregateType)
	require.Equal(t, vectorIndexSync, cmd.Payload["action"])

	// 下架：产生移除命令，且不产生版本（版本只属于发布动作）。
	unpublished, err := svc.UnpublishArticle(ctx, created.ID, 7)
	require.NoError(t, err)
	require.False(t, unpublished.IsPublished)
	require.Equal(t, 2, countCommands(created.ID), "下架应入队移除命令")

	versions, err := svc.ListArticleVersions(ctx, created.ID, 7)
	require.NoError(t, err)
	require.Len(t, versions, 1, "下架不产生版本")
}
