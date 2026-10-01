package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"testing"
	"time"

	"itsm-backend/ent"
	"itsm-backend/ent/auditlog"
	"itsm-backend/ent/enttest"
	"itsm-backend/ent/invitation"
	"itsm-backend/ent/usertenantmembership"

	_ "github.com/mattn/go-sqlite3"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap/zaptest"
)

// =============================================================================
// IP-P1-4 邀请生命周期测试：创建（token 哈希/白名单/重发失效）→ 接受（建号 +
// membership，一次性）→ 撤销/过期 → 绑定已有账号；审计 user.invite / user.invite_accept。
// =============================================================================

type invitationFixture struct {
	client  *ent.Client
	ctx     context.Context
	tenantA *ent.Tenant
	tenantB *ent.Tenant
	role    *ent.Role
	actor   InvitationActor
}

func newInvitationFixture(t *testing.T) *invitationFixture {
	t.Helper()
	ctx := context.Background()
	dsn := fmt.Sprintf("file:invitation_%d?mode=memory&cache=shared&_fk=1", time.Now().UnixNano())
	client := enttest.Open(t, "sqlite3", dsn)
	t.Cleanup(func() { _ = client.Close() })

	tenantA, err := client.Tenant.Create().SetName("Inv A").SetCode("inv-a").SetStatus("active").SetType("msp_provider").Save(ctx)
	require.NoError(t, err)
	tenantB, err := client.Tenant.Create().SetName("Inv B").SetCode("inv-b").SetStatus("active").SetType("msp_customer").Save(ctx)
	require.NoError(t, err)
	inviter, err := client.User.Create().
		SetUsername("inviter").SetEmail("inviter@example.com").SetName("Inviter").
		SetPasswordHash("x").SetActive(true).SetTenantID(tenantA.ID).SetRole("super_admin").Save(ctx)
	require.NoError(t, err)
	roleAgent, err := client.Role.Create().SetName("Agent").SetCode("agent").SetTenantID(tenantA.ID).Save(ctx)
	require.NoError(t, err)

	return &invitationFixture{
		client: client, ctx: ctx, tenantA: tenantA, tenantB: tenantB, role: roleAgent,
		actor: InvitationActor{UserID: inviter.ID, HomeTenantID: tenantA.ID, Role: "super_admin", Username: "root"},
	}
}

func (f *invitationFixture) service(t *testing.T) *InvitationService {
	t.Helper()
	return NewInvitationService(f.client, NewUserService(f.client, zaptest.NewLogger(t).Sugar()), zaptest.NewLogger(t).Sugar())
}

func requireInvitationCode(t *testing.T, err error, code string) {
	t.Helper()
	require.Error(t, err)
	ie, ok := AsInvitationError(err)
	require.True(t, ok, "期望 InvitationError，实际: %v", err)
	assert.Equal(t, code, ie.Code)
}

func TestInvitationService_CreateAndAcceptNewUser(t *testing.T) {
	f := newInvitationFixture(t)
	svc := f.service(t)

	result, err := svc.Create(f.ctx, f.actor, &CreateInvitationRequest{
		TenantID: f.tenantA.ID, Email: "New.User@Example.com", RoleID: f.role.ID,
	})
	require.NoError(t, err)
	require.NotNil(t, result.Invitation)
	assert.Equal(t, invitation.StatusPending, result.Invitation.Status)
	assert.Equal(t, 32, len(result.Token), "128-bit token hex")
	assert.Contains(t, result.InviteURL, result.Token)
	assert.False(t, result.EmailSent, "SMTP 未配置 → emailSent=false + inviteUrl")
	sum := sha256.Sum256([]byte(result.Token))
	assert.Equal(t, hex.EncodeToString(sum[:]), result.Invitation.TokenHash, "原始 token 不落库")
	assert.Equal(t, "new.user@example.com", result.Invitation.Email, "邮箱小写规范化")

	accepted, err := svc.Accept(f.ctx, result.Token, &AcceptInvitationRequest{Password: "Str0ng!Pass2026", Name: "New User"})
	require.NoError(t, err)
	require.NotNil(t, accepted.User)
	assert.Equal(t, f.tenantA.ID, accepted.User.TenantID)
	assert.Equal(t, "new.user@example.com", accepted.User.Email)
	require.NotNil(t, accepted.Membership)
	assert.Equal(t, usertenantmembership.SourceInvite, accepted.Membership.Source)
	require.NotNil(t, accepted.Membership.RoleID)
	assert.Equal(t, f.role.ID, *accepted.Membership.RoleID)
	assert.True(t, accepted.Membership.IsDefault)

	_, err = svc.Accept(f.ctx, result.Token, &AcceptInvitationRequest{Password: "Str0ng!Pass2026"})
	requireInvitationCode(t, err, InvitationCodeAlreadyAccepted)

	info, err := svc.Inspect(f.ctx, result.Token)
	require.NoError(t, err)
	assert.Equal(t, "accepted", info.Status)
	assert.Equal(t, "n***@example.com", info.EmailMasked)
	assert.Equal(t, f.role.Code, info.RoleCode)

	inviteAudits, err := f.client.AuditLog.Query().Where(auditlog.ActionEQ("user.invite")).Count(f.ctx)
	require.NoError(t, err)
	assert.GreaterOrEqual(t, inviteAudits, 1)
	acceptAudits, err := f.client.AuditLog.Query().Where(auditlog.ActionEQ("user.invite_accept")).Count(f.ctx)
	require.NoError(t, err)
	assert.GreaterOrEqual(t, acceptAudits, 1)
}

