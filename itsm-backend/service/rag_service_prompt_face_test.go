package service

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

// 本组测试锁定 2026-09-30 修复：聊天系统提示词必须按「本次会话实际下发的工具面」生成，
// 不得再硬编码内置工具名。回归场景：会话选中只授权 MCP 工具的 Bot 时，模型此前会声称
// 自己具备 create_ticket/list_cis 等能力，并把缺失归因为"MCP 服务未挂载"。

// builtinToolNamesForPromptRegression 是修复前被硬编码进提示词的 7 个内置工具名。
var builtinToolNamesForPromptRegression = []string{
	"create_ticket",
	"create_ticket_type",
	"update_ticket",
	"list_tickets",
	"list_cis",
	"get_ci_tickets",
	"link_ticket_ci",
}

// TestBuildToolAwareSystemPrompt_MCPOnlyFaceOmitsBuiltinNames 复现事故现场：
// 工具面只有 MCP 工具（如 Bot 仅授权 mcp__mock__list_issues）时，提示词里
// 不得出现任何内置工具名，且必须明确"未挂载写工具"且禁止"MCP 服务未挂载"归因。
func TestBuildToolAwareSystemPrompt_MCPOnlyFaceOmitsBuiltinNames(t *testing.T) {
	face := []ToolFaceEntry{
		{
			Tool: LLMTool{
				Name:        "mcp__mock__list_issues",
				Description: "列出问题。支持按 state 过滤；返回 issues 数组",
			},
			ReadOnly: true,
			Provider: "mcp",
		},
	}

	prompt := buildToolAwareSystemPrompt(face)

	assert.Contains(t, prompt, "- mcp__mock__list_issues：列出问题", "可用工具清单必须列出本次真正下发的工具")
	for _, name := range builtinToolNamesForPromptRegression {
		assert.NotContainsf(t, prompt, name, "未下发的内置工具不得出现在提示词：%s", name)
	}
	assert.Contains(t, prompt, "未挂载写工具", "无写工具时必须给出明确口径")
	assert.Contains(t, prompt, "MCP 服务未挂载", "必须显式禁止把缺失归因为 MCP 未挂载")
	assert.NotContains(t, prompt, "CMDB 本体关联", "无 CMDB 工具时不得给出闭环操作指引")
}

// TestBuildToolAwareSystemPrompt_BuiltinFaceKeepsClosureGuidance 覆盖默认助手/场景 Bot 面：
// 内置读工具 + 遗留写白名单在场时，写审批口径与 CMDB 闭环指引（a）～（d）必须完整保留。
func TestBuildToolAwareSystemPrompt_BuiltinFaceKeepsClosureGuidance(t *testing.T) {
	face := []ToolFaceEntry{
		{Tool: LLMTool{Name: "list_tickets", Description: "查询工单列表。支持状态过滤"}, ReadOnly: true, Provider: "builtin"},
		{Tool: LLMTool{Name: "list_cis", Description: "查询配置项（CI）。支持 search 模糊匹配"}, ReadOnly: true, Provider: "builtin"},
		{Tool: LLMTool{Name: "get_ci_tickets", Description: "查询某个 CI 上已关联的工单。"}, ReadOnly: true, Provider: "builtin"},
		{Tool: LLMTool{Name: "create_ticket", Description: "创建工单（需审批）。可传入 ci_id 关联配置项"}, ReadOnly: false, Provider: "builtin"},
		{Tool: LLMTool{Name: "link_ticket_ci", Description: "把工单挂到 CI（需审批）。"}, ReadOnly: false, Provider: "builtin"},
	}

	prompt := buildToolAwareSystemPrompt(face)

	assert.Contains(t, prompt, "可先用 list_tickets 查询确认", "list_tickets 在场时保留口语纠正提示")
	assert.Contains(t, prompt, "写工具（create_ticket、link_ticket_ci）会进入人工审批流", "写工具须逐个点名并说明审批口径")
	assert.Contains(t, prompt, "(a) 用 list_cis 定位 CI")
	assert.Contains(t, prompt, "(b) 建单时带 ci_id")
	assert.Contains(t, prompt, "(c) 若用户先报障建单")
	assert.Contains(t, prompt, "(d) 影响面分析")
	assert.NotContains(t, prompt, "未挂载写工具")
}

// TestBuildToolAwareSystemPrompt_TrimsClosureGuidanceByTools 覆盖部分工具面：
// 只有 list_cis（无写工具）时，闭环指引只保留（a）与兜底句，不得出现建单/补挂动作。
func TestBuildToolAwareSystemPrompt_TrimsClosureGuidanceByTools(t *testing.T) {
	face := []ToolFaceEntry{
		{Tool: LLMTool{Name: "list_cis", Description: "查询配置项"}, ReadOnly: true, Provider: "builtin"},
	}

	prompt := buildToolAwareSystemPrompt(face)

	assert.Contains(t, prompt, "(a) 用 list_cis 定位 CI")
	assert.Contains(t, prompt, "(b) 若 list_cis 未找到匹配 CI", "缺少建单/补挂工具时，兜底句按顺序顺延为 (b)")
	assert.NotContains(t, prompt, "建单时带 ci_id")
	assert.NotContains(t, prompt, "补挂到 CI")
	assert.NotContains(t, prompt, "create_ticket")
	assert.NotContains(t, prompt, "link_ticket_ci")
	assert.Contains(t, prompt, "未挂载写工具")
}

// TestBuildToolAwareSystemPrompt_EmptyAndBlankFace 覆盖边界：空 face 与空名字项不产生工具清单行。
func TestBuildToolAwareSystemPrompt_EmptyAndBlankFace(t *testing.T) {
	prompt := buildToolAwareSystemPrompt(nil)
	assert.Contains(t, prompt, "本次会话可调用的工具（仅以下清单，未列出的工具一律不可调用）：")
	assert.Contains(t, prompt, "未挂载写工具")
	for _, name := range builtinToolNamesForPromptRegression {
		assert.NotContainsf(t, prompt, name, "空工具面不得出现工具名：%s", name)
	}

	blank := buildToolAwareSystemPrompt([]ToolFaceEntry{{Tool: LLMTool{Name: "   "}, ReadOnly: true}})
	assert.NotContains(t, blank, "   - ")
}

// TestSummarizeToolDescription 锁定描述压缩规则：折叠空白 → 取首句 → 截断加省略号。
func TestSummarizeToolDescription(t *testing.T) {
	assert.Equal(t, "查询工单", summarizeToolDescription("查询工单。支持状态过滤"))
	assert.Equal(t, "多行 描述 带空白", summarizeToolDescription("多行\n描述  带空白；后续说明"))
	assert.Equal(t, "", summarizeToolDescription("  \n\t "))

	long := strings.Repeat("超", toolPromptDescriptionMaxRunes+20)
	out := summarizeToolDescription(long)
	assert.Equal(t, toolPromptDescriptionMaxRunes+1, len([]rune(out)), "截断后长度 = 上限 + 省略号")
	assert.True(t, strings.HasSuffix(out, "…"))
}
