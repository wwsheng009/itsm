package service

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"itsm-backend/common/tenantctx"
	"itsm-backend/connector"
	"itsm-backend/dto"
	"itsm-backend/ent"
	"itsm-backend/ent/enttest"
	"itsm-backend/ent/notificationdelivery"
	"itsm-backend/ent/operationalcommand"
	"itsm-backend/ent/ticket"
	"itsm-backend/ent/usertenantmembership"
	"itsm-backend/internal/commandbus"

	"entgo.io/ent/dialect"
	_ "github.com/mattn/go-sqlite3"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

type notificationTestConnector struct {
	sent atomic.Int32
	fail bool
}

func (*notificationTestConnector) Manifest() connector.Manifest {
	return connector.Manifest{
		Name: "feishu", Version: "1.0.0", Title: "Test Feishu", Type: connector.TypeIM,
		Capabilities: []connector.Capability{connector.CapSendMessage}, RequiredPermissions: []string{"connector:send"},
	}
}
func (*notificationTestConnector) Init(context.Context, connector.Config) error { return nil }
func (c *notificationTestConnector) Send(context.Context, *connector.Message) error {
	c.sent.Add(1)
	if c.fail {
		return errors.New("provider-secret-shaped-error-must-not-persist")
	}
	return nil
}

func (*notificationTestConnector) HealthCheck(context.Context) connector.HealthStatus {
	return connector.HealthStatus{OK: true}
}
func (*notificationTestConnector) Close() error { return nil }

func notificationDeliveryFixture(t *testing.T) (*ent.Client, context.Context, int, int, int) {
	t.Helper()
	dsn := fmt.Sprintf("file:%s?mode=memory&cache=shared&_fk=1", strings.ReplaceAll(t.Name(), "/", "-"))
	client := enttest.Open(t, dialect.SQLite, dsn)
	ctx := context.Background()
	tenant, err := client.Tenant.Create().SetName("Notify Tenant").SetCode("notify-tenant").SetDomain("notify.example.com").SetStatus("active").Save(ctx)
	require.NoError(t, err)
	user, err := client.User.Create().SetUsername("notify-user").SetEmail("notify@example.com").SetName("Notify User").
		SetPasswordHash("hash").SetRole("agent").SetActive(true).SetTenantID(tenant.ID).Save(ctx)
	require.NoError(t, err)
	ticket, err := client.Ticket.Create().SetTitle("通知工单").SetTicketNumber("NOTIFY-1").SetRequesterID(user.ID).SetTenantID(tenant.ID).Save(ctx)
	require.NoError(t, err)
	return client, ctx, tenant.ID, user.ID, ticket.ID
}

func TestTicketNotificationServiceEnqueuesDurableDelivery(t *testing.T) {
	client, ctx, tenantID, userID, ticketID := notificationDeliveryFixture(t)
	svc := NewTicketNotificationService(client, zap.NewNop().Sugar())
	svc.EnableOutbox()
	err := svc.SendNotification(ctx, ticketID, &dto.SendTicketNotificationRequest{
		UserIDs: []int{userID, userID}, Type: "assigned", Channel: "in_app", Content: "你有一个新工单", IdempotencyKey: "assignment-event-1",
	}, tenantID)
	require.NoError(t, err)
	err = svc.SendNotification(ctx, ticketID, &dto.SendTicketNotificationRequest{
		UserIDs: []int{userID}, Type: "assigned", Channel: "in_app", Content: "你有一个新工单", IdempotencyKey: "assignment-event-1",
	}, tenantID)
	require.NoError(t, err)
	commands, err := client.OperationalCommand.Query().Where(operationalcommand.TenantIDEQ(tenantID)).All(ctx)
	require.NoError(t, err)
	require.Len(t, commands, 1)
	require.Equal(t, commandbus.CommandDeliverNotification, commands[0].CommandType)
}

func TestNotificationDeliveryHandlerCreatesInAppRecordsIdempotently(t *testing.T) {
	client, ctx, tenantID, userID, ticketID := notificationDeliveryFixture(t)
	cmd, err := commandbus.Enqueue(ctx, client, commandbus.EnqueueRequest{
		TenantID: tenantID, CommandType: commandbus.CommandDeliverNotification,
		AggregateType: "ticket", AggregateID: ticketID, IdempotencyKey: "notify-once",
		Payload: map[string]interface{}{"ticketId": ticketID, "recipientId": userID, "type": "assigned", "channel": "in_app", "content": "你有一个新工单"},
	})
	require.NoError(t, err)
	cmd.Attempt = 1
	handler := NewNotificationDeliveryCommandHandler(client, nil, zap.NewNop().Sugar())
	require.NoError(t, handler.Handle(ctx, cmd))
	require.NoError(t, handler.Handle(ctx, cmd))

	ticketNotifications, err := client.TicketNotification.Query().All(ctx)
	require.NoError(t, err)
	require.Len(t, ticketNotifications, 1)
	notifications, err := client.Notification.Query().All(ctx)
	require.NoError(t, err)
	require.Len(t, notifications, 1)
	deliveries, err := client.NotificationDelivery.Query().Where(notificationdelivery.OperationalCommandIDEQ(cmd.ID)).All(ctx)
	require.NoError(t, err)
	require.Len(t, deliveries, 1)
	require.Equal(t, "sent", deliveries[0].Status)
}

