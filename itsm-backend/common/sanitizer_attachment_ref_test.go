package common

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// TestSanitizeHTML_KeepsAttachmentRefAttrs 覆盖 BE-7 的「净化对齐」：
// 知识库正文（SanitizeHTML / UGCPolicy）必须保留 data-attachment-id 与 data-align，
// 否则富文本图片的附件回链会在落库时被静默抹掉，删除引用保护与宿主归属校验双双失效。
func TestSanitizeHTML_KeepsAttachmentRefAttrs(t *testing.T) {
	in := `<p>图</p><img src="/api/v1/attachments/7/content" alt="图" data-attachment-id="7" data-align="center">`
	out := SanitizeHTML(in)

	require.Contains(t, out, `src="/api/v1/attachments/7/content"`)
	require.Contains(t, out, `data-attachment-id="7"`)
	require.Contains(t, out, `data-align="center"`)

	// 非法取值按属性剥离，图片本体保留（与 internal/sanitize 的取值约束一致）。
	out = SanitizeHTML(`<img src="/api/v1/attachments/7/content" data-attachment-id="abc" data-align="evil">`)
	require.NotContains(t, strings.ToLower(out), "data-attachment-id")
	require.NotContains(t, strings.ToLower(out), "data-align")
	require.Contains(t, out, `src="/api/v1/attachments/7/content"`)

	// 放行 data-* 不得削弱既有 XSS 防护。
	out = SanitizeHTML(`<img src="/api/v1/attachments/7/content" onerror="alert(1)" data-attachment-id="7"><script>alert(2)</script>`)
	require.NotContains(t, strings.ToLower(out), "onerror")
	require.NotContains(t, strings.ToLower(out), "script")
	require.NotContains(t, out, "alert(")
	require.Contains(t, out, `data-attachment-id="7"`)
}
