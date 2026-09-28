package service

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"strconv"
	"strings"
	"sync/atomic"
)

// MockLLMEnabledEnv 是 mock LLM provider 的**显式启用**开关（M2-05「E2E 环境可启动 mock provider」）。
//
// 安全约束：mock provider 是测试替身，绝不允许在生产隐式生效——因此它需要
// `llm.provider=mock` **且** `LLM_MOCK_ENABLED=true` 两个条件同时满足（见 NewProviderFromConfig）。
const MockLLMEnabledEnv = "LLM_MOCK_ENABLED"

// 默认行为（可用环境变量覆盖，便于 E2E 精确断言）：
//
//	LLM_MOCK_REPLY      固定回复文本
//	LLM_MOCK_TRIGGER    触发词（出现在对话内容中即发起一次工具调用）
//	LLM_MOCK_TOOL_NAME  指定要调用的工具名（留空 → 声明列表中的第一个）
//	LLM_MOCK_TOOL_ARGS  工具参数（JSON 字符串）
const (
	MockLLMReplyEnv    = "LLM_MOCK_REPLY"
	MockLLMTriggerEnv  = "LLM_MOCK_TRIGGER"
	MockLLMToolNameEnv = "LLM_MOCK_TOOL_NAME"
	MockLLMToolArgsEnv = "LLM_MOCK_TOOL_ARGS"

	// DefaultMockLLMTrigger 是默认触发词：E2E 提示词里带上它即稳定触发工具调用。
	DefaultMockLLMTrigger = "__tool__"
	// DefaultMockLLMReply 是默认回复文本（前端可用它断言流式输出完成）。
	DefaultMockLLMReply = "（mock LLM）这是 E2E 替身模型的确定性回复。"
)

// MockProviderOptions 是 MockProvider 的确定性行为参数。
type MockProviderOptions struct {
	// Reply 固定回复文本（流式时按 ChunkSize 切片下发）。
	Reply string
	// Trigger 触发词：出现在任一消息内容中时发起一次工具调用；留空表示不触发。
	Trigger string
	// ToolName 指定被调用的工具名；留空用声明列表中的第一个。
	ToolName string
	// ToolArgs 工具参数（JSON 字符串）；非法 JSON 会被规整为 "{}"。
	ToolArgs string
	// ChunkSize 流式切片大小（字符数）；<=0 时取 24。
	ChunkSize int
}

// MockProviderOptionsFromEnv 从环境变量装配参数（未设置项取默认值）。
func MockProviderOptionsFromEnv() MockProviderOptions {
	options := MockProviderOptions{
		Reply:    strings.TrimSpace(os.Getenv(MockLLMReplyEnv)),
		Trigger:  os.Getenv(MockLLMTriggerEnv),
		ToolName: strings.TrimSpace(os.Getenv(MockLLMToolNameEnv)),
		ToolArgs: strings.TrimSpace(os.Getenv(MockLLMToolArgsEnv)),
	}
	if options.Reply == "" {
		options.Reply = DefaultMockLLMReply
	}
	if options.Trigger == "" {
		options.Trigger = DefaultMockLLMTrigger
	}
	if options.ToolArgs == "" {
		options.ToolArgs = "{}"
	}
	if !json.Valid([]byte(options.ToolArgs)) {
		options.ToolArgs = "{}"
	}
	return options
}

// MockLLMEnabled 判定显式开关是否打开（大小写不敏感；仅 "true"/"1" 视为开启）。
func MockLLMEnabled() bool {
	switch strings.ToLower(strings.TrimSpace(os.Getenv(MockLLMEnabledEnv))) {
	case "true", "1":
		return true
	default:
		return false
	}
}

// MockProvider 是 E2E / 本地联调用的**确定性** LLM 替身（实现 LLMProvider /
// StreamingLLMProvider / ToolCallingStreamProvider 三件套）。
//
// 用途：让「对话 → 工具调用 → 审批 → 执行 → 时间线/审计」链路在没有真实模型额度时
// 也能端到端驱动（A2-05 的浏览器全链路需要它）。行为完全由选项决定、无随机数、无外部调用，
// 因此可断言、可重放。
type MockProvider struct {
	options MockProviderOptions
	// callSeq 仅用于生成稳定的调用 ID（mock-call-1、mock-call-2…）。
	callSeq atomic.Int64
}

