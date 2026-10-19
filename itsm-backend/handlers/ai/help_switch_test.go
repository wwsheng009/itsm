package ai

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"itsm-backend/capability"
	"itsm-backend/common"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

// T5-05：help.agent.enabled=false 时帮助数据面 fail-closed（403）。
// 未注入能力源/读取失败时按产品默认开启（由 capability 包测试覆盖）。
func TestHelpDataPlaneHonorsHelpAgentSwitch(t *testing.T) {
	capability.SetRuntimeSource(capability.NewConfigSource(nil, capability.Defaults{
		HelpAgentEnabled: false,
	}, nil))
	t.Cleanup(func() { capability.SetRuntimeSource(nil) })

	gin.SetMode(gin.TestMode)
	handler := NewHandler(nil)
	router := gin.New()
	router.GET("/help/search", func(c *gin.Context) {
		c.Set("tenant_id", 7)
		handler.SearchHelp(c)
	})
	router.GET("/help/page", func(c *gin.Context) {
		c.Set("tenant_id", 7)
		handler.GetPageHelp(c)
	})

	for _, path := range []string{"/help/search?q=x", "/help/page?pageKey=ticket.detail"} {
		recorder := httptest.NewRecorder()
		router.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, path, nil))
		require.Equal(t, http.StatusForbidden, recorder.Code, path)

		var envelope struct {
			Code int    `json:"code"`
			Msg  string `json:"message"`
		}
		require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &envelope))
		require.Equal(t, common.ForbiddenCode, envelope.Code)
		require.Contains(t, envelope.Msg, "帮助智能体已关闭")
	}
}
