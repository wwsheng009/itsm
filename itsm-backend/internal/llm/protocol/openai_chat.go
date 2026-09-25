package protocol

import (
	"encoding/json"
	"fmt"
	"io"
	"strings"
)

// OpenAIChatAdapter 承载 openai_chat_completions 形态（BE-9 首适配器）：
// OpenAI Chat Completions 及兼容网关（DeepSeek / Qwen / vLLM / Ollama /v1 / Azure 等）。
//
// 等价基线：service/llm_providers.go 的 OpenAIProvider（go-openai 客户端）。
// 本适配器直接处理线上格式（不引入 go-openai 依赖），请求体字段（含 omitempty 语义）、
// 流式回调顺序与工具调用累积口径与既有实现保持一致；等价性由同一桩服务下的
// 对照测试逐项锁定（见 equivalence_test.go）。
type OpenAIChatAdapter struct{}

// NewOpenAIChatAdapter 创建首适配器实例（无状态，可并发复用）。
func NewOpenAIChatAdapter() *OpenAIChatAdapter { return &OpenAIChatAdapter{} }

// Name 返回协议枚举值。
func (a *OpenAIChatAdapter) Name() string { return ProtocolOpenAIChatCompletions }

// GetAPIPath 返回默认 API 路径（与参考实现同值）。
func (a *OpenAIChatAdapter) GetAPIPath() string { return "/v1/chat/completions" }

// IsReasoningModel 判断推理模型（参考实现口径）：codex 与 gpt-5/o1-o5 前缀。
func (a *OpenAIChatAdapter) IsReasoningModel(model string) bool {
	return isOpenAIReasoningModel(model)
}

// isOpenAIReasoningModel 是 openai_chat_completions 与 openai_responses 共用的推理模型口径
// （参考实现：codex 与 gpt-5/o1-o5 前缀，容忍 models/ 前缀）。
func isOpenAIReasoningModel(model string) bool {
	modelID := strings.ToLower(strings.TrimSpace(model))
	modelID = strings.TrimPrefix(modelID, "models/")
	if strings.Contains(modelID, "codex") {
		return true
	}
	for _, prefix := range []string{"gpt-5", "o1", "o3", "o4", "o5"} {
		if strings.HasPrefix(modelID, prefix) {
			return true
		}
	}
	return false
}

// BuildHeaders 构建请求头：Bearer 鉴权 + 调用方附加 headers（大小写不敏感覆盖）。
func (a *OpenAIChatAdapter) BuildHeaders(cfg AdapterConfig) map[string]string {
	headers := map[string]string{"Content-Type": "application/json"}
	if apiKey := strings.TrimSpace(cfg.APIKey); apiKey != "" {
		headers["Authorization"] = "Bearer " + apiKey
	}
	for key, value := range cfg.Headers {
		setHeaderCaseInsensitive(headers, key, value)
	}
	return headers
}

// BuildRequest 构建 Chat Completions 请求体。
//
// 字段对齐既有 go-openai 实现：stream/max_tokens/temperature/tools/tool_choice
// 均为按需下发（省略等价默认值），messages 为 OpenAI 线上格式。
func (a *OpenAIChatAdapter) BuildRequest(cfg RequestConfig) map[string]any {
	body := map[string]any{
		"model":    cfg.Model,
		"messages": buildOpenAIMessages(cfg.Messages),
	}
	if cfg.Stream {
		body["stream"] = true
	}
	if cfg.MaxTokens > 0 {
		body["max_tokens"] = cfg.MaxTokens
	}
	if !cfg.ReasoningModel {
		body["temperature"] = cfg.Temperature
	}
	if tools := collectOpenAITools(cfg.Messages, cfg.Tools); len(tools) > 0 {
		body["tools"] = tools
	}
	if cfg.ToolChoice != nil {
		body["tool_choice"] = cfg.ToolChoice
	}
	return body
}

