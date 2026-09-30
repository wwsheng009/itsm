package systemconfig

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"itsm-backend/capability"
	"itsm-backend/middleware"
)

// M2 能力开关管理端点契约（GET/PUT /system-configs/ai-capabilities）：
//  1. 读：生效值 + 默认值 + 覆盖来源 + 键清单；
//  2. 写：缺省字段不改动、显式布尔写覆盖、reset 恢复跟随默认；未知键 400；
//  3. 未注入能力源 → 503（fail-closed）。

type stubCapabilityAdmin struct {
	snap       capability.Snapshot
	defaults   capability.Defaults
	lastPatch  capability.Patch
	lastReset  []string
	lastTenant int
	operator   string
}

func (s *stubCapabilityAdmin) For(context.Context, int) capability.Snapshot { return s.snap }
func (s *stubCapabilityAdmin) Invalidate(int)                               {}
func (s *stubCapabilityAdmin) Defaults() capability.Defaults                { return s.defaults }

func (s *stubCapabilityAdmin) Update(_ context.Context, tenantID int, patch capability.Patch, operator string) (capability.Snapshot, error) {
	s.lastTenant, s.lastPatch, s.operator = tenantID, patch, operator
	if patch.MCPEnabled != nil {
		s.snap.MCPEnabled = *patch.MCPEnabled
	}
	if patch.MCPWriteEnabled != nil {
		s.snap.MCPWriteEnabled = *patch.MCPWriteEnabled
	}
	if patch.BotEnabled != nil {
		s.snap.BotEnabled = *patch.BotEnabled
	}
	return s.snap, nil
}

func (s *stubCapabilityAdmin) Clear(_ context.Context, tenantID int, keys ...string) (capability.Snapshot, error) {
	s.lastTenant, s.lastReset = tenantID, keys
	for _, key := range keys {
		switch key {
		case capability.KeyMCPEnabled:
			s.snap.MCPEnabled = s.defaults.MCPEnabled
		case capability.KeyMCPWriteEnabled:
			s.snap.MCPWriteEnabled = s.defaults.MCPWriteEnabled
		case capability.KeyBotEnabled:
			s.snap.BotEnabled = s.defaults.BotEnabled
		}
	}
	return s.snap, nil
}

func newCapabilityEndpointHarness(t *testing.T, source capability.Source) *gin.Engine {
	t.Helper()
	handler := NewHandler(nil, nil)
	handler.SetCapabilitySource(source)

	gin.SetMode(gin.TestMode)
	engine := gin.New()
	group := engine.Group("/api/v1")
	group.Use(func(c *gin.Context) {
		if c.Request.Header.Get("X-Test-Tenant") == "" {
			c.Next()
			return
		}
		// 与生产一致：租户上下文经 middleware.TenantContextKey 注入（handler 用 GetTenantID 读取）。
		c.Set(middleware.TenantContextKey, &middleware.TenantContext{TenantID: 7})
		c.Set("username", "admin")
		c.Next()
	})
	group.GET("/system-configs/ai-capabilities", handler.GetAICapabilities)
	group.PUT("/system-configs/ai-capabilities", handler.UpdateAICapabilities)
	return engine
}

func doCapabilityRequest(t *testing.T, engine *gin.Engine, method string, body any, tenant bool) *httptest.ResponseRecorder {
	t.Helper()
	reader := bytes.NewBuffer(nil)
	if body != nil {
		raw, err := json.Marshal(body)
		require.NoError(t, err)
		reader = bytes.NewBuffer(raw)
	}
	request := httptest.NewRequest(method, "/api/v1/system-configs/ai-capabilities", reader)
	request.Header.Set("Content-Type", "application/json")
	if tenant {
		request.Header.Set("X-Test-Tenant", "7")
	}
	recorder := httptest.NewRecorder()
	engine.ServeHTTP(recorder, request)
	return recorder
}

func decodeCapabilityData(t *testing.T, recorder *httptest.ResponseRecorder) map[string]any {
	t.Helper()
	var envelope struct {
		Code int            `json:"code"`
		Data map[string]any `json:"data"`
	}
	require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &envelope), "body=%s", recorder.Body.String())
	require.Equal(t, 0, envelope.Code, "body=%s", recorder.Body.String())
	return envelope.Data
}

