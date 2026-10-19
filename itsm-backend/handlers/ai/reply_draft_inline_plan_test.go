package ai_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"

	"itsm-backend/ent/enttest"
	"itsm-backend/handlers/ai"
	"itsm-backend/service"
	"itsm-backend/service/bot"
)

// V0-03：Inline 任务卡经 `POST /agent/tools/execute` 直达执行 plan 工具
// （analyze_ci_impact_plan）——只读 + 产物归属调用者 + 零业务写入。
func TestExecuteTool_RunsCIImpactPlanInline(t *testing.T) {
	ctx := context.Background()
	client := enttest.Open(t, "sqlite3", "file:tool_contract_plan?mode=memory&cache=shared&_fk=1")
	t.Cleanup(func() { _ = client.Close() })

	tenant := client.Tenant.Create().SetName("Plan Tools").SetCode("plan-tools").SetDomain("plan.test").SaveX(ctx)
	user := client.User.Create().
		SetUsername("planner").SetEmail("planner@example.com").SetName("Planner").
		SetPasswordHash("hash").SetTenantID(tenant.ID).SaveX(ctx)
	ciType := client.CIType.Create().SetName("服务器").SetTenantID(tenant.ID).SaveX(ctx)
	ci := client.ConfigurationItem.Create().
		SetTenantID(tenant.ID).SetCiNumber("CI-V003-1").SetName("核心网关").
		SetCiTypeID(ciType.ID).SetCiType("server").SaveX(ctx)

	registry := service.NewToolRegistry(nil, nil, nil, client)
	registry.SetArtifactStore(bot.NewArtifactStore(client))
	svc := ai.NewService(ai.NewEntRepository(client), zap.NewNop().Sugar(), nil, registry, nil, nil, nil, nil, nil, nil, nil)
	svc.SetEntClient(client)
	h := ai.NewHandler(svc)

	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.POST("/api/v1/agent/tools/execute", func(c *gin.Context) {
		c.Set("tenant_id", tenant.ID)
		c.Set("user_id", user.ID)
		c.Set("role", "super_admin")
		h.ExecuteTool(c)
	})

	body := fmt.Sprintf(
		`{"name":"analyze_ci_impact_plan","args":{"ciId":%d,"change":"升级网关固件","summary":"Inline 任务卡"}}`,
		ci.ID,
	)
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/agent/tools/execute", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)

	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	var response struct {
		Code int `json:"code"`
		Data struct {
			Status string `json:"status"`
			Data   struct {
				ArtifactID int    `json:"artifactId"`
				Kind       string `json:"kind"`
				Content    struct {
					Checklist []string `json:"checklist"`
				} `json:"content"`
			} `json:"data"`
		} `json:"data"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &response), w.Body.String())
	require.Zero(t, response.Code)
	require.Equal(t, "success", response.Data.Status, "plan 工具为只读：直接执行，不进审批队列")
	require.NotZero(t, response.Data.Data.ArtifactID)
	require.Equal(t, bot.ArtifactKindAnalysis, response.Data.Data.Kind)
	require.NotEmpty(t, response.Data.Data.Content.Checklist)

	// 产物归属调用者与租户（Inline 数据面与聊天链路口径一致）。
	artifact := client.BotArtifact.GetX(ctx, response.Data.Data.ArtifactID)
	require.Equal(t, tenant.ID, artifact.TenantID)
	require.Equal(t, user.ID, artifact.OwnerUserID)
}
