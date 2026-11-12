package service

import (
	"context"
	"errors"
	"strings"
	"testing"

	"go.uber.org/zap/zaptest"

	"itsm-backend/common/tenantctx"
	"itsm-backend/ent/auditlog"
	"itsm-backend/ent/enttest"
	"itsm-backend/ent/usertenantmembership"
)

func TestMSPAllocationRoleMapping_DefaultOverrideReset(t *testing.T) {
	client := enttest.Open(t, "sqlite3", testDSN())
	defer client.Close()
	ctx := context.Background()
	logger := zaptest.NewLogger(t).Sugar()

	provider := msTenant(t, ctx, client, "Provider", "arm-prov", "msp_provider", 0)
	customer := msTenant(t, ctx, client, "Customer", "arm-cust", "msp_customer", provider.ID)
	techRole := msRoleWithPerm(t, ctx, client, customer.ID, "msp_tech", "ticket", "read")
	managerRole := msRoleWithPerm(t, ctx, client, customer.ID, "msp_manager", "ticket", "read")
	user := msUser(t, ctx, client, "arm-tech", provider.ID, "end_user", "provider_agent")

	svc := NewMSPAllocationService(client, logger)
	meta := MSPAllocationRoleMappingAuditMeta{
		ActorUserID:  99,
		ActorAccount: "tester",
		Source:       "test",
		Path:         "/api/v1/msp/allocation-role-mapping",
		Method:       "PUT",
	}

	view, err := svc.GetAllocationRoleMapping(ctx, provider.ID)
	if err != nil {
		t.Fatalf("get default mapping: %v", err)
	}
	if view.Overridden {
		t.Fatal("expected default mapping before any override")
	}
	if got := view.Mapping["primary"]; got != "msp_tech" {
		t.Fatalf("default primary = %q, want msp_tech", got)
	}

	view, err = svc.UpdateAllocationRoleMapping(ctx, provider.ID, map[string]string{
		"primary":    "msp_manager",
		"backup":     "msp_tech",
		"specialist": "msp_specialist",
	}, meta)
	if err != nil {
		t.Fatalf("update mapping: %v", err)
	}
	if !view.Overridden || view.Mapping["primary"] != "msp_manager" {
		t.Fatalf("unexpected updated view: %+v", view)
	}
	updateLogs, err := client.AuditLog.Query().
		Where(
			auditlog.ResourceEQ("msp_allocation_role_mapping"),
			auditlog.ActionEQ("msp.allocation_role_mapping.update"),
		).
		All(ctx)
	if err != nil {
		t.Fatalf("query update audit: %v", err)
	}
	if len(updateLogs) != 1 || updateLogs[0].RequestBody == nil ||
		!strings.Contains(*updateLogs[0].RequestBody, "msp_manager") {
		t.Fatalf("update audit missing before/after mapping: %+v", updateLogs)
	}

	alloc, err := svc.Create(tenantctx.WithTenantID(ctx, provider.ID), user.ID, customer.ID, "primary")
	if err != nil {
		t.Fatalf("create allocation: %v", err)
	}
	if alloc == nil || alloc.ID <= 0 {
		t.Fatal("allocation was not created")
	}
	mb := msLiveMembership(t, ctx, client, user.ID, customer.ID)
	if mb.RoleID == nil || *mb.RoleID != managerRole.ID {
		t.Fatalf("membership role_id = %v, want msp_manager role %d", mb.RoleID, managerRole.ID)
	}
	if mb.MspRole == nil || *mb.MspRole != "msp_manager" {
		t.Fatalf("membership msp_role = %v, want msp_manager", mb.MspRole)
	}

	meta.Method = "DELETE"
	view, err = svc.ResetAllocationRoleMapping(ctx, provider.ID, meta)
	if err != nil {
		t.Fatalf("reset mapping: %v", err)
	}
	if view.Overridden || view.Mapping["primary"] != "msp_tech" {
		t.Fatalf("unexpected reset view: %+v", view)
	}
	resetLogs, err := client.AuditLog.Query().
		Where(
			auditlog.ResourceEQ("msp_allocation_role_mapping"),
			auditlog.ActionEQ("msp.allocation_role_mapping.reset"),
		).
		All(ctx)
	if err != nil {
		t.Fatalf("query reset audit: %v", err)
	}
	if len(resetLogs) != 1 {
		t.Fatalf("reset audit count = %d, want 1", len(resetLogs))
	}

	// 新分配恢复默认（primary/backup → msp_tech）。
	user2 := msUser(t, ctx, client, "arm-tech-2", provider.ID, "end_user", "provider_agent")
	if _, err := svc.Create(tenantctx.WithTenantID(ctx, provider.ID), user2.ID, customer.ID, "backup"); err != nil {
		t.Fatalf("create allocation after reset: %v", err)
	}
	mb2 := msLiveMembership(t, ctx, client, user2.ID, customer.ID)
	if mb2.RoleID == nil || *mb2.RoleID != techRole.ID {
		t.Fatalf("membership role_id after reset = %v, want msp_tech role %d", mb2.RoleID, techRole.ID)
	}
}