// TestNotificationDeliveryHandlerDeliversProjectMilestone PE-14 收尾：里程碑到期
// 通知（resourceType=project_milestone）走 in_app 投递：落通用 notification +
// notification_delivery 审计，动作链接指向里程碑所属项目工作台，无 ticket 关联行。
func TestNotificationDeliveryHandlerDeliversProjectMilestone(t *testing.T) {
	client, ctx, tenantID, userID, _ := notificationDeliveryFixture(t)
	projectEntity, err := client.Project.Create().SetTenantID(tenantID).
		SetName("交付项目").SetCode("PRJ-NOTIFY").SetStatus("active").Save(ctx)
	require.NoError(t, err)
	milestone, err := client.ProjectMilestone.Create().SetTenantID(tenantID).
		SetProjectID(projectEntity.ID).SetName("UAT 完成").
		SetDueDate(time.Now().Add(24 * time.Hour)).SetStatus("planned").Save(ctx)
	require.NoError(t, err)
	cmd, err := commandbus.Enqueue(ctx, client, commandbus.EnqueueRequest{
		TenantID: tenantID, CommandType: commandbus.CommandDeliverNotification,
		AggregateType: "project_milestone", AggregateID: milestone.ID, IdempotencyKey: "milestone-due-1",
		Payload: map[string]interface{}{
			"resourceType": "project_milestone", "resourceId": milestone.ID,
			"recipientId": userID, "type": ProjectMilestoneDueNotificationType,
			"channel": "in_app", "content": "项目「交付项目」的里程碑「UAT 完成」将于 2026-10-18 到期",
		},
	})
	require.NoError(t, err)
	cmd.Attempt = 1
	handler := NewNotificationDeliveryCommandHandler(client, nil, zap.NewNop().Sugar())
	require.NoError(t, handler.Handle(ctx, cmd))

	notifications, err := client.Notification.Query().All(ctx)
	require.NoError(t, err)
	require.Len(t, notifications, 1)
	require.Equal(t, fmt.Sprintf("/projects/%d", projectEntity.ID), notifications[0].ActionURL)
	require.Equal(t, "查看项目里程碑", notifications[0].ActionText)
	deliveries, err := client.NotificationDelivery.Query().
		Where(notificationdelivery.OperationalCommandIDEQ(cmd.ID)).All(ctx)
	require.NoError(t, err)
	require.Len(t, deliveries, 1)
	require.Equal(t, "sent", deliveries[0].Status)
	require.Zero(t, client.TicketNotification.Query().CountX(ctx), "项目通知不关联工单通知行")
}

// TestNotificationDeliveryHandlerMilestoneDueHonorsPreference 事件类型偏好隔离：
// 用户显式关闭 project_milestone_due 的 in_app 渠道后，投递按偏好跳过（成功且不落记录）。
func TestNotificationDeliveryHandlerMilestoneDueHonorsPreference(t *testing.T) {
	client, ctx, tenantID, userID, _ := notificationDeliveryFixture(t)
	_, err := client.NotificationPreference.Create().
		SetUserID(userID).SetTenantID(tenantID).SetEventType(ProjectMilestoneDueNotificationType).
		SetInAppEnabled(false).SetEmailEnabled(false).Save(ctx)
	require.NoError(t, err)
	projectEntity, err := client.Project.Create().SetTenantID(tenantID).
		SetName("交付项目").SetCode("PRJ-NOTIFY-PREF").SetStatus("active").Save(ctx)
	require.NoError(t, err)
	milestone, err := client.ProjectMilestone.Create().SetTenantID(tenantID).
		SetProjectID(projectEntity.ID).SetName("UAT 完成").
		SetDueDate(time.Now().Add(24 * time.Hour)).SetStatus("planned").Save(ctx)
	require.NoError(t, err)
	cmd, err := commandbus.Enqueue(ctx, client, commandbus.EnqueueRequest{
		TenantID: tenantID, CommandType: commandbus.CommandDeliverNotification,
		AggregateType: "project_milestone", AggregateID: milestone.ID, IdempotencyKey: "milestone-due-pref",
		Payload: map[string]interface{}{
			"resourceType": "project_milestone", "resourceId": milestone.ID,
			"recipientId": userID, "type": ProjectMilestoneDueNotificationType,
			"channel": "in_app", "content": "应被偏好拦截",
		},
	})
	require.NoError(t, err)
	cmd.Attempt = 1
	handler := NewNotificationDeliveryCommandHandler(client, nil, zap.NewNop().Sugar())
	require.NoError(t, handler.Handle(ctx, cmd), "偏好跳过应视为成功而非错误")
	require.Zero(t, client.Notification.Query().CountX(ctx))
	require.Zero(t, client.NotificationDelivery.Query().CountX(ctx))
}

// TestNotificationDeliveryHandlerSkipsWhenPreferenceDisabled 核心契约：偏好表对
// 某事件类型的 in_app 渠道显式关闭后，投递命令应成功返回且不落任何投递/通知记录
// （语义=按用户选择跳过，而非失败；避免 worker 无效重试到 dead_letter）。
// 同时验证短词表映射：载荷 type=created 必须命中偏好表 ticket_created 行。
func TestNotificationDeliveryHandlerSkipsWhenPreferenceDisabled(t *testing.T) {
	client, ctx, tenantID, userID, ticketID := notificationDeliveryFixture(t)
	_, err := client.NotificationPreference.Create().
		SetUserID(userID).SetTenantID(tenantID).SetEventType("ticket_created").
		SetInAppEnabled(false).SetEmailEnabled(false).Save(ctx)
	require.NoError(t, err)
	cmd, err := commandbus.Enqueue(ctx, client, commandbus.EnqueueRequest{
		TenantID: tenantID, CommandType: commandbus.CommandDeliverNotification,
		AggregateType: "ticket", AggregateID: ticketID, IdempotencyKey: "pref-skip",
		Payload: map[string]interface{}{"ticketId": ticketID, "recipientId": userID, "type": "created", "channel": "in_app", "content": "应被偏好拦截"},
	})
	require.NoError(t, err)
	cmd.Attempt = 1
	handler := NewNotificationDeliveryCommandHandler(client, nil, zap.NewNop().Sugar())
	require.NoError(t, handler.Handle(ctx, cmd), "偏好跳过应视为成功而非错误")

	require.Zero(t, client.TicketNotification.Query().CountX(ctx), "不应创建 ticket_notification 记录")
	require.Zero(t, client.Notification.Query().CountX(ctx), "不应创建通用 notification 记录")
	require.Zero(t, client.NotificationDelivery.Query().CountX(ctx), "不应创建投递审计记录")
}