// HandleResponse 解析响应：非流式解析 JSON；流式按 SSE 逐帧回调并累积工具调用。
func (a *OpenAIChatAdapter) HandleResponse(isStream bool, respBody io.Reader, callbacks StreamCallbacks) (ProcessResult, error) {
	if respBody == nil {
		return ProcessResult{}, fmt.Errorf("%s: response body is required", ProtocolOpenAIChatCompletions)
	}
	if isStream {
		return a.handleStream(respBody, callbacks)
	}
	return a.handleCompletion(respBody)
}

// ProcessResponse 从非流式响应体提取归一化结果（含工具调用与推理链）。
func (a *OpenAIChatAdapter) ProcessResponse(result map[string]any) ProcessResult {
	var out ProcessResult
	choice := firstOpenAIChoice(result)
	if choice == nil {
		return out
	}
	out.FinishReason = stringValue(choice["finish_reason"])
	message, _ := choice["message"].(map[string]any)
	if message == nil {
		return out
	}
	out.Content = stringValue(message["content"])
	out.Reasoning = reasoningText(message)
	out.ToolCalls = parseOpenAIToolCalls(message["tool_calls"])
	return out
}

func (a *OpenAIChatAdapter) handleCompletion(respBody io.Reader) (ProcessResult, error) {
	var result map[string]any
	if err := json.NewDecoder(respBody).Decode(&result); err != nil {
		return ProcessResult{}, fmt.Errorf("%s: decode response: %w", ProtocolOpenAIChatCompletions, err)
	}
	if payloadErr := errorFromPayload(ProtocolOpenAIChatCompletions, result); payloadErr != nil {
		return ProcessResult{}, payloadErr
	}
	return a.ProcessResponse(result), nil
}

type streamToolCallAccumulator struct {
	id   string
	name string
	args strings.Builder
}

func (a *OpenAIChatAdapter) handleStream(respBody io.Reader, callbacks StreamCallbacks) (ProcessResult, error) {
	var (
		content      strings.Builder
		reasoning    strings.Builder
		finishReason string
	)
	calls := map[int]*streamToolCallAccumulator{}
	var order []int

	scanErr := scanSSEFrames(respBody, func(frame SSEFrame) (bool, error) {
		data := strings.TrimSpace(frame.Data)
		if data == "" {
			return true, nil
		}
		if data == "[DONE]" {
			return false, nil
		}
		var chunk map[string]any
		if err := json.Unmarshal([]byte(data), &chunk); err != nil {
			return false, fmt.Errorf("%s: malformed stream chunk: %w", ProtocolOpenAIChatCompletions, err)
		}
		if payloadErr := errorFromPayload(ProtocolOpenAIChatCompletions, chunk); payloadErr != nil {
			return false, payloadErr
		}
		choice := firstOpenAIChoice(chunk)
		if choice == nil {
			return true, nil
		}
		if finish := stringValue(choice["finish_reason"]); finish != "" {
			finishReason = finish
		}
		delta, _ := choice["delta"].(map[string]any)
		if delta == nil {
			return true, nil
		}
		if text := stringValue(delta["content"]); text != "" {
			content.WriteString(text)
			callbacks.EmitText(text)
		}
		if reason := reasoningText(delta); reason != "" {
			reasoning.WriteString(reason)
			callbacks.EmitReasoning(reason)
		}
		accumulateOpenAIToolCalls(delta["tool_calls"], calls, &order)
		return true, nil
	})
	if scanErr != nil {
		return ProcessResult{}, scanErr
	}
	return ProcessResult{
		Content:      content.String(),
		Reasoning:    reasoning.String(),
		ToolCalls:    toolCallsFromAccumulator(calls, order),
		FinishReason: finishReason,
	}, nil
}

// buildOpenAIMessages 组装 OpenAI 消息数组（字段存在性与 go-openai omitempty 对齐）。
func buildOpenAIMessages(messages []Message) []map[string]any {
	out := make([]map[string]any, 0, len(messages))
	for _, message := range messages {
		item := map[string]any{"role": message.Role}
		if message.Content != "" {
			item["content"] = message.Content
		}
		if len(message.ToolCalls) > 0 {
			item["tool_calls"] = toolCallsToWire(message.ToolCalls)
		}
		if message.ToolCallID != "" {
			item["tool_call_id"] = message.ToolCallID
		}
		out = append(out, item)
	}
	return out
}