func TestInvitationService_RoleWhitelistAndMSPRoleGuard(t *testing.T) {
	f := newInvitationFixture(t)
	svc := f.service(t)

	superRole, err := f.client.Role.Create().SetName("Super").SetCode("super_admin").SetTenantID(f.tenantA.ID).Save(f.ctx)
	require.NoError(t, err)
	_, err = svc.Create(f.ctx, f.actor, &CreateInvitationRequest{
		TenantID: f.tenantA.ID, Email: "super@example.com", RoleID: superRole.ID,
	})
	requireInvitationCode(t, err, InvitationCodeRoleNotGrantable)

	adminRole, err := f.client.Role.Create().SetName("Admin").SetCode("admin").SetTenantID(f.tenantA.ID).Save(f.ctx)
	require.NoError(t, err)
	lowActor := InvitationActor{UserID: f.actor.UserID, HomeTenantID: f.tenantA.ID, Role: "end_user", Username: "low"}
	_, err = svc.Create(f.ctx, lowActor, &CreateInvitationRequest{
		TenantID: f.tenantA.ID, Email: "admin@example.com", RoleID: adminRole.ID,
	})
	requireInvitationCode(t, err, InvitationCodeRoleNotGrantable)

	// msp_role 白名单：customer 租户通道不允许写 msp_role。
	customerRole, err := f.client.Role.Create().SetName("Agent B").SetCode("agent").SetTenantID(f.tenantB.ID).Save(f.ctx)
	require.NoError(t, err)
	customerAdmin := InvitationActor{UserID: f.actor.UserID, HomeTenantID: f.tenantB.ID, Role: "admin", Username: "admin-b"}
	_, err = svc.Create(f.ctx, customerAdmin, &CreateInvitationRequest{
		TenantID: f.tenantB.ID, Email: "agent@example.com", RoleID: customerRole.ID, MSPRole: "provider_admin",
	})
	requireInvitationCode(t, err, InvitationCodeMSPRoleNotAllowed)
}

