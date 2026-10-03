package service

import (
	"context"
	"testing"

	_ "github.com/mattn/go-sqlite3"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap/zaptest"

	"itsm-backend/dto"
	"itsm-backend/ent"
	"itsm-backend/ent/auditlog"
	"itsm-backend/ent/enttest"
	"itsm-backend/ent/tenant"
	"itsm-backend/ent/user"
)

// TestProvisioningService_Channels 锁定 IP-P0-5 通道矩阵与错误码：
// platform / msp / tenant 三通道；未分配客户 403 MSP_ALLOCATION_REQUIRED；
// 平台角色 422 ROLE_NOT_GRANTABLE；mspRole 白名单；灰度开关关闭即拒绝。
func TestProvisioningService_Channels(t *testing.T) {
	client := enttest.Open(t, "sqlite3", testDSN())
	defer client.Close()
	ctx := context.Background()
	logger := zaptest.NewLogger(t).Sugar()
	users := NewUserService(client, logger)
	prov := NewUserProvisioningService(client, users, logger)
	prov.SetChannelsEnabled(true)

	mkTenant := func(name, code, typ string, providerID int) *ent.Tenant {
		t.Helper()
		tb := client.Tenant.Create().SetName(name).SetCode(code).SetType(tenant.Type(typ)).SetStatus("active")
		if providerID > 0 {
			tb = tb.SetMspProviderID(providerID)
		}
		tn, err := tb.Save(ctx)
		require.NoError(t, err)
		return tn
	}
	mkUser := func(name string, tenantID int, mspRole string) *ent.User {
		t.Helper()
		tb := client.User.Create().
			SetUsername(name).SetEmail(name + "@example.com").SetName(name).
			SetPasswordHash("hash").SetActive(true).SetTenantID(tenantID)
		if mspRole != "" {
			tb = tb.SetMspRole(user.MspRole(mspRole))
		}
		u, err := tb.Save(ctx)
		require.NoError(t, err)
		return u
	}
	req := func(username string) *dto.CreateUserRequest {
		return &dto.CreateUserRequest{
			Username: username, Email: username + "@corp.example.com",
			Name: username, Password: "Str0ng-P@ssw0rd!",
		}
	}

	platform := mkTenant("Platform", "platform-1", "internal", 0)
	provider := mkTenant("Provider", "provider-1", "msp_provider", 0)
	customer := mkTenant("Customer", "customer-1", "msp_customer", provider.ID)
	otherCustomer := mkTenant("Customer2", "customer-2", "msp_customer", provider.ID)

	platformActor := ProvisionActor{UserID: 1, HomeTenantID: platform.ID, Role: "super_admin", Username: "root"}

	t.Run("platform channel creates first admin in provider tenant", func(t *testing.T) {
		r := req("prov-admin")
		r.Role = "admin"
		created, err := prov.ProvisionUser(ctx, platformActor, provider.ID, r)
		require.NoError(t, err)
		assert.Equal(t, provider.ID, created.TenantID)

		// IP-P0-10：建号落 user.provision 审计（source=platform_selected，target=目标租户）。
		logs, err := client.AuditLog.Query().Where(auditlog.ActionEQ("user.provision")).All(ctx)
		require.NoError(t, err)
		require.Len(t, logs, 1)
		assert.Equal(t, "platform_selected", logs[0].Source)
		assert.Equal(t, platform.ID, logs[0].TenantID)
		assert.Equal(t, provider.ID, logs[0].TargetTenantID)
		assert.Equal(t, "root", logs[0].ActorAccount)
	})

	t.Run("tenant channel admin creates end_user in own tenant", func(t *testing.T) {
		customerAdmin := mkUser("cust-admin", customer.ID, "")
		actor := ProvisionActor{UserID: customerAdmin.ID, HomeTenantID: customer.ID, Role: "admin", Username: "cust-admin"}
		r := req("cust-agent")
		r.Role = "agent"
		created, err := prov.ProvisionUser(ctx, actor, customer.ID, r)
		require.NoError(t, err)
		assert.Equal(t, customer.ID, created.TenantID)
	})

	t.Run("tenant channel cannot grant platform role", func(t *testing.T) {
		customerAdmin := mkUser("cust-admin-2", customer.ID, "")
		actor := ProvisionActor{UserID: customerAdmin.ID, HomeTenantID: customer.ID, Role: "admin", Username: "cust-admin-2"}
		r := req("evil-root")
		r.Role = "super_admin"
		_, err := prov.ProvisionUser(ctx, actor, customer.ID, r)
		pe, ok := AsProvisionError(err)
		require.True(t, ok)
		assert.Equal(t, ProvisionCodeRoleNotGrantable, pe.Code)
		assert.Equal(t, 422, pe.Status)
	})

	t.Run("tenant channel cannot set mspRole on customer tenant", func(t *testing.T) {
		customerAdmin := mkUser("cust-admin-3", customer.ID, "")
		actor := ProvisionActor{UserID: customerAdmin.ID, HomeTenantID: customer.ID, Role: "admin", Username: "cust-admin-3"}
		r := req("cust-msp-role")
		r.MSPRole = "provider_admin"
		_, err := prov.ProvisionUser(ctx, actor, customer.ID, r)
		pe, ok := AsProvisionError(err)
		require.True(t, ok)
		assert.Equal(t, ProvisionCodeMSPRoleNotAllowed, pe.Code)
	})

	t.Run("msp channel requires active allocation", func(t *testing.T) {
		mspUser := mkUser("msp-admin", provider.ID, "provider_admin")
		actor := ProvisionActor{
			UserID: mspUser.ID, HomeTenantID: provider.ID,
			Role: "msp_manager", MSPRole: "provider_admin", Username: "msp-admin",
		}
		_, err := prov.ProvisionUser(ctx, actor, customer.ID, req("cust-user-1"))
		pe, ok := AsProvisionError(err)
		require.True(t, ok, "err=%v", err)
		assert.Equal(t, "MSP_ALLOCATION_REQUIRED", pe.Code)
		assert.Equal(t, 403, pe.Status)
	})

	t.Run("msp channel creates user for allocated customer", func(t *testing.T) {
		mspAdmin := mkUser("msp-admin-2", provider.ID, "provider_admin")
		_, err := client.MSPAllocation.Create().
			SetMspUserID(mspAdmin.ID).
			SetCustomerTenantID(customer.ID).
			SetProviderTenantID(provider.ID).
			SetRole("provider_agent").
			Save(ctx)
		require.NoError(t, err)
		actor := ProvisionActor{
			UserID: mspAdmin.ID, HomeTenantID: provider.ID,
			Role: "msp_manager", MSPRole: "provider_admin", Username: "msp-admin-2",
		}
		created, err := prov.ProvisionUser(ctx, actor, customer.ID, req("cust-user-2"))
		require.NoError(t, err)
		assert.Equal(t, customer.ID, created.TenantID)

		// 未分配的另一客户 → 拒绝
		_, err = prov.ProvisionUser(ctx, actor, otherCustomer.ID, req("cust-user-3"))
		pe, ok := AsProvisionError(err)
		require.True(t, ok, "err=%v", err)
		assert.Equal(t, "MSP_ALLOCATION_REQUIRED", pe.Code)
	})

	t.Run("non-platform cross tenant forbidden", func(t *testing.T) {
		otherUser := mkUser("outsider", otherCustomer.ID, "")
		actor := ProvisionActor{UserID: otherUser.ID, HomeTenantID: otherCustomer.ID, Role: "end_user", Username: "outsider"}
		_, err := prov.ProvisionUser(ctx, actor, customer.ID, req("cust-user-x"))
		pe, ok := AsProvisionError(err)
		require.True(t, ok)
		assert.Equal(t, ProvisionCodeCrossTenantForbidden, pe.Code)
	})

	t.Run("channels disabled gate", func(t *testing.T) {
		prov.SetChannelsEnabled(false)
		defer prov.SetChannelsEnabled(true)
		_, err := prov.ProvisionUser(ctx, platformActor, provider.ID, req("prov-admin-2"))
		pe, ok := AsProvisionError(err)
		require.True(t, ok)
		assert.Equal(t, ProvisionCodeChannelsDisabled, pe.Code)
		assert.Equal(t, 404, pe.Status)
	})
}
