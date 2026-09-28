# M1-08 证据（运维收口与告警）

> 验收项：A1-08（对应任务 M1-08）｜DoD：`integration_verified`
> 实际状态：**`integration_verified`**（告警阈值、策略回读、读重试/写不重试、禁用/删除 in-flight 宽限全部自动化用例通过）
> 执行人 / 环境：AI 辅助执行；Windows + pwsh，go1.25.13，基线 `d2fd9da9`（分支 `feat/bot-mcp-integration`）
> 时间：2026-09-27

## 交付物

| 文件 | 变更 |
| --- | --- |
| `mcp/manager/events.go` | 新增事件：`mcp.server.health_alert`（连续失败告警）、`mcp.server.disable_grace_expired`（宽限超时强断）；`Event.Failures` 机器可读失败次数 |
| `mcp/manager/pool.go` | `consecutiveFailures` 连续失败计数：失败 `markFailure` 自增、建连成功（`setSession`）/探活成功（`resetFailures`）清零；`setLastError` / `failures` / `maxRetryBudget` 访问器 |
| `mcp/manager/health.go` | 探活/重连成功复位计数；`emitHealthAlertIfNeeded`（建连失败与探活失败**共用同一条计数**；`failures % 阈值 == 0` 时发出，即 3/6/9…，不漏报也不刷屏） |
| `mcp/manager/manager.go` | ① `Options.HealthFailureThreshold`（默认 3）、`Options.ReadRetry`（平台读重试上限，默认 1，负值禁用）；② `CallToolWithPolicy`（读工具至多重试、写工具恒不重试、仅瞬时错误可重试）+ `callOnce` 重构；③ `retryableCallError`（不可达/连接超时/传输层/调用超时=可重试；认证失败/协议不匹配/服务端业务错误/SSRF 拦截/主动取消=不重试）；④ `Policy` 有效策略回读；⑤ `Retire`（删除：立即拒新调用 + in-flight 宽限）；⑥ `waitInFlightAndClose`（禁用/删除共用：宽限超时→记 `last_error` + 告警事件 + 强制断开） |
| `mcp/provider/{provider.go,execute.go}` | 可选接口 `RetryableToolSource`：provider 按工具 `read_only` 传策略（写工具恒 false）；测试替身未实现该接口时退化为不重试的 `CallTool`（向后兼容） |
| `mcp/provider/provider_test.go` | 断言 `DefaultMaxResultBytes == 256*1024`（§5.4 输出上限）与默认注入 |
| `mcp/admin/service.go` | `ServerView.Policy`（`ServerPolicyView`：超时/连接超时/并发/有效读重试/平台上限/宽限/告警阈值/健康间隔）；`DeleteServer` 改走 `Retire`（保留宽限语义） |
| `mcp/admin/policy_test.go`（新增） | 策略回读断言（默认值 → 服务器覆盖 → 平台上限收紧 → 服务器关闭重试） |
| `mcp/manager/ops_test.go`（新增） | 6 个用例：告警阈值与复位、策略默认表、可重试错误分类、读重试/写不重试、禁用宽限与强断、删除宽限 |
| `mcp/manager/manager_test.go` | `fakeSession` 增 `calls` 计数与 `blockCh` 阻塞钩子（确定性构造「在途调用」，消除时序 flake） |

## 口径（与 §5.4 默认表对齐）

| 维度 | 默认 | 实现位置 |
| --- | --- | --- |
| 单次调用超时 | 30s（服务器 `timeout_ms` 覆盖） | `manager.Options.CallTimeout` / `ServerConfig.TimeoutMS` |
| 连接超时 | 10s | `manager.Options.ConnectTimeout` |
| 并发 | 每服务器 4（`max_parallel_calls` 覆盖） | `newConn` + 信号量 |
| 重试 | **读工具至多 1 次**（`min(平台 ReadRetry, 服务器 max_retry)`）；**写工具恒 0** | `CallToolWithPolicy` + `readRetryBudget` |
| 输出上限 | 256KB，超限截断并标记 | `provider.DefaultMaxResultBytes` |
| 禁用/删除宽限 | ≤30s 等在途 → 强断并审计 | `Disable` / `Retire` + `waitInFlightAndClose` |
| 连续失败告警 | 3 次（`failures % 3 == 0` 上报） | `emitHealthAlertIfNeeded` |

**读重试的边界**：仅 `unreachable` / `connect_timeout` / `transport_error` 与调用超时（`context.DeadlineExceeded`）可重试；`server_error`（远端业务错误）不重试——重试可能造成重复副作用且无法自愈。服务器 `max_retry=0`（ent 字段 `Min(0)`，默认 1）可单独关闭该服务器的读重试。

## 执行记录

| # | 命令 | 结果 |
| --- | --- | --- |
| 1 | `go test ./mcp/manager/ -count=3` | **ok**（含 6 个新用例；连跑 3 次稳定） |
| 2 | `go test ./mcp/admin/ -run TestServerView_PolicyReadback` | **ok** |
| 3 | `go test ./mcp/... ./handlers/mcp/ ./handlers/ai/ -count=1` | 9 包全 **ok** |
| 4 | `go build ./...` | exit 0 |

## 判定说明与已知缺口

| # | 项 | 说明 |
| --- | --- | --- |
| 1 | 告警出口 | 告警以**生命周期事件**（`mcp.server.health_alert`）落在 `EventBuffer`（`GET /api/v1/mcp/servers/:id/events` 可查，M0-08 已有）。接入外部通知渠道（邮件/IM）属 M2-06 运维手册与告警对接，本任务只保证**信号产生、去重口径与可查询** |
| 2 | 假时钟 | 计数与阈值判定不依赖时钟（纯计数），因此无需注入假时钟即可确定性断言；宽限与退避的时序用例改用「阻塞会话」而非墙钟竞速，重复执行稳定 |
| 3 | 输出上限位置 | 256KB 上限在 provider 层，**不在** `ServerView.Policy` 内回读（provider 与 admin 之间无装配关系）；已在 provider 用例断言默认值，方案 §4.2 M1-08 的「策略参数回读」以此二分口径满足 |
| 4 | `Remove` 保留 | 仍保留「立即断开」的 `Remove`（装配清理/测试用）；生产删除路径（`DeleteServer`）已改用 `Retire` |
| 5 | 事件持久化 | `EventBuffer` 仅内存环形缓冲（最近 N 条）；落库审计仍走 M0-11 的 `tool_invocations` + 管理操作审计，生命周期事件的持久化未做（与 M0-07 时的边界一致） |
