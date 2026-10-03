package mspguard

import (
	"context"
	"fmt"
	"sync/atomic"
	"testing"
	"time"

	_ "github.com/mattn/go-sqlite3"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"itsm-backend/ent"
	"itsm-backend/ent/enttest"
	"itsm-backend/ent/tenant"
	"itsm-backend/ent/user"
)

var testDBCounter int64

type accessFixture struct {
	client        *ent.Client
	provider      *ent.Tenant
	otherProvider *ent.Tenant
	customer      *ent.Tenant
	mspUser       *ent.User
	nonMSPUser    *ent.User
}

func newAccessFixture(t *testing.T) *accessFixture {
	t.Helper()
	dsn := fmt.Sprintf("file:mg_test_%d?mode=memory&cache=shared&_fk=1", atomic.AddInt64(&testDBCounter, 1))
	client := enttest.Open(t, "sqlite3", dsn)
	t.Cleanup(func() { client.Close() })
	ctx := context.Background()

	tenant := func(name, code, typ, status string, providerID int) *ent.Tenant {
		tb := client.Tenant.Create().SetName(name).SetCode(code).SetType(tenant.Type(typ)).SetStatus(status)
		if providerID > 0 {
			tb = tb.SetMspProviderID(providerID)
		}
		tn, err := tb.Save(ctx)
		require.NoError(t, err)
		return tn
	}
	user := func(name, mspRole string, tenantID int) *ent.User {
		ub := client.User.Create().
			SetUsername(name).
			SetEmail(name + "@example.com").
			SetName(name).
			SetPasswordHash("hash").
			SetTenantID(tenantID)
		if mspRole != "" {
			ub = ub.SetMspRole(user.MspRole(mspRole))
		}
		u, err := ub.Save(ctx)
		require.NoError(t, err)
		return u
	}

	provider := tenant("Provider", "prov", "msp_provider", "active", 0)
	otherProvider := tenant("Other Provider", "prov-other", "msp_provider", "active", 0)
	customer := tenant("Customer A", "cust-a", "msp_customer", "active", provider.ID)

	return &accessFixture{
		client:        client,
		provider:      provider,
		otherProvider: otherProvider,
		customer:      customer,
		mspUser:       user("msp-user", "provider_agent", provider.ID),
		nonMSPUser:    user("plain-user", "", provider.ID),
	}
}

func (f *accessFixture) allocate(t *testing.T, mspUserID, customerTenantID int) *ent.MSPAllocation {
	t.Helper()
	// provider 维度收尾：allocation.provider_tenant_id 必须 == customer.msp_provider_id；
	// 未显式设置客户 provider 的用例回退到 fixture 的 provider。
	providerID := f.provider.ID
	if cust, cErr := f.client.Tenant.Get(context.Background(), customerTenantID); cErr == nil && cust.MspProviderID > 0 {
		providerID = cust.MspProviderID
	}
	alloc, err := f.client.MSPAllocation.Create().
		SetMspUserID(mspUserID).
		SetCustomerTenantID(customerTenantID).
		SetProviderTenantID(providerID).
		SetRole("primary").
		Save(context.Background())
	require.NoError(t, err)
	return alloc
}

func requireAccessCode(t *testing.T, err error, wantCode string) {
	t.Helper()
	ae, ok := AsAccessError(err)
	require.True(t, ok, "want *AccessError, got %v", err)
	assert.Equal(t, wantCode, ae.Code)
}

