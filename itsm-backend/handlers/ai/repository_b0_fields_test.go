package ai_test

import (
	"context"
	"testing"
	"time"

	"itsm-backend/ent/enttest"
	"itsm-backend/handlers/ai"

	_ "github.com/mattn/go-sqlite3"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// B0-02：Bot 运行态字段（run_id/step_id/target_*/support_ref/idempotency_key_hash/
// expires_at/verify_*/attempt_count/last_error_code）与 dry_run 的实体读写契约：
// 零值不写列（既有行为不变）、有值往返一致、幂等键在租户内唯一。
func TestEntRepository_BotFieldsRoundTrip(t *testing.T) {
	client := enttest.Open(t, "sqlite3", "file:ai_repo_b0_fields?mode=memory&cache=shared&_fk=1")
	t.Cleanup(func() { _ = client.Close() })

	repo := ai.NewEntRepository(client)
	ctx := context.Background()

	tenant, err := client.Tenant.Create().SetCode("bot-fields").SetName("Bot Fields").Save(ctx)
	require.NoError(t, err)
	user, err := client.User.Create().
		SetUsername("bot-fields-user").
		SetEmail("bot-fields@example.com").
		SetName("Bot Fields User").
		SetPasswordHash("x").
		SetTenantID(tenant.ID).
		Save(ctx)
	require.NoError(t, err)

	conv, err := client.Conversation.Create().
		SetTenantID(tenant.ID).
		SetUserID(user.ID).
		SetTitle("B0-02 会话").
		Save(ctx)
	require.NoError(t, err)

	expires := time.Now().Add(15 * time.Minute).Truncate(time.Second)
	created, err := repo.CreateToolInvocation(ctx, &ai.ToolInvocation{
		TenantID:           tenant.ID,
		UserID:             user.ID,
		ConversationID:     conv.ID,
		ToolName:           "create_ticket",
		Arguments:          `{"title":"断电演练"}`,
		Status:             "pending",
		NeedsApproval:      true,
		ApprovalState:      "pending",
		RunID:              1001,
		StepID:             3003,
		TargetType:         "ticket",
		TargetID:           "t-42",
		SupportRef:         "kb:runbook/42",
		IdempotencyKeyHash: "hash-abc",
		ExpiresAt:          &expires,
		VerifyState:        "pending",
		VerifyNote:         "待回读",
		AttemptCount:       2,
		LastErrorCode:      "timeout",
		DryRun:             true,
	})
	require.NoError(t, err)

	got, err := repo.GetToolInvocation(ctx, created.ID, tenant.ID)
	require.NoError(t, err)
	assert.Equal(t, conv.ID, got.ConversationID)
	assert.Equal(t, 1001, got.RunID)
	assert.Equal(t, 3003, got.StepID)
	assert.Equal(t, "ticket", got.TargetType)
	assert.Equal(t, "t-42", got.TargetID)
	assert.Equal(t, "kb:runbook/42", got.SupportRef)
	assert.Equal(t, "hash-abc", got.IdempotencyKeyHash)
	require.NotNil(t, got.ExpiresAt)
	assert.WithinDuration(t, expires, *got.ExpiresAt, time.Second)
	assert.Equal(t, "pending", got.VerifyState)
	assert.Equal(t, "待回读", got.VerifyNote)
	assert.Equal(t, 2, got.AttemptCount)
	assert.Equal(t, "timeout", got.LastErrorCode)
	assert.True(t, got.DryRun)

	// 零值不写列：既有调用方（不带这些字段）行为不变。
	plain, err := repo.CreateToolInvocation(ctx, &ai.ToolInvocation{
		TenantID:      tenant.ID,
		UserID:        user.ID,
		ToolName:      "list_tickets",
		Status:        "executed",
		ApprovalState: "auto",
	})
	require.NoError(t, err)
	assert.Zero(t, plain.RunID)
	assert.Zero(t, plain.StepID)
	assert.Empty(t, plain.IdempotencyKeyHash)
	assert.Nil(t, plain.ExpiresAt)
	assert.Zero(t, plain.AttemptCount)
	assert.False(t, plain.DryRun, "默认非 dry-run")
}

// B0-05 前置：幂等键 hash 在租户内唯一（同租户重复 → 冲突；跨租户互不影响）。
func TestEntRepository_IdempotencyKeyUniqueness(t *testing.T) {
	client := enttest.Open(t, "sqlite3", "file:ai_repo_b0_idem?mode=memory&cache=shared&_fk=1")
	t.Cleanup(func() { _ = client.Close() })

	repo := ai.NewEntRepository(client)
	ctx := context.Background()

	t1, err := client.Tenant.Create().SetCode("idem-1").SetName("Idem 1").Save(ctx)
	require.NoError(t, err)
	t2, err := client.Tenant.Create().SetCode("idem-2").SetName("Idem 2").Save(ctx)
	require.NoError(t, err)

	// tool_invocations.user_id 是指向 users 的外键：显式写入非零值时必须存在真实行。
	u1, err := client.User.Create().
		SetUsername("idem-u1").SetEmail("idem-u1@example.com").SetName("Idem U1").
		SetPasswordHash("x").SetTenantID(t1.ID).Save(ctx)
	require.NoError(t, err)
	u2, err := client.User.Create().
		SetUsername("idem-u2").SetEmail("idem-u2@example.com").SetName("Idem U2").
		SetPasswordHash("x").SetTenantID(t2.ID).Save(ctx)
	require.NoError(t, err)

	base := func(tenantID int) *ai.ToolInvocation {
		userID := u1.ID
		if tenantID == t2.ID {
			userID = u2.ID
		}
		return &ai.ToolInvocation{
			TenantID:           tenantID,
			UserID:             userID,
			ToolName:           "create_ticket",
			Status:             "pending",
			ApprovalState:      "pending",
			IdempotencyKeyHash: "same-hash",
		}
	}

	_, err = repo.CreateToolInvocation(ctx, base(t1.ID))
	require.NoError(t, err)

	_, err = repo.CreateToolInvocation(ctx, base(t1.ID))
	require.Error(t, err, "同租户同幂等键必须冲突（防止重复写）")

	_, err = repo.CreateToolInvocation(ctx, base(t2.ID))
	require.NoError(t, err, "幂等作用域为租户：跨租户同 hash 不冲突")

	// 读工具（hash 为空）不受唯一约束影响：多行 NULL 不冲突。
	for i := 0; i < 2; i++ {
		_, err = repo.CreateToolInvocation(ctx, &ai.ToolInvocation{
			TenantID:      t1.ID,
			UserID:        u1.ID,
			ToolName:      "list_tickets",
			Status:        "executed",
			ApprovalState: "auto",
		})
		require.NoError(t, err)
	}
}
