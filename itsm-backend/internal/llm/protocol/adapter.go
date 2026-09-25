// Package protocol 提供 LLM 协议适配层：把不同厂商/网关的 API 形态
// （openai_chat_completions / openai_responses / anthropic_messages / google_gemini）
// 归一为统一的请求构建、响应解析、流式事件、工具调用与错误映射。
//
// 本包是主计划《多 LLM Provider 支持与可切换方案》v1.4 BE-9 的交付物（P0 首适配器），
// 接口形状对齐参考实现（只读参考，不引入依赖）：
// E:\projects\ai-agent-runtime\backend\internal\llm\adapter ——
// Name / BuildRequest / BuildHeaders / HandleResponse / ProcessResponse /
// IsReasoningModel / GetAPIPath。
//
// 设计约束：
//   - 本包不 import service 包：Message/Tool/ToolCall 是 service.LLMMessage 的最小镜像，
//     避免后续 service 接线（Registry 构建路径分派）产生循环依赖；接线层负责类型转换。
//   - 一协议一实现：适配器与 4 值协议枚举一一对应，variant 只是构造期选项（不进注册表键）。
//     已交付 openai_chat_completions（P0）/ anthropic_messages（PA-1）/ openai_responses（PA-3）；
//     其余协议由独立计划 docs/plan/llm-protocol-adapter-plan.md 补齐（主计划 §11）。
package protocol

import "io"

// 协议枚举（主计划 §2.2 D3）：4 种 API 形态。
const (
	ProtocolOpenAIChatCompletions = "openai_chat_completions"
	ProtocolOpenAIResponses       = "openai_responses"
	ProtocolAnthropicMessages     = "anthropic_messages"
	ProtocolGoogleGemini          = "google_gemini"
)

// 兼容变体（主计划 §3.1.1 的 variant 字段）：同一 API 形态下的厂商/部署差异。
const (
	VariantDefault = ""
	VariantAzure   = "azure"
	VariantOllama  = "ollama"
	VariantMiniMax = "minimax"
)

// Message 是协议无关的会话消息（service.LLMMessage 的最小镜像）。
type Message struct {
	Role       string
	Content    string
	ToolCallID string
	ToolCalls  []ToolCall
	// Tools 请求侧：本消息声明的可用工具（网关通常挂载在 user/system 消息上）。
	Tools []Tool
}

// Tool 声明可供模型调用的工具（OpenAI function calling 风格）。
type Tool struct {
	Name        string
	Description string
	Parameters  map[string]any
}

// ToolCall 模型发起的一次工具调用。
type ToolCall struct {
	ID        string
	Name      string
	Arguments string
}

// RequestConfig 一次请求的协议无关配置（对齐参考实现同名类型，字段按 itsm 收敛）。
type RequestConfig struct {
	Model       string
	Messages    []Message
	Stream      bool
	MaxTokens   int
	Temperature float64
	// ReasoningModel 为 true 时按参考实现口径抑制 temperature（推理模型不接受该参数）。
	ReasoningModel bool
	// Tools 调用侧显式声明的工具（网关 ChatStreamWithTools 路径），与消息携带的 Tools 合并。
	Tools []Tool
	// ToolChoice 透传；nil 时不下发该字段（与既有 go-openai 实现的线上行为保持一致）。
	ToolChoice any
}

// AdapterConfig 适配器配置（鉴权、变体与调用方附加 headers）。
type AdapterConfig struct {
	Protocol string
	Variant  string
	APIKey   string
	Endpoint string
	Model    string
	Headers  map[string]string
}

// StreamCallbacks 统一流式增量回调（参考实现同名类型的子集：itsm 暂无图片事件）。
type StreamCallbacks struct {
	OnText      func(string)
	OnReasoning func(string)
}

// EmitText 下发正文增量（空串或未注册回调时忽略）。
func (c StreamCallbacks) EmitText(text string) {
	if text == "" || c.OnText == nil {
		return
	}
	c.OnText(text)
}

// EmitReasoning 下发推理链增量。
func (c StreamCallbacks) EmitReasoning(reasoning string) {
	if reasoning == "" || c.OnReasoning == nil {
		return
	}
	c.OnReasoning(reasoning)
}

// ProcessResult 响应归一化结果（正文/推理链/工具调用/finish_reason）。
type ProcessResult struct {
	Content      string
	Reasoning    string
	ToolCalls    []ToolCall
	FinishReason string
}

// HasToolCalls 报告响应中是否包含工具调用。
func (r ProcessResult) HasToolCalls() bool { return len(r.ToolCalls) > 0 }

// ProtocolAdapter 协议适配器接口（形状对齐参考实现，见包注释）。
type ProtocolAdapter interface {
	// Name 返回适配器名称（协议枚举值）。
	Name() string
	// BuildRequest 构建请求体（JSON 可序列化的 map）。
	BuildRequest(cfg RequestConfig) map[string]any
	// BuildHeaders 构建请求头（含鉴权与调用方附加 headers）。
	BuildHeaders(cfg AdapterConfig) map[string]string
	// HandleResponse 解析响应：isStream 为 true 时按 SSE 解析并实时回调，
	// 返回归一化结果；解析失败或上游错误事件返回 *ProtocolError。
	HandleResponse(isStream bool, respBody io.Reader, callbacks StreamCallbacks) (ProcessResult, error)
	// ProcessResponse 从非流式响应体中提取归一化结果。
	ProcessResponse(result map[string]any) ProcessResult
	// IsReasoningModel 判断模型是否属于推理模型（影响请求参数与展示）。
	IsReasoningModel(model string) bool
	// GetAPIPath 返回默认 API 路径（相对 endpoint）。
	GetAPIPath() string
}

// P0 首适配器必须满足接口（BE-9）。
var _ ProtocolAdapter = (*OpenAIChatAdapter)(nil)

// PA-1/PA-2/PA-3 适配器必须满足接口（一协议一实现）。
var (
	_ ProtocolAdapter = (*AnthropicMessagesAdapter)(nil)
	_ ProtocolAdapter = (*OpenAIResponsesAdapter)(nil)
)
