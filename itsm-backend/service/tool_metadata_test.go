package service

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// builtinToolCount 内置工具总数。新增/删除工具时必须同步更新本文件与风险矩阵
// （方案 B0-01 要点 3：守卫测试保证全量标注，防止元数据漂移）。
//
// 2026-09-30：14 → 17——B3-06 新增 3 个 plan 工具（draft_ticket_fields /
// analyze_ci_impact_plan / draft_kb_article）时未同步本计数，守卫测试自此恒失败；
// 本次一并补齐（这 3 个工具在注册表已按 plan 完整标注）。
const builtinToolCount = 17

// wantBuiltinRisk 是 B0-01 冻结的风险矩阵（对齐阶段一报告 §5.5）：
// 读工具 read/plan；写工具按影响面 act_low → act_medium → act_high。
var wantBuiltinRisk = map[string]string{
	// 读 / 计划（11）
	"get_incident_stats":   ToolRiskRead,
	"list_kb":              ToolRiskRead,
	"list_tickets":         ToolRiskRead,
	"list_cis":             ToolRiskRead,
	"get_ci_tickets":       ToolRiskRead,
	"get_ci":               ToolRiskRead,
	"get_ci_relationships": ToolRiskRead,
	"get_ci_impact":        ToolRiskPlan,
	// B3-06 plan 工具（3）：只读草案生成，产物落 bot_artifacts
	"draft_ticket_fields":    ToolRiskPlan,
	"analyze_ci_impact_plan": ToolRiskPlan,
	"draft_kb_article":       ToolRiskPlan,
	// 写（6）
	"link_ticket_ci":         ToolRiskActLow,
	"create_ticket":          ToolRiskActLow,
	"update_ticket":          ToolRiskActMedium,
	"create_ticket_type":     ToolRiskActHigh,
	"create_ci_relationship": ToolRiskActHigh,
	"delete_ci_relationship": ToolRiskActHigh,
}

// TestBuiltinToolMetadataComplete 覆盖 AB0-01：17/17 内置工具含
// risk/category/dry-run/幂等/超时/输出上限/脱敏档标注；缺标注即失败。
func TestBuiltinToolMetadataComplete(t *testing.T) {
	reg := NewToolRegistry(nil, nil, nil, nil)
	tools := reg.ListTools()
	require.Len(t, tools, builtinToolCount,
		"内置工具数量与标注矩阵不一致：新增/删除工具必须同步更新元数据与 wantBuiltinRisk")

	validRisk := map[string]bool{
		ToolRiskRead: true, ToolRiskPlan: true,
		ToolRiskActLow: true, ToolRiskActMedium: true, ToolRiskActHigh: true,
	}
	validRedaction := map[string]bool{ToolRedactionDefault: true, ToolRedactionStrict: true}

	for _, td := range tools {
		td := td
		t.Run(td.Name, func(t *testing.T) {
			require.True(t, validRisk[td.Risk], "risk 必须是冻结枚举之一，实际 %q", td.Risk)
			require.NotEmpty(t, td.Category, "category 必填")
			require.NotEqual(t, ToolCategoryUnclassified, td.Category, "category 不得为兜底值")
			require.True(t, validRedaction[td.RedactionProfile], "redactionProfile 必须是 default|strict，实际 %q", td.RedactionProfile)
			require.Positive(t, td.TimeoutMs, "timeoutMs 必须为正")
			require.Positive(t, td.MaxOutputBytes, "maxOutputBytes 必须为正")
			require.Equal(t, !td.ReadOnly, td.Idempotent, "写工具必须要求幂等键，读工具不得要求")
			require.Equal(t, wantBuiltinRisk[td.Name], td.Risk, "risk 与冻结矩阵不一致")
		})
	}

	// 反向断言：矩阵中的每个工具都必须在注册表中存在（防漏注册）。
	got := make(map[string]bool, len(tools))
	for _, td := range tools {
		got[td.Name] = true
	}
	for name := range wantBuiltinRisk {
		require.True(t, got[name], "风险矩阵中的工具 %s 未在注册表注册", name)
	}
}

// TestNormalizeToolMetadataConservative 覆盖 B0-01 要点 3：
// 缺标注 → 最保守兜底（act_high + strict）且 annotated=false（工具面不默认下发）。
func TestNormalizeToolMetadataConservative(t *testing.T) {
	skeleton := ToolDefinition{Name: "unannotated", ReadOnly: false}
	annotated := NormalizeToolMetadata(&skeleton)

	require.False(t, annotated, "缺标注工具必须报告 annotated=false")
	require.Equal(t, ToolRiskActHigh, skeleton.Risk)
	require.Equal(t, ToolCategoryUnclassified, skeleton.Category)
	require.Equal(t, ToolRedactionStrict, skeleton.RedactionProfile)
	require.Equal(t, DefaultToolTimeoutMs, skeleton.TimeoutMs)
	require.Equal(t, DefaultToolMaxOutputBytes, skeleton.MaxOutputBytes)
	require.True(t, skeleton.Idempotent, "写工具兜底要求幂等键")
}

// TestNormalizeToolMetadataKeepsComplete 验证完整标注不被改写（annotated=true）。
func TestNormalizeToolMetadataKeepsComplete(t *testing.T) {
	td := ToolDefinition{
		Name:             "list_kb",
		ReadOnly:         true,
		Risk:             ToolRiskRead,
		Category:         "knowledge",
		TimeoutMs:        DefaultToolTimeoutMs,
		MaxOutputBytes:   DefaultToolMaxOutputBytes,
		RedactionProfile: ToolRedactionDefault,
	}
	require.True(t, NormalizeToolMetadata(&td))
	require.Equal(t, ToolRiskRead, td.Risk)
	require.Equal(t, "knowledge", td.Category)
	require.False(t, td.Idempotent, "读工具不要求幂等键")
}

// TestGetToolReturnsNormalizedMetadata 验证 GetTool 对外返回的定义带完整元数据
// （B0-01 要点 2：调用时快照的数据来源）。
func TestGetToolReturnsNormalizedMetadata(t *testing.T) {
	reg := NewToolRegistry(nil, nil, nil, nil)
	td := reg.GetTool("create_ticket")
	require.NotNil(t, td)
	require.Equal(t, ToolRiskActLow, td.Risk)
	require.Equal(t, "ticket", td.Category)
	require.True(t, td.SupportsDryRun)
	require.True(t, td.Idempotent)
	require.Equal(t, DefaultToolTimeoutMs, td.TimeoutMs)
}
