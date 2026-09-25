package service

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	openai "github.com/sashabaranov/go-openai"
	"github.com/spf13/viper"
	"go.uber.org/zap"

	"itsm-backend/common"
	"itsm-backend/ent"
	"itsm-backend/ent/llmproviderconfig"
	"itsm-backend/ent/llmuserpreference"
	"itsm-backend/internal/llm/protocol"
)

// 本文件是主计划《多 LLM Provider 支持与可切换方案》v1.4 BE-9 的接线层：
// Registry 构建路径分派（适配器优先、旧分支回退，开关控制，默认关）。
//
// 承载范围（一协议一实现，见 internal/llm/protocol/registry.go）：
//   - openai_chat_completions：默认 / azure / ollama 三个变体共用同一适配器，
//     变体差异体现在 endpoint（azure 见 normalizeVariantEndpoint）与调用参数上；
//   - anthropic_messages：官方 / minimax 两个变体共用同一适配器（minimax 的
//     camelCase 字段口径在构造期固化）；
//   - openai_responses：标准形态共用同一适配器（PA-3）；
//   - google_gemini：标准形态共用同一适配器（PA-4，资源路径含模型名，
//     endpoint 缺省回退官方地址 https://generativelanguage.googleapis.com）；
//     4 值枚举在 PA-4 收官后均有适配器承载：动态路径经 protocol.ModelPathAdapter 在请求期
//     解析；AI_PROTOCOL_NOT_IMPLEMENTED(422) 仅剩"协议未注册或变体不在白名单"两种来源；
//   - 开关关闭（默认）时静态构建路径完全不经过本文件，旧路径零变化（QA-3 回归门禁）。
//
// BE-2/BE-4 落地 DB 实例后，协议与变体由记录直接给出，调用 NewProtocolProvider 即可；
// 未注册时按 ErrProtocolNotImplemented 映射 422 AI_PROTOCOL_NOT_IMPLEMENTED。

const (
	// llmProtocolAdapterEnabledKey 部署级开关（config.yaml，默认 false）。
	llmProtocolAdapterEnabledKey = "llm.protocol_adapter_enabled"
	// LLMProtocolAdapterEnabledEnv 环境变量覆盖（优先级高于配置文件，便于升级部署不改进文件即开）。
	LLMProtocolAdapterEnabledEnv = "LLM_PROTOCOL_ADAPTER_ENABLED"

	// protocolProviderDefaultMaxTokens / protocolProviderDefaultTemperature 与既有
	// OpenAIProvider 的线上取值保持一致（NewOpenAIProvider 的 maxTokens 与 Chat 的 temperature）。
	protocolProviderDefaultMaxTokens   = 4096
	protocolProviderDefaultTemperature = 0.3

	// protocolMiniMaxDefaultTemperature 旧 MiniMaxProvider 的线上取值（硬编码 temperature 1.0）：
	// anthropic_messages/minimax 变体由适配器承载后必须保持逐字段一致（方案 v1.0 PA-1）。
	protocolMiniMaxDefaultTemperature = 1.0

	// protocolOpenAICompatibleDefaultEndpoint 与 go-openai 客户端默认 BaseURL 同源。
	protocolOpenAICompatibleDefaultEndpoint = "https://api.openai.com"
)

// ErrProtocolNotImplemented 表示 (protocol, variant) 未注册适配器（主计划 §3.4 错误码
// AI_PROTOCOL_NOT_IMPLEMENTED，管理 API 映射 422、前端选项置灰）。
// 同时通过 Unwrap 链保留 protocol.ErrAdapterNotFound，便于按层断言。
var ErrProtocolNotImplemented = errors.New("AI_PROTOCOL_NOT_IMPLEMENTED")

// protocolAdapters P0 默认协议注册表：启动期构建、运行期只读（并发安全见 internal/llm/protocol）。
var protocolAdapters = protocol.NewDefaultRegistry()

// DefaultProtocolRegistry 返回 P0 默认协议注册表（后续 BE-2/BE-4 与测试复用）。
func DefaultProtocolRegistry() *protocol.Registry { return protocolAdapters }

// protocolAdapterEnabled 读取部署级开关（默认关）：
// 环境变量 LLM_PROTOCOL_ADAPTER_ENABLED 优先，其次 config.yaml llm.protocol_adapter_enabled；
// 取值非法或未配置一律按关闭处理（零破坏优先）。
func protocolAdapterEnabled() bool {
	if raw := strings.TrimSpace(os.Getenv(LLMProtocolAdapterEnabledEnv)); raw != "" {
		if value, err := strconv.ParseBool(raw); err == nil {
			return value
		}
		return false
	}
	return viper.GetBool(llmProtocolAdapterEnabledKey)
}

// staticProviderProtocolSpec 把静态配置的 provider 名映射到 (协议, 变体)。
//
// 该映射只服务静态回退路径（config.yaml），是主计划 §3.1.4 映射表的静态侧；
// DB 实例的协议/变体由记录字段直接给出（BE-8），不经过这里。
// 未识别的 provider 返回 ok=false：保持既有"默认 OpenAI + 不改行为"的兜底。
func staticProviderProtocolSpec(provider string) (protocolName, variant string, ok bool) {
	switch strings.ToLower(strings.TrimSpace(provider)) {
	case "", "openai":
		return protocol.ProtocolOpenAIChatCompletions, protocol.VariantDefault, true
	case "azure":
		return protocol.ProtocolOpenAIChatCompletions, protocol.VariantAzure, true
	case "local":
		return protocol.ProtocolOpenAIChatCompletions, protocol.VariantOllama, true
	case "minimax":
		return protocol.ProtocolAnthropicMessages, protocol.VariantMiniMax, true
	default:
		return "", "", false
	}
}

// ProtocolProviderOptions 构建协议适配器 provider 的参数。
type ProtocolProviderOptions struct {
	Protocol    string
	Variant     string
	APIKey      string
	Endpoint    string
	Model       string
	MaxTokens   int
	Temperature float64
	// Headers 调用方附加请求头（如追踪头），由适配器按大小写不敏感合并。
	Headers map[string]string
	// HTTPClient 为空时使用无超时的默认客户端（与 go-openai 客户端一致，超时由 ctx 控制，
	// 避免长流式响应被客户端级超时截断）。
	HTTPClient *http.Client
	// Registry 为空时使用 DefaultProtocolRegistry()。
	Registry *protocol.Registry
}

