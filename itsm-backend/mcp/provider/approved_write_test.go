// M1-02：审批后写执行的 provider 契约。
//
// 锁定的行为：
//  1. 直接执行入口（Execute）对写工具保持 fail-closed（审批链路外的调用拿不到写路径）；
//  2. ExecuteApprovedWrite 允许写工具，且**单次执行不重试**（成功与失败都只尝试一次）；
//  3. 审批后执行与只读执行共用同一套解析/租户/治理/健康/schema 校验（不因「已审批」而放松）。
package provider

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"

	"itsm-backend/mcp/manager"
	"itsm-backend/mcp/transport"
)

func TestProvider_ExecuteApprovedWrite(t *testing.T) {
	client := newTestClient(t)
	ctx := context.Background()
	serverID := createServer(t, client, 1, "github", true)

	source := &fakeSource{records: []manager.ToolRecord{
		toolRecord(serverID, 1, "github", "create_issue", false,
			`{"type":"object","properties":{"title":{"type":"string"}},"required":["title"]}`),
		toolRecord(serverID, 1, "github", "list_issues", true, `{"type":"object"}`),
	}}
	provider := New(client, source, Options{Enabled: true, IncludeWriteTools: true})

	// 1) 直接执行：写工具仍拒绝，且**在调用前**就拒绝（不产生真实副作用）。
	_, err := provider.Execute(ctx, 1, "mcp__github__create_issue", map[string]interface{}{"title": "x"})
	require.Error(t, err)
	require.Equal(t, CodeToolNotFound, CodeOf(err))
	require.Empty(t, source.calls, "未审批的写调用不得触达服务器")

	// 2) 审批后执行：成功且恰好一次调用。
	execution, err := provider.ExecuteApprovedWrite(ctx, 1, "mcp__github__create_issue",
		map[string]interface{}{"title": "打印机故障"})
	require.NoError(t, err)
	require.NotNil(t, execution)
	require.NotNil(t, execution.Value)
	require.Equal(t, ProviderName, execution.Provider)
	require.Equal(t, "github", execution.ServerName)
	require.Equal(t, "create_issue", execution.RawToolName)
	require.Equal(t, "mcp__github__create_issue", execution.CallableName)
	require.GreaterOrEqual(t, execution.DurationMs, int64(1), "耗时口径 ≥1ms")
	require.Len(t, source.calls, 1, "写调用必须恰好一次（不自动重试）")
	require.Equal(t, "create_issue", source.calls[0].rawName)
	require.Equal(t, "打印机故障", source.calls[0].args["title"])

	// 3) 失败路径：同样单次尝试（无自动重试），错误码稳定且不回显底层细节。
	source.err = &transport.Error{Code: transport.CodeServerError, Op: "call", Err: errors.New("upstream boom")}
	_, err = provider.ExecuteApprovedWrite(ctx, 1, "mcp__github__create_issue",
		map[string]interface{}{"title": "again"})
	require.Error(t, err)
	require.Equal(t, CodeServerError, CodeOf(err))
	require.Len(t, source.calls, 2, "失败不得自动重试（累计恰好 2 次调用）")

	// 4) 审批不等于绕过校验：schema 不通过仍拒绝，且不发请求。
	source.err = nil
	_, err = provider.ExecuteApprovedWrite(ctx, 1, "mcp__github__create_issue", map[string]interface{}{})
	require.Error(t, err)
	require.Equal(t, CodeInvalidArgs, CodeOf(err))
	require.Len(t, source.calls, 2, "schema 不通过不得发起调用")

	// 5) 只读工具走审批入口等价于普通执行（幂等语义，便于 ToolQueue 统一分流）。
	_, err = provider.ExecuteApprovedWrite(ctx, 1, "mcp__github__list_issues", nil)
	require.NoError(t, err)
	require.Len(t, source.calls, 3)
}
