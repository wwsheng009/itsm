// B1-06：执行后回读校验（verify_state / verify_note）。
//
// 目的：写工具执行后**回读目标对象**，确认副作用真的落地（阶段一报告 §3.4：执行后无 verify）。
// 未装配校验器或工具无对应校验规则时落 `skipped`，不阻塞终态。
package service

import (
	"context"
	"fmt"
	"strings"

	"itsm-backend/dto"
	ticketrepo "itsm-backend/repository/ticket"
)

// 校验状态（与 tool_invocations.verify_state 口径一致）。
const (
	VerifyStateVerified = "verified"
	VerifyStateFailed   = "failed"
	VerifyStateSkipped  = "skipped"
)

// ToolVerifier 在写工具执行成功后回读目标对象，返回 (state, note)。
type ToolVerifier interface {
	Verify(ctx context.Context, tenantID int, toolName string, args map[string]interface{}, result interface{}) (string, string)
}

// ticketGetter 只需「按 ID 读工单」能力（*TicketService 天然满足），便于单测替身。
type ticketGetter interface {
	GetTicket(ctx context.Context, id int, tenantID int) (*ticketrepo.Ticket, error)
}

// TicketWriteVerifier 校验工单类写工具的副作用（B1-06）：
//   - update_ticket：回读工单，比对 status / assignee_id 是否与请求一致；
//   - create_ticket：执行结果已含新建工单（ID/编号），有 ID 即视为已验证。
type TicketWriteVerifier struct{ Tickets ticketGetter }

// NewTicketWriteVerifier 构造校验器；Tickets 为 nil 时所有校验落 skipped。
func NewTicketWriteVerifier(tickets ticketGetter) *TicketWriteVerifier {
	return &TicketWriteVerifier{Tickets: tickets}
}

// Verify 实现 ToolVerifier。
func (v *TicketWriteVerifier) Verify(ctx context.Context, tenantID int, toolName string, args map[string]interface{}, result interface{}) (string, string) {
	if v == nil || v.Tickets == nil {
		return VerifyStateSkipped, ""
	}
	switch toolName {
	case "update_ticket":
		id := argInt(args, "ticket_id")
		if id <= 0 {
			return VerifyStateSkipped, ""
		}
		t, err := v.Tickets.GetTicket(ctx, id, tenantID)
		if err != nil {
			return VerifyStateFailed, "回读工单失败：" + err.Error()
		}
		if t == nil {
			return VerifyStateFailed, fmt.Sprintf("工单 %d 回读为空", id)
		}
		if want, ok := args["status"].(string); ok && strings.TrimSpace(want) != "" && string(t.Status) != want {
			return VerifyStateFailed, fmt.Sprintf("状态未生效：期望 %s，实际 %s", want, t.Status)
		}
		if want := argInt(args, "assignee_id"); want > 0 && (t.AssigneeID == nil || *t.AssigneeID != want) {
			return VerifyStateFailed, fmt.Sprintf("处理人未生效：期望 %d", want)
		}
		return VerifyStateVerified, fmt.Sprintf("ticket=%d status=%s", t.ID, t.Status)
	case "create_ticket":
		if id := resultTicketID(result); id > 0 {
			return VerifyStateVerified, fmt.Sprintf("ticket=%d 已创建", id)
		}
		return VerifyStateFailed, "创建结果缺少工单 ID"
	default:
		return VerifyStateSkipped, ""
	}
}

func argInt(args map[string]interface{}, key string) int {
	if args == nil {
		return 0
	}
	switch v := args[key].(type) {
	case float64:
		return int(v)
	case int:
		return v
	case int64:
		return int(v)
	default:
		return 0
	}
}

// resultTicketID 从执行结果中提取工单 ID（支持 DTO 与领域模型两种形态）。
func resultTicketID(result interface{}) int {
	switch r := result.(type) {
	case *ticketrepo.Ticket:
		if r != nil {
			return r.ID
		}
	case *dto.TicketResponse:
		if r != nil {
			return r.ID
		}
	case map[string]interface{}:
		for _, key := range []string{"id", "ID", "ticketId", "ticket_id"} {
			if id := argInt(r, key); id > 0 {
				return id
			}
		}
	}
	return 0
}
