package registry

import (
	"strings"
	"testing"
)

// TestCanonicalToolName_Basic 覆盖「唯一名」类：合法片段原样拼接、无哈希后缀。
func TestCanonicalToolName_Basic(t *testing.T) {
	if got, want := CanonicalToolName("github", "list_issues"), "mcp__github__list_issues"; got != want {
		t.Fatalf("canonical 投影 = %q，want %q", got, want)
	}
}

// TestCanonicalToolName_IllegalCharsAppendHash 覆盖「非法字符」类：
// 发生替换必须追加 FNV-1a 短哈希，且 `a.b` 与 `a_b` 投影后不得碰撞。
func TestCanonicalToolName_IllegalCharsAppendHash(t *testing.T) {
	dotted := CanonicalToolName("srv", "a.b")
	underscored := CanonicalToolName("srv", "a_b")

	if dotted == underscored {
		t.Fatalf("`a.b` 与 `a_b` 投影后碰撞：%q", dotted)
	}
	if !strings.HasPrefix(dotted, "mcp__srv__a_b_") {
		t.Fatalf("非法字符替换应追加哈希，实际 %q", dotted)
	}
	if underscored != "mcp__srv__a_b" {
		t.Fatalf("合法名不应追加哈希，实际 %q", underscored)
	}
	if dotted != CanonicalToolName("srv", "a.b") {
		t.Fatalf("投影必须确定性")
	}

	spaced := CanonicalToolName("srv", "my tool")
	if !strings.HasPrefix(spaced, "mcp__srv__my_tool_") {
		t.Fatalf("空格应替换为下划线并追加哈希，实际 %q", spaced)
	}
}

// TestCanonicalToolName_OverlongTruncatedWithIdentityHash 覆盖「超长」类：
// 结果严格不超过 64 字符，且以 identity hash 结尾、对输入唯一。
func TestCanonicalToolName_OverlongTruncatedWithIdentityHash(t *testing.T) {
	long := strings.Repeat("x", 120)
	got := CanonicalToolName("srv", long)

	if len(got) != MaxCanonicalToolNameLength {
		t.Fatalf("超长投影长度 = %d，want %d", len(got), MaxCanonicalToolNameLength)
	}
	suffix := "_" + shortIdentityHash("srv\x00"+long)
	if !strings.HasSuffix(got, suffix) {
		t.Fatalf("超长投影应以 identity hash 结尾，实际 %q", got)
	}
	if got != CanonicalToolName("srv", long) {
		t.Fatalf("超长投影必须确定性")
	}
	if other := CanonicalToolName("srv", long+"y"); other == got {
		t.Fatalf("不同超长输入不得投影为同一名字：%q", got)
	}
}

// TestCanonicalToolName_BoundaryLength 锁定 64 字符边界：恰好 64 保留原样，65 起截断 + hash。
func TestCanonicalToolName_BoundaryLength(t *testing.T) {
	prefix := "mcp__srv__" // 10 字符

	tool54 := strings.Repeat("a", 54)
	if got, want := CanonicalToolName("srv", tool54), prefix+tool54; got != want || len(got) != MaxCanonicalToolNameLength {
		t.Fatalf("恰好 64 字符应原样返回，实际 len=%d %q", len(got), got)
	}

	tool55 := strings.Repeat("a", 55)
	got := CanonicalToolName("srv", tool55)
	if len(got) != MaxCanonicalToolNameLength || got == prefix+tool55 {
		t.Fatalf("65 字符应截断 + identity hash，实际 len=%d %q", len(got), got)
	}
}

// TestCanonicalToolName_EmptyFallback 覆盖「空片段」兜底：投影仍确定且带 unnamed 前缀。
func TestCanonicalToolName_EmptyFallback(t *testing.T) {
	got := CanonicalToolName("srv", "   ")
	if !strings.HasPrefix(got, "mcp__srv__unnamed") {
		t.Fatalf("空工具名应回退 unnamed 片段，实际 %q", got)
	}
	if got != CanonicalToolName("srv", "   ") {
		t.Fatalf("空片段投影必须确定性")
	}
}
