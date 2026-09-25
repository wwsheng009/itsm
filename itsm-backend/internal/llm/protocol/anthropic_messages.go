package protocol

import (
	"encoding/json"
	"fmt"
	"io"
	"strings"
)

// AnthropicMessagesAdapter 承载 anthropic_messages 形态（Anthropic Messages API 及兼容端点）。
//
// 变体语义（主计划 §3.1.1；本适配器按 (protocol, variant) 注册，实例自带变体口径）：
//   - 默认变体（""）：官方 Anthropic 线上格式（snake_case：max_tokens / stop_reason）；
//   - minimax 变体：MiniMax Anthropic 兼容端点的 camelCase 口径（maxTokens / stopReason），
//     与既有 service/llm_providers.go 的 MiniMaxProvider 请求/响应字段逐一对应。
//
// 等价基线：service.MiniMaxProvider（minimax 变体）。同一桩服务下的对照测试见
// service/llm_anthropic_equivalence_test.go（请求路径、鉴权头、请求体字段与返回文本逐项比对）。
//
// 与旧 MiniMaxProvider 的有意差异（均已在方案 v1.0 §4 差异清单登记）：
//  1. 支持流式与工具调用（旧分支只有非流式 Chat；流式同路径 SSE，属能力增益）；
//  2. 非流式正文：拼接全部 text 块（旧分支取首个 text 块）；无 text 块时返回空串而非报错
//     （tool_use-only 响应是合法形态）；
//  3. 客户端超时由调用方 ctx 控制（旧分支 120s 硬超时），与 BE-9 openai 路径同一口径。
type AnthropicMessagesAdapter struct {
	variant string
}

// NewAnthropicMessagesAdapter 创建适配器实例（无状态，可并发复用）。
func NewAnthropicMessagesAdapter(variant string) *AnthropicMessagesAdapter {
	return &AnthropicMessagesAdapter{variant: normalizeAnthropicVariant(variant)}
}

// Name 返回协议枚举值。
func (a *AnthropicMessagesAdapter) Name() string { return ProtocolAnthropicMessages }

// GetAPIPath 返回默认 API 路径：官方与 MiniMax 兼容端点同为 /v1/messages
// （endpoint 已带 /v1 版本段时由 service 拼接规则只补资源段）。
func (a *AnthropicMessagesAdapter) GetAPIPath() string { return "/v1/messages" }

// IsReasoningModel 判断模型是否为显式推理模型（模型名含 thinking / reasoning 标记）。
//
// 口径说明：普通 claude / minimax 模型不抑制 temperature（旧 MiniMaxProvider 恒下发
// temperature=1.0，适配路径必须保持该线上行为）；真正的扩展思考参数（thinking.budget_tokens）
// 属能力位收敛范围（方案 v1.0 PA-4），本适配器只做响应侧 thinking 块解析。
func (a *AnthropicMessagesAdapter) IsReasoningModel(model string) bool {
	modelID := normalizeAnthropicVariant(model)
	return strings.Contains(modelID, "thinking") || strings.Contains(modelID, "reasoning")
}

// BuildHeaders 构建请求头：x-api-key + anthropic-version（官方与 MiniMax 兼容端点同口径，
// 对齐旧 MiniMaxProvider 的 Header.Set 集合）+ 调用方附加 headers（大小写不敏感覆盖）。
func (a *AnthropicMessagesAdapter) BuildHeaders(cfg AdapterConfig) map[string]string {
	headers := map[string]string{
		"Content-Type":      "application/json",
		"anthropic-version": anthropicVersion,
	}
	if apiKey := strings.TrimSpace(cfg.APIKey); apiKey != "" {
		headers["x-api-key"] = apiKey
	}
	for key, value := range cfg.Headers {
		setHeaderCaseInsensitive(headers, key, value)
	}
	return headers
}

