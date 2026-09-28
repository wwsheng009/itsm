package manager

import (
	"context"
	"encoding/json"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"itsm-backend/mcp/client"
)

// M2-03 工具面预算：判定口径 + 边沿触发告警。
//
// 说明：工具治理默认拒绝（D7）——新发现的工具 `Enabled=false`，故本组用例先向工具缓存
// 预置「同名 + Enabled=true」的治理记录（PlanDiscovery 对同名工具保留治理位），
// 以构造「有效工具面」的真实语义。

// seedEnabledTools 向工具缓存预置启用记录（SchemaHash 留空：不触发 schema 变更隔离）。
func seedEnabledTools(cache *MemoryToolCache, serverID, tenantID int, names ...string) {
	records := cache.List(serverID)
	existing := map[string]bool{}
	for _, record := range records {
		existing[record.RawName] = true
	}
	for _, name := range names {
		if existing[name] {
			continue
		}
		records = append(records, ToolRecord{
			ServerID:     serverID,
			TenantID:     tenantID,
			RawName:      name,
			CallableName: name,
			Enabled:      true,
			Risk:         "low",
		})
	}
	cache.Replace(serverID, records)
}

// newBudgetManager 构造注入了预算阈值与预置治理位的 manager，并注册一个租户 1 的服务器。
func newBudgetManager(t *testing.T, dialer *fakeDialer, names []string, toolBudget, contextTokens int, share float64) (*Manager, *MemoryEventSink, *MemoryToolCache) {
	t.Helper()
	events := CollectEvents()
	cache := NewMemoryToolCache()
	seedEnabledTools(cache, 1, 1, names...)
	manager := New(Options{
		Dial:           dialer.Dial,
		Events:         events,
		ToolCache:      cache,
		Backoff:        BackoffPolicy{Base: 20 * time.Millisecond, Factor: 2, Max: 80 * time.Millisecond},
		HealthInterval: time.Hour, // 本组用例不需要健康循环
		ConnectTimeout: time.Second,
		CallTimeout:    time.Second,
		DisableGrace:   50 * time.Millisecond,
		ToolBudget:     toolBudget,
		ContextTokens:  contextTokens,
		ToolTokenShare: share,
	})
	manager.Upsert(testServerConfig(1, "budget-server"))
	t.Cleanup(manager.Stop)
	return manager, events, cache
}

// enableAndWaitHealthy 启用服务器并等待工具缓存刷到期望数量（发现完成）。
func enableAndWaitHealthy(t *testing.T, manager *Manager, wantTools int) {
	t.Helper()
	require.NoError(t, manager.Enable(context.Background(), 1))
	waitFor(t, 3*time.Second, func() bool {
		status, _ := manager.Status(1)
		return status.Status == StatusHealthy && len(manager.CachedTools(1)) == wantTools
	})
}

// TestToolBudget_未超限不发事件：工具数 = 预算时无告警。
func TestToolBudget_未超限不发事件(t *testing.T) {
	names := toolNames(40)
	dialer := newFakeDialer()
	dialer.setSession(1, newFakeSession(names...))
	manager, events, _ := newBudgetManager(t, dialer, names, 40, 128000, 0.30)

	enableAndWaitHealthy(t, manager, 40)

	report := manager.ToolBudgetReport(1)
	require.Equal(t, 40, report.Surface.Tools)
	require.False(t, report.Exceeded)
	require.Empty(t, report.Reasons)
	require.Zero(t, events.Count(EventToolsBudgetExceeded), "未超限不应发出预算告警")
}

// TestToolBudget_跨阈值发一条且不重复：首次超限发 1 条；重复判定（计数不变）不再发。
func TestToolBudget_跨阈值发一条且不重复(t *testing.T) {
	names := toolNames(41)
	dialer := newFakeDialer()
	dialer.setSession(1, newFakeSession(names...))
	manager, events, _ := newBudgetManager(t, dialer, names, 40, 128000, 0.30)

	enableAndWaitHealthy(t, manager, 41)

	waitCount(t, events, EventToolsBudgetExceeded, 1)
	alert := eventsByType(events, EventToolsBudgetExceeded)[0]
	require.Equal(t, 1, alert.TenantID)
	require.Contains(t, alert.Detail, "tools=41")
	require.Contains(t, alert.Detail, "有效工具数 41 超过预算 40")

	// 计数未变化时重复判定：不再发（边沿触发）。
	require.True(t, manager.EvaluateToolBudget(1).Exceeded)
	require.True(t, manager.EvaluateToolBudget(1).Exceeded)
	require.Equal(t, 1, events.Count(EventToolsBudgetExceeded), "计数未变化时不应重复告警")
}

