# MCP 外部工具接入 — 运维手册（Runbook）

> 文档类型：运维手册（M2-06 交付物）
> 适用范围：`mcp.enabled=true` 的部署（MCP 外部工具接入功能）
> 关联文档：实施方案 `docs/plan/itsm-mcp-external-tool-integration-implementation-plan-2026-09-27.md` §8.4 / §4.3 M2-06；告警规则样例 `docs/ops/mcp-alert-rules.yml`；运维证据 `docs/plan/evidence/mcp-m2/M2-06-integration-evidence.md`
> 编制日期：2026-09-27
> 演练状态：**关键路径已自动化复现**（`itsm-backend/mcp/manager/drill_test.go`，见 §3.4）；含管理页点击的人工桌面演练归 M2-05/M2-07 出口

## 1. 快速定位（先看这三处）

| 问题 | 入口 | 说明 |
| --- | --- | --- |
| 服务器现在什么状态 | `GET /api/v1/ai/mcp-servers`（权限 `mcp:read`） | `status`（disabled/configured/connecting/healthy/error）+ `policy`（有效超时/并发/重试/预算）|
| 为什么失败 | `GET /api/v1/ai/mcp-servers/:id/events` | 事件流：`connected/disconnected/reload_failed/auth_required/health_alert/disable_grace_expired` + `mcp.tools.budget_exceeded` |
| 哪些工具生效/隔离 | `GET /api/v1/ai/mcp-servers/:id/tools` | `enabled/read_only/risk/quarantined/state` |

> 权限：读端点 `mcp:read`、治理写 `mcp:admin`（默认仅 `sysadmin`/`admin`），工具**执行**另需 `mcp:read`/`mcp:write` + Gate3（审批）；职责分离见实施方案 §4.1 M0-10。

## 2. 变更与日常操作

1. **新增服务器**：管理页「新增」或 `POST /api/v1/ai/mcp-servers`（默认 `enabled=false`，默认拒绝）；
2. **校验连通性**：`POST /api/v1/ai/mcp-servers/:id/test`（同步短超时 ≤10s，不落启用态）；
3. **启用/禁用/重载**：`POST …/:id/enable|disable|reload` → **202** + 轮询 `GET …/:id` 读回终态（异步语义，D8）；
4. **工具治理**：`POST …/:id/tools/:callable/enable|disable`、`POST …/:id/tools/bulk`（批量）；隔离解除前必须复核 schema 变更；
5. **凭据轮换**：`POST …/:id/rotate-credential`（只写不读回；写入即加密存储 AES-GCM）。

## 3. 应急处置

### 3.1 凭据泄露（服务器侧 token 泄露）

按**层级从外到内**执行，每层都可独立止血：

| 层级 | 动作 | 命令/位置 | 生效时间 | 影响 |
| --- | --- | --- | --- | --- |
| L1 | 全局关闭 MCP | 配置 `mcp.enabled=false`（环境变量 `MCP_ENABLED=false`）+ 重启 | 重启后 | 管理路由不注册、工具面不含 MCP（对内置工具零影响） |
| L1.5 | 仅关写工具面 | `mcp.write_enabled=false` + 重启 | 重启后 | 写工具不可见（只读继续可用） |
| L2 | 禁用单台服务器 | `POST /api/v1/ai/mcp-servers/:id/disable` | 202 + ≤30s | 该服务器工具面塌缩、在途调用 ≤ 宽限期（默认 30s）；**新调用立即拒绝** |
| L3 | 停用/隔离工具 | `POST …/:id/tools/:callable/disable` | 即时 | 仅该工具移出工具面，服务器连接不受影响 |
| 轮换 | 换新 token | `POST …/:id/rotate-credential` | 即时 | 旧 token 作废；随后 `reload` 让新凭据生效 |

**标准序列（推荐）**：L2 禁用服务器 → 轮换凭据 → `reload` → 抽查工具面与审计 → 恢复启用。
若疑似平台级泄露（多租户/网关凭据）：L1 立即全局关闭，再逐台排查。

### 3.2 服务器被下线/对端故障

1. `GET …/:id` 看 `status=error` 与 `last_error`；
2. `GET …/:id/events` 看 `reload_failed`（含错误摘要）/`health_alert`（连续失败达阈值 3 的倍数时发出）；
3. 平台有退避重连（默认 1s 起、倍数退避、上限 1min）；对端恢复后无需人工干预；
4. 若确认长期不可用 → L2 禁用（避免无效重连与告警刷屏）。

### 3.3 工具误启用 / 输出异常

- 误启用：`POST …/:id/tools/:callable/disable`（或 `bulk`）→ 工具面即时收缩；
- 输出超限被截断：指标 `itsm_mcp_output_truncated_total` 增长；确认工具设计（分页/过滤参数），必要时调整调用参数而非放宽上限（默认 256KB 为安全边界）；
- 参数/返回值含疑似注入：按安全用例口径（M1-09）判断为「数据透传」而非指令执行；必要时 L3 停用该工具并留证。

