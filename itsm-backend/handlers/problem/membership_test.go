package problem

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"go.uber.org/zap"

	"itsm-backend/common/tenantctx"
	"itsm-backend/ent"
	"itsm-backend/ent/enttest"
	"itsm-backend/ent/usertenantmembership"
	"itsm-backend/handlers/common/datascope"

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

func TestProblemServiceHistoricalAssigneeDoesNotBlockMaintenance(t *testing.T) {
	for _, invalidation := range []string{"revoked", "suspended", "expired", "account_disabled"} {
		t.Run(invalidation, func(t *testing.T) {
			ctx, repo, tenantID, creatorID, creatorMembershipID, assigneeID, assigneeMembershipID := newProblemMembershipFixture(t)
			ctx = datascope.WithDataScope(tenantctx.WithTenantID(ctx, tenantID), datascope.DataScopeAll)
			svc := NewService(repo, zap.NewNop().Sugar())
			historical, err := svc.Create(ctx, tenantID, &Problem{
				Title: "historical assignee", Priority: "medium", CreatedBy: creatorID,
				CreatedByMembershipID: creatorMembershipID, AssigneeID: &assigneeID,
			})
			require.NoError(t, err)
			unassigned, err := svc.Create(ctx, tenantID, &Problem{
				Title: "unassigned", Priority: "medium", CreatedBy: creatorID, CreatedByMembershipID: creatorMembershipID,
			})
			require.NoError(t, err)
			switch invalidation {
			case "revoked":
				repo.client.UserTenantMembership.UpdateOneID(assigneeMembershipID).SetDeletedAt(time.Now()).SaveX(ctx)
			case "suspended":
				repo.client.UserTenantMembership.UpdateOneID(assigneeMembershipID).SetStatus(usertenantmembership.StatusSuspended).SaveX(ctx)
			case "expired":
				repo.client.UserTenantMembership.UpdateOneID(assigneeMembershipID).SetExpiresAt(time.Now().Add(-time.Hour)).SaveX(ctx)
			case "account_disabled":
				repo.client.User.UpdateOneID(assigneeID).SetActive(false).SaveX(ctx)
			}

			updated, err := svc.Update(ctx, tenantID, historical.ID, &Problem{Title: "maintained by admin"}, creatorID, "admin")
			require.NoError(t, err)
			require.Equal(t, "maintained by admin", updated.Title)
			require.Equal(t, assigneeID, *updated.AssigneeID)
			require.Equal(t, assigneeMembershipID, updated.AssigneeMembershipID)
			_, err = svc.Update(ctx, tenantID, historical.ID, &Problem{Status: "resolved", Resolution: "fixed"}, creatorID, "admin")
			require.NoError(t, err)
			closed, err := svc.CloseProblem(ctx, tenantID, historical.ID, creatorID, "admin", "fixed")
			require.NoError(t, err)
			require.Equal(t, "closed", closed.Status)
			require.NotNil(t, closed.ClosedAt)
			require.Equal(t, assigneeID, *closed.AssigneeID)
			require.Equal(t, assigneeMembershipID, closed.AssigneeMembershipID)

			_, err = svc.Update(ctx, tenantID, unassigned.ID, &Problem{Title: "must not persist", AssigneeID: &assigneeID}, creatorID, "admin")
			require.Error(t, err, "new assignments must still reject an unavailable identity")
			unchanged := repo.client.Problem.GetX(ctx, unassigned.ID)
			require.Equal(t, "unassigned", unchanged.Title)
			require.Zero(t, unchanged.AssigneeID)
			require.Zero(t, unchanged.AssigneeMembershipID)

			// A client-provided account change is not mistaken for preserved history.
			reassigned, err := svc.Update(ctx, tenantID, historical.ID, &Problem{AssigneeID: &creatorID}, creatorID, "admin")
			require.NoError(t, err)
			require.Equal(t, creatorID, *reassigned.AssigneeID)
			require.Equal(t, creatorMembershipID, reassigned.AssigneeMembershipID)
			_, err = svc.Update(ctx, tenantID, historical.ID, &Problem{AssigneeID: &assigneeID}, creatorID, "admin")
			require.Error(t, err)
			zero := 0
			cleared, err := svc.Update(ctx, tenantID, historical.ID, &Problem{AssigneeID: &zero}, creatorID, "admin")
			require.NoError(t, err)
			require.Nil(t, cleared.AssigneeID)
			require.Zero(t, cleared.AssigneeMembershipID)
		})
	}
}

func TestProblemUpdateKeepsHistoricalMembershipInsteadOfRebinding(t *testing.T) {
	ctx, repo, tenantID, creatorID, creatorMembershipID, assigneeID, oldMembershipID := newProblemMembershipFixture(t)
	created, err := repo.Create(ctx, &Problem{
		Title: "historical pair", Priority: "medium", Status: "open", CreatedBy: creatorID,
		CreatedByMembershipID: creatorMembershipID, TenantID: tenantID, AssigneeID: &assigneeID,
	})
	require.NoError(t, err)
	repo.client.UserTenantMembership.UpdateOneID(oldMembershipID).SetDeletedAt(time.Now()).SaveX(ctx)
	newMembership := repo.client.UserTenantMembership.Create().SetUserID(assigneeID).SetTenantID(tenantID).
		SetAccountKind(usertenantmembership.AccountKindCustomer).SetSource(usertenantmembership.SourceInvite).SaveX(ctx)
	created.Title = "retain original identity"
	updated, err := repo.Update(ctx, created)
	require.NoError(t, err)
	require.Equal(t, oldMembershipID, updated.AssigneeMembershipID)

	updated.AssigneeMembershipID = newMembership.ID
	updated.Title = "tampered reference"
	_, err = repo.Update(ctx, updated)
	require.Error(t, err, "an explicit membership change must not be silently accepted")
	stored := repo.client.Problem.GetX(ctx, created.ID)
	require.Equal(t, "retain original identity", stored.Title)
	require.Equal(t, oldMembershipID, stored.AssigneeMembershipID)

	// Repository nil means clearing; both persisted identity columns must clear.
	updated.AssigneeID = nil
	cleared, err := repo.Update(ctx, updated)
	require.NoError(t, err)
	require.Nil(t, cleared.AssigneeID)
	require.Zero(t, cleared.AssigneeMembershipID)
}

func TestProblemUpdateRejectsConcurrentIdentityChange(t *testing.T) {
	ctx, repo, tenantID, creatorID, creatorMembershipID, assigneeID, _ := newProblemMembershipFixture(t)
	created, err := repo.Create(ctx, &Problem{
		Title: "original", Priority: "medium", Status: "open", CreatedBy: creatorID,
		CreatedByMembershipID: creatorMembershipID, TenantID: tenantID, AssigneeID: &assigneeID,
	})
	require.NoError(t, err)
	injected := false
	repo.client.Problem.Use(func(next ent.Mutator) ent.Mutator {
		return ent.MutateFunc(func(ctx context.Context, mutation ent.Mutation) (ent.Value, error) {
			if !injected {
				injected = true
				repo.client.Problem.UpdateOneID(created.ID).SetAssigneeID(creatorID).SetAssigneeMembershipID(creatorMembershipID).SaveX(ctx)
			}
			return next.Mutate(ctx, mutation)
		})
	})
	created.Title = "stale write"
	_, err = repo.Update(ctx, created)
	require.Error(t, err)
	stored := repo.client.Problem.GetX(ctx, created.ID)
	require.Equal(t, "original", stored.Title)
	require.Equal(t, creatorID, stored.AssigneeID)
	require.Equal(t, creatorMembershipID, stored.AssigneeMembershipID)
}
