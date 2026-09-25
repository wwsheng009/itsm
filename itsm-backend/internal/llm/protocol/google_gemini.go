package protocol

import (
	"encoding/json"
	"fmt"
	"io"
	"strings"
)

// 本文件是独立计划《LLM 协议适配层收敛》PA-4 的交付物：google_gemini 适配器
// （一协议一实现，variant 只是构造期选项）。
//
// 载体口径（参考实现 E:\projects\ai-agent-runtime\backend\internal\llm\adapter\gemini.go，
// 差异/决策逐条登记于独立计划 §4.3 PA-4 表）：
//   - REST：POST {endpoint}/v1beta/models/{model}:generateContent（非流式）/
//     :streamGenerateContent?alt=sse（流式 SSE），资源路径含模型名，故由
//     APIPathFor 在请求期给出（service 侧经 ModelPathAdapter 取值）；
//   - 鉴权：x-goog-api-key 请求头（官方推荐：密钥不进 URL/日志；`?key=` 形态不复刻）；
//   - 会话：contents[] = {role: user|model, parts[]}；前导 system 合并为顶层
//     systemInstruction（\n\n 连接），非前导 system 降级为 user 文本（Gemini 无 system 角色）；
//   - 工具：tools[].functionDeclarations[] 声明、parts[].functionCall 发起、
//     parts[].functionResponse 回填（响应侧 functionCall 无 id，适配器生成 call_N）；
//   - 采样：generationConfig.maxOutputTokens / temperature（推理模型抑制，与 chat / responses
//     适配器同表口径）；topP 等参考实现独有默认值不下发（§4.3 登记）。
const (
	// geminiAPIPrefix Gemini API 版本段（v1beta）。
	geminiAPIPrefix = "/v1beta"
	// geminiDefaultEndpoint 官方 Gemini API 默认地址（endpoint 未配置时的回退）。
	geminiDefaultEndpoint = "https://generativelanguage.googleapis.com"
	// geminiFunctionResponseFallbackName 工具结果回填时无法从历史解析出函数名所用占位名。
	geminiFunctionResponseFallbackName = "tool"
)

// GoogleGeminiAdapter 承载 google_gemini 协议（Gemini API v1beta）。
// 无状态、可被多 goroutine 并发复用；variant 差异在构造期固化。
type GoogleGeminiAdapter struct {
	variant string
}

// NewGoogleGeminiAdapter 创建 Gemini 适配器实例（标准形态只有一个变体）。
func NewGoogleGeminiAdapter(variant string) *GoogleGeminiAdapter {
	return &GoogleGeminiAdapter{variant: normalizeProtocolName(variant)}
}

// Name 返回协议枚举值。
func (a *GoogleGeminiAdapter) Name() string { return ProtocolGoogleGemini }

// GetAPIPath 返回静态回退路径（版本段）：Gemini 的资源路径包含模型名与操作，
// 由 APIPathFor 在请求期给出（service 经 ModelPathAdapter 可选接口取值）。
func (a *GoogleGeminiAdapter) GetAPIPath() string { return geminiAPIPrefix }

// APIPathFor 返回一次调用的资源路径（ModelPathAdapter）：
//   - 非流式：/v1beta/models/{model}:generateContent
//   - 流式：/v1beta/models/{model}:streamGenerateContent?alt=sse
//
// 模型名为空时退回版本段（由上游返回语义错误，不在本地构造半成品路径）。
func (a *GoogleGeminiAdapter) APIPathFor(model string, stream bool) string {
	name := geminiModelName(model)
	if name == "" {
		return geminiAPIPrefix
	}
	if stream {
		return geminiAPIPrefix + "/models/" + name + ":streamGenerateContent?alt=sse"
	}
	return geminiAPIPrefix + "/models/" + name + ":generateContent"
}

// IsReasoningModel 判定推理模型：当前恒为 false。
//
// 口径（§4.3 PA-4 登记）：Gemini 的思考能力由模型自身决定（2.5+/3.x 的 thought parts 已在
// 响应侧解析为 Reasoning），请求侧没有与 OpenAI 推理模型等价的"拒绝采样参数"约束，
// 因此恒下发 temperature（与参考实现 IsReasoningModel 恒 false 同口径）。
func (a *GoogleGeminiAdapter) IsReasoningModel(model string) bool { return false }

