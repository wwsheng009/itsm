# M0-14 证据（M0 集成验收：只读链路端到端 + 负向 + 跨租户）

> 验收项：A0-14（对应任务 M0-14）｜里程碑 DoD：M0 = `integration_verified`
> 实际状态：**`integration_verified`**（真实 ent + SQLite + M0-13 mock MCP 服务器，一条链路跑通
> 「新增服务器 → 测试连接 → 启用 → 工具发现 → 治理（标注只读 + 启用）→ 只读调用 → 审计可查」，
> 并叠加 SSRF / 认证失败 / 输出超限 / 跨租户负向；套件连续 3 次运行全绿）。
> 遗留（1 项，不影响本判定）：**管理页人工浏览器截图**待补——需真实后端进程 + 登录会话，
> 计划与 M2-05（Playwright E2E 复用 `cmd/mcp-mockserver`）一并归档。
> 执行人 / 环境：AI 辅助执行；Windows + pwsh，go1.25.13，基线 `743b0868`（分支 `feat/bot-mcp-integration`）
> 时间：2026-09-27

## 交付物

| 文件 | 说明 |
| --- | --- |
| `itsm-backend/tests/mcpintegration/mcp_flow_test.go`（新增，~420 行） | M0 端到端集成套件：3 个用例（主链路 / SSRF 严格模式 / 认证失败），真实装配 `ent + admin.Service + manager + provider + ToolRegistry + ai.Service + mockserver` |

## 主链路断言（`TestM0Flow_EndToEnd`）

| 步骤 | 断言 |
| --- | --- |
| 1 新增 | 默认 `enabled=false`、`status=configured`；`credential_masked` 只回掩码，明文 `super-secret-token` 不出现 |
| 2 测试连接 | `ok=true`、协议版本非空、ServerName=`itsm-mcp-mock`、工具预览 6 条（default 夹具） |
| 3 启用 | 202 语义；20s 内进入 `healthy`（= 连接 + 工具发现完成，事件 `mcp.server.connected` + `mcp.tools.discovered added=6`） |
| 4 发现 | 6 条工具落库；`mcp__mock__list_issues` 存在且未隔离；新工具默认不启用（D7）；非法字符名工具 `bad name!` 的原始名保留、投影名规范化为 `mcp__mock__bad_name_<hash>`（无空格、可调用） |
| 5 治理 | 标注 `read_only=true`（D7 默认按写，未标注不进只读工具面）+ 启用 → 进入工具面 |
| 6 调用 | 经 `ai.Service.ExecuteTool`（Gate1/Gate2 同源编排）实际打到 mock；`pendingID=0`（只读免审批）；mock 侧记录到 `list_issues(state=open)` |
| 7 审计 | `tool_invocations` 一行：`provider=mcp`、server=`mock`、raw=`list_issues`、callable 三元组、`status=executed`、`duration_ms≥1`、`args_redacted` 掩码且无明文 |
| 8 管理审计 | `audit_logs`（resource=mcp）≥3 条（创建/启用/工具治理 + 分类标注） |
| 9 跨租户 | 另一租户 `ListServers` 为空、`GetServer` → 404 |
| 10 输出超限 | `huge_output`（512KiB）被 provider 截断：`Truncated=true`、`Bytes≤4096`（`MaxResultBytes` 配置生效） |

## 负向断言

| 用例 | 断言 |
| --- | --- |
| `TestM0Flow_SSRFBlockedWithStrictGuard` | 严格 guard（`SSRFConfig{}`：仅 https + 公网）：配置阶段不解析 DNS（创建可成功），**建连阶段**（`TestServer`）拒绝为 `ssrf_blocked`（422）；错误文本不含 `127.0.0.1` |
| `TestM0Flow_AuthRequiredOnTestConnection` | mock 401 注入：测试连接以契约错误 `auth_required` 返回（消息含"认证"，不泄露凭据） |

## 执行记录

