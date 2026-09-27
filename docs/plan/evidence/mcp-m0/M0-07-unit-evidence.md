# M0-07 证据（连接生命周期管理 / manager）

> 验收项：A0-07（对应任务 M0-07）｜目标 DoD：`integration_verified`（本卡实际达成见下）
> 实际状态：**`unit_verified` + 进程内真实装配集成证据**；按方案 §1.3 口径，「`integration_verified`（组件间集成测试通过，含真实 ent/DB）」中的 **ent/DB 部分需 M0-08 提供 `StatusWriter`/`ToolCache` 的 ent 实现**，故正式提升由 M0-14 统一判定（不跳级）。
> 执行人 / 环境：AI 辅助执行；Windows + pwsh，`go1.25.13 windows/amd64`，基线 `7442fad5`（分支 `feat/bot-mcp-integration`）
> 时间：2026-09-27

## 变更文件

- 新增 `itsm-backend/mcp/manager/events.go`：事件类型（`mcp.server.connected|disconnected|auth_required|reload_failed`、`mcp.tools.discovered`、`mcp.tool.state_changed`、`mcp.tool.quarantined`）、`EventSink` 与测试用 `MemoryEventSink`；
- 新增 `itsm-backend/mcp/manager/pool.go`：`ServerStatus`/`StatusSnapshot`、`toolCaller` 接口（抽象 `client.Session`，可注入假实现）、`DialFunc`、`BackoffPolicy`（指数退避）、单服务器连接槽（会话 + 信号量 + 退避状态）；
- 新增 `itsm-backend/mcp/manager/discovery.go`：`SchemaHash`（键序无关，非法 JSON 退回字节哈希）、`PlanDiscovery`（差分 + 治理位保留 + schema 变更隔离）、`ToolCache`/`MemoryToolCache`；
- 新增 `itsm-backend/mcp/manager/manager.go`：`Manager`（Upsert/Remove/Enable/Disable/Reload/ConnectNow/DiscoverNow/CallTool/Status/Snapshot/CachedTools/EffectiveTools/Start/Stop）、`ServerConfig`、`StatusPatch`/`StatusWriter`、`defaultDial`（transport + client 真实建连，Guard fail-closed）；
- 新增 `itsm-backend/mcp/manager/health.go`：`HealthTick`（Ping 探活 / 退避重连）、`waitIdle`（禁用宽限）、`closeSessionWithEvent`、建连在途标记；
- 测试：`discovery_test.go`（6 用例）、`manager_test.go`（10 用例）、`integration_test.go`（1 集成用例）。

## 执行记录

| # | 命令（itsm-backend 目录） | 结果 |
| --- | --- | --- |
| 1 | `gofmt -l mcp\manager` | 无输出 |
| 2 | `go test ./mcp/manager/ -count=3` | 通过（3 轮重复，1.876s；期间修复两处事件竞态断言） |
| 3 | `go test ./mcp/... -count=1` | 全绿：`admin 1.272s`、`client 2.017s`、`manager 0.986s`、`registry 0.067s`、`transport 0.389s` |
| 4 | `go vet ./mcp/...` | `vet-exit=0` |
| 5 | `go build ./...` | `build-exit=0`（291.9s） |

## 覆盖说明（对照 M0-07 要点）

1. **状态机与异步（D8）**：`disabled → connecting → healthy(=卡片 connected) → error`；`Enable` 返回时状态必为 `connecting`（用闸门 dialer 断言时序），后台完成建连 + 发现；`Disable` 后台等待在途调用（宽限 50ms 测试值）后关闭；`Reload` 先断后连；状态回写 `StatusWriter` 连续 patch（connecting → healthy）。
2. **健康检查与退避**：`HealthTick` 对持有会话者 Ping，失败 → 断连 + `error` + `reload_failed` 事件 + 指数退避；用例断言「退避未到不重试」「到点后自动恢复 healthy」；`Start/Stop` 健康协程幂等。
3. **工具发现与差分**：`PlanDiscovery` 覆盖 Added/Updated/Removed/Quarantined/Unchanged；新工具默认 `enabled=false/read_only=false/risk=high`（D7）；`schema_hash` 变化 → 隔离待复核并**保留治理位**（便于复核后恢复）；`SchemaHash` 对键序不敏感（避免误隔离）；缓存返回拷贝（防外部污染）。
4. **工具开关不重连（D6）**：manager 不提供工具开关入口，重连只由 `Enable/Reload/健康检查` 触发；治理位翻转与「不触发重连」的校验归 M0-08（其调用 registry/DB，不经过 manager）。
5. **生命周期事件**：7 类事件全部实现并在用例中断言（connected/disconnected/auth_required/reload_failed/tools.discovered/quarantined）；`Event.Detail` 已脱敏（`ServerInfo` 断言不含 token）；接入 ITSM 审计/事件设施属 M0-11。
6. **降级**：`EffectiveTools()` = 服务器 healthy ∧ `enabled` ∧ 未隔离；MCP 故障 → 空集合且无 panic（用例断言「塌缩」）；恢复后重新发现。降级不影响内置工具与对话（同进程内 MCP 子系统已与之隔离）。
7. **并发与超时**：每服务器信号量 = `MaxParallelCalls`（6 路并发、40ms 延迟下峰值 = 2 且不超过 2）；`TimeoutMS` 生效（30ms 超时在 250ms 内返回 `context.DeadlineExceeded`，额度归还后可继续调用）；调用不自动重试（与 §6.5-3 一致）。
8. **真实装配集成**：真实 `transport` + `client` + SDK mock 服务器；注入 503 断开 → `error` + 工具面塌缩；恢复后重连 + 重新发现（`connected` 事件 2 次、`reload_failed` ≥1 次）；`Disable` 真实关闭会话。

## 未覆盖 / 待办（M0-08 及以后）

- **ent/DB 落库**：`StatusWriter` 与 `ToolCache` 当前为接口 + 内存/假实现；M0-08 提供 ent 实现（`mcp_servers` 运行态回写、`mcp_server_tools` upsert/隔离/下线）后补跑集成，再由 M0-14 判定 `integration_verified`。
- **审计接线**：事件目前仅内存 sink；M0-11 接审计/事件设施。
- **删除路径宽限**：`waitIdle` 目前仅被 `Disable` 使用；服务器删除（M0-08）需复用同一宽限语义。
- **`-race` 未跑**：`conn.cfg` 的部分读取未全面加锁（Upsert 变更配置时存在理论竞态）；M0-08 固化「配置快照」后补跑 `-race`。
- **读操作重试**：`MaxRetry` 字段尚未接线（当前仅连接级退避，工具调用一律不重试）。