// NewProtocolProvider 按 (protocol, variant) 构建适配器承载的 provider。
// 未注册返回包装 ErrProtocolNotImplemented 的错误（管理 API 映射 422）。
func NewProtocolProvider(opts ProtocolProviderOptions) (LLMProvider, error) {
	registry := opts.Registry
	if registry == nil {
		registry = protocolAdapters
	}
	adapter, err := registry.NewAdapter(opts.Protocol, opts.Variant)
	if err != nil {
		return nil, fmt.Errorf("%w: protocol=%s variant=%s: %w", ErrProtocolNotImplemented, opts.Protocol, opts.Variant, err)
	}
	maxTokens := opts.MaxTokens
	if maxTokens <= 0 {
		maxTokens = protocolProviderDefaultMaxTokens
	}
	temperature := opts.Temperature
	if temperature == 0 {
		temperature = protocolProviderVariantTemperature(opts.Protocol, opts.Variant)
	}
	client := opts.HTTPClient
	if client == nil {
		client = &http.Client{}
	}
	return &protocolProvider{
		adapter:     adapter,
		variant:     opts.Variant,
		client:      client,
		apiKey:      opts.APIKey,
		endpoint:    normalizeVariantEndpoint(opts.Endpoint, opts.Protocol, opts.Variant),
		model:       opts.Model,
		maxTokens:   maxTokens,
		temperature: temperature,
		headers:     opts.Headers,
	}, nil
}

// protocolProviderVariantTemperature 返回 (协议, 变体) 的默认 temperature（调用方未显式给出时）。
// minimax 变体沿用旧 MiniMaxProvider 的 1.0，其余沿用 BE-9 的 0.3（零破坏口径）。
func protocolProviderVariantTemperature(protocolName, variant string) float64 {
	if strings.EqualFold(strings.TrimSpace(protocolName), protocol.ProtocolAnthropicMessages) &&
		strings.EqualFold(strings.TrimSpace(variant), protocol.VariantMiniMax) {
		return protocolMiniMaxDefaultTemperature
	}
	return protocolProviderDefaultTemperature
}

// newProtocolProviderFromConfig 尝试按静态配置走适配器路径。
//
// 返回 nil 表示回退既有分支（开关关闭、provider 未识别）——静态路径不引入新的失败面。
// 静态配置无协议/变体字段，其 provider 映射见 staticProviderProtocolSpec；
// DB 实例路径（BE-4/BE-8）才可能出现协议/变体组合，由那里映射 422。
func newProtocolProviderFromConfig(cfg ProviderConfig, apiKey string) LLMProvider {
	if !cfg.ProtocolAdapterEnabled {
		return nil
	}
	protocolName, variant, ok := staticProviderProtocolSpec(cfg.Provider)
	if !ok {
		return nil
	}
	provider, err := NewProtocolProvider(ProtocolProviderOptions{
		Protocol:  protocolName,
		Variant:   variant,
		APIKey:    apiKey,
		Endpoint:  cfg.Endpoint,
		Model:     llmProviderModelForVariant(protocolName, variant, cfg.Model, cfg.Deployment),
		MaxTokens: protocolProviderDefaultMaxTokens,
	})
	if err != nil {
		return nil
	}
	return provider
}

// protocolProvider 用 ProtocolAdapter 承载 LLMProvider、StreamingLLMProvider 与
// ToolCallingStreamProvider 三种能力（BE-9 接线）。
//
// 等价基线：同包的 OpenAIProvider（go-openai 客户端）。请求体字段（含 omitempty 语义）、
// 流式回调顺序、工具调用分片累积与错误分类（网关重试口径）由对照测试逐项锁定。
// 与既有实现的两处有意差异（均不影响普通非推理模型的线上行为）：
//  1. model 覆盖不写回实例（局部生效），避免共享实例上的并发写；
//  2. 推理模型（gpt-5*/o1/o3/o4/o5/codex）不下发 temperature（参考实现口径）。
type protocolProvider struct {
	adapter     protocol.ProtocolAdapter
	variant     string
	client      *http.Client
	apiKey      string
	endpoint    string
	model       string
	maxTokens   int
	temperature float64
	headers     map[string]string
}

var (
	_ LLMProvider               = (*protocolProvider)(nil)
	_ StreamingLLMProvider      = (*protocolProvider)(nil)
	_ ToolCallingStreamProvider = (*protocolProvider)(nil)
)

// Chat 非流式调用（语义等价 OpenAIProvider.Chat）。
func (p *protocolProvider) Chat(ctx context.Context, model string, messages []LLMMessage) (string, error) {
	result, err := p.call(ctx, model, messages, nil, false, protocol.StreamCallbacks{})
	if err != nil {
		return "", err
	}
	return result.Content, nil
}

// ChatStream 流式调用（语义等价 OpenAIProvider.ChatStream）。
func (p *protocolProvider) ChatStream(ctx context.Context, model string, messages []LLMMessage, callback func(string)) error {
	if callback == nil {
		callback = func(string) {}
	}
	_, err := p.call(ctx, model, messages, nil, true, protocol.StreamCallbacks{OnText: callback})
	return err
}

// ChatStreamWithTools 声明工具并流式调用（语义等价 OpenAIProvider.ChatStreamWithTools）：
// 文本增量经 callback 下发，工具调用在流结束后一次性返回。
func (p *protocolProvider) ChatStreamWithTools(ctx context.Context, model string, messages []LLMMessage, tools []LLMTool, callback func(string), onToolCalls func([]LLMToolCall)) error {
	if callback == nil {
		callback = func(string) {}
	}
	if onToolCalls == nil {
		onToolCalls = func([]LLMToolCall) {}
	}
	result, err := p.call(ctx, model, messages, tools, true, protocol.StreamCallbacks{OnText: callback})
	if err != nil {
		return err
	}
	if len(result.ToolCalls) > 0 {
		onToolCalls(toLLMToolCalls(result.ToolCalls))
	}
	return nil
}

