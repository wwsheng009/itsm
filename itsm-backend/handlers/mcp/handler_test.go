package mcp

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"itsm-backend/common"
	"itsm-backend/ent/enttest"
	_ "itsm-backend/ent/runtime"
	"itsm-backend/mcp/admin"
	"itsm-backend/mcp/manager"

	"github.com/gin-gonic/gin"
	_ "github.com/mattn/go-sqlite3"
	"github.com/stretchr/testify/require"
)

// 处理器层测试（M0-08）：httptest + ent 测试库 + 假会话；覆盖信封、掩码、202 与错误码映射。

func newHandlerEngine(t *testing.T) *gin.Engine {
	t.Helper()
	gin.SetMode(gin.TestMode)

	dsn := filepath.Join(t.TempDir(), "mcp-handler-test.db") + "?_fk=1&_busy_timeout=15000&_journal_mode=WAL"
	client := enttest.Open(t, "sqlite3", dsn)
	t.Cleanup(func() { _ = client.Close() })
	db, err := sql.Open("sqlite3", dsn)
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })

	credentials, err := admin.NewCredentialService("unit-test-mcp-handler-key")
	require.NoError(t, err)

	// 闸门 dialer：Enable 返回后保持 connecting（断言 202 语义），清理时放行避免 goroutine 泄漏。
	gate := make(chan struct{})
	t.Cleanup(func() { close(gate) })
	managerInstance := manager.New(manager.Options{
		Dial: func(context.Context, manager.ServerConfig) (manager.ToolSession, error) {
			<-gate
			return nil, errors.New("handler 测试不建连")
		},
		HealthInterval: time.Hour,
	})

	service, err := admin.NewService(admin.Config{
		Client:      client,
		Credentials: credentials,
		Manager:     managerInstance,
	})
	require.NoError(t, err)
	handler := NewHandler(service)

	engine := gin.New()
	api := engine.Group("/api/v1")
	api.Use(func(c *gin.Context) {
		c.Set("tenant_id", 1)
		c.Set("user_id", 7)
		c.Next()
	})
	group := api.Group("/ai")
	{
		group.GET("/mcp-servers", handler.ListServers)
		group.POST("/mcp-servers", handler.CreateServer)
		group.GET("/mcp-servers/:id", handler.GetServer)
		group.POST("/mcp-servers/:id/enable", handler.EnableServer)
		group.POST("/mcp-servers/:id/tools/:callable/enable", handler.SetToolEnabled)
	}
	return engine
}

func doJSON(t *testing.T, engine *gin.Engine, method, path string, body any) *httptest.ResponseRecorder {
	t.Helper()
	var reader *bytes.Reader
	if body != nil {
		payload, err := json.Marshal(body)
		require.NoError(t, err)
		reader = bytes.NewReader(payload)
	} else {
		reader = bytes.NewReader(nil)
	}
	request := httptest.NewRequest(method, path, reader)
	request.Header.Set("Content-Type", "application/json")
	recorder := httptest.NewRecorder()
	engine.ServeHTTP(recorder, request)
	return recorder
}

func decodeEnvelope(t *testing.T, recorder *httptest.ResponseRecorder) map[string]any {
	t.Helper()
	var payload map[string]any
	require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &payload))
	return payload
}

