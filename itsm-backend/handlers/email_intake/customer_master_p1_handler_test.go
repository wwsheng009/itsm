package email_intake

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"

	_ "github.com/mattn/go-sqlite3"
	"github.com/stretchr/testify/require"
	"itsm-backend/ent/enttest"
)

func p1Request(t *testing.T, router http.Handler, method, path, body string) (*httptest.ResponseRecorder, testEnvelope) {
	t.Helper()
	req := httptest.NewRequest(method, path, bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	return rec, decodeTestEnvelope(t, rec)
}

func TestCustomerMasterP1_HTTPRelationshipsProjectsMergeUndo(t *testing.T) {
	client := enttest.Open(t, "sqlite3", "file:customer-master-p1-http?mode=memory&cache=shared&_fk=1")
	defer client.Close()
	ctx := context.Background()
	provider := client.Tenant.Create().
		SetName("p1-http-provider").SetCode("p1-http-provider").SetType("msp_provider").SaveX(ctx)
	router := newCustomerMasterHTTPRouter(t, client, provider.ID, 7)
	a := createTestCustomer(t, client, provider.ID, "HTTP 集团 A")
	b := createTestCustomer(t, client, provider.ID, "HTTP 子公司 B")
	c := createTestCustomer(t, client, provider.ID, "HTTP 子公司 C")
	project := client.Project.Create().
		SetTenantID(provider.ID).SetName("HTTP 交付项目").SetCode("PRJ-P1-HTTP").
		SetType("delivery").SetStatus("active").SaveX(ctx)

	rec, envelope := p1Request(t, router, http.MethodPost,
		"/api/v1/master-data/customers/"+itoa(a.ID)+"/relationships",
		`{"toCustomerId":`+itoa(b.ID)+`,"relationType":"parent"}`)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	require.Equal(t, 0, envelope.Code)

	rec, envelope = p1Request(t, router, http.MethodPost,
		"/api/v1/master-data/customers/"+itoa(b.ID)+"/relationships",
		`{"toCustomerId":`+itoa(c.ID)+`,"relationType":"parent"}`)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	require.Equal(t, 0, envelope.Code)

	rec, envelope = p1Request(t, router, http.MethodGet,
		"/api/v1/master-data/customers/"+itoa(b.ID)+"/relations-tree?depth=5", "")
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	require.Equal(t, 0, envelope.Code)
	var tree struct {
		Parents []struct {
			Customer relationCustomerSummaryResponse `json:"customer"`
		} `json:"parents"`
	}
	require.NoError(t, json.Unmarshal(envelope.Data, &tree))
	require.Len(t, tree.Parents, 1)
	require.Equal(t, a.ID, tree.Parents[0].Customer.ID)

	rec, envelope = p1Request(t, router, http.MethodPost,
		"/api/v1/master-data/customers/"+itoa(c.ID)+"/relationships",
		`{"toCustomerId":`+itoa(a.ID)+`,"relationType":"parent"}`)
	require.Equal(t, http.StatusConflict, rec.Code, rec.Body.String())
	require.Equal(t, 4090, envelope.Code)

	rec, envelope = p1Request(t, router, http.MethodPost,
		"/api/v1/master-data/customers/"+itoa(a.ID)+"/projects",
		`{"projectId":`+itoa(project.ID)+`,"relationType":"delivery","isPrimary":true}`)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	require.Equal(t, 0, envelope.Code)

	rec, envelope = p1Request(t, router, http.MethodGet,
		"/api/v1/master-data/customers/"+itoa(a.ID)+"/projects", "")
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	var customerProjects struct {
		Total int `json:"total"`
	}
	require.NoError(t, json.Unmarshal(envelope.Data, &customerProjects))
	require.Equal(t, 1, customerProjects.Total)

	rec, envelope = p1Request(t, router, http.MethodGet,
		"/api/v1/projects/"+itoa(project.ID)+"/customer-links", "")
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	var projectCustomers struct {
		Total int `json:"total"`
	}
	require.NoError(t, json.Unmarshal(envelope.Data, &projectCustomers))
	require.Equal(t, 1, projectCustomers.Total)

	rec, envelope = p1Request(t, router, http.MethodPost,
		"/api/v1/master-data/customers/"+itoa(b.ID)+"/merge",
		`{"targetCustomerId":`+itoa(c.ID)+`}`)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	require.Equal(t, 0, envelope.Code)

	rec, envelope = p1Request(t, router, http.MethodGet,
		"/api/v1/master-data/customers/"+itoa(c.ID)+"/merge-history", "")
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	require.Equal(t, 0, envelope.Code)
	var history struct {
		Items []customerMergeHistoryItem `json:"items"`
	}
	require.NoError(t, json.Unmarshal(envelope.Data, &history))
	require.Len(t, history.Items, 1)
	require.Equal(t, b.ID, history.Items[0].SourceID)
	require.True(t, history.Items[0].CanUndo)

	rec, _ = p1Request(t, router, http.MethodGet,
		"/api/v1/master-data/customers/"+itoa(b.ID), "")
	require.Equal(t, http.StatusNotFound, rec.Code, rec.Body.String())

	rec, envelope = p1Request(t, router, http.MethodPost,
		"/api/v1/master-data/customers/"+itoa(b.ID)+"/merge/undo", "{}")
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	require.Equal(t, 0, envelope.Code)

	rec, envelope = p1Request(t, router, http.MethodGet,
		"/api/v1/master-data/customers/"+itoa(c.ID)+"/merge-history", "")
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	require.NoError(t, json.Unmarshal(envelope.Data, &history))
	require.Len(t, history.Items, 1)
	require.False(t, history.Items[0].CanUndo)

	rec, _ = p1Request(t, router, http.MethodGet,
		"/api/v1/master-data/customers/"+itoa(b.ID), "")
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
}

func itoa(value int) string {
	return strconv.Itoa(value)
}