// call 执行一次协议调用：构建请求 → HTTP → 归一化响应。
func (p *protocolProvider) call(
	ctx context.Context,
	model string,
	messages []LLMMessage,
	tools []LLMTool,
	stream bool,
	callbacks protocol.StreamCallbacks,
) (protocol.ProcessResult, error) {
	resolvedModel := p.model
	if strings.TrimSpace(model) != "" {
		resolvedModel = model
	}

	requestBody, err := json.Marshal(p.adapter.BuildRequest(protocol.RequestConfig{
		Model:          resolvedModel,
		Messages:       toProtocolMessages(messages),
		Stream:         stream,
		MaxTokens:      p.maxTokens,
		Temperature:    p.temperature,
		ReasoningModel: p.adapter.IsReasoningModel(resolvedModel),
		Tools:          toProtocolTools(tools),
	}))
	if err != nil {
		return protocol.ProcessResult{}, fmt.Errorf("%s: encode request: %w", p.adapter.Name(), err)
	}

	// PA-4：google_gemini 的资源路径含模型名与操作（流式带 ?alt=sse），
	// 由 ModelPathAdapter 在请求期解析；其余协议维持静态 GetAPIPath。
	apiPath := p.adapter.GetAPIPath()
	if modelPathAdapter, ok := p.adapter.(protocol.ModelPathAdapter); ok {
		apiPath = modelPathAdapter.APIPathFor(resolvedModel, stream)
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, resolveProtocolURL(p.endpoint, apiPath, p.defaultEndpoint()), bytes.NewReader(requestBody))
	if err != nil {
		return protocol.ProcessResult{}, fmt.Errorf("%s: create request: %w", p.adapter.Name(), err)
	}
	for key, value := range p.adapter.BuildHeaders(protocol.AdapterConfig{
		Protocol: p.adapter.Name(),
		APIKey:   p.apiKey,
		Endpoint: p.endpoint,
		Model:    resolvedModel,
		Headers:  p.headers,
	}) {
		request.Header.Set(key, value)
	}

	response, err := p.client.Do(request)
	if err != nil {
		// 网络层错误（url.Error 包装连接拒绝/超时等）保持原样透出：
		// 网关 isTransientLLMError 按 url.Error / 关键词分类，与既有实现一致。
		return protocol.ProcessResult{}, err
	}
	defer response.Body.Close()

	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		responseBody, _ := io.ReadAll(response.Body)
		errorPrefix := "OpenAI API error"
		if stream {
			errorPrefix = "OpenAI stream error"
		}
		return protocol.ProcessResult{}, p.wrapHTTPError(errorPrefix, response, responseBody)
	}
	return p.adapter.HandleResponse(stream, response.Body, callbacks)
}

// wrapHTTPError 把非 2xx 响应映射为双身份错误：*openai.APIError（网关重试分类依赖，
// 与既有 go-openai 客户端同形状）+ *protocol.ProtocolError（协议层明细），
// 文案前缀与既有 OpenAIProvider 完全一致。
func (p *protocolProvider) wrapHTTPError(prefix string, response *http.Response, body []byte) error {
	protocolErr := protocol.NewHTTPError(p.adapter.Name(), response, body)
	apiErr := &openai.APIError{
		Code:           protocolErr.Code,
		Message:        protocolErr.Message,
		HTTPStatusCode: protocolErr.StatusCode,
	}
	if response != nil {
		apiErr.HTTPStatus = response.Status
	}
	return &protocolProviderError{prefix: prefix, apiError: apiErr, protocolError: protocolErr}
}

// protocolProviderError 同时呈现既有实现与协议层两种错误身份。
type protocolProviderError struct {
	prefix        string
	apiError      *openai.APIError
	protocolError *protocol.ProtocolError
}

func (e *protocolProviderError) Error() string {
	return e.prefix + ": " + e.apiError.Error()
}

// Unwrap 暴露两条链：errors.As(*openai.APIError)（网关 isTransientLLMError）
// 与 errors.As(*protocol.ProtocolError)（协议层错误码/明细）。
func (e *protocolProviderError) Unwrap() []error {
	return []error{e.apiError, e.protocolError}
}

// defaultEndpoint 返回该 (协议, 变体) 的默认地址：协议包登记值优先（anthropic 官方 / minimax
// 兼容端点、ollama 本机地址），未登记回退 BE-9 的 OpenAI 兼容默认地址（保持既有行为）。
func (p *protocolProvider) defaultEndpoint() string {
	if endpoint := protocol.DefaultEndpoint(p.adapter.Name(), p.variant); endpoint != "" {
		return endpoint
	}
	return protocolOpenAICompatibleDefaultEndpoint
}

// resolveProtocolURL 拼接 endpoint 与适配器路径。
//
// 口径（与既有 config.yaml 写法对齐）：endpoint 为空回退默认地址；endpoint 已带版本段
// （如 https://api.deepseek.com/v1）时只补路径余部，保证与既有 go-openai 客户端
// （BaseURL + /chat/completions）逐字节一致；endpoint 已含完整协议路径时原样使用。
func resolveProtocolURL(endpoint, apiPath, fallbackEndpoint string) string {
	base := strings.TrimRight(strings.TrimSpace(endpoint), "/")
	if base == "" {
		base = fallbackEndpoint
	}
	if base == "" {
		base = protocolOpenAICompatibleDefaultEndpoint
	}
	if apiPath == "" {
		return base
	}
	if strings.HasSuffix(strings.ToLower(base), strings.ToLower(apiPath)) {
		return base
	}
	// endpoint 已带版本段（/v1、/v1beta…）：只补资源段，避免拼成 /v1/v1/...
	segments := strings.Split(strings.TrimPrefix(apiPath, "/"), "/")
	if len(segments) > 1 {
		versionPrefix := "/" + segments[0]
		if strings.HasSuffix(strings.ToLower(base), strings.ToLower(versionPrefix)) {
			return base + "/" + strings.Join(segments[1:], "/")
		}
	}
	return base + apiPath
}

// normalizeVariantEndpoint 归一部署变体的 endpoint 口径（与旧分支逐字节一致）：
//
//   - azure：旧 AzureProvider 固定以 `{endpoint}/openai/v1` 为 BaseURL（go-openai 再补
//     `/chat/completions`）。因此 endpoint 只给主机名（无路径）时补 `/openai`，再交给
//     `resolveProtocolURL` 的版本段规则拼出 `/openai/v1/chat/completions`，与旧分支同址；
//     endpoint 已带路径（如 `/openai`、`/openai/v1`）或为空（旧分支回退 go-openai 默认
//     BaseURL）时原样透传；
//   - 其余协议/变体：原样透传（ollama 的 `/v1/chat/completions` 由同一拼接规则得到）。
func normalizeVariantEndpoint(endpoint, protocolName, variant string) string {
	if !strings.EqualFold(strings.TrimSpace(protocolName), protocol.ProtocolOpenAIChatCompletions) ||
		!strings.EqualFold(strings.TrimSpace(variant), protocol.VariantAzure) {
		return endpoint
	}
	trimmed := strings.TrimSpace(endpoint)
	if trimmed == "" {
		return endpoint
	}
	parsed, err := url.Parse(trimmed)
	if err != nil || parsed.Host == "" {
		return endpoint
	}
	if strings.Trim(parsed.Path, "/") != "" {
		return endpoint
	}
	parsed.Path = "/openai"
	return strings.TrimRight(parsed.String(), "/")
}