// BuildHeaders 构建请求头：x-goog-api-key 鉴权 + 调用方附加 headers（大小写不敏感覆盖）。
func (a *GoogleGeminiAdapter) BuildHeaders(cfg AdapterConfig) map[string]string {
	headers := map[string]string{"Content-Type": "application/json"}
	if apiKey := strings.TrimSpace(cfg.APIKey); apiKey != "" {
		headers["x-goog-api-key"] = apiKey
	}
	for key, value := range cfg.Headers {
		setHeaderCaseInsensitive(headers, key, value)
	}
	return headers
}

// geminiModelName 归一模型名：去空白与 `models/` 前缀（`gemini-2.0-flash` 与
// `models/gemini-2.0-flash` 两种写法都接受）。
func geminiModelName(model string) string {
	name := strings.TrimSpace(model)
	name = strings.TrimPrefix(name, "models/")
	return strings.TrimSpace(name)
}

// BuildRequest 构建 Gemini generateContent 请求体（字段按需下发，省略等价默认值）。
func (a *GoogleGeminiAdapter) BuildRequest(cfg RequestConfig) map[string]any {
	contents, systemInstruction := buildGeminiContents(cfg.Messages)
	body := map[string]any{"contents": contents}
	if systemInstruction != "" {
		body["systemInstruction"] = map[string]any{
			"parts": []map[string]any{{"text": systemInstruction}},
		}
	}

	generationConfig := map[string]any{}
	if cfg.MaxTokens > 0 {
		generationConfig["maxOutputTokens"] = cfg.MaxTokens
	}
	if !cfg.ReasoningModel {
		generationConfig["temperature"] = cfg.Temperature
	}
	if len(generationConfig) > 0 {
		body["generationConfig"] = generationConfig
	}

	if declarations := collectGeminiTools(cfg.Messages, cfg.Tools); len(declarations) > 0 {
		body["tools"] = []map[string]any{{"functionDeclarations": declarations}}
	}
	if toolConfig := geminiToolConfig(cfg.ToolChoice); toolConfig != nil {
		body["toolConfig"] = toolConfig
	}
	return body
}

// buildGeminiContents 把归一消息转换为 Gemini contents[]，并抽取前导 system 文本。
//
// 角色映射：user/system → user（system 见下）、assistant/model → model、tool/function → user
// （functionResponse part）。前导（出现在任何非 system 消息之前）的 system 消息拼接为
// systemInstruction；更靠后的 system 消息降级为 user 文本（Gemini 无 system 角色）。
func buildGeminiContents(messages []Message) ([]map[string]any, string) {
	toolNames := collectGeminiToolNames(messages)
	contents := make([]map[string]any, 0, len(messages))
	systemParts := make([]string, 0, 1)
	leadingSystem := true

	for _, message := range messages {
		role := strings.ToLower(strings.TrimSpace(message.Role))
		if role == "system" {
			if leadingSystem {
				if text := strings.TrimSpace(message.Content); text != "" {
					systemParts = append(systemParts, text)
				}
				continue
			}
			if text := strings.TrimSpace(message.Content); text != "" {
				contents = append(contents, geminiPartsMessage("user", []map[string]any{{"text": text}}))
			}
			continue
		}
		leadingSystem = false

		switch role {
		case "tool", "function":
			if part, ok := geminiFunctionResponsePart(message, toolNames); ok {
				contents = append(contents, geminiPartsMessage("user", []map[string]any{part}))
			}
		case "assistant", "model":
			if parts := geminiModelParts(message); len(parts) > 0 {
				contents = append(contents, geminiPartsMessage("model", parts))
			}
		default:
			if text := strings.TrimSpace(message.Content); text != "" {
				contents = append(contents, geminiPartsMessage("user", []map[string]any{{"text": text}}))
			}
		}
	}
	return contents, strings.Join(systemParts, "\n\n")
}

// geminiPartsMessage 构造一条 contents 项。
func geminiPartsMessage(role string, parts []map[string]any) map[string]any {
	return map[string]any{"role": role, "parts": parts}
}

