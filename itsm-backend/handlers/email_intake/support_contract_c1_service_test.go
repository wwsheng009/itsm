package email_intake

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestSupportContractC1_AggregateItemsAmendmentsUsageAndLinks(t *testing.T) {
	client := openCustomerMasterTestClient(t, "contract-c1-aggregate")
	defer client.Close()
	ctx := context.Background()
	provider, _ := seedProviderAndCustomerTenant(t, client, "c1-provider", "c1-customer")
	svc := NewService(client)
	customer := createTestCustomer(t, client, provider.ID, "C1 合同客户")
	now := time.Now()

	contract, err := svc.CreateSupportContract(ctx, provider.ID, &supportContractRequest{
		CustomerID: customer.ID, ContractNumber: "C1-AGG-001", ContractName: "C1 运维合同",
		ContractType: "support", Status: "draft", Amount: contractFloatPtr(1000), Currency: "CNY",
		EffectiveFrom: contractTimePtr(now), EffectiveUntil: contractTimePtr(now.AddDate(1, 0, 0)),
	})
	require.NoError(t, err)
	require.Equal(t, "draft", contract.Status)
	require.Equal(t, 1000.0, *contract.Amount)

	_, err = svc.SignSupportContract(ctx, provider.ID, 7, contract.ID, nil)
	require.ErrorIs(t, err, ErrContractServiceItemRequired)

	item, err := svc.CreateContractServiceItem(ctx, provider.ID, 7, contract.ID, &contractServiceItemRequest{
		ItemName: "运维工时", BillingUnit: "hour", Quantity: contractFloatPtr(1),
		PeriodCount: contractFloatPtr(1), UnitPrice: contractFloatPtr(1000), QuotaTotal: contractFloatPtr(100),
	})
	require.NoError(t, err)
	require.Equal(t, 1000.0, item.Amount)

	signed, err := svc.SignSupportContract(ctx, provider.ID, 7, contract.ID, nil)
	require.NoError(t, err)
	require.Equal(t, "active", signed.Status)
	require.NotNil(t, signed.SignedAt)

	_, err = svc.UpdateSupportContract(ctx, provider.ID, contract.ID, &supportContractRequest{
		CustomerID: customer.ID, ContractNumber: "C1-AGG-001", ContractType: "support",
		Status: "active", Amount: contractFloatPtr(2000), EffectiveFrom: contractTimePtr(now),
		EffectiveUntil: contractTimePtr(now.AddDate(1, 0, 0)),
	})
	require.ErrorIs(t, err, ErrContractAmountImmutable)

	amendment, err := svc.CreateContractAmendment(ctx, provider.ID, 7, contract.ID, &contractAmendmentRequest{
		AmendmentNo: "A1", AmendType: "amount", EffectiveFrom: contractTimePtr(now),
		AmountDelta: contractFloatPtr(100), Reason: "追加运维额度",
	})
	require.NoError(t, err)
	_, err = svc.EffectContractAmendment(ctx, provider.ID, 7, contract.ID, amendment.ID)
	require.NoError(t, err)

	balance, err := svc.contractBalance(ctx, provider.ID, contract.ID)
	require.NoError(t, err)
	require.Equal(t, 1100.0, *balance.EffectiveAmount)

	firstUsage, err := svc.RecordContractUsage(ctx, provider.ID, 7, contract.ID, &contractUsageRequest{
		ServiceItemID: item.ID, SourceType: "timesheet", SourceRef: "TS-C1-1",
		QuantityDelta: contractFloatPtr(10), AmountDelta: contractFloatPtr(50), OccurredAt: contractTimePtr(now),
	})
	require.NoError(t, err)
	duplicateUsage, err := svc.RecordContractUsage(ctx, provider.ID, 7, contract.ID, &contractUsageRequest{
		ServiceItemID: item.ID, SourceType: "timesheet", SourceRef: "TS-C1-1",
		QuantityDelta: contractFloatPtr(10), AmountDelta: contractFloatPtr(50), OccurredAt: contractTimePtr(now),
	})
	require.NoError(t, err)
	require.Equal(t, firstUsage.ID, duplicateUsage.ID)
	_, err = svc.RecordContractUsage(ctx, provider.ID, 7, contract.ID, &contractUsageRequest{
		ServiceItemID: item.ID, SourceType: "timesheet", SourceRef: "TS-C1-1",
		QuantityDelta: contractFloatPtr(10), AmountDelta: contractFloatPtr(60),
	})
	require.ErrorIs(t, err, ErrContractUsageSourceConflict)
	_, err = svc.RecordContractUsage(ctx, provider.ID, 7, contract.ID, &contractUsageRequest{
		ServiceItemID: item.ID, SourceType: "timesheet", SourceRef: "TS-C1-2",
		QuantityDelta: contractFloatPtr(200),
	})
	require.ErrorIs(t, err, ErrContractQuotaExceeded)

	project := client.Project.Create().
		SetTenantID(provider.ID).SetName("C1 交付项目").SetCode("PRJ-C1-1").
		SetType("delivery").SetStatus("active").SaveX(ctx)
	client.ProjectCustomerLink.Create().
		SetTenantID(provider.ID).SetProjectID(project.ID).SetCustomerID(customer.ID).
		SetRelationType("delivery").SetIsPrimary(true).SaveX(ctx)
	basis, err := svc.CreateContractProjectLink(ctx, provider.ID, 7, contract.ID, &contractProjectLinkRequest{
		ProjectID: project.ID, RelationType: "basis", IsPrimary: true,
	})
	require.NoError(t, err)
	require.True(t, basis.IsPrimary)
	_, err = svc.CreateContractProjectLink(ctx, provider.ID, 7, contract.ID, &contractProjectLinkRequest{
		ProjectID: project.ID, RelationType: "billing", ServiceItemID: &item.ID,
		AllocatedHours: contractFloatPtr(50), AllocatedAmount: contractFloatPtr(400),
	})
	require.NoError(t, err)

	secondProject := client.Project.Create().
		SetTenantID(provider.ID).SetName("C1 第二项目").SetCode("PRJ-C1-2").
		SetType("delivery").SetStatus("active").SaveX(ctx)
	client.ProjectCustomerLink.Create().
		SetTenantID(provider.ID).SetProjectID(secondProject.ID).SetCustomerID(customer.ID).
		SetRelationType("delivery").SetIsPrimary(true).SaveX(ctx)
	_, err = svc.CreateContractProjectLink(ctx, provider.ID, 7, contract.ID, &contractProjectLinkRequest{
		ProjectID: secondProject.ID, RelationType: "billing", ServiceItemID: &item.ID,
		AllocatedHours: contractFloatPtr(50),
	})
	require.ErrorIs(t, err, ErrContractAllocationExceeded)

	internalProject := client.Project.Create().
		SetTenantID(provider.ID).SetName("C1 内部项目").SetCode("PRJ-C1-INT").
		SetType("internal").SetStatus("active").SaveX(ctx)
	_, err = svc.CreateContractProjectLink(ctx, provider.ID, 7, contract.ID, &contractProjectLinkRequest{
		ProjectID: internalProject.ID, RelationType: "coverage",
	})
	require.ErrorIs(t, err, ErrContractProjectLinkInvalid)

	balance, err = svc.contractBalance(ctx, provider.ID, contract.ID)
	require.NoError(t, err)
	require.Equal(t, 400.0, balance.AllocatedAmount)
	require.Equal(t, 50.0, balance.UsedAmount)
	require.Equal(t, 1050.0, *balance.RemainingAmount)
	require.Equal(t, 700.0, *balance.AvailableAmount)
	require.Len(t, balance.ServiceItems, 1)
	require.Equal(t, 10.0, balance.ServiceItems[0].QuotaUsed)
	require.Equal(t, 50.0, balance.ServiceItems[0].QuotaReserved)
}

