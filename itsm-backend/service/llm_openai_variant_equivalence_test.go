package service

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"itsm-backend/internal/llm/protocol"
)

// 本文件锁定 PA-2 收敛后的口径（一协议一实现）：azure / ollama 变体不新增独立适配器，
// 由 openai_chat_completions 适配器以「endpoint / model 归一 + 构造期参数」承载。
//
//  1. azure：与旧 AzureProvider 同址同鉴权（`{endpoint}/openai/v1/chat/completions`、
//     `Authorization: Bearer`、model 缺省回退 deployment），对照测试锁定等价；
//  2. ollama：有意差异——旧 LocalProvider 走 Ollama 原生 `/api/chat`，适配器统一走
//     OpenAI 兼容 `/v1/chat/completions`（Ollama 官方支持），差异登记于独立计划 §4.3。

// TestAzureAdapterMatchesLegacyAzureProvider 是 PA-2 的等价性对照：
// 同一桩服务下，适配器路径与旧 AzureProvider 的请求路径 / 鉴权头 / 请求体字段 / 返回文本一致。
func TestAzureAdapterMatchesLegacyAzureProvider(t *testing.T) {
	ctx := context.Background()
	stub := newProtocolStub(t)
	messages := []LLMMessage{{Role: "user", Content: "hi"}}

	legacy := NewAzureProvider("sk-azure", stub.server.URL, "gpt4o-deploy")
	legacyText, err := legacy.Chat(ctx, "", messages)
	require.NoError(t, err)

	provider, err := NewProtocolProvider(ProtocolProviderOptions{
		Protocol: protocol.ProtocolOpenAIChatCompletions,
		Variant:  protocol.VariantAzure,
		APIKey:   "sk-azure",
		Endpoint: stub.server.URL,
		Model:    "gpt4o-deploy",
	})
	require.NoError(t, err)
	adapterText, err := provider.Chat(ctx, "", messages)
	require.NoError(t, err)

	assert.Equal(t, "你好，世界", adapterText)
	assert.Equal(t, legacyText, adapterText, "返回文本一致")

	requests := stub.snapshot()
	require.Len(t, requests, 2)
	legacyRequest, adapterRequest := requests[0], requests[1]

	assert.Equal(t, "/openai/v1/chat/completions", legacyRequest.Path, "旧 AzureProvider 固定 BaseURL = {endpoint}/openai/v1")
	assert.Equal(t, legacyRequest.Path, adapterRequest.Path, "endpoint 只给主机名时补 /openai，与旧分支同址")
	assert.Equal(t, legacyRequest.Authorization, adapterRequest.Authorization, "Bearer 鉴权头一致")
	assert.Equal(t, "Bearer sk-azure", adapterRequest.Authorization)
	for _, key := range []string{"model", "messages", "max_tokens", "temperature"} {
		assert.Equal(t, legacyRequest.Body[key], adapterRequest.Body[key], "请求体字段 %s 一致", key)
	}
}

// TestAzureVariantModelFallsBackToDeployment 锁定 azure 变体的 model 缺省口径
// （旧 AzureProvider `actualModel := deploymentID`）：显式 model 优先、缺省回退 deployment、
// 其余变体不受影响。
func TestAzureVariantModelFallsBackToDeployment(t *testing.T) {
	assert.Equal(t, "gpt4o-deploy", llmProviderModelForVariant(
		protocol.ProtocolOpenAIChatCompletions, protocol.VariantAzure, "", "gpt4o-deploy"))
	assert.Equal(t, "gpt-4o", llmProviderModelForVariant(
		protocol.ProtocolOpenAIChatCompletions, protocol.VariantAzure, "gpt-4o", "gpt4o-deploy"))
	assert.Equal(t, "llama3.1", llmProviderModelForVariant(
		protocol.ProtocolOpenAIChatCompletions, protocol.VariantOllama, "llama3.1", ""))
}

// TestNormalizeVariantEndpoint 锁定 endpoint 归一规则：azure 主机名补 `/openai`、
// 已带路径或为空时原样透传，其余协议/变体不动。
func TestNormalizeVariantEndpoint(t *testing.T) {
	assert.Equal(t, "https://res.openai.azure.com/openai", normalizeVariantEndpoint(
		"https://res.openai.azure.com", protocol.ProtocolOpenAIChatCompletions, protocol.VariantAzure))
	assert.Equal(t, "https://res.openai.azure.com/openai", normalizeVariantEndpoint(
		"https://res.openai.azure.com/", protocol.ProtocolOpenAIChatCompletions, protocol.VariantAzure))
	assert.Equal(t, "https://res.openai.azure.com/custom", normalizeVariantEndpoint(
		"https://res.openai.azure.com/custom", protocol.ProtocolOpenAIChatCompletions, protocol.VariantAzure),
		"已带路径的 endpoint 原样透传")
	assert.Empty(t, normalizeVariantEndpoint(
		"", protocol.ProtocolOpenAIChatCompletions, protocol.VariantAzure))
	assert.Equal(t, "http://localhost:11434", normalizeVariantEndpoint(
		"http://localhost:11434", protocol.ProtocolOpenAIChatCompletions, protocol.VariantOllama),
		"非 azure 变体原样透传")
}

// TestOllamaVariantUsesOpenAICompatibleEndpoint 登记 ollama 变体的有意差异：
// 统一走 OpenAI 兼容端点（非 Ollama 原生 `/api/chat`），无密钥实例不下发鉴权头。
func TestOllamaVariantUsesOpenAICompatibleEndpoint(t *testing.T) {
	ctx := context.Background()
	stub := newProtocolStub(t)

	provider, err := NewProtocolProvider(ProtocolProviderOptions{
		Protocol: protocol.ProtocolOpenAIChatCompletions,
		Variant:  protocol.VariantOllama,
		Endpoint: stub.server.URL + "/v1",
		Model:    "llama3.1",
	})
	require.NoError(t, err)
	content, err := provider.Chat(ctx, "", []LLMMessage{{Role: "user", Content: "hi"}})
	require.NoError(t, err)
	assert.Equal(t, "你好，世界", content)

	requests := stub.snapshot()
	require.Len(t, requests, 1)
	assert.Equal(t, "/v1/chat/completions", requests[0].Path, "统一走 OpenAI 兼容端点（非 Ollama 原生 /api/chat）")
	assert.Empty(t, requests[0].Authorization, "无密钥实例不下发 Authorization（旧 LocalProvider 同样无鉴权头）")
	assert.Equal(t, "llama3.1", requests[0].Body["model"])
}
