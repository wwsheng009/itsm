package transport

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/url"
	"strconv"
	"strings"
	"sync"
)

// 出站安全（SSRF）校验器 —— M0-05 交付；M0-08 在新增/编辑/测试连接路径强制调用。
// 设计依据：分析报告 §5.9 第一行、§6.5-2；实施方案 M0-05 卡（上线硬门槛）。
//
// 控制项：
//  1. 仅 https（http 需平台级开关 AllowHTTP）；
//  2. 禁止 URL 携带 userinfo（避免凭据混淆与日志泄露）；
//  3. 域名 + 端口 allowlist（可选）；
//  4. 拒绝环回 / 私网（RFC1918）/ 链路本地 / ULA / 保留段 / 组播 / 未指定（含 IPv4-mapped 解包）；
//  5. 首次通过后**钉住**解析结果，后续校验（每个请求都会触发）必须落在钉住集合内 → DNS rebinding 防护；
//  6. 失败统一 ssrf_blocked，错误文本不含 IP/内网结构（IP 仅进入审计事件）；
//  7. 禁跟随重定向由 transport.go 的 CheckRedirect 结构性保证。

// Resolver 抽象 DNS 解析（可注入，便于测试与自定义 DNS）。
type Resolver interface {
	LookupIPAddr(ctx context.Context, host string) ([]net.IPAddr, error)
}

// SSRFCheckEvent 是一次出站校验的审计事件（IP 仅供审计，不得写入对外响应）。
type SSRFCheckEvent struct {
	Target  string
	Host    string
	Allowed bool
	Reason  string
	IPs     []string
}

// SSRFConfig 是校验器配置。
type SSRFConfig struct {
	// AllowHTTP 平台级开关：true 时允许 http://（默认 false，仅 https）。
	AllowHTTP bool
	// AllowPrivate 平台级开关：true 时允许私网/环回等地址（仅供私有化部署/本地调试）。
	AllowPrivate bool
	// AllowedHosts 域名 allowlist；非空时目标 host 必须命中其中之一或其后代子域。
	AllowedHosts []string
	// AllowedPorts 端口 allowlist；为空时仅允许 scheme 默认端口（https=443 / http=80）。
	AllowedPorts []int
	// Resolver 可注入解析器；nil 时使用 net.DefaultResolver。
	Resolver Resolver
	// Audit 校验审计钩子（M0-08 接入审计设施）；nil 时跳过。
	Audit func(ctx context.Context, event SSRFCheckEvent)
}

// SSRFGuard 是 Guard 的默认实现（DNS rebinding 钉住状态按 host 记录）。
type SSRFGuard struct {
	cfg SSRFConfig

	mu   sync.Mutex
	pins map[string]map[string]struct{}
}

// NewSSRFGuard 创建校验器。
func NewSSRFGuard(cfg SSRFConfig) *SSRFGuard {
	return &SSRFGuard{cfg: cfg, pins: map[string]map[string]struct{}{}}
}

// Validate 实现 Guard 接口（构造期与每个请求各调用一次）。
func (g *SSRFGuard) Validate(ctx context.Context, rawURL string) error {
	parsed, err := url.Parse(strings.TrimSpace(rawURL))
	if err != nil {
		return g.deny(ctx, "", "", "URL 无法解析", nil)
	}
	target := sanitizedTarget(parsed)

	if parsed.Scheme != "https" && !(g.cfg.AllowHTTP && parsed.Scheme == "http") {
		return g.deny(ctx, target, "", "仅允许 https（http 需平台级开关）", nil)
	}
	if parsed.User != nil {
		return g.deny(ctx, target, "", "URL 不得包含凭据", nil)
	}
	host := strings.ToLower(parsed.Hostname())
	if host == "" {
		return g.deny(ctx, target, "", "缺少主机名", nil)
	}
	if len(g.cfg.AllowedHosts) > 0 && !hostAllowed(host, g.cfg.AllowedHosts) {
		return g.deny(ctx, target, host, "host 不在 allowlist", nil)
	}

	port, err := effectivePort(parsed)
	if err != nil {
		return g.deny(ctx, target, host, "URL 端口非法", nil)
	}
	if !g.portAllowed(parsed.Scheme, port) {
		return g.deny(ctx, target, host, "端口不在 allowlist", nil)
	}

	ips, err := g.resolve(ctx, host)
	if err != nil {
		return g.deny(ctx, target, host, "DNS 解析失败", nil)
	}
	if len(ips) == 0 {
		return g.deny(ctx, target, host, "DNS 未返回地址", nil)
	}
	if !g.cfg.AllowPrivate {
		for _, ip := range ips {
			if isBlockedIP(ip) {
				return g.deny(ctx, target, host, "目标解析到受限地址段", ips)
			}
		}
	}
	if !g.pin(host, ips) {
		return g.deny(ctx, target, host, "DNS rebinding 检测：解析结果与首次校验不一致", ips)
	}

	g.audit(ctx, SSRFCheckEvent{Target: target, Host: host, Allowed: true, Reason: "允许", IPs: ipStrings(ips)})
	return nil
}

