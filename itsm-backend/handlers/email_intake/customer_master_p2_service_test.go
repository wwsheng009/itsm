package email_intake

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestCustomerMasterP2_ExternalIDsAndMerge(t *testing.T) {
	client := openCustomerMasterTestClient(t, "customer-master-p2-external")
	defer client.Close()
	ctx := context.Background()
	provider, _ := seedProviderAndCustomerTenant(t, client, "p2-provider", "p2-customer")
	svc := NewService(client)

	source := createTestCustomer(t, client, provider.ID, "P2 源客户")
	target := createTestCustomer(t, client, provider.ID, "P2 目标客户")

	created, err := svc.CreateCustomerExternalID(ctx, provider.ID, source.ID, &customerExternalIDRequest{
		SystemCode: " ERP ", ExternalID: " E-100 ", ExternalURL: "https://erp.example/customers/E-100",
	})
	require.NoError(t, err)
	require.Equal(t, "erp", created.SystemCode)
	require.Equal(t, "E-100", created.ExternalID)

	_, err = svc.CreateCustomerExternalID(ctx, provider.ID, target.ID, &customerExternalIDRequest{
		SystemCode: "erp", ExternalID: "E-100",
	})
	require.ErrorIs(t, err, ErrCustomerExternalIDDuplicate)

	detail, err := svc.GetCustomerDetail(ctx, provider.ID, source.ID)
	require.NoError(t, err)
	require.Len(t, detail.ExternalIDs, 1)

	updated, err := svc.UpdateCustomerExternalID(ctx, provider.ID, source.ID, created.ID, &customerExternalIDRequest{
		SystemCode: "crm", ExternalID: "C-200",
	})
	require.NoError(t, err)
	require.Equal(t, "crm", updated.SystemCode)

	report, err := svc.MergeCustomers(ctx, provider.ID, 7, source.ID, &customerMergeRequest{TargetCustomerID: target.ID})
	require.NoError(t, err)
	require.Equal(t, 1, report.ExternalIDsMoved)
	moved, err := client.CustomerExternalID.Get(ctx, created.ID)
	require.NoError(t, err)
	require.Equal(t, target.ID, moved.CustomerID)

	_, err = svc.UndoCustomerMerge(ctx, provider.ID, 7, source.ID)
	require.NoError(t, err)
	restored, err := client.CustomerExternalID.Get(ctx, created.ID)
	require.NoError(t, err)
	require.Equal(t, source.ID, restored.CustomerID)

	require.NoError(t, svc.DeleteCustomerExternalID(ctx, provider.ID, source.ID, created.ID))
	items, err := svc.ListCustomerExternalIDs(ctx, provider.ID, source.ID)
	require.NoError(t, err)
	require.Empty(t, items)
}

