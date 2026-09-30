package provider

import (
	"context"
	"testing"

	"itsm-backend/capability"
	"itsm-backend/mcp/manager"

	"github.com/stretchr/testify/require"
)

// M2 能力开关（2026-09-30 方案）：运行时写面与总开关的门禁契约。
//
//  1. 写面关闭 → 写工具不进面，GateReason 给出 capability_disabled:mcp_write；
//  2. 打开写面（同一实例、无需重建）→ 写工具立即可见；
//  3. 总开关关闭 → 面为空，读/写工具均给 capability_disabled:mcp；
//  4. 执行期复判（含审批后队列路径）与面判定同源：关了就执行不了。
//
// fakeCaps 是 capability.Source 的测试替身：可动态切换以证明「免重启」。
type fakeCaps struct {
	snap capability.Snapshot
}

func (f *fakeCaps) For(context.Context, int) capability.Snapshot { return f.snap }
func (f *fakeCaps) Invalidate(int)                               {}

func TestProvider_CapabilityWriteFaceSwitch(t *testing.T) {
	client := newTestClient(t)
	ctx := context.Background()
	serverID := createServer(t, client, 1, "mock", true)
	source := &fakeSource{records: []manager.ToolRecord{
		toolRecord(serverID, 1, "mock", "list_issues", true, `{"type":"object"}`),
		toolRecord(serverID, 1, "mock", "create_issue", false, `{"type":"object"}`),
	}}
	caps := &fakeCaps{snap: capability.Snapshot{MCPEnabled: true, MCPWriteEnabled: false}}
	provider := New(client, source, Options{Enabled: true, Capabilities: caps})

	// 写面关：仅只读工具在面内；被挡的写工具给出精确原因（未知工具返回空）。
	require.Len(t, provider.ListTools(ctx, 1), 1)
	require.Equal(t, ReasonMCPWriteDisabled, provider.GateReason(ctx, 1, "mcp__mock__create_issue"))
	require.Empty(t, provider.GateReason(ctx, 1, "mcp__mock__list_issues"))
	require.Empty(t, provider.GateReason(ctx, 1, "mcp__mock__unknown_tool"))
	_, ok := provider.Resolve(ctx, 1, "mcp__mock__create_issue")
	require.False(t, ok, "写面关闭时写工具不可解析")

	// 执行期：审批后写路径同样 fail-closed（G2）。
	_, err := provider.ExecuteApprovedWrite(ctx, 1, "mcp__mock__create_issue", nil)
	require.Error(t, err)
	require.Equal(t, CodeMCPWriteDisabled, CodeOf(err))

	// 打开写面（同一 provider 实例，免重启）。
	caps.snap.MCPWriteEnabled = true
	require.Len(t, provider.ListTools(ctx, 1), 2)
	require.Empty(t, provider.GateReason(ctx, 1, "mcp__mock__create_issue"))
	execution, err := provider.ExecuteApprovedWrite(ctx, 1, "mcp__mock__create_issue", nil)
	require.NoError(t, err)
	require.NotNil(t, execution)
	require.Len(t, source.calls, 1, "写面开启后审批后执行可达 manager")

	// 总开关关闭：面为空；读工具给出总开关原因；直接执行被拒。
	caps.snap.MCPEnabled = false
	require.Empty(t, provider.ListTools(ctx, 1))
	require.Equal(t, ReasonMCPDisabled, provider.GateReason(ctx, 1, "mcp__mock__list_issues"))
	require.Equal(t, ReasonMCPDisabled, provider.GateReason(ctx, 1, "mcp__mock__create_issue"))
	_, err = provider.Execute(ctx, 1, "mcp__mock__list_issues", nil)
	require.Error(t, err)
	require.Equal(t, CodeMCPDisabled, CodeOf(err))
	require.Len(t, source.calls, 1, "关闭后不得再触发 manager 调用")
}

// 未注入 Capabilities 时保持静态口径（升级零行为变化）。
func TestProvider_StaticFallbackWithoutCapabilitySource(t *testing.T) {
	client := newTestClient(t)
	ctx := context.Background()
	serverID := createServer(t, client, 1, "mock", true)
	source := &fakeSource{records: []manager.ToolRecord{
		toolRecord(serverID, 1, "mock", "list_issues", true, `{"type":"object"}`),
		toolRecord(serverID, 1, "mock", "create_issue", false, `{"type":"object"}`),
	}}

	readOnlyFace := New(client, source, Options{Enabled: true, IncludeWriteTools: false})
	require.Len(t, readOnlyFace.ListTools(ctx, 1), 1)
	// 无能力源时静态口径生效：写面关闭仍可给出精确原因（与运行时判定同语义，
	// 便于静态部署下前端/审计区分「写面关闭」与「工具不存在」）。
	require.Equal(t, ReasonMCPWriteDisabled, readOnlyFace.GateReason(ctx, 1, "mcp__mock__create_issue"))
	require.Empty(t, readOnlyFace.GateReason(ctx, 1, "mcp__mock__list_issues"))

	writeFace := New(client, source, Options{Enabled: true, IncludeWriteTools: true})
	require.Len(t, writeFace.ListTools(ctx, 1), 2)
}
