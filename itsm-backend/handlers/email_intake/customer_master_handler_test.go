package email_intake

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	_ "github.com/mattn/go-sqlite3"
	"github.com/stretchr/testify/require"
	"itsm-backend/ent"
	"itsm-backend/ent/enttest"
	"itsm-backend/middleware"
)

type testEnvelope struct {
	Code    int             `json:"code"`
	Message string          `json:"message"`
	Data    json.RawMessage `json:"data"`
}

func newCustomerMasterHTTPRouter(t *testing.T, client *ent.Client, tenantID, userID int) *gin.Engine {
	t.Helper()
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(func(c *gin.Context) {
		c.Set("role", "super_admin")
		c.Set("tenant_id", tenantID)
		c.Set("user_id", userID)
		c.Set("client", client)
		c.Set(middleware.TenantContextKey, &middleware.TenantContext{TenantID: tenantID})
		c.Next()
	})
	NewHandler(NewService(client)).RegisterRoutes(r.Group("/api/v1"))
	return r
}

func decodeTestEnvelope(t *testing.T, recorder *httptest.ResponseRecorder) testEnvelope {
	t.Helper()
	var envelope testEnvelope
	require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &envelope))
	return envelope
}

func TestCustomerMasterP0_HTTPRouterContractAndTenantIsolation(t *testing.T) {
	client := enttest.Open(t, "sqlite3", "file:customer-master-http?mode=memory&cache=shared&_fk=1")
	defer client.Close()
	ctx := context.Background()
	provider := client.Tenant.Create().
		SetName("http-provider").
		SetCode("http-provider").
		SetType("msp_provider").
		SaveX(ctx)

	routerA := newCustomerMasterHTTPRouter(t, client, provider.ID, 7)

	invalidReq := httptest.NewRequest(http.MethodPost, "/api/v1/master-data/customers", bytes.NewBufferString(`{
		"name": "非法等级客户",
		"tier": "Z"
	}`))
	invalidReq.Header.Set("Content-Type", "application/json")
	invalidRec := httptest.NewRecorder()
	routerA.ServeHTTP(invalidRec, invalidReq)
	require.Equal(t, http.StatusUnprocessableEntity, invalidRec.Code)
	require.Equal(t, 4220, decodeTestEnvelope(t, invalidRec).Code)

	createReq := httptest.NewRequest(http.MethodPost, "/api/v1/master-data/customers", bytes.NewBufferString(`{
		"name": "HTTP 客户",
		"legalName": "HTTP 客户股份有限公司",
		"taxId": "9131-0000-HTTP",
		"emailDomains": ["http.example"],
		"tier": "A",
		"customerType": "direct",
		"lifecycleStage": "active"
	}`))
	createReq.Header.Set("Content-Type", "application/json")
	createRec := httptest.NewRecorder()
	routerA.ServeHTTP(createRec, createReq)
	require.Equal(t, http.StatusOK, createRec.Code)
	createEnvelope := decodeTestEnvelope(t, createRec)
	require.Equal(t, 0, createEnvelope.Code)
	var created customerResponse
	require.NoError(t, json.Unmarshal(createEnvelope.Data, &created))
	require.Equal(t, "HTTP 客户", created.Name)
	require.Equal(t, "CUS-0001", created.Code)
	require.Equal(t, "A", created.Tier)
	require.Equal(t, "91310000HTTP", created.NormalizedTaxID)

	contactReq := httptest.NewRequest(http.MethodPost, "/api/v1/master-data/customers/1/contacts", bytes.NewBufferString(`{
		"name": "HTTP 联系人",
		"email": "HTTP@EXAMPLE.COM",
		"contactRole": "technical",
		"isPrimary": true
	}`))
	contactReq.Header.Set("Content-Type", "application/json")
	contactRec := httptest.NewRecorder()
	routerA.ServeHTTP(contactRec, contactReq)
	require.Equal(t, http.StatusOK, contactRec.Code)
	contactEnvelope := decodeTestEnvelope(t, contactRec)
	require.Equal(t, 0, contactEnvelope.Code)
	var contact customerContactResponse
	require.NoError(t, json.Unmarshal(contactEnvelope.Data, &contact))
	require.Equal(t, "http@example.com", contact.NormalizedEmail)

	detailReq := httptest.NewRequest(http.MethodGet, "/api/v1/master-data/customers/1", nil)
	detailRec := httptest.NewRecorder()
	routerA.ServeHTTP(detailRec, detailReq)
	require.Equal(t, http.StatusOK, detailRec.Code)
	detailEnvelope := decodeTestEnvelope(t, detailRec)
	require.Equal(t, 0, detailEnvelope.Code)
	var detail struct {
		Customer customerResponse          `json:"customer"`
		Contacts []customerContactResponse `json:"contacts"`
	}
	require.NoError(t, json.Unmarshal(detailEnvelope.Data, &detail))
	require.Equal(t, 1, detail.Customer.ContactCount)
	require.Len(t, detail.Contacts, 1)

	listReq := httptest.NewRequest(http.MethodGet, "/api/v1/master-data/customers?keyword=HTTP&page=1&pageSize=10", nil)
	listRec := httptest.NewRecorder()
	routerA.ServeHTTP(listRec, listReq)
	require.Equal(t, http.StatusOK, listRec.Code)
	listEnvelope := decodeTestEnvelope(t, listRec)
	require.Equal(t, 0, listEnvelope.Code)
	var list struct {
		Items      []customerResponse `json:"items"`
		Total      int                `json:"total"`
		Page       int                `json:"page"`
		PageSize   int                `json:"pageSize"`
		TotalPages int                `json:"totalPages"`
	}
	require.NoError(t, json.Unmarshal(listEnvelope.Data, &list))
	require.Equal(t, 1, list.Total)
	require.Len(t, list.Items, 1)
	require.Equal(t, 1, list.Page)
	require.Equal(t, 10, list.PageSize)
	require.Equal(t, 1, list.TotalPages)

	routerB := newCustomerMasterHTTPRouter(t, client, provider.ID+100, 8)
	crossTenantReq := httptest.NewRequest(http.MethodGet, "/api/v1/master-data/customers/1", nil)
	crossTenantRec := httptest.NewRecorder()
	routerB.ServeHTTP(crossTenantRec, crossTenantReq)
	require.Equal(t, http.StatusNotFound, crossTenantRec.Code)
	crossTenantEnvelope := decodeTestEnvelope(t, crossTenantRec)
	require.Equal(t, 4004, crossTenantEnvelope.Code)

	updateReq := httptest.NewRequest(http.MethodPut, "/api/v1/master-data/customers/1", bytes.NewBufferString(`{
		"name": "HTTP 客户（更新）",
		"tier": "S",
		"customerType": "direct",
		"lifecycleStage": "renewal",
		"status": "active",
		"version": 1
	}`))
	updateReq.Header.Set("Content-Type", "application/json")
	updateRec := httptest.NewRecorder()
	routerA.ServeHTTP(updateRec, updateReq)
	require.Equal(t, http.StatusOK, updateRec.Code)
	updateEnvelope := decodeTestEnvelope(t, updateRec)
	require.Equal(t, 0, updateEnvelope.Code)
	var updated customerResponse
	require.NoError(t, json.Unmarshal(updateEnvelope.Data, &updated))
	require.Equal(t, int64(2), updated.Version)
	require.Equal(t, "S", updated.Tier)

	staleReq := httptest.NewRequest(http.MethodPut, "/api/v1/master-data/customers/1", bytes.NewBufferString(`{
		"name": "过期版本",
		"version": 1
	}`))
	staleReq.Header.Set("Content-Type", "application/json")
	staleRec := httptest.NewRecorder()
	routerA.ServeHTTP(staleRec, staleReq)
	require.Equal(t, http.StatusConflict, staleRec.Code)
	require.Equal(t, 4090, decodeTestEnvelope(t, staleRec).Code)

	deleteReq := httptest.NewRequest(http.MethodDelete, "/api/v1/master-data/customers/1", nil)
	deleteRec := httptest.NewRecorder()
	routerA.ServeHTTP(deleteRec, deleteReq)
	require.Equal(t, http.StatusOK, deleteRec.Code)

	afterDeleteReq := httptest.NewRequest(http.MethodGet, "/api/v1/master-data/customers/1", nil)
	afterDeleteRec := httptest.NewRecorder()
	routerA.ServeHTTP(afterDeleteRec, afterDeleteReq)
	require.Equal(t, http.StatusOK, afterDeleteRec.Code)
	afterDeleteEnvelope := decodeTestEnvelope(t, afterDeleteRec)
	var afterDelete struct {
		Customer customerResponse `json:"customer"`
	}
	require.NoError(t, json.Unmarshal(afterDeleteEnvelope.Data, &afterDelete))
	require.Equal(t, "inactive", afterDelete.Customer.Status)
}