// geminiModelParts 构造 model 消息的 parts：文本 + 历史工具调用（functionCall）。
func geminiModelParts(message Message) []map[string]any {
	parts := make([]map[string]any, 0, len(message.ToolCalls)+1)
	if text := strings.TrimSpace(message.Content); text != "" {
		parts = append(parts, map[string]any{"text": text})
	}
	for _, call := range message.ToolCalls {
		name := strings.TrimSpace(call.Name)
		if name == "" {
			continue
		}
		parts = append(parts, map[string]any{
			"functionCall": map[string]any{"name": name, "args": geminiFunctionArgs(call.Arguments)},
		})
	}
	return parts
}

// geminiFunctionResponsePart 构造工具结果的 functionResponse part。
//
// Gemini 要求 functionResponse 带函数名（itsm 的 tool 消息只携带 ToolCallID），
// 这里从同一会话历史里的 functionCall 反查；反查不到时用占位名保持请求结构合法。
func geminiFunctionResponsePart(message Message, toolNames map[string]string) (map[string]any, bool) {
	name := strings.TrimSpace(toolNames[strings.TrimSpace(message.ToolCallID)])
	if name == "" {
		name = geminiFunctionResponseFallbackName
	}
	content := strings.TrimSpace(message.Content)
	response := map[string]any{}
	if content != "" {
		if err := json.Unmarshal([]byte(content), &response); err != nil || response == nil {
			response = map[string]any{"result": content}
		}
	}
	if len(response) == 0 {
		response = map[string]any{"result": ""}
	}
	return map[string]any{
		"functionResponse": map[string]any{"name": name, "response": response},
	}, true
}

// collectGeminiToolNames 建立 ToolCallID → 函数名 的索引（顺序遍历，后者覆盖同名 id）。
func collectGeminiToolNames(messages []Message) map[string]string {
	names := map[string]string{}
	for _, message := range messages {
		for _, call := range message.ToolCalls {
			id := strings.TrimSpace(call.ID)
			name := strings.TrimSpace(call.Name)
			if id == "" || name == "" {
				continue
			}
			names[id] = name
		}
	}
	return names
}

// collectGeminiTools 合并工具声明（消息携带的 Tools 优先，与 openai_chat 适配器同序），
// 输出 functionDeclarations 数组；无参数工具补空对象 schema（Gemini 要求 parameters 为对象）。
func collectGeminiTools(messages []Message, explicit []Tool) []map[string]any {
	seen := map[string]bool{}
	out := make([]map[string]any, 0, len(explicit))
	appendTool := func(tool Tool) {
		name := strings.TrimSpace(tool.Name)
		if name == "" || seen[name] {
			return
		}
		seen[name] = true
		parameters := tool.Parameters
		if parameters == nil {
			parameters = map[string]any{"type": "object", "properties": map[string]any{}}
		}
		declaration := map[string]any{"name": name, "parameters": parameters}
		if description := strings.TrimSpace(tool.Description); description != "" {
			declaration["description"] = description
		}
		out = append(out, declaration)
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

// geminiFunctionArgs 解析工具调用参数（JSON 对象串）；空/非法一律降级为空对象，
// 保证 functionCall.args 恒为 JSON 对象（Gemini 要求，参考实现同口径）。
func geminiFunctionArgs(raw string) map[string]any {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return map[string]any{}
	}
	var args map[string]any
	if err := json.Unmarshal([]byte(trimmed), &args); err != nil || args == nil {
		return map[string]any{}
	}
	return args
}

// geminiToolConfig 归一 tool_choice 为 Gemini toolConfig：
//   - 字符串 auto / none / required(=any) → functionCallingConfig.mode；
//   - {"functionCallingConfig":{…}} 原样透传（Gemini 原生形态）；
//   - OpenAI Chat 风格 {"type":"function","function":{"name":…}} → ANY + allowedFunctionNames；
//   - {"mode":"ANY"} 简写与上述等价；
//   - nil / 不可识别 → 不下发（与既有 go-openai 实现"未设置不下发"一致）。
func geminiToolConfig(choice any) map[string]any {
	if choice == nil {
		return nil
	}
	switch value := choice.(type) {
	case string:
		if mode := geminiToolMode(value); mode != "" {
			return map[string]any{"functionCallingConfig": map[string]any{"mode": mode}}
		}
		return nil
	case map[string]any:
		if config, ok := value["functionCallingConfig"].(map[string]any); ok && len(config) > 0 {
			return map[string]any{"functionCallingConfig": config}
		}
		if function, ok := value["function"].(map[string]any); ok {
			if name := strings.TrimSpace(stringValue(function["name"])); name != "" {
				return map[string]any{"functionCallingConfig": map[string]any{
					"mode":                 "ANY",
					"allowedFunctionNames": []string{name},
				}}
			}
			return nil
		}
		if mode := geminiToolMode(stringValue(value["mode"])); mode != "" {
			return map[string]any{"functionCallingConfig": map[string]any{"mode": mode}}
		}
		return nil
	default:
		return nil
	}
}

// geminiToolMode 归一工具选择模式为 Gemini 枚举（AUTO / NONE / ANY）。
func geminiToolMode(raw string) string {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "auto":
		return "AUTO"
	case "none":
		return "NONE"
	case "required", "any":
		return "ANY"
	default:
		return ""
	}
}

