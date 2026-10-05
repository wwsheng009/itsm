package common

import (
	"context"
	"testing"

	"itsm-backend/ent/auditlog"
	"itsm-backend/ent/enttest"
	entuser "itsm-backend/ent/user"

	_ "github.com/mattn/go-sqlite3"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	"golang.org/x/crypto/bcrypt"
)

// TestServiceChangePassword IP-P1-5 自助改密闭环（旧密码 → 策略 → 清标志 → 审计）。
func TestServiceChangePassword(t *testing.T) {
	ctx := context.Background()
	client := enttest.Open(t, "sqlite3", "file:ent?mode=memory&_fk=1")
	defer client.Close()

	tenant, err := client.Tenant.Create().SetName("客户A").SetCode("cred-a").SetStatus("active").SetType("saas_customer").Save(ctx)
	require.NoError(t, err)

	oldHash, err := bcrypt.GenerateFromPassword([]byte("OldPass123!"), bcrypt.DefaultCost)
	require.NoError(t, err)
	u, err := client.User.Create().
		SetUsername("cred-user").
		SetEmail("cred-user@example.com").
		SetName("Cred User").
		SetPasswordHash(string(oldHash)).
		SetTenantID(tenant.ID).
		SetRole(entuser.RoleAdmin).
		SetActive(true).
		SetMustChangePassword(true).
		Save(ctx)
	require.NoError(t, err)

	svc := NewService(NewEntRepository(client), "test-secret", zap.NewNop().Sugar(), client)

	t.Run("旧密码错误被拒", func(t *testing.T) {
		err := svc.ChangePassword(ctx, u.ID, tenant.ID, "WrongPass123!", "NewPass456!")
		assert.ErrorIs(t, err, ErrOldPasswordMismatch)
	})

	t.Run("弱口令按策略拒绝并透出文案", func(t *testing.T) {
		err := svc.ChangePassword(ctx, u.ID, tenant.ID, "OldPass123!", "weak")
		var policyErr *PasswordPolicyViolationError
		require.ErrorAs(t, err, &policyErr)
		assert.NotEmpty(t, policyErr.Reason)
	})

	t.Run("跨租户双键不可改", func(t *testing.T) {
		other, err := client.Tenant.Create().SetName("客户B").SetCode("cred-b").SetStatus("active").SetType("saas_customer").Save(ctx)
		require.NoError(t, err)
		err = svc.ChangePassword(ctx, u.ID, other.ID, "OldPass123!", "NewPass456!")
		assert.Error(t, err)
	})

	t.Run("成功改密清标志并落审计", func(t *testing.T) {
		require.NoError(t, svc.ChangePassword(ctx, u.ID, tenant.ID, "OldPass123!", "NewPass456!"))

		updated, err := client.User.Get(ctx, u.ID)
		require.NoError(t, err)
		assert.False(t, updated.MustChangePassword, "首登强制改密标志应被清除")
		assert.NoError(t, bcrypt.CompareHashAndPassword([]byte(updated.PasswordHash), []byte("NewPass456!")), "新口令应可校验")

		logs, err := client.AuditLog.Query().
			Where(auditlog.ActionEQ("auth.change_password"), auditlog.TenantIDEQ(tenant.ID)).
			Order(auditlog.ByID()).
			All(ctx)
		require.NoError(t, err)
		require.NotEmpty(t, logs)
		last := logs[len(logs)-1]
		assert.Equal(t, 200, last.StatusCode)
		assert.Equal(t, u.ID, last.UserID)
		if last.RequestBody != nil {
			assert.NotContains(t, *last.RequestBody, "NewPass456!")
			assert.NotContains(t, *last.RequestBody, "OldPass123!")
		}
	})
}
