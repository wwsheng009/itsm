package protocol

import (
	"encoding/json"
	"fmt"
	"io"
	"strconv"
	"strings"
)

// OpenAIResponsesAdapter 承载 openai_responses 形态（独立计划 PA-3）：
// OpenAI Responses API（POST /v1/responses）及其兼容上游。
//
// 载体决策（Q1，PA-3 启动时定）：
//   - 以**官方 Responses API** 为载体（`Authorization: Bearer`、`/v1/responses`），
//     不复刻 ChatGPT backend 形态（依赖 OAuth/session 凭证与专有头部，不适配
//     itsm 的「endpoint + api_key」实例模型）；
//   - 默认 endpoint 不登记（与 openai_chat_completions 同址回退 api.openai.com）；
//   - 变体：仅标准形态（VariantDefault）。厂商差异（若有）按「一协议一实现」以构造期
//     选项引入，不新增变体适配器。
//
// 会话模型：一次调用重放完整历史（`input` 数组），默认 `store=false` 关闭服务端存储
// （与参考实现 codex.go 的 stateless 口径一致；itsm 无 previous_response_id 续接语义）。
//
// 与 openai_chat_completions 的形态差异（线上格式，逐项见独立计划 §4.3 PA-3 表）：
// messages → input 项数组（function_call / function_call_output 独立项）、max_tokens →
// max_output_tokens、tools 为扁平结构（无嵌套 function）、流式为事件驱动 SSE。
type OpenAIResponsesAdapter struct{}

// NewOpenAIResponsesAdapter 创建 Responses 适配器实例（无状态，可并发复用）。
func NewOpenAIResponsesAdapter() *OpenAIResponsesAdapter { return &OpenAIResponsesAdapter{} }

// Name 返回协议枚举值。
func (a *OpenAIResponsesAdapter) Name() string { return ProtocolOpenAIResponses }

// GetAPIPath 返回默认 API 路径（参考实现 codex.go:876 同值）。
func (a *OpenAIResponsesAdapter) GetAPIPath() string { return "/v1/responses" }

// IsReasoningModel 与 openai_chat_completions 同口径（codex / gpt-5 / o1-o5 前缀）：
// 推理模型不下发 temperature（Responses API 对推理模型同样拒绝采样参数）。
func (a *OpenAIResponsesAdapter) IsReasoningModel(model string) bool {
	return isOpenAIReasoningModel(model)
}

// BuildHeaders 构建请求头：Bearer 鉴权（apiKey 为空时不下发）+ 调用方附加 headers。
func (a *OpenAIResponsesAdapter) BuildHeaders(cfg AdapterConfig) map[string]string {
	headers := map[string]string{"Content-Type": "application/json"}
	if apiKey := strings.TrimSpace(cfg.APIKey); apiKey != "" {
		headers["Authorization"] = "Bearer " + apiKey
	}
	for key, value := range cfg.Headers {
		setHeaderCaseInsensitive(headers, key, value)
	}
	return headers
}

// BuildRequest 构建 Responses 请求体。
//
// 字段口径（按需下发，省略等价默认值）：
//   - 前导 system/developer 消息合并为顶层 instructions（后续 system 保留在 input 中）；
//   - input 为 Responses 项数组（message / function_call / function_call_output）；
//   - store 恒为 false：itsm 每次重放完整历史，不启用服务端响应存储；
//   - max_output_tokens 仅在 >0 时下发（接线层缺省 4096）；
//   - temperature 仅非推理模型下发；
//   - tools 为扁平结构（type/name/description/parameters），tool_choice 归一为
//     Responses 形态（Chat 风格 {"type":"function","function":{...}} 展平）。
func (a *OpenAIResponsesAdapter) BuildRequest(cfg RequestConfig) map[string]any {
	instructions, input := buildResponsesInput(cfg.Messages)
	body := map[string]any{
		"model": cfg.Model,
		"input": input,
		"store": false,
	}
	if instructions != "" {
		body["instructions"] = instructions
	}
	if cfg.Stream {
		body["stream"] = true
	}
	if cfg.MaxTokens > 0 {
		body["max_output_tokens"] = cfg.MaxTokens
	}
	if !cfg.ReasoningModel {
		body["temperature"] = cfg.Temperature
	}
	if tools := collectResponsesTools(cfg.Messages, cfg.Tools); len(tools) > 0 {
		body["tools"] = tools
	}
	if cfg.ToolChoice != nil {
		if choice := normalizeResponsesToolChoice(cfg.ToolChoice); choice != nil {
			body["tool_choice"] = choice
		}
	}
	return body
}