// llmProviderModelForVariant 解析实例的默认模型：azure 变体在 model 缺省时回退
// deployment（旧 AzureProvider 的 `actualModel := deploymentID` 口径），其余变体直接用 model。
func llmProviderModelForVariant(protocolName, variant, model, deployment string) string {
	if strings.EqualFold(strings.TrimSpace(protocolName), protocol.ProtocolOpenAIChatCompletions) &&
		strings.EqualFold(strings.TrimSpace(variant), protocol.VariantAzure) &&
		strings.TrimSpace(model) == "" {
		return strings.TrimSpace(deployment)
	}
	return model
}

// toProtocolMessages 把 service.LLMMessage 转换为协议层消息（字段一一对应）。
func toProtocolMessages(messages []LLMMessage) []protocol.Message {
	out := make([]protocol.Message, 0, len(messages))
	for _, message := range messages {
		item := protocol.Message{
			Role:       message.Role,
			Content:    message.Content,
			ToolCallID: message.ToolCallID,
		}
		if len(message.ToolCalls) > 0 {
			item.ToolCalls = make([]protocol.ToolCall, 0, len(message.ToolCalls))
			for _, call := range message.ToolCalls {
				item.ToolCalls = append(item.ToolCalls, protocol.ToolCall{
					ID:        call.ID,
					Name:      call.Name,
					Arguments: call.Arguments,
				})
			}
		}
		if len(message.Tools) > 0 {
			item.Tools = toProtocolTools(message.Tools)
		}
		out = append(out, item)
	}
	return out
}

// toProtocolTools 把 service.LLMTool 转换为协议层工具声明。
func toProtocolTools(tools []LLMTool) []protocol.Tool {
	if len(tools) == 0 {
		return nil
	}
	out := make([]protocol.Tool, 0, len(tools))
	for _, tool := range tools {
		out = append(out, protocol.Tool{
			Name:        tool.Name,
			Description: tool.Description,
			Parameters:  tool.Parameters,
		})
	}
	return out
}

// toLLMToolCalls 把协议层工具调用还原为 service 契约。
func toLLMToolCalls(calls []protocol.ToolCall) []LLMToolCall {
	out := make([]LLMToolCall, 0, len(calls))
	for _, call := range calls {
		out = append(out, LLMToolCall{
			ID:        call.ID,
			Name:      call.Name,
			Arguments: call.Arguments,
		})
	}
	return out
}

// ============================================================================
// BE-2：LLMProviderRegistry（租户快照加载 / 缓存 / 失效 / 解密 / 构建 / 降级）
// ============================================================================
//
// 主计划《多 LLM Provider 支持与可切换方案》§3.2/§3.3 的服务内核轨交付物：
//
//	请求级 override（严格命中，不回退）→ 个人默认（user）→ 租户默认（tenant）→ 静态配置（static）
//
// 边界：本文件只做"解析 + 构建"，HTTP 状态映射（§3.4）与响应回带（provider/providerSource）
// 由 BE-3/BE-4 承担；调用方一律用 errors.Is 判断哨兵错误（错误统一以 %w 包装以携带 provider key）。
//
// 并发与安全约定：
//   - cache 由 mu(RWMutex) 保护；租户快照构建后不可变，Resolve 在读锁内取出指针后立即释放锁，
//     槽位读取无共享写（§3.2 第 7 条：Resolve 可被多 goroutine 并发调用）；
//   - override 未命中快照时做一次定向查询（区分 NOT_FOUND / DISABLED，并覆盖快照加载后新建的
//     实例），不重新加载整租户快照，避免热路径写共享快照；
//   - 密钥只以密文形态存在于 DB：日志仅输出 name 与 common.MaskSecret 脱敏值（§3.5），
//     错误信息与日志绝不回带明文或密文；
//   - 所有 ent 查询强制 tenant_id 过滤（§3.2 第 5 条：租户隔离，跨租户引用视为 NOT_FOUND）。
const llmRegistryCacheTTL = 30 * time.Second

// providerSource 取值（§3.3 解析链来源标注，BE-3/BE-7 回带 providerSource）。
const (
	// ProviderSourceRequest 请求级显式选择（override 非空）。
	ProviderSourceRequest = "request"
	// ProviderSourceUser 个人默认（llm_user_preferences.provider_key 命中同租户启用实例）。
	ProviderSourceUser = "user"
	// ProviderSourceTenant 租户默认（is_default=true 且 enabled 且未软删）。
	ProviderSourceTenant = "tenant"
	// ProviderSourceStatic config.yaml / env 静态回退（现状行为）。
	ProviderSourceStatic = "static"
)

// 解析哨兵错误（§3.4 错误码；HTTP 状态映射：404/409/422/503，由 BE-3/BE-4 承担）。
var (
	// ErrProviderNotFound 本租户内不存在该 key（或引用已被软删/跨租户）。
	ErrProviderNotFound = errors.New("AI_PROVIDER_NOT_FOUND")
	// ErrProviderDisabled 实例存在但被禁用（显式选择时不回退，可见地失败）。
	ErrProviderDisabled = errors.New("AI_PROVIDER_DISABLED")
	// ErrProviderKeyMissing 实例启用但密钥缺失或解密失败（该 slot 不可用，不影响其它 slot）。
	ErrProviderKeyMissing = errors.New("AI_PROVIDER_KEY_MISSING")
	// ErrProviderUnavailable 实例/静态配置不可用：协议未实现、变体非法、适配器缺失、静态配置为空。
	ErrProviderUnavailable = errors.New("AI_PROVIDER_UNAVAILABLE")
)

// SecretDecrypter 解密实例的 encrypted_api_key（*middleware.EncryptionService 天然满足）。
//
// 收敛为窄接口：便于测试注入 broken decrypter（不 panic、标记 slot 不可用），
// 也避免 registry 直接依赖整个加密服务实现。
type SecretDecrypter interface {
	Decrypt(ciphertext string) (string, error)
}

