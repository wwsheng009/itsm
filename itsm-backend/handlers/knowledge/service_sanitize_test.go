package knowledge

import (
	"context"
	"strings"
	"testing"

	"itsm-backend/ent/enttest"

	_ "github.com/mattn/go-sqlite3"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap/zaptest"
)

// TestCreateArticle_MarkdownContentNotEntityEncoded 锁定「Markdown 正文原样落库」。
//
// 背景（2026-10-17 修复）：此前 CreateArticle 对任何 content_type 都跑
// common.SanitizeHTML，bluemonday 把非标签文本里的直引号/&/<> 转成 HTML 实体，
// 而渲染端在代码块内不解码实体 —— 示例代码 `d = {"a": 1}` 会字面显示成
// `d = {&#34;a&#34;: 1}`，复制即错。这里逐字符比对，防止回归。
func TestCreateArticle_MarkdownContentNotEntityEncoded(t *testing.T) {
	ctx := context.Background()
	client := enttest.Open(t, "sqlite3", "file:kb_sanitize_md?mode=memory&cache=shared&_fk=1")
	defer client.Close()
	svc := NewService(NewEntRepository(client), zaptest.NewLogger(t).Sugar())

	content := "示例：\n\n```python\nd = {\"skills\": [1]}\nprint('ok')\n```\n\n" +
		"条件：a & b，阈值 < 10 且 > 0，直引号 \"双\" 与 '单'。\n\n" +
		"```html\n<div class=\"x\">hi</div>\n```\n"

	created, err := svc.CreateArticle(ctx, &Article{
		Title:       "Markdown 正文保真",
		Content:     content,
		ContentType: "markdown",
		Category:    "操作指南",
		AuthorID:    1,
		TenantID:    7,
	})
	require.NoError(t, err)

	require.Equal(t, content, created.Content, "Markdown 正文必须逐字节原样落库")
	for _, bad := range []string{"&#34;", "&#39;", "&amp;", "&lt;", "&gt;"} {
		require.NotContains(t, created.Content, bad, "不应出现 HTML 实体 %s", bad)
	}

	// 类型判定也不该被代码块里的 HTML 片段带偏。
	require.Equal(t, "markdown", created.ContentType,
		"代码块内含 <div> 不应把整篇 Markdown 判成 rich_text")

	// 回读同一份内容，确认存储层没有二次改写。
	got, err := svc.GetArticle(ctx, created.ID, 7)
	require.NoError(t, err)
	require.Equal(t, content, got.Content)
}

// TestCreateArticle_RichTextStillSanitized 确认分流没有削弱 HTML 正文的 XSS 防护。
func TestCreateArticle_RichTextStillSanitized(t *testing.T) {
	ctx := context.Background()
	client := enttest.Open(t, "sqlite3", "file:kb_sanitize_html?mode=memory&cache=shared&_fk=1")
	defer client.Close()
	svc := NewService(NewEntRepository(client), zaptest.NewLogger(t).Sugar())

	created, err := svc.CreateArticle(ctx, &Article{
		Title:       "富文本消毒",
		Content:     `<p>正文</p><script>alert(1)</script><a href="javascript:alert(2)">x</a>`,
		ContentType: "html",
		Category:    "操作指南",
		AuthorID:    1,
		TenantID:    7,
	})
	require.NoError(t, err)

	require.NotContains(t, strings.ToLower(created.Content), "<script", "script 标签必须被剥离")
	require.NotContains(t, strings.ToLower(created.Content), "javascript:", "javascript: URL 必须被剥离")
	require.Contains(t, created.Content, "<p>正文</p>", "正常标签应保留")
}

// TestUpdateArticle_MarkdownContentNotEntityEncoded 编辑路径同口径。
func TestUpdateArticle_MarkdownContentNotEntityEncoded(t *testing.T) {
	ctx := context.Background()
	client := enttest.Open(t, "sqlite3", "file:kb_sanitize_upd?mode=memory&cache=shared&_fk=1")
	defer client.Close()
	svc := NewService(NewEntRepository(client), zaptest.NewLogger(t).Sugar())

	created, err := svc.CreateArticle(ctx, &Article{
		Title: "初稿", Content: "占位", ContentType: "markdown",
		Category: "操作指南", AuthorID: 1, TenantID: 7,
	})
	require.NoError(t, err)

	next := "curl -fsSL http://h/api/v1/skills-dist/manifest -o manifest.json\n\n" +
		"```bash\nls -d $ROOT/itsm-* | wc -l\n```\n"
	created.Content = next
	updated, err := svc.UpdateArticle(ctx, created)
	require.NoError(t, err)
	require.Equal(t, next, updated.Content)
	require.NotContains(t, updated.Content, "&#34;")
}
