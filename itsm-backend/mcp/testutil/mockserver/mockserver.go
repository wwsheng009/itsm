// Package mockserver 提供可复用的 mock MCP 服务器（M0-13）。
//
// 两种用法：
//   - 测试库：`New(Config{...})` 后挂到 `httptest.NewServer(srv.Handler())`，供 Go 集成测试
//     直接消费（M0-04/07/08 已用它验证传输、连接生命周期与管理 API）；
//   - 独立进程：`cmd/mcp-mockserver` 以同样的 Config 起监听，供前端 E2E 与人工冒烟使用。
//
// 能力：
//   - 夹具工具集：只读 / 写 / 超长输出 / 非法字符名 / 慢响应 / schema 变更（`FixtureSet`）；
//   - 故障注入：401、慢响应（建连与调用超时）、传输中断连、协议版本不匹配、超大输出、TLS 握手失败；
//   - 运行时切换工具集：`SetFixtureSet` 走 SDK 的 AddTool/RemoveTools，自动下发
//     `notifications/tools/list_changed`（治理与 schema 隔离用例依赖）。
//
// 约束：本包只允许被测试与 cmd 依赖，不得进入生产链路。
package mockserver

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// FixtureSet 是工具集夹具名（对应 Fixtures 表中的键）。
type FixtureSet string

const (
	// FixtureDefault：默认工具集（只读 + 写 + 超长 + 非法名 + 慢响应 + schema 变更候选）。
	FixtureDefault FixtureSet = "default"
	// FixtureMinimal：仅一个只读工具（最小面，用于治理/会话用例）。
	FixtureMinimal FixtureSet = "minimal"
	// FixtureSchemaA：schema 变更前形态（v1 字段）。
	FixtureSchemaA FixtureSet = "schema-a"
	// FixtureSchemaB：schema 变更后形态（新增必填字段，触发隔离用例）。
	FixtureSchemaB FixtureSet = "schema-b"
)

// FaultMode 是故障注入模式。
type FaultMode string

const (
	// FaultNone：正常服务。
	FaultNone FaultMode = ""
	// FaultAuthRequired：所有请求返回 401（凭据被拒）。
	FaultAuthRequired FaultMode = "auth_required"
	// FaultSlowConnect：建连阶段延迟（触发 connect timeout）。
	FaultSlowConnect FaultMode = "slow_connect"
	// FaultSlowCall：工具调用延迟（配合 SlowDelay 触发 call timeout）。
	FaultSlowCall FaultMode = "slow_call"
	// FaultDisconnect：请求处理中断开连接（模拟网络中断）。
	FaultDisconnect FaultMode = "disconnect"
	// FaultProtocolMismatch：initialize 响应改写为客户端不支持的协议版本（D4：不降级）。
	FaultProtocolMismatch FaultMode = "protocol_mismatch"
	// FaultInternal：返回 500（服务端异常）。
	FaultInternal FaultMode = "internal"
)

// mismatchProtocolVersion 是客户端必然不支持的版本号（用于 D4 断言）。
const mismatchProtocolVersion = "1999-01-01"

// Config 是 mock 服务器配置。
type Config struct {
	// Name 是服务端实现名（ServerInfo.Name），默认 "itsm-mcp-mock"。
	Name string
	// Version 是服务端版本，默认 "v0.1.0"。
	Version string
	// Fixture 是工具集夹具，默认 FixtureDefault。
	Fixture FixtureSet
	// Fault 是故障注入模式，默认 FaultNone。
	Fault FaultMode
	// SlowDelay 是慢响应的延迟（FaultSlowConnect/FaultSlowCall 生效），默认 2s。
	SlowDelay time.Duration
	// JSONResponse 为 true 时 Streamable HTTP 用 application/json 响应（默认 true，
	// 与官方 SDK 客户端在本仓的既有用法一致）。
	JSONResponse bool
	// OversizeBytes 是超大输出工具（`huge_output`）返回的文本长度，默认 512KiB。
	OversizeBytes int
}

// Call 是一次工具调用的记录。
type Call struct {
	Tool string
	Args map[string]any
	At   time.Time
}

// Server 是 mock MCP 服务器实例。
type Server struct {
	cfg Config

	mcp *mcp.Server
	// handler 是最终对外的 HTTP 处理器。
	handler http.HandlerFunc

	mu    sync.Mutex
	calls []Call
	// fixture 是当前夹具（可运行时切换）。
	fixture FixtureSet
}

// New 构造 mock 服务器（未启动监听）。
func New(cfg Config) *Server {
	cfg = normalizeConfig(cfg)
	srv := &Server{cfg: cfg}
	srv.mcp = mcp.NewServer(&mcp.Implementation{Name: cfg.Name, Version: cfg.Version}, nil)
	srv.applyFixture(cfg.Fixture, true)

	streamable := mcp.NewStreamableHTTPHandler(
		func(*http.Request) *mcp.Server { return srv.mcp },
		&mcp.StreamableHTTPOptions{JSONResponse: cfg.JSONResponse},
	)
	srv.handler = srv.wrap(streamable)
	return srv
}

// Handler 返回可直接挂载的 HTTP 处理器（含故障注入包装）。
func (s *Server) Handler() http.Handler { return s.handler }

// Config 返回归一化后的配置（便于测试断言）。
func (s *Server) Config() Config { return s.cfg }

// SetFixtureSet 运行时切换工具集：AddTool/RemoveTools 会自动下发
// `notifications/tools/list_changed`，供客户端/注册表重新发现。
func (s *Server) SetFixtureSet(set FixtureSet) {
	s.mu.Lock()
	s.fixture = set
	s.mu.Unlock()
	s.applyFixture(set, false)
}

