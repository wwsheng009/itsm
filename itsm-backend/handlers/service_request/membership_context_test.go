package service_request

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"itsm-backend/ent/enttest"
	"itsm-backend/ent/usertenantmembership"

	_ "github.com/mattn/go-sqlite3"
)

func TestGetUserContextAllowsProviderMembership(t *testing.T) {
	ctx := context.Background()
	client := enttest.Open(t, "sqlite3", "file:sr-user-context-provider?mode=memory&cache=shared&_fk=1")
	t.Cleanup(func() { _ = client.Close() })

	providerTenant := client.Tenant.Create().SetCode("ctx-provider").SetName("provider").SaveX(ctx)
	customerTenant := client.Tenant.Create().SetCode("ctx-customer").SetName("customer").SaveX(ctx)
	providerUser := client.User.Create().
		SetUsername("ctx-provider-user").SetEmail("ctx-provider@example.com").SetName("Provider User").
		SetPasswordHash("hash").SetTenantID(providerTenant.ID).SetActive(true).
		SetDepartment("provider-delivery").SaveX(ctx)
	client.UserTenantMembership.Create().
		SetUserID(providerUser.ID).SetTenantID(customerTenant.ID).
		SetAccountKind(usertenantmembership.AccountKindProvider).
		SetSource(usertenantmembership.SourceAllocation).
		SetStatus(usertenantmembership.StatusActive).SetIsDefault(false).SaveX(ctx)

	repo := NewEntRepository(client)
	department, name, err := repo.GetUserContext(ctx, providerUser.ID, customerTenant.ID)
	require.NoError(t, err)
	require.Equal(t, "provider-delivery", department)
	require.Equal(t, "Provider User", name)
}

func TestGetUserContextRejectsNonMember(t *testing.T) {
	ctx := context.Background()
	client := enttest.Open(t, "sqlite3", "file:sr-user-context-nonmember?mode=memory&cache=shared&_fk=1")
	t.Cleanup(func() { _ = client.Close() })

	providerTenant := client.Tenant.Create().SetCode("ctx-provider-2").SetName("provider").SaveX(ctx)
	customerTenant := client.Tenant.Create().SetCode("ctx-customer-2").SetName("customer").SaveX(ctx)
	outsider := client.User.Create().
		SetUsername("ctx-outsider").SetEmail("ctx-outsider@example.com").SetName("Outsider").
		SetPasswordHash("hash").SetTenantID(providerTenant.ID).SetActive(true).SaveX(ctx)

	repo := NewEntRepository(client)
	_, _, err := repo.GetUserContext(ctx, outsider.ID, customerTenant.ID)
	require.Error(t, err)
}