// ProviderSlot 一次解析得到的可调用槽位（§3.2 第 1 条）。
type ProviderSlot struct {
	// Key 实例 key；静态回退为静态 provider 名（provider 为空时用 "static"）。
	Key string
	// Protocol 协议枚举（§3.1.4 四值；静态回退按 staticProviderProtocolSpec 归一）。
	Protocol string
	// Variant 兼容变体（空串 = 标准实现）。
	Variant string
	// Model 传给 provider 的模型名。
	Model string
	// Provider 已构建的 provider；非 nil 表示可用。
	Provider LLMProvider
	// SupportsTools 是否实现 ToolCallingStreamProvider（能力位按真实实现填写，不查表）。
	SupportsTools bool
}

// LLMProviderRegistry 多 Provider 解析器（BE-2）。
//
// 生命周期：bootstrap 期构造并注入 LLMGateway（BE-3/BE-5）；运行期并发调用 Resolve。
// 管理写操作（创建/更新/删除/启停/设默认）成功后调用 Invalidate(tenantID) 立即失效；
// 多副本部署不做跨进程失效，靠 TTL 30s 收敛（D5）。
type LLMProviderRegistry struct {
	client    *ent.Client
	decrypter SecretDecrypter
	staticCfg ProviderConfig
	logger    *zap.SugaredLogger

	mu    sync.RWMutex
	cache map[int]*llmTenantSnapshot

	// ttl / now 为测试注入点（未导出）：生产用 llmRegistryCacheTTL + time.Now。
	ttl time.Duration
	now func() time.Time
}

// NewLLMProviderRegistry 构造注册表。
//
//	client    ent 客户端（nil 时视为"无 DB 实例"，Resolve 空 override 直接走静态回退）
//	decrypter 密钥解密服务（nil 时 DB 实例的密钥一律按缺失处理）
//	staticCfg 静态配置（LoadLLMConfig() 结果，作最终回退）
//	logger    服务层日志（可为 nil；密钥始终脱敏）
func NewLLMProviderRegistry(client *ent.Client, decrypter SecretDecrypter, staticCfg ProviderConfig, logger *zap.SugaredLogger) *LLMProviderRegistry {
	return &LLMProviderRegistry{
		client:    client,
		decrypter: decrypter,
		staticCfg: staticCfg,
		logger:    logger,
		cache:     make(map[int]*llmTenantSnapshot),
		ttl:       llmRegistryCacheTTL,
		now:       time.Now,
	}
}

// Resolve 解析生效 provider（§3.3）。
//
// override 非空（请求级显式选择）：必须是本租户 enabled 且未软删的实例，一律不回退——
// 不存在 → ErrProviderNotFound；禁用 → ErrProviderDisabled；密钥缺失/解密失败 → ErrProviderKeyMissing；
// 协议未实现/变体非法 → ErrProviderUnavailable。成功时 source = ProviderSourceRequest。
//
// override 为空：租户默认（is_default=true 且 enabled 且未软删）→ source = ProviderSourceTenant；
// 无租户默认 → 静态配置（NewProviderFromConfig(staticCfg)）→ source = ProviderSourceStatic；
// 静态配置为空 → ErrProviderUnavailable。
//
// 注意：租户默认存在但不可用（密钥缺失等）时返回该 slot 的错误，不在 registry 内静默降级到静态；
// 是否回落静态由 BE-3 按 §3.2 的网关语义决定。
func (r *LLMProviderRegistry) Resolve(ctx context.Context, tenantID int, override string) (ProviderSlot, string, error) {
	providerKey := strings.TrimSpace(override)
	if providerKey != "" {
		slot, err := r.resolveOverride(ctx, tenantID, providerKey)
		if err != nil {
			return ProviderSlot{}, "", err
		}
		return slot, ProviderSourceRequest, nil
	}
	return r.resolveTenantDefault(ctx, tenantID)
}

// ResolveUserDefault 解析个人默认（§3.3 第二级）：
// llm_user_preferences.provider_key 命中同租户 enabled 且未软删实例时返回 source = ProviderSourceUser；
// 无偏好、provider_key 为空、指向软删/跨租户/禁用实例时返回 (zero, "", ErrProviderNotFound)
// （不自动改写个人偏好；BE-3 据此回落租户默认并在响应标注 providerSource=tenant）。
//
// 实例已启用但密钥缺失/协议未实现时透出对应错误（ErrProviderKeyMissing / ErrProviderUnavailable），
// 不做"视而不见"的降级。
func (r *LLMProviderRegistry) ResolveUserDefault(ctx context.Context, tenantID, userID int) (ProviderSlot, string, error) {
	if r.client == nil || tenantID <= 0 || userID <= 0 {
		return ProviderSlot{}, "", fmt.Errorf("%w: 用户 %d 无个人默认", ErrProviderNotFound, userID)
	}
	preference, err := r.client.LLMUserPreference.Query().
		Where(
			llmuserpreference.UserIDEQ(userID),
			llmuserpreference.TenantIDEQ(tenantID),
		).
		Only(ctx)
	if err != nil {
		if ent.IsNotFound(err) {
			return ProviderSlot{}, "", fmt.Errorf("%w: 用户 %d 无个人默认", ErrProviderNotFound, userID)
		}
		return ProviderSlot{}, "", fmt.Errorf("llm registry: query user %d llm preference: %w", userID, err)
	}
	providerKey := strings.TrimSpace(preference.ProviderKey)
	if providerKey == "" {
		return ProviderSlot{}, "", fmt.Errorf("%w: 用户 %d 未设置个人默认", ErrProviderNotFound, userID)
	}

	slot, err := r.resolveOverride(ctx, tenantID, providerKey)
	if err != nil {
		if errors.Is(err, ErrProviderNotFound) || errors.Is(err, ErrProviderDisabled) {
			// §3.1.2：实例被禁用/软删（或跨租户不可见）→ 读取路径降级，不自动改写个人偏好。
			safeLog(r.logger, "llm registry: user preference degraded to tenant default",
				"tenant_id", tenantID, "user_id", userID, "provider", providerKey, "reason", "preference_unavailable")
			return ProviderSlot{}, "", fmt.Errorf("%w: 用户偏好 %q 已失效", ErrProviderNotFound, providerKey)
		}
		return ProviderSlot{}, "", err
	}
	return slot, ProviderSourceUser, nil
}

// Invalidate 立即失效指定租户的快照（管理写操作成功后调用；多副本靠 TTL 收敛，§3.2 第 2 条）。
func (r *LLMProviderRegistry) Invalidate(tenantID int) {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.cache, tenantID)
}

