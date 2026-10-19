// 会话历史回放的端到端断言（2026-10-19 修复）：
// 通过真实的 ChatStream 链路（真实 ent/SQLite + 真实 RAG + 捕获型 LLM 替身）验证
// 「第二轮请求能带上第一轮 user/assistant 消息」，以及「同一会话的消息序列在轮次之间
// 是稳定前缀（append-only）——这是 OpenAI/DeepSeek 等 provider 前缀缓存命中的前提」。
package botintegration

import (
	"context"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"

	"itsm-backend/handlers/ai"
	"itsm-backend/service"
)

// capturingChatProvider 记录每次 LLM 调用实际收到的消息序列；不实现 ToolCallingStreamProvider，
// 让聊天链路走「无工具」的纯 RAG 分支（本用例只关心历史回放与消息布局）。
type capturingChatProvider struct {
	mu    sync.Mutex
	calls [][]service.LLMMessage
	reply string
}

func (p *capturingChatProvider) record(messages []service.LLMMessage) {
	copied := make([]service.LLMMessage, len(messages))
	copy(copied, messages)
	p.mu.Lock()
	p.calls = append(p.calls, copied)
	p.mu.Unlock()
}

func (p *capturingChatProvider) Chat(_ context.Context, _ string, messages []service.LLMMessage) (string, error) {
	p.record(messages)
	return p.reply, nil
}

func (p *capturingChatProvider) ChatStream(_ context.Context, _ string, messages []service.LLMMessage, callback func(string)) error {
	p.record(messages)
	if callback != nil {
		callback(p.reply)
	}
	return nil
}

func (p *capturingChatProvider) call(index int) []service.LLMMessage {
	p.mu.Lock()
	defer p.mu.Unlock()
	if index < 0 || index >= len(p.calls) {
		return nil
	}
	return append([]service.LLMMessage(nil), p.calls[index]...)
}

func TestChatStreamReplaysStableConversationHistory(t *testing.T) {
	ctx := context.Background()
	h := newB0Harness(t)

	rag := service.NewRAGService(h.client, nil, nil, zap.NewNop().Sugar(), service.RAGConfig{UseKeyword: true})
	svc := ai.NewService(ai.NewEntRepository(h.client), zap.NewNop().Sugar(), rag, h.registry, h.queue,
		nil, nil, nil, nil, nil, nil)
	svc.SetEntClient(h.client)

	provider := &capturingChatProvider{reply: "好的，我记住了：你叫张三。"}
	svc.SetLLMGateway(service.NewLLMGateway(provider, nil, nil, "capture"))

	chat := func(query string, convID int) (int, string) {
		t.Helper()
		outConvID, answer, _, err := svc.ChatStream(ctx, h.tenantID, h.userID, "super_admin", query, 5, convID,
			func([]map[string]any) {}, func(string) {}, nil, nil)
		require.NoError(t, err)
		return outConvID, answer
	}

	// ---- 第一轮：新会话（convID=0）只有 system + 当前问题 ----
	convID, answer := chat("我叫张三，请记住这个名字。", 0)
	require.NotZero(t, convID, "流结束后必须落库并回带会话 id")
	require.NotEmpty(t, answer)

	first := provider.call(0)
	require.Len(t, first, 2, "新会话首轮不得凭空带历史")
	assert.Equal(t, "system", first[0].Role)
	assert.Equal(t, "user", first[1].Role)

	// ---- 第二轮：携带 conversationId，必须回放上一轮 user/assistant ----
	convID2, _ := chat("我叫什么名字？", convID)
	require.Equal(t, convID, convID2, "已有会话必须原样延续")

	second := provider.call(1)
	require.Len(t, second, 4, "第二轮 = system + 上一轮 user/assistant + 当前问题")
	assert.Equal(t, "system", second[0].Role)
	assert.Equal(t, "user", second[1].Role)
	assert.Equal(t, "我叫张三，请记住这个名字。", second[1].Content)
	assert.Equal(t, "assistant", second[2].Role)
	assert.Equal(t, "好的，我记住了：你叫张三。", second[2].Content,
		"assistant 落库信封 {\"answer\":...} 必须解包为自然语言后回放")
	assert.Equal(t, "user", second[3].Role)
	assert.Contains(t, second[3].Content, "我叫什么名字？", "当前问题必须位于序列末尾")

	// ---- 第三轮：消息序列必须是第二轮请求的稳定前缀（缓存命中的前提） ----
	_, _ = chat("我姓什么？", convID)

	third := provider.call(2)
	require.Greater(t, len(third), len(second), "第三轮应在前缀后追加新的 user/assistant")
	for i := range second {
		assert.Equal(t, second[i], third[i],
			"第 %d 条消息前缀必须逐字节稳定（provider 前缀缓存前提）", i)
	}
}
