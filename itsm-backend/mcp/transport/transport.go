package transport

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// Transport 是 ITSM 内部传输描述符：业务层只见 Kind/Endpoint；
// SDK 类型仅经 SDK() 暴露给 mcp/client 装配层（便于未来替换传输实现）。
type Transport struct {
	kind     Kind
	endpoint string
	sdk      mcp.Transport
}

// New 构造远程传输。
//
// 规则（M0-04）：
//   - 传输类型缺省为 Streamable HTTP；仅支持 streamable / sse；
//   - URL 必填；Guard 必填（SSRF 硬门槛，fail-closed）；
//   - 不跟随重定向（CheckRedirect=ErrUseLastResponse），401/407 直接归为 auth_required；
//   - 不设置 http.Client.Timeout（SSE 长连接需存活）；建连/握手超时由 Dialer、
//     TLSHandshakeTimeout、ResponseHeaderTimeout（均取 connect timeout）与调用方 ctx 共同控制。
func New(ctx context.Context, cfg Config) (*Transport, error) {
	kind := cfg.Kind
	if kind == "" {
		kind = KindStreamableHTTP
	}
	if kind != KindStreamableHTTP && kind != KindSSE {
		return nil, &Error{Code: CodeInvalidTransport, Op: "new", Err: fmt.Errorf("不支持的传输类型 %q", kind)}
	}

	endpoint := strings.TrimSpace(cfg.URL)
	if endpoint == "" {
		return nil, &Error{Code: CodeInvalidTransport, Op: "new", Err: errors.New("缺少服务器 URL")}
	}
	if cfg.Guard == nil {
		return nil, &Error{Code: CodeInvalidTransport, Op: "new", Err: errors.New("缺少出站安全校验器（Guard 必填）")}
	}
	if err := cfg.Guard.Validate(ctx, endpoint); err != nil {
		return nil, wrapGuardError(err)
	}

	httpClient := newHTTPClient(cfg)
	var inner mcp.Transport
	switch kind {
	case KindStreamableHTTP:
		inner = newStreamableTransport(endpoint, httpClient)
	case KindSSE:
		inner = newSSETransport(endpoint, httpClient)
	}

	return &Transport{kind: kind, endpoint: endpoint, sdk: inner}, nil
}

// Kind 返回传输类型。
func (t *Transport) Kind() Kind { return t.kind }

// Endpoint 返回目标 URL（不含凭据）。
func (t *Transport) Endpoint() string { return t.endpoint }

// SDK 返回底层 SDK 传输；仅允许 mcp/client 装配层引用（业务层禁止直接依赖 SDK 类型）。
func (t *Transport) SDK() mcp.Transport { return t.sdk }

func wrapGuardError(err error) error {
	var e *Error
	if errors.As(err, &e) {
		return e
	}
	return &Error{Code: CodeSSRFBlocked, Op: "guard", Err: err}
}

func newHTTPClient(cfg Config) *http.Client {
	timeout := cfg.connectTimeout()

	dialer := &net.Dialer{Timeout: timeout, KeepAlive: 30 * time.Second}
	base := &http.Transport{
		Proxy:                 http.ProxyFromEnvironment,
		DialContext:           dialer.DialContext,
		TLSHandshakeTimeout:   timeout,
		ResponseHeaderTimeout: timeout,
		MaxIdleConnsPerHost:   4,
		ForceAttemptHTTP2:     true,
	}

	return &http.Client{
		Transport: &headerRoundTripper{base: base, cfg: cfg},
		// 禁止跟随重定向（M0-05 要求之一，在此结构性保证）。
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}
}

// headerRoundTripper 负责：每请求出站安全校验（防 DNS rebinding）、请求头注入、认证失败拦截。
type headerRoundTripper struct {
	base http.RoundTripper
	cfg  Config
}

func (rt *headerRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	// 每请求校验：即使首次校验通过，后续请求仍重新校验（DNS rebinding 防护在下层 Guard 内比对）。
	if err := rt.cfg.Guard.Validate(req.Context(), req.URL.String()); err != nil {
		return nil, wrapGuardError(err)
	}

	headers := make(map[string]string, len(rt.cfg.Headers))
	for key, value := range rt.cfg.Headers {
		headers[key] = value
	}
	if rt.cfg.HeaderProvider != nil {
		dynamic, err := rt.cfg.HeaderProvider(req.Context())
		if err != nil {
			return nil, &Error{Code: CodeTransportError, Op: "header_provider", Err: err}
		}
		for key, value := range dynamic {
			headers[key] = value
		}
	}

	out := req.Clone(req.Context())
	for key, value := range headers {
		out.Header.Set(key, value)
	}

	resp, err := rt.base.RoundTrip(out)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusProxyAuthRequired {
		_ = resp.Body.Close()
		return nil, &Error{Code: CodeAuthRequired, Op: "http", Err: fmt.Errorf("服务器要求认证（HTTP %d）", resp.StatusCode)}
	}
	return resp, nil
}
