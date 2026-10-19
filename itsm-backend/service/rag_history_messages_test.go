package service

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestChatMessagesWithHistory_StablePrefixLayout 锁定缓存友好的消息布局：
// 稳定 system → 稳定历史 → 当前输入（本轮动态内容只允许出现在末尾）。
func TestChatMessagesWithHistory_StablePrefixLayout(t *testing.T) {
	history := []LLMMessage{
		{Role: "user", Content: "u1"},
		{Role: "assistant", Content: "a1"},
	}

	messages := chatMessagesWithHistory("SYS", history, "u2+本轮检索上下文")

	require.Len(t, messages, 4)
	assert.Equal(t, LLMMessage{Role: "system", Content: "SYS"}, messages[0])
	assert.Equal(t, LLMMessage{Role: "user", Content: "u1"}, messages[1])
	assert.Equal(t, LLMMessage{Role: "assistant", Content: "a1"}, messages[2])
	assert.Equal(t, LLMMessage{Role: "user", Content: "u2+本轮检索上下文"}, messages[3],
		"易变内容只能出现在序列末尾")

	// 同一历史反复组装必须逐字节一致——provider 前缀缓存命中的前提。
	assert.Equal(t, messages, chatMessagesWithHistory("SYS", history, "u2+本轮检索上下文"))
}

// TestChatMessagesWithHistory_NoHistoryKeepsLegacyShape 无历史时保持修复前的
// [system, user] 形状，保证既有调用方零回归。
func TestChatMessagesWithHistory_NoHistoryKeepsLegacyShape(t *testing.T) {
	messages := chatMessagesWithHistory("SYS", nil, "q")
	require.Len(t, messages, 2)
	assert.Equal(t, "system", messages[0].Role)
	assert.Equal(t, "user", messages[1].Role)
	assert.Equal(t, "q", messages[1].Content)
}

// TestSortedToolFace_DeterministicOrder 工具面排序锁定：同一工具集合必须得到稳定的
// schema/系统提示词顺序，且不得原地修改调用方切片。
func TestSortedToolFace_DeterministicOrder(t *testing.T) {
	face := []ToolFaceEntry{
		{Tool: LLMTool{Name: "list_tickets"}},
		{Tool: LLMTool{Name: "get_ci_tickets"}},
		{Tool: LLMTool{Name: "list_cis"}},
	}

	sorted := sortedToolFace(face)

	names := make([]string, 0, len(sorted))
	for _, entry := range sorted {
		names = append(names, entry.Tool.Name)
	}
	assert.Equal(t, []string{"get_ci_tickets", "list_cis", "list_tickets"}, names)
	assert.Equal(t, "list_tickets", face[0].Tool.Name, "不得原地修改调用方切片")
	assert.Equal(t, sorted, sortedToolFace(sorted), "已排序输入再排序保持逐字节一致")
}