// TestNotificationDeliveryHandlerAllowsWhenNoPreference 无偏好记录 = 默认放行，
// 与同步路径（channelAllowedForEvent）及偏好 API 的默认语义一致。
func TestNotificationDeliveryHandlerAllowsWhenNoPreference(t *testing.T) {
	client, ctx, tenantID, userID, ticketID := notificationDeliveryFixture(t)
	cmd, err := commandbus.Enqueue(ctx, client, commandbus.EnqueueRequest{
		TenantID: tenantID, CommandType: commandbus.CommandDeliverNotification,
		AggregateType: "ticket", AggregateID: ticketID, IdempotencyKey: "pref-allow",
		Payload: map[string]interface{}{"ticketId": ticketID, "recipientId": userID, "type": "sla_breached", "channel": "in_app", "content": "无偏好应放行"},
	})
	require.NoError(t, err)
	cmd.Attempt = 1
	handler := NewNotificationDeliveryCommandHandler(client, nil, zap.NewNop().Sugar())
	require.NoError(t, handler.Handle(ctx, cmd))
	require.Equal(t, 1, client.Notification.Query().CountX(ctx), "无偏好记录应默认放行")
}

// TestNotificationDeliveryHandlerConnectorBlockedByPushToggle 连接器渠道按
// push_enabled 判定：push=false 时 feishu 投递被跳过且不调用 connector。
func TestNotificationDeliveryHandlerConnectorBlockedByPushToggle(t *testing.T) {
	client, ctx, tenantID, userID, ticketID := notificationDeliveryFixture(t)
	_, err := client.User.UpdateOneID(userID).SetFeishuOpenID("ou_pref_blocked").Save(ctx)
	require.NoError(t, err)
	_, err = client.NotificationPreference.Create().
		SetUserID(userID).SetTenantID(tenantID).SetEventType("ticket_assigned").
		SetInAppEnabled(true).SetPushEnabled(false).Save(ctx)
	require.NoError(t, err)
	fake := &notificationTestConnector{}
	registry := connector.NewRegistry()
	registry.Register(func() connector.Connector { return fake })
	manager := connector.NewManager(registry, zap.NewNop().Sugar())
	require.NoError(t, manager.Provision(ctx, connector.Config{TenantID: tenantID, Name: "feishu", Provider: "test", Enabled: true}))
	cmd, err := commandbus.Enqueue(ctx, client, commandbus.EnqueueRequest{
		TenantID: tenantID, CommandType: commandbus.CommandDeliverNotification,
		AggregateType: "ticket", AggregateID: ticketID, IdempotencyKey: "pref-push",
		Payload: map[string]interface{}{"ticketId": ticketID, "recipientId": userID, "type": "assigned", "channel": "feishu", "content": "push 关闭应拦截"},
	})
	require.NoError(t, err)
	cmd.Attempt = 1
	handler := NewNotificationDeliveryCommandHandler(client, manager, zap.NewNop().Sugar())
	require.NoError(t, handler.Handle(ctx, cmd))
	require.Equal(t, int32(0), fake.sent.Load(), "push_enabled=false 不应调用 connector")
	require.Zero(t, client.NotificationDelivery.Query().CountX(ctx), "被拦截的投递不应产生审计记录")
}

// TestNormalizeNotificationEventType 锁定短→长词表映射：这是偏好匹配正确性的前提，
// 任何一侧词表变更都必须同步本映射与该测试。
func TestNormalizeNotificationEventType(t *testing.T) {
	cases := map[string]string{
		"created":                  "ticket_created",
		"assigned":                 "ticket_assigned",
		"updated":                  "ticket_updated",
		"status_changed":           "ticket_updated",
		"commented":                "comment_added",
		"resolved":                 "ticket_resolved",
		"closed":                   "ticket_closed",
		"sla_warning":              "sla_warning",
		"sla_alert":                "sla_warning",
		"sla_breached":             "sla_violated",
		"change_approval_required": "approval_required",
		"change_approval_decided":  "approval_completed",
		// 未识别值原样返回 → 查无偏好 → 默认放行
		"some_future_type": "some_future_type",
	}
	for in, want := range cases {
		require.Equal(t, want, normalizeNotificationEventType(in), "映射错误: %s", in)
	}
}

func TestNotificationDeliveryHandlerRejectsCrossTenantRecipient(t *testing.T) {
	client, ctx, tenantID, _, ticketID := notificationDeliveryFixture(t)
	otherTenant, err := client.Tenant.Create().SetName("Other").SetCode("other-notify").SetDomain("other-notify.example.com").SetStatus("active").Save(ctx)
	require.NoError(t, err)
	foreign, err := client.User.Create().SetUsername("foreign-notify").SetEmail("foreign@example.com").SetName("Foreign").
		SetPasswordHash("hash").SetRole("agent").SetActive(true).SetTenantID(otherTenant.ID).Save(ctx)
	require.NoError(t, err)
	cmd, err := commandbus.Enqueue(ctx, client, commandbus.EnqueueRequest{
		TenantID: tenantID, CommandType: commandbus.CommandDeliverNotification,
		AggregateType: "ticket", AggregateID: ticketID, IdempotencyKey: "cross-tenant",
		Payload: map[string]interface{}{"ticketId": ticketID, "recipientId": foreign.ID, "type": "assigned", "channel": "in_app", "content": "forbidden"},
	})
	require.NoError(t, err)
	err = NewNotificationDeliveryCommandHandler(client, nil, zap.NewNop().Sugar()).Handle(ctx, cmd)
	require.Error(t, err)
	require.Zero(t, client.Notification.Query().CountX(ctx))
}

