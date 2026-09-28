package bot

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"itsm-backend/pkg/redact"
)

// B0-06：default 档——常规字段保留（长值截断）、敏感键强制掩码，且与 pkg/redact 口径一致。
func TestRedactor_DefaultProfile(t *testing.T) {
	r := NewRedactor()
	args := map[string]interface{}{
		"title":    "打印机故障",
		"priority": "high",
		"password": "P@ssw0rd-明文",
		"api_key":  "sk-live-abcdef",
		"nested": map[string]interface{}{
			"token": "nested-token-value",
			"note":  "普通说明",
		},
		"items": []interface{}{
			map[string]interface{}{"secret": "array-secret", "name": "正常项"},
		},
	}

	out := r.RedactArgs(ProfileDefault, args, 0)

	// 明文密钥类不得出现；常规字段保留。
	for _, leaked := range []string{"P@ssw0rd-明文", "sk-live-abcdef", "nested-token-value", "array-secret"} {
		assert.NotContains(t, out, leaked, "敏感值不得落库：%s", leaked)
	}
	assert.Contains(t, out, "打印机故障")
	assert.Contains(t, out, "普通说明")
	assert.Contains(t, out, "正常项")
	assert.Contains(t, out, redact.Mask)

	// 与 pkg/redact 的默认口径互查（MCP 凭据脱敏同源）：无额外键时输出必须逐字节一致。
	assert.Equal(t, redact.ArgsJSON(args, 0), out, "default 档必须与 pkg/redact 口径一致")

	// 合法 JSON（可落 Text 列、可 diff）。
	var decoded map[string]interface{}
	require.NoError(t, json.Unmarshal([]byte(out), &decoded))
}

// B0-06：敏感键名单可配（追加子串，大小写不敏感，作用于嵌套结构）。
func TestRedactor_ConfigurableSensitiveKeys(t *testing.T) {
	r := NewRedactor("id_card", "身份证")
	args := map[string]interface{}{
		"ID_Card": "330100199001011234",
		"备注":      "身份证 1234",
		"姓名":      "张三",
	}

	out := r.RedactArgs(ProfileDefault, args, 0)
	assert.NotContains(t, out, "330100199001011234")
	assert.Contains(t, out, `"ID_Card":"`+redact.Mask+`"`, "自定义敏感键（大小写不敏感）必须掩码")

	// 口径边界（如实记录）：default 档按**键名**判定敏感，不对自由文本做值级扫描；
	// 含敏感信息但键名普通的文本必须由调用方选 strict 档（或改写键名）来兜底。
	assert.NotContains(t, out, "330100199001011234")
	assert.Contains(t, out, "张三", "常规键值保留")
}

// B0-06：strict 档——只保留顶层键名，任何值都不输出（高敏工具全掩码）。
func TestRedactor_StrictProfileMasksAllValues(t *testing.T) {
	r := NewRedactor()
	args := map[string]interface{}{
		"title":    "普通标题",
		"password": "P@ssw0rd-明文",
		"count":    float64(3),
	}

	out := r.RedactArgs(ProfileStrict, args, 0)
	assert.NotContains(t, out, "普通标题", "strict 档不得输出任何值")
	assert.NotContains(t, out, "P@ssw0rd-明文")
	assert.Contains(t, out, "title", "键名保留，便于审计定位字段")
	assert.Contains(t, out, "password")
	assert.Contains(t, out, `"masked":true`)

	res := r.RedactResult(ProfileStrict, map[string]interface{}{"ticket": map[string]interface{}{"id": 99}}, 0)
	assert.NotContains(t, res, "99", "strict 结果摘要不得泄露值")
	assert.Contains(t, res, "ticket")

	assert.Equal(t, `{"masked":true,"profile":"strict"}`, r.RedactText(ProfileStrict, "拒绝原因原文", 0))
}

// B0-06：超长输出截断；超限入参整体替换为键名信封（仍是合法 JSON）。
func TestRedactor_Truncation(t *testing.T) {
	r := NewRedactor()
	long := strings.Repeat("x", 2000)

	out := r.RedactArgs(ProfileDefault, map[string]interface{}{"description": long}, 0)
	assert.Contains(t, out, redact.TruncatedMarker)
	assert.NotContains(t, out, long)

	oversized := r.RedactArgs(ProfileDefault, map[string]interface{}{"description": long}, 64)
	assert.Contains(t, oversized, `"truncated":true`)

	// 上限足够容纳键名信封时保留键名（审计可定位字段）。
	withKeys := r.RedactArgs(ProfileDefault, map[string]interface{}{"description": long}, 128)
	assert.Contains(t, withKeys, `"truncated":true`)
	assert.Contains(t, withKeys, "description")

	// 上限极小 → 退化为最小信封（仍是合法 JSON）。
	tiny := r.RedactArgs(ProfileDefault, map[string]interface{}{"description": long}, 16)
	assert.Equal(t, `{"truncated":true}`, tiny)

	// 值摘要同样受上限约束。
	summary := r.RedactResult(ProfileDefault, map[string]interface{}{"body": long}, 128)
	assert.LessOrEqual(t, len(summary), 128+len(redact.TruncatedMarker))
}

// B0-06：未知/空档位收敛为 strict（与 B0-01 兜底口径一致）。
func TestNormalizeProfile(t *testing.T) {
	assert.Equal(t, ProfileDefault, NormalizeProfile("default"))
	assert.Equal(t, ProfileStrict, NormalizeProfile("strict"))
	assert.Equal(t, ProfileStrict, NormalizeProfile(""))
	assert.Equal(t, ProfileStrict, NormalizeProfile("unknown-profile"))
}
