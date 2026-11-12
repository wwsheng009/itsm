package membershipref

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"itsm-backend/common"
	"itsm-backend/ent/enttest"
	"itsm-backend/ent/usertenantmembership"

	_ "github.com/mattn/go-sqlite3"
)

func TestResolveAssignableRejectsCrossTenantUser(t *testing.T) {
	ctx := context.Background()
	client := enttest.Open(t, "sqlite3", "file:membershipref-cross-tenant?mode=memory&cache=shared&_fk=1")
	t.Cleanup(func() { _ = client.Close() })

	tenantA := client.Tenant.Create().SetCode("ref-a").SetName("ref A").SaveX(ctx)
	tenantB := client.Tenant.Create().SetCode("ref-b").SetName("ref B").SaveX(ctx)
	userB := client.User.Create().
		SetUsername("ref-user-b").SetEmail("ref-b@example.com").SetName("user B").
		SetPasswordHash("hash").SetTenantID(tenantB.ID).SetActive(true).SaveX(ctx)
	client.UserTenantMembership.Create().
		SetUserID(userB.ID).SetTenantID(tenantB.ID).
		SetAccountKind(usertenantmembership.AccountKindCustomer).
		SetSource(usertenantmembership.SourceHome).
		SetStatus(usertenantmembership.StatusActive).SetIsDefault(true).SaveX(ctx)

	_, err := ResolveAssignable(ctx, client, userB.ID, tenantA.ID)
	require.Error(t, err)
	appErr, ok := common.AsAppError(err)
	require.True(t, ok, "cross-tenant assignee must be a typed AppError, got %T: %v", err, err)
	require.Equal(t, http.StatusBadRequest, appErr.HTTPStatus)
}

func TestResolveActorRejectsMismatchedMembership(t *testing.T) {
	ctx := context.Background()
	client := enttest.Open(t, "sqlite3", "file:membershipref-mismatch?mode=memory&cache=shared&_fk=1")
	t.Cleanup(func() { _ = client.Close() })

	tenant := client.Tenant.Create().SetCode("ref-mismatch").SetName("ref mismatch").SaveX(ctx)
	user := client.User.Create().
		SetUsername("ref-user").SetEmail("ref@example.com").SetName("user").
		SetPasswordHash("hash").SetTenantID(tenant.ID).SetActive(true).SaveX(ctx)
	membership := client.UserTenantMembership.Create().
		SetUserID(user.ID).SetTenantID(tenant.ID).
		SetAccountKind(usertenantmembership.AccountKindCustomer).
		SetSource(usertenantmembership.SourceHome).
		SetStatus(usertenantmembership.StatusActive).SetIsDefault(true).SaveX(ctx)

	_, err := ResolveActor(ctx, client, user.ID, tenant.ID, membership.ID+999)
	require.Error(t, err)
	appErr, ok := common.AsAppError(err)
	require.True(t, ok, "mismatched membership must be a typed AppError, got %T: %v", err, err)
	require.Equal(t, http.StatusForbidden, appErr.HTTPStatus)
}

func TestResolveAssignableAccountAndMembershipLifecycle(t *testing.T) {
	for _, state := range []string{"active", "disabled", "suspended", "expired", "revoked", "legacy", "foreign"} {
		t.Run(state, func(t *testing.T) {
			ctx := context.Background()
			client := enttest.Open(t, "sqlite3", "file:membershipref-lifecycle-"+state+"?mode=memory&cache=shared&_fk=1")
			t.Cleanup(func() { _ = client.Close() })
			tenant := client.Tenant.Create().SetCode("lifecycle").SetName("lifecycle").SaveX(ctx)
			home := tenant.ID
			if state == "foreign" {
				home = client.Tenant.Create().SetCode("foreign").SetName("foreign").SaveX(ctx).ID
			}
			account := client.User.Create().SetUsername("member").SetName("member").
				SetEmail("member@example.com").SetPasswordHash("hash").
				SetTenantID(home).SetActive(state != "disabled").SaveX(ctx)
			wantID := 0
			if state != "legacy" && state != "foreign" {
				builder := client.UserTenantMembership.Create().
					SetUserID(account.ID).SetTenantID(tenant.ID).
					SetAccountKind(usertenantmembership.AccountKindCustomer).
					SetSource(usertenantmembership.SourceHome).SetStatus(usertenantmembership.StatusActive)
				switch state {
				case "suspended":
					builder.SetStatus(usertenantmembership.StatusSuspended)
				case "expired":
					builder.SetExpiresAt(time.Now().Add(-time.Hour))
				case "revoked":
					builder.SetDeletedAt(time.Now())
				}
				wantID = builder.SaveX(ctx).ID
			}
			gotID, err := ResolveAssignable(ctx, client, account.ID, tenant.ID)
			if state == "active" || state == "legacy" {
				require.NoError(t, err)
				require.Equal(t, wantID, gotID)
			} else {
				require.Error(t, err)
				require.Zero(t, gotID)
			}
		})
	}
}