// HandleResponse 解析响应：非流式解析 JSON；流式按事件驱动 SSE 逐帧回调并累积工具调用。
func (a *OpenAIResponsesAdapter) HandleResponse(isStream bool, respBody io.Reader, callbacks StreamCallbacks) (ProcessResult, error) {
	if respBody == nil {
		return ProcessResult{}, fmt.Errorf("%s: response body is required", ProtocolOpenAIResponses)
	}
	if isStream {
		return a.handleStream(respBody, callbacks)
	}
	return a.handleCompletion(respBody)
}

// ProcessResponse 从非流式响应体提取归一化结果（text / reasoning / function_call / finish_reason）。
func (a *OpenAIResponsesAdapter) ProcessResponse(result map[string]any) ProcessResult {
	items := responsesOutputItems(result)
	var (
		content   strings.Builder
		reasoning strings.Builder
		calls     []ToolCall
	)
	for _, item := range items {
		switch stringValue(item["type"]) {
		case "message":
			content.WriteString(responsesMessageText(item))
		case "reasoning":
			reasoning.WriteString(responsesReasoningText(item))
		case "function_call":
			if call, ok := responsesFunctionCall(item); ok {
				calls = append(calls, call)
			}
		}
	}
	return ProcessResult{
		Content:      content.String(),
		Reasoning:    reasoning.String(),
		ToolCalls:    calls,
		FinishReason: responsesFinishReason(result, calls),
	}
}

func (a *OpenAIResponsesAdapter) handleCompletion(respBody io.Reader) (ProcessResult, error) {
	var result map[string]any
	if err := json.NewDecoder(respBody).Decode(&result); err != nil {
		return ProcessResult{}, fmt.Errorf("%s: decode response: %w", ProtocolOpenAIResponses, err)
	}
	if payloadErr := responsesPayloadError(result); payloadErr != nil {
		return ProcessResult{}, payloadErr
	}
	return a.ProcessResponse(result), nil
}

// responsesStreamState 流式累积状态（事件驱动：文本/推理增量与 function_call 参数分片）。
type responsesStreamState struct {
	content      strings.Builder
	reasoning    strings.Builder
	finishReason string
	calls        map[int]*streamToolCallAccumulator
	order        []int
}

func (s *responsesStreamState) call(index int) *streamToolCallAccumulator {
	acc := s.calls[index]
	if acc == nil {
		acc = &streamToolCallAccumulator{}
		s.calls[index] = acc
		s.order = append(s.order, index)
	}
	return acc
}

func (a *OpenAIResponsesAdapter) handleStream(respBody io.Reader, callbacks StreamCallbacks) (ProcessResult, error) {
	state := &responsesStreamState{calls: map[int]*streamToolCallAccumulator{}}

	scanErr := scanSSEFrames(respBody, func(frame SSEFrame) (bool, error) {
		data := strings.TrimSpace(frame.Data)
		if data == "" {
			return true, nil
		}
		var event map[string]any
		if err := json.Unmarshal([]byte(data), &event); err != nil {
			return false, fmt.Errorf("%s: malformed stream chunk: %w", ProtocolOpenAIResponses, err)
		}
		eventType := strings.TrimSpace(frame.Event)
		if eventType == "" {
			eventType = stringValue(event["type"])
		}
		return a.processStreamEvent(state, eventType, event, callbacks)
	})
	if scanErr != nil {
		return ProcessResult{}, scanErr
	}
	return ProcessResult{
		Content:      state.content.String(),
		Reasoning:    state.reasoning.String(),
		ToolCalls:    toolCallsFromAccumulator(state.calls, state.order),
		FinishReason: state.finishReason,
	}, nil
}