func TestNotificationDeliveryHandlerSendsConnectorAndAuditsMaskedTarget(t *testing.T) {
	client, ctx, tenantID, userID, ticketID := notificationDeliveryFixture(t)
	_, err := client.User.UpdateOneID(userID).SetFeishuOpenID("ou_sensitive_target_1234").Save(ctx)
	require.NoError(t, err)
	fake := &notificationTestConnector{}
	registry := connector.NewRegistry()
	registry.Register(func() connector.Connector { return fake })
	manager := connector.NewManager(registry, zap.NewNop().Sugar())
	require.NoError(t, manager.Provision(ctx, connector.Config{TenantID: tenantID, Name: "feishu", Provider: "test", Enabled: true}))
	cmd, err := commandbus.Enqueue(ctx, client, commandbus.EnqueueRequest{
		TenantID: tenantID, CommandType: commandbus.CommandDeliverNotification,
		AggregateType: "ticket", AggregateID: ticketID, IdempotencyKey: "feishu-once",
		Payload: map[string]interface{}{"ticketId": ticketID, "recipientId": userID, "type": "assigned", "channel": "feishu", "content": "你有一个新工单"},
	})
	require.NoError(t, err)
	cmd.Attempt = 1
	handler := NewNotificationDeliveryCommandHandler(client, manager, zap.NewNop().Sugar())
	require.NoError(t, handler.Handle(ctx, cmd))
	require.NoError(t, handler.Handle(ctx, cmd))
	require.Equal(t, int32(1), fake.sent.Load())
	delivery, err := client.NotificationDelivery.Query().Only(ctx)
	require.NoError(t, err)
	require.Equal(t, "sent", delivery.Status)
	require.NotContains(t, delivery.TargetMasked, "sensitive")
	require.NotEqual(t, "ou_sensitive_target_1234", delivery.TargetMasked)
}

// TestNotifyTicketCreatedTxExcludesInactiveUsers 广播分支必须排除停用用户：
// 停用用户入箱后投递端报「user not found」进 dead_letter（prod 实测 user5 场景）。
func TestNotifyTicketCreatedTxExcludesInactiveUsers(t *testing.T) {
	client, ctx, tenantID, userID, _ := notificationDeliveryFixture(t)
	// 工单创建后无 assignee，只挂 requester → 触发广播分支
	_, err := client.User.Create().SetUsername("inactive-broadcast").SetEmail("inactive-b@example.com").
		SetName("Inactive B").SetPasswordHash("hash").SetRole("agent").SetActive(false).
		SetTenantID(tenantID).Save(ctx)
	require.NoError(t, err)
	activeB, err := client.User.Create().SetUsername("active-broadcast").SetEmail("active-b@example.com").
		SetName("Active B").SetPasswordHash("hash").SetRole("agent").SetActive(true).
		SetTenantID(tenantID).Save(ctx)
	require.NoError(t, err)

	svc := NewTicketNotificationService(client, zap.NewNop().Sugar())
	svc.EnableTxOutbox()
	tx, err := client.Tx(ctx)
	require.NoError(t, err)
	tk, err := tx.Ticket.Create().SetTitle("broadcast-ticket").SetTicketNumber("NOTIFY-BC").
		SetRequesterID(userID).SetTenantID(tenantID).Save(ctx)
	require.NoError(t, err)
	require.NoError(t, svc.NotifyTicketCreatedTx(ctx, tx, tk))
	require.NoError(t, tx.Commit())

	commands, err := client.OperationalCommand.Query().
		Where(operationalcommand.TenantIDEQ(tenantID), operationalcommand.AggregateIDEQ(tk.ID)).
		All(ctx)
	require.NoError(t, err)
	recipients := map[int]struct{}{}
	for _, cmd := range commands {
		recipients[asInt(cmd.Payload["recipientId"])] = struct{}{}
	}
	require.NotContains(t, recipients, 0)
	require.Contains(t, recipients, activeB.ID, "活跃用户应被广播")
	require.Len(t, commands, 2, "requester + 1 活跃广播用户，停用用户不入箱")
}

func TestNotificationDeliveryFailureRetriesThenDeadLettersWithSafeAudit(t *testing.T) {
	client, ctx, tenantID, userID, ticketID := notificationDeliveryFixture(t)
	_, err := client.User.UpdateOneID(userID).SetFeishuOpenID("ou_failure_target").Save(ctx)
	require.NoError(t, err)
	fake := &notificationTestConnector{fail: true}
	registry := connector.NewRegistry()
	registry.Register(func() connector.Connector { return fake })
	manager := connector.NewManager(registry, zap.NewNop().Sugar())
	require.NoError(t, manager.Provision(ctx, connector.Config{TenantID: tenantID, Name: "feishu", Provider: "test", Enabled: true}))
	cmd, err := commandbus.Enqueue(ctx, client, commandbus.EnqueueRequest{
		TenantID: tenantID, CommandType: commandbus.CommandDeliverNotification, AggregateType: "ticket", AggregateID: ticketID,
		IdempotencyKey: "feishu-dead-letter", MaxAttempts: 2,
		Payload: map[string]interface{}{"ticketId": ticketID, "recipientId": userID, "type": "assigned", "channel": "feishu", "content": "失败重试"},
	})
	require.NoError(t, err)
	commandRegistry := commandbus.NewRegistry()
	handler := NewNotificationDeliveryCommandHandler(client, manager, zap.NewNop().Sugar())
	require.NoError(t, commandRegistry.Register(commandbus.CommandDeliverNotification, handler.Handle))
	worker := commandbus.NewWorker(client, commandRegistry, zap.NewNop().Sugar(), "notification-worker")
	processed, err := worker.RunOnce(ctx)
	require.NoError(t, err)
	require.True(t, processed)
	_, err = client.OperationalCommand.UpdateOneID(cmd.ID).SetAvailableAt(time.Now().Add(-time.Second)).Save(ctx)
	require.NoError(t, err)
	processed, err = worker.RunOnce(ctx)
	require.NoError(t, err)
	require.True(t, processed)
	stored, err := client.OperationalCommand.Get(ctx, cmd.ID)
	require.NoError(t, err)
	require.Equal(t, commandbus.StatusDeadLetter, stored.Status)
	require.NotContains(t, stored.LastError, "provider-secret")
	delivery, err := client.NotificationDelivery.Query().Only(ctx)
	require.NoError(t, err)
	require.Equal(t, "failed", delivery.Status)
	require.Equal(t, 2, delivery.Attempt)
	require.NotContains(t, delivery.ErrorMessage, "provider-secret")
}

