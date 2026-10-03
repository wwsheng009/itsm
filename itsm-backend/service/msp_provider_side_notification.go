package service

// MSP 通知双投递（canon §7.2 通知行 / A12）：
//
// 托管工单的事件通知在客户侧（requester / 客户 assignee / 被 @ 用户）之外，
// 再向 **provider 租户** 投递一份，收件人 = 托管处理人（managed_by_user_id）
// + 工单 assignee（若属 provider 租户且 active）+ provider 租户 active 的 provider_admin。
//
// 边界与约束（与 canon §3 子系统挂接规范一致）：
//   - 通知行归属 provider 租户（notification / ticket_notification.tenant_id = provider），
//     不把 provider 配置写进客户租户，也不把客户数据落成 provider 数据；
//   - 仅当工单带 MSP 快照（is_managed_by_msp=true 且 msp_provider_id>0）、
//     provider 租户存在、active 且类型为 msp_provider 时生效，其余 fail-closed 跳过；
//   - provider 侧投递不阻塞主流程：非事务路径失败只记日志；Tx 变体在调用方事务内
//     写行（与主表同生同死），失败返回错误由调用方决定回滚语义；
//   - 直接写 in_app 行（同步），与 outbox 开关无关；邮件/短信渠道不在双投递内
//     （provider 侧目前仅站内消息，后续如需扩展按 D3 模板租户化另行立项）。

import (
	"context"
	"fmt"
	"time"

	"itsm-backend/ent"
	"itsm-backend/ent/user"
	"itsm-backend/pkg/tenantmode"
)

// mspWorkbenchActionURL provider 侧通知的深链（工作台入口）。
const mspWorkbenchActionURL = "/msp/workbench"

// mspProviderSideRecipients 计算 provider 侧收件人（已去重、排除 actor）。
// 仅返回 provider 租户内 active 的用户；managed_by_user_id / assignee 必须
// 确实属于 provider 租户（防脏数据跨租户投递）。
func mspProviderSideRecipients(ctx context.Context, uc *ent.UserClient, ticket *ent.Ticket, excludeUserID int) []int {
	out := make([]int, 0, 4)
	seen := map[int]bool{}
	add := func(id int) {
		if id <= 0 || id == excludeUserID || seen[id] {
			return
		}
		seen[id] = true
		out = append(out, id)
	}

	// ① 托管处理人 / 工单 assignee（须属 provider 租户且 active）。
	for _, cand := range []int{ticket.ManagedByUserID, ticket.AssigneeID} {
		if cand <= 0 {
			continue
		}
		u, err := uc.Query().
			Where(user.IDEQ(cand), user.TenantIDEQ(ticket.MspProviderID)).
			Only(ctx)
		if err != nil || u == nil || !u.Active {
			continue
		}
		add(cand)
	}

	// ② provider 租户 active 管理员（canon §7.2：被指派人 + provider 管理员）。
	admins, err := uc.Query().
		Where(
			user.TenantIDEQ(ticket.MspProviderID),
			user.ActiveEQ(true),
			user.MspRoleEQ(user.MspRoleProviderAdmin),
		).
		All(ctx)
	if err == nil {
		for _, a := range admins {
			add(a.ID)
		}
	}
	return out
}

// providerTenantDeliverable 校验 provider 租户可投递性（存在 / active / 类型正确）。
func providerTenantDeliverable(pt *ent.Tenant) bool {
	if pt == nil {
		return false
	}
	return pt.Status == "active" && tenantmode.IsMSPProviderTenantType(string(pt.Type))
}

// notifyMSPProviderSide 非事务路径：向 provider 租户直写 in_app 通知行（best-effort）。
func (s *TicketNotificationService) notifyMSPProviderSide(
	ctx context.Context,
	ticket *ent.Ticket,
	eventType, content string,
	excludeUserID int,
) {
	if s == nil || s.client == nil || ticket == nil || !ticket.IsManagedByMsp || ticket.MspProviderID <= 0 {
		return
	}
	pt, err := s.client.Tenant.Get(ctx, ticket.MspProviderID)
	if err != nil || !providerTenantDeliverable(pt) {
		s.logger.Warnw("msp dual delivery skipped: provider tenant unavailable",
			"ticket_id", ticket.ID, "provider_tenant_id", ticket.MspProviderID, "error", err)
		return
	}
	recipients := mspProviderSideRecipients(ctx, s.client.User, ticket, excludeUserID)
	if len(recipients) == 0 {
		return
	}
	now := time.Now()
	for _, uid := range recipients {
		if _, err := s.client.TicketNotification.Create().
			SetTicketID(ticket.ID).SetUserID(uid).SetType(eventType).SetChannel("in_app").
			SetContent(content).SetTenantID(pt.ID).SetStatus("sent").SetNillableSentAt(&now).
			Save(ctx); err != nil {
			s.logger.Warnw("msp dual delivery: create ticket notification failed",
				"ticket_id", ticket.ID, "user_id", uid, "error", err)
			continue
		}
		if _, err := s.client.Notification.Create().
			SetTitle(eventType).SetMessage(content).SetType("info").
			SetUserID(uid).SetTenantID(pt.ID).
			SetActionURL(mspWorkbenchActionURL).SetActionText("打开工作台").
			Save(ctx); err != nil {
			s.logger.Warnw("msp dual delivery: create notification failed",
				"ticket_id", ticket.ID, "user_id", uid, "error", err)
		}
	}
}

// notifyMSPProviderSideTx 事务路径：在调用方事务内写 provider 侧通知行（与主表同生同死）。
func (s *TicketNotificationService) notifyMSPProviderSideTx(
	ctx context.Context,
	tx *ent.Tx,
	ticket *ent.Ticket,
	eventType, content string,
	excludeUserID int,
) error {
	if s == nil || tx == nil || ticket == nil || !ticket.IsManagedByMsp || ticket.MspProviderID <= 0 {
		return nil
	}
	pt, err := tx.Tenant.Get(ctx, ticket.MspProviderID)
	if err != nil || !providerTenantDeliverable(pt) {
		s.logger.Warnw("msp dual delivery(tx) skipped: provider tenant unavailable",
			"ticket_id", ticket.ID, "provider_tenant_id", ticket.MspProviderID, "error", err)
		return nil
	}
	recipients := mspProviderSideRecipients(ctx, tx.User, ticket, excludeUserID)
	if len(recipients) == 0 {
		return nil
	}
	now := time.Now()
	for _, uid := range recipients {
		if _, err := tx.TicketNotification.Create().
			SetTicketID(ticket.ID).SetUserID(uid).SetType(eventType).SetChannel("in_app").
			SetContent(content).SetTenantID(pt.ID).SetStatus("sent").SetNillableSentAt(&now).
			Save(ctx); err != nil {
			return fmt.Errorf("msp dual delivery(tx): create ticket notification: %w", err)
		}
		if _, err := tx.Notification.Create().
			SetTitle(eventType).SetMessage(content).SetType("info").
			SetUserID(uid).SetTenantID(pt.ID).
			SetActionURL(mspWorkbenchActionURL).SetActionText("打开工作台").
			Save(ctx); err != nil {
			return fmt.Errorf("msp dual delivery(tx): create notification: %w", err)
		}
	}
	return nil
}
