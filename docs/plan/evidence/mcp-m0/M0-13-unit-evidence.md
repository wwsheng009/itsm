# M0-13 证据（mock MCP 服务器与测试设施）

> 验收项：A0-13（对应任务 M0-13）｜目标 DoD：`unit_verified`
> 实际状态：**`unit_verified`**（11 个用例全绿，含真实客户端消费样例与 6 类故障注入；独立进程可执行 smoke 通过；`go build ./...` / `go vet` 干净）。
> 执行人 / 环境：AI 辅助执行（子代理因运行时排队失败未产出，改由主会话实施）；Windows + pwsh，go1.25.13，基线 `229d168c`（分支 `feat/bot-mcp-integration`）
> 时间：2026-09-27

## 设计要点

- **同源 SDK**：mock 基于官方 `github.com/modelcontextprotocol/go-sdk v1.4.0` 的服务端
  （`mcp.NewServer` + `mcp.NewStreamableHTTPHandler(JSONResponse: true)`），与 M0-04 客户端、
  仓内既有 `mcp/client` 测试同源，协议兼容性由 SDK 保证（无自研协议栈）。
- **两种形态**（任务卡要求）：
  1. **测试库**：`mockserver.New(Config)` + `Start()`（httptest）/ `StartTLS()`，可被 Go 集成测试直接 import；
  2. **独立进程**：`cmd/mcp-mockserver`（同 Config 起监听），供前端 E2E 与人工冒烟使用。
- **夹具矩阵**（`FixtureSet`）：
  | 夹具 | 工具 | 用途 |
  | --- | --- | --- |
  | `default` | `list_issues`（只读、带 `readOnlyHint`）、`create_issue`（写）、`huge_output`（超长）、`bad name!`（非法字符名）、`slow_tool`（慢）、`schema_probe`（schema v1） | 全量治理/投影/隔离用例 |
  | `minimal` | `ping_tool` | 最小面（会话/治理开关） |
  | `schema-a` / `schema-b` | `schema_probe`（v1 → v2 新增必填 `limit`） | schema 变更 → 隔离 → 复核用例 |
  | 跨实例同名 | 两个 mock 实例各暴露 `schema_probe` | 投影重名/碰撞用例 |
- **运行时切换**：`SetFixtureSet()` 走 SDK 的 `AddTool`/`RemoveTools`，由 SDK 自动下发
  `notifications/tools/list_changed`；独立进程额外暴露控制面 `POST /__mock/tools?set=…`，
  E2E 可在不重启的情况下切换工具集（治理与 schema 隔离场景要求）。
- **故障注入**（`FaultMode`，HTTP 层包装，不改业务语义）：
  | 模式 | 行为 | 期望客户端分类 |
  | --- | --- | --- |
  | `auth_required` | 401 + `WWW-Authenticate` | `auth_required` |
  | `slow_connect` | 建连前 sleep（可配） | `connect_timeout` |
  | `slow_call` | 每次 tools/call 前 sleep（可配） | 调用超时 |
  | `disconnect` | hijack 后立即断开连接 | 网络/传输错误（快速失败） |
  | `protocol_mismatch` | 改写 initialize 响应的 `protocolVersion=1999-01-01` | 协议不匹配拒绝（D4 不降级） |
  | `internal` | 500 | 服务端错误 |
  | TLS | `StartTLS()`/`-tls` 自签证书 | `tls_error` |
  超大输出由 `huge_output` 夹具 + `OversizeBytes` 控制（默认 512KiB）。
- **不进入生产链路**：仅 `mcp/testutil/**` 与 `cmd/mcp-mockserver/**`；无生产包 import 本包
  （已核对：`go build ./...` 通过且本包仅出现在测试与 cmd 依赖中）。

## 变更文件

| 文件 | 行数 | 说明 |
| --- | --- | --- |
| `itsm-backend/mcp/testutil/mockserver/mockserver.go` | ~330 | `Config`/`Server`/`FaultMode`/`Call`、故障注入包装、协议版本改写、httptest/独立进程共用的 `Handler()` |
| `itsm-backend/mcp/testutil/mockserver/fixtures.go` | ~170 | 夹具定义（工具/schema/行为）与 `fixtureToolNames` |
| `itsm-backend/mcp/testutil/mockserver/mockserver_test.go` | ~270 | 11 个用例（见下） |
| `itsm-backend/cmd/mcp-mockserver/main.go` | ~190 | 独立进程：`-addr/-tools/-mode/-slow/-oversize/-tls` + 控制面 `/__mock/*` + 自签 TLS |

