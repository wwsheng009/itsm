package ai_test

import (
	"context"
	"testing"

	ai "itsm-backend/handlers/ai"
	"itsm-backend/service/bot"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestResolveBotID_Precedence B2-04 归属解析优先级：
// ① 请求显式选择（ctx）→ ② 注入解析器 → ③ 会话已绑定（conversation.bot_id）→ ④ 0=默认助手。
func TestResolveBotID_Precedence(t *testing.T) {
	env := newBotPolicyEnv(t)
	ctx := context.Background()

	// 建两个模板 + 授权，其中绑定到会话的那个授权 create_ticket，另一个不授权。
	boundTpl := env.gaTemplateWithGrant(t, 1, []string{bot.EntrypointChat}, "create_ticket", bot.RiskActLow)
	otherTpl, err := env.admin.CreateTemplate(ctx, 1, bot.TemplateInput{
		Slug: "other-bot", Name: "另一助手", RiskLimit: bot.RiskActMedium,
		Status: bot.StatusGA, Entrypoints: []string{bot.EntrypointChat},
	})
	require.NoError(t, err)
	_, err = env.admin.UpsertGrant(ctx, 1, otherTpl.ID, bot.GrantInput{ToolName: "list_tickets", RiskLimit: bot.RiskRead})
	require.NoError(t, err)

	// 会话绑定到 boundTpl（模拟"新会话时选择器写入的归属"）。
	conv, err := env.repo.CreateConversation(ctx, &ai.Conversation{Title: "绑定会话", UserID: 9, TenantID: 1, BotID: boundTpl.ID})
	require.NoError(t, err)

	// ③ 无显式选择、无解析器 → 用会话归属（boundTpl 授权 create_ticket → 放行）。
	_, pendingID, err := env.svc.ExecuteToolWithConversation(ctx, 9, 1, "super_admin", "create_ticket",
		map[string]interface{}{"title": "会话归属放行"}, conv.ID)
	require.NoError(t, err)
	assert.Greater(t, pendingID, 0)

	// ① 显式选择优先：把请求选择设为 otherTpl（未授权 create_ticket）→ 拒绝。
	explicit := ai.WithBotID(ctx, otherTpl.ID)
	_, _, err = env.svc.ExecuteToolWithConversation(explicit, 9, 1, "super_admin", "create_ticket", nil, conv.ID)
	require.Error(t, err)
	assert.Contains(t, err.Error(), bot.ReasonToolNotGranted, "显式选择必须覆盖会话归属")

	// ② 注入解析器优先于会话归属（解析器指向 otherTpl）→ 拒绝。
	env.svc.SetBotIDResolver(func(context.Context, int, int) int { return otherTpl.ID })
	_, _, err = env.svc.ExecuteToolWithConversation(ctx, 9, 1, "super_admin", "create_ticket", nil, conv.ID)
	require.Error(t, err)
	assert.Contains(t, err.Error(), bot.ReasonToolNotGranted, "解析器必须覆盖会话归属")
	env.svc.SetBotIDResolver(nil)

	// ④ 无会话、无选择 → 0 → 默认助手（本租户未配置默认助手授权 → 兼容默认 → 放行）。
	_, pendingID, err = env.svc.ExecuteToolWithConversation(ctx, 9, 1, "super_admin", "create_ticket",
		map[string]interface{}{"title": "默认助手"}, 0)
	require.NoError(t, err)
	assert.Greater(t, pendingID, 0)
}
