package middleware

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"itsm-backend/common"
	"itsm-backend/ent"
	"itsm-backend/ent/user"
	"itsm-backend/ent/usertenantmembership"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func membershipRequestRouter(t *testing.T, client *ent.Client) *gin.Engine {
	t.Helper()
	gin.SetMode(gin.TestMode)
	oldMode, oldCache, oldFallback := PermissionConfig.Mode, PermissionConfig.EnableCache, AuthzStaticFallbackEnabled()
	oldStore := currentAccessTokenRevocationStore()
	PermissionConfig.Mode, PermissionConfig.EnableCache = PermissionConfigModeDBOnly, true
	SetAuthzStaticFallback(false)
	setAccessTokenRevocationStore(newMemoryAccessTokenRevocationStore())
	t.Cleanup(func() {
		PermissionConfig.Mode, PermissionConfig.EnableCache = oldMode, oldCache
		SetAuthzStaticFallback(oldFallback)
		setAccessTokenRevocationStore(oldStore)
	})
	r := gin.New()
	// Same production order as router.go and ticket_routes.go. The terminal
	// handler must never be reached by a stale tenant identity.
	r.DELETE("/api/v1/tickets/:id/cc/:cc_id", AuthMiddleware("membership-lifecycle-test"),
		RBACMiddleware(client), RequirePermission("ticket", "update"), func(c *gin.Context) {
			c.Status(http.StatusNoContent)
		})
	return r
}

func membershipCCRequest(r *gin.Engine, token string) *httptest.ResponseRecorder {
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodDelete, "/api/v1/tickets/1/cc/1", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	r.ServeHTTP(w, req)
	return w
}

