package protocol

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
)

// ProtocolError 协议层统一错误：HTTP 状态、上游错误码与消息。
//
// 说明：AI_PROTOCOL_NOT_IMPLEMENTED（未实现协议 → 422）由 service 层在
// Registry 未命中（ErrAdapterNotFound）时映射，不在本包内重复定义。
type ProtocolError struct {
	// Protocol 协议枚举值（如 openai_chat_completions）。
	Protocol string
	// StatusCode HTTP 状态码；流内错误事件（无 HTTP 语义）为 0。
	StatusCode int
	// Code 上游错误码（如 invalid_api_key / context_length_exceeded）。
	Code string
	// Message 上游错误消息（已尽力提取）。
	Message string
	// Body 原始响应体（截断后），便于排障。
	Body string
}

func (e *ProtocolError) Error() string {
	if e == nil {
		return ""
	}
	var b strings.Builder
	b.WriteString(e.Protocol)
	if e.StatusCode != 0 {
		fmt.Fprintf(&b, ": http %d", e.StatusCode)
	} else {
		b.WriteString(": stream error")
	}
	if e.Code != "" {
		fmt.Fprintf(&b, " (%s)", e.Code)
	}
	if e.Message != "" {
		b.WriteString(": ")
		b.WriteString(e.Message)
	}
	return b.String()
}

// Retryable 与网关口径一致（service/llm_gateway.go isTransientLLMError）：
// 429 与 5xx 可重试，其余（含流内错误事件）不重试。
func (e *ProtocolError) Retryable() bool {
	if e == nil {
		return false
	}
	return e.StatusCode == http.StatusTooManyRequests || e.StatusCode >= 500
}

// NewHTTPError 把非 2xx 响应映射为 *ProtocolError，尽力从 OpenAI 风格错误体
// {"error":{"message":...,"type":...,"code":...}} 提取消息与错误码。
func NewHTTPError(protocolName string, resp *http.Response, body []byte) *ProtocolError {
	err := &ProtocolError{Protocol: protocolName, Body: truncateBody(body)}
	if resp != nil {
		err.StatusCode = resp.StatusCode
		if msg := strings.TrimSpace(resp.Status); msg != "" {
			err.Message = msg
		}
	}
	if payload := decodeJSONMap(body); payload != nil {
		if upstream := errorPayload(payload); upstream != nil {
			if upstream.Message != "" {
				err.Message = upstream.Message
			}
			if upstream.Code != "" {
				err.Code = upstream.Code
			}
		}
	}
	if err.Message == "" {
		err.Message = "upstream request failed"
	}
	return err
}

// errorFromPayload 识别响应体内的错误对象（流式/非流式共用的带内错误）。
// 无错误返回 nil。
func errorFromPayload(protocolName string, payload map[string]any) *ProtocolError {
	upstream := errorPayload(payload)
	if upstream == nil {
		return nil
	}
	return &ProtocolError{
		Protocol: protocolName,
		Code:     upstream.Code,
		Message:  upstream.Message,
	}
}

type upstreamError struct {
	Code    string
	Message string
}

// errorPayload 从 payload["error"] 提取错误；支持 string 与 object 两种形态。
func errorPayload(payload map[string]any) *upstreamError {
	raw, ok := payload["error"]
	if !ok || raw == nil {
		return nil
	}
	switch value := raw.(type) {
	case string:
		if strings.TrimSpace(value) == "" {
			return nil
		}
		return &upstreamError{Message: strings.TrimSpace(value)}
	case map[string]any:
		message := firstString(value["message"], value["msg"])
		code := firstString(value["code"], value["type"])
		if message == "" && code == "" {
			return nil
		}
		return &upstreamError{Code: code, Message: message}
	default:
		return nil
	}
}

func firstString(values ...any) string {
	for _, value := range values {
		if text, ok := value.(string); ok && strings.TrimSpace(text) != "" {
			return strings.TrimSpace(text)
		}
	}
	return ""
}

func decodeJSONMap(body []byte) map[string]any {
	if len(body) == 0 {
		return nil
	}
	var payload map[string]any
	if err := json.Unmarshal(body, &payload); err != nil {
		return nil
	}
	return payload
}

const protocolErrorBodyLimit = 2048

func truncateBody(body []byte) string {
	text := strings.TrimSpace(string(body))
	if len(text) <= protocolErrorBodyLimit {
		return text
	}
	return text[:protocolErrorBodyLimit]
}
