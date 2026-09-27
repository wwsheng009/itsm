package admin

import (
	"net/url"
	"strings"

	"itsm-backend/mcp/registry"
	"itsm-backend/mcp/transport"
)

// 服务器配置校验（M0-08；安全默认 D7：一切非法/未声明 → 拒绝）。
const (
	// MaxDisplayNameLen 显示名上限（与 ent mcp_servers.display_name 对齐）。
	MaxDisplayNameLen = 100
	// MinTimeoutMS / MaxTimeoutMS 单次调用超时上下限。
	MinTimeoutMS = 1000
	MaxTimeoutMS = 300000
	// MinParallelCalls / MaxParallelCalls 并发上限范围。
	MinParallelCalls = 1
	MaxParallelCalls = 32
	// MaxRetryLimit 重试上限。
	MaxRetryLimit = 5
	// MaxHeaderCount / MaxHeaderValueLen 自定义请求头上限。
	MaxHeaderCount    = 20
	MaxHeaderNameLen  = 128
	MaxHeaderValueLen = 4096
)

// ValidateServerName 校验稳定标识（与 registry 同源，保证投影名合法）。
func ValidateServerName(name string) error {
	if !registry.ServerNamePattern.MatchString(strings.TrimSpace(name)) {
		return NewAdminError(400, CodeValidationFailed, "name 必须匹配 ^[a-z0-9_-]{1,32}$")
	}
	return nil
}

// ValidateDisplayName 校验显示名长度。
func ValidateDisplayName(name string) error {
	if len([]rune(name)) > MaxDisplayNameLen {
		return NewAdminError(400, CodeValidationFailed, "display_name 超长")
	}
	return nil
}

// ValidateTransport 校验传输类型（一期仅远程：streamable / sse）。
func ValidateTransport(kind transport.Kind) error {
	switch kind {
	case transport.KindStreamableHTTP, transport.KindSSE:
		return nil
	default:
		return NewAdminError(400, CodeInvalidTransport, "一期仅支持 streamable / sse 传输（stdio 为平台级，另行启用）")
	}
}

// ValidateURL 校验端点形态（协议 https，或平台开关放行的 http；深度校验由 SSRFGuard 执行）。
func ValidateURL(rawURL string) error {
	trimmed := strings.TrimSpace(rawURL)
	if trimmed == "" {
		return NewAdminError(400, CodeValidationFailed, "url 不能为空")
	}
	parsed, err := url.Parse(trimmed)
	if err != nil {
		return NewAdminError(400, CodeValidationFailed, "url 解析失败")
	}
	if parsed.Scheme != "https" && parsed.Scheme != "http" {
		return NewAdminError(400, CodeValidationFailed, "url 仅允许 http(s) 协议")
	}
	if parsed.Host == "" {
		return NewAdminError(400, CodeValidationFailed, "url 缺少主机名")
	}
	if parsed.User != nil {
		return NewAdminError(400, CodeValidationFailed, "url 不得包含用户信息")
	}
	return nil
}

// ValidateTimeout 校验调用超时（毫秒）。
func ValidateTimeout(timeoutMS int) error {
	if timeoutMS == 0 {
		return nil // 缺省 30s
	}
	if timeoutMS < MinTimeoutMS || timeoutMS > MaxTimeoutMS {
		return NewAdminError(400, CodeValidationFailed, "timeout_ms 需在 1000–300000 之间")
	}
	return nil
}

// ValidateParallelCalls 校验并发上限。
func ValidateParallelCalls(limit int) error {
	if limit == 0 {
		return nil // 缺省 4
	}
	if limit < MinParallelCalls || limit > MaxParallelCalls {
		return NewAdminError(400, CodeValidationFailed, "max_parallel_calls 需在 1–32 之间")
	}
	return nil
}

// ValidateRetry 校验重试上限。
func ValidateRetry(maxRetry int) error {
	if maxRetry < 0 || maxRetry > MaxRetryLimit {
		return NewAdminError(400, CodeValidationFailed, "max_retry 需在 0–5 之间")
	}
	return nil
}

// NormalizeSecrets 清洗敏感键值（供自定义请求头 / 凭据登记）：
//   - trim 键；空键拒绝；
//   - 拒绝 CR/LF 注入（HTTP 头注入防护）；
//   - 数量与长度上限；
//   - 空值字符串表示「不修改」（由调用方按 M0-06 语义处理，本函数只做清洗）。
func NormalizeSecrets(values map[string]string) (map[string]string, error) {
	if len(values) == 0 {
		return map[string]string{}, nil
	}
	if len(values) > MaxHeaderCount {
		return nil, NewAdminError(400, CodeValidationFailed, "键值对数量超限")
	}
	normalized := make(map[string]string, len(values))
	for key, value := range values {
		cleanKey := strings.TrimSpace(key)
		if cleanKey == "" {
			return nil, NewAdminError(400, CodeValidationFailed, "键名不能为空")
		}
		if len(cleanKey) > MaxHeaderNameLen {
			return nil, NewAdminError(400, CodeValidationFailed, "键名超长")
		}
		if strings.ContainsAny(cleanKey, "\r\n") || strings.ContainsAny(value, "\r\n") {
			return nil, NewAdminError(400, CodeValidationFailed, "键值不得包含换行符")
		}
		if len(value) > MaxHeaderValueLen {
			return nil, NewAdminError(400, CodeValidationFailed, "键值超长")
		}
		normalized[cleanKey] = value
	}
	return normalized, nil
}