// HandleResponse 解析响应：非流式解析 JSON；流式按 SSE（alt=sse）逐帧回调并累积。
func (a *GoogleGeminiAdapter) HandleResponse(isStream bool, respBody io.Reader, callbacks StreamCallbacks) (ProcessResult, error) {
	if respBody == nil {
		return ProcessResult{}, fmt.Errorf("%s: response body is required", ProtocolGoogleGemini)
	}
	if isStream {
		return a.handleStream(respBody, callbacks)
	}
	var payload map[string]any
	if err := json.NewDecoder(respBody).Decode(&payload); err != nil {
		return ProcessResult{}, fmt.Errorf("%s: decode response: %w", ProtocolGoogleGemini, err)
	}
	if payloadErr := errorFromPayload(ProtocolGoogleGemini, payload); payloadErr != nil {
		return ProcessResult{}, payloadErr
	}
	if blocked := geminiPromptBlockedError(payload); blocked != nil {
		return ProcessResult{}, blocked
	}
	return a.ProcessResponse(payload), nil
}

// ProcessResponse 从非流式响应体提取归一化结果（含工具调用与 thought 推理链）。
func (a *GoogleGeminiAdapter) ProcessResponse(result map[string]any) ProcessResult {
	state := &geminiStreamState{}
	geminiConsumeChunk(state, result, StreamCallbacks{})
	return state.result()
}

// handleStream 按 SSE 解析 streamGenerateContent 响应：
//
//	帧内错误（error）/ 拦截（promptFeedback.blockReason）→ 带内错误（StatusCode=0，不重试）；
//	candidates[0].content.parts → 文本 / thought / functionCall 增量（functionCall 为完整对象，
//	不需要跨帧拼接，与 OpenAI 的 arguments 分片不同）；
//	candidates[0].finishReason → 原生值透传（STOP / MAX_TOKENS / SAFETY…）。
func (a *GoogleGeminiAdapter) handleStream(respBody io.Reader, callbacks StreamCallbacks) (ProcessResult, error) {
	state := &geminiStreamState{}
	err := scanSSEFrames(respBody, func(frame SSEFrame) (bool, error) {
		data := strings.TrimSpace(frame.Data)
		if data == "" || data == "[DONE]" {
			return true, nil
		}
		var chunk map[string]any
		if err := json.Unmarshal([]byte(data), &chunk); err != nil {
			return false, fmt.Errorf("%s: decode stream chunk: %w", ProtocolGoogleGemini, err)
		}
		if payloadErr := errorFromPayload(ProtocolGoogleGemini, chunk); payloadErr != nil {
			return false, payloadErr
		}
		if blocked := geminiPromptBlockedError(chunk); blocked != nil {
			return false, blocked
		}
		geminiConsumeChunk(state, chunk, callbacks)
		return true, nil
	})
	if err != nil {
		return ProcessResult{}, err
	}
	return state.result(), nil
}

