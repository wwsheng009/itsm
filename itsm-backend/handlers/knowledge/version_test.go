package knowledge

import (
	"context"
	"testing"

	"itsm-backend/dto"
	"itsm-backend/ent"
	"itsm-backend/ent/enttest"

	_ "github.com/mattn/go-sqlite3"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap/zaptest"
)

// TestArticleVersionHistory_Lifecycle 覆盖版本历史的写入与读取闭环：
// 创建落初始版本 → 每次更新把新正文记为一条版本 → 列表倒序 →
// 恢复旧版本（回写内容并记录恢复结果）→ 版本对比。
func TestArticleVersionHistory_Lifecycle(t *testing.T) {
	ctx := context.Background()
	client := enttest.Open(t, "sqlite3", "file:knowledge_versions?mode=memory&cache=shared&_fk=1")
	defer client.Close()

	svc := NewService(NewEntRepository(client), zaptest.NewLogger(t).Sugar())

	created, err := svc.CreateArticle(ctx, &Article{
		Title: "v1 标题", Content: "line-a\nline-b", Category: "操作指南",
		AuthorID: 1, TenantID: 7,
	})
	require.NoError(t, err)

	versions, err := svc.ListArticleVersions(ctx, created.ID, 7)
	require.NoError(t, err)
	require.Len(t, versions, 1)
	require.Equal(t, 1, versions[0].Version)
	require.Equal(t, "初始版本", versions[0].ChangeSummary)
	require.Equal(t, "v1 标题", versions[0].Title)

	// 更新：写入成功后把新正文记为 v2，因此最新版本始终等于文章当前内容。
	created.Title = "v2 标题"
	created.Content = "line-a\nline-c"
	updated, err := svc.UpdateArticle(ctx, created)
	require.NoError(t, err)
	require.Equal(t, "v2 标题", updated.Title)

	versions, err = svc.ListArticleVersions(ctx, updated.ID, 7)
	require.NoError(t, err)
	require.Len(t, versions, 2)
	require.Equal(t, 2, versions[0].Version) // 倒序：最新在前
	require.Equal(t, "文章更新", versions[0].ChangeSummary)
	require.Equal(t, "v2 标题", versions[0].Title)
	require.Equal(t, "line-a\nline-c", versions[0].Content)
	require.Equal(t, 1, versions[1].Version)

	// 恢复到 v1：标题/正文回写为 v1 的内容，并把恢复结果记为 v3。
	restored, err := svc.RestoreArticleVersion(ctx, updated.ID, 1, 7)
	require.NoError(t, err)
	require.Equal(t, "v1 标题", restored.Title)
	require.Equal(t, "line-a\nline-b", restored.Content)

	versions, err = svc.ListArticleVersions(ctx, restored.ID, 7)
	require.NoError(t, err)
	require.Len(t, versions, 3)
	require.Equal(t, 3, versions[0].Version)
	require.Equal(t, "恢复到 v1", versions[0].ChangeSummary)
	require.Equal(t, "v1 标题", versions[0].Title)
	require.Equal(t, "line-a\nline-b", versions[0].Content)

	// 对比 v1 → v2：line-b 被 line-c 替换。
	cmp, err := svc.CompareArticleVersions(ctx, restored.ID, 1, 2, 7)
	require.NoError(t, err)
	require.Equal(t, 1, cmp.FromVersion)
	require.Equal(t, 2, cmp.ToVersion)
	require.Contains(t, cmp.Changes, dto.KnowledgeArticleVersionChange{Type: "removed", Content: "line-b"})
	require.Contains(t, cmp.Changes, dto.KnowledgeArticleVersionChange{Type: "added", Content: "line-c"})

	// 跨租户必须按未找到返回，不能泄露版本历史。
	_, err = svc.ListArticleVersions(ctx, restored.ID, 999)
	require.Error(t, err)
	require.True(t, ent.IsNotFound(err))
}
