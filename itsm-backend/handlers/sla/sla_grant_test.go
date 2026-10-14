package sla

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	_ "github.com/mattn/go-sqlite3"

	"itsm-backend/ent"
	"itsm-backend/ent/enttest"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap/zaptest"
)

// =====================================================================
// P0-3: SLA 下发授权（D-SLA-1）——三件套 / provided / 护栏 / Atoi 400
// =====================================================================

type grantFixture struct {
	client   *ent.Client
	svc      *Service
	ctx      context.Context
	provider *ent.Tenant
	other    *ent.Tenant
	customer *ent.Tenant
	def      *ent.SLADefinition
}

func setupSLAGrantFixture(t *testing.T) *grantFixture {
	t.Helper()
	gin.SetMode(gin.TestMode)
	dsn := "file:" + filepath.Join(t.TempDir(), "sla_grant_test.db") + "?_fk=1"
	client := enttest.Open(t, "sqlite3", dsn)
	t.Cleanup(func() { _ = client.Close() })
	logger := zaptest.NewLogger(t).Sugar()
	ctx := context.Background()
	uid := slaUniqueID()

	provider, err := client.Tenant.Create().
		SetName("Grant Provider").SetCode("gprov" + uid).SetType("msp_provider").SetStatus("active").Save(ctx)
	require.NoError(t, err)
	other, err := client.Tenant.Create().
		SetName("Other Provider").SetCode("gother" + uid).SetType("msp_provider").SetStatus("active").Save(ctx)
	require.NoError(t, err)
	customer, err := client.Tenant.Create().
		SetName("Grant Customer").SetCode("gcust" + uid).SetType("msp_customer").
		SetMspProviderID(provider.ID).SetStatus("active").Save(ctx)
	require.NoError(t, err)
	def, err := client.SLADefinition.Create().
		SetName("Grant Def").SetServiceType("incident").SetPriority("high").
		SetResponseTime(60).SetResolutionTime(480).SetIsActive(true).
		SetTenantID(provider.ID).SetOwnerProviderTenantID(provider.ID).Save(ctx)
	require.NoError(t, err)

	svc := NewService(NewEntRepository(client), logger)
	return &grantFixture{client: client, svc: svc, ctx: ctx, provider: provider, other: other, customer: customer, def: def}
}

// 三件套之一：customer 归属不匹配（msp_provider_id != provider）→ 拒绝。
func TestSLAGrant_ProviderMismatchForbidden(t *testing.T) {
	f := setupSLAGrantFixture(t)

	_, err := f.svc.CreateGrant(f.ctx, &SLADefinitionGrant{
		SLADefinitionID:  f.def.ID,
		CustomerTenantID: f.customer.ID,
		ProviderTenantID: f.other.ID, // 冒名其他 provider
		TenantID:         f.other.ID,
	})
	require.Error(t, err, "非归属 provider 必须拒绝（TOCTOU/越权面）")
	assert.Contains(t, err.Error(), "不属于当前服务商")
}

// 三件套之三：deactivate-then-activate——同 customer+type 连发两条，旧的自动停用。
func TestSLAGrant_DeactivateThenActivate(t *testing.T) {
	f := setupSLAGrantFixture(t)

	defB, err := f.client.SLADefinition.Create().
		SetName("Grant Def B").SetServiceType("incident").SetPriority("medium").
		SetResponseTime(30).SetResolutionTime(240).SetIsActive(true).
		SetTenantID(f.provider.ID).SetOwnerProviderTenantID(f.provider.ID).Save(f.ctx)
	require.NoError(t, err)

	g1, err := f.svc.CreateGrant(f.ctx, &SLADefinitionGrant{
		SLADefinitionID: f.def.ID, CustomerTenantID: f.customer.ID,
		ProviderTenantID: f.provider.ID, TicketTypeID: 0, TenantID: f.provider.ID,
	})
	require.NoError(t, err)
	require.True(t, g1.IsActive)

	g2, err := f.svc.CreateGrant(f.ctx, &SLADefinitionGrant{
		SLADefinitionID: defB.ID, CustomerTenantID: f.customer.ID,
		ProviderTenantID: f.provider.ID, TicketTypeID: 0, TenantID: f.provider.ID,
	})
	require.NoError(t, err, "同客户同类型的第二条授权必须能创建（旧授权自动停用，partial unique 不阻塞）")

	active, err := f.svc.ListGrants(f.ctx, f.provider.ID, map[string]interface{}{"isActive": true})
	require.NoError(t, err)
	require.Len(t, active, 1, "同 customer+type 同时只有一条生效授权")
	assert.Equal(t, g2.ID, active[0].ID)

	old, err := f.svc.GetGrant(f.ctx, g1.ID, f.provider.ID)
	require.NoError(t, err)
	assert.False(t, old.IsActive, "旧授权必须已被停用")
}

