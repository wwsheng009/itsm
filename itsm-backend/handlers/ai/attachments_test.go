package ai

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

// T5-03a：附件服务未装配时 AI 附件端点 fail-closed（503）。
func TestAIAttachmentEndpointsUnavailable(t *testing.T) {
	gin.SetMode(gin.TestMode)
	h := NewHandler(nil)
	router := gin.New()
	router.POST("/api/v1/ai/attachments", func(c *gin.Context) {
		c.Set("tenant_id", 7)
		c.Set("user_id", 9)
		h.UploadAIAttachment(c)
	})
	router.GET("/api/v1/ai/attachments/:id/content", func(c *gin.Context) {
		c.Set("tenant_id", 7)
		c.Set("user_id", 9)
		h.DownloadAIAttachment(c)
	})

	for _, tc := range []struct {
		method string
		path   string
		body   string
	}{
		{http.MethodPost, "/api/v1/ai/attachments", ""},
		{http.MethodGet, "/api/v1/ai/attachments/1/content", ""},
	} {
		recorder := httptest.NewRecorder()
		router.ServeHTTP(recorder, httptest.NewRequest(tc.method, tc.path, nil))
		require.Equal(t, http.StatusServiceUnavailable, recorder.Code, tc.path)
	}
}
