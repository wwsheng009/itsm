package change

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"itsm-backend/ent/enttest"
	"itsm-backend/ent/usertenantmembership"

	_ "github.com/mattn/go-sqlite3"
)

func newChangeMembershipFixture(t *testing.T) (context.Context, *EntRepository, int, int, int, int, int) {
	t.Helper()
	ctx := context.Background()
	client := enttest.Open(t, "sqlite3", "file:change-membership?mode=memory&cache=shared&_fk=1")
	t.Cleanup(func() { _ = client.Close() })

	tenant := client.Tenant.Create().SetCode("change-membership").SetName("change membership").SaveX(ctx)
	creator := client.User.Create().
		SetUsername("change-creator").SetEmail("change-creator@example.com").SetName("creator").
		SetPasswordHash("hash").SetTenantID(tenant.ID).SetActive(true).SaveX(ctx)
	assignee := client.User.Create().
		SetUsername("change-assignee").SetEmail("change-assignee@example.com").SetName("assignee").
		SetPasswordHash("hash").SetTenantID(tenant.ID).SetActive(true).SaveX(ctx)
	creatorMembership := client.UserTenantMembership.Create().
		SetUserID(creator.ID).SetTenantID(tenant.ID).
		SetAccountKind(usertenantmembership.AccountKindCustomer).
		SetSource(usertenantmembership.SourceHome).
		SetStatus(usertenantmembership.StatusActive).SetIsDefault(true).SaveX(ctx)
	assigneeMembership := client.UserTenantMembership.Create().
		SetUserID(assignee.ID).SetTenantID(tenant.ID).
		SetAccountKind(usertenantmembership.AccountKindProvider).
		SetSource(usertenantmembership.SourceAllocation).
		SetStatus(usertenantmembership.StatusActive).SetIsDefault(false).SaveX(ctx)

	return ctx, NewEntRepository(client, nil), tenant.ID, creator.ID, creatorMembership.ID, assignee.ID, assigneeMembership.ID
}

func TestChangeCreatePersistsCreatorMembership(t *testing.T) {
	ctx, repo, tenantID, creatorID, creatorMembershipID, _, _ := newChangeMembershipFixture(t)

	created, err := repo.Create(ctx, &Change{
		Title:                 "membership change",
		Type:                  "normal",
		Status:                "draft",
		Priority:              "medium",
		ImpactScope:           "medium",
		RiskLevel:             "medium",
		CreatedBy:             creatorID,
		CreatedByMembershipID: creatorMembershipID,
		TenantID:              tenantID,
	})
	require.NoError(t, err)
	require.Equal(t, creatorMembershipID, created.CreatedByMembershipID)
	require.Zero(t, created.AssigneeMembershipID)
}

func TestChangeUpdateResolvesAssigneeMembership(t *testing.T) {
	ctx, repo, tenantID, creatorID, creatorMembershipID, assigneeID, assigneeMembershipID := newChangeMembershipFixture(t)

	created, err := repo.Create(ctx, &Change{
		Title:                 "assign membership change",
		Type:                  "normal",
		Status:                "draft",
		Priority:              "medium",
		ImpactScope:           "medium",
		RiskLevel:             "medium",
		CreatedBy:             creatorID,
		CreatedByMembershipID: creatorMembershipID,
		TenantID:              tenantID,
	})
	require.NoError(t, err)

	created.AssigneeID = &assigneeID
	updated, err := repo.Update(ctx, created)
	require.NoError(t, err)
	require.Equal(t, assigneeMembershipID, updated.AssigneeMembershipID)
}

func TestChangeCreateRejectsMismatchedActorMembership(t *testing.T) {
	ctx, repo, tenantID, creatorID, _, _, _ := newChangeMembershipFixture(t)

	_, err := repo.Create(ctx, &Change{
		Title:                 "mismatched membership change",
		Type:                  "normal",
		Status:                "draft",
		Priority:              "medium",
		ImpactScope:           "medium",
		RiskLevel:             "medium",
		CreatedBy:             creatorID,
		CreatedByMembershipID: 999999,
		TenantID:              tenantID,
	})
	require.Error(t, err)
	require.Contains(t, err.Error(), "membership 上下文与目标租户不一致")
}
