package ai

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

func historyUserMessage(id int, content string) *Message {
	return &Message{ID: id, Role: "user", Content: content}
}

func historyAssistantMessage(id int, content string) *Message {
	return &Message{ID: id, Role: "assistant", Content: content}
}

// TestBuildChatHistory_ReplaysPairsAndUnwrapsAssistantEnvelope 锁定核心行为：
// user/assistant 原文按序回放，assistant 的 {"answer":...} 信封解包为自然语言。
func TestBuildChatHistory_ReplaysPairsAndUnwrapsAssistantEnvelope(t *testing.T) {
	stored := []*Message{
		historyUserMessage(1, "我叫张三，请记住这个名字。"),
		historyAssistantMessage(2, `{"answer":"好的，我记住了：你叫张三。","sources":[{"title":"kb"}]}`),
	}

	got := buildChatHistory(stored)

	require.Len(t, got, 2)
	assert.Equal(t, "user", got[0].Role)
	assert.Equal(t, "我叫张三，请记住这个名字。", got[0].Content)
	assert.Equal(t, "assistant", got[1].Role)
	assert.Equal(t, "好的，我记住了：你叫张三。", got[1].Content,
		"assistant 落库信封必须解包成 answer，不能把 JSON 噪音回放给模型")
}

// TestBuildChatHistory_SkipsStructuredNonAnswers 结构化载荷（审批回执、legacy 检索数组、
// system 角色）不是对话内容，必须跳过，避免把内部结构当作模型历史。
func TestBuildChatHistory_SkipsStructuredNonAnswers(t *testing.T) {
	stored := []*Message{
		historyUserMessage(1, "同意该操作"),
		historyAssistantMessage(2, `{"type":"tool_approval_decision","decision":"approved"}`),
		{ID: 3, Role: "system", Content: "internal note"},
		historyUserMessage(4, "继续"),
		historyAssistantMessage(5, `[{"title":"kb","snippet":"legacy chat items"}]`),
	}

	got := buildChatHistory(stored)

	require.Len(t, got, 2, "只保留有自然语言内容的两条 user 消息")
	assert.Equal(t, "同意该操作", got[0].Content)
	assert.Equal(t, "继续", got[1].Content)
}

// TestBuildChatHistory_KeepsPlainTextStartingWithBrace 以 { 开头但不是合法 JSON 的普通文本按原文回放。
func TestBuildChatHistory_KeepsPlainTextStartingWithBrace(t *testing.T) {
	stored := []*Message{
		historyUserMessage(1, "看下这个"),
		historyAssistantMessage(2, `{示例：这不是合法 JSON`),
	}

	got := buildChatHistory(stored)

	require.Len(t, got, 2)
	assert.Equal(t, `{示例：这不是合法 JSON`, got[1].Content)
}

// TestBuildChatHistory_DeterministicRebuilds 同一批 DB 行多次重建必须逐字节一致——
// provider 前缀缓存依赖这个性质（每次请求重新读库拼装）。
func TestBuildChatHistory_DeterministicRebuilds(t *testing.T) {
	stored := []*Message{
		historyUserMessage(1, "第一问"),
		historyAssistantMessage(2, `{"answer":"第一答"}`),
		historyUserMessage(3, "第二问"),
		historyAssistantMessage(4, `{"answer":"第二答","sources":null}`),
	}

	first := buildChatHistory(stored)
	second := buildChatHistory(stored)

	assert.Equal(t, first, second)
}

// TestBuildChatHistory_MessageWatermarkDropsBlockToLowWatermark 条数超过高水位后一次性回收到
// 低水位（而不是每轮滑窗一格），且裁剪后必须以 user 消息开头。
func TestBuildChatHistory_MessageWatermarkDropsBlockToLowWatermark(t *testing.T) {
	stored := make([]*Message, 0, 30)
	for i := 0; i < 30; i++ {
		if i%2 == 0 {
			stored = append(stored, historyUserMessage(i+1, "u"+strconvItoa(i)))
		} else {
			stored = append(stored, historyAssistantMessage(i+1, `{"answer":"a`+strconvItoa(i)+`"}`))
		}
	}

	got := buildChatHistory(stored)

	require.Len(t, got, chatHistoryKeepMessages, "30 条 > 高水位 24 → 回收到低水位 16")
	assert.Equal(t, "user", got[0].Role, "裁剪边界后的历史必须以 user 提问开头")
	assert.Equal(t, "u14", got[0].Content, "保留最新的 16 条（索引 14..29）")
	assert.Equal(t, "assistant", got[len(got)-1].Role, "最后一条保留为最新消息")
	assert.Equal(t, "a29", got[len(got)-1].Content)
}