// TestToolBudget_超限期间计数变化再发一条：41 → 42 应再发一条。
func TestToolBudget_超限期间计数变化再发一条(t *testing.T) {
	names := toolNames(41)
	dialer := newFakeDialer()
	session := newFakeSession(names...)
	dialer.setSession(1, session)
	manager, events, cache := newBudgetManager(t, dialer, names, 40, 128000, 0.30)

	enableAndWaitHealthy(t, manager, 41)
	waitCount(t, events, EventToolsBudgetExceeded, 1)

	// 服务器侧新增一个工具，并预置其治理位（默认拒绝下新工具不启用）→ 重载后发现 → 计数变化 → 再发一条。
	session.mu.Lock()
	session.tools = append(session.tools, clientTool("extra_tool"))
	session.mu.Unlock()
	seedEnabledTools(cache, 1, 1, "extra_tool")
	require.NoError(t, manager.Reload(context.Background(), 1))
	waitFor(t, 3*time.Second, func() bool { return len(manager.CachedTools(1)) == 42 })
	waitCount(t, events, EventToolsBudgetExceeded, 2)
	alerts := eventsByType(events, EventToolsBudgetExceeded)
	require.Contains(t, alerts[len(alerts)-1].Detail, "tools=42")
}

// TestToolBudget_占比信号：工具数未超限、描述过大导致 token 占比超限 → 发事件且理由为占比。
func TestToolBudget_占比信号(t *testing.T) {
	dialer := newFakeDialer()
	session := newFakeSession("list_issues")
	session.mu.Lock()
	// 单条描述 4000 个汉字 ≈ 4000 tokens；上下文预算 10000 → 占比 40% > 30%。
	session.tools[0].Description = strings.Repeat("查", 4000)
	session.mu.Unlock()
	dialer.setSession(1, session)
	manager, events, _ := newBudgetManager(t, dialer, []string{"list_issues"}, 40, 10000, 0.30)

	enableAndWaitHealthy(t, manager, 1)

	waitCount(t, events, EventToolsBudgetExceeded, 1)
	alert := eventsByType(events, EventToolsBudgetExceeded)[0]
	require.Contains(t, alert.Detail, "tokens=")
	require.NotContains(t, alert.Detail, "有效工具数", "工具数未超限，理由应只有占比")
	require.True(t, manager.ToolBudgetReport(1).Exceeded)
}

// TestToolBudget_默认阈值：Options 未配置时使用 40 / 128000 / 30%。
func TestToolBudget_默认阈值(t *testing.T) {
	manager := New(Options{})
	policy := manager.Policy(999) // 未注册服务器 → 平台默认值
	require.Equal(t, 40, policy.ToolBudget)
	require.Equal(t, 128000, policy.ContextTokens)
	require.InDelta(t, 0.30, policy.ToolTokenShare, 1e-9)

	// 未注册任何服务器：租户工具面为空，不超限。
	report := manager.ToolBudgetReport(1)
	require.False(t, report.Exceeded)
	require.Zero(t, report.Surface.Tools)
}

// TestToolBudget_未启用工具不计数：治理位关闭的工具不进入有效工具面（默认拒绝语义）。
func TestToolBudget_未启用工具不计数(t *testing.T) {
	names := toolNames(41)
	dialer := newFakeDialer()
	dialer.setSession(1, newFakeSession(names...))
	// 不预置治理位：41 个工具全部 Enabled=false → 有效工具面为空 → 不应告警。
	manager, events, _ := newBudgetManager(t, dialer, nil, 40, 128000, 0.30)

	enableAndWaitHealthy(t, manager, 41)

	require.Zero(t, events.Count(EventToolsBudgetExceeded), "未启用（治理位关闭）的工具不应触发预算告警")
	require.Zero(t, manager.ToolBudgetReport(1).Surface.Tools)
}

// TestToolBudget_忽略其它租户与关停：只统计本租户；关停后工具面为空。
func TestToolBudget_忽略其它租户与关停(t *testing.T) {
	names := toolNames(41)
	dialer := newFakeDialer()
	dialer.setSession(1, newFakeSession(names...))
	manager, _, _ := newBudgetManager(t, dialer, names, 40, 128000, 0.30)
	enableAndWaitHealthy(t, manager, 41)

	// 其它租户：不在统计内。
	require.False(t, manager.ToolBudgetReport(2).Exceeded)
	require.True(t, manager.ToolBudgetReport(1).Exceeded)

	// 关停服务器（healthy → 非 healthy）→ 有效工具面为空 → 不再超限。
	require.NoError(t, manager.Disable(context.Background(), 1))
	require.Eventually(t, func() bool { return !manager.ToolBudgetReport(1).Exceeded },
		time.Second, 5*time.Millisecond, "禁用后不应再判超限")
}

// —— 测试辅助 ——

func toolNames(count int) []string {
	names := make([]string, 0, count)
	for index := 0; index < count; index++ {
		names = append(names, "tool_"+strconv.Itoa(index))
	}
	return names
}

func clientTool(name string) client.Tool {
	return client.Tool{
		RawName:     name,
		Description: name + " description",
		InputSchema: json.RawMessage(`{"type":"object"}`),
	}
}

// eventsByType 过滤指定类型的事件（MemoryEventSink 只提供 Count/Events）。
func eventsByType(events *MemoryEventSink, eventType EventType) []Event {
	var out []Event
	for _, event := range events.Events() {
		if event.Type == eventType {
			out = append(out, event)
		}
	}
	return out
}
