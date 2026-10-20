package service

import (
	"context"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap/zaptest"
)

type duplicateReadToolProvider struct {
	rounds int
}

func (p *duplicateReadToolProvider) Chat(context.Context, string, []LLMMessage) (string, error) {
	return "", nil
}

func (p *duplicateReadToolProvider) ChatStreamWithTools(
	_ context.Context,
	_ string,
	_ []LLMMessage,
	_ []LLMTool,
	callback func(string),
	onToolCalls func([]LLMToolCall),
) error {
	p.rounds++
	if p.rounds == 1 {
		onToolCalls([]LLMToolCall{
			{ID: "call-1", Name: "help_search", Arguments: `{"limit":5,"q":"VPN 开通"}`},
			{ID: "call-2", Name: "help_search", Arguments: `{"q":"VPN 开通","limit":5}`},
		})
		return nil
	}
	if callback != nil {
		callback("已根据检索结果整理回答。")
	}
	onToolCalls(nil)
	return nil
}

func TestRAG_ToolLoopReusesIdenticalReadOnlyResult(t *testing.T) {
	client := setupRAGTestClient(t)
	defer client.Close()

	ctx := context.Background()
	tenant, err := createTestTenant(ctx, client)
	require.NoError(t, err)

	rag := NewRAGService(client, nil, nil, zaptest.NewLogger(t).Sugar(), RAGConfig{UseKeyword: true})
	provider := &duplicateReadToolProvider{}
	gateway := NewLLMGateway(provider, nil, nil, "test")
	face := []ToolFaceEntry{{
		Tool:     LLMTool{Name: "help_search", Description: "检索帮助文档"},
		ReadOnly: true,
		Provider: "builtin",
	}}

	executions := 0
	execTool := func(name string, args map[string]any) (any, error) {
		executions++
		return map[string]any{"tool": name, "count": 1}, nil
	}
	var answer strings.Builder
	err = rag.AskWithLLMStreamWithToolsAndHistory(
		ctx,
		tenant.ID,
		"VPN 开通",
		nil,
		gateway,
		5,
		face,
		nil,
		func(delta string) { answer.WriteString(delta) },
		execTool,
	)
	require.NoError(t, err)
	assert.Equal(t, 1, executions, "相同参数的只读工具调用只执行一次")
	assert.Equal(t, 2, provider.rounds)
	assert.Contains(t, answer.String(), "已根据检索结果整理回答")
}
