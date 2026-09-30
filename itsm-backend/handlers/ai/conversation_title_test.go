package ai

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestDeriveConversationTitle(t *testing.T) {
	tests := []struct {
		name  string
		query string
		want  string
	}{
		{
			name:  "短中文 prompt 原样作为标题",
			query: "帮我查一下昨天的告警",
			want:  "帮我查一下昨天的告警",
		},
		{
			name:  "多行与连续空白折叠为单行",
			query: "  帮我看看\n\n  这个工单\t怎么处理？ ",
			want:  "帮我看看 这个工单 怎么处理？",
		},
		{
			name:  "全角空格同样折叠",
			query: "知识库里\u3000有没有相关方案？",
			want:  "知识库里 有没有相关方案？",
		},
		{
			name:  "空输入兜底默认标题",
			query: "",
			want:  "AI 会话",
		},
		{
			name:  "纯空白兜底默认标题",
			query: " \n\t\u3000 ",
			want:  "AI 会话",
		},
		{
			name:  "恰好达到上限不追加省略号",
			query: strings.Repeat("a", conversationTitleMaxRunes),
			want:  strings.Repeat("a", conversationTitleMaxRunes),
		},
		{
			name:  "超长中文按码点截断并追加省略号",
			query: strings.Repeat("很", conversationTitleMaxRunes+10),
			want:  strings.Repeat("很", conversationTitleMaxRunes) + "…",
		},
		{
			name:  "emoji 按码点截断不产生半个字符",
			query: strings.Repeat("🙂", conversationTitleMaxRunes+1),
			want:  strings.Repeat("🙂", conversationTitleMaxRunes) + "…",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, deriveConversationTitle(tt.query))
		})
	}
}

// 标题必须是单行：历史列表按行渲染，换行会破坏布局。
func TestDeriveConversationTitle_NeverContainsNewline(t *testing.T) {
	got := deriveConversationTitle("第一行\n第二行\r\n第三行")
	assert.NotContains(t, got, "\n")
	assert.NotContains(t, got, "\r")
	assert.Equal(t, "第一行 第二行 第三行", got)
}
