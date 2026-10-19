package assistantcontext

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// D1-01：内嵌助手注册表的加载、页面解析与注册表不变量（与前端 registry.ts 同口径）。

func TestRegistry_LoadsGeneratedPages(t *testing.T) {
	r := Load()
	require.NotNil(t, r)
	require.Positive(t, r.Len(), "生成物必须包含页面声明")
	assert.NotEmpty(t, r.GeneratedAt)
}

func TestRegistry_DeclarationForKnownPage(t *testing.T) {
	r := Load()

	decl := r.Declaration("ticket.detail")
	require.NotNil(t, decl)
	assert.Equal(t, "ticket.detail", decl.PageKey)
	assert.Equal(t, "/tickets/:id", decl.Pattern)
	assert.Equal(t, "s1-ticket-assistant", decl.Scene)
	assert.Equal(t, "ticket_detail", decl.Entrypoint)
	require.GreaterOrEqual(t, len(decl.Suggestions), 3)

	found := false
	for _, action := range decl.Suggestions {
		if action.ID == "triage_plan" {
			found = true
			assert.Equal(t, "task", action.Kind)
			assert.True(t, action.ReadOnly, "任务卡必须只读（D4 不变量）")
			assert.NotEmpty(t, action.Label)
			assert.NotEmpty(t, action.Prompt)
		}
	}
	assert.True(t, found, "ticket.detail 必须包含 triage_plan 建议")
}

func TestRegistry_UnknownPageReturnsNil(t *testing.T) {
	r := Load()
	assert.Nil(t, r.Declaration("no.such.page"))
	assert.Nil(t, r.Declaration(""))
}

// 注册表不变量：每条声明与建议的必填字段齐全；task 卡只读；tools 声明仅非空字符串。
func TestRegistry_DeclarationInvariants(t *testing.T) {
	r := Load()
	require.Positive(t, r.Len())

	for _, page := range r.pages {
		assert.NotEmpty(t, page.PageKey)
		assert.NotEmpty(t, page.Pattern)
		assert.NotEmpty(t, page.Title)
		assert.NotEmpty(t, page.Suggestions, "页面 %s 必须有建议", page.PageKey)

		ids := map[string]bool{}
		for _, action := range page.Suggestions {
			assert.NotEmpty(t, action.ID, page.PageKey)
			assert.False(t, ids[action.ID], "页面 %s 建议 id 必须唯一: %s", page.PageKey, action.ID)
			ids[action.ID] = true
			assert.Contains(t, []string{"task", "question"}, action.Kind, page.PageKey)
			assert.NotEmpty(t, action.Label, page.PageKey)
			assert.NotEmpty(t, action.Prompt, page.PageKey)
			if action.Kind == "task" {
				assert.True(t, action.ReadOnly, "任务卡必须只读（D4 不变量）: %s/%s", page.PageKey, action.ID)
			}
			for _, tool := range action.Tools {
				assert.NotEmpty(t, tool, "工具声明不得为空: %s/%s", page.PageKey, action.ID)
			}
		}
	}
}