// provided 只读视图：仅 active + 未过期 + 定义 active。
func TestSLAGrant_ProvidedFilters(t *testing.T) {
	f := setupSLAGrantFixture(t)

	// 有效授权
	_, err := f.svc.CreateGrant(f.ctx, &SLADefinitionGrant{
		SLADefinitionID: f.def.ID, CustomerTenantID: f.customer.ID,
		ProviderTenantID: f.provider.ID, TicketTypeID: 7, TenantID: f.provider.ID,
	})
	require.NoError(t, err)

	// 过期授权（不同类型，避免 deactivate 干扰）
	expired := time.Now().Add(-time.Hour)
	_, err = f.svc.CreateGrant(f.ctx, &SLADefinitionGrant{
		SLADefinitionID: f.def.ID, CustomerTenantID: f.customer.ID,
		ProviderTenantID: f.provider.ID, TicketTypeID: 8, ValidUntil: &expired, TenantID: f.provider.ID,
	})
	require.NoError(t, err)

	// 已停用授权的定义
	defC, err := f.client.SLADefinition.Create().
		SetName("Grant Def C").SetResponseTime(10).SetResolutionTime(20).SetIsActive(true).
		SetTenantID(f.provider.ID).Save(f.ctx)
	require.NoError(t, err)
	g3, err := f.svc.CreateGrant(f.ctx, &SLADefinitionGrant{
		SLADefinitionID: defC.ID, CustomerTenantID: f.customer.ID,
		ProviderTenantID: f.provider.ID, TicketTypeID: 9, TenantID: f.provider.ID,
	})
	require.NoError(t, err)
	require.NoError(t, f.svc.DeactivateGrant(f.ctx, g3.ID, f.provider.ID))

	provided, err := f.svc.ListProvidedDefinitions(f.ctx, f.customer.ID)
	require.NoError(t, err)
	require.Len(t, provided, 1, "仅 active 且未过期的授权进入 provided 视图")
	assert.Equal(t, int64(7), provided[0].TicketTypeID)
	assert.Equal(t, f.def.ID, provided[0].SLADefinitionID)
}

// 有效期更新与清空。
func TestSLAGrant_UpdateValidUntilAndClear(t *testing.T) {
	f := setupSLAGrantFixture(t)
	g, err := f.svc.CreateGrant(f.ctx, &SLADefinitionGrant{
		SLADefinitionID: f.def.ID, CustomerTenantID: f.customer.ID,
		ProviderTenantID: f.provider.ID, TenantID: f.provider.ID,
	})
	require.NoError(t, err)
	require.Nil(t, g.ValidUntil)

	until := time.Now().Add(24 * time.Hour)
	updated, err := f.svc.UpdateGrantValidUntil(f.ctx, g.ID, f.provider.ID, &until)
	require.NoError(t, err)
	require.NotNil(t, updated.ValidUntil)

	cleared, err := f.svc.UpdateGrantValidUntil(f.ctx, g.ID, f.provider.ID, nil)
	require.NoError(t, err)
	assert.Nil(t, cleared.ValidUntil)
}

// D-SLA-7 护栏：租户上下文缺失（tenantID<=0）时定义 CRUD fail-closed。
func TestSLADefinition_RequireTenantGuard(t *testing.T) {
	f := setupSLAGrantFixture(t)

	_, err := f.svc.CreateDefinition(f.ctx, &SLADefinition{Name: "No Tenant", TenantID: 0, ResponseTime: 10, ResolutionTime: 20})
	assert.ErrorIs(t, err, ErrTenantRequired)
	_, err = f.svc.GetDefinition(f.ctx, 1, 0)
	assert.ErrorIs(t, err, ErrTenantRequired)
	_, _, err = f.svc.ListDefinitions(f.ctx, 0, 1, 10)
	assert.ErrorIs(t, err, ErrTenantRequired)
	err = f.svc.DeleteDefinition(f.ctx, 1, 0)
	assert.ErrorIs(t, err, ErrTenantRequired)
	_, err = f.svc.ListProvidedDefinitions(f.ctx, 0)
	assert.ErrorIs(t, err, ErrTenantRequired)
}

// Atoi 修复：非法 :id 路径参数 → 400（不再静默变 0）。
func TestSLAHandler_InvalidIDReturns400(t *testing.T) {
	f := setupSLAGrantFixture(t)

	h := NewHandler(f.svc)
	r := gin.New()
	r.Use(slaTestAuth(f.provider.ID, 1))
	r.GET("/definitions/:id", h.GetSLADefinition)
	r.DELETE("/grants/:id", h.DeactivateGrant)

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/definitions/abc", nil)
	r.ServeHTTP(w, req)
	require.Equal(t, http.StatusBadRequest, w.Code)
	var body map[string]interface{}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &body))
	assert.Contains(t, body["message"], "无效的ID参数")

	w2 := httptest.NewRecorder()
	req2, _ := http.NewRequest("DELETE", "/grants/0", nil)
	r.ServeHTTP(w2, req2)
	require.Equal(t, http.StatusBadRequest, w2.Code, "id<=0 同样拒绝")
}
