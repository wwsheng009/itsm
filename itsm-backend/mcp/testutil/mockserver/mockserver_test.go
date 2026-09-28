package mockserver_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"itsm-backend/mcp/client"
	"itsm-backend/mcp/testutil/mockserver"
	"itsm-backend/mcp/transport"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// M0-13：mock MCP 服务器自测 + 既有客户端（M0-04）消费样例。
//
// 覆盖：初始化握手与工具发现、工具调用、夹具齐全性、运行时切换（list_changed）、
// 故障注入（401 / 慢响应 / 断连 / 协议不匹配 / 超大输出 / TLS）。

// allowGuard 放行出站校验（mock 跑在本机，SSRF 拦截属于 transport 自身用例的职责）。
type allowGuard struct{}

func (allowGuard) Validate(context.Context, string) error { return nil }

func dial(t *testing.T, url string) (*client.Session, error) {
	t.Helper()
	tr, err := transport.New(context.Background(), transport.Config{
		Kind:  transport.KindStreamableHTTP,
		URL:   url,
		Guard: allowGuard{},
	})
	require.NoError(t, err)
	return client.New(client.Options{
		Timeouts: client.Timeouts{Connect: 3 * time.Second, Call: 3 * time.Second},
	}).Connect(context.Background(), tr)
}

func TestMockServer_HandshakeAndFixtures(t *testing.T) {
	srv := mockserver.New(mockserver.Config{Name: "itsm-mock", Version: "v9.9.9"})
	httpServer := srv.Start()
	defer httpServer.Close()

	session, err := dial(t, httpServer.URL)
	require.NoError(t, err)
	defer session.Close()

	require.Equal(t, "itsm-mock", session.ServerName())
	require.Equal(t, "v9.9.9", session.ServerVersion())
	require.NotEmpty(t, session.ProtocolVersion())

	tools, err := session.ListTools(context.Background())
	require.NoError(t, err)
	names := make([]string, 0, len(tools))
	for _, tool := range tools {
		names = append(names, tool.RawName)
	}
	for _, want := range []string{"list_issues", "create_issue", "huge_output", "bad name!", "slow_tool", "schema_probe"} {
		assert.Contains(t, names, want, "默认夹具必须包含 %s", want)
	}
}

func TestMockServer_CallToolRecorded(t *testing.T) {
	srv := mockserver.New(mockserver.Config{Fixture: mockserver.FixtureMinimal})
	httpServer := srv.Start()
	defer httpServer.Close()

	session, err := dial(t, httpServer.URL)
	require.NoError(t, err)
	defer session.Close()

	result, err := session.CallTool(context.Background(), "ping_tool", map[string]any{"q": "1"})
	require.NoError(t, err)
	require.False(t, result.IsError)
	require.Contains(t, result.Content[0].Text, "pong")

	calls := srv.Calls()
	require.Len(t, calls, 1)
	assert.Equal(t, "ping_tool", calls[0].Tool)
	assert.Equal(t, "1", calls[0].Args["q"])
	assert.Contains(t, mockserver.DescribeCall(calls[0]), "ping_tool")
}

func TestMockServer_RuntimeFixtureSwitch(t *testing.T) {
	srv := mockserver.New(mockserver.Config{Fixture: mockserver.FixtureSchemaA})
	httpServer := srv.Start()
	defer httpServer.Close()

	session, err := dial(t, httpServer.URL)
	require.NoError(t, err)
	defer session.Close()

	before, err := session.ListTools(context.Background())
	require.NoError(t, err)
	require.Len(t, before, 1)
	require.Contains(t, string(before[0].InputSchema), `"q"`)
	require.NotContains(t, string(before[0].InputSchema), `"required"`)

	// 运行时切换 → SDK 自动下发 notifications/tools/list_changed；重新发现应看到新 schema。
	srv.SetFixtureSet(mockserver.FixtureSchemaB)
	require.Equal(t, mockserver.FixtureSchemaB, srv.FixtureSet())

	after, err := session.ListTools(context.Background())
	require.NoError(t, err)
	require.Len(t, after, 1)
	require.Contains(t, string(after[0].InputSchema), `"required"`, "schema 变更必须可被重新发现（隔离用例依赖）")
}

func TestMockServer_FaultAuthRequired(t *testing.T) {
	srv := mockserver.New(mockserver.Config{Fault: mockserver.FaultAuthRequired})
	httpServer := srv.Start()
	defer httpServer.Close()

	_, err := dial(t, httpServer.URL)
	require.Error(t, err)
	assert.Contains(t, strings.ToLower(err.Error()), "401")
}