// TestNotifyTicketCreatedTxRollsBackWithTicket 验证事务性入箱与主表变更「同生同死」：
// 在 tx 内调用 NotifyTicketCreatedTx 后 rollback，operational_command 必须为零。
// 这是阶段 B（工单创建）下沉的核心契约：业务事务失败时不得有「孤儿」通知入箱。
func TestNotifyTicketCreatedTxRollsBackWithTicket(t *testing.T) {
	client, ctx, tenantID, userID, ticketID := notificationDeliveryFixture(t)
	svc := NewTicketNotificationService(client, zap.NewNop().Sugar())
	svc.EnableTxOutbox()

	tx, err := client.Tx(ctx)
	require.NoError(t, err)

	createdTicket, err := tx.Ticket.Create().
		SetTitle("rollback-ticket").
		SetTicketNumber("NOTIFY-RB").
		SetRequesterID(userID).
		SetTenantID(tenantID).
		Save(ctx)
	require.NoError(t, err)

	err = svc.NotifyTicketCreatedTx(ctx, tx, createdTicket)
	require.NoError(t, err)
	require.NoError(t, tx.Rollback())

	commands, err := client.OperationalCommand.Query().
		Where(operationalcommand.TenantIDEQ(tenantID)).
		All(ctx)
	require.NoError(t, err)
	require.Empty(t, commands, "rollback 之后 operational_command 必须随业务事务一起消失")
	_, err = client.Ticket.Get(ctx, ticketID)
	require.NoError(t, err)
	_, err = client.Ticket.Query().Where(ticket.TicketNumberEQ("NOTIFY-RB")).Only(ctx)
	require.Error(t, err, "rollback 后刚创建的 ticket 也必须消失")
}

// TestNotifyTicketCreatedTxCommitsWithTicket 验证事务提交后通知与工单都可见，且收件人覆盖 assignee/requester。
func TestNotifyTicketCreatedTxCommitsWithTicket(t *testing.T) {
	client, ctx, tenantID, userID, _ := notificationDeliveryFixture(t)
	assignee, err := client.User.Create().SetUsername("notify-assignee").SetEmail("assignee@example.com").SetName("Assignee").
		SetPasswordHash("hash").SetRole("agent").SetActive(true).SetTenantID(tenantID).Save(ctx)
	require.NoError(t, err)

	svc := NewTicketNotificationService(client, zap.NewNop().Sugar())
	svc.EnableTxOutbox()

	tx, err := client.Tx(ctx)
	require.NoError(t, err)

	createdTicket, err := tx.Ticket.Create().
		SetTitle("commit-ticket").
		SetTicketNumber("NOTIFY-OK").
		SetRequesterID(userID).
		SetAssigneeID(assignee.ID).
		SetTenantID(tenantID).
		Save(ctx)
	require.NoError(t, err)

	require.NoError(t, svc.NotifyTicketCreatedTx(ctx, tx, createdTicket))
	require.NoError(t, tx.Commit())

	commands, err := client.OperationalCommand.Query().
		Where(operationalcommand.TenantIDEQ(tenantID), operationalcommand.AggregateIDEQ(createdTicket.ID)).
		All(ctx)
	require.NoError(t, err)
	require.Len(t, commands, 2, "assignee + requester 两个收件人应各产生一条入箱行")

	recipients := map[int]struct{}{}
	for _, cmd := range commands {
		require.Equal(t, commandbus.CommandDeliverNotification, cmd.CommandType)
		require.Equal(t, "ticket", cmd.AggregateType)
		require.NotNil(t, cmd.Payload)
		recipients[asInt(cmd.Payload["recipientId"])] = struct{}{}
		require.Equal(t, "created", cmd.Payload["type"])
		require.Equal(t, "in_app", cmd.Payload["channel"])
	}
	require.Contains(t, recipients, userID)
	require.Contains(t, recipients, assignee.ID)
	// 域下沉的语义保证：recipientId 必须是 user.id，而不是 ticket.id 或其它主键。
	// fixture 中 ticketID=1 与 userID=1 恰好相同是巧合，所以这里改用「恰好 2 条收件人」
	// 且 Payload 中 ticketId 字段等于 createdTicket.ID 的硬约束来证明这一点。
	seenTicketID := 0
	for _, cmd := range commands {
		seenTicketID++
		require.Equal(t, createdTicket.ID, asInt(cmd.Payload["ticketId"]))
	}
	require.Equal(t, 2, seenTicketID)
}

func TestNotificationDeliveryHandlerDeliversChangeInApp(t *testing.T) {
	client, ctx, tenantID, userID, _ := notificationDeliveryFixture(t)
	creator, err := client.User.Get(ctx, userID)
	require.NoError(t, err)
	changeEntity, err := client.Change.Create().SetTitle("approval delivery").SetDescription("change notification").
		SetCreatedBy(creator.ID).SetTenantID(tenantID).Save(ctx)
	require.NoError(t, err)
	cmd, err := commandbus.Enqueue(ctx, client, commandbus.EnqueueRequest{
		TenantID: tenantID, CommandType: commandbus.CommandDeliverNotification,
		AggregateType: "change", AggregateID: changeEntity.ID, IdempotencyKey: "change-delivery-test",
		Payload: map[string]interface{}{
			"resourceType": "change", "resourceId": changeEntity.ID, "recipientId": userID,
			"type": "change_approval_required", "channel": "in_app", "content": "请审批变更",
		},
	})
	require.NoError(t, err)

	handler := NewNotificationDeliveryCommandHandler(client, nil, zap.NewNop().Sugar())
	require.NoError(t, handler.Handle(ctx, cmd))

	deliveries, err := client.NotificationDelivery.Query().All(ctx)
	require.NoError(t, err)
	require.Len(t, deliveries, 1)
	require.Nil(t, deliveries[0].TicketID)
	require.Nil(t, deliveries[0].TicketNotificationID)
	notifications, err := client.Notification.Query().All(ctx)
	require.NoError(t, err)
	require.Len(t, notifications, 1)
	require.Equal(t, fmt.Sprintf("/changes/%d", changeEntity.ID), notifications[0].ActionURL)
}

// asInt 兼容 SQLite JSON 反序列化后数字统一为 float64 的现实，阶段 A 测试专用。
func asInt(v interface{}) int {
	switch n := v.(type) {
	case int:
		return n
	case int64:
		return int(n)
	case float64:
		return int(n)
	default:
		return -1
	}
}

