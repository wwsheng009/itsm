package service

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"itsm-backend/ent"
	"itsm-backend/ent/enttest"
	"itsm-backend/ent/notification"
	"itsm-backend/ent/user"
	"itsm-backend/service/bot"

	_ "github.com/mattn/go-sqlite3"
)

// O1-02 实时健康与告警：SLO 纯函数 + 快照口径 + 告警投递（通知中心/去重/租户隔离）。

type stubQueue struct{ length, capacity int }

func (s stubQueue) Len() int { return s.length }
func (s stubQueue) Cap() int { return s.capacity }

func mkRun(t *testing.T, client *ent.Client, ctx context.Context, tenantID int, status string, started time.Time) int {
	t.Helper()
	finished := started.Add(30 * time.Second)
	create := client.BotRun.Create().
		SetTenantID(tenantID).
		SetEntrypoint("chat").
		SetStatus(status).
		SetStartedAt(started)
	if status == "completed" || status == "failed" {
		create = create.SetFinishedAt(finished)
	}
	return create.SaveX(ctx).ID
}

func mkInvocation(t *testing.T, client *ent.Client, ctx context.Context, tenantID, runID int, status string) {
	t.Helper()
	client.ToolInvocation.Create().
		SetTenantID(tenantID).SetRunID(runID).SetToolName("create_ticket").
		SetStatus(status).SetCreatedAt(time.Now().Add(-2 * time.Hour)).
		SaveX(ctx)
}

func TestEvaluateAIHealthSLOs(t *testing.T) {
	// 任务成功率 80%（样本 10）→ 违规（critical：低于阈值 10% 以上）；工具错误率 33% → 违规。
	breaching := &AIHealthSnapshot{
		Runs:      AIHealthRuns{Completed: 8, Failed: 2, SuccessRate: 0.8},
		Tools:     AIHealthTools{Total: 6, Errors: 2, ErrorRate: 2.0 / 6.0},
		Providers: AIHealthProviders{Known: true, CallCount: 20, SuccessRate: 0.97},
		Quality: AIHealthQuality{
			LatencyP95Ms: 9000, FirstTokenP95Ms: 2500, FirstTokenCount: 10,
			OverallP95Ms: 9000, ErrorShown: 10, ErrorRecovered: 9, RecoveryRate: 0.9,
		},
	}
	results := EvaluateAIHealthSLOs(breaching)
	byRule := map[string]SLOResult{}
	for _, item := range results {
		byRule[item.Rule] = item
	}
	assert.Equal(t, "breach", byRule["ai_task_success_rate"].Status)
	assert.Equal(t, "breach", byRule["ai_tool_error_rate"].Status)
	assert.Equal(t, "ok", byRule["ai_first_token_p95"].Status)
	assert.Equal(t, float64(2500), byRule["ai_first_token_p95"].Actual,
		"首 token SLO 必须取 first_token，不得取整体时延")
	assert.Equal(t, 10, byRule["ai_first_token_p95"].Sample)
	assert.Equal(t, "ok", byRule["ai_error_recovery_rate"].Status) // 0.9 >= 0.9
	assert.Equal(t, "ok", byRule["ai_llm_success_rate"].Status)

	alerts := buildAlerts(results)
	require.Len(t, alerts, 2)
	for _, alert := range alerts {
		assert.Equal(t, "critical", alert.Severity, "明显偏离按 critical")
		assert.NotEmpty(t, alert.Title)
		assert.NotEmpty(t, alert.Message)
	}

	// 样本不足 → insufficient_data（不误报）；P95 缺失同样不判定。
	insufficient := &AIHealthSnapshot{
		Runs:  AIHealthRuns{Completed: 1, Failed: 0, SuccessRate: 1},
		Tools: AIHealthTools{Total: 1, ErrorRate: 1},
	}
	results = EvaluateAIHealthSLOs(insufficient)
	for _, rule := range []string{"ai_task_success_rate", "ai_tool_error_rate", "ai_error_recovery_rate", "ai_llm_success_rate", "ai_first_token_p95"} {
		item := SLOResult{}
		for _, r := range results {
			if r.Rule == rule {
				item = r
			}
		}
		assert.Equal(t, "insufficient_data", item.Status, rule)
	}
	assert.Empty(t, buildAlerts(results))
	assert.Nil(t, EvaluateAIHealthSLOs(nil))
}

