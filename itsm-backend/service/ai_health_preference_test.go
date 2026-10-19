package service

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"itsm-backend/ent/enttest"
	"itsm-backend/ent/notification"
	"itsm-backend/service/bot"

	_ "github.com/mattn/go-sqlite3"
)

// O1-02 条件项闭环：订阅偏好映射——AI 健康告警收件人按用户
// notification_preferences(event_type=ai_health_alert) 过滤（无偏好行=默认放行）。

func TestAIHealthDispatch_RespectsSubscriptionPreference(t *testing.T) {
	ctx := context.Background()
	client := enttest.Open(t, "sqlite3", "file:ai-health-pref?mode=memory&cache=shared&_fk=1")
	t.Cleanup(func() { _ = client.Close() })
	fixedNow := time.Date(2026, 10, 18, 12, 0, 0, 0, time.UTC)

	tenant, admins := seedHealthTenant(t, client, ctx, "health-pref", 10, 2, 4, 2, true)
	require.Len(t, admins, 2)

	// admin[0] 显式退订站内渠道 → 排除；admin[1] 显式开启 → 保留。
	client.NotificationPreference.Create().
		SetUserID(admins[0]).SetTenantID(tenant).
		SetEventType(AIHealthAlertEventType).
		SetInAppEnabled(false).
		SaveX(ctx)
	client.NotificationPreference.Create().
		SetUserID(admins[1]).SetTenantID(tenant).
		SetEventType(AIHealthAlertEventType).
		SetInAppEnabled(true).
		SaveX(ctx)

	svc := NewAIHealthService(client, bot.NewMetricsService(client), nil)
	svc.now = func() time.Time { return fixedNow }

	result, err := svc.DispatchAlerts(ctx, tenant, 7)
	require.NoError(t, err)
	require.Len(t, result.Alerts, 2)
	assert.Equal(t, 1, result.Recipients, "显式退订的管理员被排除")
	assert.Equal(t, 1, result.SkippedByPreference)
	assert.Equal(t, 2, result.Created, "1 收件人 × 2 条告警")

	rows := client.Notification.Query().Where(notification.TenantID(tenant)).AllX(ctx)
	require.Len(t, rows, 2)
	for _, row := range rows {
		assert.Equal(t, admins[1], row.UserID, "只投递给订阅放行的用户")
	}
}

func TestAIHealthDispatch_AllOptedOutSkipsDelivery(t *testing.T) {
	ctx := context.Background()
	client := enttest.Open(t, "sqlite3", "file:ai-health-optout?mode=memory&cache=shared&_fk=1")
	t.Cleanup(func() { _ = client.Close() })

	tenant, admins := seedHealthTenant(t, client, ctx, "health-optout", 10, 2, 4, 2, true)
	for _, id := range admins {
		client.NotificationPreference.Create().
			SetUserID(id).SetTenantID(tenant).
			SetEventType(AIHealthAlertEventType).
			SetInAppEnabled(false).
			SaveX(ctx)
	}

	svc := NewAIHealthService(client, bot.NewMetricsService(client), nil)
	svc.now = time.Now

	result, err := svc.DispatchAlerts(ctx, tenant, 7)
	require.NoError(t, err)
	require.Len(t, result.Alerts, 2, "告警评估照常，投递被订阅偏好拦截")
	assert.Equal(t, 0, result.Recipients)
	assert.Equal(t, 2, result.SkippedByPreference)
	assert.Equal(t, 0, result.Created)
	assert.Empty(t, client.Notification.Query().Where(notification.TenantID(tenant)).AllX(ctx))
}

func TestAIHealthDispatch_NoPreferenceRowsDefaultAllow(t *testing.T) {
	ctx := context.Background()
	client := enttest.Open(t, "sqlite3", "file:ai-health-nopref?mode=memory&cache=shared&_fk=1")
	t.Cleanup(func() { _ = client.Close() })

	tenant, admins := seedHealthTenant(t, client, ctx, "health-nopref", 10, 2, 4, 2, true)

	svc := NewAIHealthService(client, bot.NewMetricsService(client), nil)
	svc.now = time.Now

	result, err := svc.DispatchAlerts(ctx, tenant, 7)
	require.NoError(t, err)
	assert.Equal(t, len(admins), result.Recipients, "无偏好行=放行，与平台既有语义一致")
	assert.Equal(t, 0, result.SkippedByPreference)
}