// TestNotifySLABreachedTxRollsBack 验证 SLA 违规通知同样随事务回滚消失。
func TestNotifySLABreachedTxRollsBack(t *testing.T) {
	client, ctx, tenantID, _, ticketID := notificationDeliveryFixture(t)
	svc := NewTicketNotificationService(client, zap.NewNop().Sugar())
	svc.EnableTxOutbox()

	tx, err := client.Tx(ctx)
	require.NoError(t, err)

	err = svc.NotifySLABreachedTx(ctx, tx, ticketID, "response_time", 30, tenantID)
	require.NoError(t, err)
	require.NoError(t, tx.Rollback())

	commands, err := client.OperationalCommand.Query().
		Where(operationalcommand.TenantIDEQ(tenantID), operationalcommand.AggregateIDEQ(ticketID)).
		All(ctx)
	require.NoError(t, err)
	require.Empty(t, commands, "SLA breach 通知随 tx rollback 必须消失")
}

// TestNotifySLABreachedTxIdempotent 验证 SLA breach 入箱对同一参数幂等：
// 重复 enqueue 同一 occurrenceKey 只产生一行 operational_command。
func TestNotifySLABreachedTxIdempotent(t *testing.T) {
	client, ctx, tenantID, _, ticketID := notificationDeliveryFixture(t)
	svc := NewTicketNotificationService(client, zap.NewNop().Sugar())
	svc.EnableTxOutbox()

	tx, err := client.Tx(ctx)
	require.NoError(t, err)
	defer tx.Rollback()

	require.NoError(t, svc.NotifySLABreachedTx(ctx, tx, ticketID, "response_time", 30, tenantID))
	require.NoError(t, svc.NotifySLABreachedTx(ctx, tx, ticketID, "response_time", 30, tenantID))
	require.NoError(t, tx.Commit())

	commands, err := client.OperationalCommand.Query().
		Where(operationalcommand.TenantIDEQ(tenantID), operationalcommand.AggregateIDEQ(ticketID)).
		All(ctx)
	require.NoError(t, err)
	require.Len(t, commands, 1, "同一 occurrenceKey 重复 enqueue 必须被唯一索引合并为一行")
}

// TestNotifySLABreachedTxRejectsCrossTenant 验证调用方传入与 ticket 不一致的 tenantID 直接 fail-closed。
func TestNotifySLABreachedTxRejectsCrossTenant(t *testing.T) {
	client, ctx, tenantID, _, ticketID := notificationDeliveryFixture(t)
	svc := NewTicketNotificationService(client, zap.NewNop().Sugar())
	svc.EnableTxOutbox()

	tx, err := client.Tx(ctx)
	require.NoError(t, err)
	defer tx.Rollback()

	err = svc.NotifySLABreachedTx(ctx, tx, ticketID, "response_time", 30, tenantID+999)
	require.Error(t, err, "tenantID 与 ticket.TenantID 不一致必须被拒绝")
	commands, err := client.OperationalCommand.Query().Where(operationalcommand.AggregateIDEQ(ticketID)).All(ctx)
	require.NoError(t, err)
	require.Empty(t, commands)
}

// TestNotifyTicketCreatedTxFailsClosedWhenFlagDisabled 验证未调用 EnableTxOutbox 时 Tx API 直接报错，
// 避免静默回退到 client 路径产生主表/通知分离提交。
func TestNotifyTicketCreatedTxFailsClosedWhenFlagDisabled(t *testing.T) {
	client, ctx, tenantID, userID, _ := notificationDeliveryFixture(t)
	svc := NewTicketNotificationService(client, zap.NewNop().Sugar())

	tx, err := client.Tx(ctx)
	require.NoError(t, err)
	defer tx.Rollback()

	createdTicket, err := tx.Ticket.Create().
		SetTitle("disabled-flag-ticket").
		SetTicketNumber("NOTIFY-DIS").
		SetRequesterID(userID).
		SetTenantID(tenantID).
		Save(ctx)
	require.NoError(t, err)

	err = svc.NotifyTicketCreatedTx(ctx, tx, createdTicket)
	require.Error(t, err)
	require.Contains(t, err.Error(), "transactional notification outbox disabled")
}

// TestNotifySLAAlertLevelChangedTxCommits 验证 SLA 预警级别变更通知随 tx 提交而持久化。
func TestNotifySLAAlertLevelChangedTxCommits(t *testing.T) {
	client, ctx, tenantID, _, ticketID := notificationDeliveryFixture(t)
	svc := NewTicketNotificationService(client, zap.NewNop().Sugar())
	svc.EnableTxOutbox()

	tx, err := client.Tx(ctx)
	require.NoError(t, err)

	require.NoError(t, svc.NotifySLAAlertLevelChangedTx(ctx, tx, ticketID, "warning", 80.0, tenantID))
	require.NoError(t, tx.Commit())

	commands, err := client.OperationalCommand.Query().
		Where(operationalcommand.AggregateIDEQ(ticketID)).
		All(ctx)
	require.NoError(t, err)
	require.Len(t, commands, 1)
	require.Equal(t, commandbus.CommandDeliverNotification, commands[0].CommandType)
	require.NotNil(t, commands[0].Payload)
	require.Equal(t, "sla_alert", commands[0].Payload["type"])
}

func notificationMembershipTestCommand(t *testing.T, client *ent.Client, ctx context.Context, tenantID, userID, ticketID int, channel string) *ent.OperationalCommand {
	t.Helper()
	cmd, err := commandbus.Enqueue(ctx, client, commandbus.EnqueueRequest{
		TenantID: tenantID, CommandType: commandbus.CommandDeliverNotification,
		AggregateType: "ticket", AggregateID: ticketID, IdempotencyKey: "membership-delivery",
		Payload: map[string]interface{}{
			"ticketId": ticketID, "recipientId": userID, "type": "assigned", "channel": channel, "content": "membership notification",
			// A payload claim must never authorize delivery or choose the stored reference.
			"recipientMembershipId": 999999, "tenantId": tenantID + 999,
		},
	})
	require.NoError(t, err)
	cmd.Attempt = 1
	return cmd
}