// processStreamEvent 处理单个 Responses 事件；返回 (是否继续读取, 错误)。
func (a *OpenAIResponsesAdapter) processStreamEvent(state *responsesStreamState, eventType string, event map[string]any, callbacks StreamCallbacks) (bool, error) {
	switch eventType {
	case "response.output_text.delta":
		if delta := stringValue(event["delta"]); delta != "" {
			state.content.WriteString(delta)
			callbacks.EmitText(delta)
		}
	case "response.reasoning_summary_text.delta", "response.reasoning_text.delta":
		if delta := stringValue(event["delta"]); delta != "" {
			state.reasoning.WriteString(delta)
			callbacks.EmitReasoning(delta)
		}
	case "response.output_item.added":
		if item, _ := event["item"].(map[string]any); stringValue(item["type"]) == "function_call" {
			acc := state.call(responsesEventIndex(event))
			if id := responsesCallID(item); id != "" {
				acc.id = id
			}
			if name := stringValue(item["name"]); name != "" {
				acc.name = name
			}
			if args := stringValue(item["arguments"]); args != "" {
				acc.args.WriteString(args)
			}
		}
	case "response.function_call_arguments.delta":
		if delta := stringValue(event["delta"]); delta != "" {
			state.call(responsesEventIndex(event)).args.WriteString(delta)
		}
	case "response.output_item.done":
		item, _ := event["item"].(map[string]any)
		if stringValue(item["type"]) != "function_call" {
			break
		}
		// done 事件携带完整参数：作为权威值补齐/修正增量缺失。
		acc := state.call(responsesEventIndex(event))
		if id := responsesCallID(item); id != "" {
			acc.id = id
		}
		if name := stringValue(item["name"]); name != "" {
			acc.name = name
		}
		if args := stringValue(item["arguments"]); args != "" && acc.args.Len() == 0 {
			acc.args.WriteString(args)
		}
	case "response.completed", "response.incomplete", "response.done":
		response, _ := event["response"].(map[string]any)
		// 先补齐快照再定结束原因：completed 的 "tool_calls"/"stop" 取决于最终工具调用集合。
		a.recoverStreamSnapshot(state, response, callbacks)
		state.finishReason = firstNonEmpty(state.finishReason,
			responsesFinishReason(response, toolCallsFromAccumulator(state.calls, state.order)))
	case "response.failed":
		return false, responsesFailureError(event)
	case "error":
		return false, responsesInBandError(event)
	}
	return true, nil
}

// recoverStreamSnapshot 在事件流未携带增量（或上游只在终态快照给出结果）时补齐正文与工具调用：
// 仅在对应累积为空时生效，避免与增量重复。
func (a *OpenAIResponsesAdapter) recoverStreamSnapshot(state *responsesStreamState, response map[string]any, callbacks StreamCallbacks) {
	for _, item := range responsesOutputItems(response) {
		switch stringValue(item["type"]) {
		case "message":
			if state.content.Len() == 0 {
				if text := responsesMessageText(item); text != "" {
					state.content.WriteString(text)
					callbacks.EmitText(text)
				}
			}
		case "reasoning":
			if state.reasoning.Len() == 0 {
				if text := responsesReasoningText(item); text != "" {
					state.reasoning.WriteString(text)
					callbacks.EmitReasoning(text)
				}
			}
		case "function_call":
			call, ok := responsesFunctionCall(item)
			if !ok {
				continue
			}
			acc := state.callResponsesCall(call)
			if acc.id == "" {
				acc.id = call.ID
			}
			if acc.name == "" {
				acc.name = call.Name
			}
			if acc.args.Len() == 0 && call.Arguments != "" {
				acc.args.WriteString(call.Arguments)
			}
		}
	}
}

// callResponsesCall 按 call_id/name 复用已累积的调用；未命中时追加一个新的索引槽。
func (s *responsesStreamState) callResponsesCall(call ToolCall) *streamToolCallAccumulator {
	for _, index := range s.order {
		acc := s.calls[index]
		if acc == nil {
			continue
		}
		if acc.id != "" && acc.id == call.ID {
			return acc
		}
		if acc.name != "" && acc.name == call.Name {
			return acc
		}
	}
	return s.call(len(s.order))
}

