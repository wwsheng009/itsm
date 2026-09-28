package budget

import (
	"strings"
	"testing"
)

// TestEstimateTokens_口径 钉住估算口径：CJK 1 token/字、其余 4 字符/token。
func TestEstimateTokens_口径(t *testing.T) {
	cases := []struct {
		name string
		text string
		want int
	}{
		{"空串为 0", "", 0},
		{"短 ASCII 至少 1", "abc", 1},
		{"4 个 ASCII = 1", "abcd", 1},
		{"8 个 ASCII = 2", "abcdefgh", 2},
		{"5 个 ASCII 向上取整 = 2", "abcde", 2},
		{"4 个汉字 = 4", "查询工单", 4},
		{"中英混合（4 汉字 + 1 空格 + 8 ASCII = 4 + 3）", "查询工单 abcdefgh", 7},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := EstimateTokens(tc.text); got != tc.want {
				t.Fatalf("EstimateTokens(%q) = %d, want %d", tc.text, got, tc.want)
			}
		})
	}
}

// TestMeasure_汇总 校验工具面规模的汇总口径。
func TestMeasure_汇总(t *testing.T) {
	entries := []Entry{
		{Name: "mcp__a__list", Description: "查询工单列表", Schema: []byte(`{"type":"object"}`)},
		{Name: "mcp__a__get", Description: "获取工单详情", Schema: []byte(`{"type":"object","properties":{}}`)},
	}
	surface := Measure(entries)
	if surface.Tools != 2 {
		t.Fatalf("Tools = %d, want 2", surface.Tools)
	}
	wantBytes := 0
	wantTokens := 0
	for _, entry := range entries {
		wantBytes += len(entry.Name) + len(entry.Description) + len(entry.Schema)
		wantTokens += EstimateTokens(entry.Name) + EstimateTokens(entry.Description) + EstimateTokens(string(entry.Schema))
	}
	if surface.Bytes != wantBytes || surface.Tokens != wantTokens {
		t.Fatalf("surface = %+v, want bytes=%d tokens=%d", surface, wantBytes, wantTokens)
	}
	if Measure(nil).Tools != 0 || Measure(nil).Tokens != 0 {
		t.Fatalf("空工具面应为零值：%+v", Measure(nil))
	}
}

// TestEvaluate_工具数信号 覆盖信号一：工具数超预算。
func TestEvaluate_工具数信号(t *testing.T) {
	limits := Limits{Tools: 40}

	below := Evaluate(Surface{Tools: 40, Tokens: 100}, limits)
	if below.Exceeded {
		t.Fatalf("工具数 = 预算（40）不应判超限：%+v", below)
	}
	if len(below.Reasons) != 0 {
		t.Fatalf("未超限不应有理由：%+v", below.Reasons)
	}

	over := Evaluate(Surface{Tools: 41, Tokens: 100}, limits)
	if !over.Exceeded {
		t.Fatalf("工具数 41 > 40 应判超限：%+v", over)
	}
	if len(over.Reasons) != 1 || !strings.Contains(over.Reasons[0], "41") || !strings.Contains(over.Reasons[0], "40") {
		t.Fatalf("理由应含工具数与阈值：%+v", over.Reasons)
	}
	// 占比未超限时不应出现占比理由。
	for _, reason := range over.Reasons {
		if strings.Contains(reason, "tokens") {
			t.Fatalf("占比未超限不应产生 token 理由：%+v", over.Reasons)
		}
	}
}

