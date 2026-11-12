package knowledge

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"itsm-backend/common"
	"itsm-backend/ent/enttest"

	"github.com/gin-gonic/gin"
	_ "github.com/mattn/go-sqlite3"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap/zaptest"
)

// 回归真实 demo 发现的问题：无新增文章时，知识增量接口曾返回
// {"items":null,"articles":null}，客户端按数组解析会报协议错误。
func TestListArticlesEmptyIncrementalResponseUsesArrays(t *testing.T) {
	gin.SetMode(gin.TestMode)
	client := enttest.Open(t, "sqlite3", "file:knowledge_list_empty?mode=memory&cache=shared&_fk=1")
	defer client.Close()

	svc := NewService(NewEntRepository(client), zaptest.NewLogger(t).Sugar())
	handler := NewHandler(svc)
	engine := gin.New()
	engine.GET("/api/v1/knowledge-articles", func(c *gin.Context) {
		c.Set("tenant_id", 7)
		handler.ListArticles(c)
	})

	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(
		http.MethodGet,
		"/api/v1/knowledge-articles?updatedSince=2026-11-12T00:00:00Z&page=1&pageSize=2",
		nil,
	)
	engine.ServeHTTP(recorder, request)
	require.Equal(t, http.StatusOK, recorder.Code)

	var payload struct {
		Code int `json:"code"`
		Data struct {
			Items      json.RawMessage `json:"items"`
			Articles   json.RawMessage `json:"articles"`
			Total      int             `json:"total"`
			ServerTime time.Time       `json:"serverTime"`
		} `json:"data"`
	}
	require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &payload))
	require.Equal(t, common.SuccessCode, payload.Code)
	require.JSONEq(t, `[]`, string(payload.Data.Items))
	require.JSONEq(t, `[]`, string(payload.Data.Articles))
	require.Zero(t, payload.Data.Total)
	require.False(t, payload.Data.ServerTime.IsZero(), "serverTime 必须保留")
}
