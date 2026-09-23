package sanitize

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestSanitizeRichTextHTML_StripsScriptAndEventHandlers(t *testing.T) {
	input := `<p onclick="alert(1)" onmouseover="alert(2)">正文</p><script>alert('xss')</script>`
	out := SanitizeRichTextHTML(input)

	require.Contains(t, out, "<p>正文</p>")
	require.NotContains(t, strings.ToLower(out), "script")
	require.NotContains(t, strings.ToLower(out), "onclick")
	require.NotContains(t, strings.ToLower(out), "onmouseover")
	require.NotContains(t, out, "alert(")
}

func TestSanitizeRichTextHTML_StripsJavascriptLinks(t *testing.T) {
	cases := []string{
		`<a href="javascript:alert(1)">点我</a>`,
		`<a href="JaVaScRiPt:alert(1)">点我</a>`,
		`<a href="vbscript:msgbox(1)">点我</a>`,
		`<a href="data:text/html;base64,PHNjcmlwdD4=">点我</a>`,
	}
	for _, input := range cases {
		out := SanitizeRichTextHTML(input)
		require.NotContains(t, strings.ToLower(out), "javascript:", "input=%s", input)
		require.NotContains(t, strings.ToLower(out), "vbscript:", "input=%s", input)
		require.NotContains(t, strings.ToLower(out), "data:text/html", "input=%s", input)
		require.Contains(t, out, "点我", "文本内容应保留：%s", input)
		require.NotContains(t, out, "href=", "危险 URL 不应残留 href：%s", input)
	}

	// 协议相对 // 与站内相对 href 同样不在允许范围（href 仅 http/https/mailto）。
	require.NotContains(t, SanitizeRichTextHTML(`<a href="//evil.com/x">x</a>`), "href=")
	require.NotContains(t, SanitizeRichTextHTML(`<a href="/tickets/1">x</a>`), "href=")
}

func TestSanitizeRichTextHTML_RemovesDisallowedImages(t *testing.T) {
	// data: 图片整元素移除，不落库内联大图。
	out := SanitizeRichTextHTML(`<p>图：</p><img src="data:image/png;base64,iVBORw0KGgo=" alt="inline">`)
	require.Contains(t, out, "<p>图：</p>")
	require.NotContains(t, strings.ToLower(out), "<img")
	require.NotContains(t, strings.ToLower(out), "data:image")

	// 协议相对与反斜杠变体（浏览器会归一化为 //）同样移除。
	for _, input := range []string{
		`<img src="//evil.com/track.png">`,
		`<img src="/\evil.com/track.png">`,
		`<img src="ftp://evil.com/a.png">`,
		`<img alt="no-src">`,
	} {
		require.NotContains(t, strings.ToLower(SanitizeRichTextHTML(input)), "<img", "input=%s", input)
	}

	// on* 属性被剥离但合法图片保留。
	out = SanitizeRichTextHTML(`<img src="/api/v1/attachments/9/content" onerror="alert(1)" alt="附件">`)
	require.Contains(t, out, `src="/api/v1/attachments/9/content"`)
	require.Contains(t, out, `alt="附件"`)
	require.NotContains(t, strings.ToLower(out), "onerror")
}

func TestSanitizeRichTextHTML_KeepsWhitelistedTags(t *testing.T) {
	input := strings.Join([]string{
		`<h1>标题</h1><h2>小标题</h2><h3>更小</h3>`,
		`<p><strong>粗</strong><b>粗2</b><em>斜</em><i>斜2</i><u>下划线</u><s>删除线</s></p>`,
		`<ul><li>一</li></ul><ol><li>二</li></ol>`,
		`<blockquote>引用</blockquote><pre><code>code()</code></pre><hr>`,
		`<table><thead><tr><th>列</th></tr></thead><tbody><tr><td>值</td></tr></tbody></table>`,
		`<p><a href="https://example.com/doc" rel="opener" target="_self">外链</a></p>`,
		`<p><a href="mailto:help@example.com">邮件</a></p>`,
		`<p><img src="https://cdn.example.com/a.png" alt="远端" title="标题" width="10" height="20" data-attachment-id="7" data-align="center"></p>`,
		`<p><img src="/api/v1/attachments/1/content"></p>`,
	}, "")

	out := SanitizeRichTextHTML(input)

	for _, tag := range []string{
		"<h1>", "<h2>", "<h3>", "<strong>", "<b>", "<em>", "<i>", "<u>", "<s>",
		"<ul>", "<ol>", "<li>", "<blockquote>", "<pre>", "<code>", "<hr>",
		"<table>", "<thead>", "<tbody>", "<tr>", "<th>", "<td>",
	} {
		require.Contains(t, out, tag)
	}
	require.Contains(t, out, `href="https://example.com/doc"`)
	require.Contains(t, out, `href="mailto:help@example.com"`)
	require.Contains(t, out, `src="https://cdn.example.com/a.png"`)
	require.Contains(t, out, `src="/api/v1/attachments/1/content"`)
	require.Contains(t, out, `data-attachment-id="7"`)
	require.Contains(t, out, `width="10"`)
	require.Contains(t, out, `data-align="center"`)
	// rel/target 由服务端策略强制改写，不信任客户端提交值。
	require.Contains(t, out, "noreferrer")
	require.NotContains(t, out, `target="_self"`)
	require.NotContains(t, out, `rel="opener"`)
}

func TestSanitizeRichTextHTML_DropsInvalidImageAlign(t *testing.T) {
	// data-align 仅允许 left/center/right，其余取值剥离但图片保留。
	for _, invalid := range []string{"evil", "javascript:alert(1)", `"><script>`} {
		out := SanitizeRichTextHTML(
			`<img src="/api/v1/attachments/1/content" alt="图" data-align="` + invalid + `">`,
		)
		require.Contains(t, out, `src="/api/v1/attachments/1/content"`, "invalid=%s", invalid)
		require.NotContains(t, strings.ToLower(out), "data-align", "invalid=%s", invalid)
		require.NotContains(t, strings.ToLower(out), "<script", "invalid=%s", invalid)
	}
}

func TestSanitizeRichTextHTML_StripsStyleIframeAndForm(t *testing.T) {
	input := `<style>body{background:url(javascript:alert(1))}</style><iframe src="https://evil.com"></iframe><form action="/x"><input name="a"></form><p style="color:red">文本</p>`
	out := SanitizeRichTextHTML(input)

	lower := strings.ToLower(out)
	require.NotContains(t, lower, "<style")
	require.NotContains(t, lower, "<iframe")
	require.NotContains(t, lower, "<form")
	require.NotContains(t, lower, "<input")
	require.NotContains(t, lower, "style=")
	require.Contains(t, out, "<p>文本</p>")
}

func TestSanitizeRichTextHTML_EmptyResults(t *testing.T) {
	for _, input := range []string{
		"",
		"   \n\t ",
		"<script>alert(1)</script>",
		"<p></p>",
		"<p><br></p>",
		"<hr>",
		"<img src=\"data:image/png;base64,AAAA\">",
		`<img src="//evil.com/a.png">`,
	} {
		require.Equal(t, "", SanitizeRichTextHTML(input), "input=%q", input)
	}
}

func TestSanitizeRichTextHTML_ImageOnlyContentIsNotEmpty(t *testing.T) {
	out := SanitizeRichTextHTML(`<img src="/uploads/tickets/1/a.png" alt="图">`)
	require.NotEqual(t, "", out)
	require.Contains(t, out, `src="/uploads/tickets/1/a.png"`)
}
