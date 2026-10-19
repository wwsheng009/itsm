package ai_test

import (
	"net/http"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"itsm-backend/handlers/ai"
	"itsm-backend/service"
)

// D0-04 端点级契约：pageContext 是提示层——
//   - 合法/非法/超长 pageKey 一律不引入 400（未知值静默忽略，容忍前端版本先行）；
//   - 不改变既有失败面与状态码（provider 门禁/解析失败语义逐字保持）。
//
// 说明：完整的 run_started 事件携带 pageKey 的正向断言需要 RAG/运行档案工装，
// 归后续联调（证据文档已登记为条件项）。
func TestChatPageContextHintIsHintOnly(t *testing.T) {
	h := ai.NewHandler(newProviderChatService(nil))

	hints := []string{
		`{"pageKey":"ticket.detail","route":"/tickets/42"}`,           // 合法
		`{"pageKey":"Unknown Page!","route":"not-relative"}`,          // 非法：整体忽略
		`{"pageKey":"` + strings.Repeat("a", 300) + `","route":"/x"}`, // 超长：忽略
		`{"route":"/no-page-key"}`,                                    // 缺 pageKey：忽略
	}
	for _, hint := range hints {
		for _, stream := range []bool{false, true} {
			body := `{"query":"hello","provider":"deepseek-prod","pageContext":` + hint + `}`
			w := postProviderChat(t, h, "agent", body, stream)
			require.Equal(t, http.StatusForbidden, w.Code,
				"hint=%s stream=%v body=%s", hint, stream, w.Body.String())
			assert.Equal(t, "AI_PROVIDER_FORBIDDEN", decodeErrorEnvelope(t, w)["errorCode"])
		}
	}

	// super_admin + 实例不存在 + pageContext：仍按既有 404 可见失败（提示层不改变状态码）。
	gw := service.NewLLMGateway(stubChatProvider{response: "ok"}, nil, nil, "openai").
		WithResolver(&stubChatResolver{err: service.ErrProviderNotFound})
	h2 := ai.NewHandler(newProviderChatService(gw))
	w := postProviderChat(t, h2, "super_admin",
		`{"query":"hello","provider":"ghost","pageContext":{"pageKey":"ticket.detail","route":"/tickets/42"}}`,
		false)
	require.Equal(t, http.StatusNotFound, w.Code, "body=%s", w.Body.String())
	assert.Equal(t, "AI_PROVIDER_NOT_FOUND", decodeErrorEnvelope(t, w)["errorCode"])
}