func TestEvaluateAIHealthSLOs_FirstTokenIndependentFromOverall(t *testing.T) {
	// 整体慢但首 token 达标 → SLO 通过。
	ok := &AIHealthSnapshot{Quality: AIHealthQuality{
		FirstTokenP95Ms: 2000, FirstTokenCount: 10, OverallP95Ms: 9000, OverallCount: 10,
	}}
	byRule := map[string]SLOResult{}
	for _, item := range EvaluateAIHealthSLOs(ok) {
		byRule[item.Rule] = item
	}
	assert.Equal(t, "ok", byRule["ai_first_token_p95"].Status)

	// 首 token 超阈值但整体更快 → SLO 违规（证明两口径独立）。
	breach := &AIHealthSnapshot{Quality: AIHealthQuality{
		FirstTokenP95Ms: 3200, FirstTokenCount: 10, OverallP95Ms: 1000, OverallCount: 10,
	}}
	byRule = map[string]SLOResult{}
	for _, item := range EvaluateAIHealthSLOs(breach) {
		byRule[item.Rule] = item
	}
	assert.Equal(t, "breach", byRule["ai_first_token_p95"].Status)
}

// seedHealthTenant 落一个租户的基础数据（run/工具调用），返回租户与管理员 ID。
func seedHealthTenant(t *testing.T, client *ent.Client, ctx context.Context, code string, runs, failedRuns, successTools, failedTools int, withAdmins bool) (int, []int) {
	t.Helper()
	tenant := client.Tenant.Create().SetCode(code).SetName(code).SaveX(ctx)
	base := time.Now().Add(-2 * time.Hour)

	runIDs := make([]int, 0, runs)
	for i := 0; i < runs; i++ {
		status := "completed"
		if i < failedRuns {
			status = "failed"
		}
		runIDs = append(runIDs, mkRun(t, client, ctx, tenant.ID, status, base.Add(time.Duration(i)*time.Minute)))
	}
	if len(runIDs) > 0 {
		for i := 0; i < successTools; i++ {
			mkInvocation(t, client, ctx, tenant.ID, runIDs[0], "success")
		}
		for i := 0; i < failedTools; i++ {
			mkInvocation(t, client, ctx, tenant.ID, runIDs[0], "failed")
		}
	}

	adminIDs := []int{}
	mkUser := func(name string, role user.Role, active bool) int {
		create := client.User.Create().
			SetUsername(name).SetEmail(name + "@example.com").SetName(name).
			SetPasswordHash("hash").SetTenantID(tenant.ID).SetRole(role)
		if !active {
			create = create.SetActive(false)
		}
		return create.SaveX(ctx).ID
	}
	if withAdmins {
		adminIDs = append(adminIDs, mkUser(code+"-admin", user.RoleAdmin, true))
		adminIDs = append(adminIDs, mkUser(code+"-super", user.RoleSuperAdmin, true))
		mkUser(code+"-inactive", user.RoleAdmin, false)
	}
	mkUser(code+"-agent", user.RoleAgent, true)
	return tenant.ID, adminIDs
}