func TestCustomerMasterP2_Customer360Aggregation(t *testing.T) {
	client := openCustomerMasterTestClient(t, "customer-master-p2-360")
	defer client.Close()
	ctx := context.Background()
	provider, customerTenant := seedProviderAndCustomerTenant(t, client, "p2-360-provider", "p2-360-customer")
	svc := NewService(client)
	now := time.Now()

	sla := client.SLADefinition.Create().
		SetTenantID(provider.ID).SetName("P2 SLA").
		SetResponseTime(30).SetResolutionTime(240).SaveX(ctx)
	customerEntity, err := svc.CreateCustomerMaster(ctx, provider.ID, 7, &customerRequest{
		Name: "P2 客户 360", Tier: "A", LifecycleStage: "active", Status: "active",
		SLADefinitionID: &sla.ID, Industry: "制造业",
	})
	require.NoError(t, err)
	customer := customerEntity

	_, err = svc.CreateCustomerContact(ctx, provider.ID, customer.ID, &customerContactRequest{
		Name: "P2 主联系人", Email: "p2-contact@example.com", ContactRole: "technical", IsPrimary: true,
	})
	require.NoError(t, err)
	_, err = svc.CreateCustomerTenantLink(ctx, provider.ID, customer.ID, &customerTenantLinkRequest{
		LinkedTenantID: customerTenant.ID, LinkType: "primary", IsBillingAnchor: true,
	})
	require.NoError(t, err)
	_, err = svc.CreateCustomerExternalID(ctx, provider.ID, customer.ID, &customerExternalIDRequest{
		SystemCode: "erp", ExternalID: "P2-360",
	})
	require.NoError(t, err)

	project := client.Project.Create().
		SetTenantID(provider.ID).SetName("P2 交付项目").SetCode("PRJ-P2-360").
		SetType("delivery").SetStatus("active").SetHealth("yellow").SetProgress(40).SaveX(ctx)
	_, err = svc.CreateCustomerProjectLink(ctx, provider.ID, 7, customer.ID, &projectCustomerLinkRequest{
		ProjectID: project.ID, RelationType: "delivery", IsPrimary: true,
	})
	require.NoError(t, err)

	_, err = svc.CreateSupportContract(ctx, provider.ID, &supportContractRequest{
		CustomerID: customer.ID, ContractNumber: "P2-CONTRACT-1", Status: "active",
		StartAt: &now, EndAt: timePtr(now.Add(20 * 24 * time.Hour)),
	})
	require.NoError(t, err)

	user := client.User.Create().
		SetTenantID(provider.ID).SetUsername("p2-360-user").SetEmail("p2-360-user@example.com").
		SetPasswordHash("x").SetName("P2 用户").SetActive(true).SaveX(ctx)
	openTicket := client.Ticket.Create().
		SetTenantID(provider.ID).SetTitle("P2 未结工单").SetTicketNumber("P2-T-1").
		SetRequesterID(user.ID).SetProjectID(project.ID).SetStatus("open").SetPriority("high").
		SetSLAResolutionDeadline(now.Add(-time.Hour)).SetCreatedAt(now.Add(-2 * time.Hour)).SaveX(ctx)
	client.Incident.Create().
		SetTenantID(provider.ID).SetTitle("P2 未结事件").SetIncidentNumber("P2-I-1").
		SetReporterID(user.ID).SetProjectID(project.ID).SetStatus("new").SetPriority("critical").
		SetSLAResolutionDeadline(now.Add(-time.Hour)).SetCreatedAt(now.Add(-time.Hour)).SaveX(ctx)
	client.ProjectRisk.Create().
		SetTenantID(provider.ID).SetProjectID(project.ID).SetTitle("P2 高风险").
		SetLevel("high").SetStatus("open").SaveX(ctx)
	client.ProjectMilestone.Create().
		SetTenantID(provider.ID).SetProjectID(project.ID).SetName("P2 里程碑").
		SetDueDate(now.Add(24 * time.Hour)).SetStatus("planned").SaveX(ctx)
	client.SLAViolation.Create().
		SetTenantID(provider.ID).SetSLADefinitionID(sla.ID).SetTicketID(openTicket.ID).
		SetTicketType("ticket").SetViolationType("resolution_time").
		SetViolationTime(now.Add(-time.Hour)).SetIsResolved(false).SaveX(ctx)
	client.SLAMetric.Create().
		SetTenantID(provider.ID).SetSLADefinitionID(sla.ID).SetMetricType("compliance_rate").
		SetMetricName("SLA 达标率").SetMetricValue(0.9).SetUnit("%").
		SetMeasurementTime(now).SaveX(ctx)
	asset := client.Asset.Create().
		SetTenantID(provider.ID).SetAssetNumber("P2-ASSET-1").SetName("P2 核心交换机").
		SetStatus("in-use").SaveX(ctx)
	client.ProjectAssetLink.Create().
		SetTenantID(provider.ID).SetProjectID(project.ID).SetLinkType("asset").SetTargetID(asset.ID).SaveX(ctx)

	parent := createTestCustomer(t, client, provider.ID, "P2 父客户")
	_, err = svc.CreateCustomerRelationship(ctx, provider.ID, 7, customer.ID, &customerRelationshipRequest{
		ToCustomerID: parent.ID, RelationType: "subsidiary",
	})
	require.NoError(t, err)

	data, err := svc.GetCustomer360(ctx, provider.ID, customer.ID)
	require.NoError(t, err)
	require.Equal(t, customer.ID, data.Overview.CustomerID)
	require.Len(t, data.ExternalIDs, 1)
	require.Equal(t, 1, data.Projects.Total)
	require.Equal(t, 1, data.Projects.InFlight)
	require.Equal(t, 1, data.Projects.Yellow)
	require.Equal(t, 1, data.Projects.OpenRisks)
	require.Equal(t, 1, data.Projects.MilestoneRisks)
	require.Equal(t, 1, data.Tickets.TicketTotal)
	require.Equal(t, 1, data.Tickets.TicketOpen)
	require.Equal(t, 1, data.Tickets.TicketOverdue)
	require.Equal(t, 1, data.Tickets.IncidentTotal)
	require.Equal(t, 1, data.Tickets.IncidentOverdue)
	require.Equal(t, 2, data.Tickets.P1P2)
	require.Equal(t, 1, data.Contracts.ActiveCount)
	require.Equal(t, 1, data.Contracts.ExpiringSoon)
	require.NotNil(t, data.Contracts.Current)
	require.NotNil(t, data.ServiceHealth.ComplianceRate)
	require.InDelta(t, 90, *data.ServiceHealth.ComplianceRate, 0.01)
	require.Equal(t, 1, data.ServiceHealth.Violations30d)
	require.Equal(t, 1, data.ServiceHealth.OpenViolations)
	require.Equal(t, 1, data.Assets.AssetCount)
	require.Equal(t, 1, data.Contacts.Total)
	require.NotNil(t, data.Contacts.Primary)
	require.Equal(t, 1, data.Tenants.Total)
	require.Equal(t, 1, data.Tenants.Primary)
	require.Equal(t, 1, data.Relationships.Parents)
}

func timePtr(value time.Time) *time.Time {
	return &value
}
