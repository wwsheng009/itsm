package email_intake

import (
	"context"
	"testing"

	_ "github.com/mattn/go-sqlite3"
	"github.com/stretchr/testify/require"
	"itsm-backend/ent"
	"itsm-backend/ent/enttest"
	tenantpkg "itsm-backend/ent/tenant"
)

func openCustomerMasterTestClient(t *testing.T, name string) *ent.Client {
	t.Helper()
	return enttest.Open(t, "sqlite3", "file:"+name+"?mode=memory&cache=shared&_fk=1")
}

func seedProviderAndCustomerTenant(t *testing.T, client *ent.Client, providerCode, customerCode string) (*ent.Tenant, *ent.Tenant) {
	t.Helper()
	ctx := context.Background()
	provider := client.Tenant.Create().
		SetName(providerCode).
		SetCode(providerCode).
		SetType(tenantpkg.TypeMspProvider).
		SaveX(ctx)
	customer := client.Tenant.Create().
		SetName(customerCode).
		SetCode(customerCode).
		SetType(tenantpkg.TypeMspCustomer).
		SetMspProviderID(provider.ID).
		SaveX(ctx)
	return provider, customer
}

func TestCustomerMasterP0_CRUDDetailListAndVersion(t *testing.T) {
	client := openCustomerMasterTestClient(t, "customer-master-crud")
	defer client.Close()
	ctx := context.Background()
	provider, customerTenant := seedProviderAndCustomerTenant(t, client, "provider-crud", "customer-crud")
	svc := NewService(client)

	entity, err := svc.CreateCustomerMaster(ctx, provider.ID, 7, &customerRequest{
		Name:         "华辰智能制造",
		ShortName:    "华辰",
		LegalName:    "华辰智能制造股份有限公司",
		TaxID:        "9131-0000-ABCD",
		EmailDomains: []string{"huachen.example"},
		Aliases:      []string{"华辰制造"},
	})
	require.NoError(t, err)
	require.Equal(t, "CUS-0001", entity.Code)
	require.Equal(t, "91310000ABCD", entity.NormalizedTaxID)
	require.Equal(t, "direct", entity.CustomerType)
	require.Equal(t, "C", entity.Tier)
	require.Equal(t, "prospect", entity.LifecycleStage)
	require.Equal(t, int64(1), entity.Version)
	require.Equal(t, []string{"华辰制造"}, entity.Aliases)
	require.NotNil(t, entity.CreatedBy)
	require.Equal(t, 7, *entity.CreatedBy)

	_, err = svc.CreateCustomerMaster(ctx, provider.ID, 7, &customerRequest{Name: "华辰智能制造"})
	require.ErrorIs(t, err, ErrCustomerDuplicate)
	_, err = svc.CreateCustomerMaster(ctx, provider.ID, 7, &customerRequest{Name: "另一个客户", TaxID: "9131-0000-ABCD"})
	require.ErrorIs(t, err, ErrCustomerDuplicate)
	_, err = svc.CreateCustomerMaster(ctx, provider.ID, 7, &customerRequest{Name: "非法等级", Tier: "Z"})
	require.ErrorIs(t, err, ErrCustomerInvalidEnum)

	branch, err := svc.CreateBranch(ctx, provider.ID, &branchRequest{
		CustomerID:   entity.ID,
		Name:         "上海总部",
		Code:         "BR-SH",
		BranchType:   "hq",
		Address:      map[string]any{"city": "上海"},
		ContactPhone: "021-1000",
		IsPrimary:    true,
	})
	require.NoError(t, err)
	require.Equal(t, "hq", branch.BranchType)
	require.True(t, branch.IsPrimary)

	_, err = svc.CreateCustomerContact(ctx, provider.ID, entity.ID, &customerContactRequest{
		Name:        "张三",
		Email:       "ZHANG@EXAMPLE.COM",
		ContactRole: "technical",
		IsPrimary:   true,
	})
	require.NoError(t, err)
	_, err = svc.CreateCustomerContact(ctx, provider.ID, entity.ID, &customerContactRequest{
		Name:        "张三副本",
		Email:       "zhang@example.com",
		ContactRole: "business",
	})
	require.ErrorIs(t, err, ErrCustomerContactDuplicate)
	_, err = svc.CreateCustomerContact(ctx, provider.ID, entity.ID, &customerContactRequest{
		Name:        "非法角色",
		Email:       "bad-role@example.com",
		ContactRole: "unknown",
	})
	require.ErrorIs(t, err, ErrCustomerInvalidEnum)

	link, err := svc.CreateCustomerTenantLink(ctx, provider.ID, entity.ID, &customerTenantLinkRequest{
		LinkedTenantID:  customerTenant.ID,
		LinkType:        "primary",
		IsBillingAnchor: true,
	})
	require.NoError(t, err)
	require.Equal(t, "pending", link.ProvisionStatus)

	detail, err := svc.GetCustomerDetail(ctx, provider.ID, entity.ID)
	require.NoError(t, err)
	require.Equal(t, 1, detail.BranchCount)
	require.Equal(t, 1, detail.ContactCount)
	require.Equal(t, 1, detail.TenantLinkCount)
	require.Equal(t, "张三", detail.Contacts[0].Name)
	require.Equal(t, "zhang@example.com", detail.Contacts[0].NormalizedEmail)

	rows, total, err := svc.ListCustomers(ctx, provider.ID, CustomerListFilters{
		Keyword:       "华辰制造",
		HasTenantLink: boolPtr(true),
		Page:          1,
		PageSize:      10,
	})
	require.NoError(t, err)
	require.Equal(t, 1, total)
	require.Len(t, rows, 1)
	require.Equal(t, 1, rows[0].BranchCount)
	require.Equal(t, 1, rows[0].ContactCount)
	require.Equal(t, 1, rows[0].TenantLinkCount)

	updated, err := svc.UpdateCustomerMaster(ctx, provider.ID, 8, entity.ID, &customerRequest{
		Name:           "华辰智能制造（更新）",
		ShortName:      "华辰",
		EmailDomains:   []string{"huachen.example"},
		CustomerType:   "direct",
		Tier:           "A",
		LifecycleStage: "active",
		Status:         "active",
		Version:        int64Ptr(1),
	})
	require.NoError(t, err)
	require.Equal(t, "华辰智能制造（更新）", updated.Name)
	require.Equal(t, "A", updated.Tier)
	require.Equal(t, "active", updated.LifecycleStage)
	require.Equal(t, int64(2), updated.Version)

	_, err = svc.UpdateCustomerMaster(ctx, provider.ID, 8, entity.ID, &customerRequest{
		Name:    "过期版本",
		Version: int64Ptr(1),
	})
	require.ErrorIs(t, err, ErrCustomerVersionConflict)

	_, err = svc.DisableCustomerMaster(ctx, provider.ID, 8, entity.ID)
	require.NoError(t, err)
	detail, err = svc.GetCustomerDetail(ctx, provider.ID, entity.ID)
	require.NoError(t, err)
	require.Equal(t, "inactive", detail.Customer.Status)
}

