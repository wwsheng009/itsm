package mcp

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	_ "github.com/mattn/go-sqlite3"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"itsm-backend/capability"
	"itsm-backend/ent/enttest"
	"itsm-backend/mcp/admin"
	"itsm-backend/mcp/manager"
)

// M2 能力开关：MCP 管理面的运行时门禁契约（2026-09-30 方案 §2.3 G5）。
//
//  1. mcp.enabled=false → 治理写端 403（中间件 RequireEnabled），读端保留；
//  2. 列表响应携带 capabilities 块（与门禁同源）；
//  3. 开启时行为与既有完全一致。

type stubCapSource struct {
	snap capability.Snapshot
}

func (s stubCapSource) For(context.Context, int) capability.Snapshot { return s.snap }
func (s stubCapSource) Invalidate(int)                               {}

func newCapabilityGatedEngine(t *testing.T, caps capability.Source) *gin.Engine {
	t.Helper()
	gin.SetMode(gin.TestMode)

	dsn := filepath.Join(t.TempDir(), "mcp-capability-handler.db") + "?_fk=1&_busy_timeout=15000&_journal_mode=WAL"
	client := enttest.Open(t, "sqlite3", dsn)
	t.Cleanup(func() { _ = client.Close() })

	credentials, err := admin.NewCredentialService("unit-test-mcp-capability-key")
	require.NoError(t, err)

	managerInstance := manager.New(manager.Options{
		Dial: func(context.Context, manager.ServerConfig) (manager.ToolSession, error) {
			return nil, context.Canceled // 本组用例不建连
		},
		HealthInterval: time.Hour,
	})
	service, err := admin.NewService(admin.Config{Client: client, Credentials: credentials, Manager: managerInstance})
	require.NoError(t, err)

	handler := NewHandler(service)
	handler.SetCapabilitySource(caps)

	engine := gin.New()
	api := engine.Group("/api/v1")
	api.Use(func(c *gin.Context) {
		c.Set("tenant_id", 1)
		c.Set("user_id", 7)
		c.Next()
	})
	read := api.Group("/ai")
	read.GET("/mcp-servers", handler.ListServers)
	adminGroup := api.Group("/ai", handler.RequireEnabled())
	adminGroup.POST("/mcp-servers", handler.CreateServer)
	return engine
}

func doCapabilityJSON(t *testing.T, engine *gin.Engine, method, path string, body any) *httptest.ResponseRecorder {
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

func TestMCPHandler_CapabilityDisabledGatesWritesKeepsReads(t *testing.T) {
	engine := newCapabilityGatedEngine(t, stubCapSource{snap: capability.Snapshot{
		MCPEnabled: false, MCPWriteEnabled: false, BotEnabled: true,
	}})

	// 读端保留：列表 200 + capabilities（页面展示「已关闭」与重新开启入口）。
	recorder := doCapabilityJSON(t, engine, http.MethodGet, "/api/v1/ai/mcp-servers", nil)
	require.Equal(t, http.StatusOK, recorder.Code, recorder.Body.String())
	var envelope struct {
		Code int `json:"code"`
		Data struct {
			Capabilities *admin.CapabilityView `json:"capabilities"`
		} `json:"data"`
	}
	require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &envelope), "body=%s", recorder.Body.String())
	require.NotNil(t, envelope.Data.Capabilities, "列表必须下发 capabilities 块")
	assert.False(t, envelope.Data.Capabilities.MCPEnabled)
	assert.True(t, envelope.Data.Capabilities.BotEnabled)

	// 治理写端 403（中间件在进入 handler 前拦截）。
	recorder = doCapabilityJSON(t, engine, http.MethodPost, "/api/v1/ai/mcp-servers", map[string]any{
		"name": "blocked", "transport": "streamable", "url": "https://mcp.example.com/mcp",
	})
	require.Equal(t, http.StatusForbidden, recorder.Code, recorder.Body.String())
	assert.Contains(t, recorder.Body.String(), "mcp.enabled=false")
}

func TestMCPHandler_CapabilityEnabledAllowsWrites(t *testing.T) {
	engine := newCapabilityGatedEngine(t, stubCapSource{snap: capability.Snapshot{
		MCPEnabled: true, MCPWriteEnabled: true, BotEnabled: true,
	}})

	recorder := doCapabilityJSON(t, engine, http.MethodPost, "/api/v1/ai/mcp-servers", map[string]any{
		"name": "allowed", "transport": "streamable", "url": "https://mcp.example.com/mcp",
	})
	// 创建成功（201）或至少非 403：本用例只锁「门禁不误伤」。
	require.NotEqual(t, http.StatusForbidden, recorder.Code, recorder.Body.String())
}