func TestMockServer_FaultSlowConnect(t *testing.T) {
	srv := mockserver.New(mockserver.Config{Fault: mockserver.FaultSlowConnect, SlowDelay: 400 * time.Millisecond})
	httpServer := srv.Start()
	defer httpServer.Close()

	tr, err := transport.New(context.Background(), transport.Config{
		Kind:  transport.KindStreamableHTTP,
		URL:   httpServer.URL,
		Guard: allowGuard{},
	})
	require.NoError(t, err)

	start := time.Now()
	_, err = client.New(client.Options{Timeouts: client.Timeouts{Connect: 80 * time.Millisecond}}).
		Connect(context.Background(), tr)
	require.Error(t, err, "慢建连必须触发超时")
	// 只守「不得挂起」：全量并发测试下 3s 墙钟阈值会因排程抖动误报（曾观测 3.43s），放宽到 10s。
	assert.Less(t, time.Since(start), 10*time.Second, "超时后必须尽快返回，不得挂起")
}

func TestMockServer_FaultSlowCall(t *testing.T) {
	srv := mockserver.New(mockserver.Config{Fixture: mockserver.FixtureMinimal, Fault: mockserver.FaultSlowCall, SlowDelay: 500 * time.Millisecond})
	httpServer := srv.Start()
	defer httpServer.Close()

	tr, err := transport.New(context.Background(), transport.Config{
		Kind:  transport.KindStreamableHTTP,
		URL:   httpServer.URL,
		Guard: allowGuard{},
	})
	require.NoError(t, err)
	session, err := client.New(client.Options{Timeouts: client.Timeouts{Connect: 2 * time.Second, Call: 100 * time.Millisecond}}).
		Connect(context.Background(), tr)
	require.NoError(t, err)
	defer session.Close()

	start := time.Now()
	_, err = session.CallTool(context.Background(), "ping_tool", nil)
	require.Error(t, err, "慢调用必须触发调用超时")
	assert.Less(t, time.Since(start), 2*time.Second)
}

func TestMockServer_FaultDisconnect(t *testing.T) {
	srv := mockserver.New(mockserver.Config{Fault: mockserver.FaultDisconnect})
	httpServer := srv.Start()
	defer httpServer.Close()

	_, err := dial(t, httpServer.URL)
	require.Error(t, err, "连接被断开必须尽快失败，而不是挂起等待")
}

func TestMockServer_FaultProtocolMismatch(t *testing.T) {
	srv := mockserver.New(mockserver.Config{Fault: mockserver.FaultProtocolMismatch})
	httpServer := srv.Start()
	defer httpServer.Close()

	_, err := dial(t, httpServer.URL)
	require.Error(t, err, "协议版本不匹配必须拒绝（D4：不降级）")
	assert.Contains(t, strings.ToLower(err.Error()), "protocol", "错误信息需指出协议版本问题：%v", err)
}

func TestMockServer_OversizeOutput(t *testing.T) {
	const size = 256 * 1024
	srv := mockserver.New(mockserver.Config{Fixture: mockserver.FixtureDefault, OversizeBytes: size})
	httpServer := srv.Start()
	defer httpServer.Close()

	session, err := dial(t, httpServer.URL)
	require.NoError(t, err)
	defer session.Close()

	result, err := session.CallTool(context.Background(), "huge_output", nil)
	require.NoError(t, err)
	require.NotEmpty(t, result.Content)
	assert.GreaterOrEqual(t, len(result.Content[0].Text), size, "超大输出必须完整返回，由上层做限额/截断")
}

func TestMockServer_TLSHandshakeFailure(t *testing.T) {
	srv := mockserver.New(mockserver.Config{Fixture: mockserver.FixtureMinimal})
	httpServer := srv.StartTLS()
	defer httpServer.Close()

	_, err := dial(t, httpServer.URL)
	require.Error(t, err, "自签证书必须被客户端拒绝（TLS 错误注入）")
}

func TestMockServer_DuplicateNameAcrossServers(t *testing.T) {
	first := mockserver.New(mockserver.Config{Name: "first", Fixture: mockserver.FixtureSchemaA})
	second := mockserver.New(mockserver.Config{Name: "second", Fixture: mockserver.FixtureSchemaA})
	firstHTTP := first.Start()
	defer firstHTTP.Close()
	secondHTTP := second.Start()
	defer secondHTTP.Close()

	sessionA, err := dial(t, firstHTTP.URL)
	require.NoError(t, err)
	defer sessionA.Close()
	sessionB, err := dial(t, secondHTTP.URL)
	require.NoError(t, err)
	defer sessionB.Close()

	toolsA, err := sessionA.ListTools(context.Background())
	require.NoError(t, err)
	toolsB, err := sessionB.ListTools(context.Background())
	require.NoError(t, err)
	require.Len(t, toolsA, 1)
	require.Len(t, toolsB, 1)
	// 同名工具来自不同服务器：注册表投影必须按 `mcp__<server>__<tool>` 区分（M0-02 用例消费）。
	assert.Equal(t, toolsA[0].RawName, toolsB[0].RawName)
	assert.NotEqual(t, firstHTTP.URL, secondHTTP.URL)
}