func TestAIHealthService_SnapshotAndDispatch(t *testing.T) {
	ctx := context.Background()
	client := enttest.Open(t, "sqlite3", "file:ai-health-dispatch?mode=memory&cache=shared&_fk=1")
	t.Cleanup(func() { _ = client.Close() })
	fixedNow := time.Date(2026, 10, 18, 12, 0, 0, 0, time.UTC)

	tenantA, adminsA := seedHealthTenant(t, client, ctx, "health-a", 10, 2, 4, 2, true)
	require.Len(t, adminsA, 2)

	svc := NewAIHealthService(client, bot.NewMetricsService(client), nil)
	svc.SetQueue(stubQueue{length: 3, capacity: 100})
	svc.now = func() time.Time { return fixedNow }

	snapshot, err := svc.Snapshot(ctx, tenantA, 7)
	require.NoError(t, err)
	assert.Equal(t, 7, snapshot.WindowDays)
	assert.Equal(t, 10, snapshot.Runs.Total)
	assert.InDelta(t, 0.8, snapshot.Runs.SuccessRate, 0.001)
	assert.Equal(t, 6, snapshot.Tools.Total)
	assert.InDelta(t, 2.0/6.0, snapshot.Tools.ErrorRate, 0.001)
	assert.True(t, snapshot.Queue.Available)
	assert.Equal(t, 3, snapshot.Queue.Length)
	assert.False(t, snapshot.Providers.Available)
	assert.NotEmpty(t, snapshot.Notes, "遥测未装配须显式降级说明")

	slos := EvaluateAIHealthSLOs(snapshot)
	require.NotEmpty(t, slos)
	assert.Equal(t, "breach", findSLO(slos, "ai_task_success_rate").Status)
	assert.Equal(t, "breach", findSLO(slos, "ai_tool_error_rate").Status)

	result, err := svc.DispatchAlerts(ctx, tenantA, 7)
	require.NoError(t, err)
	require.Len(t, result.Alerts, 2)
	assert.Equal(t, 2, result.Recipients, "仅活跃管理员收件（停用/普通用户排除）")
	assert.Equal(t, result.Recipients*len(result.Alerts), result.Created)

	rows := client.Notification.Query().Where(notification.TenantID(tenantA)).AllX(ctx)
	require.Len(t, rows, result.Created)
	assert.Equal(t, "ai_health", rows[0].Type)
	assert.Equal(t, "/ai/observability", rows[0].ActionURL)
	for _, row := range rows {
		assert.Contains(t, adminsA, row.UserID)
	}

	// 同租户同标题 30 分钟去重：重复派发 0 新建。
	second, err := svc.DispatchAlerts(ctx, tenantA, 7)
	require.NoError(t, err)
	assert.Equal(t, 0, second.Created)
	assert.Equal(t, len(second.Alerts), second.Skipped)

	// 跨租户：B 数据量不足（1 run/1 tool）→ 无告警、无通知。
	tenantB, _ := seedHealthTenant(t, client, ctx, "health-b", 1, 0, 0, 0, true)
	resultB, err := svc.DispatchAlerts(ctx, tenantB, 7)
	require.NoError(t, err)
	assert.Empty(t, resultB.Alerts)
	assert.Equal(t, 0, resultB.Created)
	assert.Empty(t, client.Notification.Query().Where(notification.TenantID(tenantB)).AllX(ctx))
}

func findSLO(items []SLOResult, rule string) SLOResult {
	for _, item := range items {
		if item.Rule == rule {
			return item
		}
	}
	return SLOResult{}
}

func TestAIHealthService_DispatchFallsBackToActiveUsers(t *testing.T) {
	ctx := context.Background()
	client := enttest.Open(t, "sqlite3", "file:ai-health-fallback?mode=memory&cache=shared&_fk=1")
	t.Cleanup(func() { _ = client.Close() })

	// 无管理员 → 回退活跃用户（停用用户不投递）。
	tenant, _ := seedHealthTenant(t, client, ctx, "health-c", 10, 5, 4, 2, false)
	svc := NewAIHealthService(client, bot.NewMetricsService(client), nil)
	svc.now = time.Now

	result, err := svc.DispatchAlerts(ctx, tenant, 7)
	require.NoError(t, err)
	// 任务成功率 50% + 工具错误率 2/6 → 两条违规。
	require.Len(t, result.Alerts, 2)
	assert.Equal(t, 1, result.Recipients, "无管理员 → 回退活跃用户（仅 1 个 agent；停用用户不在内）")
	assert.Equal(t, 2, result.Created)
	rows := client.Notification.Query().Where(notification.TenantID(tenant)).AllX(ctx)
	require.Len(t, rows, 2)
	for _, row := range rows {
		assert.Equal(t, "ai_health", row.Type)
	}
}