// llmTenantSnapshot 一个租户的 provider 快照；构建完成后只读，供并发 Resolve 共享。
type llmTenantSnapshot struct {
	loadedAt time.Time
	// entries 覆盖本租户全部未软删实例（含禁用）：缓存 NOT_FOUND / DISABLED 判定所需的最小信息，
	// 避免每次 miss 都查库。
	entries map[string]*llmRegistryEntry
	// defaultKey 命中「is_default=true 且 enabled 且未软删」的实例 key；空串 = 无租户默认。
	defaultKey string
}

// llmRegistryEntry 快照中的一个实例。
type llmRegistryEntry struct {
	key           string
	protocol      string
	variant       string
	model         string
	enabled       bool
	provider      LLMProvider
	supportsTools bool
	// buildErr 非 nil = 实例已启用但不可用（ErrProviderKeyMissing / ErrProviderUnavailable），
	// 该 slot 的失败不影响同租户其它 slot，也不 panic。
	buildErr error
}

// slot 把快照条目转换为解析结果；禁用/不可用在此统一收敛为哨兵错误。
func (e *llmRegistryEntry) slot() (ProviderSlot, error) {
	if !e.enabled {
		return ProviderSlot{}, fmt.Errorf("%w: provider %q", ErrProviderDisabled, e.key)
	}
	if e.buildErr != nil {
		return ProviderSlot{}, e.buildErr
	}
	return ProviderSlot{
		Key:           e.key,
		Protocol:      e.protocol,
		Variant:       e.variant,
		Model:         e.model,
		Provider:      e.provider,
		SupportsTools: e.supportsTools,
	}, nil
}

// snapshot 返回租户快照：命中未过期缓存直接复用（读锁），否则加载并原子替换（写锁）。
//
// 加载在写锁内完成：同一租户的并发冷启动只查库一次；单租户查询很轻（tenant_id 索引），
// 换取实现简单与"多副本 TTL 收敛"语义的确定性。
func (r *LLMProviderRegistry) snapshot(ctx context.Context, tenantID int) (*llmTenantSnapshot, error) {
	if snap, ok := r.lookupFresh(tenantID, true); ok {
		return snap, nil
	}

	r.mu.Lock()
	defer r.mu.Unlock()
	if snap, ok := r.lookupFreshLocked(tenantID); ok {
		return snap, nil
	}
	snap, err := r.loadSnapshot(ctx, tenantID)
	if err != nil {
		return nil, err
	}
	r.cache[tenantID] = snap
	return snap, nil
}

// lookupFresh 读取未过期快照；readLock=false 表示调用方已持写锁（写锁同时排斥读锁）。
func (r *LLMProviderRegistry) lookupFresh(tenantID int, readLock bool) (*llmTenantSnapshot, bool) {
	if readLock {
		r.mu.RLock()
		defer r.mu.RUnlock()
	}
	return r.lookupFreshLocked(tenantID)
}

// lookupFreshLocked 读取未过期快照（调用方需持锁）。
func (r *LLMProviderRegistry) lookupFreshLocked(tenantID int) (*llmTenantSnapshot, bool) {
	snap, ok := r.cache[tenantID]
	if !ok || snap == nil {
		return nil, false
	}
	if r.expired(snap) {
		return nil, false
	}
	return snap, true
}

// expired 判断快照是否超过 TTL（ttl <= 0 时回退默认 TTL，避免误配成"永不过期"）。
func (r *LLMProviderRegistry) expired(snap *llmTenantSnapshot) bool {
	ttl := r.ttl
	if ttl <= 0 {
		ttl = llmRegistryCacheTTL
	}
	return r.nowTime().Sub(snap.loadedAt) >= ttl
}

func (r *LLMProviderRegistry) nowTime() time.Time {
	if r.now == nil {
		return time.Now()
	}
	return r.now()
}

// loadSnapshot 加载租户全量快照（强制 tenant_id + 未软删过滤）。
func (r *LLMProviderRegistry) loadSnapshot(ctx context.Context, tenantID int) (*llmTenantSnapshot, error) {
	snap := &llmTenantSnapshot{
		loadedAt: r.nowTime(),
		entries:  make(map[string]*llmRegistryEntry),
	}
	// nil client / 非法租户：视为"无 DB 实例"（静态回退仍可用），不 panic、不跨租户查询。
	if r.client == nil || tenantID <= 0 {
		return snap, nil
	}
	records, err := r.client.LLMProviderConfig.Query().
		Where(
			llmproviderconfig.TenantIDEQ(tenantID),
			llmproviderconfig.DeletedAtIsNil(),
		).
		All(ctx)
	if err != nil {
		return nil, fmt.Errorf("llm registry: load tenant %d provider snapshot: %w", tenantID, err)
	}
	for _, record := range records {
		entry := r.buildEntry(record)
		snap.entries[entry.key] = entry
		if entry.enabled && record.IsDefault && snap.defaultKey == "" {
			snap.defaultKey = entry.key
		}
	}
	return snap, nil
}

// resolveOverride 严格解析实例 key（请求级 override 与个人默认共用；命中失败不回退）。
func (r *LLMProviderRegistry) resolveOverride(ctx context.Context, tenantID int, providerKey string) (ProviderSlot, error) {
	snap, err := r.snapshot(ctx, tenantID)
	if err != nil {
		return ProviderSlot{}, err
	}
	if entry, ok := snap.entries[providerKey]; ok {
		return entry.slot()
	}

	// 快照未命中：一次定向查询区分 NOT_FOUND / DISABLED，同时覆盖"快照加载后新建"的实例。
	// （快照过期前不写回共享快照，避免热路径数据竞争；下一次快照加载即整体收敛。）
	if r.client == nil || tenantID <= 0 {
		return ProviderSlot{}, fmt.Errorf("%w: provider %q", ErrProviderNotFound, providerKey)
	}
	record, err := r.client.LLMProviderConfig.Query().
		Where(
			llmproviderconfig.TenantIDEQ(tenantID),
			llmproviderconfig.NameEQ(providerKey),
			llmproviderconfig.DeletedAtIsNil(),
		).
		Only(ctx)
	if err != nil {
		if ent.IsNotFound(err) {
			return ProviderSlot{}, fmt.Errorf("%w: provider %q", ErrProviderNotFound, providerKey)
		}
		return ProviderSlot{}, fmt.Errorf("llm registry: query provider %q: %w", providerKey, err)
	}
	return r.buildEntry(record).slot()
}

