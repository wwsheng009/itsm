package provider

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"itsm-backend/ent"
	"itsm-backend/ent/enttest"
	_ "itsm-backend/ent/runtime"
	mcpclient "itsm-backend/mcp/client"
	"itsm-backend/mcp/manager"
	"itsm-backend/mcp/registry"
	"itsm-backend/mcp/transport"

	_ "github.com/mattn/go-sqlite3"
	"github.com/stretchr/testify/require"
)

// M0-09 集成测试：真实 ent 库（服务器治理行）+ 假 ToolSource（manager 替身）。

type callRecord struct {
	serverID int
	rawName  string
	args     map[string]interface{}
}

type fakeSource struct {
	records []manager.ToolRecord
	calls   []callRecord
	result  *mcpclient.CallResult
	err     error
}

func (f *fakeSource) EffectiveTools() []manager.ToolRecord { return f.records }

func (f *fakeSource) CallTool(_ context.Context, serverID int, rawName string, args map[string]interface{}) (*mcpclient.CallResult, error) {
	f.calls = append(f.calls, callRecord{serverID: serverID, rawName: rawName, args: args})
	if f.err != nil {
		return nil, f.err
	}
	if f.result != nil {
		return f.result, nil
	}
	return &mcpclient.CallResult{Content: []mcpclient.Content{{Type: "text", Text: "ok"}}}, nil
}

func newTestClient(t *testing.T) *ent.Client {
	t.Helper()
	dsn := filepath.Join(t.TempDir(), "mcp-provider-test.db") + "?_fk=1&_busy_timeout=15000"
	client := enttest.Open(t, "sqlite3", dsn)
	t.Cleanup(func() { _ = client.Close() })
	return client
}

func createServer(t *testing.T, client *ent.Client, tenantID int, name string, enabled bool) int {
	t.Helper()
	row, err := client.MCPServer.Create().
		SetTenantID(tenantID).
		SetName(name).
		SetTransport("streamable").
		SetURL("https://mcp.example.com/mcp").
		SetEnabled(enabled).
		Save(context.Background())
	require.NoError(t, err)
	return row.ID
}

func toolRecord(serverID int, tenantID int, server, rawName string, readOnly bool, schema string) manager.ToolRecord {
	return manager.ToolRecord{
		ServerID:     serverID,
		TenantID:     tenantID,
		RawName:      rawName,
		CallableName: registry.CanonicalToolName(server, rawName),
		Description:  rawName + " 描述",
		InputSchema:  json.RawMessage(schema),
		ReadOnly:     readOnly,
		Enabled:      true,
		Healthy:      true,
	}
}

func TestProvider_FaceFiltering(t *testing.T) {
	client := newTestClient(t)
	ctx := context.Background()
	githubID := createServer(t, client, 1, "github", true)
	disabledID := createServer(t, client, 1, "oldserver", false)
	otherTenantID := createServer(t, client, 2, "gitlab", true)

	source := &fakeSource{records: []manager.ToolRecord{
		toolRecord(githubID, 1, "github", "list_issues", true, `{"type":"object","properties":{"q":{"type":"string"}},"required":["q"]}`),
		toolRecord(githubID, 1, "github", "create_issue", false, `{"type":"object"}`),
		toolRecord(githubID, 1, "github", "broken_schema", true, `{not-json`),
		{ServerID: githubID, TenantID: 1, RawName: "", CallableName: "", ReadOnly: true, Enabled: true}, // 非法名 → 隔离
		toolRecord(disabledID, 1, "oldserver", "ghost", true, `{"type":"object"}`),
		toolRecord(otherTenantID, 2, "gitlab", "list_merge_requests", true, `{"type":"object"}`),
	}}
	provider := New(client, source, Options{Enabled: true})

	tools := provider.ListTools(ctx, 1)
	require.Len(t, tools, 1, "只读 ∧ 服务器启用 ∧ 同租户 ∧ schema 可解析 ∧ 投影未隔离")
	tool := tools[0]
	require.Equal(t, "mcp__github__list_issues", tool.Name)
	require.True(t, tool.ReadOnly)
	require.Equal(t, "mcp", tool.Resource)
	require.Equal(t, "read", tool.Action)
	require.True(t, strings.HasPrefix(tool.Description, "[MCP:github] "))
	require.NotNil(t, tool.ArgsSchema)

	// 写工具默认不进面（一期只读先行）。
	_, ok := provider.Resolve(ctx, 1, "mcp__github__create_issue")
	require.False(t, ok)

	// 打开写工具面（M1-02 前置）后可解析，但 ReadOnly=false/Action=write。
	withWrite := New(client, source, Options{Enabled: true, IncludeWriteTools: true})
	def, ok := withWrite.Resolve(ctx, 1, "mcp__github__create_issue")
	require.True(t, ok)
	require.False(t, def.ReadOnly)
	require.Equal(t, "write", def.Action)

	// 跨租户：租户 2 只见自己的工具（github/oldserver 的工具不可见）。
	tenant2Tools := provider.ListTools(ctx, 2)
	require.Len(t, tenant2Tools, 1)
	require.Equal(t, "mcp__gitlab__list_merge_requests", tenant2Tools[0].Name)
}

