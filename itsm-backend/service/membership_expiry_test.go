package service

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"
	"time"

	_ "github.com/mattn/go-sqlite3"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap/zaptest"

	"itsm-backend/ent"
	"itsm-backend/ent/auditlog"
	"itsm-backend/ent/enttest"
	"itsm-backend/ent/usertenantmembership"
)

// =============================================================================
// IP-P2-9：membership 到期回收（惰性判定见 middleware；本文件覆盖后台回收任务）
// =============================================================================

func membershipExpiryTestClient(t *testing.T) *ent.Client {
	t.Helper()
	dsn := fmt.Sprintf("file:membership_expiry_%d?mode=memory&cache=shared&_fk=1", time.Now().UnixNano())
	client := enttest.Open(t, "sqlite3", dsn)
	t.Cleanup(func() { _ = client.Close() })
	return client
}

// TestMembershipExpiry_RecyclesExpiredAllocationMembership：到期分配型 membership →
// 软删 + allocation 级联解除 + membership.revoke 审计（trigger=membership.expired）+ 幂等重跑。
func TestMembershipExpiry_RecyclesExpiredAllocationMembership(t *testing.T) {
	ctx := context.Background()
	client := membershipExpiryTestClient(t)

	provider := msTenant(t, ctx, client, "到期回收服务商", "exp-prov", "msp_provider", 0)
	customer := msTenant(t, ctx, client, "到期回收客户", "exp-cust", "saas_customer", provider.ID)
	staff := msUser(t, ctx, client, "exp-staff", provider.ID, "technician", "provider_agent")

	alloc, err := client.MSPAllocation.Create().
		SetMspUserID(staff.ID).
		SetCustomerTenantID(customer.ID).
		SetProviderTenantID(provider.ID).
		SetRole("primary").
		Save(ctx)
	require.NoError(t, err)

	past := time.Now().Add(-2 * time.Hour)
	_, err = client.UserTenantMembership.Create().
		SetUserID(staff.ID).
		SetTenantID(customer.ID).
		SetAccountKind(usertenantmembership.AccountKindProvider).
		SetSource(usertenantmembership.SourceAllocation).
		SetAllocationID(alloc.ID).
		SetStatus(usertenantmembership.StatusActive).
		SetExpiresAt(past).
		Save(ctx)
	require.NoError(t, err)

	svc := NewMembershipExpiryService(client, nil, zaptest.NewLogger(t).Sugar())
	res, err := svc.RecycleExpired(ctx, 0)
	require.NoError(t, err)
	assert.Equal(t, 1, res.Scanned)
	assert.Equal(t, 1, res.Recycled)
	assert.Equal(t, 0, res.Failed)
	assert.Equal(t, 0, res.SkippedHome)

	// membership 已软删（存活行 0；回收即失效）。
	liveCount, err := client.UserTenantMembership.Query().
		Where(
			usertenantmembership.UserIDEQ(staff.ID),
			usertenantmembership.TenantIDEQ(customer.ID),
			usertenantmembership.DeletedAtIsNil(),
		).Count(ctx)
	require.NoError(t, err)
	assert.Zero(t, liveCount, "到期回收后不得残留存活 membership")

	// allocation 级联解除。
	got, err := client.MSPAllocation.Get(ctx, alloc.ID)
	require.NoError(t, err)
	require.NotNil(t, got.DeassignedAt, "到期回收必须级联解除分配（避免 allocation 仍可切换）")

	// 审计：membership.revoke ×1，trigger=membership.expired。
	audits, err := client.AuditLog.Query().Where(auditlog.ActionEQ(MembershipAuditActionRevoke)).All(ctx)
	require.NoError(t, err)
	require.Len(t, audits, 1)
	require.NotNil(t, audits[0].RequestBody)
	var payload map[string]any
	require.NoError(t, json.Unmarshal([]byte(*audits[0].RequestBody), &payload))
	assert.Equal(t, "membership.expired", payload["trigger"])
	assert.Equal(t, "expired", payload["outcome"])

	// 幂等重跑：无存活行可回收。
	res2, err := svc.RecycleExpired(ctx, 0)
	require.NoError(t, err)
	assert.Equal(t, 0, res2.Recycled)
	assert.Zero(t, res2.Scanned)

	// 重跑不得重复审计。
	audits2, err := client.AuditLog.Query().Where(auditlog.ActionEQ(MembershipAuditActionRevoke)).Count(ctx)
	require.NoError(t, err)
	assert.Equal(t, 1, audits2, "幂等重跑不得重复写审计")
}

