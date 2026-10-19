package service

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"

	"itsm-backend/ent/enttest"
	"itsm-backend/ent/notification"
	"itsm-backend/service/bot"

	_ "github.com/mattn/go-sqlite3"
)

// O1-02 条件项闭环：定时巡检 sweeper（默认值 / 全租户巡检 / 30 分钟去重 / 租户级错误隔离）。

func TestAIHealthSweeper_Defaults(t *testing.T) {
	sw := NewAIHealthSweeper(nil, nil, nil, 0, 0)
	assert.Equal(t, 15*time.Minute, sw.Interval())
	assert.Equal(t, AIHealthDefaultWindowDays, sw.WindowDays())
	assert.Equal(t, aiHealthSweeperMaxTenantsPerRun, sw.maxTenants)

	custom := NewAIHealthSweeper(nil, nil, nil, 5, 90)
	assert.Equal(t, 5*time.Minute, custom.Interval())
	assert.Equal(t, AIHealthMaxWindowDays, custom.WindowDays(), "窗口上限 30 天")
}

func TestAIHealthSweeper_Unavailable(t *testing.T) {
	n, err := NewAIHealthSweeper(nil, nil, nil, 15, 7).RunOnce(context.Background())
	assert.Nil(t, n)
	assert.ErrorIs(t, err, ErrAIHealthSweeperUnavailable)

	// nil 接收者与未装配实例：Start 直接返回，不 panic。
	var nilSvc *AIHealthSweeper
	nilSvc.Start(context.Background())
	NewAIHealthSweeper(nil, nil, nil, 15, 7).Start(context.Background())
}

func TestAIHealthSweeper_RunOnceAggregatesTenantsAndDedupes(t *testing.T) {
	ctx := context.Background()
	client := enttest.Open(t, "sqlite3", "file:ai-health-sweep?mode=memory&cache=shared&_fk=1")
	t.Cleanup(func() { _ = client.Close() })
	fixedNow := time.Date(2026, 10, 18, 12, 0, 0, 0, time.UTC)

	// A：10 runs / 2 failed + 工具错误率 2/6 → 2 条违规；B：样本不足 → 0 条。
	tenantA, _ := seedHealthTenant(t, client, ctx, "sweep-a", 10, 2, 4, 2, true)
	tenantB, _ := seedHealthTenant(t, client, ctx, "sweep-b", 1, 0, 0, 0, true)

	health := NewAIHealthService(client, bot.NewMetricsService(client), nil)
	health.now = func() time.Time { return fixedNow }
	sw := NewAIHealthSweeper(client, health, zap.NewNop().Sugar(), 15, 7)

	summary, err := sw.RunOnce(ctx)
	require.NoError(t, err)
	assert.Equal(t, 2, summary.Tenants)
	assert.Equal(t, 2, summary.Evaluated)
	assert.Equal(t, 2, summary.Alerts, "仅 A 租户两条违规")
	assert.Equal(t, 4, summary.Created, "A 租户 2 管理员 × 2 条告警")
	assert.Equal(t, 0, summary.Errors)
	assert.Empty(t, client.Notification.Query().Where(notification.TenantID(tenantB)).AllX(ctx))

	rowsA := client.Notification.Query().Where(notification.TenantID(tenantA)).AllX(ctx)
	require.Len(t, rowsA, 4)

	// 第二轮：同租户同标题 30 分钟去重 → 0 新建、2 skip（B 无告警）。
	second, err := sw.RunOnce(ctx)
	require.NoError(t, err)
	assert.Equal(t, 0, second.Created)
	assert.Equal(t, 2, second.Skipped)
}

func TestAIHealthSweeper_IsolatesTenantErrors(t *testing.T) {
	ctx := context.Background()
	client := enttest.Open(t, "sqlite3", "file:ai-health-sweep-err?mode=memory&cache=shared&_fk=1")
	t.Cleanup(func() { _ = client.Close() })

	seedHealthTenant(t, client, ctx, "sweep-err", 10, 2, 4, 2, true)

	// 健康服务持有一个已关闭的 client：Snapshot 必然失败，用于验证「单租户失败不升级为整体错误」。
	badClient := enttest.Open(t, "sqlite3", "file:ai-health-sweep-bad?mode=memory&cache=shared&_fk=1")
	require.NoError(t, badClient.Close())
	badHealth := NewAIHealthService(badClient, bot.NewMetricsService(badClient), nil)

	sw := NewAIHealthSweeper(client, badHealth, zap.NewNop().Sugar(), 15, 7)
	summary, err := sw.RunOnce(ctx)
	require.NoError(t, err, "租户级失败不升级为整体错误")
	assert.Equal(t, 1, summary.Tenants)
	assert.Equal(t, 1, summary.Errors)
	assert.Equal(t, 0, summary.Evaluated)
	assert.Equal(t, 0, summary.Created)
}