// BuildRequest 构建 Messages 请求体。
//
// 字段口径：
//   - 鉴权/版本走 headers；system 从 system 消息提取（多条取最后一条非空，与旧 MiniMaxProvider 一致）；
//   - max_tokens 必填（Anthropic 语义）：调用方未给时回填 anthropicDefaultMaxTokens；
//     minimax 变体按兼容端点口径写作 maxTokens；
//   - stream 仅在流式请求时下发（旧分支无流式，非流式请求体逐字段一致）；
//   - tools 使用 Anthropic 形态 {name, description, input_schema}，消息携带与显式声明合并
//     （顺序：消息在前、显式在后，与 openai 适配器同序）。
func (a *AnthropicMessagesAdapter) BuildRequest(cfg RequestConfig) map[string]any {
	body := map[string]any{
		"model":    cfg.Model,
		"messages": buildAnthropicMessages(cfg.Messages),
	}
	maxTokens := cfg.MaxTokens
	if maxTokens <= 0 {
		maxTokens = anthropicDefaultMaxTokens
	}
	body[a.maxTokensField()] = maxTokens
	if system := collectAnthropicSystem(cfg.Messages); system != "" {
		body["system"] = system
	}
	if cfg.Stream {
		body["stream"] = true
	}
	if !cfg.ReasoningModel {
		body["temperature"] = cfg.Temperature
	}
	if tools := collectAnthropicTools(cfg.Messages, cfg.Tools); len(tools) > 0 {
		body["tools"] = tools
	}
	if cfg.ToolChoice != nil {
		if choice := anthropicToolChoice(cfg.ToolChoice); choice != nil {
			body["tool_choice"] = choice
		}
	}
	return body
}

// HandleResponse 解析响应：非流式解析 JSON 消息；流式按 SSE 事件逐帧回调并累积工具调用。
func (a *AnthropicMessagesAdapter) HandleResponse(isStream bool, respBody io.Reader, callbacks StreamCallbacks) (ProcessResult, error) {
	if respBody == nil {
		return ProcessResult{}, fmt.Errorf("%s: response body is required", ProtocolAnthropicMessages)
	}
	if isStream {
		return a.handleStream(respBody, callbacks)
	}
	return a.handleMessage(respBody)
}

// ProcessResponse 从非流式响应体提取归一化结果（text / thinking / tool_use 三类内容块）。
func (a *AnthropicMessagesAdapter) ProcessResponse(result map[string]any) ProcessResult {
	var out ProcessResult
	out.FinishReason = a.stopReason(result)
	blocks, _ := result["content"].([]any)
	for _, raw := range blocks {
		block, _ := raw.(map[string]any)
		if block == nil {
			continue
		}
		switch stringValue(block["type"]) {
		case "text":
			out.Content += stringValue(block["text"])
		case "thinking":
			out.Reasoning += stringValue(block["thinking"])
		case "tool_use":
			out.ToolCalls = append(out.ToolCalls, ToolCall{
				ID:        stringValue(block["id"]),
				Name:      stringValue(block["name"]),
				Arguments: encodeToolInput(block["input"]),
			})
		}
	}
	return out
}

func (a *AnthropicMessagesAdapter) handleMessage(respBody io.Reader) (ProcessResult, error) {
	var result map[string]any
	if err := json.NewDecoder(respBody).Decode(&result); err != nil {
		return ProcessResult{}, fmt.Errorf("%s: decode response: %w", ProtocolAnthropicMessages, err)
	}
	if payloadErr := errorFromPayload(ProtocolAnthropicMessages, result); payloadErr != nil {
		return ProcessResult{}, payloadErr
	}
	return a.ProcessResponse(result), nil
}

