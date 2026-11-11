package middleware

import (
	"context"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"itsm-backend/ent/enttest"
	"itsm-backend/ent/usertenantmembership"

	_ "github.com/mattn/go-sqlite3"
)

func TestMembershipContext_ResolveAndExpose(t *testing.T) {
	gin.SetMode(gin.TestMode)
	ctx := context.Background()
	client := enttest.Open(t, "sqlite3", "file:membership-context?mode=memory&cache=shared&_fk=1")
	t.Cleanup(func() { _ = client.Close() })

	tenant := client.Tenant.Create().SetCode("ctx-customer").SetName("ctx-customer").SaveX(ctx)
	user := client.User.Create().
		SetUsername("ctx-provider").SetEmail("ctx-provider@example.com").SetName("ctx-provider").
		SetPasswordHash("hash").SetTenantID(tenant.ID).SetActive(true).SaveX(ctx)
	membership := client.UserTenantMembership.Create().
		SetUserID(user.ID).SetTenantID(tenant.ID).
		SetAccountKind(usertenantmembership.AccountKindProvider).
		SetSource(usertenantmembership.SourceAllocation).
		SetStatus(usertenantmembership.StatusActive).SetIsDefault(false).SaveX(ctx)

	resolved, err := ResolveActiveMembership(ctx, client, user.ID, tenant.ID)
	require.NoError(t, err)
	require.NotNil(t, resolved)
	assert.Equal(t, membership.ID, resolved.ID)

	c, _ := gin.CreateTestContext(nil)
	SetMembershipContext(c, resolved)
	assert.Equal(t, membership.ID, GetMembershipID(c))
	assert.Equal(t, string(usertenantmembership.AccountKindProvider), GetMembershipAccountKind(c))
	assert.True(t, IsProviderMembership(c))
}

func TestMembershipContext_RevokedMembershipIsNotResolved(t *testing.T) {
	ctx := context.Background()
	client := enttest.Open(t, "sqlite3", "file:membership-context-revoked?mode=memory&cache=shared&_fk=1")
	t.Cleanup(func() { _ = client.Close() })

	tenant := client.Tenant.Create().SetCode("ctx-revoked").SetName("ctx-revoked").SaveX(ctx)
	user := client.User.Create().
		SetUsername("ctx-revoked-user").SetEmail("ctx-revoked@example.com").SetName("ctx-revoked").
		SetPasswordHash("hash").SetTenantID(tenant.ID).SetActive(true).SaveX(ctx)
	client.UserTenantMembership.Create().
		SetUserID(user.ID).SetTenantID(tenant.ID).
		SetAccountKind(usertenantmembership.AccountKindProvider).
		SetSource(usertenantmembership.SourceAllocation).
		SetStatus(usertenantmembership.StatusActive).SetIsDefault(false).
		SetDeletedAt(time.Now()).SaveX(ctx)

	resolved, err := ResolveActiveMembership(ctx, client, user.ID, tenant.ID)
	require.NoError(t, err)
	assert.Nil(t, resolved)
}
