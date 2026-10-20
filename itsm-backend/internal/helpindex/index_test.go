package helpindex

import (
	"sort"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// H0-03 帮助索引单测：基于生成物（docs/help 真源）锁定检索/覆盖/gaps 口径。

func TestLoad_IndexesGeneratedArtifacts(t *testing.T) {
	idx := Load()
	require.NotNil(t, idx)
	assert.NotEmpty(t, idx.GeneratedAt)
	require.NotEmpty(t, idx.docs, "help-index.json 应包含文档")

	coverage, totals := idx.Coverage()
	assert.Equal(t, 1.0, coverage, "当前种子文档应 100% 覆盖页面注册表")
	assert.Equal(t, 22, totals["pages"])
	assert.Equal(t, 0, totals["missing"])
	assert.Equal(t, 0, totals["stale"])
	assert.Equal(t, 0, totals["orphan"])
}

func TestSearch_ByKeyword(t *testing.T) {
	idx := Load()

	results := idx.Search("SLA", "", 5)
	require.NotEmpty(t, results)
	found := false
	for _, r := range results {
		if r.Path == "docs/help/sla/monitor.md" {
			found = true
		}
	}
	assert.True(t, found, "SLA 检索应命中 sla/monitor.md，实际 %+v", results)
	assert.NotEmpty(t, results[0].Snippet)

	// 多词查询：两词均命中的文档应进入结果（评分同级时按路径排序，这里只锁定包含关系）。
	multi := idx.Search("工单 分诊", "", 5)
	require.NotEmpty(t, multi)
	paths := make([]string, 0, len(multi))
	for _, r := range multi {
		paths = append(paths, r.Path)
	}
	assert.Contains(t, paths, "docs/help/tickets/detail.md")

	// 标题命中查询：标题词权重更高，应为第一条命中。
	unique := idx.Search("工单详情", "", 5)
	require.NotEmpty(t, unique)
	assert.Equal(t, "docs/help/tickets/detail.md", unique[0].Path)

	// 中文无空格查询（2-gram 退化匹配）。
	cjk := idx.Search("影响分析计划", "", 5)
	require.NotEmpty(t, cjk)

	// 空查询与无命中（ASCII 词不得因 2-gram 回退误命中）。
	assert.Empty(t, idx.Search("   ", "", 5))
	assert.Empty(t, idx.Search("zzz-nonexistent-xyz", "", 5))
}

func TestSearch_PageKeyFilterAndLimit(t *testing.T) {
	idx := Load()

	// pageKey 过滤：仅返回该页面的文档。
	filtered := idx.Search("影响", "ci.detail", 10)
	require.NotEmpty(t, filtered)
	for _, r := range filtered {
		assert.Contains(t, r.PageKeys, "ci.detail")
	}

	// 未知页面键 → 空结果（面板自行降级）。
	assert.Empty(t, idx.Search("影响", "no.such.page", 10))

	// limit 夹取：0 → 缺省 5；超上限 → 20。
	assert.LessOrEqual(t, len(idx.Search("帮助", "", 0)), 5)
	assert.LessOrEqual(t, len(idx.Search("帮助", "", 100)), 20)
}

func TestDocsForPage_StatusAndContent(t *testing.T) {
	idx := Load()

	info, docs := idx.DocsForPage("ticket.detail")
	assert.Equal(t, "ok", info.Status)
	assert.Equal(t, "工单详情", info.Title)
	require.Len(t, docs, 1)
	assert.Equal(t, "docs/help/tickets/detail.md", docs[0].Path)
	assert.True(t, strings.Contains(docs[0].Text, "分诊建议"), "正文应包含关键操作描述")

	unknown, unknownDocs := idx.DocsForPage("no.such.page")
	assert.Equal(t, "unknown", unknown.Status)
	assert.Empty(t, unknownDocs)
}

func TestReadDoc(t *testing.T) {
	idx := Load()

	doc, ok := idx.ReadDoc("docs/help/ai/chat.md")
	require.True(t, ok)
	assert.Equal(t, "AI 助手（对话工作区）", doc.Title)
	assert.Contains(t, doc.Text, "确认卡片")

	_, missing := idx.ReadDoc("docs/help/not-exists.md")
	assert.False(t, missing)
}

func TestGaps_ConsistentWithLedger(t *testing.T) {
	idx := Load()
	_, totals := idx.Coverage()
	assert.Len(t, idx.Gaps(), totals["missing"]+totals["stale"],
		"Gaps 必须与账本 missing+stale 一致（当前种子文档应为 0）")
}

// H1-04：Pages / OrphanDocs 访问器与账本一致性（覆盖率看板数据面）。

func TestPagesAndOrphanDocs_ConsistentWithLedger(t *testing.T) {
	idx := Load()
	_, totals := idx.Coverage()

	pages := idx.Pages()
	require.NotEmpty(t, pages)
	assert.Len(t, pages, totals["pages"], "Pages 数必须与账本 totals.pages 一致")

	okCount := 0
	for _, page := range pages {
		assert.NotEmpty(t, page.PageKey)
		assert.NotEmpty(t, page.Status, page.PageKey)
		if page.Status == "ok" {
			okCount++
		}
	}
	assert.Equal(t, totals["ok"], okCount, "ok 页数必须与账本一致")
	assert.Len(t, idx.OrphanDocs(), totals["orphan"], "orphan 文档数必须与账本一致")

	// 返回副本：调用方排序不影响索引内部顺序。
	first := idx.Pages()[0]
	sorted := idx.Pages()
	sort.Slice(sorted, func(a, b int) bool { return sorted[a].PageKey > sorted[b].PageKey })
	assert.Equal(t, first, idx.Pages()[0], "Pages 必须返回副本")
}
