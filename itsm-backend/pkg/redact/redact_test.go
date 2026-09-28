package redact

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSensitiveKey(t *testing.T) {
	for _, key := range []string{"password", "PASSWORD", "api_key", "apiKey", "access_token", "Authorization", "client_secret", "private_key", "refreshToken"} {
		assert.True(t, SensitiveKey(key), "%s 应判定为敏感键", key)
	}
	for _, key := range []string{"title", "id", "description", "server_name", "limit"} {
		assert.False(t, SensitiveKey(key), "%s 不应判定为敏感键", key)
	}
}

func TestArgsJSON_MasksSensitiveAndKeepsStructure(t *testing.T) {
	args := map[string]interface{}{
		"project": "itops",
		"token":   "glpat-abcdef123456",
		"nested": map[string]interface{}{
			"password": "p@ssw0rd",
			"keep":     "visible",
		},
		"list": []interface{}{"a", map[string]interface{}{"secret": "s"}},
	}
	encoded := ArgsJSON(args, 0)
	assert.NotContains(t, encoded, "glpat-abcdef123456", "token 明文不得出现在快照中")
	assert.NotContains(t, encoded, "p@ssw0rd", "嵌套 password 明文不得出现")
	assert.Contains(t, encoded, "itops")
	assert.Contains(t, encoded, "visible")

	var decoded map[string]interface{}
	require.NoError(t, json.Unmarshal([]byte(encoded), &decoded), "输出必须是合法 JSON")
	assert.Equal(t, Mask, decoded["token"])
	nested := decoded["nested"].(map[string]interface{})
	assert.Equal(t, Mask, nested["password"])
	assert.Equal(t, "visible", nested["keep"])
}

func TestArgsJSON_TruncatesWithEnvelope(t *testing.T) {
	args := map[string]interface{}{"note": strings.Repeat("x", 4096)}
	original, err := json.Marshal(args)
	require.NoError(t, err)

	encoded := ArgsJSON(args, 256)
	assert.LessOrEqual(t, len(encoded), 256+len(TruncatedMarker))
	var decoded map[string]interface{}
	require.NoError(t, json.Unmarshal([]byte(encoded), &decoded), "截断后仍是合法 JSON")
	assert.Equal(t, true, decoded["truncated"])
	assert.Contains(t, decoded["keys"], "note")
	// 长值本身先被单值截断（512），再次超限时走截断信封；两种路径都不含完整原文。
	assert.NotContains(t, encoded, string(original))
}

func TestValueSummary(t *testing.T) {
	assert.Equal(t, "", ValueSummary(nil, 0))
	assert.Equal(t, "short", ValueSummary("short", 0))

	long := strings.Repeat("y", 1000)
	summary := ValueSummary(long, 100)
	assert.Less(t, len(summary), 200)
	assert.True(t, strings.HasSuffix(summary, TruncatedMarker))

	structured := map[string]interface{}{"token": "abc", "value": "ok"}
	summary = ValueSummary(structured, 0)
	assert.Contains(t, summary, Mask)
	assert.Contains(t, summary, "ok")
	assert.NotContains(t, summary, `"token":"abc"`)
}

func TestBodyJSON(t *testing.T) {
	body := BodyJSON(map[string]interface{}{
		"object_type": "mcp_server",
		"before":      map[string]interface{}{"name": "gitlab", "token": "plain-secret"},
		"after":       map[string]interface{}{"name": "gitlab-2"},
	}, 0)
	assert.NotContains(t, body, "plain-secret")
	var decoded map[string]interface{}
	require.NoError(t, json.Unmarshal([]byte(body), &decoded))
	before := decoded["before"].(map[string]interface{})
	assert.Equal(t, Mask, before["token"])
	assert.Equal(t, "gitlab", before["name"])

	// 超限：整体截断信封（合法 JSON，标记 truncated）。
	oversized := BodyJSON(map[string]interface{}{"blob": strings.Repeat("z", 20000)}, 512)
	assert.LessOrEqual(t, len(oversized), 1024)
	require.NoError(t, json.Unmarshal([]byte(oversized), &decoded))
	assert.Equal(t, true, decoded["truncated"])
}

// TestValueSummary_MasksSensitiveKeysInStructs 回归守卫：
// 传结构体/slice-of-struct（如 mcp/provider 的 Output/Content）时也必须按键名脱敏。
// 历史缺陷：Map 的 default 分支原样返回结构体，敏感键漏过掩码，明文落 output_summary。
func TestValueSummary_MasksSensitiveKeysInStructs(t *testing.T) {
	type content struct {
		Type string                 `json:"type"`
		Text string                 `json:"text,omitempty"`
		Data map[string]interface{} `json:"data,omitempty"`
	}
	type output struct {
		Provider  string    `json:"provider"`
		Content   []content `json:"content"`
		Truncated bool      `json:"truncated"`
	}

	summary := ValueSummary(output{
		Provider: "mcp",
		Content: []content{
			{Type: "text", Text: "ok"},
			{Type: "structured", Data: map[string]interface{}{
				"token":    "s3cr3t-value",
				"password": "p@ss",
				"total":    3,
			}},
		},
	}, 512)

	assert.NotContains(t, summary, "s3cr3t-value")
	assert.NotContains(t, summary, "p@ss")
	assert.Contains(t, summary, Mask)

	var decoded map[string]interface{}
	require.NoError(t, json.Unmarshal([]byte(summary), &decoded))
	items := decoded["content"].([]interface{})
	data := items[1].(map[string]interface{})["data"].(map[string]interface{})
	assert.Equal(t, Mask, data["token"])
	assert.Equal(t, Mask, data["password"])
	assert.Equal(t, float64(3), data["total"])
}
