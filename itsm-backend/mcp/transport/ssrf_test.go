package transport

import (
	"context"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"
)

// fakeResolver 是可控 DNS 解析桩（支持运行时切换答案，模拟 rebinding）。
type fakeResolver struct {
	mu      sync.Mutex
	answers map[string][]string
	err     error
}

func (r *fakeResolver) LookupIPAddr(_ context.Context, host string) ([]net.IPAddr, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.err != nil {
		return nil, r.err
	}
	raw, ok := r.answers[host]
	if !ok {
		return nil, &net.DNSError{Err: "no such host", Name: host, IsNotFound: true}
	}
	addrs := make([]net.IPAddr, 0, len(raw))
	for _, item := range raw {
		addrs = append(addrs, net.IPAddr{IP: net.ParseIP(item)})
	}
	return addrs, nil
}

func (r *fakeResolver) set(host string, ips []string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.answers[host] = ips
}

func newPublicResolver() *fakeResolver {
	return &fakeResolver{answers: map[string][]string{
		"api.example.com":      {"93.184.216.34"},
		"mcp.api.example.com":  {"93.184.216.34"},
		"internal.example.com": {"10.0.0.5"},
		"mixed.example.com":    {"93.184.216.34", "10.0.0.5"},
	}}
}

func TestSSRFGuard_TableDriven(t *testing.T) {
	ctx := context.Background()

	cases := []struct {
		name        string
		cfg         SSRFConfig
		rawURL      string
		wantAllowed bool
	}{
		{"https 公网放行", SSRFConfig{}, "https://api.example.com/mcp", true},
		{"http 默认拒绝", SSRFConfig{}, "http://api.example.com/mcp", false},
		{"http 平台开关放行", SSRFConfig{AllowHTTP: true}, "http://api.example.com/mcp", true},
		{"非 http(s) 协议拒绝", SSRFConfig{}, "ftp://api.example.com/mcp", false},
		{"userinfo 拒绝", SSRFConfig{}, "https://user:pass@api.example.com/mcp", false},
		{"缺少主机名拒绝", SSRFConfig{}, "https:///mcp", false},
		{"IPv4 环回拒绝", SSRFConfig{}, "https://127.0.0.1/mcp", false},
		{"IPv6 环回拒绝", SSRFConfig{}, "https://[::1]/mcp", false},
		{"RFC1918 私网拒绝", SSRFConfig{}, "https://10.1.2.3/mcp", false},
		{"172.16 私网拒绝", SSRFConfig{}, "https://172.16.5.5/mcp", false},
		{"192.168 私网拒绝", SSRFConfig{}, "https://192.168.1.1/mcp", false},
		{"云元数据链路本地拒绝", SSRFConfig{}, "https://169.254.169.254/latest/meta-data", false},
		{"ULA 拒绝", SSRFConfig{}, "https://[fd00::1]/mcp", false},
		{"IPv4-mapped 私网拒绝", SSRFConfig{}, "https://[::ffff:192.168.1.1]/mcp", false},
		{"CGNAT 保留段拒绝", SSRFConfig{}, "https://100.64.0.1/mcp", false},
		{"基准测试段拒绝", SSRFConfig{}, "https://198.18.0.1/mcp", false},
		{"未指定地址拒绝", SSRFConfig{}, "https://0.0.0.0/mcp", false},
		{"域名解析到私网拒绝", SSRFConfig{}, "https://internal.example.com/mcp", false},
		{"解析结果含私网即拒绝", SSRFConfig{}, "https://mixed.example.com/mcp", false},
		{"域名不在 allowlist 拒绝", SSRFConfig{AllowedHosts: []string{"example.com"}}, "https://other.test/mcp", false},
		{"allowlist 命中子域放行", SSRFConfig{AllowedHosts: []string{"example.com"}}, "https://mcp.api.example.com/mcp", true},
		{"端口不在 allowlist 拒绝", SSRFConfig{}, "https://api.example.com:8443/mcp", false},
		{"端口 allowlist 放行", SSRFConfig{AllowedPorts: []int{8443}}, "https://api.example.com:8443/mcp", true},
		{"平台开关允许私网（私有化）", SSRFConfig{AllowPrivate: true}, "https://127.0.0.1/mcp", true},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cfg := tc.cfg
			cfg.Resolver = newPublicResolver()
			guard := NewSSRFGuard(cfg)

			err := guard.Validate(ctx, tc.rawURL)
			if tc.wantAllowed {
				require.NoError(t, err)
				return
			}
			require.Error(t, err)
			require.Equal(t, CodeSSRFBlocked, CodeOf(err), "err=%v", err)
		})
	}
}

