# M2-06 指标、告警与运维手册 — 集成验证证据

> 文档类型：验收证据（集成验证）
> 任务：M2-06「指标、告警看板与运维手册（运维）」（实施方案 §4.3）
> 验收项：A2-06（`integration_verified`）
> 编制日期：2026-09-27
> 状态级别：`integration_verified`

## 1. 交付物

| 交付物 | 路径 | 说明 |
| --- | --- | --- |
| MCP 指标定义 | `itsm-backend/metrics/mcp_metrics.go` | 10 组指标（连接态/握手耗时/调用计数与耗时/截断/工具状态/并发使用率/租户工具面规模）；标签只含 `server/tool/outcome/state/tenant_id`，**不含参数内容** |
| 指标接线（管理器） | `itsm-backend/mcp/manager/{manager.go,health.go,pool.go,budget.go}` | 建连成功观测握手耗时并置连接态 1；会话关闭（健康失败/禁用/删除/重载共用路径）置 0；发现后写工具状态计数；并发 acquire/release 写使用率；预算判定写租户工具面规模 |
| 指标接线（执行面） | `itsm-backend/mcp/provider/execute.go` | 每次下游调用观测耗时 + 计数（`ok/error/timeout/canceled`）；结果被截断单独计数 |
| 告警规则样例 | `docs/ops/mcp-alert-rules.yml` | 1 组 10 条规则（断流/连接不可用/握手慢/失败率/时延/工具面预算/token 占比/隔离堆积/截断频繁/并发饱和） |
| 运维手册 | `docs/ops/mcp-runbook.md` | 快速定位 / 日常操作 / 应急处置（凭据泄露四级处置）/ 故障排查 / 指标与告警 / 审计取证 / 回滚路径 |
| 演练（自动化复现） | `itsm-backend/mcp/manager/drill_test.go` | `TestDrill_CredentialLeakEmergencyDisable`：runbook §3.1 的 L2/L3/恢复路径逐步断言 |

## 2. 指标清单（与手册 §5 同源）

| 指标 | 类型 | 标签 | 采集点 |
| --- | --- | --- | --- |
| `itsm_mcp_server_connected` | Gauge | server | 建连成功=1；`closeSessionWithEvent`=0 |
| `itsm_mcp_handshake_duration_seconds` | Histogram | server | 建连成功路径（失败不进入分布） |
| `itsm_mcp_tool_calls_total` | Counter | server, tool, outcome | provider 调用返回处 |
| `itsm_mcp_tool_call_duration_seconds` | Histogram | server, tool | 同上（仅下游调用段） |
| `itsm_mcp_output_truncated_total` | Counter | server, tool | 结果超 256KB 上限被截断时 |
| `itsm_mcp_tools` | Gauge | server, state(enabled/disabled/quarantined) | 每次发现刷新 |
| `itsm_mcp_concurrency_in_use` / `itsm_mcp_concurrency_limit` | Gauge | server | 并发额度 acquire/release |
| `itsm_mcp_tool_face_tools` / `itsm_mcp_tool_face_tokens` | Gauge | tenant_id | 预算判定（M2-03 `EvaluateToolBudget`） |

暴露路径：后端既有 `/metrics`（`router.go` 的 `metricsAuth` 分组挂 `promhttp.Handler()`），无需新增端点。

## 3. 验证记录（本机实跑）

| 命令 | 结果 |
| --- | --- |
| `go test ./metrics/ -count=1 -v` | **ok**：`TestMCPMetrics_暴露断言`（10 个指标名均出现在默认注册表 `Gather()` 输出）、`TestMCPMetrics_注册与写入`、`TestMCPCallOutcome_常量` |
| `go test ./mcp/provider/ -count=1` | **ok 9.2s**（含 `TestProvider_MetricsOnCall`：ok/error 分类 + 截断计数 + 取消/超时分类） |
| `go test ./mcp/manager/ -count=2 -run TestDrill_CredentialLeakEmergencyDisable` | **ok**（演练用例 2 连跑稳定） |
| `go test ./metrics/ ./mcp/... ./config/ -count=1` | **10 包全 ok**（metrics 0.9s / admin 16.2s / budget 0.4s / client 5.5s / manager 2.7s / provider 9.2s / registry 0.6s / mockserver 3.4s / transport 0.6s / config 1.1s） |
| `go build ./...` / `go vet` | exit 0 |
| `gofumpt -l ./mcp ./metrics ./config`（CI 定版 v0.7.0） | 无输出 |
| `staticcheck ./mcp/... ./metrics/ ./config/`（CI 同款 v0.6.1） | exit 0 |
| 告警规则 YAML 解析（`js-yaml`） | **通过**：`groups=1 rules=10`，逐条列出 alert/for/severity（见 §5） |

## 4. 演练记录（A2-06 的「凭据泄露应急禁用」）

**自动化复现**（`TestDrill_CredentialLeakEmergencyDisable`；对应 runbook §3.1/§3.4）：

