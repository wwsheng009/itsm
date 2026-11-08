package email_intake

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
	"itsm-backend/ent"
)

func TestCustomerRelationshipP1_ReverseCycleAndTree(t *testing.T) {
	client := openCustomerMasterTestClient(t, "customer-relationship-p1")
	defer client.Close()
	ctx := context.Background()
	provider, _ := seedProviderAndCustomerTenant(t, client, "provider-rel", "customer-rel")
	svc := NewService(client)
	customerA := createTestCustomer(t, client, provider.ID, "集团 A")
	customerB := createTestCustomer(t, client, provider.ID, "子公司 B")
	customerC := createTestCustomer(t, client, provider.ID, "子公司 C")

	_, err := svc.CreateCustomerRelationship(ctx, provider.ID, 1, customerA.ID, &customerRelationshipRequest{
		ToCustomerID: customerB.ID,
		RelationType: "parent",
	})
	require.NoError(t, err)
	_, err = svc.CreateCustomerRelationship(ctx, provider.ID, 1, customerB.ID, &customerRelationshipRequest{
		ToCustomerID: customerC.ID,
		RelationType: "parent",
	})
	require.NoError(t, err)

	relations, err := svc.ListCustomerRelations(ctx, provider.ID, customerA.ID)
	require.NoError(t, err)
	require.Len(t, relations, 2)
	require.True(t, hasRelation(relations, customerA.ID, customerB.ID, "parent"))

	allRelations, err := svc.ListCustomerRelations(ctx, provider.ID, customerB.ID)
	require.NoError(t, err)
	require.Len(t, allRelations, 4)
	require.True(t, hasRelation(allRelations, customerA.ID, customerB.ID, "parent"))
	require.True(t, hasRelation(allRelations, customerB.ID, customerA.ID, "subsidiary"))

	tree, err := svc.GetCustomerRelationsTree(ctx, provider.ID, customerA.ID, 5)
	require.NoError(t, err)
	require.Empty(t, tree.Parents)
	require.Len(t, tree.Children, 1)
	require.Equal(t, customerB.ID, tree.Children[0].Customer.ID)
	require.Len(t, tree.Children[0].Children, 1)
	require.Equal(t, customerC.ID, tree.Children[0].Children[0].Customer.ID)

	treeB, err := svc.GetCustomerRelationsTree(ctx, provider.ID, customerB.ID, 5)
	require.NoError(t, err)
	require.Len(t, treeB.Parents, 1)
	require.Equal(t, customerA.ID, treeB.Parents[0].Customer.ID)
	require.Len(t, treeB.Children, 1)
	require.Equal(t, customerC.ID, treeB.Children[0].Customer.ID)

	_, err = svc.CreateCustomerRelationship(ctx, provider.ID, 1, customerC.ID, &customerRelationshipRequest{
		ToCustomerID: customerA.ID,
		RelationType: "parent",
	})
	require.ErrorIs(t, err, ErrCustomerRelationCycle)

	_, err = svc.CreateCustomerRelationship(ctx, provider.ID, 1, customerA.ID, &customerRelationshipRequest{
		ToCustomerID: customerA.ID,
		RelationType: "partner",
	})
	require.ErrorIs(t, err, ErrCustomerRelationInvalid)
}

func TestProjectCustomerLinkP1_InternalProjectRejectedAndPrimary(t *testing.T) {
	client := openCustomerMasterTestClient(t, "project-customer-link-p1")
	defer client.Close()
	ctx := context.Background()
	provider, _ := seedProviderAndCustomerTenant(t, client, "provider-pcl", "customer-pcl")
	svc := NewService(client)
	customer := createTestCustomer(t, client, provider.ID, "项目客户")
	delivery := client.Project.Create().
		SetTenantID(provider.ID).SetName("交付项目").SetCode("PRJ-D-1").SetType("delivery").SetStatus("active").
		SaveX(ctx)
	internal := client.Project.Create().
		SetTenantID(provider.ID).SetName("内部项目").SetCode("PRJ-I-1").SetType("internal").SetStatus("active").
		SaveX(ctx)

	_, err := svc.CreateCustomerProjectLink(ctx, provider.ID, 1, customer.ID, &projectCustomerLinkRequest{
		ProjectID:    internal.ID,
		RelationType: "delivery",
	})
	require.ErrorIs(t, err, ErrProjectCustomerTypeInvalid)

	link, err := svc.CreateCustomerProjectLink(ctx, provider.ID, 1, customer.ID, &projectCustomerLinkRequest{
		ProjectID:    delivery.ID,
		RelationType: "delivery",
		IsPrimary:    true,
	})
	require.NoError(t, err)
	require.True(t, link.IsPrimary)

	rows, err := svc.ListCustomerProjects(ctx, provider.ID, customer.ID)
	require.NoError(t, err)
	require.Len(t, rows, 1)
	require.Equal(t, delivery.ID, rows[0].Project.ID)
	require.Equal(t, "delivery", rows[0].Link.RelationType)

	projectRows, err := svc.ListProjectCustomerLinks(ctx, provider.ID, delivery.ID)
	require.NoError(t, err)
	require.Len(t, projectRows, 1)
	require.Equal(t, customer.ID, projectRows[0].Customer.ID)

	require.NoError(t, svc.DeleteCustomerProjectLink(ctx, provider.ID, customer.ID, link.ID))
	rows, err = svc.ListCustomerProjects(ctx, provider.ID, customer.ID)
	require.NoError(t, err)
	require.Empty(t, rows)
}

