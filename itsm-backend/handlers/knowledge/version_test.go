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

// TestArticleVersionHistory_Lifecycle 覆盖「版本 = 发布历史」的完整闭环：
//
//	创建即草稿（无版本）→ 保存草稿（无版本）→ 发布 v1 → 重复发布（幂等，无版本）
//	→ 编辑已发布文章回到草稿（无版本）→ 再次发布 v2 → 下架（无版本）
//	→ 原样重新上架（幂等，无版本）→ 恢复到 v1（草稿，无版本）→ 重新发布 v3
//	→ 版本对比 → 跨租户不可见。
func TestArticleVersionHistory_Lifecycle(t *testing.T) {
	ctx := context.Background()
	client := enttest.Open(t, "sqlite3", "file:knowledge_versions?mode=memory&cache=shared&_fk=1")
	defer client.Close()

	svc := NewService(NewEntRepository(client), zaptest.NewLogger(t).Sugar())

	created, err := svc.CreateArticle(ctx, &Article{
		Title: "标题 v2", Content: "line-a\nline-b", Category: "操作指南",
		AuthorID: 1, TenantID: 7,
	})
	require.NoError(t, err)
	require.False(t, created.IsPublished, "新建文章必须是草稿")

	// 创建与保存草稿都不是发布动作：版本历史必须为空。
	versions, err := svc.ListArticleVersions(ctx, created.ID, 7)
	require.NoError(t, err)
	require.Empty(t, versions)

	created.Content = "line-a\nline-c"
	updated, err := svc.UpdateArticle(ctx, created)
	require.NoError(t, err)
	require.False(t, updated.IsPublished)
	versions, err = svc.ListArticleVersions(ctx, updated.ID, 7)
	require.NoError(t, err)
	require.Empty(t, versions, "保存草稿不应产生版本")

	// 首次发布 → v1，快照内容即当前草稿。
	published, err := svc.PublishArticle(ctx, updated.ID, 7, 1, "")
	require.NoError(t, err)
	require.True(t, published.IsPublished)
	versions, err = svc.ListArticleVersions(ctx, published.ID, 7)
	require.NoError(t, err)
	require.Len(t, versions, 1)
	require.Equal(t, 1, versions[0].Version)
	require.Equal(t, "首次发布", versions[0].ChangeSummary)
	require.Equal(t, "标题 v2", versions[0].Title)
	require.Equal(t, "line-a\nline-c", versions[0].Content)
	require.Equal(t, 1, versions[0].AuthorID, "版本作者应为发布人")

	// 重复发布：内容与 v1 一致，只恢复发布态，不重复占用版本号。
	republished, err := svc.PublishArticle(ctx, published.ID, 7, 1, "")
	require.NoError(t, err)
	require.True(t, republished.IsPublished)
	versions, err = svc.ListArticleVersions(ctx, republished.ID, 7)
	require.NoError(t, err)
	require.Len(t, versions, 1, "重复发布不应产生新版本")

	// 编辑已发布文章：保存即回到草稿（线上内容与未发布修改不能共存）。
	republished.Title = "标题 v3"
	republished.Content = "line-a\nline-d"
	edited, err := svc.UpdateArticle(ctx, republished)
	require.NoError(t, err)
	require.False(t, edited.IsPublished, "编辑已发布文章必须回到草稿")
	versions, err = svc.ListArticleVersions(ctx, edited.ID, 7)
	require.NoError(t, err)
	require.Len(t, versions, 1, "编辑本身不是发布动作")

	// 再次发布 → v2，发布说明由调用方提供。
	released, err := svc.PublishArticle(ctx, edited.ID, 7, 2, "调整内容")
	require.NoError(t, err)
	require.True(t, released.IsPublished)
	versions, err = svc.ListArticleVersions(ctx, released.ID, 7)
	require.NoError(t, err)
	require.Len(t, versions, 2)
	require.Equal(t, 2, versions[0].Version)
	require.Equal(t, "调整内容", versions[0].ChangeSummary)
	require.Equal(t, "line-a\nline-d", versions[0].Content)
	require.Equal(t, 2, versions[0].AuthorID)

	// 下架不产生版本；原样重新上架同样不产生版本（幂等）。
	unpublished, err := svc.UnpublishArticle(ctx, released.ID, 7)
	require.NoError(t, err)
	require.False(t, unpublished.IsPublished)
	republished, err = svc.PublishArticle(ctx, unpublished.ID, 7, 1, "")
	require.NoError(t, err)
	require.True(t, republished.IsPublished)
	versions, err = svc.ListArticleVersions(ctx, republished.ID, 7)
	require.NoError(t, err)
	require.Len(t, versions, 2, "下架 / 原样上架不应产生版本")

	// 从 v1 恢复：回写 v1 内容并回到草稿；恢复是编辑，不是发布。
	restored, err := svc.RestoreArticleVersion(ctx, republished.ID, 1, 7)
	require.NoError(t, err)
	require.False(t, restored.IsPublished, "恢复历史版本后必须回到草稿")
	require.Equal(t, "标题 v2", restored.Title)
	require.Equal(t, "line-a\nline-c", restored.Content)
	versions, err = svc.ListArticleVersions(ctx, restored.ID, 7)
	require.NoError(t, err)
	require.Len(t, versions, 2, "恢复版本不是发布动作")

	// 恢复后重新发布 → v3（内容相对最近发布版本 v2 有差异）。
	restoredRelease, err := svc.PublishArticle(ctx, restored.ID, 7, 3, "回滚到 v1")
	require.NoError(t, err)
	require.True(t, restoredRelease.IsPublished)
	versions, err = svc.ListArticleVersions(ctx, restoredRelease.ID, 7)
	require.NoError(t, err)
	require.Len(t, versions, 3)
	require.Equal(t, 3, versions[0].Version)
	require.Equal(t, "line-a\nline-c", versions[0].Content)

	// 对比 v1 → v2：line-c 被 line-d 替换。
	cmp, err := svc.CompareArticleVersions(ctx, restoredRelease.ID, 1, 2, 7)
	require.NoError(t, err)
	require.Equal(t, 1, cmp.FromVersion)
	require.Equal(t, 2, cmp.ToVersion)
	require.Contains(t, cmp.Changes, dto.KnowledgeArticleVersionChange{Type: "removed", Content: "line-c"})
	require.Contains(t, cmp.Changes, dto.KnowledgeArticleVersionChange{Type: "added", Content: "line-d"})

	// 跨租户必须按未找到返回，不能泄露版本历史。
	_, err = svc.ListArticleVersions(ctx, restoredRelease.ID, 999)
	require.Error(t, err)
	require.True(t, ent.IsNotFound(err))
}