func TestHandler_CreateGet_MaskingAndEnvelope(t *testing.T) {
	engine := newHandlerEngine(t)

	recorder := doJSON(t, engine, http.MethodPost, "/api/v1/ai/mcp-servers", map[string]any{
		"name":         "github",
		"display_name": "GitHub",
		"transport":    "streamable",
		"url":          "https://mcp.example.com/mcp",
		"headers":      map[string]string{"Authorization": "Bearer super-secret-token-123"},
	})
	require.Equal(t, http.StatusOK, recorder.Code)
	payload := decodeEnvelope(t, recorder)
	require.EqualValues(t, 0, payload["code"])
	data := payload["data"].(map[string]any)
	require.Equal(t, "github", data["name"])
	require.Equal(t, false, data["enabled"], "默认禁用（D7）")
	require.Equal(t, "configured", data["status"])
	masked := data["headers_masked"].(map[string]any)
	require.Equal(t, "Bear****23", masked["Authorization"])
	require.NotContains(t, recorder.Body.String(), "super-secret-token-123")

	// GET 详情同样不回显明文。
	recorder = doJSON(t, engine, http.MethodGet, "/api/v1/ai/mcp-servers/1", nil)
	require.Equal(t, http.StatusOK, recorder.Code)
	require.NotContains(t, recorder.Body.String(), "super-secret-token-123")
}

func TestHandler_DuplicateAndNotFoundErrorEnvelope(t *testing.T) {
	engine := newHandlerEngine(t)
	create := map[string]any{
		"name": "github", "transport": "streamable", "url": "https://mcp.example.com/mcp",
	}
	require.Equal(t, http.StatusOK, doJSON(t, engine, http.MethodPost, "/api/v1/ai/mcp-servers", create).Code)

	recorder := doJSON(t, engine, http.MethodPost, "/api/v1/ai/mcp-servers", create)
	require.Equal(t, http.StatusConflict, recorder.Code)
	payload := decodeEnvelope(t, recorder)
	require.Equal(t, "duplicate_name", payload["errorCode"])
	require.EqualValues(t, common.ConflictCode, payload["code"])

	recorder = doJSON(t, engine, http.MethodGet, "/api/v1/ai/mcp-servers/999", nil)
	require.Equal(t, http.StatusNotFound, recorder.Code)
	payload = decodeEnvelope(t, recorder)
	require.Equal(t, "not_found", payload["errorCode"])
	require.EqualValues(t, common.NotFoundCode, payload["code"])

	recorder = doJSON(t, engine, http.MethodPost, "/api/v1/ai/mcp-servers/1/tools/mcp__github__nope/enable", nil)
	require.Equal(t, http.StatusNotFound, recorder.Code)
	require.Equal(t, "not_found", decodeEnvelope(t, recorder)["errorCode"])
}

func TestHandler_EnableReturns202WithConnectingStatus(t *testing.T) {
	engine := newHandlerEngine(t)
	create := map[string]any{
		"name": "github", "transport": "streamable", "url": "https://mcp.example.com/mcp",
	}
	require.Equal(t, http.StatusOK, doJSON(t, engine, http.MethodPost, "/api/v1/ai/mcp-servers", create).Code)

	recorder := doJSON(t, engine, http.MethodPost, "/api/v1/ai/mcp-servers/1/enable", nil)
	require.Equal(t, http.StatusAccepted, recorder.Code, "D8：异步启停返回 202 + 状态回读")
	payload := decodeEnvelope(t, recorder)
	require.Equal(t, "accepted", payload["message"])
	data := payload["data"].(map[string]any)
	require.Equal(t, true, data["enabled"])
	require.Equal(t, "connecting", data["running_status"], "202 响应必须携带 connecting 供前端轮询")

	// 轮询回读：GET /:id 可用（状态仍为 connecting 或 error）。
	recorder = doJSON(t, engine, http.MethodGet, "/api/v1/ai/mcp-servers/1", nil)
	require.Equal(t, http.StatusOK, recorder.Code)
}

func TestHandler_ValidationErrorEnvelope(t *testing.T) {
	engine := newHandlerEngine(t)
	recorder := doJSON(t, engine, http.MethodPost, "/api/v1/ai/mcp-servers", map[string]any{
		"name": "github", "transport": "stdio", "url": "https://mcp.example.com/mcp",
	})
	require.Equal(t, http.StatusBadRequest, recorder.Code)
	require.Equal(t, "invalid_transport", decodeEnvelope(t, recorder)["errorCode"])
}