// FixtureSet 返回当前夹具名。
func (s *Server) FixtureSet() FixtureSet {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.fixture
}

// Calls 返回已记录的工具调用（副本）。
func (s *Server) Calls() []Call {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]Call, len(s.calls))
	copy(out, s.calls)
	return out
}

// Start 起一个 httptest 服务器（HTTP）。调用方负责 Close。
func (s *Server) Start() *httptest.Server { return httptest.NewServer(s.Handler()) }

// StartTLS 起一个自签 TLS 的 httptest 服务器（用于 TLS 握手失败注入：客户端默认校验会拒绝）。
func (s *Server) StartTLS() *httptest.Server { return httptest.NewTLSServer(s.Handler()) }

// —— 内部：夹具 ——

func (s *Server) applyFixture(set FixtureSet, initial bool) {
	// 初始构建时忽略"移除不存在工具"的返回值；切换时逐个移除旧夹具工具。
	if !initial {
		s.mcp.RemoveTools(fixtureToolNames[s.fixture]...)
	}
	s.mu.Lock()
	s.fixture = set
	s.mu.Unlock()
	for _, spec := range fixturesFor(set, s.cfg.OversizeBytes) {
		s.mcp.AddTool(spec.tool, s.toolHandler(spec))
	}
}

// toolHandler 包装工具实现：记录调用 + 慢调用故障注入。
func (s *Server) toolHandler(spec toolSpec) mcp.ToolHandler {
	return func(ctx context.Context, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		args := map[string]any{}
		if len(req.Params.Arguments) > 0 {
			_ = json.Unmarshal(req.Params.Arguments, &args)
		}
		s.mu.Lock()
		s.calls = append(s.calls, Call{Tool: spec.tool.Name, Args: args, At: time.Now()})
		s.mu.Unlock()

		if spec.slow || s.cfg.Fault == FaultSlowCall {
			time.Sleep(s.cfg.SlowDelay)
		}
		if spec.errText != "" {
			return &mcp.CallToolResult{
				IsError: true,
				Content: []mcp.Content{&mcp.TextContent{Text: spec.errText}},
			}, nil
		}
		return spec.result(), nil
	}
}

// —— 内部：故障注入 ——

func (s *Server) wrap(next http.Handler) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		switch s.cfg.Fault {
		case FaultAuthRequired:
			w.Header().Set("WWW-Authenticate", `Bearer realm="mcp-mock"`)
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		case FaultInternal:
			http.Error(w, "internal error", http.StatusInternalServerError)
			return
		case FaultSlowConnect:
			time.Sleep(s.cfg.SlowDelay)
		case FaultDisconnect:
			// 立刻断开：客户端应把它分类为网络/传输错误，而不是挂起。
			if hijacker, ok := w.(http.Hijacker); ok {
				conn, _, err := hijacker.Hijack()
				if err == nil {
					_ = conn.Close()
					return
				}
			}
			http.Error(w, "connection reset", http.StatusBadGateway)
			return
		case FaultProtocolMismatch:
			next = s.rewriteProtocolVersion(next)
		}
		next.ServeHTTP(w, r)
	}
}

// rewriteProtocolVersion 把 initialize 响应里的 protocolVersion 改写为客户端不支持的版本。
func (s *Server) rewriteProtocolVersion(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		recorder := &responseRecorder{header: http.Header{}}
		next.ServeHTTP(recorder, r)
		body := recorder.body.Bytes()
		if bytes.Contains(body, []byte(`"protocolVersion"`)) {
			var payload map[string]any
			if err := json.Unmarshal(body, &payload); err == nil {
				if result, ok := payload["result"].(map[string]any); ok {
					if _, has := result["protocolVersion"]; has {
						result["protocolVersion"] = mismatchProtocolVersion
						if patched, err := json.Marshal(payload); err == nil {
							body = patched
						}
					}
				}
			}
		}
		for key, values := range recorder.header {
			for _, value := range values {
				w.Header().Add(key, value)
			}
		}
		if recorder.status != 0 {
			w.WriteHeader(recorder.status)
		}
		_, _ = w.Write(body)
	})
}

// responseRecorder 是轻量响应缓冲（避免 httptest.ResponseRecorder 对 SSE 头的处理差异）。
type responseRecorder struct {
	header http.Header
	body   bytes.Buffer
	status int
}

func (r *responseRecorder) Header() http.Header { return r.header }

func (r *responseRecorder) Write(data []byte) (int, error) { return r.body.Write(data) }

func (r *responseRecorder) WriteHeader(status int) {
	if r.status == 0 {
		r.status = status
	}
}

// —— 内部：辅助 ——

func normalizeConfig(cfg Config) Config {
	if strings.TrimSpace(cfg.Name) == "" {
		cfg.Name = "itsm-mcp-mock"
	}
	if strings.TrimSpace(cfg.Version) == "" {
		cfg.Version = "v0.1.0"
	}
	if cfg.Fixture == "" {
		cfg.Fixture = FixtureDefault
	}
	if cfg.SlowDelay <= 0 {
		cfg.SlowDelay = 2 * time.Second
	}
	if cfg.OversizeBytes <= 0 {
		cfg.OversizeBytes = 512 * 1024
	}
	// 固定 JSON 响应（Streamable HTTP §2.1.5），与仓内既有客户端用法一致。
	cfg.JSONResponse = true
	return cfg
}

// DescribeCall 便于测试输出：`tool(args...)`。
func DescribeCall(call Call) string {
	encoded, err := json.Marshal(call.Args)
	if err != nil {
		return fmt.Sprintf("%s(<%v>)", call.Tool, err)
	}
	return fmt.Sprintf("%s(%s)", call.Tool, string(encoded))
}