// TestEvaluate_占比信号 覆盖信号二：工具面 token 占上下文比例超上限。
func TestEvaluate_占比信号(t *testing.T) {
	limits := Limits{Tools: 40, ContextTokens: 1000, Share: 0.30}

	atLimit := Evaluate(Surface{Tools: 1, Tokens: 300}, limits)
	if atLimit.Exceeded {
		t.Fatalf("占比 = 上限（30%%）不应判超限：%+v", atLimit)
	}
	if atLimit.Share != 0.3 {
		t.Fatalf("Share 应为 0.3，实际 %v", atLimit.Share)
	}

	over := Evaluate(Surface{Tools: 1, Tokens: 301}, limits)
	if !over.Exceeded {
		t.Fatalf("占比 30.1%% > 30%% 应判超限：%+v", over)
	}
	if len(over.Reasons) != 1 || !strings.Contains(over.Reasons[0], "301") || !strings.Contains(over.Reasons[0], "30.1%") {
		t.Fatalf("理由应含 token 数与百分比：%+v", over.Reasons)
	}

	// 两个信号同时命中：理由两条。
	both := Evaluate(Surface{Tools: 41, Tokens: 301}, limits)
	if !both.Exceeded || len(both.Reasons) != 2 {
		t.Fatalf("双信号命中应给出两条理由：%+v", both.Reasons)
	}
}

// TestEvaluate_默认值 校验 Limits 零值补默认（40 / 128000 / 30%）。
func TestEvaluate_默认值(t *testing.T) {
	report := Evaluate(Surface{Tools: 40, Tokens: 38400}, Limits{})
	if report.ToolLimit != DefaultToolLimit || report.ContextTokens != DefaultContextTokens || report.ShareLimit != DefaultTokenShare {
		t.Fatalf("默认阈值不符：%+v", report)
	}
	// 38400 / 128000 = 30% 恰好等于上限（不超限）。
	if report.Exceeded {
		t.Fatalf("恰好 30%% 不应判超限：%+v", report)
	}
	report = Evaluate(Surface{Tools: 40, Tokens: 38401}, Limits{})
	if !report.Exceeded {
		t.Fatalf("30.0008%% 应判超限：%+v", report)
	}
}

// TestEvaluate_负值钳制 覆盖负数阈值被默认值替换（防止把预算配成 0 导致误报）。
func TestEvaluate_负值钳制(t *testing.T) {
	report := Evaluate(Surface{Tools: 1}, Limits{Tools: -1, ContextTokens: -1, Share: -1})
	if report.ToolLimit != DefaultToolLimit || report.ContextTokens != DefaultContextTokens || report.ShareLimit != DefaultTokenShare {
		t.Fatalf("负值应被默认值替换：%+v", report)
	}
	if report.Exceeded {
		t.Fatalf("1 个工具不应超限：%+v", report)
	}
}

// TestAdvise_评估产物 覆盖 M2-03「元工具/检索式工具面方案」的评估口径：
// 预算内 → 维持直出；超限 → 建议评估元工具（灰度、默认关闭），并给出建议形态。
func TestAdvise_评估产物(t *testing.T) {
	ok := Advise(Evaluate(Surface{Tools: 10, Tokens: 100}, Limits{}))
	if ok.Action != ActionNone || ok.MetaTools != nil {
		t.Fatalf("预算内应为维持直出：%+v", ok)
	}
	if !strings.Contains(ok.Reason, "tools=10/40") {
		t.Fatalf("理由应含规模与阈值：%s", ok.Reason)
	}

	over := Advise(Evaluate(Surface{Tools: 41, Tokens: 100}, Limits{}))
	if over.Action != ActionMetaTools {
		t.Fatalf("超限应建议元工具：%+v", over)
	}
	if len(over.MetaTools) != 4 || over.MetaTools[0] != "mcp__meta__list" {
		t.Fatalf("应给出元工具最小集：%+v", over.MetaTools)
	}
	if !strings.Contains(over.Reason, "灰度开关默认关闭") {
		t.Fatalf("建议必须写明灰度约束：%s", over.Reason)
	}

	// 返回的建议形态是拷贝：调用方修改不影响包级常量。
	over.MetaTools[0] = "mutated"
	if MetaToolNames[0] != "mcp__meta__list" {
		t.Fatalf("MetaToolNames 不应被外部修改：%+v", MetaToolNames)
	}
}