func notificationMembershipTestConnector(t *testing.T, ctx context.Context, tenantID int) (*notificationTestConnector, *connector.Manager) {
	t.Helper()
	fake := &notificationTestConnector{}
	registry := connector.NewRegistry()
	registry.Register(func() connector.Connector { return fake })
	manager := connector.NewManager(registry, zap.NewNop().Sugar())
	require.NoError(t, manager.Provision(ctx, connector.Config{TenantID: tenantID, Name: "feishu", Provider: "test", Enabled: true}))
	return fake, manager
}

func assertNoMembershipNotificationDelivery(t *testing.T, client *ent.Client, ctx context.Context, fake *notificationTestConnector) {
	t.Helper()
	require.Zero(t, fake.sent.Load(), "ineligible recipients must not reach any connector")
	require.Zero(t, client.Notification.Query().CountX(ctx))
	require.Zero(t, client.TicketNotification.Query().CountX(ctx))
	require.Zero(t, client.NotificationDelivery.Query().CountX(ctx))
}

// Exercise the registered worker handler, not only the shared membership helper.
// In particular, a provider home account must be delivered in the customer scope.
func TestNotificationDeliveryHandlerMembershipEligibility(t *testing.T) {
	cases := []struct {
		name       string
		home       bool
		membership string
		disabled   bool
		allow      bool
	}{
		{name: "provider_active", membership: "active", allow: true},
		{name: "provider_nonmember"},
		{name: "provider_suspended", membership: "suspended"},
		{name: "provider_revoked", membership: "revoked"},
		{name: "provider_expired", membership: "expired"},
		{name: "provider_disabled", membership: "active", disabled: true},
		{name: "membership_only_in_other_tenant", membership: "other_tenant"},
		{name: "home_active", home: true, membership: "active", allow: true},
		{name: "home_legacy_never_member", home: true, allow: true},
		{name: "home_suspended_cannot_fallback", home: true, membership: "suspended"},
		{name: "home_revoked_cannot_fallback", home: true, membership: "revoked"},
		{name: "home_expired_cannot_fallback", home: true, membership: "expired"},
		{name: "home_disabled_member", home: true, membership: "active", disabled: true},
		{name: "home_disabled_legacy", home: true, disabled: true},
	}
	for _, tc := range cases {
		for _, channel := range []string{"in_app", "feishu"} {
			t.Run(tc.name+"/"+channel, func(t *testing.T) {
				client, ctx, tenantID, userID, ticketID := notificationDeliveryFixture(t)
				ctx = tenantctx.WithTenantID(ctx, tenantID)
				provider := client.Tenant.Create().SetName("Provider").SetCode("provider").SaveX(ctx)
				account := client.User.UpdateOneID(userID).SetFeishuOpenID("ou_member_target").SetActive(!tc.disabled)
				if !tc.home {
					account.SetTenantID(provider.ID)
				}
				account.SaveX(ctx)
				membershipID := 0
				if tc.membership != "" {
					membershipTenantID := tenantID
					if tc.membership == "other_tenant" {
						membershipTenantID = provider.ID
					}
					create := client.UserTenantMembership.Create().SetUserID(userID).SetTenantID(membershipTenantID).
						SetAccountKind(usertenantmembership.AccountKindProvider).SetSource(usertenantmembership.SourceAllocation)
					switch tc.membership {
					case "suspended":
						create.SetStatus(usertenantmembership.StatusSuspended)
					case "revoked":
						create.SetDeletedAt(time.Now()).SetDeassignedAt(time.Now())
					case "expired":
						create.SetExpiresAt(time.Now().Add(-time.Hour))
					default:
						create.SetExpiresAt(time.Now().Add(time.Hour))
					}
					membershipID = create.SaveX(tenantctx.WithTenantID(ctx, membershipTenantID)).ID
				}
				fake, manager := notificationMembershipTestConnector(t, ctx, tenantID)
				cmd := notificationMembershipTestCommand(t, client, ctx, tenantID, userID, ticketID, channel)
				handler := NewNotificationDeliveryCommandHandler(client, manager, zap.NewNop().Sugar())
				err := handler.Handle(ctx, cmd)
				if !tc.allow {
					require.Error(t, err)
					assertNoMembershipNotificationDelivery(t, client, ctx, fake)
					return
				}
				require.NoError(t, err)
				require.NoError(t, handler.Handle(ctx, cmd), "same command replay remains idempotent")
				tn := client.TicketNotification.Query().OnlyX(ctx)
				require.Equal(t, userID, tn.UserID)
				require.Equal(t, tenantID, tn.TenantID)
				require.Equal(t, membershipID, tn.UserMembershipID, "persist authoritative membership, not payload claims")
				delivery := client.NotificationDelivery.Query().OnlyX(ctx)
				require.Equal(t, "sent", delivery.Status)
				require.Equal(t, tenantID, delivery.TenantID)
				require.Equal(t, userID, delivery.RecipientID)
				if channel == "in_app" {
					n := client.Notification.Query().OnlyX(ctx)
					require.Equal(t, tenantID, n.TenantID)
					require.Equal(t, membershipID, n.UserMembershipID)
					require.Zero(t, fake.sent.Load())
				} else {
					require.Equal(t, int32(1), fake.sent.Load())
					require.Equal(t, cmd.IdempotencyKey, delivery.ProviderMessageID)
					require.Zero(t, client.Notification.Query().CountX(ctx))
				}
				client.User.UpdateOneID(userID).SetActive(false).SaveX(ctx)
				require.NoError(t, handler.Handle(ctx, cmd), "already-sent replay stays a no-op even after eligibility changes")
				require.Equal(t, 1, client.TicketNotification.Query().CountX(ctx))
				require.Equal(t, 1, client.NotificationDelivery.Query().CountX(ctx))
				if channel == "feishu" {
					require.Equal(t, int32(1), fake.sent.Load())
				} else {
					require.Equal(t, 1, client.Notification.Query().CountX(ctx))
				}
			})
		}
	}
}