| # | 命令（workdir=`itsm-backend`） | 结果 |
| --- | --- | --- |
| 1 | `gofmt -l ./tests/mcpintegration` | 无输出 |
| 2 | `go vet ./tests/mcpintegration/ ./mcp/provider/ ./mcp/testutil/... ./cmd/mcp-mockserver/` | exit 0 |
| 3 | `go build ./...` | exit 0 |
| 4 | `go test ./tests/mcpintegration/ ./mcp/provider/ ./mcp/testutil/mockserver/ -count=1 -timeout 300s` ×3 | **3/3 全绿**（0 failures；单次约 3–5s 测试 + 编译） |
| 5 | `go test ./tests/mcpintegration/ -v` | 3 用例 PASS（含逐工具发现日志：6 工具 / 投影名 / 风险默认 high） |

> 稳定性说明：首次运行曾命中 `duration_ms=0`（亚毫秒往返），已在 `mcp/provider/execute.go` 修正为
> 「成功调用耗时口径 ≥1ms」（亚毫秒记 1ms），随后连续 3 次全绿。

## 过程中的真实缺陷（本轮修复）

| 缺陷 | 影响 | 修复 |
| --- | --- | --- |
| 前端传输取值写成 `streamable_http` | 与后端 `transport.Kind`（`streamable`/`sse`）不一致 → 新建服务器必 400 `invalid_transport` | `lib/api/mcp-api.ts` 类型改 `'streamable' \| 'sse'` + 新增 `MCP_TRANSPORTS` 常量；页面下拉改用该常量；`mcp-api.test.ts` 增加取值守卫用例；页面测试夹具同步 |
| provider 亚毫秒调用 `duration_ms=0` | 审计出现 0 耗时，无法区分"未记录"与"极快" | `mcp/provider/execute.go`：`DurationMs==0 → 1` |

## 偏差与说明

1. **测试环境放行环回**：mock 跑在 `127.0.0.1`，故 harness 用 `SSRFConfig{AllowHTTP:true, AllowPrivate:true, AllowedPorts:[mockPort]}`（仅测试）；
   严格模式（生产默认）的拒绝能力由 `TestM0Flow_SSRFBlockedWithStrictGuard` 独立断言。
2. **SSRF 校验时机**：实现上出站校验发生在 `transport.New`（建连）与每个请求（`RoundTripper` 复查），
   而非 `CreateServer`（配置阶段不做 DNS 解析）。负向断言因此落在 `TestServer`；`CreateServer` 仍做 URL 形态校验
   （协议/主机/不得含 userinfo）。
3. **read_only 默认按写**：MCP 协议的 `annotations.readOnlyHint` 不作为授权依据（D7 默认拒绝），
   必须由管理员在治理面标注 `read_only=true` 后工具才进只读工具面；本套件如实断言该行为（与 M1-01 口径一致）。
4. **非法字符名处理**：投影层将 `bad name!` 规范化为 `mcp__mock__bad_name_<hash>`（未隔离）；
   隔离（quarantine）主要覆盖 canonical 碰撞 / 空名 / 非法服务器名（M0-02）。本套件断言"原始名保留 + 投影名安全可调用"。
5. **管理页截图待补**（见文首遗留）。

## 未覆盖 / 待办

- **管理页人工冒烟截图**（A0-14 证据集的一部分）：需真实后端进程（`mcp.enabled=true` + 加密密钥）+ 登录会话；
  建议与 M2-05 Playwright E2E 一并完成（可复用 `cmd/mcp-mockserver` 与控制面）。
- M0 里程碑收口说明：M0-01～M0-14 全部达 `unit_verified` 及以上，其中 M0-08/09/10/11/14 = `integration_verified`；
  M0-12 = `unit_verified`（截图待补）；M0-13 = `unit_verified`。
- 未做 `-race` 全量回归（本机耗时不可控），建议在 CI 上补 `go test -race ./mcp/... ./tests/mcpintegration/`。
