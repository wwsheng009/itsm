// B3-01 集成验收：入口上下文进入运行记录（entrypoint/target 落库）+
// 执行面按入口过滤（同一工具在 ticket_detail 可用、在 chat 被拒）。
package botintegration

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	ai "itsm-backend/handlers/ai"
	"itsm-backend/service/bot"
)

// TestB3Entrypoint_RunRecordPersistsTarget：run 记录必须保留入口与目标对象（审计可回溯
// "从哪个页面发起、针对哪个对象"）；未携带目标时不写 target 字段。
func TestB3Entrypoint_RunRecordPersistsTarget(t *testing.T) {
	h := newB2Harness(t)
	ctx := context.Background()
	store := bot.NewRunStore(h.client)
	require.NotNil(t, store)

	withTarget, err := store.StartRun(ctx, bot.StartRunInput{
		TenantID: h.tenantID, ConversationID: 0, Entrypoint: bot.EntrypointTicketDetail,
		TargetType: bot.TargetTypeTicket, TargetID: 4242,
	})
	require.NoError(t, err)
	assert.Equal(t, bot.EntrypointTicketDetail, withTarget.Entrypoint)
	assert.Equal(t, bot.TargetTypeTicket, withTarget.TargetType)
	assert.Equal(t, 4242, withTarget.TargetID)

	withoutTarget, err := store.StartRun(ctx, bot.StartRunInput{
		TenantID: h.tenantID, Entrypoint: bot.EntrypointChat,
	})
	require.NoError(t, err)
	assert.Equal(t, bot.EntrypointChat, withoutTarget.Entrypoint)
	assert.Equal(t, "", withoutTarget.TargetType)
	assert.Zero(t, withoutTarget.TargetID)
}

// TestB3Entrypoint_ExecutionFaceFiltering：执行面（Gate 2.5）按入口过滤——
// 模板只声明 ticket_detail 时，chat 入口对同一工具被拒（audit 留痕 entrypoint_denied）。
func TestB3Entrypoint_ExecutionFaceFiltering(t *testing.T) {
	h := newB2Harness(t)
	ctx := context.Background()

	tpl := h.createTemplate(t, "b3-entry", bot.StatusGA, bot.RiskActHigh, []string{bot.EntrypointTicketDetail}, map[string]string{
		"probe_list_tickets": bot.RiskRead,
	})
	h.svc.SetBotIDResolver(func(context.Context, int, int) int { return tpl.ID })

	// 入口内：放行（读工具直接执行）。
	ticketCtx := ai.WithScope(ctx, bot.Scope{Entrypoint: bot.EntrypointTicketDetail})
	_, _, err := h.svc.ExecuteTool(ticketCtx, h.userID, h.tenantID, "super_admin", "probe_list_tickets", nil)
	require.NoError(t, err)

	// 入口外（chat）：拒绝，原因码 entrypoint_denied。
	_, _, err = h.svc.ExecuteTool(ctx, h.userID, h.tenantID, "super_admin", "probe_list_tickets", nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), bot.ReasonEntrypointDenied)

	// 审计：两行可区分（一行 executed/passed，一行 denied/entrypoint_denied）。
	rows, err := h.client.ToolInvocation.Query().All(ctx)
	require.NoError(t, err)
	require.Len(t, rows, 2)
	reasons := map[string]string{}
	for _, row := range rows {
		reasons[row.PermissionCheck] = row.PermissionReason
	}
	assert.Equal(t, "", reasons["passed"])
	assert.Contains(t, reasons["denied"], bot.ReasonEntrypointDenied)
}
