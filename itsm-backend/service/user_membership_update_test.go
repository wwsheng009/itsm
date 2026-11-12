package service_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"itsm-backend/dto"
	"itsm-backend/ent"
	"itsm-backend/ent/enttest"
	"itsm-backend/ent/usertenantmembership"
	userhandler "itsm-backend/handlers/user"
	"itsm-backend/middleware"
	"itsm-backend/service"

	"github.com/gin-gonic/gin"
	_ "github.com/mattn/go-sqlite3"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

type membershipUpdateFixture struct {
	client      *ent.Client
	svc         *service.UserService
	account     *ent.User
	membership  *ent.UserTenantMembership
	admin, read *ent.Role
}

func newMembershipUpdateFixture(t *testing.T) membershipUpdateFixture {
	t.Helper()
	ctx := context.Background()
	client := enttest.Open(t, "sqlite3", fmt.Sprintf("file:update_membership_%d?mode=memory&cache=shared&_fk=1", time.Now().UnixNano()))
	t.Cleanup(func() { _ = client.Close() })
	middleware.InvalidateAllPermissionCaches()
	t.Cleanup(middleware.InvalidateAllPermissionCaches)
	tenant, err := client.Tenant.Create().SetName("Membership update").SetCode("membership-update").SetStatus("active").Save(ctx)
	require.NoError(t, err)
	admin, err := client.Role.Create().SetCode("admin").SetName("Admin").SetTenantID(tenant.ID).Save(ctx)
	require.NoError(t, err)
	read, err := client.Role.Create().SetCode("end_user").SetName("Reader").SetTenantID(tenant.ID).Save(ctx)
	require.NoError(t, err)
	perm, err := client.Permission.Create().SetCode("user:write").SetName("Write users").
		SetResource("user").SetAction("write").SetTenantID(tenant.ID).Save(ctx)
	require.NoError(t, err)
	_, err = client.RolePermission.Create().SetRoleID(admin.ID).SetPermissionID(perm.ID).SetTenantID(tenant.ID).Save(ctx)
	require.NoError(t, err)
	account, err := client.User.Create().SetUsername("membership-target").SetName("Target").
		SetEmail("membership-target@example.test").SetPasswordHash("not-a-password").
		SetTenantID(tenant.ID).SetRole("admin").SetActive(true).AddRoleIDs(admin.ID).Save(ctx)
	require.NoError(t, err)
	membership, err := client.UserTenantMembership.Create().SetUserID(account.ID).SetTenantID(tenant.ID).
		SetRoleID(admin.ID).SetSource(usertenantmembership.SourceInvite).Save(ctx)
	require.NoError(t, err)
	return membershipUpdateFixture{client, service.NewUserService(client, zap.NewNop().Sugar()), account, membership, admin, read}
}

func TestUserMembershipDowngradeThroughHTTP(t *testing.T) {
	for _, payload := range []string{`{"role":"end_user"}`, `{"role":"end_user","roleIds":[]}`} {
		t.Run(payload, func(t *testing.T) {
			f := newMembershipUpdateFixture(t)
			ctx := context.Background()
			actor, err := f.client.User.Create().SetUsername("membership-admin").SetName("Operator").
				SetEmail("membership-admin@example.test").SetPasswordHash("not-used").
				SetTenantID(f.account.TenantID).SetRole("admin").SetActive(true).Save(ctx)
			require.NoError(t, err)
			_, err = f.client.UserTenantMembership.Create().SetUserID(actor.ID).SetTenantID(actor.TenantID).
				SetRoleID(f.admin.ID).SetSource(usertenantmembership.SourceHome).Save(ctx)
			require.NoError(t, err)
			before, _, err := middleware.ResolvePermissions(ctx, f.client, f.account.ID, f.account.TenantID)
			require.NoError(t, err)
			require.Contains(t, before, "user:write")

			gin.SetMode(gin.TestMode)
			r := gin.New()
			h := userhandler.NewHandler(f.svc, zap.NewNop().Sugar())
			r.PUT("/api/v1/users/:id", middleware.AuthMiddleware("membership-user-update"),
				middleware.RBACMiddleware(f.client), middleware.RequirePermission("user", "write"), h.UpdateUser)
			token, err := middleware.GenerateAccessToken(actor.ID, actor.Username, "admin", actor.TenantID, "membership-user-update", time.Hour)
			require.NoError(t, err)
			req := httptest.NewRequest(http.MethodPut, fmt.Sprintf("/api/v1/users/%d", f.account.ID), strings.NewReader(payload))
			req.Header.Set("Authorization", "Bearer "+token)
			req.Header.Set("Content-Type", "application/json")
			w := httptest.NewRecorder()
			r.ServeHTTP(w, req)
			require.Equal(t, http.StatusOK, w.Code, w.Body.String())
			var response struct {
				Code int `json:"code"`
				Data struct {
					Role string `json:"role"`
				} `json:"data"`
			}
			require.NoError(t, json.Unmarshal(w.Body.Bytes(), &response))
			require.Zero(t, response.Code)
			require.Equal(t, "end_user", response.Data.Role)
			require.NotContains(t, w.Body.String(), "not-a-password")
			require.NotContains(t, w.Body.String(), "passwordHash")
			m, err := f.client.UserTenantMembership.Get(ctx, f.membership.ID)
			require.NoError(t, err)
			require.NotNil(t, m.RoleID)
			require.Equal(t, f.read.ID, *m.RoleID)
			// This is the same resolver used after login/refresh. Primed admin
			// permissions and the mirrored admin M2M edge must both disappear.
			after, _, err := middleware.ResolvePermissions(ctx, f.client, f.account.ID, f.account.TenantID)
			require.NoError(t, err)
			require.NotContains(t, after, "user:write")
			roles, err := f.client.User.QueryRoles(f.account).All(ctx)
			require.NoError(t, err)
			for _, role := range roles {
				require.NotEqual(t, f.admin.ID, role.ID)
			}
		})
	}
}