func TestSupportContractC1_RenewCarriesContractAndProjects(t *testing.T) {
	client := openCustomerMasterTestClient(t, "contract-c1-renew")
	defer client.Close()
	ctx := context.Background()
	provider, _ := seedProviderAndCustomerTenant(t, client, "c1-renew-provider", "c1-renew-customer")
	svc := NewService(client)
	customer := createTestCustomer(t, client, provider.ID, "C1 续签客户")
	now := time.Now()

	contract, err := svc.CreateSupportContract(ctx, provider.ID, &supportContractRequest{
		CustomerID: customer.ID, ContractNumber: "C1-RENEW-001", Status: "active",
		Amount: contractFloatPtr(500), Currency: "CNY",
		EffectiveFrom: contractTimePtr(now), EffectiveUntil: contractTimePtr(now.AddDate(1, 0, 0)),
	})
	require.NoError(t, err)
	item, err := svc.CreateContractServiceItem(ctx, provider.ID, 7, contract.ID, &contractServiceItemRequest{
		ItemName: "续签服务项", BillingUnit: "project", UnitPrice: contractFloatPtr(500),
	})
	require.NoError(t, err)
	project := client.Project.Create().
		SetTenantID(provider.ID).SetName("C1 续签项目").SetCode("PRJ-C1-RENEW").
		SetType("delivery").SetStatus("active").SaveX(ctx)
	client.ProjectCustomerLink.Create().
		SetTenantID(provider.ID).SetProjectID(project.ID).SetCustomerID(customer.ID).
		SetRelationType("delivery").SetIsPrimary(true).SaveX(ctx)
	_, err = svc.CreateContractProjectLink(ctx, provider.ID, 7, contract.ID, &contractProjectLinkRequest{
		ProjectID: project.ID, RelationType: "basis", IsPrimary: true,
	})
	require.NoError(t, err)

	newContract, err := svc.RenewSupportContract(ctx, provider.ID, 7, contract.ID, &contractRenewRequest{
		ContractNumber: "C1-RENEW-002", Amount: contractFloatPtr(600),
		EffectiveFrom:   contractTimePtr(now.AddDate(1, 0, 0)),
		EffectiveUntil:  contractTimePtr(now.AddDate(2, 0, 0)),
		CarryProjectIDs: []int{project.ID},
	})
	require.NoError(t, err)
	require.Equal(t, contract.ID, *newContract.RenewalOfContractID)
	old, err := client.SupportContract.Get(ctx, contract.ID)
	require.NoError(t, err)
	require.Equal(t, "renewed", old.Status)
	items, err := svc.ListContractServiceItems(ctx, provider.ID, newContract.ID)
	require.NoError(t, err)
	require.Len(t, items, 1)
	require.Equal(t, "续签服务项", items[0].ItemName)
	links, err := svc.ListContractProjects(ctx, provider.ID, newContract.ID)
	require.NoError(t, err)
	require.Len(t, links, 1)
	require.Equal(t, project.ID, links[0].ProjectID)
	require.NotZero(t, item.ID)
}
