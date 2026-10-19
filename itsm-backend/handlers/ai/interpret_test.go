package ai_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"itsm-backend/handlers/ai"
)

// V1-03 口语↔术语确认契约：口语 → 术语（分类/优先级）+ 证据片段 + 置信度；
// 纯确定性词典（无 IO/无 Provider）；未识别降级 other/medium；输入长度 fail-visible。

func newInterpretHarness(t *testing.T) *gin.Engine {
	t.Helper()
	gin.SetMode(gin.TestMode)
	engine := gin.New()
	// InterpretTicketIntent 不依赖 svc（纯函数理解），nil Service 即可验证契约。
	handler := ai.NewHandler(nil)
	engine.POST("/api/v1/ai/tickets/interpret", handler.InterpretTicketIntent)
	return engine
}

func postInterpret(t *testing.T, engine *gin.Engine, payload interface{}) (int, map[string]interface{}, map[string]interface{}) {
	t.Helper()
	body, err := json.Marshal(payload)
	require.NoError(t, err)
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/ai/tickets/interpret", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	engine.ServeHTTP(w, req)

	var resp struct {
		Code    int                    `json:"code"`
		Message string                 `json:"message"`
		Data    map[string]interface{} `json:"data"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp), w.Body.String())
	return w.Code, resp.Data, map[string]interface{}{"code": resp.Code, "message": resp.Message}
}

// interpretTerms 提取 terms 数组（按 field 建立索引，便于断言证据片段）。
func interpretTerms(t *testing.T, data map[string]interface{}) map[string]map[string]interface{} {
	t.Helper()
	raw, ok := data["terms"].([]interface{})
	require.True(t, ok, "terms 必须为数组：%v", data)
	byField := make(map[string]map[string]interface{}, len(raw))
	for _, entry := range raw {
		term, ok := entry.(map[string]interface{})
		require.True(t, ok)
		field, _ := term["field"].(string)
		byField[field] = term
	}
	return byField
}

func TestInterpretTicketIntent_MapsColloquialToTerms(t *testing.T) {
	engine := newInterpretHarness(t)

	status, data, _ := postInterpret(t, engine, map[string]interface{}{
		"utterance": "我的电脑连不上网了，很急",
	})
	require.Equal(t, http.StatusOK, status)

	assert.Equal(t, "网络故障（高）", data["summary"])
	assert.Equal(t, true, data["matched"])
	assert.Equal(t, true, data["needsConfirmation"], "理解必须经用户确认后才进入建单")
	assert.Greater(t, data["confidence"].(float64), 0.8)

	fields := data["fields"].(map[string]interface{})
	assert.Equal(t, "network", fields["type"])
	assert.Equal(t, "high", fields["priority"])
	assert.Equal(t, "我的电脑连不上网了，很急", fields["description"])

	terms := interpretTerms(t, data)
	require.Contains(t, terms, "type")
	assert.Equal(t, "连不上网", terms["type"]["surface"], "术语必须带口语证据片段")
	assert.Equal(t, "network", terms["type"]["value"])
	assert.Equal(t, "网络故障", terms["type"]["term"])
	require.Contains(t, terms, "priority")
	assert.Equal(t, "很急", terms["priority"]["surface"])
	assert.Equal(t, "high", terms["priority"]["value"])
	assert.Equal(t, "高", terms["priority"]["term"])
}

func TestInterpretTicketIntent_PriorityUrgentBeatsPlainUrgency(t *testing.T) {
	engine := newInterpretHarness(t)

	status, data, _ := postInterpret(t, engine, map[string]interface{}{
		"utterance": "网络断了好几次，生产环境非常紧急",
	})
	require.Equal(t, http.StatusOK, status)

	terms := interpretTerms(t, data)
	require.Contains(t, terms, "type")
	assert.Equal(t, "network", terms["type"]["value"])
	require.Contains(t, terms, "priority")
	assert.Equal(t, "urgent", terms["priority"]["value"])
	assert.Equal(t, "非常紧急", terms["priority"]["surface"])
	assert.Equal(t, "网络故障（紧急）", data["summary"])
}

func TestInterpretTicketIntent_NegationBeatsUrgencyKeyword(t *testing.T) {
	engine := newInterpretHarness(t)

	// 「不着急」「不紧急」等否定表达必须压过「急/紧急」关键词，否则会误判升级。
	for _, tc := range []struct {
		utterance string
		surface   string
	}{
		{utterance: "打印机坏了，不着急", surface: "不着急"},
		{utterance: "网络有点慢，这件事不紧急", surface: "不紧急"},
		{utterance: "软件装不上，不急", surface: "不急"},
	} {
		t.Run(tc.utterance, func(t *testing.T) {
			status, data, _ := postInterpret(t, engine, map[string]interface{}{"utterance": tc.utterance})
			require.Equal(t, http.StatusOK, status)

			terms := interpretTerms(t, data)
			require.Contains(t, terms, "priority", "否定表达必须给出低优先级术语")
			assert.Equal(t, "low", terms["priority"]["value"])
			assert.Equal(t, tc.surface, terms["priority"]["surface"])
			// 否定命中为锁定规则：置信度高于普通模式（0.95 上限内）。
			assert.GreaterOrEqual(t, terms["priority"]["confidence"].(float64), 0.9)
		})
	}
}

func TestInterpretTicketIntent_UnknownFallsBackWithoutGuessing(t *testing.T) {
	engine := newInterpretHarness(t)

	status, data, _ := postInterpret(t, engine, map[string]interface{}{
		"utterance": "qwerty zzz 123",
	})
	require.Equal(t, http.StatusOK, status)

	assert.Equal(t, false, data["matched"])
	assert.Equal(t, true, data["needsConfirmation"])
	assert.Less(t, data["confidence"].(float64), 0.5, "未识别不得虚高置信度")
	assert.Equal(t, "其他（中）", data["summary"])
	terms, _ := data["terms"].([]interface{})
	assert.Empty(t, terms)

	fields := data["fields"].(map[string]interface{})
	assert.Equal(t, "other", fields["type"])
	assert.Equal(t, "medium", fields["priority"])
}

func TestInterpretTicketIntent_InputBoundaries(t *testing.T) {
	engine := newInterpretHarness(t)

	t.Run("空 utterance 400", func(t *testing.T) {
		status, _, body := postInterpret(t, engine, map[string]interface{}{"utterance": "   "})
		assert.Equal(t, http.StatusBadRequest, status)
		assert.Equal(t, 4000, body["code"])
	})

	t.Run("缺字段 400", func(t *testing.T) {
		status, _, _ := postInterpret(t, engine, map[string]interface{}{})
		assert.Equal(t, http.StatusBadRequest, status)
	})

	t.Run("超长（501 字）400", func(t *testing.T) {
		status, _, _ := postInterpret(t, engine, map[string]interface{}{
			"utterance": strings.Repeat("网", 501),
		})
		assert.Equal(t, http.StatusBadRequest, status)
	})

	t.Run("上限内（500 字）200 且标题截断至 30 字", func(t *testing.T) {
		status, data, _ := postInterpret(t, engine, map[string]interface{}{
			"utterance": strings.Repeat("网络很慢", 125), // 500 字
		})
		require.Equal(t, http.StatusOK, status)
		fields := data["fields"].(map[string]interface{})
		title, _ := fields["title"].(string)
		assert.LessOrEqual(t, utf8.RuneCountInString(title), 30)
	})
}