// buildResponsesInput 组装 Responses input 项数组：前导 system/developer 合并为 instructions，
// 其余消息按角色映射为 message / function_call / function_call_output 项。
func buildResponsesInput(messages []Message) (string, []map[string]any) {
	input := make([]map[string]any, 0, len(messages))
	instructions := make([]string, 0, 2)
	inLeadingInstructions := true

	for _, message := range messages {
		role := strings.ToLower(strings.TrimSpace(message.Role))
		if role == "system" || role == "developer" {
			if inLeadingInstructions {
				if text := strings.TrimSpace(message.Content); text != "" {
					instructions = append(instructions, text)
				}
				continue
			}
			input = append(input, responsesMessageItem("developer", message.Content, "input_text"))
			continue
		}
		inLeadingInstructions = false

		switch role {
		case "assistant":
			if strings.TrimSpace(message.Content) != "" {
				input = append(input, responsesMessageItem("assistant", message.Content, "output_text"))
			}
			for _, call := range message.ToolCalls {
				input = append(input, map[string]any{
					"type":      "function_call",
					"call_id":   call.ID,
					"name":      call.Name,
					"arguments": call.Arguments,
				})
			}
		case "tool":
			input = append(input, map[string]any{
				"type":    "function_call_output",
				"call_id": message.ToolCallID,
				"output":  message.Content,
			})
		default:
			input = append(input, responsesMessageItem("user", message.Content, "input_text"))
		}
	}
	return strings.Join(instructions, "\n\n"), input
}

func responsesMessageItem(role, content, contentType string) map[string]any {
	item := map[string]any{"type": "message", "role": role}
	if strings.TrimSpace(content) != "" {
		item["content"] = []map[string]any{{"type": contentType, "text": content}}
	} else {
		item["content"] = []map[string]any{}
	}
	return item
}

