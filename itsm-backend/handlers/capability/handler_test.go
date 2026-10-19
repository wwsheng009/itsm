package capability

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	caproot "itsm-backend/capability"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

// runtimeStubSource 是运行时能力源的最小替身（V2-01 个性化记忆租户开关）。
type runtimeStubSource struct{ snap caproot.Snapshot }

func (s runtimeStubSource) For(context.Context, int) caproot.Snapshot { return s.snap }
func (s runtimeStubSource) Invalidate(int)                            {}

// V2-01/T5-05：公共能力面下发租户级 assistant 策略：
//   - 注入源且租户关闭 → 对应字段 false；
//   - 未注入源（测试/未装配）→ 产品默认 true（fail-safe，不阻断）。
func TestHandlerExposesAssistantPolicies(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.GET("/capabilities", func(c *gin.Context) {
		c.Set("tenant_id", 7)
		Handler(c)
	})

	type assistantPolicy struct {
		MemoryEnabled bool `json:"memoryEnabled"`
		DockEnabled   bool `json:"dockEnabled"`
		HelpEnabled   bool `json:"helpEnabled"`
	}
	read := func() assistantPolicy {
		recorder := httptest.NewRecorder()
		router.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/capabilities", nil))
		require.Equal(t, http.StatusOK, recorder.Code)
		var response struct {
			Data struct {
				Assistant assistantPolicy `json:"assistant"`
			} `json:"data"`
		}
		require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &response))
		return response.Data.Assistant
	}

	t.Cleanup(func() { caproot.SetRuntimeSource(nil) })
	caproot.SetRuntimeSource(nil)
	require.Equal(t, assistantPolicy{MemoryEnabled: true, DockEnabled: true, HelpEnabled: true}, read(),
		"未注入源 → 全部按产品默认开启")

	caproot.SetRuntimeSource(runtimeStubSource{snap: caproot.Snapshot{
		AssistantMemoryEnabled: false,
		AssistantDockEnabled:   false,
		HelpAgentEnabled:       false,
	}})
	require.Equal(t, assistantPolicy{}, read(), "租户关闭 → 三个策略均为 false")

	caproot.SetRuntimeSource(runtimeStubSource{snap: caproot.Snapshot{
		AssistantMemoryEnabled: true,
		AssistantDockEnabled:   true,
		HelpAgentEnabled:       true,
	}})
	require.Equal(t, assistantPolicy{MemoryEnabled: true, DockEnabled: true, HelpEnabled: true}, read())
}

func TestHandlerReturnsTenantAwareCapabilityContract(t *testing.T) {
	gin.SetMode(gin.TestMode)
	t.Setenv("ITSM_FEISHU_READY", "false")

	router := gin.New()
	router.GET("/capabilities", func(c *gin.Context) {
		c.Set("tenant_id", 7)
		c.Set("role", "admin")
		Handler(c)
	})

	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/capabilities", nil))
	require.Equal(t, http.StatusOK, recorder.Code)

	var response struct {
		Code int `json:"code"`
		Data struct {
			Items []Capability `json:"items"`
		} `json:"data"`
	}
	require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &response))
	require.Zero(t, response.Code)
	require.NotEmpty(t, response.Data.Items)

	byKey := make(map[string]Capability)
	for _, item := range response.Data.Items {
		byKey[item.Key] = item
	}
	require.Equal(t, MaturityPilot, byKey["marketplace"].Maturity)
	require.Equal(t, []string{"read"}, byKey["marketplace"].AllowedActions)
	require.False(t, byKey["connector.feishu"].DeploymentReady)
	require.NotEmpty(t, byKey["connector.feishu"].DegradedReason)
	require.Equal(t, MaturityDisabled, byKey["identity.oidc"].Maturity)
	require.Empty(t, byKey["identity.oidc"].AllowedActions)
}

func TestHandlerDoesNotGrantActionsWithoutTenant(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.GET("/capabilities", Handler)
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/capabilities", nil))

	var response struct {
		Data struct {
			Items []Capability `json:"items"`
		} `json:"data"`
	}
	require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &response))
	for _, item := range response.Data.Items {
		require.False(t, item.TenantReady)
		require.Empty(t, item.AllowedActions)
	}
}