## 执行记录

| # | 命令（workdir=`itsm-backend`） | 结果 |
| --- | --- | --- |
| 1 | `gofmt -l ./mcp/testutil/mockserver ./cmd/mcp-mockserver` | 无输出（已格式化） |
| 2 | `go vet ./mcp/testutil/... ./cmd/mcp-mockserver/` | exit 0 |
| 3 | `go test ./mcp/testutil/mockserver/ -count=1 -v` | **11 passed**（3.35s） |
| 4 | `go build ./...` | exit 0 |
| 5 | 独立进程 smoke：`go build -o $TEMP/mcp-mockserver.exe ./cmd/mcp-mockserver/` → 起 `127.0.0.1:19099 -tools minimal` → `GET /__mock/healthz` / `POST /__mock/tools?set=schema-b` / `GET /__mock/calls` | `healthz=ok`、`switch=fixture=schema-b`、`calls=[]`（随后停止进程） |

### 用例清单（`mockserver_test.go`）

| 用例 | 覆盖 |
| --- | --- |
| `TestMockServer_HandshakeAndFixtures` | 初始化握手（ServerInfo/协议版本）+ 默认夹具 6 工具齐全（**真实客户端消费样例**：`mcp/client` + `mcp/transport` 接入） |
| `TestMockServer_CallToolRecorded` | 工具调用返回 + 调用记录（tool/args/时间） |
| `TestMockServer_RuntimeFixtureSwitch` | schema-a → schema-b 运行时切换；重新发现可见 `required` 变更（隔离用例依赖） |
| `TestMockServer_FaultAuthRequired` | 401 注入 |
| `TestMockServer_FaultSlowConnect` | 慢建连 → 连接超时且快速返回（不挂起） |
| `TestMockServer_FaultSlowCall` | 慢调用 → 调用超时 |
| `TestMockServer_FaultDisconnect` | 断连注入 → 快速失败 |
| `TestMockServer_FaultProtocolMismatch` | 协议版本不匹配 → 拒绝且错误含 protocol（D4） |
| `TestMockServer_OversizeOutput` | 256KiB 超大输出完整返回（限额/截断是上层职责） |
| `TestMockServer_TLSHandshakeFailure` | 自签 TLS → 客户端拒绝 |
| `TestMockServer_DuplicateNameAcrossServers` | 两实例同名工具（投影重名用例消费） |

## 偏差与说明（相对任务卡）

1. **SSE 形态**：任务卡提到"两种形态"若指传输则为 Streamable HTTP + SSE。本包目前只提供 **Streamable HTTP**
   （一期主传输，SDK 亦以它为主）；SSE 的客户端兼容性已由 M0-04 自身的 `TestConnect_SSE_Handshake` 覆盖
   （仓内既有用例直接用 `mcp.NewSSEHandler` 起服务）。如需 E2E 级 SSE mock，可在 M2-05 复用同一 Config 增加 `-sse` 开关。
2. **`-mode` 与夹具正交**：故障模式是 HTTP 层注入（不改工具行为），因此可与任意夹具组合（例如 `-mode protocol_mismatch -tools schema-b`）。
3. **`internal` 模式**未在单测中单列用例（401/断连/500 属同类 HTTP 层失败，已由前两者覆盖）；独立进程可用 `-mode internal` 手工验证。
4. **协议版本改写**实现为响应体重写（仅 initialize 响应），避免为 mock 维护第二套协议栈。

## 未覆盖 / 待办

- **M0-14**：以本 mock 跑通"新增服务器 → 测试连接 → 启用 → 工具发现 → 治理启用 → 对话只读调用 → 审计可查"端到端，并归档管理页截图。
- **M2-05**：Playwright E2E 接入 `cmd/mcp-mockserver`（控制面已就绪：`/__mock/tools` 切换夹具、`/__mock/calls` 断言调用）。
- 可选增强：SSE 形态开关、`notifications/tools/list_changed` 的客户端侧断言（当前只断言"重新发现可见变更"）。