func TestMSPAllocationRoleMapping_InvalidAndMissingTargetFallback(t *testing.T) {
	client := enttest.Open(t, "sqlite3", testDSN())
	defer client.Close()
	ctx := context.Background()
	logger := zaptest.NewLogger(t).Sugar()

	provider := msTenant(t, ctx, client, "Provider", "arm-prov2", "msp_provider", 0)
	customer := msTenant(t, ctx, client, "Customer", "arm-cust2", "msp_customer", provider.ID)
	techRole := msRoleWithPerm(t, ctx, client, customer.ID, "msp_tech", "ticket", "read")
	user := msUser(t, ctx, client, "arm-fallback", provider.ID, "end_user", "provider_agent")
	svc := NewMSPAllocationService(client, logger)
	meta := MSPAllocationRoleMappingAuditMeta{ActorUserID: 99, ActorAccount: "tester"}

	if _, err := svc.UpdateAllocationRoleMapping(ctx, provider.ID, map[string]string{
		"primary":    "admin",
		"backup":     "msp_tech",
		"specialist": "msp_specialist",
	}, meta); err == nil {
		t.Fatal("expected invalid target role to be rejected")
	}
	if _, err := svc.UpdateAllocationRoleMapping(ctx, provider.ID, map[string]string{
		"primary": "msp_tech",
	}, meta); err == nil {
		t.Fatal("expected missing keys to be rejected")
	}
	if _, err := svc.GetAllocationRoleMapping(ctx, customer.ID); err == nil {
		t.Fatal("expected customer tenant to be rejected")
	}

	// 合法配置但客户租户缺 msp_manager：回退 msp_tech，而不是写 NULL。
	if _, err := svc.UpdateAllocationRoleMapping(ctx, provider.ID, map[string]string{
		"primary":    "msp_manager",
		"backup":     "msp_tech",
		"specialist": "msp_specialist",
	}, meta); err != nil {
		t.Fatalf("update mapping: %v", err)
	}
	if _, err := svc.Create(tenantctx.WithTenantID(ctx, provider.ID), user.ID, customer.ID, "primary"); err != nil {
		t.Fatalf("create allocation: %v", err)
	}
	mb := msLiveMembership(t, ctx, client, user.ID, customer.ID)
	if mb.RoleID == nil || *mb.RoleID != techRole.ID {
		t.Fatalf("membership role_id = %v, want fallback msp_tech role %d", mb.RoleID, techRole.ID)
	}
}

