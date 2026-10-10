package service

// 通知联动清理的兼容门面：实现位于 internal/notificationcleanup（供 repository 层
// 直接调用，避免 service ↔ repository 循环依赖），本文件保持 service 层调用方
// （ticket/change/service_request 服务与清理 CLI）签名不变。

import (
	"context"

	"itsm-backend/ent"
	"itsm-backend/internal/notificationcleanup"

	"go.uber.org/zap"
)

// NotificationResourceType 通知深链资源类型。
type NotificationResourceType = notificationcleanup.NotificationResourceType

const (
	NotificationResourceTicket         = notificationcleanup.NotificationResourceTicket
	NotificationResourceChange         = notificationcleanup.NotificationResourceChange
	NotificationResourceServiceRequest = notificationcleanup.NotificationResourceServiceRequest
)

// OrphanNotification 指向不存在（或已软删）单据的通知。
type OrphanNotification = notificationcleanup.OrphanNotification

// CleanupResourceNotifications 删除指向指定单据的站内通知，返回删除行数。
func CleanupResourceNotifications(ctx context.Context, client *ent.Client, resourceType NotificationResourceType, resourceID int) (int, error) {
	return notificationcleanup.CleanupResourceNotifications(ctx, client, resourceType, resourceID)
}

// CleanupResourceNotificationsLogged 是 CleanupResourceNotifications 的 best-effort 包装。
func CleanupResourceNotificationsLogged(ctx context.Context, client *ent.Client, logger *zap.SugaredLogger, resourceType NotificationResourceType, resourceID int) int {
	return notificationcleanup.CleanupResourceNotificationsLogged(ctx, client, logger, resourceType, resourceID)
}

// ScanOrphanResourceNotifications 扫描孤儿通知（只读）。
func ScanOrphanResourceNotifications(ctx context.Context, client *ent.Client, tenantID int, limit int) ([]OrphanNotification, error) {
	return notificationcleanup.ScanOrphanResourceNotifications(ctx, client, tenantID, limit)
}

// CleanupOrphanResourceNotifications 扫描并删除孤儿通知。
func CleanupOrphanResourceNotifications(ctx context.Context, client *ent.Client, tenantID int, limit int) ([]OrphanNotification, error) {
	return notificationcleanup.CleanupOrphanResourceNotifications(ctx, client, tenantID, limit)
}

// ParseNotificationTarget 解析 action_url 指向的单据。
func ParseNotificationTarget(actionURL string) (NotificationResourceType, int, bool) {
	return notificationcleanup.ParseNotificationTarget(actionURL)
}
