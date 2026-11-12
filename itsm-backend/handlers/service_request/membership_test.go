package service_request

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"itsm-backend/ent/enttest"
	"itsm-backend/ent/usertenantmembership"

	_ "github.com/mattn/go-sqlite3"
)

func newServiceRequestMembershipFixture(t *testing.T) (context.Context, *EntRepository, int, int, int, int, int) {
	t.Helper()
	ctx := context.Background()
	client := enttest.Open(t, "sqlite3", "file:service-request-membership?mode=memory&cache=shared&_fk=1")
	t.Cleanup(func() { _ = client.Close() })

	tenant := client.Tenant.Create().SetCode("sr-membership").SetName("sr membership").SaveX(ctx)
	requester := client.User.Create().
		SetUsername("sr-requester").SetEmail("sr-requester@example.com").SetName("requester").
		SetPasswordHash("hash").SetTenantID(tenant.ID).SetActive(true).SaveX(ctx)
	processor := client.User.Create().
		SetUsername("sr-processor").SetEmail("sr-processor@example.com").SetName("processor").
		SetPasswordHash("hash").SetTenantID(tenant.ID).SetActive(true).SaveX(ctx)
	requesterMembership := client.UserTenantMembership.Create().
		SetUserID(requester.ID).SetTenantID(tenant.ID).
		SetAccountKind(usertenantmembership.AccountKindCustomer).
		SetSource(usertenantmembership.SourceHome).
		SetStatus(usertenantmembership.StatusActive).SetIsDefault(true).SaveX(ctx)
	processorMembership := client.UserTenantMembership.Create().
		SetUserID(processor.ID).SetTenantID(tenant.ID).
		SetAccountKind(usertenantmembership.AccountKindProvider).
		SetSource(usertenantmembership.SourceAllocation).
		SetStatus(usertenantmembership.StatusActive).SetIsDefault(false).SaveX(ctx)

	return ctx, NewEntRepository(client), tenant.ID, requester.ID, requesterMembership.ID, processor.ID, processorMembership.ID
}

func TestServiceRequestCreatePersistsRequesterMembership(t *testing.T) {
	ctx, repo, tenantID, requesterID, requesterMembershipID, _, _ := newServiceRequestMembershipFixture(t)

	created, err := repo.Create(ctx, &ServiceRequest{
		TenantID:              tenantID,
		CatalogID:             1,
		RequesterID:           requesterID,
		RequesterMembershipID: requesterMembershipID,
		Status:                SRStatusSubmitted,
		CurrentLevel:          1,
		TotalLevels:           3,
		DataClassification:    "internal",
	}, nil)
	require.NoError(t, err)
	require.Equal(t, requesterMembershipID, created.RequesterMembershipID)
}

func TestServiceRequestProvisioningPersistsProcessorMembership(t *testing.T) {
	ctx, repo, tenantID, requesterID, requesterMembershipID, processorID, processorMembershipID := newServiceRequestMembershipFixture(t)

	created, err := repo.Create(ctx, &ServiceRequest{
		TenantID:              tenantID,
		CatalogID:             1,
		RequesterID:           requesterID,
		RequesterMembershipID: requesterMembershipID,
		Status:                SRStatusSubmitted,
		CurrentLevel:          1,
		TotalLevels:           3,
		DataClassification:    "internal",
	}, nil)
	require.NoError(t, err)

	require.NoError(t, repo.UpdateStatus(ctx, created, SRStatusProvisioning, processorID, processorMembershipID))

	stored, err := repo.Get(ctx, created.ID, tenantID)
	require.NoError(t, err)
	require.NotNil(t, stored.ProcessorID)
	require.Equal(t, processorID, *stored.ProcessorID)
	require.Equal(t, processorMembershipID, stored.ProcessorMembershipID)
}

func TestServiceRequestCreateRejectsMismatchedRequesterMembership(t *testing.T) {
	ctx, repo, tenantID, requesterID, _, _, _ := newServiceRequestMembershipFixture(t)

	_, err := repo.Create(ctx, &ServiceRequest{
		TenantID:              tenantID,
		CatalogID:             1,
		RequesterID:           requesterID,
		RequesterMembershipID: 999999,
		Status:                SRStatusSubmitted,
		CurrentLevel:          1,
		TotalLevels:           3,
		DataClassification:    "internal",
	}, nil)
	require.Error(t, err)
	require.Contains(t, err.Error(), "membership 上下文与目标租户不一致")
}
