package change

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"

	"itsm-backend/common/tenantctx"
	"itsm-backend/ent"
	"itsm-backend/ent/enttest"
	"itsm-backend/ent/usertenantmembership"
	"itsm-backend/handlers/common/datascope"
	"itsm-backend/middleware"

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

func createChangeMembershipMaintenanceFixture(t *testing.T, ctx context.Context, repo *EntRepository, tenantID, creatorID, creatorMembershipID, assigneeID int) *Change {
	t.Helper()
	created, err := repo.Create(ctx, &Change{
		Title: "historical change", Type: "normal", Status: "draft", Priority: "medium", ImpactScope: "medium", RiskLevel: "medium",
		CreatedBy: creatorID, CreatedByMembershipID: creatorMembershipID, TenantID: tenantID,
	})
	require.NoError(t, err)
	if assigneeID > 0 {
		created.AssigneeID = &assigneeID
		created, err = repo.Update(ctx, created)
		require.NoError(t, err)
	}
	return created
}

func TestChangeHandlerHistoricalAssigneeDoesNotBlockMaintenance(t *testing.T) {
	for _, invalidation := range []string{"revoked", "suspended", "expired", "account_disabled"} {
		t.Run(invalidation, func(t *testing.T) {
			ctx, repo, tenantID, creatorID, creatorMembershipID, assigneeID, assigneeMembershipID := newChangeMembershipFixture(t)
			ctx = datascope.WithDataScope(tenantctx.WithTenantID(ctx, tenantID), datascope.DataScopeAll)
			historical := createChangeMembershipMaintenanceFixture(t, ctx, repo, tenantID, creatorID, creatorMembershipID, assigneeID)
			unassigned := createChangeMembershipMaintenanceFixture(t, ctx, repo, tenantID, creatorID, creatorMembershipID, 0)
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
			svc := NewService(repo, repo.client, zap.NewNop().Sugar(), nil)
			handler := NewHandler(svc)
			gin.SetMode(gin.TestMode)
			router := gin.New()
			router.Use(func(c *gin.Context) {
				c.Set("user_id", creatorID)
				c.Set("role", "admin")
				c.Set(middleware.TenantContextKey, &middleware.TenantContext{TenantID: tenantID})
				c.Request = c.Request.WithContext(datascope.WithDataScope(tenantctx.WithTenantID(c.Request.Context(), tenantID), datascope.DataScopeAll))
				c.Next()
			})
			// Same production handlers and HTTP routes; only authentication is fixture-injected.
			router.PUT("/api/v1/changes/:id", handler.UpdateChange)
			router.POST("/api/v1/changes/:id/assign", handler.AssignChange)
			request := func(method, path, body string, wantStatus int) {
				t.Helper()
				req := httptest.NewRequest(method, path, strings.NewReader(body))
				req.Header.Set("Content-Type", "application/json")
				res := httptest.NewRecorder()
				router.ServeHTTP(res, req)
				require.Equal(t, wantStatus, res.Code, res.Body.String())
				var response struct {
					Code int `json:"code"`
					Data struct {
						ID int `json:"id"`
					} `json:"data"`
				}
				require.NoError(t, json.Unmarshal(res.Body.Bytes(), &response))
				if wantStatus == http.StatusOK {
					require.Zero(t, response.Code)
					require.Positive(t, response.Data.ID)
				} else {
					require.NotZero(t, response.Code)
				}
				require.NotContains(t, res.Body.String(), "passwordHash")
			}
			request(http.MethodPut, fmt.Sprintf("/api/v1/changes/%d", historical.ID), `{"title":"maintained by admin"}`, http.StatusOK)
			stored := repo.client.Change.GetX(ctx, historical.ID)
			require.Equal(t, "maintained by admin", stored.Title)
			require.Equal(t, assigneeID, stored.AssigneeID)
			require.Equal(t, assigneeMembershipID, stored.AssigneeMembershipID)

			request(http.MethodPost, fmt.Sprintf("/api/v1/changes/%d/assign", unassigned.ID), fmt.Sprintf(`{"assigneeId":%d}`, assigneeID), http.StatusBadRequest)
			stored = repo.client.Change.GetX(ctx, unassigned.ID)
			require.Zero(t, stored.AssigneeID)
			require.Zero(t, stored.AssigneeMembershipID)

			// Explicit account changes are validated and write the matching new pair.
			request(http.MethodPost, fmt.Sprintf("/api/v1/changes/%d/assign", historical.ID), fmt.Sprintf(`{"assigneeId":%d}`, creatorID), http.StatusOK)
			stored = repo.client.Change.GetX(ctx, historical.ID)
			require.Equal(t, creatorID, stored.AssigneeID)
			require.Equal(t, creatorMembershipID, stored.AssigneeMembershipID)
			request(http.MethodPost, fmt.Sprintf("/api/v1/changes/%d/assign", historical.ID), fmt.Sprintf(`{"assigneeId":%d}`, assigneeID), http.StatusBadRequest)
			stored = repo.client.Change.GetX(ctx, historical.ID)
			require.Equal(t, creatorID, stored.AssigneeID)
			require.Equal(t, creatorMembershipID, stored.AssigneeMembershipID)

			current, err := svc.GetChange(ctx, historical.ID, tenantID)
			require.NoError(t, err)
			zero := 0
			current.AssigneeID = &zero
			cleared, err := svc.UpdateChange(ctx, current, creatorID, "admin")
			require.NoError(t, err)
			require.Zero(t, *cleared.AssigneeID)
			require.Zero(t, cleared.AssigneeMembershipID)
		})
	}
}