func TestProvider_TenantIsolationAndDisabledSwitch(t *testing.T) {
	client := newTestClient(t)
	ctx := context.Background()
	githubID := createServer(t, client, 1, "github", true)
	otherTenantID := createServer(t, client, 2, "gitlab", true)
	source := &fakeSource{records: []manager.ToolRecord{
		toolRecord(githubID, 1, "github", "list_issues", true, `{"type":"object"}`),
		toolRecord(otherTenantID, 2, "gitlab", "list_merge_requests", true, `{"type":"object"}`),
	}}

	provider := New(client, source, Options{Enabled: true})
	tenant1 := provider.ListTools(ctx, 1)
	require.Len(t, tenant1, 1)
	require.Equal(t, "mcp__github__list_issues", tenant1[0].Name)

	tenant2 := provider.ListTools(ctx, 2)
	require.Len(t, tenant2, 1)
	require.Equal(t, "mcp__gitlab__list_merge_requests", tenant2[0].Name)

	// 全局开关关闭：工具面恒为空（零行为变化）。
	require.Empty(t, New(client, source, Options{Enabled: false}).ListTools(ctx, 1))
	// 依赖缺失：fail-closed。
	require.Empty(t, New(nil, source, Options{Enabled: true}).ListTools(ctx, 1))
	require.Empty(t, New(client, nil, Options{Enabled: true}).ListTools(ctx, 1))
	// 非法租户。
	require.Empty(t, provider.ListTools(ctx, 0))
}

func TestProvider_ResolveSemantics(t *testing.T) {
	client := newTestClient(t)
	ctx := context.Background()
	githubID := createServer(t, client, 1, "github", true)
	gitlabID := createServer(t, client, 1, "gitlab", true)
	source := &fakeSource{records: []manager.ToolRecord{
		toolRecord(githubID, 1, "github", "list_issues", true, `{"type":"object"}`),
		toolRecord(gitlabID, 1, "gitlab", "list_issues", true, `{"type":"object"}`),
	}}
	provider := New(client, source, Options{Enabled: true})

	// canonical 精确匹配优先。
	def, ok := provider.Resolve(ctx, 1, "mcp__github__list_issues")
	require.True(t, ok)
	require.Equal(t, "mcp__github__list_issues", def.Name)

	// 短名重复 → 歧义 fail-closed。
	_, ok = provider.Resolve(ctx, 1, "list_issues")
	require.False(t, ok, "多候选必须 fail-closed，要求 canonical")

	// 未知名 / 空名。
	_, ok = provider.Resolve(ctx, 1, "mcp__github__missing")
	require.False(t, ok)
	_, ok = provider.Resolve(ctx, 1, "")
	require.False(t, ok)

	// 唯一短名可用：删除 gitlab 的候选后（新 provider 快照）解析成功。
	single := New(client, &fakeSource{records: []manager.ToolRecord{
		toolRecord(githubID, 1, "github", "list_issues", true, `{"type":"object"}`),
	}}, Options{Enabled: true})
	def, ok = single.Resolve(ctx, 1, "list_issues")
	require.True(t, ok)
	require.Equal(t, "mcp__github__list_issues", def.Name)
}