func TestUserMembershipUpdateRollsBackOnInvalidRole(t *testing.T) {
	f := newMembershipUpdateFixture(t)
	ctx := context.Background()
	foreign, err := f.client.Tenant.Create().SetName("Foreign").SetCode("membership-foreign").Save(ctx)
	require.NoError(t, err)
	role, err := f.client.Role.Create().SetName("Foreign admin").SetCode("admin").SetTenantID(foreign.ID).Save(ctx)
	require.NoError(t, err)
	_, err = f.svc.UpdateUser(ctx, f.account.ID, &dto.UpdateUserRequest{
		Role: "end_user", Name: "Must roll back", RoleIDs: []int{role.ID},
	}, f.account.TenantID)
	require.Error(t, err)
	account, err := f.client.User.Get(ctx, f.account.ID)
	require.NoError(t, err)
	require.Equal(t, f.account.Name, account.Name)
	require.Equal(t, f.account.Role, account.Role)
	m, err := f.client.UserTenantMembership.Get(ctx, f.membership.ID)
	require.NoError(t, err)
	require.Equal(t, f.membership.RoleID, m.RoleID)
	roles, err := account.QueryRoles().IDs(ctx)
	require.NoError(t, err)
	require.Equal(t, []int{f.admin.ID}, roles)
	_, err = f.svc.UpdateUser(ctx, f.account.ID, &dto.UpdateUserRequest{Name: "Foreign edit"}, foreign.ID)
	require.Error(t, err)
}

func TestUserMembershipUpdateDoesNotReinstateRevokedIdentity(t *testing.T) {
	f := newMembershipUpdateFixture(t)
	ctx := context.Background()
	require.NoError(t, f.membership.Update().SetDeletedAt(time.Now()).Exec(ctx))
	_, err := f.svc.UpdateUser(ctx, f.account.ID, &dto.UpdateUserRequest{Role: "end_user"}, f.account.TenantID)
	require.NoError(t, err)
	count, err := f.client.UserTenantMembership.Query().Where(
		usertenantmembership.UserIDEQ(f.account.ID), usertenantmembership.TenantIDEQ(f.account.TenantID),
		usertenantmembership.DeletedAtIsNil()).Count(ctx)
	require.NoError(t, err)
	require.Zero(t, count)
	m, err := f.client.UserTenantMembership.Get(ctx, f.membership.ID)
	require.NoError(t, err)
	require.Equal(t, f.membership.RoleID, m.RoleID, "historical attribution is immutable")
}

func TestUserMembershipUpdateRollsBackWhenMembershipWriteFails(t *testing.T) {
	f := newMembershipUpdateFixture(t)
	ctx := context.Background()
	f.client.UserTenantMembership.Use(func(next ent.Mutator) ent.Mutator {
		return ent.MutateFunc(func(ctx context.Context, mutation ent.Mutation) (ent.Value, error) {
			if mutation.Op().Is(ent.OpUpdate | ent.OpUpdateOne) {
				return nil, errors.New("injected membership storage failure")
			}
			return next.Mutate(ctx, mutation)
		})
	})
	_, err := f.svc.UpdateUser(ctx, f.account.ID,
		&dto.UpdateUserRequest{Role: "end_user", RoleIDs: []int{}}, f.account.TenantID)
	require.ErrorContains(t, err, "injected membership storage failure")
	account, err := f.client.User.Get(ctx, f.account.ID)
	require.NoError(t, err)
	require.Equal(t, f.account.Role, account.Role)
	roles, err := account.QueryRoles().IDs(ctx)
	require.NoError(t, err)
	require.Equal(t, []int{f.admin.ID}, roles)
	m, err := f.client.UserTenantMembership.Get(ctx, f.membership.ID)
	require.NoError(t, err)
	require.Equal(t, f.membership.RoleID, m.RoleID)
}

func TestUserMembershipUpdatePreservesOtherTenantRoles(t *testing.T) {
	f := newMembershipUpdateFixture(t)
	ctx := context.Background()
	foreign, err := f.client.Tenant.Create().SetName("Other scope").SetCode("other-scope").Save(ctx)
	require.NoError(t, err)
	role, err := f.client.Role.Create().SetName("Other operator").SetCode("technician").SetTenantID(foreign.ID).Save(ctx)
	require.NoError(t, err)
	require.NoError(t, f.account.Update().AddRoleIDs(role.ID).Exec(ctx))
	_, err = f.svc.UpdateUser(ctx, f.account.ID,
		&dto.UpdateUserRequest{Role: "end_user", RoleIDs: []int{}}, f.account.TenantID)
	require.NoError(t, err)
	roles, err := f.client.User.QueryRoles(f.account).IDs(ctx)
	require.NoError(t, err)
	require.Equal(t, []int{role.ID}, roles)
}
