package email_intake

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestSupportContractC1_HTTPAggregateLifecycle(t *testing.T) {
	client := openCustomerMasterTestClient(t, "contract-c1-http")
	defer client.Close()
	ctx := context.Background()
	provider, _ := seedProviderAndCustomerTenant(t, client, "c1-http-provider", "c1-http-customer")
	router := newCustomerMasterHTTPRouter(t, client, provider.ID, 7)
	customer := createTestCustomer(t, client, provider.ID, "C1 HTTP 客户")
	project := client.Project.Create().
		SetTenantID(provider.ID).SetName("C1 HTTP 项目").SetCode("PRJ-C1-HTTP").
		SetType("delivery").SetStatus("active").SaveX(ctx)
	client.ProjectCustomerLink.Create().
		SetTenantID(provider.ID).SetProjectID(project.ID).SetCustomerID(customer.ID).
		SetRelationType("delivery").SetIsPrimary(true).SaveX(ctx)

	rec, envelope := p1Request(t, router, http.MethodPost,
		"/api/v1/master-data/support-contracts",
		`{"customerId":`+itoa(customer.ID)+`,"contractNumber":"C1-HTTP-001","contractName":"C1 HTTP 合同",`+
			`"contractType":"support","status":"draft","amount":1000,"currency":"CNY"}`)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	require.Equal(t, 0, envelope.Code)
	var contract supportContractResponse
	require.NoError(t, json.Unmarshal(envelope.Data, &contract))
	require.Equal(t, "draft", contract.Status)

	rec, envelope = p1Request(t, router, http.MethodPost,
		"/api/v1/master-data/support-contracts/"+itoa(contract.ID)+"/items",
		`{"itemName":"HTTP 运维工时","billingUnit":"hour","unitPrice":1000,"quotaTotal":100}`)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	var item contractServiceItemResponse
	require.NoError(t, json.Unmarshal(envelope.Data, &item))
	require.Equal(t, 1000.0, item.Amount)

	rec, envelope = p1Request(t, router, http.MethodPost,
		"/api/v1/master-data/support-contracts/"+itoa(contract.ID)+"/sign", "")
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	require.Equal(t, 0, envelope.Code)

	rec, envelope = p1Request(t, router, http.MethodGet,
		"/api/v1/master-data/support-contracts/"+itoa(contract.ID), "")
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	var detail supportContractResponse
	require.NoError(t, json.Unmarshal(envelope.Data, &detail))
	require.Equal(t, "active", detail.Status)
	require.NotNil(t, detail.EffectiveAmount)
	require.Equal(t, 1000.0, *detail.EffectiveAmount)

	rec, envelope = p1Request(t, router, http.MethodPost,
		"/api/v1/master-data/support-contracts/"+itoa(contract.ID)+"/amendments",
		`{"amendmentNo":"A1","amendType":"amount","amountDelta":100,"effectiveFrom":"`+
			time.Now().Format(time.RFC3339)+`","reason":"追加额度"}`)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	var amendment contractAmendmentResponse
	require.NoError(t, json.Unmarshal(envelope.Data, &amendment))
	rec, envelope = p1Request(t, router, http.MethodPost,
		"/api/v1/master-data/support-contracts/"+itoa(contract.ID)+"/amendments/"+itoa(amendment.ID)+"/effect", "")
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())

	rec, envelope = p1Request(t, router, http.MethodGet,
		"/api/v1/master-data/support-contracts/"+itoa(contract.ID)+"/balance", "")
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	var balance contractBalanceResponse
	require.NoError(t, json.Unmarshal(envelope.Data, &balance))
	require.Equal(t, 1100.0, *balance.EffectiveAmount)

	rec, envelope = p1Request(t, router, http.MethodPost,
		"/api/v1/master-data/support-contracts/"+itoa(contract.ID)+"/usage",
		`{"serviceItemId":`+itoa(item.ID)+`,"sourceType":"timesheet","sourceRef":"HTTP-TS-1",`+
			`"quantityDelta":10,"amountDelta":50}`)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	require.Equal(t, 0, envelope.Code)
	rec, envelope = p1Request(t, router, http.MethodGet,
		"/api/v1/master-data/support-contracts/"+itoa(contract.ID)+"/usage", "")
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	var usageList struct {
		Total int `json:"total"`
	}
	require.NoError(t, json.Unmarshal(envelope.Data, &usageList))
	require.Equal(t, 1, usageList.Total)

	rec, envelope = p1Request(t, router, http.MethodPost,
		"/api/v1/master-data/support-contracts/"+itoa(contract.ID)+"/projects",
		`{"projectId":`+itoa(project.ID)+`,"relationType":"basis","isPrimary":true}`)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	rec, envelope = p1Request(t, router, http.MethodPost,
		"/api/v1/master-data/support-contracts/"+itoa(contract.ID)+"/projects",
		`{"projectId":`+itoa(project.ID)+`,"relationType":"billing","serviceItemId":`+itoa(item.ID)+`,"allocatedHours":50}`)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	rec, envelope = p1Request(t, router, http.MethodGet,
		"/api/v1/projects/"+itoa(project.ID)+"/contract-links", "")
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	var projectLinks struct {
		Total int `json:"total"`
	}
	require.NoError(t, json.Unmarshal(envelope.Data, &projectLinks))
	require.Equal(t, 2, projectLinks.Total)

	rec, envelope = p1Request(t, router, http.MethodPost,
		"/api/v1/master-data/support-contracts/"+itoa(contract.ID)+"/terminate",
		`{"reason":"C1 HTTP 测试终止"}`)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	var terminated supportContractResponse
	require.NoError(t, json.Unmarshal(envelope.Data, &terminated))
	require.Equal(t, "terminated", terminated.Status)
}
