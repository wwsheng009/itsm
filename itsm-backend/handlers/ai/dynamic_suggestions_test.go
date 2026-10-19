package ai

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// P2 LLM 动态建议：解析校验（纯函数）+ 缓存/单飞/审计编排。

func TestParseDynamicSuggestions(t *testing.T) {
	// 代码围栏 + 前后噪声容忍；readOnly 强制 true、source=llm。
	fenced := "```json\n" +
		`[{"kind":"task","label":"  汇总今日待处理  ","prompt":"汇总我当前待处理的工单"},` +
		`{"kind":"question","label":"有哪些 SLA 风险？","prompt":"列出临近 SLA 的工单"},` +
		`{"kind":"bogus","label":"非法类型归任务","prompt":"查询"},` +
		`{"kind":"task","label":"第四条应被截断","prompt":"查询"}]` +
		"\n```"
	items, err := parseDynamicSuggestions(fenced)
	require.NoError(t, err)
	require.Len(t, items, DynamicSuggestionsMaxItems)
	assert.Equal(t, "task", items[0].Kind)
	assert.Equal(t, "汇总今日待处理", items[0].Label)
	assert.True(t, items[0].ReadOnly)
	assert.Equal(t, "llm", items[0].Source)
	assert.Equal(t, "dyn:0", items[0].ID)
	assert.Equal(t, "question", items[1].Kind)
	assert.Equal(t, "task", items[2].Kind, "非法 kind 归任务卡")

	// 空 label/prompt 跳过；全部无效 → 错误。
	items, err = parseDynamicSuggestions(`[{"kind":"task","label":"","prompt":"p"},{"kind":"task","label":"ok","prompt":""}]`)
	assert.Error(t, err)
	assert.Empty(t, items)

	// 非 JSON / 空输出 → 错误（fail-safe 走 parse_failed）。
	_, err = parseDynamicSuggestions("抱歉，我无法生成。")
	assert.Error(t, err)
	_, err = parseDynamicSuggestions("   ")
	assert.Error(t, err)
}

func TestDynamicSuggestionAuditScore(t *testing.T) {
	assert.Equal(t, 0, dynamicSuggestionAuditScore(false, 0))
	assert.Equal(t, 50, dynamicSuggestionAuditScore(true, 1))
	assert.Equal(t, 70, dynamicSuggestionAuditScore(true, 3))
	assert.Equal(t, 100, dynamicSuggestionAuditScore(true, 99), "上限封顶")
}

type fakeGenerator struct {
	mu     sync.Mutex
	calls  int
	model  string
	output string
	err    error
	block  chan struct{}
}

func (f *fakeGenerator) Generate(_ context.Context, _ DynamicSuggestionRequest) (string, string, error) {
	f.mu.Lock()
	f.calls++
	f.mu.Unlock()
	if f.block != nil {
		<-f.block
	}
	return f.model, f.output, f.err
}

func (f *fakeGenerator) callCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.calls
}

type fakeAudit struct {
	mu      sync.Mutex
	entries []DynamicSuggestionAuditEntry
}

func (f *fakeAudit) Record(_ context.Context, entry DynamicSuggestionAuditEntry) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.entries = append(f.entries, entry)
	return nil
}

func (f *fakeAudit) snapshot() []DynamicSuggestionAuditEntry {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]DynamicSuggestionAuditEntry(nil), f.entries...)
}

const validDynamicOutput = `[{"kind":"task","label":"汇总待处理","prompt":"汇总我当前待处理的工单"}]`

func dynamicRequest() DynamicSuggestionRequest {
	return DynamicSuggestionRequest{
		TenantID: 7, UserID: 42, PageKey: "ticket.list", Route: "/tickets",
		Role: "agent", Title: "工单列表", Scene: "s1-ticket-assistant", Entrypoint: "ticket_list",
	}
}