// TestMembershipExpiry_SkipsFutureAndHome：未到期不动；home 到期行跳过（账号级语义）并可见化计数。
func TestMembershipExpiry_SkipsFutureAndHome(t *testing.T) {
	ctx := context.Background()
	client := membershipExpiryTestClient(t)

	provider := msTenant(t, ctx, client, "到期跳过服务商", "skip-prov", "msp_provider", 0)
	customer := msTenant(t, ctx, client, "到期跳过客户", "skip-cust", "saas_customer", provider.ID)
	staff := msUser(t, ctx, client, "skip-staff", provider.ID, "technician", "provider_agent")
	homeUser := msUser(t, ctx, client, "skip-home", customer.ID, "end_user", "")

	// A. allocation membership，未到期 → 不动。
	alloc, err := client.MSPAllocation.Create().
		SetMspUserID(staff.ID).SetCustomerTenantID(customer.ID).SetProviderTenantID(provider.ID).
		Save(ctx)
	require.NoError(t, err)
	_, err = client.UserTenantMembership.Create().
		SetUserID(staff.ID).SetTenantID(customer.ID).
		SetAccountKind(usertenantmembership.AccountKindProvider).
		SetSource(usertenantmembership.SourceAllocation).
		SetAllocationID(alloc.ID).
		SetStatus(usertenantmembership.StatusActive).
		SetExpiresAt(time.Now().Add(24 * time.Hour)).
		Save(ctx)
	require.NoError(t, err)

	// B. home membership 带过期时间（配置异常）→ 跳过且不回收。
	_, err = client.UserTenantMembership.Create().
		SetUserID(homeUser.ID).SetTenantID(customer.ID).
		SetAccountKind(usertenantmembership.AccountKindCustomer).
		SetSource(usertenantmembership.SourceHome).
		SetStatus(usertenantmembership.StatusActive).
		SetIsDefault(true).
		SetExpiresAt(time.Now().Add(-time.Minute)).
		Save(ctx)
	require.NoError(t, err)

	svc := NewMembershipExpiryService(client, nil, zaptest.NewLogger(t).Sugar())
	res, err := svc.RecycleExpired(ctx, 0)
	require.NoError(t, err)
	assert.Equal(t, 1, res.Scanned, "仅 home 到期行进入扫描")
	assert.Equal(t, 0, res.Recycled)
	assert.Equal(t, 1, res.SkippedHome)
	assert.Equal(t, 0, res.Failed)

	// home 行保持存活（不自动回收）。
	liveHome, err := client.UserTenantMembership.Query().
		Where(usertenantmembership.UserIDEQ(homeUser.ID), usertenantmembership.DeletedAtIsNil()).
		Only(ctx)
	require.NoError(t, err)
	assert.Nil(t, liveHome.DeletedAt)

	// 未到期 allocation membership 仍在。
	liveStaff, err := client.UserTenantMembership.Query().
		Where(usertenantmembership.UserIDEQ(staff.ID), usertenantmembership.DeletedAtIsNil()).
		Only(ctx)
	require.NoError(t, err)
	assert.NotNil(t, liveStaff.ExpiresAt)
}

// TestMembershipExpiryRecycleEnvParsing 锁定开关/间隔解析口径（默认开；显式 false/0/no/off 关闭）。
func TestMembershipExpiryRecycleEnvParsing(t *testing.T) {
	for _, raw := range []string{"", "true", "TRUE", "1", "yes", "on", "anything"} {
		assert.True(t, MembershipExpiryRecycleEnabledFromEnv(raw), "raw=%q 应默认开", raw)
	}
	for _, raw := range []string{"false", "FALSE", "0", "no", "off", " off "} {
		assert.False(t, MembershipExpiryRecycleEnabledFromEnv(raw), "raw=%q 应关闭", raw)
	}
	def := 10 * time.Minute
	assert.Equal(t, def, MembershipExpiryRecycleIntervalFromEnv("", def))
	assert.Equal(t, def, MembershipExpiryRecycleIntervalFromEnv("0", def))
	assert.Equal(t, def, MembershipExpiryRecycleIntervalFromEnv("-3", def))
	assert.Equal(t, def, MembershipExpiryRecycleIntervalFromEnv("abc", def))
	assert.Equal(t, 5*time.Minute, MembershipExpiryRecycleIntervalFromEnv("5", def))
}
