package notificationcleanup

import (
	"context"
	"fmt"
	"regexp"

	"itsm-backend/common/tenantctx"
	"itsm-backend/ent"
	"itsm-backend/ent/change"
	"itsm-backend/ent/notification"
	"itsm-backend/ent/predicate"
	"itsm-backend/ent/servicerequest"
	"itsm-backend/ent/ticket"

	"go.uber.org/zap"
)

// 单据通知联动清理（通知中心死链治理）。
//
// 背景：通知中心记录（notifications 表）的 action_url 是纯文本深链
// （/tickets/N、/changes/N、/service-requests/N、/msp/workbench?ticket=N&...），
// 与单据表之间没有外键。单据被删除后（产品内软删/硬删，或绕过应用的清理/存量脏数据），
// 通知仍指向不存在的详情页，点击即 404。
//
// 治理分两层：
//  1. 产品删除路径：删除成功后调用 CleanupResourceNotificationsLogged 联动清理
//     （覆盖 ticket/change/service_request 及服务商侧 MSP 工作台深链）；
//  2. 硬删/存量场景：由 cmd/cleanup-orphan-notifications 调用
//     ScanOrphanResourceNotifications / CleanupOrphanResourceNotifications 兜底扫描清理。
//
// 注意：action_url 中的单据 ID 全局唯一，跨租户清理（服务商工作台深链通知落在
// 服务商租户）是安全的；清理函数内部使用 system bypass 上下文。

// NotificationResourceType 通知深链资源类型，与 notification.deliver 命令的
// resourceType 取值对齐（ticket/change/service_request）。
type NotificationResourceType string

const (
	NotificationResourceTicket         NotificationResourceType = "ticket"
	NotificationResourceChange         NotificationResourceType = "change"
	NotificationResourceServiceRequest NotificationResourceType = "service_request"
)

// notificationTargetPredicates 返回指向指定单据的 action_url 匹配条件。
// 覆盖精确路径、带查询串/子路径的变体，以及 MSP 工作台深链（ticket= 参数，
// 兼容参数顺序）。ID 匹配均带分隔符边界，不会误伤 225/2251 这类相邻编号。
func notificationTargetPredicates(resourceType NotificationResourceType, resourceID int) []predicate.Notification {
	switch resourceType {
	case NotificationResourceTicket:
		exact := fmt.Sprintf("/tickets/%d", resourceID)
		return []predicate.Notification{
			notification.ActionURLEQ(exact),
			notification.ActionURLHasPrefix(exact + "?"),
			notification.ActionURLHasPrefix(exact + "/"),
			notification.ActionURLContains(fmt.Sprintf("ticket=%d&", resourceID)),
			notification.ActionURLHasSuffix(fmt.Sprintf("ticket=%d", resourceID)),
		}
	case NotificationResourceChange:
		exact := fmt.Sprintf("/changes/%d", resourceID)
		return []predicate.Notification{
			notification.ActionURLEQ(exact),
			notification.ActionURLHasPrefix(exact + "?"),
			notification.ActionURLHasPrefix(exact + "/"),
		}
	case NotificationResourceServiceRequest:
		exact := fmt.Sprintf("/service-requests/%d", resourceID)
		return []predicate.Notification{
			notification.ActionURLEQ(exact),
			notification.ActionURLHasPrefix(exact + "?"),
			notification.ActionURLHasPrefix(exact + "/"),
		}
	default:
		return nil
	}
}

// CleanupResourceNotifications 删除指向指定单据的站内通知，返回删除行数。
// 资源类型不支持或 ID 非法时返回错误；无匹配时返回 0, nil。
//
// 调用方无需自行加 system bypass：函数内部以系统作用域执行，以便同时清理
// 落在服务商租户的 MSP 工作台深链通知。
func CleanupResourceNotifications(ctx context.Context, client *ent.Client, resourceType NotificationResourceType, resourceID int) (int, error) {
	if client == nil {
		return 0, fmt.Errorf("notification cleanup: ent client is nil")
	}
	if resourceID <= 0 {
		return 0, fmt.Errorf("notification cleanup: invalid resource id %d", resourceID)
	}
	preds := notificationTargetPredicates(resourceType, resourceID)
	if len(preds) == 0 {
		return 0, fmt.Errorf("notification cleanup: unsupported resource type %q", resourceType)
	}
	sysCtx := tenantctx.WithSystemBypass(ctx)
	deleted, err := client.Notification.Delete().
		Where(notification.Or(preds...)).
		Exec(sysCtx)
	if err != nil {
		return 0, fmt.Errorf("notification cleanup %s/%d: %w", resourceType, resourceID, err)
	}
	return deleted, nil
}

