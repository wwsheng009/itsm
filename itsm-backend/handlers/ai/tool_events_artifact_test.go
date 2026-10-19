package ai

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
)

// I0-04 Outcome Contract：产物 id 提取与事件 JSON 契约（可选字段，旧客户端忽略）。

func TestToolResultArtifactID(t *testing.T) {
	cases := []struct {
		name   string
		result interface{}
		want   int
	}{
		{"int 产物", map[string]interface{}{"artifactId": 31}, 31},
		{"int64 产物", map[string]interface{}{"artifactId": int64(32)}, 32},
		{"float64 产物（JSON 解码形态）", map[string]interface{}{"artifactId": float64(33)}, 33},
		{"json.Number 产物", map[string]interface{}{"artifactId": json.Number("34")}, 34},
		{"无 artifactId", map[string]interface{}{"kind": "read"}, 0},
		{"artifactId 非法", map[string]interface{}{"artifactId": "31"}, 0},
		{"nil 结果", nil, 0},
		{"非 map 结果", []interface{}{1, 2}, 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, toolResultArtifactID(tc.result))
		})
	}
}

func TestToolStreamEvent_ArtifactIDJSONContract(t *testing.T) {
	payload, err := json.Marshal(ToolStreamEvent{
		Tool: "analyze_ci_impact_plan", Provider: "builtin", Phase: "read", Status: "done", ArtifactID: 31,
	})
	assert.NoError(t, err)
	assert.Contains(t, string(payload), `"artifactId":31`)

	// 无产物 id 时不落字段（可选字段，保持既有事件字节形状）。
	payload, err = json.Marshal(ToolStreamEvent{Tool: "list_tickets", Provider: "builtin", Phase: "read", Status: "done"})
	assert.NoError(t, err)
	assert.NotContains(t, string(payload), "artifactId")
}