// resolveTenantDefault 解析租户默认；无默认时回退静态配置（§3.3 第三/四级）。
func (r *LLMProviderRegistry) resolveTenantDefault(ctx context.Context, tenantID int) (ProviderSlot, string, error) {
	snap, err := r.snapshot(ctx, tenantID)
	if err != nil {
		return ProviderSlot{}, "", err
	}
	if snap.defaultKey != "" {
		if entry, ok := snap.entries[snap.defaultKey]; ok {
			slot, slotErr := entry.slot()
			if slotErr != nil {
				return ProviderSlot{}, "", slotErr
			}
			return slot, ProviderSourceTenant, nil
		}
	}
	return r.resolveStatic()
}

// resolveStatic 静态配置回退（config.yaml / env，现状行为）。
func (r *LLMProviderRegistry) resolveStatic() (ProviderSlot, string, error) {
	if staticFallbackUnavailable(r.staticCfg) {
		return ProviderSlot{}, "", fmt.Errorf("%w: 静态 LLM 配置为空（provider/api_key/endpoint 均未配置）", ErrProviderUnavailable)
	}
	provider := NewProviderFromConfig(r.staticCfg)
	if provider == nil {
		return ProviderSlot{}, "", fmt.Errorf("%w: 静态 LLM 配置无法构建 provider", ErrProviderUnavailable)
	}
	protocolName, variant, ok := staticProviderProtocolSpec(r.staticCfg.Provider)
	if !ok {
		// 与 NewProviderFromConfig 的既有兜底一致：未识别 provider 名按 openai 处理。
		protocolName, variant = protocol.ProtocolOpenAIChatCompletions, protocol.VariantDefault
	}
	key := strings.TrimSpace(r.staticCfg.Provider)
	if key == "" {
		key = ProviderSourceStatic
	}
	return ProviderSlot{
		Key:           key,
		Protocol:      protocolName,
		Variant:       variant,
		Model:         r.staticCfg.Model,
		Provider:      provider,
		SupportsTools: supportsToolCalling(provider),
	}, ProviderSourceStatic, nil
}

// buildEntry 把一个 DB 实例转换为快照条目（禁用只记录状态；启用则解密 + 构建，失败仅标记该 slot）。
func (r *LLMProviderRegistry) buildEntry(record *ent.LLMProviderConfig) *llmRegistryEntry {
	entry := &llmRegistryEntry{
		key:      strings.TrimSpace(record.Name),
		protocol: NormalizeLLMProtocol(record.Protocol),
		variant:  NormalizeLLMVariant(record.Variant),
		model:    record.Model,
		enabled:  record.Enabled,
	}
	if !record.Enabled {
		// 禁用实例不构建、不解密：DevTools 不得因禁用实例的坏密钥而失败。
		return entry
	}

	// 1) 协议/变体槽位：未知协议或不在白名单的变体（BE-8 已在写入侧 422 拦截）
	//    若出现在 DB，按"该 slot 不可用"处理，不 panic（§3.2 第 4 条 / 主计划 §3.1.4）。
	if _, err := MapProtocolToLegacyProvider(entry.protocol, entry.variant); err != nil {
		entry.buildErr = fmt.Errorf("%w: provider %q protocol=%s variant=%s 未实现", ErrProviderUnavailable, entry.key, entry.protocol, entry.variant)
		r.warnUnavailable(entry, "", "protocol_unimplemented")
		return entry
	}

	// 2) 解密密钥：缺失/失败只影响本 slot（§3.2 第 4 条）。
	apiKey, err := r.decryptAPIKey(record)
	if err != nil {
		entry.buildErr = err
		r.warnUnavailable(entry, "", "key_unavailable")
		return entry
	}

	// 3) 构建 provider：适配器优先（openai_chat_completions 默认变体），其余走 §3.1.4 既有分支。
	provider, err := r.buildProvider(record, apiKey)
	if err != nil {
		entry.buildErr = fmt.Errorf("%w: provider %q protocol=%s variant=%s 构建失败", ErrProviderUnavailable, entry.key, entry.protocol, entry.variant)
		r.warnUnavailable(entry, apiKey, "build_failed")
		return entry
	}
	entry.provider = provider
	entry.supportsTools = supportsToolCalling(provider)
	// 快照加载期 Debug：解析结果结构化日志，密钥仅脱敏值（§3.7）。
	llmRegistryDebugLog(r.logger, "llm registry: provider slot built",
		"tenant_id", record.TenantID, "provider", entry.key, "protocol", entry.protocol,
		"variant", entry.variant, "model", entry.model, "api_key", common.MaskSecret(apiKey))
	return entry
}

// buildProvider 按协议分派构建（§3.2 第 3 条 / §3.1.4 映射表；变体只影响构造参数）。
func (r *LLMProviderRegistry) buildProvider(record *ent.LLMProviderConfig, apiKey string) (LLMProvider, error) {
	protocolName := NormalizeLLMProtocol(record.Protocol)
	variant := NormalizeLLMVariant(record.Variant)

	// 适配器优先：协议已在协议包注册（一协议一实现）即按适配器承载——openai_chat_completions
	// 覆盖默认 / azure / ollama 三个变体（变体只影响 endpoint 与调用参数），anthropic_messages
	// 覆盖官方 / minimax 两个变体，openai_responses / google_gemini 覆盖标准形态（PA-3/PA-4，
	// 4 值枚举已全部适配器化）；未注册协议/变体回退既有分支
	// （"适配器优先、旧分支回退"，与 BE-9 静态路径同口径）。
	if protocolAdapters.Supports(protocolName, variant) {
		provider, err := NewProtocolProvider(ProtocolProviderOptions{
			Protocol:    protocolName,
			Variant:     variant,
			APIKey:      apiKey,
			Endpoint:    record.Endpoint,
			Model:       llmProviderModelForVariant(protocolName, variant, record.Model, record.Deployment),
			MaxTokens:   adapterOptionInt(record.AdapterOptions, "max_tokens"),
			Temperature: adapterOptionFloat(record.AdapterOptions, "temperature"),
		})
		if err == nil {
			return provider, nil
		}
		safeLog(r.logger, "llm registry: protocol adapter unavailable, falling back to legacy provider",
			"provider", record.Name, "protocol", protocolName, "variant", variant, "reason", "adapter_build_failed")
	}

	// 未注册协议或适配器构建失败：归一为 ProviderConfig 后复用 NewProviderFromConfig
	// （旧分支兜底，静态路径与 DB 路径同口径）。
	legacyProvider, err := MapProtocolToLegacyProvider(protocolName, variant)
	if err != nil {
		return nil, err
	}
	if legacyProvider == "" {
		// PA-3：openai_responses 无旧分支可回退（空映射值）。走到这里说明适配器未注册或
		// 构建失败，必须显式不可用，不得静默落到 NewProviderFromConfig 的 OpenAI 默认分支
		// （那会以 Chat Completions 形态调用 Responses 实例）。
		return nil, fmt.Errorf("%w: 协议 %s 无法构建（适配器不可用且无旧分支回退）",
			ErrProviderUnavailable, protocolName)
	}
	built := NewProviderFromConfig(ProviderConfig{
		Provider:   legacyProvider,
		Model:      record.Model,
		APIKey:     apiKey,
		Endpoint:   record.Endpoint,
		Deployment: record.Deployment,
	})
	if built == nil {
		return nil, fmt.Errorf("legacy provider %q 构建为空", legacyProvider)
	}
	return built, nil
}

