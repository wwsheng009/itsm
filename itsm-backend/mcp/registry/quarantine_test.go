package registry

import (
	"errors"
	"strings"
	"testing"
)

// collisionToolNames 构造两个投影后 canonical 完全相同的工具名：
// `a.b` 因非法字符替换得到 `a_b_<hash(a.b)>`；另一名字直接取该结果（全为合法字符，不再追加哈希）。
func collisionToolNames() (first, second string) {
	first = "a.b"
	second = "a_b_" + shortNameHash(first)
	return first, second
}

// 隔离第 1 类：canonical 完全碰撞 → 后注册者隔离，先到者保留。
func TestQuarantine_CanonicalCollision(t *testing.T) {
	first, second := collisionToolNames()
	canonical := CanonicalToolName("srv", first)

	r := New()
	firstResult := r.Register([]ToolRef{{Server: "srv", RawName: first}})
	if len(firstResult.Added) != 1 {
		t.Fatalf("先注册者应成功: %+v", firstResult)
	}

	secondResult := r.Register([]ToolRef{{Server: "srv", RawName: second}})
	if len(secondResult.Added) != 0 || len(secondResult.Quarantined) != 1 {
		t.Fatalf("后注册者应被隔离: %+v", secondResult)
	}
	quarantined := secondResult.Quarantined[0]
	if quarantined.Reason != ReasonCanonicalCollision {
		t.Fatalf("隔离原因错误: %+v", quarantined)
	}
	if quarantined.CallableName != canonical {
		t.Fatalf("隔离记录 canonical 错误: %+v", quarantined)
	}
	if !strings.Contains(quarantined.Detail, "srv/"+first) {
		t.Fatalf("隔离说明应含碰撞对象: %+v", quarantined)
	}

	// 隔离工具不暴露、不可执行。
	if tools := r.List(); len(tools) != 1 || tools[0].RawName != first {
		t.Fatalf("隔离工具不得出现在可暴露列表: %+v", tools)
	}
	if _, err := r.Resolve(second); !errors.Is(err, ErrNotFound) {
		t.Fatalf("隔离工具不得可解析，实际 %v", err)
	}
	if got, err := r.Resolve(canonical); err != nil || got.RawName != first {
		t.Fatalf("先到者应保持可解析: %+v err=%v", got, err)
	}

	// 诊断列表可见。
	items := r.ListQuarantined()
	if len(items) != 1 || items[0].RawName != second {
		t.Fatalf("隔离诊断列表错误: %+v", items)
	}
}

// 隔离第 2 类：解除路径 —— Unregister 释放 canonical 后重新注册成功，隔离记录清除。
func TestQuarantine_ReleaseAfterOwnerRemoved(t *testing.T) {
	first, second := collisionToolNames()
	canonical := CanonicalToolName("srv", first)

	r := New()
	r.Register([]ToolRef{{Server: "srv", RawName: first}})
	if result := r.Register([]ToolRef{{Server: "srv", RawName: second}}); len(result.Quarantined) != 1 {
		t.Fatalf("前置条件失败: %+v", result)
	}

	r.Unregister("srv", first)

	result := r.Register([]ToolRef{{Server: "srv", RawName: second}})
	if len(result.Added) != 1 {
		t.Fatalf("canonical 释放后重新注册应成功: %+v", result)
	}
	if len(r.ListQuarantined()) != 0 {
		t.Fatalf("重新注册成功后隔离记录应清除")
	}
	got, err := r.Resolve(canonical)
	if err != nil || got.RawName != second {
		t.Fatalf("解除后应可解析到新主: %+v err=%v", got, err)
	}
}

// 隔离第 3 类：非法服务器标识与空工具名直接隔离。
func TestQuarantine_InvalidServerAndEmptyTool(t *testing.T) {
	r := New()
	result := r.Register([]ToolRef{
		{Server: "Bad.Server", RawName: "t"},
		{Server: "srv", RawName: "   "},
		{Server: strings.Repeat("a", 33), RawName: "t"},
	})

	if len(result.Added) != 0 || len(result.Quarantined) != 3 {
		t.Fatalf("非法输入应全部隔离: %+v", result)
	}
	reasons := []QuarantineReason{
		result.Quarantined[0].Reason,
		result.Quarantined[1].Reason,
		result.Quarantined[2].Reason,
	}
	want := []QuarantineReason{ReasonInvalidServerName, ReasonEmptyToolName, ReasonInvalidServerName}
	for i := range want {
		if reasons[i] != want[i] {
			t.Fatalf("第 %d 条隔离原因 = %q，want %q", i, reasons[i], want[i])
		}
	}
	if len(r.List()) != 0 {
		t.Fatalf("全部隔离时不应有可暴露工具")
	}
}

// 幂等：同一 (server, raw_name) 重复注册 → Duplicates，不隔离、不重复。
func TestRegister_DuplicateIdempotent(t *testing.T) {
	r := New()
	first := r.Register([]ToolRef{{Server: "srv", RawName: "tool"}})
	if len(first.Added) != 1 {
		t.Fatalf("首次注册失败: %+v", first)
	}

	second := r.Register([]ToolRef{{Server: "srv", RawName: "tool"}})
	if len(second.Added) != 0 || len(second.Duplicates) != 1 || len(second.Quarantined) != 0 {
		t.Fatalf("重复注册应为幂等忽略: %+v", second)
	}
	if len(r.List()) != 1 {
		t.Fatalf("重复注册不得产生第二条记录")
	}
}
