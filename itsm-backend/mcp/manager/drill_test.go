package manager

import (
	"context"
	"strconv"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus/testutil"
	"github.com/stretchr/testify/require"

	"itsm-backend/metrics"
)

// TestDrill_CredentialLeakEmergencyDisable 是 M2-06 桌面演练的**自动化复现**：
// 按 `docs/ops/mcp-runbook.md` §3.1「凭据泄露应急处置」的步骤序列执行并断言每一步的可观测效果。
//
// 覆盖的开关层级（runbook 同源）：
//   - L1/L1.5（全局开关 / 写工具面）：配置级，重启或热更新生效——不在本用例（由 M0-01 开关语义覆盖）；
//   - L2 服务器禁用：工具面塌缩 + 连接态置 0 + **零下游调用**；
//   - L3 工具停用：工具面按工具收缩，服务器连接不受影响；
//   - 恢复：重新启用后仅**仍启用**的工具回到工具面（治理位保留）。
//
// 说明：真实人工桌面演练（含管理页点击与截图）归 M2-05/M2-07 出口；本用例保证 runbook 的
// 关键步骤与语义在代码层可复现、可回归。
func TestDrill_CredentialLeakEmergencyDisable(t *testing.T) {
	ctx := context.Background()
	names := []string{"list_issues", "get_issue", "search_issues"}
	dialer := newFakeDialer()
	session := newFakeSession(names...)
	dialer.setSession(1, session)
	manager, _, cache := newBudgetManager(t, dialer, names, 40, 128000, 0.30)

	// —— 步骤 0：建立（服务器启用 + 工具发现 + 治理位启用）——
	enableAndWaitHealthy(t, manager, len(names))
	require.Len(t, manager.EffectiveTools(), len(names))
	require.Equal(t, 1.0, testutil.ToFloat64(metrics.MCPServerConnected.WithLabelValues("budget-server")))

	// —— 步骤 1（L3）：停用单个工具 → 工具面收缩，连接不受影响 ——
	disableToolInCache(cache, 1, "search_issues")
	require.Len(t, manager.EffectiveTools(), 2, "L3：停用后该工具应立即移出工具面")
	require.Equal(t, 1.0, testutil.ToFloat64(metrics.MCPServerConnected.WithLabelValues("budget-server")),
		"L3 只影响工具面，不应改变连接态")

	// —— 步骤 2（L2）：禁用服务器 → 工具面清空 + 连接态 0 + 零下游调用 ——
	callsBefore := sessionCallCount(session)
	require.NoError(t, manager.Disable(ctx, 1))
	require.Eventually(t, func() bool { return len(manager.EffectiveTools()) == 0 },
		2*time.Second, 5*time.Millisecond, func() string {
			snapshot, _ := manager.Status(1)
			return "L2：禁用后工具面必须塌缩为空（当前 status=" + string(snapshot.Status) +
				" effective=" + strconv.Itoa(len(manager.EffectiveTools())) + "）"
		})
	require.Eventually(t, func() bool {
		return testutil.ToFloat64(metrics.MCPServerConnected.WithLabelValues("budget-server")) == 0
	}, 2*time.Second, 5*time.Millisecond, "L2：禁用后连接态指标必须置 0")

	// 在途/后续调用：直接调用应 fail-closed，且不得触达服务器（零下游调用）。
	_, err := manager.CallTool(ctx, 1, "list_issues", map[string]any{})
	require.Error(t, err, "禁用后调用必须失败（fail-closed）")
	require.Equal(t, callsBefore, sessionCallCount(session), "禁用后不得产生任何下游调用")

	// —— 步骤 3：恢复（重新启用）→ 只有仍启用的工具回到工具面 ——
	require.NoError(t, manager.Enable(ctx, 1))
	require.Eventually(t, func() bool { return len(manager.EffectiveTools()) == 2 },
		3*time.Second, 5*time.Millisecond, "恢复后应仅 2 个仍启用的工具回到工具面（治理位保留）")
	require.Eventually(t, func() bool {
		return testutil.ToFloat64(metrics.MCPServerConnected.WithLabelValues("budget-server")) == 1
	}, 2*time.Second, 5*time.Millisecond, "恢复后连接态指标应回到 1")
}

// disableToolInCache 翻转治理位（模拟管理面 `POST …/tools/:callable/disable` 落库后的缓存状态）。
func disableToolInCache(cache *MemoryToolCache, serverID int, rawName string) {
	records := cache.List(serverID)
	for index := range records {
		if records[index].RawName == rawName {
			records[index].Enabled = false
		}
	}
	cache.Replace(serverID, records)
}

// sessionCallCount 读取假会话的累计调用次数（并发安全）。
func sessionCallCount(session *fakeSession) int {
	session.mu.Lock()
	defer session.mu.Unlock()
	return session.calls
}