// decryptAPIKey 解密实例密钥：缺失/解密失败/解密结果为空一律返回包装 ErrProviderKeyMissing 的错误。
//
// 错误信息只含 provider name（不含明文/密文）；底层解密错误不进入错误链，避免第三方 decrypter
// 把敏感输入带入日志（§3.5：日志只能用 name + 脱敏值）。
func (r *LLMProviderRegistry) decryptAPIKey(record *ent.LLMProviderConfig) (string, error) {
	ciphertext := strings.TrimSpace(record.EncryptedAPIKey)
	if ciphertext == "" {
		return "", fmt.Errorf("%w: provider %q 未配置 encrypted_api_key", ErrProviderKeyMissing, strings.TrimSpace(record.Name))
	}
	if r.decrypter == nil {
		return "", fmt.Errorf("%w: provider %q 未注入密钥解密服务", ErrProviderKeyMissing, strings.TrimSpace(record.Name))
	}
	plaintext, err := r.decrypter.Decrypt(ciphertext)
	if err != nil {
		return "", fmt.Errorf("%w: provider %q 密钥解密失败", ErrProviderKeyMissing, strings.TrimSpace(record.Name))
	}
	if strings.TrimSpace(plaintext) == "" {
		return "", fmt.Errorf("%w: provider %q 解密结果为空", ErrProviderKeyMissing, strings.TrimSpace(record.Name))
	}
	return plaintext, nil
}

// warnUnavailable 记录不可用 slot 的告警：只输出 name / 协议 / 变体 / 原因，密钥仅脱敏值（§3.5）。
func (r *LLMProviderRegistry) warnUnavailable(entry *llmRegistryEntry, apiKey, reason string) {
	fields := []interface{}{
		"provider", entry.key,
		"protocol", entry.protocol,
		"variant", entry.variant,
		"reason", reason,
	}
	if apiKey != "" {
		fields = append(fields, "api_key", common.MaskSecret(apiKey))
	}
	safeLog(r.logger, "llm registry: provider slot unavailable", fields...)
}

// staticFallbackUnavailable 判定静态回退是否为空（§3.3：无/密钥为占位符 → AI_PROVIDER_UNAVAILABLE）。
//
// 口径（保持既有默认 openai 兜底不被误伤）：
//   - 显式配置了 provider → 可用（是否真能调用由既有实现与启动硬约束矩阵负责）；
//   - provider 为空但配置了 endpoint（自建兼容网关/本地模型）→ 可用；
//   - provider 为空且无 endpoint：密钥（含 OPENAI_API_KEY / AZURE_OPENAI_API_KEY / MINIMAX_API_KEY
//     环境变量兜底）为空或占位符 → 不可用。
func staticFallbackUnavailable(cfg ProviderConfig) bool {
	if strings.TrimSpace(cfg.Provider) != "" {
		return false
	}
	if strings.TrimSpace(cfg.Endpoint) != "" {
		return false
	}
	apiKey := strings.TrimSpace(cfg.APIKey)
	if apiKey == "" {
		apiKey = staticEnvAPIKey()
	}
	return common.IsPlaceholderSecret(apiKey)
}

// staticEnvAPIKey 与 NewProviderFromConfig 的环境变量兜底同源（只用于"静态配置是否为空"判定）。
func staticEnvAPIKey() string {
	for _, name := range []string{"OPENAI_API_KEY", "AZURE_OPENAI_API_KEY", "MINIMAX_API_KEY"} {
		if value := strings.TrimSpace(os.Getenv(name)); value != "" {
			return value
		}
	}
	return ""
}

// supportsToolCalling 能力位按真实实现探测（不查表）：实现 ToolCallingStreamProvider 才算支持。
func supportsToolCalling(provider LLMProvider) bool {
	if provider == nil {
		return false
	}
	_, ok := provider.(ToolCallingStreamProvider)
	return ok
}

// adapterOptionInt 读取 adapter_options 中的整数参数（容忍 JSON number / float64 / 字符串；非法值按未设置）。
func adapterOptionInt(options map[string]interface{}, key string) int {
	value, ok := options[key]
	if !ok || value == nil {
		return 0
	}
	switch typed := value.(type) {
	case int:
		return typed
	case int32:
		return int(typed)
	case int64:
		return int(typed)
	case float64:
		return int(typed)
	case json.Number:
		if parsed, err := typed.Int64(); err == nil {
			return int(parsed)
		}
	case string:
		if parsed, err := strconv.Atoi(strings.TrimSpace(typed)); err == nil {
			return parsed
		}
	}
	return 0
}

// adapterOptionFloat 读取 adapter_options 中的浮点参数（容忍 JSON number / float64 / 字符串；非法值按未设置）。
func adapterOptionFloat(options map[string]interface{}, key string) float64 {
	value, ok := options[key]
	if !ok || value == nil {
		return 0
	}
	switch typed := value.(type) {
	case float64:
		return typed
	case int:
		return float64(typed)
	case int32:
		return float64(typed)
	case int64:
		return float64(typed)
	case json.Number:
		if parsed, err := typed.Float64(); err == nil {
			return parsed
		}
	case string:
		if parsed, err := strconv.ParseFloat(strings.TrimSpace(typed), 64); err == nil {
			return parsed
		}
	}
	return 0
}

// llmRegistryDebugLog 输出解析链诊断日志（Debug 级别，nil logger 安全）。
//
// 与 safeLog（Warn 级）分开：每次快照加载的"slot 已构建"属正常诊断信息，
// 不该占用告警通道；不可用 slot 仍走 safeLog 的 Warn 通道。
func llmRegistryDebugLog(logger *zap.SugaredLogger, msg string, kv ...interface{}) {
	if logger == nil {
		return
	}
	logger.Debugw(msg, kv...)
}
