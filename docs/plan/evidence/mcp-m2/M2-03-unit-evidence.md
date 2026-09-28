# M2-03 工具面预算与元工具评估 — 单元验证证据

> 文档类型：验收证据（单元验证）
> 任务：M2-03「工具面预算与元工具（后端）」（实施方案 `docs/plan/itsm-mcp-external-tool-integration-implementation-plan-2026-09-27.md` §4.3）
> 验收项：A2-03（`unit_verified`）
> 编制日期：2026-09-27
> 状态级别：`unit_verified`（本任务 DoD）
> 证据口径：静态代码 + 本机单元测试输出（**未编译前不可复跑者已标注**）

## 1. 目标与判据

| 判据（§4.3 M2-03 / §5.2 A2-03） | 落地物 | 结果 |
| --- | --- | --- |
| 有效工具数 > 阈值（默认 40）触发告警 | `mcp/manager/budget.go`：`EvaluateToolBudget` → 事件 `mcp.tools.budget_exceeded` | ✅ 通过 |
| 阈值可配 | `config.MCPConfig.ToolsBudget`（`mcp.tools_budget`，默认 40）+ manager `Options.ToolBudget` | ✅ 通过（配置解析 + 默认值双测试钉住） |
| token 占比可测量（>30% 信号） | `mcp/budget`：`EstimateTokens` / `Measure` / `Evaluate`（上下文预算与占比上限均可配） | ✅ 通过 |
| 元工具方案「先评估后灰度」 | `budget.Advise` 给出评估产物（`none` / `meta_tools_candidate` + 建议形态 + 灰度约束）；**不实施**元工具（见 §4） | ✅ 通过（评估） |
| 改动面不越界 | 仅新增 `mcp/budget`、`mcp/manager/budget.go`，扩展 manager Options/Policy、config 三项、admin 策略回读三项、bootstrap 装配；**未改动 provider/registry/执行链** | ✅ 通过 |

## 2. 实现要点

1. **估算口径**（`mcp/budget/estimate.go`）：CJK 按 1 token/字、其余按 4 字符/token（向上取整，非空至少 1）。
   面向阈值告警的保守近似，无外部依赖；如需精确计量，应在 M2-06 指标层替换实现（接口不变）。
2. **判定**（`budget.Evaluate`）：两信号任一命中即 `Exceeded`，`Reasons` 逐条给出「有效工具数 X 超过预算 Y」/「工具面估算 N tokens 占上下文 M 的 P%，超过上限 Q%」。
3. **统计口径**：`manager.EffectiveTools()`（healthy ∧ 工具治理位 enabled ∧ ¬quarantined）→ 按租户过滤 → 名称/描述/schema 三项求和。**默认拒绝（D7）下新工具不计数**，避免把未治理工具算进预算（有专门用例）。
4. **触发时机**：一次工具发现刷新后（`discoverWithSession` 末尾）自动判定；并暴露 `Manager.EvaluateToolBudget(tenantID)` 供管理面/指标层（M2-06）显式调用。
5. **边沿触发**（`budgetLast` 状态）：首次超限、超限期间工具数变化、超限期间估算 token 变化时各发一条；未超限不发「恢复」事件（恢复可由事件时间序推断）。
6. **阈值来源**：`config.yaml` 的 `mcp.tools_budget` / `mcp.tools_context_tokens` / `mcp.tools_token_share`（`config.yaml.example` 同值，两侧测试钉住）→ `manager.Options` → 亦通过 `Manager.Policy` 与 `mcp/admin` 的 `ServerPolicyView` 回读展示（管理页可见有效阈值）。

## 3. 验证记录（本机实跑）

命令与结果（Windows 本机，`E:\projects\itsm\itsm-backend`）：

| 命令 | 结果 |
| --- | --- |
| `go build ./internal/bootstrap/ ./mcp/... ./config/` | exit 0 |
| `go test ./mcp/budget/ -count=1 -v` | **ok**（估算 6 例 + 判定 4 例 + 评估 1 例） |
| `go test ./mcp/manager/ -count=3` | **ok**（3 连跑；含预算 5 例） |
| `go test ./mcp/... ./config/ ./tests/mcpintegration/ ./handlers/mcp/ -count=1` | **11 包全 ok**（admin 18.9s / budget 0.4s / client 6.0s / manager 2.3s / provider 13.0s / registry 0.6s / mockserver 4.5s / transport 1.2s / config 1.8s / mcpintegration 19.9s / handlers/mcp 9.0s） |
| `gofumpt -l ./mcp ./config`（CI 定版 v0.7.0） | 无输出 |
| `staticcheck ./mcp/... ./config/`（CI 同款 v0.6.1） | exit 0（无输出） |
| `go vet`（随建随验） | exit 0 |

### 3.1 用例清单

`mcp/budget/estimate_test.go`（11 例）：
`TestEstimateTokens_口径`（6 子例：空串/短 ASCII/4 与 8 字符/CJK/中英混合）、`TestMeasure_汇总`、`TestEvaluate_工具数信号`（含「= 预算不告警」边界）、`TestEvaluate_占比信号`（含 30% 边界与双信号两条理由）、`TestEvaluate_默认值`（40 / 128000 / 30%）、`TestEvaluate_负值钳制`、`TestAdvise_评估产物`（含返回切片为拷贝）。

