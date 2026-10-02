package middleware

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"

	_ "github.com/mattn/go-sqlite3"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"itsm-backend/ent/enttest"
	"itsm-backend/ent/auditlog"
	"itsm-backend/ent/tenant"
	"itsm-backend/ent/user"
)

// TestMSPMiddleware_Gate verifies that SetMSPEnabled(false) makes the
// /api/v1/msp/* routes return 404 even when no auth or DB state would
// otherwise block them. This is the private-deployment mode gate.
func TestMSPMiddleware_Gate(t *testing.T) {
	gin.SetMode(gin.TestMode)
	client := enttest.Open(t, "sqlite3", "file:msp_gate?mode=memory&_fk=1")
	defer client.Close()

	// enabled by default — middleware should NOT abort on the gate.
	t.Run("enabled passes through", func(t *testing.T) {
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request, _ = http.NewRequest("GET", "/api/v1/msp/customers", nil)
		MSPMiddleware(client)(c)
		// Without a real user the middleware will respond 401 inside, not 404
		// from the gate. We only care the gate did not short-circuit.
		assert.NotEqual(t, http.StatusNotFound, w.Code, "gate should not 404 when enabled")
	})

	t.Run("disabled returns 404", func(t *testing.T) {
		prev := mspEnabled
		SetMSPEnabled(false)
		defer SetMSPEnabled(prev)
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request, _ = http.NewRequest("GET", "/api/v1/msp/customers", nil)
		MSPMiddleware(client)(c)
		assert.Equal(t, http.StatusNotFound, w.Code)
		assert.True(t, c.IsAborted())
		assert.Contains(t, w.Body.String(), "MSP routes are disabled")
	})
}

// TestMSPMiddleware_HeaderChannelUnifiedGuard 验证头通道与路径/请求体共用 mspguard 判定（IP-P0-2）：
// 未分配客户的 X-Customer-Tenant-ID 在中间件层即被拒绝，且带稳定 reasonCode；
// 未知客户租户返回 404（CUSTOMER_TENANT_NOT_FOUND）。
func TestMSPMiddleware_HeaderChannelUnifiedGuard(t *testing.T) {
	gin.SetMode(gin.TestMode)
	SetMSPEnabled(true)
	client := enttest.Open(t, "sqlite3", "file:msp_header_guard?mode=memory&_fk=1")
	defer client.Close()
	ctx := context.Background()

	provider, err := client.Tenant.Create().
		SetName("Provider").SetCode("prov-hdr").SetType(tenant.Type("msp_provider")).SetStatus("active").
		Save(ctx)
	require.NoError(t, err)
	customer, err := client.Tenant.Create().
		SetName("Customer").SetCode("cust-hdr").SetType(tenant.Type("msp_customer")).SetStatus("active").
		SetMspProviderID(provider.ID).
		Save(ctx)
	require.NoError(t, err)
	mspUser, err := client.User.Create().
		SetUsername("msp-hdr").SetEmail("msp-hdr@example.com").SetName("msp-hdr").
		SetPasswordHash("hash").SetTenantID(provider.ID).SetMspRole(user.MspRole("provider_agent")).
		Save(ctx)
	require.NoError(t, err)

	call := func(headerTenantID int) *httptest.ResponseRecorder {
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request = httptest.NewRequest(http.MethodGet, "/api/v1/msp/customers/"+strconv.Itoa(headerTenantID)+"/tickets", nil)
		c.Request.Header.Set("X-Customer-Tenant-ID", strconv.Itoa(headerTenantID))
		c.Set("user_id", mspUser.ID)
		c.Set("tenant_id", provider.ID)
		c.Set("username", "msp-hdr")
		MSPMiddleware(client)(c)
		return w
	}

	t.Run("unallocated existing customer denied with reasonCode", func(t *testing.T) {
		w := call(customer.ID)
		assert.Equal(t, http.StatusForbidden, w.Code)
		assert.Contains(t, w.Body.String(), "MSP_ALLOCATION_REQUIRED")

		// IP-P1-8：头通道拒绝落审计 tenant.scope_denied（source=header，target=客户租户）。
		logs, err := client.AuditLog.Query().
			Where(
				auditlog.ActionEQ("tenant.scope_denied"),
				auditlog.TargetTenantIDEQ(customer.ID),
			).
			All(context.Background())
		assert.NoError(t, err)
		if assert.Len(t, logs, 1) {
			assert.Equal(t, "header", logs[0].Source)
			assert.Equal(t, provider.ID, logs[0].TenantID)
			assert.Equal(t, "msp-hdr", logs[0].ActorAccount)
			assert.Equal(t, http.StatusForbidden, logs[0].StatusCode)
		}
	})

	t.Run("unknown customer tenant returns not found", func(t *testing.T) {
		w := call(999999)
		assert.Equal(t, http.StatusNotFound, w.Code)
		assert.Contains(t, w.Body.String(), "CUSTOMER_TENANT_NOT_FOUND")
	})
}