// collectResponsesTools 合并消息与调用侧的工具声明，输出 Responses 扁平结构
// （type/name/description/parameters；Chat 风格嵌套 function 在此展平）。
func collectResponsesTools(messages []Message, explicit []Tool) []map[string]any {
	var out []map[string]any
	appendTool := func(tool Tool) {
		item := map[string]any{"type": "function", "name": tool.Name}
		if tool.Description != "" {
			item["description"] = tool.Description
		}
		if tool.Parameters != nil {
			item["parameters"] = tool.Parameters
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

// normalizeResponsesToolChoice 归一 tool_choice：字符串原样；Chat 风格
// {"type":"function","function":{"name":...}} 展平为 {"type":"function","name":...}。
func normalizeResponsesToolChoice(raw any) any {
	if _, ok := raw.(string); ok {
		return raw
	}
	choice, ok := raw.(map[string]any)
	if !ok || len(choice) == 0 {
		return nil
	}
	if name := strings.TrimSpace(stringValue(choice["name"])); name != "" {
		return map[string]any{"type": "function", "name": name}
	}
	if function, _ := choice["function"].(map[string]any); function != nil {
		if name := strings.TrimSpace(stringValue(function["name"])); name != "" {
			return map[string]any{"type": "function", "name": name}
		}
	}
	return nil
}

// responsesOutputItems 读取响应的 output 项数组。
func responsesOutputItems(payload map[string]any) []map[string]any {
	raw, _ := payload["output"].([]any)
	if len(raw) == 0 {
		return nil
	}
	items := make([]map[string]any, 0, len(raw))
	for _, entry := range raw {
		if item, ok := entry.(map[string]any); ok {
			items = append(items, item)
		}
	}
	return items
}

// responsesMessageText 拼接 message 项的 output_text（refusal 段落归入正文，保持可见）。
func responsesMessageText(item map[string]any) string {
	parts, _ := item["content"].([]any)
	if len(parts) == 0 {
		return ""
	}
	var builder strings.Builder
	for _, entry := range parts {
		part, _ := entry.(map[string]any)
		if part == nil {
			continue
		}
		switch stringValue(part["type"]) {
		case "output_text":
			builder.WriteString(stringValue(part["text"]))
		case "refusal":
			builder.WriteString(firstNonEmpty(stringValue(part["refusal"]), stringValue(part["text"])))
		}
	}
	return builder.String()
}

// responsesReasoningText 拼接 reasoning 项的 summary 文本（参考实现 codex_parser.md 口径）。
func responsesReasoningText(item map[string]any) string {
	parts, _ := item["summary"].([]any)
	if len(parts) == 0 {
		return ""
	}
	var builder strings.Builder
	for _, entry := range parts {
		part, _ := entry.(map[string]any)
		if part == nil {
			continue
		}
		builder.WriteString(firstNonEmpty(stringValue(part["text"]), stringValue(part["content"])))
	}
	return builder.String()
}

// responsesFunctionCall 把一个 function_call 项映射为 ToolCall；缺名或缺调用 ID 的项丢弃
// （无法回放工具结果），ID 缺省回退项自身 id。
func responsesFunctionCall(item map[string]any) (ToolCall, bool) {
	name := strings.TrimSpace(stringValue(item["name"]))
	if name == "" {
		return ToolCall{}, false
	}
	id := strings.TrimSpace(responsesCallID(item))
	if id == "" {
		return ToolCall{}, false
	}
	return ToolCall{ID: id, Name: name, Arguments: stringValue(item["arguments"])}, true
}

// responsesCallID 返回工具调用 ID：call_id 优先，回退项 id。
func responsesCallID(item map[string]any) string {
	if id := strings.TrimSpace(stringValue(item["call_id"])); id != "" {
		return id
	}
	return strings.TrimSpace(stringValue(item["id"]))
}

// responsesFinishReason 归一结束原因：优先上游 stop_reason（Codex 兼容上游），
// 否则按 status 推断（completed → tool_calls/stop；incomplete → incomplete_details.reason）。
func responsesFinishReason(payload map[string]any, toolCalls []ToolCall) string {
	if payload == nil {
		return ""
	}
	if reason := strings.TrimSpace(stringValue(payload["stop_reason"])); reason != "" {
		return reason
	}
	switch strings.ToLower(strings.TrimSpace(stringValue(payload["status"]))) {
	case "completed":
		if len(toolCalls) > 0 {
			return "tool_calls"
		}
		return "stop"
	case "incomplete":
		if details, _ := payload["incomplete_details"].(map[string]any); details != nil {
			if reason := strings.TrimSpace(stringValue(details["reason"])); reason != "" {
				return reason
			}
		}
		return "incomplete"
	default:
		return ""
	}
}

// responsesPayloadError 识别非流式响应内的失败态与错误对象（带内错误，不重试）。
func responsesPayloadError(payload map[string]any) *ProtocolError {
	if err := errorFromPayload(ProtocolOpenAIResponses, payload); err != nil {
		return err
	}
	if strings.EqualFold(strings.TrimSpace(stringValue(payload["status"])), "failed") {
		return responsesFailureError(payload)
	}
	return nil
}

// responsesFailureError 把 response.failed 事件/响应映射为带内 *ProtocolError（StatusCode=0）。
func responsesFailureError(payload map[string]any) *ProtocolError {
	err := &ProtocolError{Protocol: ProtocolOpenAIResponses}
	body := responsesErrorBody(payload)
	if upstream := errorPayload(body); upstream != nil {
		err.Code = upstream.Code
		err.Message = upstream.Message
	}
	if err.Message == "" {
		err.Message = "responses request failed"
	}
	if err.Code == "" {
		err.Code = "response_failed"
	}
	return err
}

// responsesInBandError 把顶层 error 事件映射为带内 *ProtocolError（不重试）。
func responsesInBandError(event map[string]any) *ProtocolError {
	err := &ProtocolError{Protocol: ProtocolOpenAIResponses}
	if upstream := errorPayload(responsesErrorBody(event)); upstream != nil {
		err.Code = upstream.Code
		err.Message = upstream.Message
	}
	// 兼容 error 事件把 code/message 平铺在事件顶层的上游（官方形态为 {"code","message"}）。
	if err.Code == "" {
		err.Code = stringValue(event["code"])
	}
	if err.Message == "" {
		err.Message = stringValue(event["message"])
	}
	if err.Message == "" {
		err.Message = "responses stream error"
	}
	if err.Code == "" {
		err.Code = "stream_error"
	}
	return err
}

// responsesErrorBody 定位错误对象所在层：优先 error 对象非空的 response 子文档，
// 否则回退事件/响应自身（兼容两种上游形态）。
func responsesErrorBody(payload map[string]any) map[string]any {
	if response, _ := payload["response"].(map[string]any); response != nil {
		if errorPayload(response) != nil {
			return response
		}
	}
	return payload
}

// responsesEventIndex 读取事件索引（缺失按 0 处理，与 Chat 流式工具调用分片口径一致）。
func responsesEventIndex(event map[string]any) int {
	switch value := event["index"].(type) {
	case float64:
		return int(value)
	case json.Number:
		if parsed, err := strconv.Atoi(value.String()); err == nil {
			return parsed
		}
	case string:
		if parsed, err := strconv.Atoi(strings.TrimSpace(value)); err == nil {
			return parsed
		}
	}
	return 0
}

// firstNonEmpty 返回第一个非空字符串。
func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}