func TestChangeUpdateKeepsHistoricalMembershipInsteadOfRebinding(t *testing.T) {
	ctx, repo, tenantID, creatorID, creatorMembershipID, assigneeID, oldMembershipID := newChangeMembershipFixture(t)
	created := createChangeMembershipMaintenanceFixture(t, ctx, repo, tenantID, creatorID, creatorMembershipID, assigneeID)
	repo.client.UserTenantMembership.UpdateOneID(oldMembershipID).SetDeletedAt(time.Now()).SaveX(ctx)
	newMembership := repo.client.UserTenantMembership.Create().SetUserID(assigneeID).SetTenantID(tenantID).
		SetAccountKind(usertenantmembership.AccountKindProvider).SetSource(usertenantmembership.SourceAllocation).SaveX(ctx)
	created.Title = "retain original identity"
	updated, err := repo.Update(ctx, created)
	require.NoError(t, err)
	require.Equal(t, oldMembershipID, updated.AssigneeMembershipID)

	updated.AssigneeMembershipID = newMembership.ID
	updated.Title = "tampered reference"
	_, err = repo.Update(ctx, updated)
	require.Error(t, err, "an explicit membership change must not be silently accepted")
	stored := repo.client.Change.GetX(ctx, created.ID)
	require.Equal(t, "retain original identity", stored.Title)
	require.Equal(t, assigneeID, stored.AssigneeID)
	require.Equal(t, oldMembershipID, stored.AssigneeMembershipID)

	// nil still means not supplied in this repository; explicit zero clears both.
	updated.AssigneeID = nil
	updated.AssigneeMembershipID = 0
	unchanged, err := repo.Update(ctx, updated)
	require.NoError(t, err)
	require.Equal(t, assigneeID, *unchanged.AssigneeID)
	require.Equal(t, oldMembershipID, unchanged.AssigneeMembershipID)
	zero := 0
	unchanged.AssigneeID = &zero
	cleared, err := repo.Update(ctx, unchanged)
	require.NoError(t, err)
	require.Zero(t, *cleared.AssigneeID)
	require.Zero(t, cleared.AssigneeMembershipID)
}

func TestChangeUpdateRejectsConcurrentIdentityChange(t *testing.T) {
	ctx, repo, tenantID, creatorID, creatorMembershipID, assigneeID, _ := newChangeMembershipFixture(t)
	created := createChangeMembershipMaintenanceFixture(t, ctx, repo, tenantID, creatorID, creatorMembershipID, assigneeID)
	injected := false
	repo.client.Change.Use(func(next ent.Mutator) ent.Mutator {
		return ent.MutateFunc(func(ctx context.Context, mutation ent.Mutation) (ent.Value, error) {
			if !injected {
				injected = true
				repo.client.Change.UpdateOneID(created.ID).SetAssigneeID(creatorID).SetAssigneeMembershipID(creatorMembershipID).SaveX(ctx)
			}
			return next.Mutate(ctx, mutation)
		})
	})
	created.Title = "stale write"
	_, err := repo.Update(ctx, created)
	require.Error(t, err)
	stored := repo.client.Change.GetX(ctx, created.ID)
	require.Equal(t, "historical change", stored.Title)
	require.Equal(t, creatorID, stored.AssigneeID)
	require.Equal(t, creatorMembershipID, stored.AssigneeMembershipID)
}
