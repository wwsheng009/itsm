package service

import (
	"context"
	"testing"
	"time"

	_ "github.com/mattn/go-sqlite3"
	"github.com/stretchr/testify/require"

	"itsm-backend/ent/enttest"
	"itsm-backend/ent/usertenantmembership"
)

// IP-P1-1：membership 表 3 个部分唯一索引的 DB 级强约束回归
// （uq_membership_live / uq_membership_default / uq_customer_single_scope）。
func TestUserTenantMembership_Constraints(t *testing.T) {
	client := enttest.Open(t, "sqlite3", testDSN())
	defer client.Close()
	ctx := context.Background()

	mkTenant := func(code string) int {
		return client.Tenant.Create().
			SetName("T-" + code).SetCode(code).SetDomain(code + ".test").SetStatus("active").
			SaveX(ctx).ID
	}
	tenantA := mkTenant("msp-a")
	tenantB := mkTenant("msp-b")

	mkUser := func(name string, tenantID int) int {
		return client.User.Create().
			SetUsername(name).SetEmail(name + "@example.com").SetName(name).
			SetPasswordHash("x").SetTenantID(tenantID).
			SaveX(ctx).ID
	}

	create := func(userID, tenantID int, kind usertenantmembership.AccountKind, source usertenantmembership.Source, isDefault bool) error {
		_, err := client.UserTenantMembership.Create().
			SetUserID(userID).SetTenantID(tenantID).
			SetAccountKind(kind).SetSource(source).SetIsDefault(isDefault).
			Save(ctx)
		return err
	}

	t.Run("同租户存活成员唯一（uq_membership_live）", func(t *testing.T) {
		user := mkUser("live-dup", tenantA)
		require.NoError(t, create(user, tenantA, usertenantmembership.AccountKindProvider, usertenantmembership.SourceHome, true))
		require.Error(t, create(user, tenantA, usertenantmembership.AccountKindProvider, usertenantmembership.SourceAllocation, false))
	})

	t.Run("默认作用域唯一（uq_membership_default）", func(t *testing.T) {
		user := mkUser("default-dup", tenantA)
		require.NoError(t, create(user, tenantA, usertenantmembership.AccountKindProvider, usertenantmembership.SourceHome, true))
		require.Error(t, create(user, tenantB, usertenantmembership.AccountKindProvider, usertenantmembership.SourceAllocation, true))
	})

	t.Run("客户账号单作用域（uq_customer_single_scope）", func(t *testing.T) {
		user := mkUser("customer-single", tenantA)
		require.NoError(t, create(user, tenantA, usertenantmembership.AccountKindCustomer, usertenantmembership.SourceHome, true))
		require.Error(t, create(user, tenantB, usertenantmembership.AccountKindCustomer, usertenantmembership.SourceMigration, false))
	})

	t.Run("软删后可重新加入（部分索引放行 deleted_at 行）", func(t *testing.T) {
		user := mkUser("soft-delete", tenantA)
		require.NoError(t, create(user, tenantA, usertenantmembership.AccountKindProvider, usertenantmembership.SourceHome, true))
		client.UserTenantMembership.Update().
			Where(
				usertenantmembership.UserID(user),
				usertenantmembership.TenantID(tenantA),
			).
			SetDeletedAt(time.Now()).
			SaveX(ctx)
		require.NoError(t, create(user, tenantA, usertenantmembership.AccountKindProvider, usertenantmembership.SourceMigration, false))
	})

	t.Run("服务方可为多客户建立非默认作用域（provider N 作用域）", func(t *testing.T) {
		user := mkUser("provider-multi", tenantA)
		require.NoError(t, create(user, tenantA, usertenantmembership.AccountKindProvider, usertenantmembership.SourceHome, true))
		require.NoError(t, create(user, tenantB, usertenantmembership.AccountKindProvider, usertenantmembership.SourceAllocation, false))
	})
}