func TestNotificationDeliveryHandlerMembershipLookupErrorsFailClosed(t *testing.T) {
	for _, failAt := range []int{1, 2} {
		for _, channel := range []string{"in_app", "feishu"} {
			t.Run(fmt.Sprintf("lookup_%d/%s", failAt, channel), func(t *testing.T) {
				client, ctx, tenantID, userID, ticketID := notificationDeliveryFixture(t)
				fake, manager := notificationMembershipTestConnector(t, ctx, tenantID)
				cmd := notificationMembershipTestCommand(t, client, ctx, tenantID, userID, ticketID, channel)
				lookupErr := errors.New("membership storage unavailable")
				queries := 0
				client.UserTenantMembership.Intercept(ent.InterceptFunc(func(next ent.Querier) ent.Querier {
					return ent.QuerierFunc(func(ctx context.Context, query ent.Query) (ent.Value, error) {
						queries++
						if queries == failAt {
							return nil, lookupErr
						}
						return next.Query(ctx, query)
					})
				}))
				err := NewNotificationDeliveryCommandHandler(client, manager, zap.NewNop().Sugar()).Handle(ctx, cmd)
				require.ErrorIs(t, err, lookupErr, "neither active lookup nor legacy-history lookup errors may be swallowed")
				assertNoMembershipNotificationDelivery(t, client, ctx, fake)
			})
		}
	}
}

func TestNotificationDeliveryHandlerCommandTenantGuard(t *testing.T) {
	client, ctx, tenantID, userID, ticketID := notificationDeliveryFixture(t)
	fake, manager := notificationMembershipTestConnector(t, ctx, tenantID)
	cmd := notificationMembershipTestCommand(t, client, ctx, tenantID, userID, ticketID, "in_app")
	handler := NewNotificationDeliveryCommandHandler(client, manager, zap.NewNop().Sugar())
	require.Error(t, handler.Handle(ctx, nil))
	for _, invalidTenantID := range []int{0, -1} {
		invalid := *cmd
		invalid.TenantID = invalidTenantID
		require.ErrorIs(t, handler.Handle(ctx, &invalid), tenantctx.ErrNoTenant)
	}
	require.ErrorIs(t, handler.Handle(tenantctx.WithTenantID(ctx, tenantID+1), cmd), tenantctx.ErrTenantMismatch)
	assertNoMembershipNotificationDelivery(t, client, ctx, fake)

	// Direct consumer calls bind the trusted command scope even without an HTTP
	// context. This checks the context contract used by the production RLS driver.
	client.Intercept(ent.InterceptFunc(func(next ent.Querier) ent.Querier {
		return ent.QuerierFunc(func(queryCtx context.Context, query ent.Query) (ent.Value, error) {
			actual, ok := tenantctx.TenantID(queryCtx)
			require.True(t, ok)
			require.Equal(t, tenantID, actual)
			require.False(t, tenantctx.IsSystemBypass(queryCtx))
			return next.Query(queryCtx, query)
		})
	}))
	client.Use(func(next ent.Mutator) ent.Mutator {
		return ent.MutateFunc(func(mutationCtx context.Context, mutation ent.Mutation) (ent.Value, error) {
			actual, ok := tenantctx.TenantID(mutationCtx)
			require.True(t, ok)
			require.Equal(t, tenantID, actual)
			require.False(t, tenantctx.IsSystemBypass(mutationCtx))
			return next.Mutate(mutationCtx, mutation)
		})
	})
	require.NoError(t, handler.Handle(ctx, cmd))
}

func TestNotificationDeliveryWorkerRevalidatesMembershipBeforeRetry(t *testing.T) {
	client, ctx, tenantID, userID, ticketID := notificationDeliveryFixture(t)
	provider := client.Tenant.Create().SetName("Provider").SetCode("retry-provider").SaveX(ctx)
	client.User.UpdateOneID(userID).SetTenantID(provider.ID).SetFeishuOpenID("ou_retry_member").SaveX(ctx)
	membership := client.UserTenantMembership.Create().SetUserID(userID).SetTenantID(tenantID).
		SetAccountKind(usertenantmembership.AccountKindProvider).SetSource(usertenantmembership.SourceAllocation).SaveX(ctx)
	fake, manager := notificationMembershipTestConnector(t, ctx, tenantID)
	fake.fail = true
	cmd := notificationMembershipTestCommand(t, client, ctx, tenantID, userID, ticketID, "feishu")
	client.OperationalCommand.UpdateOneID(cmd.ID).SetMaxAttempts(2).SaveX(ctx)
	registry := commandbus.NewRegistry()
	handler := NewNotificationDeliveryCommandHandler(client, manager, zap.NewNop().Sugar())
	require.NoError(t, registry.Register(commandbus.CommandDeliverNotification, handler.Handle))
	worker := commandbus.NewWorker(client, registry, zap.NewNop().Sugar(), "membership-retry-worker")
	processed, err := worker.RunOnce(ctx)
	require.NoError(t, err)
	require.True(t, processed)
	require.Equal(t, int32(1), fake.sent.Load())
	before := client.NotificationDelivery.Query().OnlyX(ctx)
	require.Equal(t, "failed", before.Status)
	require.Equal(t, membership.ID, client.TicketNotification.Query().OnlyX(ctx).UserMembershipID)

	// Revocation after enqueue/first send must prevent the next external call.
	client.UserTenantMembership.UpdateOneID(membership.ID).SetDeletedAt(time.Now()).SaveX(ctx)
	client.OperationalCommand.UpdateOneID(cmd.ID).SetAvailableAt(time.Now().Add(-time.Second)).SaveX(ctx)
	fake.fail = false
	processed, err = worker.RunOnce(ctx)
	require.NoError(t, err)
	require.True(t, processed)
	require.Equal(t, int32(1), fake.sent.Load())
	after := client.NotificationDelivery.Query().OnlyX(ctx)
	require.Equal(t, before.ID, after.ID)
	require.Equal(t, before.Status, after.Status)
	require.Equal(t, before.Attempt, after.Attempt)
	require.Equal(t, 1, client.TicketNotification.Query().CountX(ctx))
	require.Zero(t, client.Notification.Query().CountX(ctx))
	stored := client.OperationalCommand.GetX(ctx, cmd.ID)
	require.Equal(t, commandbus.StatusDeadLetter, stored.Status)
	require.Contains(t, stored.LastError, "validate notification recipient")
}
