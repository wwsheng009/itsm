package service

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"

	openai "github.com/sashabaranov/go-openai"
)

// 本文件承载 LLM 调用观测的领域契约（T5-07b-lite 可观测部分）：
//   - 调用记录包含租户、任务类型、Provider 来源与错误分类；
//   - 网关通过可选 LLMCallObserver 扩展上报，未实现该接口的旧 Observer 零破坏；
//   - 错误只保存分类、HTTP 状态与脱敏截断后的摘要，不保存 prompt/请求体。

const (
	// LLMCallErrorRateLimited 请求被本地限流或上游 429。
	LLMCallErrorRateLimited = "rate_limited"
	// LLMCallErrorAuth 鉴权失败（401/403）。
	LLMCallErrorAuth = "auth"
	// LLMCallErrorBadRequest 请求参数错误（400/422 等）。
	LLMCallErrorBadRequest = "bad_request"
	// LLMCallErrorNotFound 上游资源不存在（404）。
	LLMCallErrorNotFound = "not_found"
	// LLMCallErrorServer 上游 5xx。
	LLMCallErrorServer = "server_error"
	// LLMCallErrorTimeout 调用超时或上游超时。
	LLMCallErrorTimeout = "timeout"
	// LLMCallErrorNetwork 网络层错误（连接拒绝/DNS/连接重置等）。
	LLMCallErrorNetwork = "network"
	// LLMCallErrorCancelled 调用方取消。
	LLMCallErrorCancelled = "cancelled"
	// LLMCallErrorProviderUnavailable Provider 未配置/不可用。
	LLMCallErrorProviderUnavailable = "provider_unavailable"
	// LLMCallErrorUnknown 未分类错误。
	LLMCallErrorUnknown = "unknown"
)

const (
	// LLMTaskUnknown 未显式标注任务类型的调用。
	LLMTaskUnknown = "unknown"
	// LLMTaskChat 助手聊天（含 Dock/工作区）。
	LLMTaskChat = "chat"
	// LLMTaskDynamicSuggestion 动态建议生成。
	LLMTaskDynamicSuggestion = "dynamic_suggestion"
	// LLMTaskAsyncAnswer 异步精算回答。
	LLMTaskAsyncAnswer = "async_answer"
	// LLMTaskBatchTriage 批量分诊/批量草稿。
	LLMTaskBatch = "batch"
)

// LLMCallObservation 是一次 LLM 调用的最终观测快照。
//
// Tokens 仍沿用网关的 prompt 估算口径（字符/4），不是 provider 账单口径；
// 调用方/前端必须标注为估算，不能当作金额或真实用量。
type LLMCallObservation struct {
	TenantID       int
	ProviderKey    string
	Provider       string
	ProviderSource string
	Model          string
	TaskType       string
	Tokens         int
	Latency        time.Duration
	Success        bool
	ErrorClass     string
	HTTPStatus     int
	ErrorMessage   string
}

// LLMCallObserver 可选观测能力：比既有 Observer/ProviderKeyObserver 多携带
// ctx（租户）、任务类型与结构化错误。网关按类型断言择优调用。
type LLMCallObserver interface {
	ObserveLLMCall(ctx context.Context, call LLMCallObservation)
}

type llmTaskTypeContextKey struct{}

// WithLLMTaskType 标注本次调用链的任务类型（chat/dynamic_suggestion/...）。
// 未标注时落 unknown；空值/非法值归一为 unknown，避免报表出现不可控维度。
func WithLLMTaskType(ctx context.Context, taskType string) context.Context {
	if ctx == nil {
		return ctx
	}
	return context.WithValue(ctx, llmTaskTypeContextKey{}, NormalizeLLMTaskType(taskType))
}

// LLMTaskTypeFromContext 读取任务类型；未标注/空值返回 unknown。
func LLMTaskTypeFromContext(ctx context.Context) string {
	if ctx == nil {
		return LLMTaskUnknown
	}
	if value, ok := ctx.Value(llmTaskTypeContextKey{}).(string); ok {
		return NormalizeLLMTaskType(value)
	}
	return LLMTaskUnknown
}

