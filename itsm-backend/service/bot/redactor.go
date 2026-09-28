// B0-06：审计/消息/工件的统一脱敏入口（default / strict 两档）。
//
// 定位：`pkg/redact` 提供**原语**（敏感键掩码、截断、值摘要），本包把它收敛为
// 「按工具元数据档位」的统一入口，供 handlers/ai（args_redacted / output_summary /
// 消息回填）与执行侧调用，避免各调用点各写一套规则。
//
// 档位口径：
//   - default：常规字段保留（长值截断），密钥/token/密码类字段强制掩码（沿用 pkg/redact 口径）；
//   - strict ：**全参数/全结果掩码**——只保留顶层键名，任何值都不落库（高敏工具）。
//
// 约束：输出始终为合法 JSON；敏感键名单可扩展（不修改 pkg/redact 的全局名单）。
package bot

import (
	"encoding/json"
	"sort"
	"strings"

	"itsm-backend/pkg/redact"
)

// Profile 是脱敏档位（与 service.ToolDefinition.RedactionProfile 的取值一一对应）。
type Profile string

const (
	// ProfileDefault：常规字段保留 + 敏感键掩码。
	ProfileDefault Profile = "default"
	// ProfileStrict：全参数/全结果掩码（只留键名）。
	ProfileStrict Profile = "strict"
)

// NormalizeProfile 把未知/空档位收敛为最保守的 strict（B0-01 兜底口径一致）。
func NormalizeProfile(value string) Profile {
	if Profile(value) == ProfileDefault {
		return ProfileDefault
	}
	return ProfileStrict
}

// Redactor 是脱敏入口（并发安全：只读配置 + 无共享可变状态）。
type Redactor struct {
	patterns   []string
	argsLimit  int
	valueLimit int
}

// NewRedactor 构造脱敏入口；extraKeyPatterns 为**追加**的敏感键名子串（大小写不敏感）。
func NewRedactor(extraKeyPatterns ...string) *Redactor {
	patterns := make([]string, 0, len(redact.SensitiveKeyPatterns)+len(extraKeyPatterns))
	patterns = append(patterns, redact.SensitiveKeyPatterns...)
	patterns = append(patterns, extraKeyPatterns...)
	return &Redactor{
		patterns:   patterns,
		argsLimit:  redact.DefaultArgsLimit,
		valueLimit: redact.DefaultValueLimit,
	}
}

// RedactArgs 生成入参脱敏快照（落 args_redacted；审计/展示唯一来源）。
func (r *Redactor) RedactArgs(profile Profile, args map[string]interface{}, limit int) string {
	if profile == ProfileStrict {
		return strictEnvelope(args, limit)
	}
	return r.defaultArgsJSON(args, limit)
}

// RedactResult 生成结果脱敏摘要（落 output_summary；不落原始结果）。
func (r *Redactor) RedactResult(profile Profile, value interface{}, limit int) string {
	if limit <= 0 {
		limit = r.valueLimit
	}
	if profile == ProfileStrict {
		return strictEnvelope(genericMap(value), limit)
	}
	return redact.ValueSummary(value, limit)
}

// RedactText 生成文本脱敏摘要（如审批拒绝原因；消息内容同样不得原文落库）。
func (r *Redactor) RedactText(profile Profile, text string, limit int) string {
	if profile == ProfileStrict {
		return strictStringEnvelope(limit)
	}
	return redact.Summary(text, limit)
}

// defaultArgsJSON 与 pkg/redact.ArgsJSON 同语义，但使用本实例的敏感键名单。
func (r *Redactor) defaultArgsJSON(args map[string]interface{}, limit int) string {
	if limit <= 0 {
		limit = r.argsLimit
	}
	encoded, err := json.Marshal(r.maskMap(args))
	if err != nil {
		return redact.ArgsJSON(args, limit) // 不可序列化：回落原语（只报键名，不回显内容）
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

// maskMap 递归掩码（map/slice/标量），敏感键 → 掩码、长字符串 → 截断。
func (r *Redactor) maskMap(value interface{}) interface{} {
	switch typed := value.(type) {
	case map[string]interface{}:
		out := make(map[string]interface{}, len(typed))
		for key, item := range typed {
			if r.sensitiveKey(key) {
				out[key] = redact.Mask
				continue
			}
			out[key] = r.maskMap(item)
		}
		return out
	case map[string]string:
		out := make(map[string]interface{}, len(typed))
		for key, item := range typed {
			if r.sensitiveKey(key) {
				out[key] = redact.Mask
				continue
			}
			out[key] = r.truncate(item)
		}
		return out
	case []interface{}:
		out := make([]interface{}, len(typed))
		for index, item := range typed {
			out[index] = r.maskMap(item)
		}
		return out
	case []string:
		out := make([]interface{}, len(typed))
		for index, item := range typed {
			out[index] = r.truncate(item)
		}
		return out
	case string:
		return r.truncate(typed)
	default:
		return value
	}
}

func (r *Redactor) sensitiveKey(key string) bool {
	lower := strings.ToLower(strings.TrimSpace(key))
	for _, pattern := range r.patterns {
		if pattern != "" && strings.Contains(lower, pattern) {
			return true
		}
	}
	return false
}

func (r *Redactor) truncate(text string) string {
	if len(text) <= r.valueLimit {
		return text
	}
	return text[:r.valueLimit] + redact.TruncatedMarker
}

// strictEnvelope 是全掩码信封：只保留顶层键名，任何值都不输出。
func strictEnvelope(args map[string]interface{}, limit int) string {
	if limit <= 0 {
		limit = redact.DefaultArgsLimit
	}
	envelope, _ := json.Marshal(map[string]interface{}{
		"profile": string(ProfileStrict),
		"masked":  true,
		"keys":    sortedKeys(args),
		"reason":  "strict redaction profile",
	})
	if len(envelope) > limit {
		return `{"masked":true,"profile":"strict"}`
	}
	return string(envelope)
}

func strictStringEnvelope(limit int) string {
	envelope := `{"masked":true,"profile":"strict"}`
	if limit > 0 && len(envelope) > limit {
		return `{"masked":true}`
	}
	return envelope
}

// genericMap 把任意值归一化为 map（用于 strict 结果信封的键名列表）；非容器 → 空 map。
func genericMap(value interface{}) map[string]interface{} {
	if value == nil {
		return map[string]interface{}{}
	}
	if typed, ok := value.(map[string]interface{}); ok {
		return typed
	}
	encoded, err := json.Marshal(value)
	if err != nil {
		return map[string]interface{}{}
	}
	var normalized map[string]interface{}
	if err := json.Unmarshal(encoded, &normalized); err != nil {
		return map[string]interface{}{}
	}
	return normalized
}

func sortedKeys(m map[string]interface{}) []string {
	keys := make([]string, 0, len(m))
	for key := range m {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}