### 3.4 演练记录（凭据泄露应急禁用）

**已自动化的关键路径**（`TestDrill_CredentialLeakEmergencyDisable`，见 `docs/plan/evidence/mcp-m2/M2-06-integration-evidence.md` §4）：

1. 建立：服务器启用 + 3 个工具进面（治理位启用）→ 断言连接态指标 = 1；
2. L3：停用 1 个工具 → 工具面 2 个、连接态不变；
3. L2：禁用服务器 → 工具面清空、连接态指标 = 0、**后续调用失败且零下游调用**；
4. 恢复：重新启用 → 仅仍启用的 2 个工具回到工具面（治理位保留）、连接态回到 1。

**人工部分（待补，归 M2-05/M2-07）**：在管理页执行同样序列并截图/录屏；记录处置开始/结束时间、影响面与恢复验证。

## 4. 故障排查

| 症状 | 可能原因 | 处置 |
| --- | --- | --- |
| `auth_required` 事件 / 状态 error | 凭据失效或被拒（401/407） | 轮换凭据 → `reload`；不要靠反复重启（会触发退避） |
| 工具面突然为空 | 服务器非 healthy / 全部工具被停用 / 投影碰撞隔离 | 依次查 `GET …/:id`、`…/:id/tools`（`quarantined` 原因）、`…/:id/events`（`tool.quarantined`） |
| 隔离工具堆积 | schema 变更后被隔离（防提示注入/漂移） | 复核该工具 schema 变更内容 → 管理页解除隔离（治理动作为 `mcp:admin`） |
| 调用频繁超时 | 对端慢/参数过重/并发饱和 | 看 `itsm_mcp_concurrency_in_use/limit`、P95 时延；调 `max_parallel_calls` 或服务器 `timeout_ms` |
| 输出被截断 | 结果超 256KB | 用工具自身的分页/过滤参数；不要把上限调大当解 |
| 禁用后调用仍报错（预期内） | 在途调用宽限（≤30s） | 等 `disable_grace_expired` 事件（若有在途）；确认无新调用 |
| 管理路由 404 | `mcp.enabled=false` 或装配失败 | 查启动日志 `MCP 组件装配失败`（凭据密钥/存储错误） |

## 5. 指标与告警

抓取：后端既有 `/metrics`（需鉴权）。指标定义见 `itsm-backend/metrics/mcp_metrics.go`，规则样例见 `docs/ops/mcp-alert-rules.yml`。

| 指标 | 含义 | 典型用途 |
| --- | --- | --- |
| `itsm_mcp_server_connected{server}` | 连接态（1/0） | 可用性告警 |
| `itsm_mcp_handshake_duration_seconds{server}` | 建连+握手耗时 | P95 慢握手告警 |
| `itsm_mcp_tool_calls_total{server,tool,outcome}` | 调用计数（ok/error/timeout/canceled） | 失败率 |
| `itsm_mcp_tool_call_duration_seconds{server,tool}` | 调用耗时 | P50/P95 |
| `itsm_mcp_output_truncated_total{server,tool}` | 截断次数 | 工具设计复核 |
| `itsm_mcp_tools{server,state}` | 工具治理状态计数 | 隔离堆积告警 |
| `itsm_mcp_concurrency_in_use/limit{server}` | 并发使用率 | 扩容判断 |
| `itsm_mcp_tool_face_tools/tokens{tenant_id}` | 租户工具面规模（M2-03 预算） | 预算趋势；事件流另有 `mcp.tools.budget_exceeded` |

**说明**：指标标签只含 `server/tool/outcome/state/tenant_id`，**不含参数内容**（避免指标成为泄露面）；凭据/明文 token 永不出现在指标与事件中。

## 6. 审计与取证

| 需要什么 | 哪里取 |
| --- | --- |
| 谁改了 MCP 配置/治理 | `audit_logs`（`resource=mcp`；管理写操作全部落审计，含 before/after 脱敏快照） |
| 某次工具调用（含来源三元组） | `tool_invocations`（provider/server/tool + 参数脱敏 `args_redacted` + 状态/耗时/错误码/输出摘要） |
| 生命周期与告警事件 | `GET …/:id/events`（事件缓冲）+ 后端日志 |
| 凭据是否泄露到日志/指标 | 反向检查：`tool_invocations.output_summary`、事件 `Detail`、`/metrics` 均不含明文（M1-09 负向用例覆盖） |

## 7. 回滚路径（与 §3.1 同源）

`mcp.enabled`（L1）→ `mcp.write_enabled`（L1.5）→ 服务器 `disable`（L2）→ 工具 `disable`/隔离（L3）。
**判据**：L1/L1.5 为配置级（需重启），L2/L3 为运行级（202 + 状态回读，无需重启）。

## 变更记录

| 日期 | 变更 |
| --- | --- |
| 2026-09-27 | 首次编制（M2-06）：快速定位 / 日常操作 / 应急处置（凭据泄露四级处置 + 演练记录）/ 故障排查 / 指标与告警 / 审计取证 / 回滚路径 |