func TestCustomerMergeP1_MoveAndUndo(t *testing.T) {
	client := openCustomerMasterTestClient(t, "customer-merge-p1")
	defer client.Close()
	ctx := context.Background()
	provider, customerTenant := seedProviderAndCustomerTenant(t, client, "provider-merge", "customer-merge")
	svc := NewService(client)
	source := createTestCustomer(t, client, provider.ID, "待合并客户")
	target := createTestCustomer(t, client, provider.ID, "保留客户")

	sourceBranch, err := svc.CreateBranch(ctx, provider.ID, &branchRequest{
		CustomerID: source.ID, Name: "总部", Code: "SRC-HQ", BranchType: "hq", IsPrimary: true,
	})
	require.NoError(t, err)
	_, err = svc.CreateBranch(ctx, provider.ID, &branchRequest{
		CustomerID: target.ID, Name: "总部", Code: "TGT-HQ", BranchType: "hq", IsPrimary: true,
	})
	require.NoError(t, err)
	_, err = svc.CreateCustomerContact(ctx, provider.ID, source.ID, &customerContactRequest{
		Name: "源联系人", Email: "source-contact@example.com", ContactRole: "business", IsPrimary: true,
		BranchID: &sourceBranch.ID,
	})
	require.NoError(t, err)
	contract, err := client.SupportContract.Create().
		SetTenantID(provider.ID).SetCustomerID(source.ID).SetBranchID(sourceBranch.ID).
		SetContractNumber("SRC-CONTRACT").SetNormalizedContractNumber("SRC-CONTRACT").SetStatus("active").
		Save(ctx)
	require.NoError(t, err)
	project := client.Project.Create().
		SetTenantID(provider.ID).SetName("交付项目").SetCode("PRJ-MERGE-1").SetType("delivery").SetStatus("active").
		SaveX(ctx)
	_, err = svc.CreateCustomerProjectLink(ctx, provider.ID, 1, source.ID, &projectCustomerLinkRequest{
		ProjectID: project.ID, BranchID: &sourceBranch.ID, RelationType: "delivery", IsPrimary: true,
	})
	require.NoError(t, err)
	_, err = svc.CreateCustomerTenantLink(ctx, provider.ID, source.ID, &customerTenantLinkRequest{
		LinkedTenantID: customerTenant.ID, LinkType: "primary", IsBillingAnchor: true,
	})
	require.NoError(t, err)

	report, err := svc.MergeCustomers(ctx, provider.ID, 1, source.ID, &customerMergeRequest{TargetCustomerID: target.ID})
	require.NoError(t, err)
	require.Equal(t, source.ID, report.SourceID)
	require.Equal(t, target.ID, report.TargetID)
	require.Equal(t, 1, report.BranchesCreated)
	require.Equal(t, 1, report.ContactsMoved)
	require.Equal(t, 1, report.ContractsMoved)
	require.Equal(t, 1, report.ProjectLinksMoved)
	require.Equal(t, 1, report.TenantLinksMoved)

	mergedSource, err := client.ServiceCustomer.Get(ctx, source.ID)
	require.NoError(t, err)
	require.NotNil(t, mergedSource.MergedIntoID)
	require.Equal(t, target.ID, *mergedSource.MergedIntoID)
	require.NotNil(t, mergedSource.DeletedAt)

	history, err := svc.ListCustomerMergeHistory(ctx, provider.ID, target.ID)
	require.NoError(t, err)
	require.Len(t, history, 1)
	require.Equal(t, source.ID, history[0].SourceID)
	require.Equal(t, source.Name, history[0].SourceName)
	require.True(t, history[0].CanUndo)

	targetBranches, err := svc.ListBranches(ctx, provider.ID, target.ID)
	require.NoError(t, err)
	require.Len(t, targetBranches, 2)
	targetDetail, err := svc.GetCustomerDetail(ctx, provider.ID, target.ID)
	require.NoError(t, err)
	require.Equal(t, 1, targetDetail.ContactCount)
	require.Equal(t, 1, targetDetail.ContractCount)
	require.Equal(t, 1, targetDetail.TenantLinkCount)
	targetProjects, err := svc.ListCustomerProjects(ctx, provider.ID, target.ID)
	require.NoError(t, err)
	require.Len(t, targetProjects, 1)
	movedContract, err := client.SupportContract.Get(ctx, contract.ID)
	require.NoError(t, err)
	require.Equal(t, target.ID, movedContract.CustomerID)

	undoReport, err := svc.UndoCustomerMerge(ctx, provider.ID, 1, source.ID)
	require.NoError(t, err)
	require.Equal(t, source.ID, undoReport.SourceID)

	restoredSource, err := client.ServiceCustomer.Get(ctx, source.ID)
	require.NoError(t, err)
	require.Nil(t, restoredSource.MergedIntoID)
	require.Nil(t, restoredSource.DeletedAt)
	history, err = svc.ListCustomerMergeHistory(ctx, provider.ID, target.ID)
	require.NoError(t, err)
	require.Len(t, history, 1)
	require.False(t, history[0].CanUndo)
	targetBranches, err = svc.ListBranches(ctx, provider.ID, target.ID)
	require.NoError(t, err)
	require.Len(t, targetBranches, 1)
	sourceBranches, err := svc.ListBranches(ctx, provider.ID, source.ID)
	require.NoError(t, err)
	require.Len(t, sourceBranches, 1)
	restoredContract, err := client.SupportContract.Get(ctx, contract.ID)
	require.NoError(t, err)
	require.Equal(t, source.ID, restoredContract.CustomerID)
	sourceProjects, err := svc.ListCustomerProjects(ctx, provider.ID, source.ID)
	require.NoError(t, err)
	require.Len(t, sourceProjects, 1)
	targetProjects, err = svc.ListCustomerProjects(ctx, provider.ID, target.ID)
	require.NoError(t, err)
	require.Empty(t, targetProjects)

	_, err = svc.UndoCustomerMerge(ctx, provider.ID, 1, source.ID)
	require.ErrorIs(t, err, ErrCustomerMergeAlreadyUndone)
}