func TestMSPAllocationRoleMapping_CorruptConfigFailsClosed(t *testing.T) {
	client := enttest.Open(t, "sqlite3", testDSN())
	defer client.Close()
	ctx := context.Background()
	logger := zaptest.NewLogger(t).Sugar()

	provider := msTenant(t, ctx, client, "Provider", "arm-prov3", "msp_provider", 0)
	customer := msTenant(t, ctx, client, "Customer", "arm-cust3", "msp_customer", provider.ID)
	msRoleWithPerm(t, ctx, client, customer.ID, "msp_tech", "ticket", "read")
	user := msUser(t, ctx, client, "arm-corrupt", provider.ID, "end_user", "provider_agent")

	if _, err := client.SystemConfig.Create().
		SetTenantID(provider.ID).
		SetKey(MSPAllocationRoleMappingKey).
		SetValue("{bad json").
		SetValueType("json").
		SetCategory(MSPAllocationRoleMappingCategory).
		Save(ctx); err != nil {
		t.Fatalf("create corrupt config: %v", err)
	}

	svc := NewMSPAllocationService(client, logger)
	view, err := svc.GetAllocationRoleMapping(ctx, provider.ID)
	if err != nil {
		t.Fatalf("get corrupt mapping: %v", err)
	}
	if !view.Corrupt || view.Warning == "" {
		t.Fatalf("corrupt mapping not reported: %+v", view)
	}

	alloc, err := client.MSPAllocation.Create().
		SetMspUserID(user.ID).
		SetCustomerTenantID(customer.ID).
		SetProviderTenantID(provider.ID).
		SetRole("primary").
		Save(ctx)
	if err != nil {
		t.Fatalf("create allocation: %v", err)
	}
	_, err = NewMembershipSyncService(client, logger).SyncAllocationMembership(ctx, alloc, MembershipAuditMeta{})
	if !errors.Is(err, ErrMSPAllocationRoleMappingCorrupt) {
		t.Fatalf("sync error = %v, want ErrMSPAllocationRoleMappingCorrupt", err)
	}
	count, err := client.UserTenantMembership.Query().
		Where(
			usertenantmembership.UserID(user.ID),
			usertenantmembership.TenantID(customer.ID),
			usertenantmembership.DeletedAtIsNil(),
		).
		Count(ctx)
	if err != nil {
		t.Fatalf("count membership: %v", err)
	}
	if count != 0 {
		t.Fatalf("corrupt config must not create membership, got %d", count)
	}
}

func TestMSPAllocationRoleMapping_InactiveTargetFallsBack(t *testing.T) {
	client := enttest.Open(t, "sqlite3", testDSN())
	defer client.Close()
	ctx := context.Background()
	logger := zaptest.NewLogger(t).Sugar()

	provider := msTenant(t, ctx, client, "Provider", "arm-prov4", "msp_provider", 0)
	customer := msTenant(t, ctx, client, "Customer", "arm-cust4", "msp_customer", provider.ID)
	techRole := msRoleWithPerm(t, ctx, client, customer.ID, "msp_tech", "ticket", "read")
	managerRole := msRoleWithPerm(t, ctx, client, customer.ID, "msp_manager", "ticket", "read")
	if _, err := managerRole.Update().SetIsActive(false).Save(ctx); err != nil {
		t.Fatalf("deactivate manager role: %v", err)
	}
	user := msUser(t, ctx, client, "arm-inactive", provider.ID, "end_user", "provider_agent")
	svc := NewMSPAllocationService(client, logger)
	if _, err := svc.UpdateAllocationRoleMapping(ctx, provider.ID, map[string]string{
		"primary":    "msp_manager",
		"backup":     "msp_tech",
		"specialist": "msp_specialist",
	}, MSPAllocationRoleMappingAuditMeta{ActorAccount: "tester"}); err != nil {
		t.Fatalf("update mapping: %v", err)
	}
	if _, err := svc.Create(tenantctx.WithTenantID(ctx, provider.ID), user.ID, customer.ID, "primary"); err != nil {
		t.Fatalf("create allocation: %v", err)
	}
	mb := msLiveMembership(t, ctx, client, user.ID, customer.ID)
	if mb.RoleID == nil || *mb.RoleID != techRole.ID {
		t.Fatalf("membership role_id = %v, want active fallback msp_tech role %d", mb.RoleID, techRole.ID)
	}
}
