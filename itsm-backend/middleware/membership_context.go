package middleware

import (
	"context"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"

	"itsm-backend/common/tenantctx"
	"itsm-backend/ent"
	"itsm-backend/ent/user"
	"itsm-backend/ent/usertenantmembership"
	"itsm-backend/pkg/tenantmode"
)

// Membership context keys.
//
// C 方案（membership-tenant-identity）：
//   - user_id 是全局登录账号；
//   - tenant_id 是当前请求作用域；
//   - membership_id 是该账号在作用域租户内的租户内身份。
//
// 业务写入必须使用 membership_id 做租户内引用；客户端不得提交 membership_id 作为授权依据。
const (
	CtxKeyMembershipID          = "membership_id"
	CtxKeyMembershipAccountKind = "membership_account_kind"
	CtxKeyMembershipSource      = "membership_source"
	CtxKeyMembershipEntity      = "membership_entity"
)

// ResolveActiveMembership 返回 (userID, tenantID) 对应的 active、未删除、未过期 membership。
// 无 membership 属于业务态（返回 nil, nil）；基础设施错误返回 error，由调用方决定 fail-closed。
func ResolveActiveMembership(
	ctx context.Context,
	client *ent.Client,
	userID, tenantID int,
) (*ent.UserTenantMembership, error) {
	if client == nil || userID <= 0 || tenantID <= 0 {
		return nil, nil
	}
	ctx = tenantctx.WithTenantID(ctx, tenantID)
	membership, err := client.UserTenantMembership.Query().
		Where(
			usertenantmembership.UserID(userID),
			usertenantmembership.TenantID(tenantID),
			usertenantmembership.DeletedAtIsNil(),
			usertenantmembership.StatusEQ(usertenantmembership.StatusActive),
			MembershipNotExpired(),
		).
		Only(ctx)
	if err != nil {
		if ent.IsNotFound(err) {
			return nil, nil
		}
		zap.S().Warnw("ResolveActiveMembership: membership lookup failed",
			"user_id", userID, "tenant_id", tenantID, "error", err)
		return nil, err
	}
	return membership, nil
}

// allowMembershiplessRequest keeps only the explicit migration/governance
// exceptions. A JWT role alone is never evidence of tenant membership.
func allowMembershiplessRequest(ctx context.Context, client *ent.Client, account *ent.User, tenantID int, source string) (bool, error) {
	if client == nil || account == nil || !account.Active || tenantID <= 0 {
		return false, nil
	}
	if IsPlatformRole(string(account.Role)) &&
		(account.TenantID == tenantID || (account.Role == user.RoleSuperAdmin && source == "switch")) {
		home, err := client.Tenant.Get(tenantctx.WithTenantID(ctx, account.TenantID), account.TenantID)
		if err != nil {
			return false, err
		}
		// Platform switching is an explicitly audited governance path. Verify
		// the stored home tenant, not merely the name of a token/attached role.
		if (tenantmode.IsInternalTenantType(string(home.Type)) || strings.EqualFold(home.Code, "default")) &&
			home.Status == "active" && (home.ExpiresAt.IsZero() || home.ExpiresAt.After(time.Now())) {
			return true, nil
		}
	}
	if !AuthzStaticFallbackEnabled() || account.TenantID != tenantID {
		return false, nil
	}
	// Even the opt-in legacy window must not undo a suspension, expiry or
	// revocation. Only a home account with no membership history can use it.
	exists, err := client.UserTenantMembership.Query().
		Where(usertenantmembership.UserIDEQ(account.ID), usertenantmembership.TenantIDEQ(tenantID)).
		Exist(tenantctx.WithTenantID(ctx, tenantID))
	return !exists && err == nil, err
}

// SetMembershipContext 把已解析的 membership 写入 gin 上下文。
func SetMembershipContext(c *gin.Context, membership *ent.UserTenantMembership) {
	if c == nil || membership == nil {
		return
	}
	c.Set(CtxKeyMembershipID, membership.ID)
	c.Set(CtxKeyMembershipAccountKind, string(membership.AccountKind))
	c.Set(CtxKeyMembershipSource, string(membership.Source))
	c.Set(CtxKeyMembershipEntity, membership)
}

// GetMembershipID 返回当前请求作用域的 membership id；0 表示未解析到。
func GetMembershipID(c *gin.Context) int {
	if c == nil {
		return 0
	}
	return c.GetInt(CtxKeyMembershipID)
}

// GetMembershipAccountKind 返回 customer/provider/platform；空串表示未解析到。
func GetMembershipAccountKind(c *gin.Context) string {
	if c == nil {
		return ""
	}
	return c.GetString(CtxKeyMembershipAccountKind)
}

// GetMembershipSource 返回 home/allocation/invite/platform/migration。
func GetMembershipSource(c *gin.Context) string {
	if c == nil {
		return ""
	}
	return c.GetString(CtxKeyMembershipSource)
}

// IsProviderMembership 判断当前请求是否由 provider membership 发起。
func IsProviderMembership(c *gin.Context) bool {
	return GetMembershipAccountKind(c) == string(usertenantmembership.AccountKindProvider)
}
