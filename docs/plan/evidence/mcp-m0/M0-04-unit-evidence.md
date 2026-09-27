# M0-04 单元级证据（传输层：Streamable HTTP / SSE）

> 验收项：A0-04（对应任务 M0-04）｜目标级别：`unit_verified`（集成级由 M0-14 提升）
> 执行人 / 环境：AI 辅助执行；Windows + pwsh，`go1.25.13 windows/amd64`，基线 `7442fad5`（分支 `feat/bot-mcp-integration`）
> 时间：2026-09-27

## 变更文件

- `itsm-backend/mcp/transport/errors.go`：分层错误码枚举（`invalid_transport` / `ssrf_blocked` / `connect_timeout` / `canceled` / `tls_error` / `auth_required` / `protocol_mismatch` / `unreachable` / `server_error` / `transport_error`）与 `Classify`（含第三方包装的自身码文本兜底）；
- `itsm-backend/mcp/transport/options.go`：`Kind`（streamable / sse）、`HeaderProvider`（凭据注入，M0-06 对接）、`Guard`（出站安全校验，M0-05 实现）、`Config`（连接超时默认 10s）；
- `itsm-backend/mcp/transport/transport.go`：`New`（类型/URL/Guard 校验，fail-closed）、`headerRoundTripper`（每请求 Guard 校验 + Header 注入 + 401/407 → `auth_required`）、禁跟随重定向（`ErrUseLastResponse`）、`http.Client` 不设总超时（SSE 长连接存活）；
- `itsm-backend/mcp/transport/streamable.go`、`sse.go`：SDK 传输构造器（业务层只见 `Kind/Endpoint/SDK()`）；
- `itsm-backend/mcp/client/client.go`：`Options/Event/Observer/Connect`；**握手计时器不绑定会话生命周期**；协议版本不匹配由 SDK 拒绝并分类 `protocol_mismatch`；
- `itsm-backend/mcp/client/session.go`：`Tool`（inputSchema 原样保存）、`Content`、`CallResult` 投影 + `ListTools/CallTool/Ping/Close`（会话级 `cancel` 释放）；
- `itsm-backend/mcp/client/timeouts.go`：`DefaultConnectTimeout=10s`、`DefaultCallTimeout=30s`；
- 测试：`transport/transport_test.go`、`client/client_test.go`。

## 执行记录

| # | 命令（itsm-backend 目录） | 结果 |
| --- | --- | --- |
| 1 | `gofmt -l mcp\client mcp\transport` | 无输出 |
| 2 | `go test ./mcp/... -count=1` | 全绿：`client 1.977s`、`registry 0.199s`、`transport 0.223s` |
| 3 | `go vet ./mcp/...` | `vet-exit=0` |
| 4 | `go build ./...` | `build-exit=0`（281s） |

## 覆盖说明

- **双传输集成**：Streamable HTTP（主）与 SSE（兼容）均以 `httptest` 起最小 MCP 服务，覆盖 `Connect`（initialize）→ `tools/list` → `tools/call` → `Ping` → `Close`；断言 `inputSchema` 原样保存、文本内容投影正确。
- **协议版本（D4）**：手工 JSON-RPC 服务返回 `1999-01-01` → 连接失败且分类 `protocol_mismatch`，**不降级**（专测）。
- **错误分层**：`connect_timeout`（慢服务器 + 短超时）、`auth_required`（401）、`tls_error`（自签证书）、`ssrf_blocked`（Guard 拒绝且服务端零请求）、`protocol_mismatch`、`canceled`/`unreachable`/`server_error`/兜底（表驱动单测）。
- **出站安全钩子（M0-04 起即为硬约束）**：`Guard` 必填（nil → `invalid_transport`）；构造期 + 每请求校验（防 DNS rebinding 的挂点）；禁止跟随重定向结构性生效。
- **凭据注入**：`HeaderProvider` 每请求注入（断言服务器可见 `Authorization`），静态 Header 并存；凭据不进入日志/事件。
- **生命周期事件**：`connected`（含协议版本与 serverInfo）/`disconnected`/`error`，供 M0-07 manager 消费。
- **两处实测缺陷与修复（已加回归守卫）**：
  1. SDK 将 `Connect(ctx, ...)` 的 ctx 绑定**整个会话生命周期**（SSE 挂起 GET 流）；早期实现握手后 `defer cancel()` 导致 SSE 会话在 `tools/list` 前 EOF。修复：改为独立计时器 + 超时才 cancel，并加「超时窗口过后 SSE 仍存活」回归用例；
  2. SDK 用 `%v` 包装传输错误、丢失 `Unwrap` 链 → `Classify` 增加自身分层码文本兜底（排除兜底码自身）。

## 未覆盖 / 待办

- **SSRF 实现**：`Guard` 当前为接口 + 测试桩；真实校验（https-only/私网拒绝/DNS rebinding 比对/allowlist）由 M0-05 交付，上线前必须接入。
- **重试/并发治理**：属 M0-07 manager；当前客户端每次调用仅执行一次。
- **共享 mock**：M0-13 交付后替换本任务的 httptest 内联 mock 并重跑。
- **stdio**：一期不做（平台级，D1 冻结）。
