// Command mcp-mockserver 以独立进程形态运行 M0-13 的 mock MCP 服务器，
// 供前端 E2E、人工冒烟与跨进程集成测试使用（生产链路不得依赖）。
//
// 用法示例：
//
//	mcp-mockserver -addr :19090 -tools default
//	mcp-mockserver -addr :19091 -mode protocol_mismatch
//	mcp-mockserver -addr :19092 -tls            # 自签证书：客户端应报 tls_error
//
// 控制面（E2E 运行时切换工具集 / 读取调用记录）：
//
//	POST /__mock/tools?set=default|minimal|schema-a|schema-b   # 切换夹具（自动下发 tools/list_changed）
//	GET  /__mock/calls                                        # 已记录的工具调用（JSON）
//	GET  /__mock/healthz                                      # 健康探针
package main

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/json"
	"encoding/pem"
	"flag"
	"fmt"
	"math/big"
	"net"
	"net/http"
	"os"
	"strings"
	"time"

	"itsm-backend/mcp/testutil/mockserver"
)

func main() {
	addr := flag.String("addr", ":19090", "监听地址（host:port）")
	name := flag.String("name", "itsm-mcp-mock", "服务端实现名（ServerInfo.Name）")
	version := flag.String("version", "v0.1.0", "服务端版本")
	fixture := flag.String("tools", string(mockserver.FixtureDefault), "工具集夹具：default|minimal|schema-a|schema-b")
	mode := flag.String("mode", "normal", "故障模式：normal|auth_required|slow_connect|slow_call|disconnect|protocol_mismatch|internal")
	slow := flag.Duration("slow", 2*time.Second, "慢响应延迟（slow_* 模式与 slow_tool 生效）")
	oversize := flag.Int("oversize", 512*1024, "huge_output 工具返回的文本长度（字节）")
	useTLS := flag.Bool("tls", false, "以自签 TLS 启动（用于 TLS 握手失败注入）")
	flag.Parse()

	srv := mockserver.New(mockserver.Config{
		Name:          *name,
		Version:       *version,
		Fixture:       mockserver.FixtureSet(*fixture),
		Fault:         faultMode(*mode),
		SlowDelay:     *slow,
		OversizeBytes: *oversize,
	})

	mux := http.NewServeMux()
	mux.HandleFunc("/__mock/healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	})
	mux.HandleFunc("/__mock/calls", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		_ = json.NewEncoder(w).Encode(srv.Calls())
	})
	mux.HandleFunc("/__mock/tools", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		set := mockserver.FixtureSet(strings.TrimSpace(r.URL.Query().Get("set")))
		if set == "" {
			http.Error(w, "missing set", http.StatusBadRequest)
			return
		}
		srv.SetFixtureSet(set)
		w.WriteHeader(http.StatusOK)
		_, _ = fmt.Fprintf(w, "fixture=%s", set)
	})
	// 其余路径交给 MCP 处理器（Streamable HTTP；客户端 POST 到配置的 URL）。
	mux.Handle("/", srv.Handler())

	httpServer := &http.Server{Addr: *addr, Handler: mux, ReadHeaderTimeout: 10 * time.Second}
	listener, err := net.Listen("tcp", *addr)
	if err != nil {
		fmt.Fprintf(os.Stderr, "listen %s: %v\n", *addr, err)
		os.Exit(1)
	}

	scheme := "http"
	if *useTLS {
		scheme = "https"
		cert, certErr := selfSignedCert()
		if certErr != nil {
			fmt.Fprintf(os.Stderr, "self-signed cert: %v\n", certErr)
			os.Exit(1)
		}
		httpServer.TLSConfig = &tls.Config{Certificates: []tls.Certificate{cert}, MinVersion: tls.VersionTLS12}
		fmt.Printf("mcp-mockserver listening on %s://%s (tls, tools=%s, mode=%s)\n", scheme, listener.Addr(), *fixture, *mode)
		if serveErr := httpServer.ServeTLS(listener, "", ""); serveErr != nil && serveErr != http.ErrServerClosed {
			fmt.Fprintf(os.Stderr, "serve: %v\n", serveErr)
			os.Exit(1)
		}
		return
	}

	fmt.Printf("mcp-mockserver listening on %s://%s (tools=%s, mode=%s, slow=%s)\n", scheme, listener.Addr(), *fixture, *mode, *slow)
	if serveErr := httpServer.Serve(listener); serveErr != nil && serveErr != http.ErrServerClosed {
		fmt.Fprintf(os.Stderr, "serve: %v\n", serveErr)
		os.Exit(1)
	}
}

func faultMode(mode string) mockserver.FaultMode {
	switch strings.ToLower(strings.TrimSpace(mode)) {
	case "", "normal", "none":
		return mockserver.FaultNone
	case string(mockserver.FaultAuthRequired):
		return mockserver.FaultAuthRequired
	case string(mockserver.FaultSlowConnect):
		return mockserver.FaultSlowConnect
	case string(mockserver.FaultSlowCall):
		return mockserver.FaultSlowCall
	case string(mockserver.FaultDisconnect):
		return mockserver.FaultDisconnect
	case string(mockserver.FaultProtocolMismatch):
		return mockserver.FaultProtocolMismatch
	case string(mockserver.FaultInternal):
		return mockserver.FaultInternal
	default:
		fmt.Fprintf(os.Stderr, "unknown mode %q，按 normal 处理\n", mode)
		return mockserver.FaultNone
	}
}

// selfSignedCert 生成一次性自签证书（仅用于 TLS 故障注入）。
func selfSignedCert() (tls.Certificate, error) {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return tls.Certificate{}, err
	}
	serial, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 128))
	if err != nil {
		return tls.Certificate{}, err
	}
	template := x509.Certificate{
		SerialNumber:          serial,
		Subject:               pkix.Name{CommonName: "itsm-mcp-mockserver"},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().Add(30 * 24 * time.Hour),
		KeyUsage:              x509.KeyUsageDigitalSignature | x509.KeyUsageCertSign,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		BasicConstraintsValid: true,
		IsCA:                  true,
		DNSNames:              []string{"localhost"},
		IPAddresses:           []net.IP{net.ParseIP("127.0.0.1")},
	}
	der, err := x509.CreateCertificate(rand.Reader, &template, &template, &key.PublicKey, key)
	if err != nil {
		return tls.Certificate{}, err
	}
	keyDER, err := x509.MarshalECPrivateKey(key)
	if err != nil {
		return tls.Certificate{}, err
	}
	certPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
	keyPEM := pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: keyDER})
	return tls.X509KeyPair(certPEM, keyPEM)
}
