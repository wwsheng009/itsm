package service

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// P1-3（#11/#12）Pause/Resume 真实计时核心实现（ticket + incident 两分支）：
// 暂停记录 sla_paused_at/reason；恢复把暂停时长追加到 response/resolution deadline
// （合同"除外时间"条款的计时基础）。
func TestSLAMonitorService_PauseResumeSLA_TicketAndIncident(t *testing.T) {
	client, svc, ctx := setupSLAMonitorTest(t)
	defer client.Close()

	tenant, err := createSLATestTenant(ctx, client, "pauseresume")
	require.NoError(t, err)
	user, err := createSLATestUser(ctx, client, tenant.ID, "pr")
	require.NoError(t, err)

	now := time.Now()
	deadlineResp := now.Add(2 * time.Hour)
	deadlineRes := now.Add(8 * time.Hour)

	ticket, err := client.Ticket.Create().
		SetTitle("Pause Ticket").
		SetDescription("t").
		SetPriority("high").
		SetType("incident").
		SetStatus("open").
		SetTicketNumber("TKT-PR-001").
		SetTenantID(tenant.ID).
		SetRequesterID(user.ID).
		SetSLAResponseDeadline(deadlineResp).
		SetSLAResolutionDeadline(deadlineRes).
		Save(ctx)
	require.NoError(t, err)

	// ——— ticket 分支 ———
	require.NoError(t, svc.PauseSLA(ctx, tenant.ID, "ticket", ticket.ID, "客户侧停电"))
	got := client.Ticket.GetX(ctx, ticket.ID)
	assert.Equal(t, "paused", got.SLAStatus)
	assert.False(t, got.SLAPausedAt.IsZero())
	assert.Equal(t, "客户侧停电", got.SLAPauseReason)

	// 已暂停再暂停 → 业务错误（不得静默成功）
	assert.Error(t, svc.PauseSLA(ctx, tenant.ID, "ticket", ticket.ID, "重复"))

	// 篡改 paused_at 到 30 分钟前以获得确定性时长
	past := time.Now().Add(-30 * time.Minute)
	client.Ticket.UpdateOneID(ticket.ID).SetSLAPausedAt(past).ExecX(ctx)
	before := client.Ticket.GetX(ctx, ticket.ID)

	require.NoError(t, svc.ResumeSLA(ctx, tenant.ID, "ticket", ticket.ID))
	after := client.Ticket.GetX(ctx, ticket.ID)
	assert.Equal(t, "active", after.SLAStatus)
	assert.True(t, after.SLAPausedAt.IsZero(), "恢复后 sla_paused_at 必须清空")
	assert.Empty(t, after.SLAPauseReason)

	deltaResp := after.SLAResponseDeadline.Sub(before.SLAResponseDeadline)
	deltaRes := after.SLAResolutionDeadline.Sub(before.SLAResolutionDeadline)
	assert.GreaterOrEqual(t, deltaResp, 29*time.Minute)
	assert.LessOrEqual(t, deltaResp, 31*time.Minute)
	assert.GreaterOrEqual(t, deltaRes, 29*time.Minute)

	// 未暂停恢复 → 业务错误
	assert.Error(t, svc.ResumeSLA(ctx, tenant.ID, "ticket", ticket.ID))

	// ——— incident 分支 ———
	inc, err := client.Incident.Create().
		SetTitle("Pause Incident").
		SetIncidentNumber("INC-PR-001").
		SetStatus("open").
		SetReporterID(user.ID).
		SetTenantID(tenant.ID).
		SetSLAResponseDeadline(deadlineResp).
		SetSLAResolutionDeadline(deadlineRes).
		Save(ctx)
	require.NoError(t, err)

	require.NoError(t, svc.PauseSLA(ctx, tenant.ID, "incident", inc.ID, "维护窗口"))
	incGot := client.Incident.GetX(ctx, inc.ID)
	assert.Equal(t, "paused", incGot.SLAStatus)
	assert.Equal(t, "维护窗口", incGot.SLAPauseReason)

	incPast := time.Now().Add(-15 * time.Minute)
	client.Incident.UpdateOneID(inc.ID).SetSLAPausedAt(incPast).ExecX(ctx)
	incBefore := client.Incident.GetX(ctx, inc.ID)
	require.NoError(t, svc.ResumeSLA(ctx, tenant.ID, "incident", inc.ID))
	incAfter := client.Incident.GetX(ctx, inc.ID)
	assert.Equal(t, "active", incAfter.SLAStatus)
	incDelta := incAfter.SLAResponseDeadline.Sub(incBefore.SLAResponseDeadline)
	assert.GreaterOrEqual(t, incDelta, 14*time.Minute)
	assert.LessOrEqual(t, incDelta, 16*time.Minute)
}
