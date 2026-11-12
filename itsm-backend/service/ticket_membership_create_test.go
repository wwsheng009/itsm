package service

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap/zaptest"

	"itsm-backend/dto"
	"itsm-backend/ent/enttest"
	"itsm-backend/ent/usertenantmembership"

	_ "github.com/mattn/go-sqlite3"
)

// TestResolveCreateMemberships_ProviderOnBehalf 锁定 C 方案服务层语义：
// requester_membership_id = 客户申请人 membership；
// created_by_membership_id = 当前 provider membership（由 handler 写入）。
func TestResolveCreateMemberships_ProviderOnBehalf(t *testing.T) {
	ctx := context.Background()
	client := enttest.Open(t, "sqlite3", "file:ticket-membership-create?mode=memory&cache=shared&_fk=1")
	t.Cleanup(func() { _ = client.Close() })

	providerTenant := client.Tenant.Create().SetCode("provider").SetName("provider").SaveX(ctx)
	customerTenant := client.Tenant.Create().SetCode("customer").SetName("customer").SaveX(ctx)
	customerUser := client.User.Create().
		SetUsername("customer-user").SetEmail("customer@example.com").SetName("customer-user").
		SetPasswordHash("hash").SetTenantID(customerTenant.ID).SetActive(true).SaveX(ctx)
	providerUser := client.User.Create().
		SetUsername("provider-user").SetEmail("provider@example.com").SetName("provider-user").
		SetPasswordHash("hash").SetTenantID(providerTenant.ID).SetActive(true).
		SetMspRole("provider_admin").SaveX(ctx)
	customerMembership := client.UserTenantMembership.Create().
		SetUserID(customerUser.ID).SetTenantID(customerTenant.ID).
		SetAccountKind(usertenantmembership.AccountKindCustomer).
		SetSource(usertenantmembership.SourceHome).
		SetStatus(usertenantmembership.StatusActive).SetIsDefault(true).SaveX(ctx)
	providerMembership := client.UserTenantMembership.Create().
		SetUserID(providerUser.ID).SetTenantID(customerTenant.ID).
		SetAccountKind(usertenantmembership.AccountKindProvider).
		SetSource(usertenantmembership.SourceAllocation).
		SetStatus(usertenantmembership.StatusActive).SetIsDefault(false).SaveX(ctx)

	svc := &TicketService{client: client, logger: zaptest.NewLogger(t).Sugar()}
	req := &dto.CreateTicketRequest{
		RequesterID:           customerUser.ID,
		CreatedByMembershipID: providerMembership.ID,
	}
	require.NoError(t, svc.resolveCreateMemberships(ctx, req, customerTenant.ID))
	assert.Equal(t, customerMembership.ID, req.RequesterMembershipID)
	assert.Equal(t, providerMembership.ID, req.CreatedByMembershipID)
}

func TestResolveCreateMemberships_RejectsNonMemberRequester(t *testing.T) {
	ctx := context.Background()
	client := enttest.Open(t, "sqlite3", "file:ticket-membership-missing?mode=memory&cache=shared&_fk=1")
	t.Cleanup(func() { _ = client.Close() })

	tenant := client.Tenant.Create().SetCode("customer-missing").SetName("customer").SaveX(ctx)
	user := client.User.Create().
		SetUsername("outsider").SetEmail("outsider@example.com").SetName("outsider").
		SetPasswordHash("hash").SetTenantID(tenant.ID).SetActive(true).SaveX(ctx)

	svc := &TicketService{client: client, logger: zaptest.NewLogger(t).Sugar()}
	req := &dto.CreateTicketRequest{
		RequesterID:           user.ID + 100,
		CreatedByMembershipID: 999, // 存在 actor membership 上下文 → 强制校验 requester membership
	}
	err := svc.resolveCreateMemberships(ctx, req, tenant.ID)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "申请人不是目标租户的有效成员")
}

func TestResolveCreateMemberships_LegacyDirectCallRequiresActiveHomeUser(t *testing.T) {
	ctx := context.Background()
	client := enttest.Open(t, "sqlite3", "file:ticket-membership-legacy?mode=memory&cache=shared&_fk=1")
	t.Cleanup(func() { _ = client.Close() })

	svc := &TicketService{client: client, logger: zaptest.NewLogger(t).Sugar()}
	tenant := client.Tenant.Create().SetCode("legacy").SetName("legacy").SaveX(ctx)
	account := client.User.Create().SetUsername("legacy").SetName("legacy").
		SetEmail("legacy@example.com").SetPasswordHash("hash").SetTenantID(tenant.ID).SetActive(true).SaveX(ctx)
	req := &dto.CreateTicketRequest{RequesterID: account.ID}
	require.NoError(t, svc.resolveCreateMemberships(ctx, req, tenant.ID))
	assert.Zero(t, req.RequesterMembershipID)
	client.User.UpdateOneID(account.ID).SetActive(false).ExecX(ctx)
	require.Error(t, svc.resolveCreateMemberships(ctx, req, tenant.ID))
}