// NewMockProvider 构造 mock provider（options 零值字段回退默认值）。
func NewMockProvider(options MockProviderOptions) *MockProvider {
	if options.Reply == "" {
		options.Reply = DefaultMockLLMReply
	}
	if options.Trigger == "" {
		options.Trigger = DefaultMockLLMTrigger
	}
	if options.ToolArgs == "" || !json.Valid([]byte(options.ToolArgs)) {
		options.ToolArgs = "{}"
	}
	if options.ChunkSize <= 0 {
		options.ChunkSize = 24
	}
	return &MockProvider{options: options}
}

// Name 返回 provider 标识（用于日志/诊断，不进入审计枚举）。
func (p *MockProvider) Name() string { return "mock" }

// Chat 单次返回固定回复（不触发工具调用——工具声明只出现在 ChatStreamWithTools 路径）。
func (p *MockProvider) Chat(_ context.Context, _ string, _ []LLMMessage) (string, error) {
	return p.options.Reply, nil
}

// ChatStream 按切片流式下发固定回复，模拟 token 增量。
func (p *MockProvider) ChatStream(_ context.Context, _ string, _ []LLMMessage, callback func(string)) error {
	if callback == nil {
		return errors.New("mock provider: callback 不能为空")
	}
	runes := []rune(p.options.Reply)
	for start := 0; start < len(runes); start += p.options.ChunkSize {
		end := start + p.options.ChunkSize
		if end > len(runes) {
			end = len(runes)
		}
		callback(string(runes[start:end]))
	}
	return nil
}

// ChatStreamWithTools 流式下发回复；当「声明了工具」且「内容命中触发词」时，
// 额外通过 onToolCalls 发起一次确定性工具调用。
//
// 触发规则（与 E2E 约定一致）：
//   - tools 为空 → 永不触发（保证纯聊天场景不受影响）；
//   - 触发词在**最后一条 user 消息**或任一消息内容中出现 → 触发；
//   - 被调用工具 = ToolName（若在声明列表中）→ 否则声明列表第一个；
//   - 参数 = ToolArgs（构造时已保证是合法 JSON）。
func (p *MockProvider) ChatStreamWithTools(
	_ context.Context,
	_ string,
	messages []LLMMessage,
	tools []LLMTool,
	callback func(string),
	onToolCalls func([]LLMToolCall),
) error {
	if callback != nil {
		if err := p.ChatStream(context.Background(), "", messages, callback); err != nil {
			return err
		}
	}

	declared := declaredToolNames(tools, messages)
	if len(declared) == 0 || onToolCalls == nil || p.options.Trigger == "" {
		return nil
	}
	if !mockTriggerHit(p.options.Trigger, messages) {
		return nil
	}

	name := p.options.ToolName
	if name == "" || !containsString(declared, name) {
		name = declared[0]
	}
	onToolCalls([]LLMToolCall{{
		ID:        "mock-call-" + strconv.FormatInt(p.callSeq.Add(1), 10),
		Name:      name,
		Arguments: p.options.ToolArgs,
	}})
	return nil
}

// declaredToolNames 汇总可用工具名：优先请求侧 tools，为空时回落到消息上挂载的 Tools（去重、保序）。
func declaredToolNames(tools []LLMTool, messages []LLMMessage) []string {
	names := make([]string, 0, len(tools))
	appendName := func(name string) {
		name = strings.TrimSpace(name)
		if name != "" && !containsString(names, name) {
			names = append(names, name)
		}
	}
	for _, tool := range tools {
		appendName(tool.Name)
	}
	if len(names) == 0 {
		for _, message := range messages {
			for _, tool := range message.Tools {
				appendName(tool.Name)
			}
		}
	}
	return names
}

// mockTriggerHit 判定触发词是否命中（最后一条 user 消息优先，其次任一消息）。
func mockTriggerHit(trigger string, messages []LLMMessage) bool {
	for index := len(messages) - 1; index >= 0; index-- {
		if messages[index].Role == "user" && strings.Contains(messages[index].Content, trigger) {
			return true
		}
	}
	for _, message := range messages {
		if strings.Contains(message.Content, trigger) {
			return true
		}
	}
	return false
}

func containsString(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}