func TestCustomerMasterP0_ContactsPrimaryAndSoftDelete(t *testing.T) {
	client := openCustomerMasterTestClient(t, "customer-master-contacts")
	defer client.Close()
	ctx := context.Background()
	provider, _ := seedProviderAndCustomerTenant(t, client, "provider-contacts", "customer-contacts")
	svc := NewService(client)

	entity, err := svc.CreateCustomerMaster(ctx, provider.ID, 1, &customerRequest{Name: "联系人客户"})
	require.NoError(t, err)

	first, err := svc.CreateCustomerContact(ctx, provider.ID, entity.ID, &customerContactRequest{
		Name:        "主联系人 A",
		Email:       "a@example.com",
		ContactRole: "business",
		IsPrimary:   true,
	})
	require.NoError(t, err)
	require.True(t, first.IsPrimary)

	second, err := svc.CreateCustomerContact(ctx, provider.ID, entity.ID, &customerContactRequest{
		Name:        "主联系人 B",
		Email:       "b@example.com",
		ContactRole: "business",
		IsPrimary:   true,
	})
	require.NoError(t, err)
	require.True(t, second.IsPrimary)

	contacts, err := svc.ListCustomerContacts(ctx, provider.ID, entity.ID)
	require.NoError(t, err)
	require.Len(t, contacts, 2)
	for _, contact := range contacts {
		if contact.ID == first.ID {
			require.False(t, contact.IsPrimary)
		}
	}

	updated, err := svc.UpdateCustomerContact(ctx, provider.ID, entity.ID, first.ID, &customerContactRequest{
		Name:        "主联系人 A（更新）",
		Email:       "a2@example.com",
		ContactRole: "technical",
		IsPrimary:   true,
	})
	require.NoError(t, err)
	require.Equal(t, "a2@example.com", updated.Email)
	require.Equal(t, "technical", updated.ContactRole)
	require.True(t, updated.IsPrimary)

	require.NoError(t, svc.DeleteCustomerContact(ctx, provider.ID, entity.ID, first.ID))
	contacts, err = svc.ListCustomerContacts(ctx, provider.ID, entity.ID)
	require.NoError(t, err)
	require.Len(t, contacts, 1)
	require.Equal(t, second.ID, contacts[0].ID)
}

