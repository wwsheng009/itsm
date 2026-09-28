# B0-07 实施证据（B0 出口集成验收）

> 文档类型：实施证据（任务 B0-07 / 里程碑 B0 出口）
> Status: draft
> 编制日期：2026-09-27
> 任务：B0-07（B0 集成验收；无生产代码改动）
> 关联文档：`docs/plan/ai-bot-capability-landing-implementation-plan-2026-09-27.md`（§3.1 B0 出口、§4.1 B0-07、§5.2 AB0-01～AB0-07、§5.3 BT-01～BT-04）、`docs/plan/itsm-mcp-external-tool-integration-implementation-plan-2026-09-27.md`（M0-03 联合迁移窗口）
> 核查方式：本机真实 ent + SQLite 端到端测试 + 逐条验收项证据索引 + 与 MCP M0-03 联合评审记录

## 1. 结论

| 项 | 判定 | 说明 |
| --- | --- | --- |
| B0-07 | **`integration_verified`** | 新增 `itsm-backend/tests/botintegration/b0_flow_test.go`（3 用例，真实 ent/SQLite，4.2s 全绿）；证据包按 AB0-01～AB0-07 逐条归档 |
| **B0 里程碑** | **`integration_verified`（条件达标）** | 出口①②③④⑤⑥全部满足；**唯一条件项**：AB0-02 的 Postgres 侧按 `evidence/mcp-m1/gap-register-2026-09-27.md` A1 登记为「环境不可闭环」，CI job（S1，service container）已就位、首轮观察态，不阻塞 B0 出口（与 MCP M0 同口径） |

## 2. 本轮新增（B0-07 端到端验收测试）

文件：`itsm-backend/tests/botintegration/b0_flow_test.go`（无生产代码改动）。

| 用例 | 覆盖判据 | 关键断言 |
| --- | --- | --- |
| `TestB0Flow_EndToEnd` | AB0-01/03/05/06 主链路 | ①提交写工具 → pending 记录一次落库：`risk/category` 元数据快照、`conversation_id` 回填、幂等键 hash 落库、`args_redacted` 无明文口令且常规字段保留；②重复提交同参数 → 命中首次记录（`idempotentReplay=true`）且**记录数不变**；③审批通过 → 队列执行 → `status=done` 且 provider **恰好调用一次**；④审批拒绝 → `rejected` 且不触发执行；⑤按会话维度可检索该会话全部调用；⑥拒绝原因结构化回填会话消息（合法 JSON、`type=tool_approval_decision`） |
| `TestB0Flow_DryRunZeroWrite` | AB0-04 | `create_ticket` dry-run：返回 `*service.ToolPreview`（`mode=create`、`version` 非空）；`tickets` 表计数**前后一致**（零业务写入）；预览记录 `status=preview`、`needs_approval=false`、不进审批待办 |
| `TestB0Flow_StrictProfileAndMCPAnnotation` | AB0-06 + AB0-01（标注解析） | strict 档工具：`args_redacted` 为掩码信封（`masked:true` + 键名），密钥与自由文本值均不落库；default 档 provider 工具：非敏感字段可读 → 锁定 `GetToolForTenant` 的档位解析修复（若退化为 strict 该断言失败） |

运行记录（本机，pwsh）：

```text
$gf = Join-Path (go env GOPATH) 'bin\gofumpt.exe'; & $gf -w ./tests/botintegration
go test ./tests/botintegration/ -count=1 -v -timeout 15m
→ --- PASS: TestB0Flow_EndToEnd (1.04s)
  --- PASS: TestB0Flow_DryRunZeroWrite (1.02s)
  --- PASS: TestB0Flow_StrictProfileAndMCPAnnotation (0.83s)
  ok  itsm-backend/tests/botintegration  4.222s   （exit 0）
```

**过程记录（真实失败与修复，保留以便复现）**
1. 首版断言 `NeedsApproval == int64(0)` 编译失败（字段为 `bool`）；改为 `assert.False`。
2. dry-run 首版假设「返回 `map` 且不落库 ID」，实测返回 `*service.ToolPreview` 且**预览记录落库**（`status=preview`）——按真实契约改写断言（预览记录可审计但不进审批队列），而非放宽实现。
3. 审批后执行首轮 `status=failed / errorCode=not_supported`：`ToolRegistry` 对 provider 写工具要求实现 `WriteCapableProvider`（`service/tool_provider.go:55`）。桩 provider 补齐 `ExecuteApprovedWrite`（声明能力，仍单次执行）后通过——该失败反向验证了「provider 写执行必须显式声明能力」的护栏真实生效。

## 3. AB0-01～AB0-07 证据索引（B0 出口逐条核对）

