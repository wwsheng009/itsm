package sla

import (
	"context"
	"fmt"
	"testing"
	"time"

	"itsm-backend/ent"
	"itsm-backend/ent/enttest"

	_ "github.com/mattn/go-sqlite3"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap/zaptest"
)

// =====================================================================
// P2-1: 违约归责状态机（D-SLA-8，方案 §13.1）
// pending→accepted|disputed|waived；disputed→accepted|waived；终态不可逆；settled 锁定。
// =====================================================================

func setupViolationStateFixture(t *testing.T) (*ent.Client, *Service, context.Context, int, *ent.SLAViolation) {
	t.Helper()
	client := enttest.Open(t, "sqlite3", fmt.Sprintf("file:vstate_%d?mode=memory&cache=shared&_fk=1", time.Now().UnixNano()))
	t.Cleanup(func() { _ = client.Close() })
	logger := zaptest.NewLogger(t).Sugar()
	ctx := context.Background()

	tenant := client.Tenant.Create().
		SetName("VState").SetCode(fmt.Sprintf("vstate-%d", time.Now().UnixNano())).SetDomain("vstate.test").SaveX(ctx)
	user := client.User.Create().
		SetUsername("vstate-user").SetName("u").SetEmail("u@vstate.test").
		SetPasswordHash("hash").SetTenantID(tenant.ID).SaveX(ctx)
	def := client.SLADefinition.Create().
		SetName("VState Def").SetResponseTime(60).SetResolutionTime(240).
		SetTenantID(tenant.ID).SaveX(ctx)
	ticket := client.Ticket.Create().
		SetTitle("VState Ticket").SetDescription("d").SetPriority("high").SetType("incident").
		SetStatus("open").SetTicketNumber("TKT-VST-1").SetTenantID(tenant.ID).SetRequesterID(user.ID).SaveX(ctx)

	v := client.SLAViolation.Create().
		SetSLADefinitionID(def.ID).
		SetTicketID(ticket.ID).
		SetViolationType("response_time").
		SetSeverity("high").
		SetTenantID(tenant.ID).
		SaveX(ctx)
	require.NotNil(t, v)

	return client, NewService(NewEntRepository(client), logger), ctx, tenant.ID, v
}

// 合法链：pending → disputed（含 reason 必填校验）→ accepted。
func TestViolationState_DisputedThenAccepted(t *testing.T) {
	client, svc, ctx, tenantID, v := setupViolationStateFixture(t)

	// disputed 缺 reason → 拒绝
	err := svc.TransitionViolationResolutionState(ctx, v.ID, tenantID, "disputed", "  ", 7)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "reason")

	require.NoError(t, svc.TransitionViolationResolutionState(ctx, v.ID, tenantID, "disputed", "客户认为已按时响应", 7))
	got := client.SLAViolation.GetX(ctx, v.ID)
	assert.Equal(t, "disputed", got.ResolutionState)
	assert.Equal(t, "客户认为已按时响应", got.DisputeReason)

	require.NoError(t, svc.TransitionViolationResolutionState(ctx, v.ID, tenantID, "accepted", "", 9))
	got = client.SLAViolation.GetX(ctx, v.ID)
	assert.Equal(t, "accepted", got.ResolutionState)
}

// pending → waived：reason 必填；waived_by/at 留痕。
func TestViolationState_WaivedWithAuditTrail(t *testing.T) {
	client, svc, ctx, tenantID, v := setupViolationStateFixture(t)

	err := svc.TransitionViolationResolutionState(ctx, v.ID, tenantID, "waived", "", 9)
	require.Error(t, err)

	require.NoError(t, svc.TransitionViolationResolutionState(ctx, v.ID, tenantID, "waived", "客户侧停电，属合同除外条款", 9))
	got := client.SLAViolation.GetX(ctx, v.ID)
	assert.Equal(t, "waived", got.ResolutionState)
	assert.Equal(t, "客户侧停电，属合同除外条款", got.WaivedReason)
	assert.Equal(t, 9, got.WaivedBy)
	assert.False(t, got.WaivedAt.IsZero())
}

// 终态不可逆：accepted → 任意转换拒绝；waived → 任意转换拒绝。
func TestViolationState_TerminalIrreversible(t *testing.T) {
	_, svc, ctx, tenantID, v := setupViolationStateFixture(t)

	require.NoError(t, svc.TransitionViolationResolutionState(ctx, v.ID, tenantID, "accepted", "", 9))
	err := svc.TransitionViolationResolutionState(ctx, v.ID, tenantID, "disputed", "翻案", 9)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "非法状态迁移")

	err = svc.TransitionViolationResolutionState(ctx, v.ID, tenantID, "waived", "改判豁免", 9)
	require.Error(t, err)
}

// settled_flag=true（已进结算快照）后一切变更锁定。
func TestViolationState_SettledLock(t *testing.T) {
	client, svc, ctx, tenantID, v := setupViolationStateFixture(t)

	client.SLAViolation.UpdateOneID(v.ID).SetSettledFlag(true).ExecX(ctx)

	err := svc.TransitionViolationResolutionState(ctx, v.ID, tenantID, "accepted", "", 9)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "结算快照")

	err = svc.TransitionViolationResolutionState(ctx, v.ID, tenantID, "disputed", "尝试争议", 7)
	require.Error(t, err)
}

// 无效目标与非法迁移路径。
func TestViolationState_InvalidTargetAndPath(t *testing.T) {
	client, svc, ctx, tenantID, v := setupViolationStateFixture(t)

	err := svc.TransitionViolationResolutionState(ctx, v.ID, tenantID, "pending", "", 9)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "无效的归责状态目标")

	// CAS 并发冲突：expectedState 与库内不符（直接调 repo 模拟状态已被并发变更）
	affected, err := NewEntRepository(client).TransitionViolationResolutionState(ctx, v.ID, tenantID, "waived", "accepted", "", 9)
	require.NoError(t, err)
	assert.Equal(t, 0, affected, "expectedState 不匹配时 CAS 必须 0 行（并发冲突）")
}