func TestCustomerMasterP0_TenantLinkValidationAndPrimaryConflict(t *testing.T) {
	client := openCustomerMasterTestClient(t, "customer-master-links")
	defer client.Close()
	ctx := context.Background()
	provider, customerTenantA := seedProviderAndCustomerTenant(t, client, "provider-links", "customer-links-a")
	customerTenantB := client.Tenant.Create().
		SetName("customer-links-b").
		SetCode("customer-links-b").
		SetType(tenantpkg.TypeMspCustomer).
		SetMspProviderID(provider.ID).
		SaveX(ctx)
	otherProvider, otherCustomerTenant := seedProviderAndCustomerTenant(t, client, "other-provider", "other-customer")
	svc := NewService(client)

	otherUser := client.User.Create().
		SetTenantID(otherProvider.ID).
		SetUsername("other-manager").
		SetEmail("other-manager@example.com").
		SetPasswordHash("x").
		SetName("其他租户经理").
		SetActive(true).
		SaveX(ctx)
	_, err := svc.CreateCustomerMaster(ctx, provider.ID, 1, &customerRequest{
		Name:             "跨租户客户经理",
		AccountManagerID: &otherUser.ID,
	})
	require.ErrorIs(t, err, ErrCustomerInvalidReference)

	entity, err := svc.CreateCustomerMaster(ctx, provider.ID, 1, &customerRequest{Name: "租户关联客户"})
	require.NoError(t, err)

	first, err := svc.CreateCustomerTenantLink(ctx, provider.ID, entity.ID, &customerTenantLinkRequest{
		LinkedTenantID:  customerTenantA.ID,
		LinkType:        "primary",
		IsBillingAnchor: true,
	})
	require.NoError(t, err)

	second, err := svc.CreateCustomerTenantLink(ctx, provider.ID, entity.ID, &customerTenantLinkRequest{
		LinkedTenantID:  customerTenantB.ID,
		LinkType:        "primary",
		IsBillingAnchor: true,
	})
	require.NoError(t, err)

	links, err := svc.ListCustomerTenantLinks(ctx, provider.ID, entity.ID)
	require.NoError(t, err)
	require.Len(t, links, 2)
	for _, link := range links {
		if link.ID == first.ID {
			require.Equal(t, "secondary", link.LinkType)
			require.False(t, link.IsBillingAnchor)
		}
		if link.ID == second.ID {
			require.Equal(t, "primary", link.LinkType)
			require.True(t, link.IsBillingAnchor)
		}
	}

	_, err = svc.CreateCustomerTenantLink(ctx, provider.ID, entity.ID, &customerTenantLinkRequest{
		LinkedTenantID: otherCustomerTenant.ID,
	})
	require.ErrorIs(t, err, ErrCustomerTenantLinkInvalid)

	_, err = svc.CreateCustomerTenantLink(ctx, provider.ID, entity.ID, &customerTenantLinkRequest{
		LinkedTenantID: otherProvider.ID,
	})
	require.ErrorIs(t, err, ErrCustomerTenantLinkInvalid)

	_, err = svc.CreateCustomerTenantLink(ctx, otherProvider.ID, entity.ID, &customerTenantLinkRequest{
		LinkedTenantID: customerTenantA.ID,
	})
	require.ErrorIs(t, err, ErrCustomerNotFound)

	_, err = svc.GetCustomerDetail(ctx, otherProvider.ID, entity.ID)
	require.ErrorIs(t, err, ErrCustomerNotFound)
}

func TestCustomerMasterP0_BranchFieldsAndSoftDelete(t *testing.T) {
	client := openCustomerMasterTestClient(t, "customer-master-branches")
	defer client.Close()
	ctx := context.Background()
	provider, _ := seedProviderAndCustomerTenant(t, client, "provider-branches", "customer-branches")
	svc := NewService(client)

	entity, err := svc.CreateCustomerMaster(ctx, provider.ID, 1, &customerRequest{Name: "服务点客户"})
	require.NoError(t, err)

	first, err := svc.CreateBranch(ctx, provider.ID, &branchRequest{
		CustomerID: entity.ID,
		Name:       "总部",
		Code:       "HQ-1",
		BranchType: "hq",
		Address:    map[string]any{"city": "上海"},
		IsPrimary:  true,
	})
	require.NoError(t, err)
	require.True(t, first.IsPrimary)

	second, err := svc.CreateBranch(ctx, provider.ID, &branchRequest{
		CustomerID: entity.ID,
		Name:       "分部",
		Code:       "SITE-1",
		BranchType: "site",
		IsPrimary:  true,
	})
	require.NoError(t, err)
	require.True(t, second.IsPrimary)

	branches, err := svc.ListBranches(ctx, provider.ID, entity.ID)
	require.NoError(t, err)
	require.Len(t, branches, 2)
	for _, branch := range branches {
		if branch.ID == first.ID {
			require.False(t, branch.IsPrimary)
		}
	}

	_, err = svc.DisableBranch(ctx, provider.ID, second.ID)
	require.NoError(t, err)
	branches, err = svc.ListBranches(ctx, provider.ID, entity.ID)
	require.NoError(t, err)
	require.Len(t, branches, 2)
	for _, branch := range branches {
		if branch.ID == second.ID {
			require.Equal(t, "inactive", branch.Status)
			require.False(t, branch.IsPrimary)
		}
	}
}

func boolPtr(value bool) *bool { return &value }

func int64Ptr(value int64) *int64 { return &value }