func TestProvider_Execute(t *testing.T) {
	client := newTestClient(t)
	ctx := context.Background()
	githubID := createServer(t, client, 1, "github", true)
	source := &fakeSource{records: []manager.ToolRecord{
		toolRecord(githubID, 1, "github", "list_issues", true, `{"type":"object","properties":{"q":{"type":"string"}},"required":["q"],"additionalProperties":false}`),
		toolRecord(githubID, 1, "github", "create_issue", false, `{"type":"object"}`),
	}}
	provider := New(client, source, Options{Enabled: true})

	// 成功：结果规范化 + 路由到 (serverID, rawName)。
	execution, err := provider.Execute(ctx, 1, "mcp__github__list_issues", map[string]interface{}{"q": "bug"})
	require.NoError(t, err)
	require.NotNil(t, execution)
	// M0-11：审计元数据（三元组 + 耗时 + 摘要）必须随成功路径一并返回。
	require.Equal(t, ProviderName, execution.Provider)
	require.Equal(t, "github", execution.ServerName)
	require.Equal(t, "list_issues", execution.RawToolName)
	require.Equal(t, "mcp__github__list_issues", execution.CallableName)
	require.Empty(t, execution.ErrorCode)
	require.NotEmpty(t, execution.OutputSummary)
	out := execution.Value
	require.Len(t, source.calls, 1)
	require.Equal(t, githubID, source.calls[0].serverID)
	require.Equal(t, "list_issues", source.calls[0].rawName)
	require.Equal(t, "bug", source.calls[0].args["q"])

	normalized, ok := out.(Output)
	require.True(t, ok)
	require.Equal(t, ProviderName, normalized.Provider)
	require.False(t, normalized.Truncated)
	require.Equal(t, "ok", normalized.Content[0].Text)

	// 参数 schema 拒绝：不发车。
	source.calls = nil
	_, err = provider.Execute(ctx, 1, "mcp__github__list_issues", map[string]interface{}{"q": 42})
	require.Equal(t, CodeInvalidArgs, CodeOf(err))
	require.Empty(t, source.calls, "schema 校验失败不得发起调用")

	// 未知工具（含写工具默认不进面）→ tool_not_found。
	_, err = provider.Execute(ctx, 1, "mcp__github__missing", nil)
	require.Equal(t, CodeToolNotFound, CodeOf(err))
	_, err = provider.Execute(ctx, 1, "mcp__github__create_issue", nil)
	require.Equal(t, CodeToolNotFound, CodeOf(err))

	// 写工具在写面打开后仍拒绝直接执行（Gate3 未接入）。
	withWrite := New(client, source, Options{Enabled: true, IncludeWriteTools: true})
	_, err = withWrite.Execute(ctx, 1, "mcp__github__create_issue", nil)
	require.Equal(t, CodeToolNotFound, CodeOf(err))

	// 传输错误映射：不带内网信息与堆栈。
	source.err = &transport.Error{Code: transport.CodeConnectTimeout, Op: "call", Err: errors.New("dial tcp 10.0.0.5:8080: i/o timeout")}
	_, err = provider.Execute(ctx, 1, "mcp__github__list_issues", map[string]interface{}{"q": "x"})
	require.Equal(t, CodeToolTimeout, CodeOf(err))
	require.NotContains(t, err.Error(), "10.0.0.5")

	source.err = &transport.Error{Code: transport.CodeUnreachable, Op: "dial", Err: errors.New("connection refused")}
	_, err = provider.Execute(ctx, 1, "mcp__github__list_issues", map[string]interface{}{"q": "x"})
	require.Equal(t, CodeTargetUnavailable, CodeOf(err))

	// 工具自身报错（IsError）→ 作为结果回填给模型，而不是 Go error。
	source.err = nil
	source.result = &mcpclient.CallResult{IsError: true, Content: []mcpclient.Content{{Type: "text", Text: "tool exploded"}}}
	execution, err = provider.Execute(ctx, 1, "mcp__github__list_issues", map[string]interface{}{"q": "x"})
	require.NoError(t, err)
	normalized = execution.Value.(Output)
	require.True(t, normalized.IsError)
	require.Equal(t, "tool exploded", normalized.Content[0].Text)
}

func TestProvider_ResultNormalization(t *testing.T) {
	client := newTestClient(t)
	ctx := context.Background()
	githubID := createServer(t, client, 1, "github", true)
	source := &fakeSource{records: []manager.ToolRecord{
		toolRecord(githubID, 1, "github", "list_issues", true, `{"type":"object"}`),
	}}
	provider := New(client, source, Options{Enabled: true, MaxResultBytes: 512})

	// 控制字符清理（保留换行/制表符）。
	source.result = &mcpclient.CallResult{Content: []mcpclient.Content{
		{Type: "text", Text: "line1\nline2\tok\x00\x07end"},
		{Type: "image", MIME: "image/png", Raw: json.RawMessage(`{"base64":"AAAA"}`)},
	}}
	execution, err := provider.Execute(ctx, 1, "mcp__github__list_issues", nil)
	require.NoError(t, err)
	normalized := execution.Value.(Output)
	require.Equal(t, "line1\nline2\tokend", normalized.Content[0].Text)

	// 超限截断：置 Truncated 且不超过上限。
	source.result = &mcpclient.CallResult{Content: []mcpclient.Content{{Type: "text", Text: strings.Repeat("x", 4096)}}}
	execution, err = provider.Execute(ctx, 1, "mcp__github__list_issues", nil)
	require.NoError(t, err)
	normalized = execution.Value.(Output)
	require.True(t, normalized.Truncated)
	require.LessOrEqual(t, normalized.Bytes, 512)
	require.LessOrEqual(t, len(normalized.Content), 1)
	require.Contains(t, normalized.Content[0].Text, "truncated")

	// M1-08 策略一致性：未显式配置时输出上限必须是 §5.4 的 256KB。
	require.Equal(t, 256*1024, DefaultMaxResultBytes)
	require.Equal(t, DefaultMaxResultBytes, New(nil, nil, Options{}).opts.MaxResultBytes)

	// StructuredContent 保留为结构化条目。
	source.result = &mcpclient.CallResult{StructuredContent: json.RawMessage(`{"total":3}`)}
	execution, err = provider.Execute(ctx, 1, "mcp__github__list_issues", nil)
	require.NoError(t, err)
	normalized = execution.Value.(Output)
	require.Equal(t, "structured", normalized.Content[0].Type)
	require.Contains(t, string(normalized.Content[0].Data), "total")
}
