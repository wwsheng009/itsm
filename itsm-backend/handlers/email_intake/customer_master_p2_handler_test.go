package email_intake

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestCustomerMasterP2_HTTPExternalIDsAnd360(t *testing.T) {
	client := openCustomerMasterTestClient(t, "customer-master-p2-http")
	defer client.Close()
	provider, _ := seedProviderAndCustomerTenant(t, client, "p2-http-provider", "p2-http-customer")
	router := newCustomerMasterHTTPRouter(t, client, provider.ID, 7)
	customer := createTestCustomer(t, client, provider.ID, "P2 HTTP 客户")

	rec, envelope := p1Request(t, router, http.MethodPost,
		"/api/v1/master-data/customers/"+itoa(customer.ID)+"/external-ids",
		`{"systemCode":"ERP","externalId":"EXT-1","externalUrl":"https://erp.example/1"}`)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	require.Equal(t, 0, envelope.Code)
	var created customerExternalIDResponse
	require.NoError(t, json.Unmarshal(envelope.Data, &created))
	require.Equal(t, "erp", created.SystemCode)

	rec, envelope = p1Request(t, router, http.MethodGet,
		"/api/v1/master-data/customers/"+itoa(customer.ID)+"/external-ids", "")
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	var list struct {
		Total int                          `json:"total"`
		Items []customerExternalIDResponse `json:"items"`
	}
	require.NoError(t, json.Unmarshal(envelope.Data, &list))
	require.Equal(t, 1, list.Total)

	rec, envelope = p1Request(t, router, http.MethodGet,
		"/api/v1/master-data/customers/"+itoa(customer.ID), "")
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	var detail struct {
		ExternalIDs []customerExternalIDResponse `json:"externalIds"`
	}
	require.NoError(t, json.Unmarshal(envelope.Data, &detail))
	require.Len(t, detail.ExternalIDs, 1)

	rec, envelope = p1Request(t, router, http.MethodPut,
		"/api/v1/master-data/customers/"+itoa(customer.ID)+"/external-ids/"+itoa(created.ID),
		`{"systemCode":"CRM","externalId":"CRM-1"}`)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	require.Equal(t, 0, envelope.Code)
	var updated customerExternalIDResponse
	require.NoError(t, json.Unmarshal(envelope.Data, &updated))
	require.Equal(t, "crm", updated.SystemCode)

	rec, envelope = p1Request(t, router, http.MethodGet,
		"/api/v1/master-data/customers/"+itoa(customer.ID)+"/360", "")
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	require.Equal(t, 0, envelope.Code)
	var dashboard Customer360
	require.NoError(t, json.Unmarshal(envelope.Data, &dashboard))
	require.Equal(t, customer.ID, dashboard.Overview.CustomerID)
	require.Len(t, dashboard.ExternalIDs, 1)
	require.Equal(t, 30, dashboard.WindowDays)

	rec, envelope = p1Request(t, router, http.MethodDelete,
		"/api/v1/master-data/customers/"+itoa(customer.ID)+"/external-ids/"+itoa(created.ID), "")
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	require.Equal(t, 0, envelope.Code)

	rec, _ = p1Request(t, router, http.MethodGet,
		"/api/v1/master-data/customers/"+itoa(customer.ID)+"/360", "")
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
}
