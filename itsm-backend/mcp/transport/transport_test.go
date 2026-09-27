package transport

import (
	"context"
	"crypto/x509"
	"errors"
	"net"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/require"
)

// stubGuard 是最小 Guard 桩：记录调用次数，可选返回错误。
type stubGuard struct {
	calls int
	err   error
}

func (g *stubGuard) Validate(context.Context, string) error {
	g.calls++
	return g.err
}

func TestNew_RejectsInvalidConfig(t *testing.T) {
	ctx := context.Background()

	t.Run("未知传输类型", func(t *testing.T) {
		_, err := New(ctx, Config{Kind: Kind("websocket"), URL: "https://example.com/mcp", Guard: &stubGuard{}})
		require.Error(t, err)
		require.Equal(t, CodeInvalidTransport, CodeOf(err))
	})

	t.Run("缺少 URL", func(t *testing.T) {
		_, err := New(ctx, Config{Kind: KindStreamableHTTP, Guard: &stubGuard{}})
		require.Error(t, err)
		require.Equal(t, CodeInvalidTransport, CodeOf(err))
	})

	t.Run("缺少 Guard（fail-closed）", func(t *testing.T) {
		_, err := New(ctx, Config{Kind: KindStreamableHTTP, URL: "https://example.com/mcp"})
		require.Error(t, err)
		require.Equal(t, CodeInvalidTransport, CodeOf(err))
	})
}

func TestNew_GuardDeniedMapsToSSRFBlocked(t *testing.T) {
	guard := &stubGuard{err: errors.New("target resolves to private network")}
	_, err := New(context.Background(), Config{URL: "https://internal.example/mcp", Guard: guard})
	require.Error(t, err)
	require.Equal(t, CodeSSRFBlocked, CodeOf(err))
	require.Equal(t, 1, guard.calls, "首次校验必须在构造期执行")
}

func TestNew_BuildsSDKTransports(t *testing.T) {
	guard := &stubGuard{}
	ctx := context.Background()

	t.Run("缺省为 streamable", func(t *testing.T) {
		transport, err := New(ctx, Config{URL: "https://example.com/mcp", Guard: guard})
		require.NoError(t, err)
		require.Equal(t, KindStreamableHTTP, transport.Kind())
		require.Equal(t, "https://example.com/mcp", transport.Endpoint())
		require.IsType(t, &mcp.StreamableClientTransport{}, transport.SDK())
	})

	t.Run("SSE", func(t *testing.T) {
		transport, err := New(ctx, Config{Kind: KindSSE, URL: "https://example.com/sse", Guard: guard})
		require.NoError(t, err)
		require.Equal(t, KindSSE, transport.Kind())
		require.IsType(t, &mcp.SSEClientTransport{}, transport.SDK())
	})
}

func TestClassify(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want Code
	}{
		{"超时", context.DeadlineExceeded, CodeConnectTimeout},
		{"取消", context.Canceled, CodeCanceled},
		{"协议不匹配", errors.New(`unsupported protocol version: "1999-01-01"`), CodeProtocolMismatch},
		{"服务端 JSON-RPC 错误", errors.New("jsonrpc error: method not found"), CodeServerError},
		{"TLS 证书", x509.UnknownAuthorityError{}, CodeTLSError},
		{"DNS", &net.DNSError{Err: "no such host", Name: "nope.invalid"}, CodeUnreachable},
		{"兜底", errors.New("boom"), CodeTransportError},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			require.Equal(t, tc.want, Classify(tc.err))
		})
	}

	wrapped := &Error{Code: CodeAuthRequired, Op: "http", Err: errors.New("401")}
	require.Equal(t, CodeAuthRequired, Classify(wrapped))
	require.Equal(t, CodeAuthRequired, CodeOf(wrapped))
	require.Empty(t, CodeOf(nil))
}
