package ai

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"

	"itsm-backend/service"
)

// B2-04 内部测试：ensureConversation 是新会话创建的唯一入口，
// 必须把「本次请求选择的 Bot」（ctx）写入 conversation.bot_id。
//
// 用嵌入接口的最小 repo 替身：只实现本测试触发的 CreateConversation，
// 其余方法由嵌入的 nil 接口满足编译（不会被调用）。

type bindingRepo struct {
	Repository
	created *Conversation
}

func (r *bindingRepo) CreateConversation(_ context.Context, c *Conversation) (*Conversation, error) {
	c.ID = 77
	r.created = c
	return c, nil
}

func newBindingService(repo Repository) *Service {
	tools := service.NewToolRegistry(nil, nil, nil, nil)
	return NewService(repo, zap.NewNop().Sugar(), nil, tools, nil, nil, nil, nil, nil, nil, nil)
}

func TestEnsureConversation_PersistsSelectedBot(t *testing.T) {
	repo := &bindingRepo{}
	svc := newBindingService(repo)

	// ① 选择器指定 Bot 33 → 新会话写入 33。
	ctx := WithBotID(context.Background(), 33)
	convID := svc.ensureConversation(ctx, 1, 9, 0, "查询当前告警")
	require.Equal(t, 77, convID)
	require.NotNil(t, repo.created)
	assert.Equal(t, 33, repo.created.BotID, "新会话必须绑定请求选择的 Bot")
	assert.Equal(t, 1, repo.created.TenantID)
	assert.Equal(t, 9, repo.created.UserID)
	assert.Equal(t, "查询当前告警", repo.created.Title, "新会话标题必须由首条 prompt 推导")

	// ② 未指定（0）→ 写 0 = 默认助手（兼容默认）。
	repo.created = nil
	convID = svc.ensureConversation(context.Background(), 1, 9, 0, "第二个会话")
	require.Equal(t, 77, convID)
	require.NotNil(t, repo.created)
	assert.Equal(t, 0, repo.created.BotID, "未选择 = 默认助手（bot_id=0）")

	// ③ 已有会话 → 原样返回，不再创建（不回溯改写历史会话归属）。
	repo.created = nil
	convID = svc.ensureConversation(WithBotID(context.Background(), 55), 1, 9, 123, "已有会话不重写标题")
	assert.Equal(t, 123, convID)
	assert.Nil(t, repo.created, "已有会话不得重复创建或改写归属")

	// ④ 上下文读取助手：WithBotID(0) 与缺省等价。
	assert.Equal(t, 0, BotIDFromContext(context.Background()))
	assert.Equal(t, 0, BotIDFromContext(WithBotID(context.Background(), 0)))
	assert.Equal(t, 55, BotIDFromContext(WithBotID(context.Background(), 55)))
}
