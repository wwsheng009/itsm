package problem

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"itsm-backend/ent/enttest"
	"itsm-backend/ent/usertenantmembership"

	_ "github.com/mattn/go-sqlite3"
)

func newProblemMembershipFixture(t *testing.T) (context.Context, *EntRepository, int, int, int, int, int) {
	t.Helper()
	ctx := context.Background()
	client := enttest.Open(t, "sqlite3", "file:problem-membership?mode=memory&cache=shared&_fk=1")
	t.Cleanup(func() { _ = client.Close() })

	tenant := client.Tenant.Create().SetCode("problem-membership").SetName("problem membership").SaveX(ctx)
	creator := client.User.Create().
		SetUsername("problem-creator").SetEmail("problem-creator@example.com").SetName("creator").
		SetPasswordHash("hash").SetTenantID(tenant.ID).SetActive(true).SaveX(ctx)
	assignee := client.User.Create().
		SetUsername("problem-assignee").SetEmail("problem-assignee@example.com").SetName("assignee").
		SetPasswordHash("hash").SetTenantID(tenant.ID).SetActive(true).SaveX(ctx)
	creatorMembership := client.UserTenantMembership.Create().
		SetUserID(creator.ID).SetTenantID(tenant.ID).
		SetAccountKind(usertenantmembership.AccountKindCustomer).
		SetSource(usertenantmembership.SourceHome).
		SetStatus(usertenantmembership.StatusActive).SetIsDefault(true).SaveX(ctx)
	assigneeMembership := client.UserTenantMembership.Create().
		SetUserID(assignee.ID).SetTenantID(tenant.ID).
		SetAccountKind(usertenantmembership.AccountKindCustomer).
		SetSource(usertenantmembership.SourceHome).
		SetStatus(usertenantmembership.StatusActive).SetIsDefault(true).SaveX(ctx)

	return ctx, NewEntRepository(client), tenant.ID, creator.ID, creatorMembership.ID, assignee.ID, assigneeMembership.ID
}

func TestProblemCreatePersistsMembershipReferences(t *testing.T) {
	ctx, repo, tenantID, creatorID, creatorMembershipID, assigneeID, assigneeMembershipID := newProblemMembershipFixture(t)

	created, err := repo.Create(ctx, &Problem{
		Title:                 "membership problem",
		Priority:              "high",
		Status:                "open",
		CreatedBy:             creatorID,
		CreatedByMembershipID: creatorMembershipID,
		AssigneeID:            &assigneeID,
		TenantID:              tenantID,
	})
	require.NoError(t, err)
	require.Equal(t, creatorMembershipID, created.CreatedByMembershipID)
	require.Equal(t, assigneeMembershipID, created.AssigneeMembershipID)
}

func TestProblemCreateRejectsMismatchedActorMembership(t *testing.T) {
	ctx, repo, tenantID, creatorID, _, _, _ := newProblemMembershipFixture(t)

	_, err := repo.Create(ctx, &Problem{
		Title:                 "mismatched membership",
		Priority:              "high",
		Status:                "open",
		CreatedBy:             creatorID,
		CreatedByMembershipID: 999999,
		TenantID:              tenantID,
	})
	require.Error(t, err)
	require.Contains(t, err.Error(), "membership 上下文与目标租户不一致")
}

func TestProblemUpdateResolvesAssigneeMembership(t *testing.T) {
	ctx, repo, tenantID, creatorID, creatorMembershipID, assigneeID, assigneeMembershipID := newProblemMembershipFixture(t)

	created, err := repo.Create(ctx, &Problem{
		Title:                 "update assignee",
		Priority:              "medium",
		Status:                "open",
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
