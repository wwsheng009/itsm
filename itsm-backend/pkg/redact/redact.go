// Package redact 提供 MCP/外部工具链路的脱敏与截断原语（M0-11）。
//
// 定位：叶子包（零内部依赖），供 handlers/ai（tool_invocations.args_redacted/output_summary）、
// mcp/admin（管理审计 before/after）与 mcp/provider（结果摘要）共用，避免 handlers → mcp/admin 的耦合。
//
// 约束：
//   - 敏感键（键名匹配 SensitiveKeyPatterns）的值**一律**替换为掩码，不做"短值放行"；
//   - 非敏感的长字符串按上限截断并追加 `…(truncated)` 标记；
//   - 输出始终为合法 JSON（可落 Text 列、可 diff）；不保证保留原始类型精度。
package redact

import (
	"encoding/json"
	"sort"
	"strings"
)

// TruncatedMarker 截断标记（审计消费方依赖该常量识别不完整内容）。
const TruncatedMarker = "…(truncated)"

// DefaultArgsLimit 入参快照落库上限（字节）。
const DefaultArgsLimit = 8 * 1024

// DefaultValueLimit 单个值截断上限（字符）。
const DefaultValueLimit = 512

// SensitiveKeyPatterns 敏感键名子串（大小写不敏感，包含即命中）。
//
// 覆盖：密码、凭据、token、API key、私钥、签名、授权头、cookie、会话。
var SensitiveKeyPatterns = []string{
	"password", "passwd", "pwd",
	"secret", "token", "credential", "private_key", "api_key", "apikey",
	"access_key", "auth_key", "signature", "authorization", "cookie", "session",
}

// Mask 固定掩码（不可逆，且不泄露长度以外的信息）。
const Mask = "****"

// SensitiveKey 判断键名是否敏感。
func SensitiveKey(key string) bool {
	lower := strings.ToLower(strings.TrimSpace(key))
	for _, pattern := range SensitiveKeyPatterns {
		if strings.Contains(lower, pattern) {
			return true
		}
	}
	return false
}

// Map 深拷贝并按规则脱敏任意 JSON 化值（map/slice/标量）；长字符串截断。
func Map(value interface{}) interface{} {
	switch typed := value.(type) {
	case map[string]interface{}:
		out := make(map[string]interface{}, len(typed))
		for key, item := range typed {
			if SensitiveKey(key) {
				out[key] = Mask
				continue
			}
			out[key] = Map(item)
		}
		return out
	case map[string]string:
		out := make(map[string]interface{}, len(typed))
		for key, item := range typed {
			if SensitiveKey(key) {
				out[key] = Mask
				continue
			}
			out[key] = truncateString(item, DefaultValueLimit)
		}
		return out
	case []interface{}:
		out := make([]interface{}, len(typed))
		for index, item := range typed {
			out[index] = Map(item)
		}
		return out
	case string:
		return truncateString(typed, DefaultValueLimit)
	default:
		return value
	}
}

// ArgsJSON 把入参映射转成脱敏后的 JSON 字符串（落 args_redacted）。
//
// limit <= 0 时使用 DefaultArgsLimit；超限时整体替换为截断信封，保证输出仍是合法 JSON。
func ArgsJSON(args map[string]interface{}, limit int) string {
	if limit <= 0 {
		limit = DefaultArgsLimit
	}
	redacted := Map(args)
	encoded, err := json.Marshal(redacted)
	if err != nil {
		// 不可序列化（函数/通道等）：只报类型，不回显内容。
		keys := make([]string, 0, len(args))
		for key := range args {
			if SensitiveKey(key) {
				keys = append(keys, key)
				continue
			}
			keys = append(keys, key)
		}
		sort.Strings(keys)
		fallback, _ := json.Marshal(map[string]interface{}{"unserializable_keys": keys})
		return string(fallback)
	}
	if len(encoded) <= limit {
		return string(encoded)
	}
	envelope, _ := json.Marshal(map[string]interface{}{
		"truncated": true,
		"keys":      sortedKeys(args),
		"reason":    "args exceed limit",
	})
	if len(envelope) > limit {
		return `{"truncated":true}`
	}
	return string(envelope)
}

// Summary 生成文本摘要：先按行压缩空白，再按 limit 截断（limit <= 0 时用 DefaultValueLimit）。
func Summary(text string, limit int) string {
	if limit <= 0 {
		limit = DefaultValueLimit
	}
	return truncateString(text, limit)
}

// ValueSummary 把任意执行结果转为脱敏、截断的摘要字符串。
func ValueSummary(value interface{}, limit int) string {
	if value == nil {
		return ""
	}
	if text, ok := value.(string); ok {
		return truncateString(text, limit)
	}
	encoded, err := json.Marshal(Map(value))
	if err != nil {
		return ""
	}
	return truncateString(string(encoded), limit)
}

// BodyJSON 生成管理审计 request_body（before/after/result 脱敏 + 整体限额）。
func BodyJSON(fields map[string]interface{}, limit int) string {
	if limit <= 0 {
		limit = DefaultArgsLimit
	}
	encoded, err := json.Marshal(Map(fields))
	if err != nil {
		return `{"truncated":true}`
	}
	if len(encoded) <= limit {
		return string(encoded)
	}
	return string(mustMarshal(map[string]interface{}{"truncated": true, "keys": sortedKeys(fields)}))
}

func truncateString(text string, limit int) string {
	if limit <= 0 {
		limit = DefaultValueLimit
	}
	if len(text) <= limit {
		return text
	}
	return text[:limit] + TruncatedMarker
}

func sortedKeys(m map[string]interface{}) []string {
	keys := make([]string, 0, len(m))
	for key := range m {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

func mustMarshal(value interface{}) []byte {
	encoded, err := json.Marshal(value)
	if err != nil {
		return []byte(`{"truncated":true}`)
	}
	return encoded
}
