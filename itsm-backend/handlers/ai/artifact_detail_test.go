package ai

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strconv"
	"testing"

	_ "github.com/mattn/go-sqlite3"

	"itsm-backend/ent/enttest"
	"itsm-backend/service/bot"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

// I0-04：产物详情只读端点 —— 归属收敛（租户 + 发起人）与响应契约。
func TestGetArtifact_OwnerScopedDetail(t *testing.T) {
	client := enttest.Open(t, "sqlite3", filepath.Join(t.TempDir(), "artifact-detail.db")+"?_fk=1")
	t.Cleanup(func() { _ = client.Close() })
	store := bot.NewArtifactStore(client)
	ctx := context.Background()

	tenantA := client.Tenant.Create().SetCode("artifact-a").SetName("A").SaveX(ctx)
	tenantB := client.Tenant.Create().SetCode("artifact-b").SetName("B").SaveX(ctx)
	owner := client.User.Create().
		SetUsername("artifact-owner").SetEmail("artifact-owner@example.com").SetName("Owner").
		SetPasswordHash("x").SetTenantID(tenantA.ID).SaveX(ctx)
	other := client.User.Create().
		SetUsername("artifact-other").SetEmail("artifact-other@example.com").SetName("Other").
		SetPasswordHash("x").SetTenantID(tenantA.ID).SaveX(ctx)

	row, err := store.Create(ctx, bot.ArtifactInput{
		TenantID: tenantA.ID, OwnerUserID: owner.ID, ConversationID: 7,
		Kind: bot.ArtifactKindPlan, ToolName: "draft_ticket_fields",
		Title: "事件单字段草案",
		Content: map[string]interface{}{
			"fields": map[string]interface{}{"title": "打印机故障"},
		},
		Evidence: map[string]interface{}{"source": "user_description"},
	})
	require.NoError(t, err)
	_, err = store.Create(ctx, bot.ArtifactInput{
		TenantID: tenantB.ID, OwnerUserID: owner.ID,
		Kind: bot.ArtifactKindAnalysis, ToolName: "analyze_ci_impact_plan", Title: "B tenant artifact",
	})
	require.NoError(t, err)

	gin.SetMode(gin.TestMode)
	engine := gin.New()
	engine.GET("/api/v1/ai/artifacts/:id", func(c *gin.Context) {
		c.Set("tenant_id", tenantA.ID)
		c.Set("user_id", owner.ID)
		NewHandler(&Service{artifacts: store}).GetArtifact(c)
	})
	recorder := httptest.NewRecorder()
	engine.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/api/v1/ai/artifacts/"+strconv.Itoa(row.ID), nil))
	require.Equal(t, http.StatusOK, recorder.Code)

	var payload struct {
		Code int            `json:"code"`
		Data artifactDetail `json:"data"`
	}
	require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &payload))
	require.Equal(t, 0, payload.Code)
	require.Equal(t, row.ID, payload.Data.ID)
	require.Equal(t, "事件单字段草案", payload.Data.Title)
	require.Equal(t, bot.ArtifactKindPlan, payload.Data.Kind)
	require.Equal(t, 7, payload.Data.ConversationID)
	require.Contains(t, string(payload.Data.Content), "打印机故障")

	// 同租户但非归属人 → 404（不泄露存在性）。
	engine = gin.New()
	engine.GET("/api/v1/ai/artifacts/:id", func(c *gin.Context) {
		c.Set("tenant_id", tenantA.ID)
		c.Set("user_id", other.ID)
		NewHandler(&Service{artifacts: store}).GetArtifact(c)
	})
	recorder = httptest.NewRecorder()
	engine.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/api/v1/ai/artifacts/"+strconv.Itoa(row.ID), nil))
	require.Equal(t, http.StatusNotFound, recorder.Code)

	// 跨租户 → 404。
	engine = gin.New()
	engine.GET("/api/v1/ai/artifacts/:id", func(c *gin.Context) {
		c.Set("tenant_id", tenantB.ID)
		c.Set("user_id", owner.ID)
		NewHandler(&Service{artifacts: store}).GetArtifact(c)
	})
	recorder = httptest.NewRecorder()
	engine.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/api/v1/ai/artifacts/"+strconv.Itoa(row.ID), nil))
	require.Equal(t, http.StatusNotFound, recorder.Code)

	// 非法 id → 400。
	recorder = httptest.NewRecorder()
	engine.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/api/v1/ai/artifacts/abc", nil))
	require.Equal(t, http.StatusBadRequest, recorder.Code)

	// 存储未装配 → 503。
	engine = gin.New()
	engine.GET("/api/v1/ai/artifacts/:id", func(c *gin.Context) {
		c.Set("tenant_id", tenantA.ID)
		c.Set("user_id", owner.ID)
		NewHandler(&Service{}).GetArtifact(c)
	})
	recorder = httptest.NewRecorder()
	engine.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/api/v1/ai/artifacts/"+strconv.Itoa(row.ID), nil))
	require.Equal(t, http.StatusServiceUnavailable, recorder.Code)
}