// geminiStreamState 流式/非流式共用的累积状态。
type geminiStreamState struct {
	content      strings.Builder
	reasoning    strings.Builder
	toolCalls    []ToolCall
	finishReason string
}

// result 输出归一化结果（tools 为空时保持 nil，与其它适配器一致）。
func (s *geminiStreamState) result() ProcessResult {
	return ProcessResult{
		Content:      s.content.String(),
		Reasoning:    s.reasoning.String(),
		ToolCalls:    s.toolCalls,
		FinishReason: s.finishReason,
	}
}

// geminiConsumeChunk 消费一帧/一个响应体：候选首项的 finishReason 与 content.parts。
// 只取 candidates[0]（网关按单候选调用），未知字段与额外候选忽略（容错口径）。
func geminiConsumeChunk(state *geminiStreamState, payload map[string]any, callbacks StreamCallbacks) {
	candidates, _ := payload["candidates"].([]any)
	if len(candidates) == 0 {
		return
	}
	candidate, _ := candidates[0].(map[string]any)
	if candidate == nil {
		return
	}
	if reason := strings.TrimSpace(stringValue(candidate["finishReason"])); reason != "" && state.finishReason == "" {
		state.finishReason = reason
	}
	content, _ := candidate["content"].(map[string]any)
	if content == nil {
		return
	}
	parts, _ := content["parts"].([]any)
	for _, rawPart := range parts {
		part, _ := rawPart.(map[string]any)
		if part == nil {
			continue
		}
		geminiConsumePart(state, part, callbacks)
	}
}

// geminiConsumePart 消费一个 part：functionCall 优先（完整对象，无 id → 生成 call_N），
// 其次文本（thought=true 归入推理链，否则归入正文）。
func geminiConsumePart(state *geminiStreamState, part map[string]any, callbacks StreamCallbacks) {
	if functionCall, ok := part["functionCall"].(map[string]any); ok {
		if call, ok := geminiToolCall(functionCall, len(state.toolCalls)+1); ok {
			state.toolCalls = append(state.toolCalls, call)
		}
		return
	}
	text := stringValue(part["text"])
	if text == "" {
		return
	}
	if thought, _ := part["thought"].(bool); thought {
		state.reasoning.WriteString(text)
		callbacks.EmitReasoning(text)
		return
	}
	state.content.WriteString(text)
	callbacks.EmitText(text)
}

// geminiToolCall 归一 functionCall：id 由适配器生成（Gemini 不提供），
// args 归一为 JSON 对象字符串（ToolCall.Arguments 的既有口径）。
func geminiToolCall(functionCall map[string]any, index int) (ToolCall, bool) {
	name := strings.TrimSpace(stringValue(functionCall["name"]))
	if name == "" {
		return ToolCall{}, false
	}
	return ToolCall{
		ID:        fmt.Sprintf("call_%d", index),
		Name:      name,
		Arguments: geminiFunctionArgsValue(functionCall["args"]),
	}, true
}

// geminiFunctionArgsValue 把响应侧 functionCall.args（对象或字符串）归一为 JSON 对象串。
func geminiFunctionArgsValue(raw any) string {
	switch value := raw.(type) {
	case nil:
		return "{}"
	case string:
		trimmed := strings.TrimSpace(value)
		if trimmed == "" {
			return "{}"
		}
		return trimmed
	default:
		encoded, err := json.Marshal(value)
		if err != nil {
			return "{}"
		}
		return string(encoded)
	}
}

// geminiPromptBlockedError 识别 promptFeedback.blockReason（请求被安全策略拦截、
// 无候选返回）：映射为带内错误（StatusCode=0）——与 Anthropic/OpenAI 的流内错误同口径，不重试。
func geminiPromptBlockedError(payload map[string]any) *ProtocolError {
	feedback, _ := payload["promptFeedback"].(map[string]any)
	if feedback == nil {
		return nil
	}
	reason := strings.TrimSpace(stringValue(feedback["blockReason"]))
	if reason == "" {
		return nil
	}
	return &ProtocolError{
		Protocol: ProtocolGoogleGemini,
		Code:     reason,
		Message:  "prompt blocked: " + reason,
	}
}
