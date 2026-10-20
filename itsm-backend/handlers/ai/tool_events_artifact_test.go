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
		Tool: "analyze_ci_impact_plan", Provider: "builtin", Phase: "read", Status: "done",
		ArtifactID: 31, ArtifactKind: "analysis", ArtifactTitle: "配置项变更影响分析草案",
	})
	assert.NoError(t, err)
	assert.Contains(t, string(payload), `"artifactId":31`)
	assert.Contains(t, string(payload), `"artifactKind":"analysis"`)
	assert.Contains(t, string(payload), `"artifactTitle":"配置项变更影响分析草案"`)

	// 无产物 id 时不落字段（可选字段，保持既有事件字节形状）。
	payload, err = json.Marshal(ToolStreamEvent{Tool: "list_tickets", Provider: "builtin", Phase: "read", Status: "done"})
	assert.NoError(t, err)
	assert.NotContains(t, string(payload), "artifactId")
	assert.NotContains(t, string(payload), "artifactTitle")
}

func TestToolResultArtifactMeta(t *testing.T) {
	id, kind, title := toolResultArtifactMeta(map[string]interface{}{
		"artifactId": float64(33), "kind": "plan", "title": "事件单字段草案",
	})
	assert.Equal(t, 33, id)
	assert.Equal(t, "plan", kind)
	assert.Equal(t, "事件单字段草案", title)

	// 缺 title/kind 时只回传 id（前端按规则/类型回退，不臆造标题）。
	id, kind, title = toolResultArtifactMeta(map[string]interface{}{"artifactId": 34})
	assert.Equal(t, 34, id)
	assert.Empty(t, kind)
	assert.Empty(t, title)

	// 非法/缺少 id → 三者都为空。
	id, kind, title = toolResultArtifactMeta(map[string]interface{}{"artifactId": "35", "title": "x"})
	assert.Zero(t, id)
	assert.Empty(t, kind)
	assert.Empty(t, title)
}