// TestCanAccessCustomer 锁定 IP-P0-2 唯一授权入口的判定链（R9/R10、R2）：
// 分配有效 + 客户归属本 provider + 客户 active 才能放行；其余一律稳定拒绝。
func TestCanAccessCustomer(t *testing.T) {
	f := newAccessFixture(t)
	ctx := context.Background()
	checker := New(f.client)
	f.allocate(t, f.mspUser.ID, f.customer.ID)

	t.Run("active allocation with provider-owned customer allows", func(t *testing.T) {
		assert.NoError(t, checker.CanAccessCustomer(ctx, f.mspUser.ID, f.customer.ID))
	})

	t.Run("unallocated existing customer is denied", func(t *testing.T) {
		other := mkTenant(t, f, "Unallocated", "cust-unalloc", "msp_customer", "active", f.provider.ID)
		requireAccessCode(t, checker.CanAccessCustomer(ctx, f.mspUser.ID, other.ID), CodeMSPAllocationRequired)
	})

	t.Run("nonexistent customer is not found", func(t *testing.T) {
		requireAccessCode(t, checker.CanAccessCustomer(ctx, f.mspUser.ID, 999999), CodeCustomerTenantNotFound)
	})

	t.Run("deassigned allocation is denied", func(t *testing.T) {
		revoked := mkTenant(t, f, "Revoked", "cust-revoked", "msp_customer", "active", f.provider.ID)
		alloc := f.allocate(t, f.mspUser.ID, revoked.ID)
		_, err := f.client.MSPAllocation.UpdateOneID(alloc.ID).SetDeassignedAt(time.Now()).Save(ctx)
		require.NoError(t, err)
		requireAccessCode(t, checker.CanAccessCustomer(ctx, f.mspUser.ID, revoked.ID), CodeMSPAllocationRequired)
	})

	t.Run("cross-provider allocation is denied (R2)", func(t *testing.T) {
		foreign := mkTenant(t, f, "Foreign", "cust-foreign", "msp_customer", "active", f.otherProvider.ID)
		f.allocate(t, f.mspUser.ID, foreign.ID)
		requireAccessCode(t, checker.CanAccessCustomer(ctx, f.mspUser.ID, foreign.ID), CodeCustomerTenantNotFound)
	})

	t.Run("inactive customer is denied", func(t *testing.T) {
		suspended := mkTenant(t, f, "Suspended", "cust-susp", "msp_customer", "suspended", f.provider.ID)
		f.allocate(t, f.mspUser.ID, suspended.ID)
		requireAccessCode(t, checker.CanAccessCustomer(ctx, f.mspUser.ID, suspended.ID), CodeCustomerInactive)
	})

	t.Run("expired customer is denied", func(t *testing.T) {
		expired, err := f.client.Tenant.Create().
			SetName("Expired").SetCode("cust-exp").SetType("msp_customer").SetStatus("active").
			SetMspProviderID(f.provider.ID).
			SetExpiresAt(time.Now().Add(-time.Hour)).
			Save(ctx)
		require.NoError(t, err)
		f.allocate(t, f.mspUser.ID, expired.ID)
		requireAccessCode(t, checker.CanAccessCustomer(ctx, f.mspUser.ID, expired.ID), CodeCustomerInactive)
	})

	t.Run("non-MSP actor is denied", func(t *testing.T) {
		requireAccessCode(t, checker.CanAccessCustomer(ctx, f.nonMSPUser.ID, f.customer.ID), CodeMSPAllocationRequired)
	})
}

// TestListAccessibleCustomerIDs 确保聚合查询复用的集合同样是 fail-closed 的。
func TestListAccessibleCustomerIDs(t *testing.T) {
	f := newAccessFixture(t)
	ctx := context.Background()
	checker := New(f.client)

	f.allocate(t, f.mspUser.ID, f.customer.ID)
	suspended := mkTenant(t, f, "Suspended", "cust-susp2", "msp_customer", "suspended", f.provider.ID)
	f.allocate(t, f.mspUser.ID, suspended.ID)
	foreign := mkTenant(t, f, "Foreign", "cust-foreign2", "msp_customer", "active", f.otherProvider.ID)
	f.allocate(t, f.mspUser.ID, foreign.ID)
	revoked := mkTenant(t, f, "Revoked", "cust-revoked2", "msp_customer", "active", f.provider.ID)
	alloc := f.allocate(t, f.mspUser.ID, revoked.ID)
	_, err := f.client.MSPAllocation.UpdateOneID(alloc.ID).SetDeassignedAt(time.Now()).Save(ctx)
	require.NoError(t, err)

	ids, err := checker.ListAccessibleCustomerIDs(ctx, f.mspUser.ID)
	require.NoError(t, err)
	assert.Equal(t, []int{f.customer.ID}, ids)
}

func mkTenant(t *testing.T, f *accessFixture, name, code, typ, status string, providerID int) *ent.Tenant {
	t.Helper()
	tn, err := f.client.Tenant.Create().
		SetName(name).SetCode(code).SetType(tenant.Type(typ)).SetStatus(status).
		SetMspProviderID(providerID).
		Save(context.Background())
	require.NoError(t, err)
	return tn
}
