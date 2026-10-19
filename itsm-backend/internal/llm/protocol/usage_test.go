package protocol

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// B4-02：四协议适配器的真实 usage 解析（非流式回包 + 流式末帧/分帧），
// 无 usage 时保持 nil（接线层据此回落估算，零行为变化）。

func TestUsageParsing_OpenAIChat(t *testing.T) {
	adapter := NewOpenAIChatAdapter()

	nonStream := `{"choices":[{"index":0,"message":{"role":"assistant","content":"hi"},"finish_reason":"stop"}],` +
		`"usage":{"prompt_tokens":11,"completion_tokens":7}}`
	result, err := adapter.HandleResponse(false, strings.NewReader(nonStream), StreamCallbacks{})
	require.NoError(t, err)
	require.NotNil(t, result.Usage)
	assert.Equal(t, 11, result.Usage.PromptTokens)
	assert.Equal(t, 7, result.Usage.CompletionTokens)

	// 流式：usage 在无 choices 的末帧（OpenAI include_usage / DeepSeek 末帧同形）。
	stream := "data: {\"choices\":[{\"index\":0,\"delta\":{\"content\":\"hi\"}}]}\n\n" +
		"data: {\"choices\":[],\"usage\":{\"prompt_tokens\":3,\"completion_tokens\":2}}\n\n" +
		"data: [DONE]\n\n"
	result, err = adapter.HandleResponse(true, strings.NewReader(stream), StreamCallbacks{})
	require.NoError(t, err)
	require.NotNil(t, result.Usage)
	assert.Equal(t, 3, result.Usage.PromptTokens)
	assert.Equal(t, 2, result.Usage.CompletionTokens)
	assert.Equal(t, "hi", result.Content, "usage 帧不得干扰正文累积")

	// 无 usage：保持 nil（回落估算）。
	result, err = adapter.HandleResponse(false,
		strings.NewReader(`{"choices":[{"index":0,"message":{"role":"assistant","content":"hi"},"finish_reason":"stop"}]}`),
		StreamCallbacks{})
	require.NoError(t, err)
	assert.Nil(t, result.Usage)
}

func TestUsageParsing_Anthropic(t *testing.T) {
	adapter := NewAnthropicMessagesAdapter(VariantDefault)

	nonStream := `{"content":[{"type":"text","text":"hi"}],"stop_reason":"end_turn",` +
		`"usage":{"input_tokens":21,"output_tokens":9}}`
	result, err := adapter.HandleResponse(false, strings.NewReader(nonStream), StreamCallbacks{})
	require.NoError(t, err)
	require.NotNil(t, result.Usage)
	assert.Equal(t, 21, result.Usage.PromptTokens)
	assert.Equal(t, 9, result.Usage.CompletionTokens)

	// 流式：输入在 message_start、输出在 message_delta（分帧累加）。
	stream := "event: message_start\n" +
		"data: {\"type\":\"message_start\",\"message\":{\"usage\":{\"input_tokens\":5,\"output_tokens\":1}}}\n\n" +
		"event: message_delta\n" +
		"data: {\"type\":\"message_delta\",\"delta\":{\"stop_reason\":\"end_turn\"},\"usage\":{\"output_tokens\":4}}\n\n" +
		"event: message_stop\ndata: {\"type\":\"message_stop\"}\n\n"
	result, err = adapter.HandleResponse(true, strings.NewReader(stream), StreamCallbacks{})
	require.NoError(t, err)
	require.NotNil(t, result.Usage)
	assert.Equal(t, 5, result.Usage.PromptTokens)
	assert.Equal(t, 5, result.Usage.CompletionTokens, "输出分帧累加（1+4）")
}

func TestUsageParsing_Gemini(t *testing.T) {
	adapter := NewGoogleGeminiAdapter(VariantDefault)

	nonStream := `{"candidates":[{"content":{"parts":[{"text":"hi"}]},"finishReason":"STOP"}],` +
		`"usageMetadata":{"promptTokenCount":13,"candidatesTokenCount":6}}`
	result, err := adapter.HandleResponse(false, strings.NewReader(nonStream), StreamCallbacks{})
	require.NoError(t, err)
	require.NotNil(t, result.Usage)
	assert.Equal(t, 13, result.Usage.PromptTokens)
	assert.Equal(t, 6, result.Usage.CompletionTokens)

	// 流式：usageMetadata 出现在带候选的最后一帧。
	stream := "data: {\"candidates\":[{\"content\":{\"parts\":[{\"text\":\"hi\"}]}}]}\n\n" +
		"data: {\"candidates\":[{\"content\":{\"parts\":[{}]},\"finishReason\":\"STOP\"}],\"usageMetadata\":{\"promptTokenCount\":4,\"candidatesTokenCount\":2}}\n\n"
	result, err = adapter.HandleResponse(true, strings.NewReader(stream), StreamCallbacks{})
	require.NoError(t, err)
	require.NotNil(t, result.Usage)
	assert.Equal(t, 4, result.Usage.PromptTokens)
	assert.Equal(t, 2, result.Usage.CompletionTokens)
}

func TestUsageParsing_OpenAIResponses(t *testing.T) {
	adapter := NewOpenAIResponsesAdapter()

	nonStream := `{"output":[{"type":"message","content":[{"type":"output_text","text":"hi"}]}],` +
		`"usage":{"input_tokens":8,"output_tokens":3}}`
	result, err := adapter.HandleResponse(false, strings.NewReader(nonStream), StreamCallbacks{})
	require.NoError(t, err)
	require.NotNil(t, result.Usage)
	assert.Equal(t, 8, result.Usage.PromptTokens)
	assert.Equal(t, 3, result.Usage.CompletionTokens)

	// 流式：usage 位于 response.completed 的终态快照内。
	stream := "event: response.completed\n" +
		"data: {\"type\":\"response.completed\",\"response\":{\"output\":[{\"type\":\"message\",\"content\":[{\"type\":\"output_text\",\"text\":\"hi\"}]}],\"usage\":{\"input_tokens\":8,\"output_tokens\":3}}}\n\n"
	result, err = adapter.HandleResponse(true, strings.NewReader(stream), StreamCallbacks{})
	require.NoError(t, err)
	require.NotNil(t, result.Usage)
	assert.Equal(t, 8, result.Usage.PromptTokens)
	assert.Equal(t, 3, result.Usage.CompletionTokens)
	assert.Equal(t, "hi", result.Content)
}