func (g *SSRFGuard) deny(ctx context.Context, target, host, reason string, ips []net.IP) error {
	g.audit(ctx, SSRFCheckEvent{Target: target, Host: host, Allowed: false, Reason: reason, IPs: ipStrings(ips)})
	return &Error{Code: CodeSSRFBlocked, Op: "ssrf", Err: errors.New(reason)}
}

func (g *SSRFGuard) audit(ctx context.Context, event SSRFCheckEvent) {
	if g.cfg.Audit != nil {
		g.cfg.Audit(ctx, event)
	}
}

// pin 记录首次解析结果；后续解析必须是钉住集合的子集，否则视为 rebinding。
func (g *SSRFGuard) pin(host string, ips []net.IP) bool {
	set := make(map[string]struct{}, len(ips))
	for _, ip := range ips {
		set[ip.String()] = struct{}{}
	}

	g.mu.Lock()
	defer g.mu.Unlock()

	pinned, exists := g.pins[host]
	if !exists {
		g.pins[host] = set
		return true
	}
	for ip := range set {
		if _, ok := pinned[ip]; !ok {
			return false
		}
	}
	return true
}

func (g *SSRFGuard) resolve(ctx context.Context, host string) ([]net.IP, error) {
	if ip := net.ParseIP(host); ip != nil {
		return []net.IP{ip}, nil
	}
	resolver := g.cfg.Resolver
	if resolver == nil {
		resolver = net.DefaultResolver
	}
	addrs, err := resolver.LookupIPAddr(ctx, host)
	if err != nil {
		return nil, err
	}
	ips := make([]net.IP, 0, len(addrs))
	for _, addr := range addrs {
		ips = append(ips, addr.IP)
	}
	return ips, nil
}

func (g *SSRFGuard) portAllowed(scheme string, port int) bool {
	if len(g.cfg.AllowedPorts) == 0 {
		return (scheme == "https" && port == 443) || (scheme == "http" && port == 80)
	}
	for _, allowed := range g.cfg.AllowedPorts {
		if allowed == port {
			return true
		}
	}
	return false
}

func effectivePort(parsed *url.URL) (int, error) {
	if raw := parsed.Port(); raw != "" {
		port, err := strconv.Atoi(raw)
		if err != nil || port <= 0 || port > 65535 {
			return 0, fmt.Errorf("非法端口 %q", raw)
		}
		return port, nil
	}
	switch parsed.Scheme {
	case "https":
		return 443, nil
	case "http":
		return 80, nil
	default:
		return 0, fmt.Errorf("未知协议 %q", parsed.Scheme)
	}
}

func hostAllowed(host string, allowed []string) bool {
	for _, entry := range allowed {
		entry = strings.ToLower(strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(entry), ".")))
		if entry == "" {
			continue
		}
		if host == entry || strings.HasSuffix(host, "."+entry) {
			return true
		}
	}
	return false
}

// blockedCIDRs 覆盖保留/特殊网段（环回、私网、链路本地、组播、未指定由 net.IP 判定覆盖）。
var blockedCIDRs = func() []*net.IPNet {
	raw := []string{
		"0.0.0.0/8",       // "this network"
		"100.64.0.0/10",   // CGNAT
		"192.0.0.0/24",    // IETF 协议分配
		"192.0.2.0/24",    // TEST-NET-1
		"192.88.99.0/24",  // 6to4 中继（已废弃）
		"198.18.0.0/15",   // 基准测试
		"198.51.100.0/24", // TEST-NET-2
		"203.0.113.0/24",  // TEST-NET-3
		"224.0.0.0/4",     // 组播
		"240.0.0.0/4",     // 保留（含 255.255.255.255）
		"100::/64",        // 丢弃前缀
		"2001:db8::/32",   // 文档用
		"2002::/16",       // 6to4（内嵌 IPv4，可绕过私网判断）
		"64:ff9b::/96",    // NAT64 well-known（内嵌 IPv4）
	}
	nets := make([]*net.IPNet, 0, len(raw))
	for _, cidr := range raw {
		_, parsed, err := net.ParseCIDR(cidr)
		if err != nil {
			panic("transport: 非法内置 CIDR " + cidr)
		}
		nets = append(nets, parsed)
	}
	return nets
}()

// isBlockedIP 判定目标地址是否属于禁止出站的地址范围。
func isBlockedIP(ip net.IP) bool {
	if ip == nil {
		return true
	}
	if v4 := ip.To4(); v4 != nil {
		ip = v4 // IPv4-mapped IPv6 解包后按 IPv4 判定
	}
	if ip.IsLoopback() || ip.IsUnspecified() || ip.IsMulticast() ||
		ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() || ip.IsInterfaceLocalMulticast() ||
		ip.IsPrivate() {
		return true
	}
	for _, cidr := range blockedCIDRs {
		if cidr.Contains(ip) {
			return true
		}
	}
	return false
}

func sanitizedTarget(parsed *url.URL) string {
	clean := *parsed
	clean.User = nil
	return clean.String()
}

func ipStrings(ips []net.IP) []string {
	out := make([]string, 0, len(ips))
	for _, ip := range ips {
		out = append(out, ip.String())
	}
	return out
}