| 验收项 | 目标级别 | 本轮判定 | 证据 |
| --- | --- | --- | --- |
| AB0-01 元数据全量标注 + 快照 | `unit_verified` | 达标 | `evidence/bot-b0/BP5-B0-01-unit-evidence.md`（14/14 标注 + 缺标注守卫）；集成侧由本轮 `TestB0Flow_EndToEnd`/`StrictProfileAndMCPAnnotation` 补「快照进 `ToolInvocation`」与「provider 标注解析」 |
| AB0-02 迁移双路径 + 联合窗口 | `integration_verified` | **条件达标** | `evidence/bot-b0/B0-02-B0-03-integration-evidence.md`（空库/旧库双路径 + 幂等唯一索引，SQLite 实测）；`evidence/mcp-m0/M0-03-migration-evidence.md`（与 MCP 同一次迁移、字段清单联合冻结）；**Postgres 侧** = gap A1（本机无 Docker/PG，CI job S1 已就位） |
| AB0-03 会话归属 + 拒绝回填 + 按会话回溯 | `integration_verified` | 达标 | `evidence/bot-b0/B0-02-B0-03-integration-evidence.md`；本轮 e2e 复验：`conversation_id` 回填、按会话检索 2 条、拒绝原因回填消息 |
| AB0-04 dry-run 零业务写入 | `integration_verified` | 达标 | `evidence/bot-b0/B0-04-integration-evidence.md`；本轮 e2e 复验：真实 `tickets` 表计数不变 + 预览快照 `version` |
| AB0-05 幂等键 + 幂等回放 + 跨租户隔离 | `unit_verified` | 达标 | `evidence/bot-b0/B0-05-integration-evidence.md`（UT 4 例 + 真实 SQLite 回查）；本轮 e2e 复验：同参数重复提交不新增记录 |
| AB0-06 脱敏（不落明文 + strict 档） | `unit_verified` | 达标 | `evidence/bot-b0/B0-06-integration-evidence.md`（表驱动 + 落库断言）；本轮 e2e 复验：真实落库行无明文、strict 档只留键名 |
| AB0-07 证据包齐全 + 联合评审 | `integration_verified` | 达标 | 本文档（§2 测试记录 + §3 索引 + §4 联合评审 + §5 QA 抽检） |

## 4. 与 MCP M0-03 的联合评审记录（S1 / BP2 / BQ8）

| 项 | 结论 |
| --- | --- |
| 迁移窗口 | **同一次迁移**：`tool_invocations` 的 B0 字段（metadata 快照 / `conversation_id` / 幂等键 / 脱敏与结果字段）与 MCP M0-03 的 provider 三元组字段一次性加列，无二次迁移（S1） |
| 字段清单 | 联合冻结：B0 侧字段与 MCP 侧字段互不重名冲突；同名语义统一（`args_redacted` 为两侧唯一命名，B0-02 登记回写） |
| 兼容策略 | 新字段一律带默认值/nullable；关闭态（`bot.enabled=false` / `mcp.enabled=false`）行为与现状零差异 |
| 验证方式 | SQLite 侧双路径（空库/旧库）**已实测**；Postgres 侧由 CI `mcp-postgres-migrations` job 承载（S1，首轮 `continue-on-error`，绿跑后转阻断门） |
| 结论 | 联合窗口 **达成**；Postgres 实测为**环境外因**（gap A1），与 MCP M0 出口同口径登记，不重复计为 B0 缺口 |

## 5. QA 抽检清单（≥30% 可复跑，逐条给命令）

| # | 复跑对象 | 命令（`itsm-backend/`） | 本轮 |
| --- | --- | --- | --- |
| Q1 | B0 端到端（本任务） | `go test ./tests/botintegration/ -count=1 -v -timeout 15m` | ✅ 已跑（4.2s） |
| Q2 | 元数据守卫 + 快照（AB0-01） | `go test ./handlers/ai/ -run 'TestB0_01|TestListToolInvocations' -count=1` | ✅（B0-01 证据轮次） |
| Q3 | 迁移双路径（AB0-02） | `go test ./ent/schema/ -run 'Migration' -count=1` | ✅（B0-02 证据轮次） |
| Q4 | 会话归属与拒绝回填（AB0-03） | `go test ./handlers/ai/ -run 'TestB0_03' -count=1` | ✅（B0-03 证据轮次） |
| Q5 | dry-run 零写入（AB0-04） | `go test ./handlers/ai/ -run 'TestB0_04' -count=1` | ✅ 随 B0-06 轮次复跑 |
| Q6 | 幂等回放（AB0-05） | `go test ./handlers/ai/ -run 'TestB0_05' -count=1` | ✅ 随 B0-06 轮次复跑 |
| Q7 | 脱敏（AB0-06） | `go test ./service/bot/ ./handlers/ai/ -run 'TestRedactor|TestB0_06' -count=1` | ✅ 随 B0-06 轮次复跑 |
| Q8 | 受影响包全量 | `go test ./handlers/ai/ ./service/bot/ -count=1 -timeout 20m` | ✅ 已跑（14.3s / 0.27s 全绿） |

抽检比例：8/8 可复跑条目本轮可执行（≥30% 要求）。

## 6. 未闭环与残留（如实登记）

1. **Postgres 双驱动实测**：gap A1 外因项（本机无 Docker/Postgres；`mcp_postgres_migrations` job 已就位）；MPP 侧同源登记，B0 出口按条件达标。
2. **strict 档结果摘要下推执行侧**：当前 `mcp/provider`、`service/tool_queue` 的 `output_summary` 生成仍走 default 原语；审计**写入层**已按档位全掩码（B0-06 已闭环「不落明文」），执行侧收敛登记为 B1 后续项。
3. **Windows 本机既有 flake**：`pkg/seeder` / `TempDir` 句柄类失败（gap S5，非 B0 范围）。
4. **本任务为证据与验收任务**：未改生产代码；新增测试仅入库 `tests/botintegration/`。

## 7. 变更记录

| 日期 | 变更 | 说明 |
| --- | --- | --- |
| 2026-09-27 | 初稿 | B0-07 交付：端到端验收测试（3 用例）+ AB0-01～AB0-07 证据索引 + 与 MCP M0-03 联合评审记录 + QA 抽检清单；B0 里程碑判定 `integration_verified`（条件达标，Postgres 侧按 gap A1） |