// TestBuildChatHistory_MessageWatermarkSkipsOrphanAssistant 当低水位边界正好落在 assistant 上时，
// 孤儿回复（其提问已被裁掉）必须继续丢弃，历史以 user 开头。
func TestBuildChatHistory_MessageWatermarkSkipsOrphanAssistant(t *testing.T) {
	stored := make([]*Message, 0, 25)
	for i := 0; i < 25; i++ {
		if i%2 == 0 {
			stored = append(stored, historyUserMessage(i+1, "u"+strconvItoa(i)))
		} else {
			stored = append(stored, historyAssistantMessage(i+1, `{"answer":"a`+strconvItoa(i)+`"}`))
		}
	}

	got := buildChatHistory(stored)

	// 25 条 → 保留最后 16 条（索引 9..24，索引 9 是 assistant）→ 丢孤儿后 15 条（索引 10..24）。
	assert.Len(t, got, 15)
	assert.Equal(t, "user", got[0].Role)
	assert.Equal(t, "u10", got[0].Content)
}

// TestBuildChatHistory_CharWatermarkKeepsNewestUnderBudget 字符超预算时从最新往回保留到低水位，
// 且总量不超过低水位。
func TestBuildChatHistory_CharWatermarkKeepsNewestUnderBudget(t *testing.T) {
	stored := make([]*Message, 0, 20)
	for i := 0; i < 20; i++ {
		content := strings.Repeat("字", 600)
		if i%2 == 0 {
			stored = append(stored, historyUserMessage(i+1, content))
		} else {
			stored = append(stored, historyAssistantMessage(i+1, `{"answer":"`+content+`"}`))
		}
	}

	got := buildChatHistory(stored)

	total := 0
	for _, m := range got {
		total += len([]rune(m.Content))
	}
	assert.LessOrEqual(t, total, chatHistoryKeepChars, "回收到低水位后总量不得超过低水位")
	assert.NotEmpty(t, got)
	assert.Less(t, len(got), 20, "字符超预算必须触发回收")
	assert.Equal(t, "user", got[0].Role)
	assert.True(t, strings.HasSuffix(got[len(got)-1].Content, strings.Repeat("字", 600)),
		"最新一条消息必须保留（尾部语义）")
}

// TestBuildChatHistory_TruncatesOversizedMessageDeterministically 单条超长消息按 rune 截断，
// 多次重建结果一致（缓存稳定性前提）。
func TestBuildChatHistory_TruncatesOversizedMessageDeterministically(t *testing.T) {
	huge := strings.Repeat("超", chatHistoryMaxMessageChars+500)
	stored := []*Message{historyUserMessage(1, huge)}

	first := buildChatHistory(stored)
	second := buildChatHistory(stored)

	require.Len(t, first, 1)
	assert.Equal(t, first, second)
	runes := []rune(first[0].Content)
	assert.Len(t, runes, chatHistoryMaxMessageChars+1, "截断到上限并追加省略号")
	assert.Equal(t, '…', runes[len(runes)-1])
}

// historyRepoStub 只实现本组用例需要的方法；嵌入 Repository 接口让其余方法在未调用时保持零值。
type historyRepoStub struct {
	Repository
	conv *Conversation
	msgs []*Message
}

func (s *historyRepoStub) GetConversation(_ context.Context, id, tenantID int) (*Conversation, error) {
	if s.conv == nil || s.conv.ID != id || s.conv.TenantID != tenantID {
		return nil, errors.New("conversation not found")
	}
	return s.conv, nil
}

func (s *historyRepoStub) GetMessages(_ context.Context, conversationID int) ([]*Message, error) {
	if s.conv == nil || conversationID != s.conv.ID {
		return nil, errors.New("conversation not found")
	}
	return s.msgs, nil
}

// TestLoadChatHistory_OwnershipFailClosed 跨用户/跨租户的 conversationId 一律不加载历史。
func TestLoadChatHistory_OwnershipFailClosed(t *testing.T) {
	repo := &historyRepoStub{
		conv: &Conversation{ID: 9, TenantID: 1, UserID: 42},
		msgs: []*Message{
			historyUserMessage(1, "我叫张三"),
			historyAssistantMessage(2, `{"answer":"记住了"}`),
		},
	}
	svc := &Service{repo: repo, logger: zap.NewNop().Sugar()}
	ctx := context.Background()

	assert.Nil(t, svc.loadChatHistory(ctx, 1, 43, 9), "同租户其他用户的历史不得加载")
	assert.Nil(t, svc.loadChatHistory(ctx, 2, 42, 9), "跨租户的历史不得加载")
	assert.Nil(t, svc.loadChatHistory(ctx, 1, 42, 0), "无会话 id 时不得查询历史")

	got := svc.loadChatHistory(ctx, 1, 42, 9)
	require.Len(t, got, 2)
	assert.Equal(t, "我叫张三", got[0].Content)
	assert.Equal(t, "记住了", got[1].Content)
}

// strconvItoa 是 test-only 小工具：避免为了拼消息内容引入 strconv 依赖噪音。
func strconvItoa(v int) string {
	if v == 0 {
		return "0"
	}
	var buf [20]byte
	pos := len(buf)
	for v > 0 {
		pos--
		buf[pos] = byte('0' + v%10)
		v /= 10
	}
	return string(buf[pos:])
}
