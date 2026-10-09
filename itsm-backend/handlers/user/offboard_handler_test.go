package user

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	_ "github.com/mattn/go-sqlite3"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap/zaptest"

	"itsm-backend/ent"
	"itsm-backend/ent/enttest"
	"itsm-backend/ent/tenant"
	"itsm-backend/service"
)

// =============================================================================
// IP-P2-8：离职编排 handler 映射（200 / 404 / 409 / 非法 ID）+ 服务接线
// =============================================================================

func newOffboardTestClient(t *testing.T) *ent.Client {
	dbName := "file:" + strings.ReplaceAll(t.Name(), "/", "_") + "?mode=memory&cache=shared&_fk=1"
	client := enttest.Open(t, "sqlite3", dbName)
	t.Cleanup(func() { client.Close() })
	return client
}

func setupOffboardRouter(t *testing.T, client *ent.Client, actorID, tenantID int) *gin.Engine {
	gin.SetMode(gin.TestMode)
	logger := zaptest.NewLogger(t).Sugar()
	h := NewHandler(&mockUserService{}, logger)
	h.SetOffboardingService(service.NewOffboardingService(client, service.NewMSPAllocationService(client, logger), logger))

	r := gin.New()
	r.Use(gin.Recovery())
	r.Use(func(c *gin.Context) {
		c.Set("user_id", actorID)
		c.Set("tenant_id", tenantID)
		c.Set("username", "offboard-actor")
		c.Next()
	})
	r.POST("/api/v1/users/:id/offboard", h.OffboardUser)
	return r
}

func TestOffboardUser_Handler_Success(t *testing.T) {
	client := newOffboardTestClient(t)
	ctx := context.Background()

	provider, err := client.Tenant.Create().
		SetName("Provider").SetCode("off-h-prov").SetType(tenant.Type("msp_provider")).SetStatus("active").Save(ctx)
	require.NoError(t, err)
	customer, err := client.Tenant.Create().
		SetName("Customer").SetCode("off-h-cust").SetType(tenant.Type("msp_customer")).
		SetStatus("active").SetMspProviderID(provider.ID).Save(ctx)
	require.NoError(t, err)
	actor, err := client.User.Create().
		SetUsername("off-h-admin").SetEmail("off-h-admin@example.com").SetName("admin").
		SetPasswordHash("x").SetActive(true).SetTenantID(provider.ID).SetRole("admin").Save(ctx)
	require.NoError(t, err)
	staff, err := client.User.Create().
		SetUsername("off-h-staff").SetEmail("off-h-staff@example.com").SetName("staff").
		SetPasswordHash("x").SetActive(true).SetTenantID(provider.ID).SetRole("end_user").Save(ctx)
	require.NoError(t, err)
	_, err = client.MSPAllocation.Create().
		SetMspUserID(staff.ID).SetCustomerTenantID(customer.ID).SetProviderTenantID(provider.ID).
		SetRole("backup").SetAssignedAt(time.Now()).Save(ctx)
	require.NoError(t, err)

	r := setupOffboardRouter(t, client, actor.ID, provider.ID)
	req := httptest.NewRequest(http.MethodPost, fmt.Sprintf("/api/v1/users/%d/offboard", staff.ID), nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	require.Equal(t, http.StatusOK, w.Code)
	var resp struct {
		Code int `json:"code"`
		Data struct {
			UserID             int  `json:"userId"`
			Deactivated        bool `json:"deactivated"`
			AllocationsRevoked int  `json:"allocationsRevoked"`
		} `json:"data"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.Equal(t, 0, resp.Code)
	assert.Equal(t, staff.ID, resp.Data.UserID)
	assert.True(t, resp.Data.Deactivated)
	assert.Equal(t, 1, resp.Data.AllocationsRevoked)

	u, err := client.User.Get(ctx, staff.ID)
	require.NoError(t, err)
	assert.False(t, u.Active)
}

func TestOffboardUser_Handler_SelfConflict(t *testing.T) {
	client := newOffboardTestClient(t)
	ctx := context.Background()
	provider, err := client.Tenant.Create().
		SetName("Provider").SetCode("off-h2-prov").SetType(tenant.Type("msp_provider")).SetStatus("active").Save(ctx)
	require.NoError(t, err)
	actor, err := client.User.Create().
		SetUsername("off-h2-admin").SetEmail("off-h2-admin@example.com").SetName("admin").
		SetPasswordHash("x").SetActive(true).SetTenantID(provider.ID).SetRole("admin").Save(ctx)
	require.NoError(t, err)

	r := setupOffboardRouter(t, client, actor.ID, provider.ID)
	req := httptest.NewRequest(http.MethodPost, fmt.Sprintf("/api/v1/users/%d/offboard", actor.ID), nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	assert.Equal(t, http.StatusConflict, w.Code, "自离职必须 409")
}

func TestOffboardUser_Handler_NotFoundAndBadID(t *testing.T) {
	client := newOffboardTestClient(t)
	ctx := context.Background()
	provider, err := client.Tenant.Create().
		SetName("Provider").SetCode("off-h3-prov").SetType(tenant.Type("msp_provider")).SetStatus("active").Save(ctx)
	require.NoError(t, err)
	actor, err := client.User.Create().
		SetUsername("off-h3-admin").SetEmail("off-h3-admin@example.com").SetName("admin").
		SetPasswordHash("x").SetActive(true).SetTenantID(provider.ID).SetRole("admin").Save(ctx)
	require.NoError(t, err)

	r := setupOffboardRouter(t, client, actor.ID, provider.ID)

	// 目标不存在 → 404
	req := httptest.NewRequest(http.MethodPost, "/api/v1/users/999999/offboard", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	assert.Equal(t, http.StatusNotFound, w.Code)

	// 非法 ID → 业务码 1001（沿用既有 handler 口径）
	req2 := httptest.NewRequest(http.MethodPost, "/api/v1/users/abc/offboard", nil)
	w2 := httptest.NewRecorder()
	r.ServeHTTP(w2, req2)
	var resp struct {
		Code int `json:"code"`
	}
	require.NoError(t, json.Unmarshal(w2.Body.Bytes(), &resp))
	assert.Equal(t, 1001, resp.Code)
}
