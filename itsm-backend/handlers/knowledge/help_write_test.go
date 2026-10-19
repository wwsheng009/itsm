package knowledge

import (
	"context"
	"testing"

	"itsm-backend/ent/enttest"

	_ "github.com/mattn/go-sqlite3"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap/zaptest"
)

// T5-09a：帮助写面只落「用户指南」草稿，发布复用 KB 版本链路；
// 非用户指南文章不得经该桥发布。
func TestHelpDraft_PublishBoundary(t *testing.T) {
	ctx := context.Background()
	client := enttest.Open(t, "sqlite3", "file:knowledge_help_write?mode=memory&cache=shared&_fk=1")
	defer client.Close()
	svc := NewService(NewEntRepository(client), zaptest.NewLogger(t).Sugar())

	articleID, err := svc.CreateHelpDraft(ctx, 7, 42, "工单详情帮助", "## 新内容")
	require.NoError(t, err)
	article, err := svc.GetArticle(ctx, articleID, 7)
	require.NoError(t, err)
	require.Equal(t, "用户指南", article.Category)
	require.False(t, article.IsPublished, "提交审阅只落草稿")

	require.NoError(t, svc.PublishHelpDraft(ctx, 7, 42, articleID, "T5-09a"))
	versions, err := svc.ListArticleVersions(ctx, articleID, 7)
	require.NoError(t, err)
	require.Len(t, versions, 1)

	other, err := svc.CreateArticle(ctx, &Article{
		Title: "普通文章", Content: "x", Category: "操作指南", AuthorID: 42, TenantID: 7,
	})
	require.NoError(t, err)
	err = svc.PublishHelpDraft(ctx, 7, 42, other.ID, "不应通过")
	require.ErrorContains(t, err, "用户指南")
}