func TestDynamicSuggestionService_CacheAndAudit(t *testing.T) {
	generator := &fakeGenerator{model: "gpt-4o", output: validDynamicOutput}
	audit := &fakeAudit{}
	svc := NewDynamicSuggestionService(generator, audit, nil)
	base := time.Date(2026, 10, 18, 12, 0, 0, 0, time.UTC)
	now := base
	svc.now = func() time.Time { return now }

	first := svc.Get(context.Background(), dynamicRequest())
	require.NotNil(t, first)
	assert.True(t, first.Generated)
	assert.False(t, first.Cached)
	assert.Equal(t, "gpt-4o", first.Model)
	require.Len(t, first.Suggestions, 1)
	assert.Equal(t, generator.callCount(), 1)
	entries := audit.snapshot()
	require.Len(t, entries, 1)
	assert.True(t, entries[0].Generated)
	assert.Equal(t, 1, entries[0].Count)
	assert.Equal(t, "ticket.list", entries[0].PageKey)

	// 缓存命中：不再调用 LLM、不再审计。
	second := svc.Get(context.Background(), dynamicRequest())
	assert.True(t, second.Cached)
	assert.True(t, second.Generated)
	assert.Equal(t, 1, generator.callCount())
	assert.Len(t, audit.snapshot(), 1)

	// 不同角色 → 不同缓存键（按角色建议）。
	otherRole := dynamicRequest()
	otherRole.Role = "super_admin"
	svc.Get(context.Background(), otherRole)
	assert.Equal(t, 2, generator.callCount())

	// TTL 过期 → 重新生成并再审计。
	now = base.Add(DynamicSuggestionsDefaultTTL + time.Second)
	svc.Get(context.Background(), dynamicRequest())
	assert.Equal(t, 3, generator.callCount())
	assert.Len(t, audit.snapshot(), 3)
}

func TestDynamicSuggestionService_FailSafe(t *testing.T) {
	// LLM 失败 → 不缓存（下次重试）；审计一次失败条目。
	generator := &fakeGenerator{err: errors.New("provider down")}
	audit := &fakeAudit{}
	svc := NewDynamicSuggestionService(generator, audit, nil)

	result := svc.Get(context.Background(), dynamicRequest())
	assert.False(t, result.Generated)
	assert.Equal(t, "llm_unavailable", result.Reason)
	assert.Empty(t, result.Suggestions)
	svc.Get(context.Background(), dynamicRequest())
	assert.Equal(t, 2, generator.callCount(), "失败不缓存 → 允许重试")
	entries := audit.snapshot()
	require.Len(t, entries, 2)
	assert.False(t, entries[0].Generated)

	// 解析失败 → parse_failed（同样不缓存）。
	bad := NewDynamicSuggestionService(&fakeGenerator{output: "不好意思"}, &fakeAudit{}, nil)
	result = bad.Get(context.Background(), dynamicRequest())
	assert.False(t, result.Generated)
	assert.Equal(t, "parse_failed", result.Reason)

	// 服务未装配（nil generator）→ disabled，不触发任何生成/审计。
	disabled := NewDynamicSuggestionService(nil, &fakeAudit{}, nil)
	assert.False(t, disabled.Available())
	result = disabled.Get(context.Background(), dynamicRequest())
	assert.False(t, result.Generated)
	assert.Equal(t, "disabled", result.Reason)
}

func TestDynamicSuggestionService_SingleFlight(t *testing.T) {
	generator := &fakeGenerator{model: "gpt-4o", output: validDynamicOutput, block: make(chan struct{})}
	svc := NewDynamicSuggestionService(generator, &fakeAudit{}, nil)

	var wg sync.WaitGroup
	results := make([]*DynamicSuggestionResult, 2)
	for index := 0; index < 2; index++ {
		wg.Add(1)
		go func(slot int) {
			defer wg.Done()
			results[slot] = svc.Get(context.Background(), dynamicRequest())
		}(index)
	}
	// 等两个请求都进入（其中一个在生成、另一个在等待单飞门）。
	time.Sleep(50 * time.Millisecond)
	close(generator.block)
	wg.Wait()

	assert.Equal(t, 1, generator.callCount(), "同键并发只触发一次 LLM 调用")
	for _, result := range results {
		require.NotNil(t, result)
		assert.True(t, result.Generated)
	}
	// 一个来自生成（Cached=false）、一个来自等待后读缓存（Cached=true）。
	cachedFlags := []bool{results[0].Cached, results[1].Cached}
	assert.Contains(t, cachedFlags, true)
}