`mcp/manager/budget_test.go`（5 例）：
`TestToolBudget_未超限不发事件`（40 = 预算）、`TestToolBudget_跨阈值发一条且不重复`（41 → 1 条；重复判定不重复发）、`TestToolBudget_超限期间计数变化再发一条`（41 → 42 重载后第 2 条）、`TestToolBudget_占比信号`（工具数未超限、描述 4000 汉字 → 占比 40% 触发，理由仅占比）、`TestToolBudget_默认阈值`、`TestToolBudget_未启用工具不计数`（默认拒绝语义）、`TestToolBudget_忽略其它租户与关停`。

`config/config_mcp_test.go`：新增 6 处断言（解析值 + 默认值），与 `config.yaml.example` 同值钉住。

### 3.2 顺带修复

`TestManager_ConnectFailureBackoffThenRecover` 的「退避未到不得重试」断言原用 **20ms** 退避窗口，在满载与 `-race` 插桩下会被调度抖动打穿（本轮曾复现 1 次）。已为该用例改用 `newTestManagerWithBackoff`（Base=300ms，≪ 恢复阶段 2s 等待预算），并保留其余用例的默认 20ms；`-count=3` 稳定通过。

## 4. 元工具方案评估（结论：暂不实施，先满足灰度前置条件）

- **评估产物**：`budget.Advise(report)` 已把评估机器化——预算内返回 `none`；超限返回 `meta_tools_candidate`，给出建议形态 `mcp__meta__list` / `mcp__meta__search` / `mcp__meta__describe` / `mcp__meta__call` 与理由（含「须以灰度开关默认关闭上线」）。
- **不实施的理由**（与任务卡「先评估后灰度，避免再次引入选择困难」一致）：
  1. 元工具会**新增一层选择**（模型需先选元工具再选目标工具），在工具数未超限时净负收益；A2-03 的触发信号正是「是否超限」，故必须先有真实超限数据；
  2. `mcp__meta__call` 的执行面必须与普通外部工具同源（同一 Gate3 审批参数冻结语义），实施前需一次安全评审（写路径尤其）；
  3. 一期默认预算 40 / 30% 尚未在任何真实租户上触发，缺少灰度依据。
- **实施前置条件**（进入 M2 灰度前必须满足）：① 生产/预发出现 `mcp.tools.budget_exceeded` 事件（含租户与规模）；② 安全评审通过 `meta__call` 的审批与审计同源设计；③ 灰度开关（平台级，默认关闭）与回滚路径就位。
- **未闭环登记**：元工具实施本身不在 M2-03（DoD `unit_verified`）范围内；若后续拍板实施，按 A2-03 括注「（若实施）按灰度开关可用」追加集成测试与安全评审。

## 5. 边界与已知限制

1. **估算是近似**：CJK/ASCII 两档系数不区分中英混排细节与 JSON 结构开销，可能高估或低估 ±20%；仅用于阈值告警（已在包注释与 DoD 中写明）。
2. **事件不含结构化字段**：`mcp.tools.budget_exceeded` 的规模数据在 `Detail` 文本中（如 `tools=41 tokens=12345`），未新增 Event 字段。M2-06 指标层如需结构化指标，应改用 `Manager.ToolBudgetReport` 直读（已导出）。
3. **触发点为发现路径**：治理位翻转（`SetToolEnabled`）与隔离解除不改 manager 的 `target.discovered`，因此**不会立即**重判预算（下一次发现刷新或显式调用 `EvaluateToolBudget` 时生效）。管理面如需实时提示，可在 M2-06 的指标采集里按健康循环周期调用（本任务不引入定时扫描，避免额外后台行为）。
4. **未做**：元工具实现、Prometheus 指标暴露（M2-06）、管理页展示（前端展示预算阈值已随 `ServerPolicyView` 返回，UI 呈现归 M2-06/前端排期）。

## 6. 证据锚点（文件:行号）

| 内容 | 位置 |
| --- | --- |
| 估算与判定 | `itsm-backend/mcp/budget/estimate.go`（`EstimateTokens` / `Measure` / `Limits` / `Evaluate` / `Advise`） |
| 用例 | `itsm-backend/mcp/budget/estimate_test.go` |
| 租户统计与边沿触发 | `itsm-backend/mcp/manager/budget.go`（`ToolBudgetReport` / `EvaluateToolBudget` / `tenantBudgetEntries`） |
| 发现后挂钩 | `itsm-backend/mcp/manager/manager.go`（`discoverWithSession` 末尾调用） |
| 事件类型 | `itsm-backend/mcp/manager/events.go`（`EventToolsBudgetExceeded`） |
| 选项与默认值 | `itsm-backend/mcp/manager/manager.go`（`Options.ToolBudget/ContextTokens/ToolTokenShare`、`defaultToolBudget` 等、`Policy`） |
| 配置项 | `itsm-backend/config/config.go`（`MCPConfig.Tools*` + `applyMCPDefaults`）、`itsm-backend/config.yaml.example`（`mcp.tools_*` 三行） |
| 装配 | `itsm-backend/internal/bootstrap/app.go`（`manager.Options` 传三项） |
| 管理面回读 | `itsm-backend/mcp/admin/service_types.go`（`ServerPolicyView` 三字段 + `policyView`） |
| 用例（manager 侧） | `itsm-backend/mcp/manager/budget_test.go` |

## 变更记录

| 日期 | 变更 |
| --- | --- |
| 2026-09-27 | 首次登记：M2-03 实施与单元验证（预算告警 5 例 + 估算/判定/评估 11 例 + 配置 6 断言 + 静态检查双清），A2-03 判定 `unit_verified` |