// NormalizeLLMTaskType 归一任务类型（小写、去空白、限长 64）。
func NormalizeLLMTaskType(taskType string) string {
	value := strings.ToLower(strings.TrimSpace(taskType))
	if value == "" {
		return LLMTaskUnknown
	}
	if len(value) > 64 {
		value = value[:64]
	}
	for _, r := range value {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '_' || r == '-' || r == '.' {
			continue
		}
		return LLMTaskUnknown
	}
	return value
}

// ClassifyLLMCallError 把错误映射为稳定分类与 HTTP 状态（0 表示无 HTTP 状态）。
// 分类用于报表聚合；原始错误只经 SanitizeLLMCallError 脱敏截断后展示。
func ClassifyLLMCallError(err error) (class string, httpStatus int) {
	if err == nil {
		return "", 0
	}
	if errors.Is(err, ErrRateLimited) {
		return LLMCallErrorRateLimited, 429
	}
	if errors.Is(err, context.Canceled) {
		return LLMCallErrorCancelled, 0
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return LLMCallErrorTimeout, 0
	}
	if errors.Is(err, ErrProviderUnavailable) {
		return LLMCallErrorProviderUnavailable, 0
	}

	var apiErr *openai.APIError
	if errors.As(err, &apiErr) {
		return classifyLLMHTTPStatus(apiErr.HTTPStatusCode), apiErr.HTTPStatusCode
	}
	if status := extractLLMHTTPStatus(err.Error()); status > 0 {
		return classifyLLMHTTPStatus(status), status
	}
	if isTransientLLMError(err) {
		return LLMCallErrorNetwork, 0
	}
	return LLMCallErrorUnknown, 0
}

func classifyLLMHTTPStatus(status int) string {
	switch {
	case status == 429:
		return LLMCallErrorRateLimited
	case status == 401 || status == 403:
		return LLMCallErrorAuth
	case status == 404:
		return LLMCallErrorNotFound
	case status >= 400 && status < 500:
		return LLMCallErrorBadRequest
	case status >= 500:
		return LLMCallErrorServer
	default:
		return LLMCallErrorUnknown
	}
}

var llmHTTPStatusPattern = regexp.MustCompile(`(?i)(?:http(?: status)?|status(?: code)?)[^0-9]{0,12}([1-5][0-9]{2})`)

func extractLLMHTTPStatus(message string) int {
	match := llmHTTPStatusPattern.FindStringSubmatch(message)
	if len(match) != 2 {
		return 0
	}
	var status int
	if _, err := fmt.Sscanf(match[1], "%d", &status); err != nil {
		return 0
	}
	return status
}

var (
	llmSecretBearerPattern = regexp.MustCompile(`(?i)(bearer\s+)[a-z0-9._\-]+`)
	llmSecretSKPattern     = regexp.MustCompile(`(?i)sk-[a-z0-9._\-]{6,}`)
	llmSecretKeyPattern    = regexp.MustCompile(`(?i)((?:api[_-]?key|token|secret)\s*[=:]\s*)[^\s,;]+`)
)

// SanitizeLLMCallError 生成可落库的错误摘要：
//   - 常见密钥形态（sk-* / Bearer / api_key=...）替换为 ****；
//   - 按 rune 截断到 512 字符；
//   - 不包含 prompt、请求体或堆栈。
func SanitizeLLMCallError(err error) string {
	if err == nil {
		return ""
	}
	message := strings.TrimSpace(err.Error())
	message = llmSecretBearerPattern.ReplaceAllString(message, `${1}****`)
	message = llmSecretSKPattern.ReplaceAllString(message, "sk-****")
	message = llmSecretKeyPattern.ReplaceAllString(message, `${1}****`)
	runes := []rune(message)
	if len(runes) > 512 {
		message = string(runes[:512]) + "…"
	}
	return message
}