func TestInvitationService_ExpiredRevokedAndResend(t *testing.T) {
	f := newInvitationFixture(t)
	svc := f.service(t)

	expired, err := svc.Create(f.ctx, f.actor, &CreateInvitationRequest{TenantID: f.tenantA.ID, Email: "expired@example.com", RoleID: f.role.ID})
	require.NoError(t, err)
	require.NoError(t, f.client.Invitation.UpdateOneID(expired.Invitation.ID).SetExpiresAt(time.Now().Add(-time.Minute)).Exec(f.ctx))
	_, err = svc.Accept(f.ctx, expired.Token, &AcceptInvitationRequest{Password: "Str0ng!Pass2026"})
	requireInvitationCode(t, err, InvitationCodeExpired)

	revokable, err := svc.Create(f.ctx, f.actor, &CreateInvitationRequest{TenantID: f.tenantA.ID, Email: "revoke@example.com", RoleID: f.role.ID})
	require.NoError(t, err)
	revoked, err := svc.Revoke(f.ctx, f.actor, revokable.Invitation.ID)
	require.NoError(t, err)
	assert.Equal(t, invitation.StatusRevoked, revoked.Status)
	_, err = svc.Accept(f.ctx, revokable.Token, &AcceptInvitationRequest{Password: "Str0ng!Pass2026"})
	requireInvitationCode(t, err, InvitationCodeRevoked)

	first, err := svc.Create(f.ctx, f.actor, &CreateInvitationRequest{TenantID: f.tenantA.ID, Email: "resend@example.com", RoleID: f.role.ID})
	require.NoError(t, err)
	second, err := svc.Create(f.ctx, f.actor, &CreateInvitationRequest{TenantID: f.tenantA.ID, Email: "resend@example.com", RoleID: f.role.ID})
	require.NoError(t, err)
	old, err := f.client.Invitation.Get(f.ctx, first.Invitation.ID)
	require.NoError(t, err)
	assert.Equal(t, invitation.StatusRevoked, old.Status, "重发旧邀请须失效")
	pendingCount, err := f.client.Invitation.Query().
		Where(invitation.EmailEQ("resend@example.com"), invitation.StatusEQ(invitation.StatusPending)).
		Count(f.ctx)
	require.NoError(t, err)
	assert.Equal(t, 1, pendingCount)
	_, err = svc.Accept(f.ctx, first.Token, &AcceptInvitationRequest{Password: "Str0ng!Pass2026"})
	requireInvitationCode(t, err, InvitationCodeRevoked)
	_, err = svc.Accept(f.ctx, second.Token, &AcceptInvitationRequest{Password: "Str0ng!Pass2026"})
	require.NoError(t, err)
}

func TestInvitationService_BindExistingUser(t *testing.T) {
	f := newInvitationFixture(t)
	svc := f.service(t)

	existing, err := f.client.User.Create().
		SetUsername("bound-user").SetEmail("bound@example.com").SetName("Bound").
		SetPasswordHash("x").SetActive(true).SetTenantID(f.tenantA.ID).SetRole("end_user").Save(f.ctx)
	require.NoError(t, err)

	bound, err := svc.Create(f.ctx, f.actor, &CreateInvitationRequest{
		TenantID: f.tenantA.ID, Email: "bound@example.com", RoleID: f.role.ID, TargetUserID: existing.ID,
	})
	require.NoError(t, err)
	res, err := svc.Accept(f.ctx, bound.Token, &AcceptInvitationRequest{})
	require.NoError(t, err)
	assert.Equal(t, existing.ID, res.User.ID, "绑定路径不得新建账号")
	assert.Equal(t, existing.ID, res.Membership.UserID)

	_, err = svc.Create(f.ctx, f.actor, &CreateInvitationRequest{
		TenantID: f.tenantA.ID, Email: "other@example.com", RoleID: f.role.ID, TargetUserID: existing.ID,
	})
	requireInvitationCode(t, err, InvitationCodeEmailMismatch)
}

func TestInvitationService_CrossTenantForbidden(t *testing.T) {
	f := newInvitationFixture(t)
	svc := f.service(t)
	customerRole, err := f.client.Role.Create().SetName("Agent B").SetCode("agent").SetTenantID(f.tenantB.ID).Save(f.ctx)
	require.NoError(t, err)

	// actor 家租户 A（非平台、非 provider_admin 分配）→ 邀请到 B 被拒。
	_, err = svc.Create(f.ctx, f.actor, &CreateInvitationRequest{
		TenantID: f.tenantB.ID, Email: "cross@example.com", RoleID: customerRole.ID,
	})
	require.NoError(t, err, "平台角色走 platform 通道可跨租户邀请")
	f.actor.Role = "manager"
	_, err = svc.Create(f.ctx, f.actor, &CreateInvitationRequest{
		TenantID: f.tenantB.ID, Email: "cross2@example.com", RoleID: customerRole.ID,
	})
	requireInvitationCode(t, err, InvitationCodeForbidden)

	// 反向：A 租户内 rank 足够即可邀请（manager < agent? 用 admin 角色验证通道内通过路径）。
	adminRole, err := f.client.Role.Create().SetName("Admin A").SetCode("admin").SetTenantID(f.tenantA.ID).Save(f.ctx)
	require.NoError(t, err)
	adminActor := InvitationActor{UserID: f.actor.UserID, HomeTenantID: f.tenantA.ID, Role: "admin", Username: "admin-a"}
	_, err = svc.Create(f.ctx, adminActor, &CreateInvitationRequest{
		TenantID: f.tenantA.ID, Email: "same-tenant@example.com", RoleID: adminRole.ID,
	})
	require.NoError(t, err)
}