func requireMembershipDenied(t *testing.T, w *httptest.ResponseRecorder) {
	t.Helper()
	require.Equal(t, http.StatusForbidden, w.Code, w.Body.String())
	var response struct {
		Code int `json:"code"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &response))
	require.Equal(t, common.ForbiddenCode, response.Code)
}

func TestRBACMembershipLifecycleRejectsStaleAccessToken(t *testing.T) {
	for _, state := range []string{"revoked", "suspended", "expired", "disabled"} {
		t.Run(state, func(t *testing.T) {
			ctx := context.Background()
			client := membershipTestClient(t)
			r := membershipRequestRouter(t, client)
			home := membershipTestTenant(t, ctx, client, "lifecycle-provider")
			customer := membershipTestTenant(t, ctx, client, "lifecycle-customer")
			account := membershipTestUser(t, ctx, client, "lifecycle-operator", home.ID, user.RoleAdmin)
			role := seedMembershipRole(t, ctx, client, customer.ID, "msp_tech", [][2]string{{"ticket", "update"}})
			m, err := client.UserTenantMembership.Create().
				SetUserID(account.ID).SetTenantID(customer.ID).SetRoleID(role.ID).
				SetAccountKind(usertenantmembership.AccountKindProvider).
				SetSource(usertenantmembership.SourceAllocation).Save(ctx)
			require.NoError(t, err)
			token, err := GenerateAccessTokenWithSource(account.ID, account.Username, "admin", customer.ID, "switch", "membership-lifecycle-test", time.Hour)
			require.NoError(t, err)
			require.Equal(t, http.StatusNoContent, membershipCCRequest(r, token).Code)
			_, _, err = ResolvePermissions(ctx, client, account.ID, customer.ID)
			require.NoError(t, err) // Prime the cache before changing identity.

			switch state {
			case "revoked":
				err = m.Update().SetDeletedAt(time.Now()).Exec(ctx)
			case "suspended":
				err = m.Update().SetStatus(usertenantmembership.StatusSuspended).Exec(ctx)
			case "expired":
				err = m.Update().SetExpiresAt(time.Now().Add(-time.Minute)).Exec(ctx)
			case "disabled":
				err = account.Update().SetActive(false).Exec(ctx)
			}
			require.NoError(t, err)
			// Deliberately no token blacklist or cache invalidation: membership
			// itself must revoke access even across independent API instances.
			requireMembershipDenied(t, membershipCCRequest(r, token))
			codes, _, err := ResolvePermissions(ctx, client, account.ID, customer.ID)
			require.NoError(t, err)
			require.Empty(t, codes)
		})
	}
}

func TestRBACMembershipUsesCurrentRoleNotJWT(t *testing.T) {
	ctx := context.Background()
	client := membershipTestClient(t)
	r := membershipRequestRouter(t, client)
	scope := membershipTestTenant(t, ctx, client, "current-role")
	account := membershipTestUser(t, ctx, client, "current-role-user", scope.ID, user.RoleAdmin)
	admin := seedMembershipRole(t, ctx, client, scope.ID, "admin", [][2]string{{"ticket", "update"}})
	reader := seedMembershipRole(t, ctx, client, scope.ID, "end_user", [][2]string{{"ticket", "read"}})
	m, err := client.UserTenantMembership.Create().
		SetUserID(account.ID).SetTenantID(scope.ID).SetRoleID(admin.ID).
		SetSource(usertenantmembership.SourceHome).Save(ctx)
	require.NoError(t, err)
	token, err := GenerateAccessToken(account.ID, account.Username, "admin", scope.ID, "membership-lifecycle-test", time.Hour)
	require.NoError(t, err)
	require.Equal(t, http.StatusNoContent, membershipCCRequest(r, token).Code)
	before, _, err := ResolvePermissions(ctx, client, account.ID, scope.ID)
	require.NoError(t, err)
	require.Contains(t, before, "ticket:update")
	require.NoError(t, m.Update().SetRoleID(reader.ID).Exec(ctx))
	requireMembershipDenied(t, membershipCCRequest(r, token))
	after, _, err := ResolvePermissions(ctx, client, account.ID, scope.ID)
	require.NoError(t, err)
	require.Equal(t, []string{"ticket:read"}, after, "another API instance may not receive the local cache eviction")
}

func TestRBACMembershipLegacyFallbackCannotUndoRevocation(t *testing.T) {
	ctx := context.Background()
	client := membershipTestClient(t)
	r := membershipRequestRouter(t, client)
	scope := membershipTestTenant(t, ctx, client, "legacy-home")
	other := membershipTestTenant(t, ctx, client, "legacy-other")
	account := membershipTestUser(t, ctx, client, "legacy-user", scope.ID, user.RoleAdmin)
	seedMembershipRole(t, ctx, client, scope.ID, "admin", [][2]string{{"ticket", "update"}})
	token, err := GenerateAccessToken(account.ID, account.Username, "admin", scope.ID, "membership-lifecycle-test", time.Hour)
	require.NoError(t, err)
	requireMembershipDenied(t, membershipCCRequest(r, token))
	SetAuthzStaticFallback(true)
	require.Equal(t, http.StatusNoContent, membershipCCRequest(r, token).Code)
	foreign, err := GenerateAccessToken(account.ID, account.Username, "admin", other.ID, "membership-lifecycle-test", time.Hour)
	require.NoError(t, err)
	requireMembershipDenied(t, membershipCCRequest(r, foreign))
	_, err = client.UserTenantMembership.Create().SetUserID(account.ID).SetTenantID(scope.ID).
		SetSource(usertenantmembership.SourceInvite).SetDeletedAt(time.Now()).Save(ctx)
	require.NoError(t, err)
	requireMembershipDenied(t, membershipCCRequest(r, token))
	codes, _, err := ResolvePermissions(ctx, client, account.ID, scope.ID)
	require.NoError(t, err)
	require.Empty(t, codes)
}

func TestRBACMembershipPlatformExceptionRequiresStoredRootAndExplicitSwitch(t *testing.T) {
	ctx := context.Background()
	client := membershipTestClient(t)
	r := membershipRequestRouter(t, client)
	root := membershipTestTenant(t, ctx, client, "default")
	customer := membershipTestTenant(t, ctx, client, "platform-exception-customer")
	operator := membershipTestUser(t, ctx, client, "root-operator", root.ID, user.RoleSuperAdmin)
	spoofed := membershipTestUser(t, ctx, client, "not-root-operator", customer.ID, user.RoleEndUser)
	for _, tc := range []struct {
		name   string
		user   *ent.User
		source string
		allow  bool
	}{
		{"audited platform switch", operator, "switch", true},
		{"no explicit switch", operator, "", false},
		{"JWT role is not stored platform identity", spoofed, "switch", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			token, err := GenerateAccessTokenWithSource(tc.user.ID, tc.user.Username, "super_admin",
				customer.ID, tc.source, "membership-lifecycle-test", time.Hour)
			require.NoError(t, err)
			w := membershipCCRequest(r, token)
			if tc.allow {
				require.Equal(t, http.StatusNoContent, w.Code, w.Body.String())
			} else {
				requireMembershipDenied(t, w)
			}
		})
	}
}

func TestResolvePermissionsCacheRechecksAdditionalRoles(t *testing.T) {
	for _, mutation := range []string{"disable", "detach", "grant"} {
		t.Run(mutation, func(t *testing.T) {
			ctx := context.Background()
			client := membershipTestClient(t)
			membershipRequestRouter(t, client) // Enable caches and DB-only permissions.
			scope := membershipTestTenant(t, ctx, client, "extra-role-cache")
			account := membershipTestUser(t, ctx, client, "extra-role-reader", scope.ID, user.RoleEndUser)
			reader := seedMembershipRole(t, ctx, client, scope.ID, "end_user", [][2]string{{"ticket", "read"}})
			extra := seedMembershipRole(t, ctx, client, scope.ID, "extra_editor", [][2]string{{"ticket", "update"}})
			client.UserTenantMembership.Create().SetUserID(account.ID).SetTenantID(scope.ID).
				SetRoleID(reader.ID).SetSource(usertenantmembership.SourceHome).SaveX(ctx)
			if mutation != "grant" {
				require.NoError(t, extra.Update().AddUserIDs(account.ID).Exec(ctx))
			}
			before, source, err := ResolvePermissions(ctx, client, account.ID, scope.ID)
			require.NoError(t, err)
			require.Equal(t, PermissionSourceMembership, source)
			require.Contains(t, before, "ticket:read")
			if mutation == "grant" {
				require.NotContains(t, before, "ticket:update")
			} else {
				require.Contains(t, before, "ticket:update")
			}

			// Mutate from the role side, as another API instance can. Neither the
			// account nor primary membership role changes and no cache is evicted.
			switch mutation {
			case "disable":
				require.NoError(t, extra.Update().SetIsActive(false).Exec(ctx))
			case "detach":
				require.NoError(t, extra.Update().RemoveUserIDs(account.ID).Exec(ctx))
			case "grant":
				require.NoError(t, extra.Update().AddUserIDs(account.ID).Exec(ctx))
			}
			after, source, err := ResolvePermissions(ctx, client, account.ID, scope.ID)
			require.NoError(t, err)
			require.Equal(t, PermissionSourceMembership, source)
			want := []string{"ticket:read"}
			if mutation == "grant" {
				want = append(want, "ticket:update")
			}
			require.Equal(t, want, after)
		})
	}
}