func toolCallsToWire(calls []ToolCall) []map[string]any {
	out := make([]map[string]any, 0, len(calls))
	for _, call := range calls {
		out = append(out, map[string]any{
			"id":   call.ID,
			"type": "function",
			"function": map[string]any{
				"name":      call.Name,
				"arguments": call.Arguments,
			},
		})
	}
	return out
}

// collectOpenAITools 合并消息携带的工具声明与调用侧显式声明（顺序：消息在前、显式在后，
// 与既有 toOpenAIMessages + ChatStreamWithTools 的合并口径一致）。
func collectOpenAITools(messages []Message, explicit []Tool) []map[string]any {
	var out []map[string]any
	appendTool := func(tool Tool) {
		function := map[string]any{"name": tool.Name}
		if tool.Description != "" {
			function["description"] = tool.Description
		}
		if tool.Parameters != nil {
			function["parameters"] = tool.Parameters
		}
		out = append(out, map[string]any{"type": "function", "function": function})
	}
	for _, message := range messages {
		for _, tool := range message.Tools {
			appendTool(tool)
		}
	}
	for _, tool := range explicit {
		appendTool(tool)
	}
	return out
}

func firstOpenAIChoice(payload map[string]any) map[string]any {
	choices, _ := payload["choices"].([]any)
	if len(choices) == 0 {
		return nil
	}
	choice, _ := choices[0].(map[string]any)
	return choice
}

// reasoningText 提取推理链文本：DeepSeek 风格 reasoning_content 优先，兼容 reasoning。
func reasoningText(payload map[string]any) string {
	if text := stringValue(payload["reasoning_content"]); text != "" {
		return text
	}
	return stringValue(payload["reasoning"])
}

func stringValue(value any) string {
	text, _ := value.(string)
	return text
}

func parseOpenAIToolCalls(raw any) []ToolCall {
	items, _ := raw.([]any)
	if len(items) == 0 {
		return nil
	}
	calls := make([]ToolCall, 0, len(items))
	for _, item := range items {
		entry, _ := item.(map[string]any)
		if entry == nil {
			continue
		}
		function, _ := entry["function"].(map[string]any)
		if function == nil {
			continue
		}
		calls = append(calls, ToolCall{
			ID:        stringValue(entry["id"]),
			Name:      stringValue(function["name"]),
			Arguments: stringValue(function["arguments"]),
		})
	}
	return calls
}

// accumulateOpenAIToolCalls 按 index 累积流式工具调用分片（与既有 OpenAIProvider
// ChatStreamWithTools 的 map[int]*acc + order 口径一致：index 缺失按 0 处理）。
func accumulateOpenAIToolCalls(raw any, calls map[int]*streamToolCallAccumulator, order *[]int) {
	items, _ := raw.([]any)
	for _, item := range items {
		entry, _ := item.(map[string]any)
		if entry == nil {
			continue
		}
		index := 0
		if value, ok := entry["index"].(float64); ok {
			index = int(value)
		}
		acc := calls[index]
		if acc == nil {
			acc = &streamToolCallAccumulator{}
			calls[index] = acc
			*order = append(*order, index)
		}
		if id := stringValue(entry["id"]); id != "" {
			acc.id = id
		}
		if function, _ := entry["function"].(map[string]any); function != nil {
			if name := stringValue(function["name"]); name != "" {
				acc.name = name
			}
			if args := stringValue(function["arguments"]); args != "" {
				acc.args.WriteString(args)
			}
		}
	}
}

func toolCallsFromAccumulator(calls map[int]*streamToolCallAccumulator, order []int) []ToolCall {
	if len(order) == 0 {
		return nil
	}
	out := make([]ToolCall, 0, len(order))
	for _, index := range order {
		acc := calls[index]
		if acc == nil {
			continue
		}
		out = append(out, ToolCall{ID: acc.id, Name: acc.name, Arguments: acc.args.String()})
	}
	return out
}

func setHeaderCaseInsensitive(headers map[string]string, key, value string) {
	for existing := range headers {
		if strings.EqualFold(existing, key) {
			headers[existing] = value
			return
		}
	}
	headers[key] = value
}