func TestAICapabilities_GetExposesEffectiveAndDefaults(t *testing.T) {
	source := &stubCapabilityAdmin{
		snap: capability.Snapshot{
			MCPEnabled: true, MCPWriteEnabled: true, BotEnabled: false,
			Overridden: map[string]bool{capability.KeyMCPWriteEnabled: true},
			UpdatedBy:  "admin",
		},
		defaults: capability.Defaults{MCPEnabled: true, MCPWriteEnabled: false, BotEnabled: true},
	}
	engine := newCapabilityEndpointHarness(t, source)

	recorder := doCapabilityRequest(t, engine, http.MethodGet, nil, true)
	require.Equal(t, http.StatusOK, recorder.Code, recorder.Body.String())
	data := decodeCapabilityData(t, recorder)

	assert.Equal(t, true, data["mcpEnabled"])
	assert.Equal(t, true, data["mcpWriteEnabled"])
	assert.Equal(t, false, data["botEnabled"])

	defaults, ok := data["defaults"].(map[string]any)
	require.True(t, ok, "必须下发默认值（页面展示「跟随环境默认」）")
	assert.Equal(t, false, defaults["mcpWriteEnabled"])

	overridden, ok := data["overridden"].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, true, overridden[capability.KeyMCPWriteEnabled])

	keys, ok := data["keys"].([]any)
	require.True(t, ok)
	assert.Len(t, keys, 3)
	assert.Equal(t, "admin", data["updatedBy"])
}

func TestAICapabilities_PutAppliesPatchAndReset(t *testing.T) {
	source := &stubCapabilityAdmin{
		snap:     capability.Snapshot{MCPEnabled: true, MCPWriteEnabled: false, BotEnabled: true},
		defaults: capability.Defaults{MCPEnabled: true, MCPWriteEnabled: false, BotEnabled: true},
	}
	engine := newCapabilityEndpointHarness(t, source)

	recorder := doCapabilityRequest(t, engine, http.MethodPut, map[string]any{
		"mcpWriteEnabled": true,
		"reset":           []string{capability.KeyMCPEnabled},
	}, true)
	require.Equal(t, http.StatusOK, recorder.Code, recorder.Body.String())

	require.NotNil(t, source.lastPatch.MCPWriteEnabled)
	assert.True(t, *source.lastPatch.MCPWriteEnabled)
	assert.Nil(t, source.lastPatch.MCPEnabled, "缺省字段不得被改动")
	assert.Equal(t, []string{capability.KeyMCPEnabled}, source.lastReset)
	assert.Equal(t, 7, source.lastTenant)
	assert.Equal(t, "admin", source.operator)

	data := decodeCapabilityData(t, recorder)
	assert.Equal(t, true, data["mcpWriteEnabled"], "写后返回最新生效值")
}

func TestAICapabilities_RejectsUnknownResetKeyAndMissingTenant(t *testing.T) {
	source := &stubCapabilityAdmin{defaults: capability.Defaults{MCPEnabled: true}}
	engine := newCapabilityEndpointHarness(t, source)

	recorder := doCapabilityRequest(t, engine, http.MethodPut, map[string]any{
		"reset": []string{"mcp.unknown"},
	}, true)
	require.Equal(t, http.StatusBadRequest, recorder.Code, recorder.Body.String())
	assert.Empty(t, source.lastReset, "未知键必须在写入前拒绝")

	recorder = doCapabilityRequest(t, engine, http.MethodGet, nil, false)
	require.Equal(t, http.StatusUnauthorized, recorder.Code, recorder.Body.String())
}

func TestAICapabilities_UnavailableSourceFailsClosed(t *testing.T) {
	engine := newCapabilityEndpointHarness(t, nil)

	recorder := doCapabilityRequest(t, engine, http.MethodGet, nil, true)
	require.Equal(t, http.StatusServiceUnavailable, recorder.Code, recorder.Body.String())

	recorder = doCapabilityRequest(t, engine, http.MethodPut, map[string]any{"mcpEnabled": true}, true)
	require.Equal(t, http.StatusServiceUnavailable, recorder.Code, recorder.Body.String())
}