func TestCustomerQualityP1_FullScore(t *testing.T) {
	client := openCustomerMasterTestClient(t, "customer-quality-p1")
	defer client.Close()
	ctx := context.Background()
	provider, customerTenant := seedProviderAndCustomerTenant(t, client, "provider-quality", "customer-quality")
	svc := NewService(client)
	accountManagerID := 1
	slaID := 1
	customer, err := client.ServiceCustomer.Create().
		SetTenantID(provider.ID).
		SetCode("CUS-Q-1").
		SetName("质量分客户").
		SetNormalizedName("质量分客户").
		SetLegalName("质量分客户有限公司").
		SetTaxID("91310000QUALITY").
		SetEmailDomains([]string{"quality.example"}).
		SetIndustry("制造业").
		SetSizeClass("large").
		SetOwnershipType("private").
		SetTier("A").
		SetVipLevel(1).
		SetNillableSLADefinitionID(&slaID).
		SetNillableAccountManagerID(&accountManagerID).
		SetSupportHours("7x24").
		SetMainPhone("021-1000").
		SetServiceEmail("support@quality.example").
		SetAddress(map[string]any{"city": "上海"}).
		Save(ctx)
	require.NoError(t, err)
	_, err = client.CustomerContact.Create().
		SetTenantID(provider.ID).SetCustomerID(customer.ID).SetName("主联系人").
		SetContactRole("business").SetIsPrimary(true).Save(ctx)
	require.NoError(t, err)
	_, err = client.CustomerTenantLink.Create().
		SetTenantID(provider.ID).SetCustomerID(customer.ID).SetLinkedTenantID(customerTenant.ID).
		SetLinkType("primary").SetIsBillingAnchor(true).Save(ctx)
	require.NoError(t, err)
	project := client.Project.Create().
		SetTenantID(provider.ID).SetName("质量分项目").SetCode("PRJ-Q-1").SetType("delivery").SetStatus("active").
		SaveX(ctx)
	_, err = client.ProjectCustomerLink.Create().
		SetTenantID(provider.ID).SetProjectID(project.ID).SetCustomerID(customer.ID).
		SetRelationType("delivery").SetIsPrimary(true).Save(ctx)
	require.NoError(t, err)

	require.NoError(t, svc.recalculateCustomerQuality(ctx, provider.ID, customer.ID))
	updated, err := client.ServiceCustomer.Get(ctx, customer.ID)
	require.NoError(t, err)
	require.Equal(t, int16(100), updated.DataQualityScore)
}

func createTestCustomer(t *testing.T, client *ent.Client, tenantID int, name string) *ent.ServiceCustomer {
	t.Helper()
	return client.ServiceCustomer.Create().
		SetTenantID(tenantID).
		SetName(name).
		SetNormalizedName(name).
		SaveX(context.Background())
}

func hasRelation(rows []*customerRelationshipRow, fromID, toID int, relationType string) bool {
	for _, row := range rows {
		if row.Relation.FromCustomerID == fromID && row.Relation.ToCustomerID == toID && row.Relation.RelationType == relationType {
			return true
		}
	}
	return false
}