// handleStream 解析 Anthropic 事件流（官方与 MiniMax 兼容端点同事件名）：
// message_start / content_block_start / content_block_delta / content_block_stop /
// message_delta / message_stop / ping / error。
func (a *AnthropicMessagesAdapter) handleStream(respBody io.Reader, callbacks StreamCallbacks) (ProcessResult, error) {
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
		var event map[string]any
		if err := json.Unmarshal([]byte(data), &event); err != nil {
			return false, fmt.Errorf("%s: malformed stream event: %w", ProtocolAnthropicMessages, err)
		}
		if payloadErr := errorFromPayload(ProtocolAnthropicMessages, event); payloadErr != nil {
			return false, payloadErr
		}

		eventType := stringValue(event["type"])
		if eventType == "" {
			eventType = frame.Event
		}
		index := intValue(event["index"])

		switch eventType {
		case "content_block_start":
			block, _ := event["content_block"].(map[string]any)
			if block == nil || stringValue(block["type"]) != "tool_use" {
				return true, nil
			}
			accumulator := ensureToolCallAccumulator(calls, &order, index)
			accumulator.id = stringValue(block["id"])
			accumulator.name = stringValue(block["name"])
			if input := encodeToolInput(block["input"]); input != "" && input != "{}" {
				accumulator.args.WriteString(input)
			}
		case "content_block_delta":
			delta, _ := event["delta"].(map[string]any)
			if delta == nil {
				return true, nil
			}
			switch stringValue(delta["type"]) {
			case "text_delta":
				if text := stringValue(delta["text"]); text != "" {
					content.WriteString(text)
					callbacks.EmitText(text)
				}
			case "thinking_delta":
				if thinking := stringValue(delta["thinking"]); thinking != "" {
					reasoning.WriteString(thinking)
					callbacks.EmitReasoning(thinking)
				}
			case "input_json_delta":
				if partial := stringValue(delta["partial_json"]); partial != "" {
					ensureToolCallAccumulator(calls, &order, index).args.WriteString(partial)
				}
			}
		case "message_delta":
			delta, _ := event["delta"].(map[string]any)
			if stop := a.stopReason(delta); stop != "" {
				finishReason = stop
			}
		case "error":
			// 带内错误事件已在 errorFromPayload 前置识别；此处兜底空错误体。
			return false, &ProtocolError{Protocol: ProtocolAnthropicMessages, Message: "upstream stream error"}
		}
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

// stopReason 读取结束原因：官方 snake_case 优先，兼容 minimax 变体 camelCase。
func (a *AnthropicMessagesAdapter) stopReason(payload map[string]any) string {
	if payload == nil {
		return ""
	}
	if reason := stringValue(payload["stop_reason"]); reason != "" {
		return reason
	}
	return stringValue(payload["stopReason"])
}

func (a *AnthropicMessagesAdapter) maxTokensField() string {
	if a.variant == VariantMiniMax {
		return anthropicMiniMaxMaxTokensField
	}
	return anthropicMaxTokensField
}

const (
	// anthropicVersion 官方要求的 API 版本头（与旧 MiniMaxProvider 同值）。
	anthropicVersion = "2023-06-01"
	// anthropicDefaultMaxTokens max_tokens 为 Anthropic 必填字段，调用方未给时回填。
	// 与旧 MiniMaxProvider 的线上取值一致（4096）。
	anthropicDefaultMaxTokens = 4096
	// anthropicMaxTokensField 官方字段名。
	anthropicMaxTokensField = "max_tokens"
	// anthropicMiniMaxMaxTokensField MiniMax Anthropic 兼容端点的字段名（对齐旧 MiniMaxProvider）。
	anthropicMiniMaxMaxTokensField = "maxTokens"
	// anthropicDefaultEndpoint 官方 Anthropic 默认地址（路径 /v1/messages 由 service 拼接）。
	anthropicDefaultEndpoint = "https://api.anthropic.com"
	// anthropicMiniMaxDefaultEndpoint 与旧 service.MiniMaxProvider.baseURL 同值。
	anthropicMiniMaxDefaultEndpoint = "https://api.minimaxi.com/anthropic/v1"
)

// 适配器接口实现（编译期断言）。
var _ ProtocolAdapter = (*AnthropicMessagesAdapter)(nil)

// buildAnthropicMessages 组装 messages 数组：system 消息由调用方提取到顶层 system 字段；
// assistant 工具调用转 tool_use 内容块；OpenAI 风格 tool 角色（工具结果）转 user + tool_result 块。
func buildAnthropicMessages(messages []Message) []map[string]any {
	out := make([]map[string]any, 0, len(messages))
	for _, message := range messages {
		if message.Role == "system" {
			continue
		}
		if message.Role == "tool" || message.ToolCallID != "" {
			out = append(out, map[string]any{
				"role": "user",
				"content": []map[string]any{{
					"type":        "tool_result",
					"tool_use_id": message.ToolCallID,
					"content":     message.Content,
				}},
			})
			continue
		}
		if len(message.ToolCalls) > 0 {
			blocks := make([]map[string]any, 0, len(message.ToolCalls)+1)
			if message.Content != "" {
				blocks = append(blocks, map[string]any{"type": "text", "text": message.Content})
			}
			for _, call := range message.ToolCalls {
				blocks = append(blocks, map[string]any{
					"type":  "tool_use",
					"id":    call.ID,
					"name":  call.Name,
					"input": decodeToolInput(call.Arguments),
				})
			}
			out = append(out, map[string]any{"role": message.Role, "content": blocks})
			continue
		}
		out = append(out, map[string]any{"role": message.Role, "content": message.Content})
	}
	return out
}

// collectAnthropicSystem 提取 system 提示词：多条 system 消息取最后一条非空
// （与旧 MiniMaxProvider 的 systemPrompt = m.Content 覆盖口径一致）。
func collectAnthropicSystem(messages []Message) string {
	system := ""
	for _, message := range messages {
		if message.Role == "system" && strings.TrimSpace(message.Content) != "" {
			system = message.Content
		}
	}
	return system
}

// collectAnthropicTools 合并工具声明为 Anthropic 形态（消息在前、显式在后）。
func collectAnthropicTools(messages []Message, explicit []Tool) []map[string]any {
	var out []map[string]any
	appendTool := func(tool Tool) {
		item := map[string]any{
			"name":         tool.Name,
			"input_schema": tool.Parameters,
		}
		if tool.Description != "" {
			item["description"] = tool.Description
		}
		if tool.Parameters == nil {
			item["input_schema"] = map[string]any{"type": "object"}
		}
		out = append(out, item)
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

// anthropicToolChoice 把 OpenAI 风格 tool_choice 归一为 Anthropic 形态：
// {type:function,name} → {type:tool,name}；required/any → any；none → nil（不下发）。
// 已是 Anthropic 形态的值原样透传。
func anthropicToolChoice(value any) any {
	payload, _ := value.(map[string]any)
	if payload == nil {
		return value
	}
	switch strings.ToLower(stringValue(payload["type"])) {
	case "function":
		name := stringValue(payload["name"])
		if function, _ := payload["function"].(map[string]any); function != nil {
			name = stringValue(function["name"])
		}
		return map[string]any{"type": "tool", "name": name}
	case "required", "any":
		return map[string]any{"type": "any"}
	case "none":
		return nil
	default:
		return value
	}
}

// decodeToolInput 把工具参数 JSON 字符串还原为对象：解析失败回退空对象
// （Anthropic 的 input 必须为对象；保留 id/name 以维持 tool_use ↔ tool_result 配对）。
func decodeToolInput(arguments string) map[string]any {
	trimmed := strings.TrimSpace(arguments)
	if trimmed == "" {
		return map[string]any{}
	}
	var input map[string]any
	if err := json.Unmarshal([]byte(trimmed), &input); err != nil || input == nil {
		return map[string]any{}
	}
	return input
}

// encodeToolInput 把工具参数对象序列化为 JSON 字符串（nil / 空对象返回 "")。
func encodeToolInput(input any) string {
	if input == nil {
		return ""
	}
	encoded, err := json.Marshal(input)
	if err != nil {
		return ""
	}
	if string(encoded) == "null" || string(encoded) == "{}" {
		return ""
	}
	return string(encoded)
}

// normalizeAnthropicVariant 归一化变体（去空白 + 小写；service 层 NormalizeLLMVariant 同口径）。
func normalizeAnthropicVariant(variant string) string {
	return strings.ToLower(strings.TrimSpace(variant))
}

// intValue 读取事件中的数值字段（JSON 数字统一解码为 float64），缺失按 0 处理。
func intValue(value any) int {
	if number, ok := value.(float64); ok {
		return int(number)
	}
	return 0
}

// ensureToolCallAccumulator 取得（必要时创建）指定 index 的工具调用累积器。
func ensureToolCallAccumulator(calls map[int]*streamToolCallAccumulator, order *[]int, index int) *streamToolCallAccumulator {
	accumulator := calls[index]
	if accumulator == nil {
		accumulator = &streamToolCallAccumulator{}
		calls[index] = accumulator
		*order = append(*order, index)
	}
	return accumulator
}
