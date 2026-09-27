package registry

import (
	"errors"
	"reflect"
	"testing"
)

func newTestRegistry(t *testing.T) *Registry {
	t.Helper()

	r := New()
	result := r.Register([]ToolRef{
		{Server: "github", RawName: "list_issues"},
		{Server: "gitlab", RawName: "list_issues"},
		{Server: "github", RawName: "get_issue"},
	})
	if len(result.Added) != 3 || len(result.Quarantined) != 0 {
		t.Fatalf("测试注册失败: added=%d quarantined=%d", len(result.Added), len(result.Quarantined))
	}
	return r
}

// 解析第 1 类：canonical 精确匹配优先。
func TestResolve_CanonicalExactWins(t *testing.T) {
	r := newTestRegistry(t)

	got, err := r.Resolve("mcp__github__get_issue")
	if err != nil {
		t.Fatalf("Resolve 失败: %v", err)
	}
	if got.Server != "github" || got.RawName != "get_issue" {
		t.Fatalf("解析结果错误: %+v", got)
	}
	if got.Key() != "github\x00get_issue" {
		t.Fatalf("执行归一化键错误: %q", got.Key())
	}
}

// 解析第 2 类：原始短名仅唯一命中时可用。
func TestResolve_UniqueRawName(t *testing.T) {
	r := newTestRegistry(t)

	got, err := r.Resolve("get_issue")
	if err != nil {
		t.Fatalf("Resolve 失败: %v", err)
	}
	if got.CallableName != "mcp__github__get_issue" {
		t.Fatalf("短名唯一命中结果错误: %+v", got)
	}
}

// 解析第 3 类：多候选 fail-closed，返回排序候选。
func TestResolve_AmbiguousRawName(t *testing.T) {
	r := newTestRegistry(t)

	_, err := r.Resolve("list_issues")
	var ambiguous *AmbiguousToolError
	if !errors.As(err, &ambiguous) {
		t.Fatalf("应返回 *AmbiguousToolError，实际 %v", err)
	}
	want := []string{"mcp__github__list_issues", "mcp__gitlab__list_issues"}
	if !reflect.DeepEqual(ambiguous.Candidates, want) {
		t.Fatalf("候选列表 = %v，want %v", ambiguous.Candidates, want)
	}
	if ambiguous.Name != "list_issues" {
		t.Fatalf("歧义名称错误: %q", ambiguous.Name)
	}
}

// 解析第 4 类：未找到 / 空名。
func TestResolve_NotFoundAndEmpty(t *testing.T) {
	r := newTestRegistry(t)

	if _, err := r.Resolve("no_such_tool"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("未找到应返回 ErrNotFound，实际 %v", err)
	}
	if _, err := r.Resolve("   "); !errors.Is(err, ErrEmptyName) {
		t.Fatalf("空名应返回 ErrEmptyName，实际 %v", err)
	}
}

// 遮蔽场景：某工具的原始短名恰为另一工具的 canonical 名 → canonical 精确匹配优先。
func TestResolve_ShadowingCanonicalWins(t *testing.T) {
	r := New()
	result := r.Register([]ToolRef{
		{Server: "github", RawName: "alpha"},
		{Server: "gitlab", RawName: "mcp__github__alpha"},
	})
	if len(result.Added) != 2 {
		t.Fatalf("测试注册失败: %+v", result)
	}

	got, err := r.Resolve("mcp__github__alpha")
	if err != nil {
		t.Fatalf("Resolve 失败: %v", err)
	}
	if got.Server != "github" || got.RawName != "alpha" {
		t.Fatalf("canonical 应优先于同名短名，实际 %+v", got)
	}
}
