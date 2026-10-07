// msp_bootstrap_allocation.go：provider 首管默认分配（P0 首体验修复）。
//
// 背景：provider 首管默认不持有任何 Allocation → 登录后跨客户工作台空白。
// 本文件提供幂等补齐：provider 的管理员（is_bootstrap_admin=true，或角色为
// admin/super_admin）对其全部 active 客户持有 primary allocation；双向触发：
//
//   - 客户租户模板供给完成后（TenantProvisioningService.Provision）→ 补该客户；
//   - provider 首管创建后（TenantProvisioningService.CreateBootstrapAdmin）→ 补其存量客户。
//
// 分配规则与 service/msp_allocation_service.go 一致：provider/customer 类型与归属校验、
// active allocation 唯一（迁移 022 部分唯一索引），创建即写审计（source=system）。
package service

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"itsm-backend/ent"
	"itsm-backend/ent/mspallocation"
	"itsm-backend/ent/tenant"
	"itsm-backend/ent/user"
	"itsm-backend/middleware"
	"itsm-backend/pkg/tenantmode"

	"go.uber.org/zap"
)

// EnsureBootstrapAllocationsForCustomer 幂等确保指定客户租户的 provider 首管持有 primary allocation。
// apply=false 为 dry-run（只统计待创建数）。
func EnsureBootstrapAllocationsForCustomer(ctx context.Context, client *ent.Client, logger *zap.SugaredLogger, apply bool, customerTenantID int) (int, error) {
	if client == nil {
		return 0, fmt.Errorf("ent client is nil")
	}
	if customerTenantID <= 0 {
		return 0, fmt.Errorf("customer tenant id must be positive")
	}
	cust, err := client.Tenant.Query().Where(tenant.IDEQ(customerTenantID)).Only(ctx)
	if err != nil {
		if ent.IsNotFound(err) {
			return 0, nil
		}
		return 0, fmt.Errorf("load customer tenant %d: %w", customerTenantID, err)
	}
	providerID := resolveCustomerProviderID(cust)
	if providerID <= 0 {
		return 0, nil
	}
	return ensureBootstrapAllocationPair(ctx, client, logger, apply, providerID, cust)
}

// EnsureBootstrapAllocationsForProvider 幂等确保 provider 首管对其全部（含 legacy parent 归属）客户持有 primary allocation。
func EnsureBootstrapAllocationsForProvider(ctx context.Context, client *ent.Client, logger *zap.SugaredLogger, apply bool, providerTenantID int) (int, error) {
	if client == nil {
		return 0, fmt.Errorf("ent client is nil")
	}
	if providerTenantID <= 0 {
		return 0, fmt.Errorf("provider tenant id must be positive")
	}
	customers, err := client.Tenant.Query().Where(
		tenant.Or(
			tenant.MspProviderIDEQ(providerTenantID),
			tenant.And(
				tenant.TypeEQ(tenant.Type(tenantmode.TenantTypeLegacyCustomer)),
				tenant.ParentTenantIDEQ(providerTenantID),
			),
		),
	).All(ctx)
	if err != nil {
		return 0, fmt.Errorf("list customers of provider %d: %w", providerTenantID, err)
	}
	total := 0
	for _, cust := range customers {
		n, err := ensureBootstrapAllocationPair(ctx, client, logger, apply, providerTenantID, cust)
		if err != nil {
			return total, err
		}
		total += n
	}
	return total, nil
}

// resolveCustomerProviderID 解析客户租户归属（D2 兼容：legacy customer 回退 parent_tenant_id）。
func resolveCustomerProviderID(cust *ent.Tenant) int {
	if cust == nil {
		return 0
	}
	providerID := cust.MspProviderID
	if providerID <= 0 && string(cust.Type) == tenantmode.TenantTypeLegacyCustomer {
		providerID = cust.ParentTenantID
	}
	return providerID
}