// CleanupResourceNotificationsLogged 是 CleanupResourceNotifications 的 best-effort 包装：
// 失败只告警不阻断单据删除主链路（与附件级联清理同语义），返回清理行数。
func CleanupResourceNotificationsLogged(ctx context.Context, client *ent.Client, logger *zap.SugaredLogger, resourceType NotificationResourceType, resourceID int) int {
	if client == nil {
		return 0
	}
	deleted, err := CleanupResourceNotifications(ctx, client, resourceType, resourceID)
	if err != nil {
		if logger != nil {
			logger.Warnw("resource notification cleanup failed",
				"resource_type", string(resourceType), "resource_id", resourceID, "error", err)
		}
		return 0
	}
	if deleted > 0 && logger != nil {
		logger.Infow("resource notifications cleaned",
			"resource_type", string(resourceType), "resource_id", resourceID, "cleaned", deleted)
	}
	return deleted
}

// OrphanNotification 描述一条指向不存在（或已软删）单据的通知。
type OrphanNotification struct {
	ID           int
	TenantID     int
	ActionURL    string
	ResourceType NotificationResourceType
	ResourceID   int
}

var (
	orphanTicketPathRe = regexp.MustCompile(`^/tickets/(\d+)(?:[/?].*)?$`)
	orphanChangePathRe = regexp.MustCompile(`^/changes/(\d+)(?:[/?].*)?$`)
	orphanSRPathRe     = regexp.MustCompile(`^/service-requests/(\d+)(?:[/?].*)?$`)
	orphanMSPTicketRe  = regexp.MustCompile(`(?:^|[?&])ticket=(\d+)(?:&|$)`)
)

// ParseNotificationTarget 解析 action_url 指向的单据；无法识别时返回 ok=false。
func ParseNotificationTarget(actionURL string) (resourceType NotificationResourceType, resourceID int, ok bool) {
	if actionURL == "" {
		return "", 0, false
	}
	if m := orphanTicketPathRe.FindStringSubmatch(actionURL); m != nil {
		return NotificationResourceTicket, atoiOrZero(m[1]), true
	}
	if m := orphanChangePathRe.FindStringSubmatch(actionURL); m != nil {
		return NotificationResourceChange, atoiOrZero(m[1]), true
	}
	if m := orphanSRPathRe.FindStringSubmatch(actionURL); m != nil {
		return NotificationResourceServiceRequest, atoiOrZero(m[1]), true
	}
	if m := orphanMSPTicketRe.FindStringSubmatch(actionURL); m != nil {
		return NotificationResourceTicket, atoiOrZero(m[1]), true
	}
	return "", 0, false
}

func atoiOrZero(s string) int {
	n := 0
	for _, ch := range s {
		if ch < '0' || ch > '9' {
			return 0
		}
		n = n*10 + int(ch-'0')
	}
	return n
}

// ScanOrphanResourceNotifications 扫描 action_url 指向不存在（或已软删）单据的通知。
// tenantID > 0 时仅扫描该租户；limit > 0 时限制扫描行数（按 id 升序）。
// 只读，不修改数据。
func ScanOrphanResourceNotifications(ctx context.Context, client *ent.Client, tenantID int, limit int) ([]OrphanNotification, error) {
	if client == nil {
		return nil, fmt.Errorf("orphan notification scan: ent client is nil")
	}
	sysCtx := tenantctx.WithSystemBypass(ctx)
	query := client.Notification.Query().
		Where(notification.Or(
			notification.ActionURLHasPrefix("/tickets/"),
			notification.ActionURLHasPrefix("/changes/"),
			notification.ActionURLHasPrefix("/service-requests/"),
			notification.ActionURLContains("ticket="),
		)).
		Order(ent.Asc(notification.FieldID))
	if tenantID > 0 {
		query = query.Where(notification.TenantIDEQ(tenantID))
	}
	if limit > 0 {
		query = query.Limit(limit)
	}
	rows, err := query.All(sysCtx)
	if err != nil {
		return nil, fmt.Errorf("orphan notification scan: %w", err)
	}
	if len(rows) == 0 {
		return nil, nil
	}

	type candidate struct {
		row          *ent.Notification
		resourceType NotificationResourceType
		resourceID   int
	}
	candidates := make([]candidate, 0, len(rows))
	ticketIDs := map[int]struct{}{}
	changeIDs := map[int]struct{}{}
	srIDs := map[int]struct{}{}
	for _, row := range rows {
		resourceType, resourceID, ok := ParseNotificationTarget(row.ActionURL)
		if !ok || resourceID <= 0 {
			continue
		}
		candidates = append(candidates, candidate{row: row, resourceType: resourceType, resourceID: resourceID})
		switch resourceType {
		case NotificationResourceTicket:
			ticketIDs[resourceID] = struct{}{}
		case NotificationResourceChange:
			changeIDs[resourceID] = struct{}{}
		case NotificationResourceServiceRequest:
			srIDs[resourceID] = struct{}{}
		}
	}
	if len(candidates) == 0 {
		return nil, nil
	}

	liveTickets, err := liveTicketIDs(sysCtx, client, ticketIDs)
	if err != nil {
		return nil, err
	}
	liveChanges, err := liveChangeIDs(sysCtx, client, changeIDs)
	if err != nil {
		return nil, err
	}
	liveSRs, err := liveServiceRequestIDs(sysCtx, client, srIDs)
	if err != nil {
		return nil, err
	}

	orphans := make([]OrphanNotification, 0)
	for _, c := range candidates {
		alive := false
		switch c.resourceType {
		case NotificationResourceTicket:
			_, alive = liveTickets[c.resourceID]
		case NotificationResourceChange:
			_, alive = liveChanges[c.resourceID]
		case NotificationResourceServiceRequest:
			_, alive = liveSRs[c.resourceID]
		}
		if alive {
			continue
		}
		orphans = append(orphans, OrphanNotification{
			ID:           c.row.ID,
			TenantID:     c.row.TenantID,
			ActionURL:    c.row.ActionURL,
			ResourceType: c.resourceType,
			ResourceID:   c.resourceID,
		})
	}
	return orphans, nil
}

