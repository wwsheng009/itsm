package service

import (
	"context"
	"testing"

	_ "github.com/mattn/go-sqlite3"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap/zaptest"

	"itsm-backend/common/tenantctx"
	"itsm-backend/ent/auditlog"
	"itsm-backend/ent/enttest"
)

// IP-P0-11：执行器必须先做租户 guard（错误 ctx 拒绝），再按任务租户收窄并落 source=job 审计。
func TestTimerEventHandler_TenantGuardAndJobAudit(t *testing.T) {
	client := enttest.Open(t, "sqlite3", testDSN())
	defer client.Close()
	logger := zaptest.NewLogger(t).Sugar()
	h := NewTimerEventHandler(&CustomProcessEngine{client: client}, logger)
	ctx := context.Background()

	// 1) 错误 ctx（租户不符）→ 拒绝，且不落审计。
	err := h.HandleTimerFire(tenantctx.WithTenantID(ctx, 9), &TimerRecord{TimerID: "t1", TenantID: 7, TimerType: "intermediate"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "tenant mismatch")

	// 2) 缺失租户身份 → 拒绝。
	err = h.HandleTimerFire(ctx, &TimerRecord{TimerID: "t2", TenantID: 0, TimerType: "intermediate"})
	require.Error(t, err)

	count, err := client.AuditLog.Query().Where(auditlog.SourceEQ("job")).Count(ctx)
	require.NoError(t, err)
	assert.Zero(t, count, "被拒的 timer 不应落审计")

	// 3) ctx 一致 → 通过守卫并落 source=job 审计（unknown type 在分发阶段报错）。
	err = h.HandleTimerFire(tenantctx.WithTenantID(ctx, 7), &TimerRecord{TimerID: "t3", TenantID: 7, TimerType: "unknown"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unknown timer type")

	logs, err := client.AuditLog.Query().Where(auditlog.SourceEQ("job")).All(ctx)
	require.NoError(t, err)
	require.Len(t, logs, 1)
	assert.Equal(t, "timer.fire", logs[0].Action)
	assert.Equal(t, 7, logs[0].TenantID)
	assert.Equal(t, 7, logs[0].TargetTenantID)
	assert.Equal(t, "timer-scheduler", logs[0].ActorAccount)
}

// 超时扫描器：错误 ctx 不得执行其他租户的 job；正确 ctx 下空扫描通过。
func TestTimeoutScanner_RejectsWrongTenantCtx(t *testing.T) {
	client := newTimeoutScannerClient(t, "job_guard")
	defer client.Close()
	tenantID, _ := setupTimeoutScannerTenant(t, client, "job_guard")
	scanner := NewTimeoutScanner(client, zaptest.NewLogger(t).Sugar())

	_, err := scanner.ScanOverdueTasks(tenantctx.WithTenantID(context.Background(), tenantID+1), tenantID)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "tenant mismatch")

	processed, err := scanner.ScanOverdueTasks(context.Background(), tenantID)
	require.NoError(t, err)
	assert.Zero(t, processed)
}

// 自动升级任务：错误 ctx 拒绝；正确 ctx 通过（无待升级任务时不落审计）。
func TestWorkflowAutomation_RejectsWrongTenantCtx(t *testing.T) {
	client := enttest.Open(t, "sqlite3", testDSN())
	defer client.Close()
	was := NewWorkflowAutomationService(client, zaptest.NewLogger(t).Sugar())

	err := was.CheckAutoEscalation(tenantctx.WithTenantID(context.Background(), 2), 1)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "tenant mismatch")

	require.NoError(t, was.CheckAutoEscalation(tenantctx.WithTenantID(context.Background(), 1), 1))
}
