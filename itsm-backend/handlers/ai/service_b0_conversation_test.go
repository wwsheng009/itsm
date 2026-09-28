package ai_test

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"

	"itsm-backend/ent"
	"itsm-backend/handlers/ai"
	"itsm-backend/middleware"
	"itsm-backend/service"
)

// b0ConvRepo 在 rbacMockRepo 之上补「消息回填」与「待审批记录读写」，
// 用于 B0-03 会话归属与拒绝回填的行为断言（不依赖真实 DB）。
type b0ConvRepo struct {
	*rbacMockRepo
	messages []*ai.Message
	inv      *ai.ToolInvocation
}

func (m *b0ConvRepo) CreateMessage(_ context.Context, msg *ai.Message) (*ai.Message, error) {
	m.messages = append(m.messages, msg)
	return msg, nil
}

func (m *b0ConvRepo) GetMessages(_ context.Context, convID int) ([]*ai.Message, error) {
	out := make([]*ai.Message, 0, len(m.messages))
	for _, msg := range m.messages {
		if msg.ConversationID == convID {
			out = append(out, msg)
		}
	}
	return out, nil
}

func (m *b0ConvRepo) GetToolInvocation(_ context.Context, id, tenantID int) (*ai.ToolInvocation, error) {
	if m.inv == nil || m.inv.ID != id || m.inv.TenantID != tenantID {
		return nil, errors.New("tool invocation not found")
	}
	return m.inv, nil
}

func (m *b0ConvRepo) UpdateToolInvocation(_ context.Context, i *ai.ToolInvocation) (*ai.ToolInvocation, error) {
	m.inv = i
	return i, nil
}

// newB0ConvEnv 构造带硬编码权限模式的 Service（写工具 create_ticket 走审批，不触真实执行）。
func newB0ConvEnv(t *testing.T) (*ai.Service, *b0ConvRepo) {
	t.Helper()
	repo := &b0ConvRepo{rbacMockRepo: &rbacMockRepo{}}
	tools := service.NewToolRegistry(nil, nil, nil, nil)
	svc := ai.NewService(repo, zap.NewNop().Sugar(), nil, tools, nil, nil, nil, nil, nil, nil, nil)
	svc.SetEntClient(&ent.Client{})

	prevMode := middleware.PermissionConfig.Mode
	middleware.PermissionConfig.Mode = middleware.PermissionConfigModeHardcodeOnly
	middleware.InvalidateAllPermissionCaches()
	ai.ResetRBACFlagForTest()
	t.Cleanup(func() {
		middleware.PermissionConfig.Mode = prevMode
		middleware.InvalidateAllPermissionCaches()
		ai.ResetRBACFlagForTest()
	})
	return svc, repo
}

// B0-03：聊天路径调用携带 conversationId → 待审批记录落 conversation_id；
// 不带会话的既有调用行为不变（0 = 不写列）。
func TestB0_03_ConversationIDInjectedIntoInvocation(t *testing.T) {
	svc, repo := newB0ConvEnv(t)

	_, invID, err := svc.ExecuteToolWithConversation(context.Background(), 1, 10, "super_admin", "create_ticket",
		map[string]interface{}{"title": "带会话的调用"}, 4242)
	require.NoError(t, err)
	require.Greater(t, invID, 0)
	require.NotNil(t, repo.lastInvocation())
	assert.Equal(t, 4242, repo.lastInvocation().ConversationID, "会话归属必须随 pending 一次落库")

	// 既有入口（无会话）行为不变。
	_, _, err = svc.ExecuteTool(context.Background(), 1, 10, "super_admin", "create_ticket",
		map[string]interface{}{"title": "无会话的调用"})
	require.NoError(t, err)
	assert.Zero(t, repo.lastInvocation().ConversationID, "未携带会话时应保持零值（不入列）")
}

// B0-03：审批拒绝 → 结构化结论回填会话（模型可在后续轮次重新规划）。
func TestB0_03_RejectBackfillsConversation(t *testing.T) {
	svc, repo := newB0ConvEnv(t)
	repo.inv = &ai.ToolInvocation{
		ID:             7,
		TenantID:       10,
		ConversationID: 4242,
		ToolName:       "create_ticket",
		ApprovalState:  "pending",
		Status:         "pending",
	}

	state, err := svc.ApproveTool(context.Background(), 7, 10, 1, false, "风险过高：非计划变更窗口")
	require.NoError(t, err)
	assert.Equal(t, "rejected", state)

	msgs, err := repo.GetMessages(context.Background(), 4242)
	require.NoError(t, err)
	require.Len(t, msgs, 1, "拒绝必须回填一条结构化结论")
	assert.Equal(t, "assistant", msgs[0].Role)
	assert.Contains(t, msgs[0].Content, `"type":"tool_approval_decision"`)
	assert.Contains(t, msgs[0].Content, `"decision":"rejected"`)
	assert.Contains(t, msgs[0].Content, `"tool":"create_ticket"`)
	assert.Contains(t, msgs[0].Content, "非计划变更窗口", "拒绝原因需对模型可见")
}

// B0-03：无会话归属的记录拒绝时不产生回填（避免污染无关会话/空会话）。
func TestB0_03_RejectWithoutConversationSkipsBackfill(t *testing.T) {
	svc, repo := newB0ConvEnv(t)
	repo.inv = &ai.ToolInvocation{
		ID:            8,
		TenantID:      10,
		ToolName:      "create_ticket",
		ApprovalState: "pending",
		Status:        "pending",
	}

	state, err := svc.ApproveTool(context.Background(), 8, 10, 1, false, "无会话")
	require.NoError(t, err)
	assert.Equal(t, "rejected", state)

	msgs, err := repo.GetMessages(context.Background(), 0)
	require.NoError(t, err)
	assert.Empty(t, msgs, "无会话归属不得写入消息")
}