// CleanupOrphanResourceNotifications 扫描并删除孤儿通知，返回被删除的通知明细。
func CleanupOrphanResourceNotifications(ctx context.Context, client *ent.Client, tenantID int, limit int) ([]OrphanNotification, error) {
	orphans, err := ScanOrphanResourceNotifications(ctx, client, tenantID, limit)
	if err != nil || len(orphans) == 0 {
		return orphans, err
	}
	sysCtx := tenantctx.WithSystemBypass(ctx)
	ids := make([]int, 0, len(orphans))
	for _, o := range orphans {
		ids = append(ids, o.ID)
	}
	const chunk = 500
	for start := 0; start < len(ids); start += chunk {
		end := start + chunk
		if end > len(ids) {
			end = len(ids)
		}
		if _, err := client.Notification.Delete().
			Where(notification.IDIn(ids[start:end]...)).
			Exec(sysCtx); err != nil {
			return nil, fmt.Errorf("orphan notification cleanup: %w", err)
		}
	}
	return orphans, nil
}

func liveTicketIDs(ctx context.Context, client *ent.Client, ids map[int]struct{}) (map[int]struct{}, error) {
	live := map[int]struct{}{}
	if len(ids) == 0 {
		return live, nil
	}
	list := make([]int, 0, len(ids))
	for id := range ids {
		list = append(list, id)
	}
	rows, err := client.Ticket.Query().
		Where(ticket.IDIn(list...), ticket.DeletedAtIsNil()).
		Select(ticket.FieldID).
		All(ctx)
	if err != nil {
		return nil, fmt.Errorf("orphan notification scan: query tickets: %w", err)
	}
	for _, row := range rows {
		live[row.ID] = struct{}{}
	}
	return live, nil
}

func liveChangeIDs(ctx context.Context, client *ent.Client, ids map[int]struct{}) (map[int]struct{}, error) {
	live := map[int]struct{}{}
	if len(ids) == 0 {
		return live, nil
	}
	list := make([]int, 0, len(ids))
	for id := range ids {
		list = append(list, id)
	}
	rows, err := client.Change.Query().
		Where(change.IDIn(list...)).
		Select(change.FieldID).
		All(ctx)
	if err != nil {
		return nil, fmt.Errorf("orphan notification scan: query changes: %w", err)
	}
	for _, row := range rows {
		live[row.ID] = struct{}{}
	}
	return live, nil
}

func liveServiceRequestIDs(ctx context.Context, client *ent.Client, ids map[int]struct{}) (map[int]struct{}, error) {
	live := map[int]struct{}{}
	if len(ids) == 0 {
		return live, nil
	}
	list := make([]int, 0, len(ids))
	for id := range ids {
		list = append(list, id)
	}
	rows, err := client.ServiceRequest.Query().
		Where(servicerequest.IDIn(list...), servicerequest.DeletedAtIsNil()).
		Select(servicerequest.FieldID).
		All(ctx)
	if err != nil {
		return nil, fmt.Errorf("orphan notification scan: query service requests: %w", err)
	}
	for _, row := range rows {
		live[row.ID] = struct{}{}
	}
	return live, nil
}
