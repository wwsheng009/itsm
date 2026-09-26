package knowledgecontent

import (
	"strings"
	"testing"
)

func TestNormalize(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string
	}{
		{"empty", "", ""},
		{"blank", "   ", ""},
		{"text", "text", TypeText},
		{"trim + case", " Text ", TypeText},
		{"markdown", "markdown", TypeMarkdown},
		{"md alias", "MD", TypeMarkdown},
		{"html", "html", TypeHTML},
		{"rich_text", "rich_text", TypeRichText},
		{"rich-text alias", "rich-text", TypeRichText},
		{"richtext alias", "RichText", TypeRichText},
		{"unknown", "pdf", ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := Normalize(tc.in); got != tc.want {
				t.Fatalf("Normalize(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}

func TestParse(t *testing.T) {
	// 空值合法：表示由服务端按内容形态自动判定（历史客户端兼容）。
	if got, err := Parse("  "); err != nil || got != "" {
		t.Fatalf("Parse(\"  \") = (%q, %v), want (\"\", nil)", got, err)
	}
	if got, err := Parse("md"); err != nil || got != TypeMarkdown {
		t.Fatalf("Parse(\"md\") = (%q, %v), want (%q, nil)", got, err, TypeMarkdown)
	}
	if _, err := Parse("pdf"); err == nil {
		t.Fatal("Parse(\"pdf\") 应返回错误，避免脏值静默落库")
	} else if !strings.Contains(err.Error(), "pdf") {
		t.Fatalf("错误信息应包含非法值原文，got: %v", err)
	}
}

func TestDetect(t *testing.T) {
	cases := []struct {
		name    string
		content string
		want    string
	}{
		{"empty", "", TypeText},
		{"blank", "  \n\t ", TypeText},
		{"plain text", "服务重启步骤：先停 A，再停 B", TypeMarkdown},
		{"markdown", "## 标题\n\n- 列表\n\n| a | b |\n| - | - |", TypeMarkdown},
		{"inline html stays markdown", "第一行<br>第二行", TypeMarkdown},
		{"html paragraph", "<p>hello</p>", TypeRichText},
		{"html table", "<table><tbody><tr><td>x</td></tr></tbody></table>", TypeRichText},
		{"html heading with attr", `<h2 class="x">标题</h2>`, TypeRichText},
		{"html image", `<img src="/a.png" data-attachment-id="1">`, TypeRichText},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := Detect(tc.content); got != tc.want {
				t.Fatalf("Detect(%q) = %q, want %q", tc.content, got, tc.want)
			}
		})
	}
}

func TestResolve(t *testing.T) {
	// 显式值优先：即便内容形态与声明不符，也以用户声明为准。
	if got := Resolve(TypeMarkdown, "<p>看起来像 HTML</p>"); got != TypeMarkdown {
		t.Fatalf("显式 markdown 应优先，got %q", got)
	}
	if got := Resolve("rich_text", "## 看起来像 Markdown"); got != TypeRichText {
		t.Fatalf("显式 rich_text 应优先，got %q", got)
	}
	// 空值 / 非法值 → 按内容形态兜底（历史数据路径）。
	if got := Resolve("", "<p>hello</p>"); got != TypeRichText {
		t.Fatalf("空类型 + HTML 内容应兜底为 %q，got %q", TypeRichText, got)
	}
	if got := Resolve("legacy-unknown", "## 标题"); got != TypeMarkdown {
		t.Fatalf("非法类型 + Markdown 内容应兜底为 %q，got %q", TypeMarkdown, got)
	}
	if got := Resolve("", ""); got != TypeText {
		t.Fatalf("空类型 + 空内容应为 %q，got %q", TypeText, got)
	}
}