| 步骤 | 动作（runbook 对应） | 断言 |
| --- | --- | --- |
| 0 | 建立：服务器启用 + 3 个工具进面（治理位启用） | 有效工具面 3；`itsm_mcp_server_connected=1` |
| 1 | **L3** 停用 1 个工具 | 工具面 2；连接态仍为 1（L3 不影响连接） |
| 2 | **L2** 禁用服务器 | 工具面塌缩为 0；连接态指标=0；随后 `CallTool` 失败且**下游调用计数不增加**（零下游调用） |
| 3 | 恢复：重新启用 | 仅**仍启用**的 2 个工具回到工具面（治理位保留）；连接态回到 1 |

**命令**：`go test ./mcp/manager/ -run TestDrill_CredentialLeakEmergencyDisable -count=1 -v` → `--- PASS`。

**范围说明（诚实登记）**：本演练覆盖 runbook 的**运行级**处置（L2/L3）与恢复语义；L1/L1.5（`mcp.enabled` / `mcp.write_enabled`）为配置级开关，其语义由 M0-01/M1-02 的测试与 §5.4 回滚口径覆盖，演练中不重启进程验证。含**管理页点击与截图/录屏**的人工演练归 M2-05（浏览器 E2E 设施就绪后）与 M2-07 出口，未在本任务虚报。

## 5. 告警规则清单（解析结果）

| 规则 | for | severity | 触发口径 |
| --- | --- | --- | --- |
| `ItsmMCPMetricsMissing` | 15m | warning | `count(itsm_mcp_server_connected) == 0`（功能未启用或抓取失败） |
| `ItsmMCPServerDisconnected` | 5m | critical | `itsm_mcp_server_connected == 0` |
| `ItsmMCPHandshakeSlow` | 15m | warning | 握手 P95 > 5s |
| `ItsmMCPCallFailureRateHigh` | 10m | critical | error/timeout/canceled 占比 > 20%（低流量钳制） |
| `ItsmMCPCallLatencyHigh` | 10m | warning | 调用 P95 > 10s |
| `ItsmMCPToolFaceBudgetExceeded` | 30m | warning | `itsm_mcp_tool_face_tools > 40` |
| `ItsmMCPToolFaceTokenShareHigh` | 30m | warning | `itsm_mcp_tool_face_tokens / 128000 > 0.30` |
| `ItsmMCPToolsQuarantined` | 15m | warning | `itsm_mcp_tools{state="quarantined"} > 0` |
| `ItsmMCPOutputTruncatedFrequent` | 15m | info | 截断速率 > 0.1/s（10m） |
| `ItsmMCPConcurrencySaturated` | 10m | warning | 并发使用率 > 90% |

> 阈值与 `mcp.tools_budget` / `mcp.tools_context_tokens` / `mcp.tools_token_share` 默认值对齐；部署侧改配置时需同步规则表达式（文件头已注明）。

## 6. 边界与已知限制

1. **指标不含每租户调用标签**：调用类指标按 `server/tool` 聚合（不带 `tenant_id`），避免高基数；租户维度看工具面规模指标与 `tool_invocations` 查询。
2. **握手耗时只记成功路径**：失败握手不计入分布（失败由连接态与失败率反映），避免把失败样本混进时延 SLA。
3. **`ItsmMCPMetricsMissing` 为启发式**：`mcp.enabled=false` 时进程不装配 MCP 组件、不产生序列，该规则会持续告警——部署时应按「是否启用 MCP」决定是否加载本条（或加 `absent` 关联的业务指标作为启用信号）。
4. **未做**：Grafana 看板 JSON（团队按指标清单自建）、告警通知路由与静默策略（属部署侧配置）、外部通知渠道（M2-06 卡内已注明归后续）。
5. **人工桌面演练**（浏览器点击 + 记录）归 M2-05/M2-07，见 §4 范围说明。

## 7. 证据锚点

| 内容 | 位置 |
| --- | --- |
| 指标定义与结果分类常量 | `itsm-backend/metrics/mcp_metrics.go` |
| 暴露断言与注册断言 | `itsm-backend/metrics/mcp_metrics_test.go` |
| 连接态/握手/工具状态/并发接线 | `itsm-backend/mcp/manager/manager.go`、`health.go`、`pool.go` |
| 工具面规模接线 | `itsm-backend/mcp/manager/budget.go` |
| 调用与截断接线 | `itsm-backend/mcp/provider/execute.go`（`metricsCallOutcome`） |
| 调用指标用例 | `itsm-backend/mcp/provider/metrics_test.go` |
| 演练用例 | `itsm-backend/mcp/manager/drill_test.go` |
| 告警规则 | `docs/ops/mcp-alert-rules.yml` |
| 运维手册 | `docs/ops/mcp-runbook.md` |

## 变更记录

| 日期 | 变更 |
| --- | --- |
| 2026-09-27 | 首次登记：M2-06 指标接线（10 组）、告警规则样例（10 条）、运维手册、应急禁用演练自动化复现；A2-06 判定 `integration_verified`（人工浏览器演练归 M2-05/M2-07） |