func TestSSRFGuard_DNSRebindingBlocked(t *testing.T) {
	ctx := context.Background()
	resolver := newPublicResolver()
	guard := NewSSRFGuard(SSRFConfig{Resolver: resolver})

	require.NoError(t, guard.Validate(ctx, "https://api.example.com/mcp"), "首次校验应通过并钉住解析结果")

	// 同名域名的解析结果被更换为私网地址 → 命中受限地址段，必须拒绝。
	resolver.set("api.example.com", []string{"10.0.0.5"})
	err := guard.Validate(ctx, "https://api.example.com/mcp")
	require.Error(t, err)
	require.Equal(t, CodeSSRFBlocked, CodeOf(err))

	// 换成另一个**未钉住**的公网地址同样拒绝（rebinding 钉住比对生效）。
	resolver.set("api.example.com", []string{"1.1.1.1"})
	err = guard.Validate(ctx, "https://api.example.com/mcp")
	require.Error(t, err)
	require.Equal(t, CodeSSRFBlocked, CodeOf(err))
	require.Contains(t, err.Error(), "rebinding")

	// 回到钉住集合内则通过（多 A 记录轮换场景：子集放行）。
	resolver.set("api.example.com", []string{"93.184.216.34"})
	require.NoError(t, guard.Validate(ctx, "https://api.example.com/mcp"))
}

func TestSSRFGuard_AuditEventsAndNoLeak(t *testing.T) {
	ctx := context.Background()

	var mu sync.Mutex
	var events []SSRFCheckEvent
	guard := NewSSRFGuard(SSRFConfig{
		Resolver: newPublicResolver(),
		Audit: func(_ context.Context, event SSRFCheckEvent) {
			mu.Lock()
			defer mu.Unlock()
			events = append(events, event)
		},
	})

	// 拒绝路径：审计含 IP，但对外错误文本不含 IP/内网结构。
	err := guard.Validate(ctx, "https://internal.example.com/mcp")
	require.Error(t, err)
	require.NotContains(t, err.Error(), "10.0.0.5", "错误文本不得泄露内网地址")
	require.NotContains(t, err.Error(), "internal.example.com")

	// 允许路径。
	require.NoError(t, guard.Validate(ctx, "https://api.example.com/mcp"))

	mu.Lock()
	defer mu.Unlock()
	require.Len(t, events, 2)
	require.False(t, events[0].Allowed)
	require.Contains(t, events[0].IPs, "10.0.0.5")
	require.True(t, events[1].Allowed)
	require.NotContains(t, events[1].Target, "user", "审计目标不含 userinfo")
}

func TestSSRFGuard_IntegratesWithTransportNew(t *testing.T) {
	ctx := context.Background()

	guard := NewSSRFGuard(SSRFConfig{Resolver: newPublicResolver()})
	tr, err := New(ctx, Config{URL: "https://api.example.com/mcp", Guard: guard})
	require.NoError(t, err)
	require.NotNil(t, tr)

	blocked := NewSSRFGuard(SSRFConfig{Resolver: newPublicResolver()})
	_, err = New(ctx, Config{URL: "https://internal.example.com/mcp", Guard: blocked})
	require.Error(t, err)
	require.Equal(t, CodeSSRFBlocked, CodeOf(err))
}

// TestHTTPClient_RevalidatesPerRequest 验证每个请求都会重新校验（rebinding 挂点生效）。
func TestHTTPClient_RevalidatesPerRequest(t *testing.T) {
	ctx := context.Background()
	resolver := newPublicResolver()
	guard := NewSSRFGuard(SSRFConfig{Resolver: resolver})

	require.NoError(t, guard.Validate(ctx, "https://api.example.com/mcp"), "先钉住公网解析结果")
	httpClient := newHTTPClient(Config{URL: "https://api.example.com/mcp", Guard: guard})

	// 请求前解析结果被换成私网：RoundTripper 应在发起网络请求前拒绝。
	resolver.set("api.example.com", []string{"10.0.0.5"})
	_, err := httpClient.Get("https://api.example.com/mcp")
	require.Error(t, err)
	require.Equal(t, CodeSSRFBlocked, CodeOf(err))
}

// TestHTTPClient_DoesNotFollowRedirects 验证禁跟随重定向（§5.9 控制项之一）。
func TestHTTPClient_DoesNotFollowRedirects(t *testing.T) {
	var followed bool
	var mu sync.Mutex

	mux := http.NewServeMux()
	mux.HandleFunc("/start", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "/next", http.StatusFound)
	})
	mux.HandleFunc("/next", func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		followed = true
		mu.Unlock()
		w.WriteHeader(http.StatusOK)
	})

	server := httptest.NewServer(mux)
	defer server.Close()

	parsed, err := url.Parse(server.URL)
	require.NoError(t, err)
	port, err := strconv.Atoi(parsed.Port())
	require.NoError(t, err)

	guard := NewSSRFGuard(SSRFConfig{AllowHTTP: true, AllowPrivate: true, AllowedPorts: []int{port}})
	httpClient := newHTTPClient(Config{URL: server.URL + "/start", Guard: guard})

	resp, err := httpClient.Get(server.URL + "/start")
	require.NoError(t, err)
	defer resp.Body.Close()
	require.Equal(t, http.StatusFound, resp.StatusCode, "必须停在 302 而不得跟随")

	mu.Lock()
	defer mu.Unlock()
	require.False(t, followed, "重定向目标不得被请求")
}