func ensureBootstrapAllocationPair(ctx context.Context, client *ent.Client, logger *zap.SugaredLogger, apply bool, providerTenantID int, cust *ent.Tenant) (int, error) {
	if cust == nil || cust.ID <= 0 {
		return 0, nil
	}
	if !tenantmode.IsCustomerTenantType(string(cust.Type)) {
		return 0, nil
	}
	if cust.Status != "" && cust.Status != "active" {
		return 0, nil
	}
	if !cust.ExpiresAt.IsZero() && cust.ExpiresAt.Before(time.Now()) {
		return 0, nil
	}
	provider, err := client.Tenant.Query().Where(tenant.IDEQ(providerTenantID)).Only(ctx)
	if err != nil {
		if ent.IsNotFound(err) {
			return 0, nil
		}
		return 0, fmt.Errorf("load provider tenant %d: %w", providerTenantID, err)
	}
	if !tenantmode.IsMSPProviderTenantType(string(provider.Type)) {
		return 0, nil
	}
	if provider.Status != "" && provider.Status != "active" {
		return 0, nil
	}

	// provider 管理员 = bootstrap 首管，或 admin/super_admin 角色（覆盖 seeder 创建的存量 provider）。
	admins, err := client.User.Query().Where(
		user.TenantIDEQ(providerTenantID),
		user.ActiveEQ(true),
		user.Or(
			user.IsBootstrapAdminEQ(true),
			user.RoleIn(user.RoleSuperAdmin, user.RoleAdmin),
		),
	).All(ctx)
	if err != nil {
		return 0, fmt.Errorf("list bootstrap admins of provider %d: %w", providerTenantID, err)
	}

	created := 0
	for _, admin := range admins {
		exists, err := client.MSPAllocation.Query().Where(
			mspallocation.MspUserIDEQ(admin.ID),
			mspallocation.CustomerTenantIDEQ(cust.ID),
			mspallocation.DeassignedAtIsNil(),
		).Exist(ctx)
		if err != nil {
			return created, fmt.Errorf("check existing allocation user=%d customer=%d: %w", admin.ID, cust.ID, err)
		}
		if exists {
			continue
		}
		if !apply {
			created++
			continue
		}
		alloc, err := client.MSPAllocation.Create().
			SetMspUserID(admin.ID).
			SetCustomerTenantID(cust.ID).
			SetProviderTenantID(providerTenantID).
			SetRole("primary").
			SetAssignedAt(time.Now()).
			Save(ctx)
		if err != nil {
			if isUniqueViolation(err) {
				continue // 并发下已被其它路径创建，幂等跳过
			}
			return created, fmt.Errorf("create bootstrap allocation user=%d customer=%d: %w", admin.ID, cust.ID, err)
		}
		created++
		writeBootstrapAllocationAudit(ctx, client, logger, provider, admin, cust)
		// MSW-1：自动分配同样同步客户作用域 membership（source=allocation；失败仅告警）。
		if _, err := NewMembershipSyncService(client, logger).SyncAllocationMembership(ctx, alloc, MembershipAuditMeta{
			RowTenantID:  providerTenantID,
			ActorUserID:  admin.ID,
			ActorAccount: admin.Username,
			Source:       middleware.AuditSourceSystem,
			Trigger:      "msp.allocation_auto_assigned",
		}); err != nil && logger != nil {
			logger.Warnw("membership sync for bootstrap allocation failed",
				"error", err, "allocation_id", alloc.ID, "user_id", admin.ID, "customer_tenant_id", cust.ID)
		}
	}
	return created, nil
}

func writeBootstrapAllocationAudit(ctx context.Context, client *ent.Client, logger *zap.SugaredLogger, provider *ent.Tenant, admin *ent.User, cust *ent.Tenant) {
	if client == nil || provider == nil || admin == nil || cust == nil {
		return
	}
	payload, _ := json.Marshal(map[string]any{
		"source":             "bootstrap_allocation",
		"actor_user_id":      admin.ID,
		"actor_account":      admin.Username,
		"provider_tenant_id": provider.ID,
		"target_tenant_id":   cust.ID,
		"allocation_role":    "primary",
		"reason":             "provider bootstrap admin default allocation",
	})
	auditCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	if _, err := client.AuditLog.Create().
		SetCreatedAt(time.Now()).
		SetTenantID(provider.ID).
		SetUserID(admin.ID).
		SetActorAccount(admin.Username).
		SetTargetTenantID(cust.ID).
		SetSource(middleware.AuditSourceSystem).
		SetResource("msp_allocation").
		SetAction("msp.allocation_auto_assigned").
		SetPath("/internal/bootstrap/allocation").
		SetMethod("SYSTEM").
		SetStatusCode(200).
		SetRequestBody(string(payload)).
		Save(auditCtx); err != nil && logger != nil {
		logger.Warnw("bootstrap allocation audit write failed", "error", err, "user_id", admin.ID, "customer_id", cust.ID)
	}
}

func isUniqueViolation(err error) bool {
	if err == nil {
		return false
	}
	msg := err.Error()
	return strings.Contains(msg, "unique") || strings.Contains(msg, "duplicate") || strings.Contains(msg, "23505")
}
