# B0-06 实施证据（统一脱敏入口：default / strict 两档）

> 文档类型：实施证据（任务 B0-06）
> Status: draft
> 编制日期：2026-09-27
> 任务：B0-06（基础脱敏引擎）
> 关联文档：`docs/plan/ai-bot-capability-landing-implementation-plan-2026-09-27.md`（§4.1 B0-06、§5.2 AB0-06）
> 核查方式：本机编译 + 定向/全量测试（含真实落库字段断言与既有 MCP 审计用例回归）

## 1. 结论

| 任务 | 目标级别 | 本轮判定 | 依据 |
| --- | --- | --- | --- |
| B0-06 | `unit_verified` | **`unit_verified`** | 统一入口 `service/bot/redactor.go`（default/strict 两档 + 可配敏感键） + 审计写入四类字段接线（入参快照/结果摘要/审批拒绝原因/strict 结果） + 表驱动 UT 5 例 + 落库断言 2 例。**未达** `integration_verified`：跨服务（MCP provider / 工具队列）真实结果摘要在 B0-07 端到端验收中一并核验 |

## 2. 交付物

| 变更 | 位置 | 说明 |
| --- | --- | --- |
| 统一入口 | `itsm-backend/service/bot/redactor.go`（新增） | `Redactor{patterns}` + `Profile(default\|strict)` + `NormalizeProfile`（未知/空 → **strict**）+ `RedactArgs` / `RedactResult` / `RedactText`；`default` 档与 `pkg/redact` 同语义（UT 逐字节互查），`strict` 档输出 `{profile,masked,keys,reason}` 信封（只留顶层键名） |
| 可配键名单 | 同上（`NewRedactor(extraKeyPatterns...)`） | 在 `pkg/redact.SensitiveKeyPatterns` 基础上**追加**，不改全局名单（避免跨模块副作用） |
| 审计接线 | `handlers/ai/service.go` | ①`redactionProfileFor(ctx, tenantID, toolName)`：经 `GetToolForTenant` 解析（内置优先 → provider 兜底），使 **MCP 治理标注同样生效**；未知工具 → strict；②`redactArgs(...)` 统一生成 `args_redacted`：写工具 pending、dry-run 预览、只读审计三处一致；③`recordToolAudit`：strict 档结果摘要改为全掩码信封（default 档沿用既有 `execution.OutputSummary`）；④审批拒绝回填消息的 `reason` 走 `RedactText`（按被拒工具的档位） |
| 原语复用 | `pkg/redact`（未改） | 保留为叶子原语；本包负责「档位 × 工具元数据」的策略层 |

## 3. 测试记录（本机）

| 用例 | 断言要点 | 结果 |
| --- | --- | --- |
| `TestRedactor_DefaultProfile` | 明文口令/密钥（含嵌套、数组元素）不出现；常规字段保留；**与 `redact.ArgsJSON` 输出逐字节一致**（口径互查）；输出合法 JSON | PASS |
| `TestRedactor_ConfigurableSensitiveKeys` | 追加键名（大小写不敏感）生效；**登记口径边界**：default 档按**键名**判定，不做自由文本值级扫描 | PASS |
| `TestRedactor_StrictProfileMasksAllValues` | 只留顶层键名、任何值不输出；结果摘要与文本摘要同样全掩码 | PASS |
| `TestRedactor_Truncation` | 单值截断标记；超限入参 → 键名信封；极小上限 → 最小信封；结果摘要受上限约束 | PASS |
| `TestNormalizeProfile` | 未知/空档位收敛 strict | PASS |
| `TestB0_06_PendingAuditArgsAreRedacted` | 落库 `args_redacted` 无明文口令/密钥、含掩码、常规字段可读、合法 JSON；执行真源 `Arguments` 仍在（审批重放依赖，不参与展示） | PASS |
| `TestB0_06_StrictProfileMasksAllValues` | 取真实 strict 档内置写工具（不硬编码工具名）→ 落库无任何值、`masked:true`、键名保留 | PASS |

编译/格式：`gofumpt -l ./handlers/ai ./service/bot` 无输出；`go build ./...` exit 0；`go test ./handlers/ai/ ./service/bot/ -count=1` 全绿（14.3s / 0.27s）。

**过程记录（真实失败与修复，保留以便复现）**
1. 首版 `redactionProfileFor` 使用 `GetTool`（**仅内置**），MCP 工具因此解析为 nil → 兜底 strict → `TestExecuteTool_MCPReadOnlyAuditTriple` 回归失败（入参全掩码、输出摘要被替换）。
   - 修复：改用 `GetToolForTenant(ctx, tenantID, name)`（内置优先 → provider 兜底），MCP 治理标注的 `redactionProfile` 生效；同时把该测试的 stub 定义补全为「带完整元数据标注」（B0-01 之后治理面下发的工具即应如此），并加注说明缺标注会被保守归一化为 strict。
2. 测试首版有两处过度断言（对自由文本做值级扫描、极小上限仍期望保留键名），按实际契约修正并**在测试中显式登记口径边界**，而非放宽实现。

## 4. 设计取舍（登记）

1. **strict = 只留键名**：高敏工具连字段值类型都不落库（避免"值很短/是布尔"这类侧信道）；键名保留以支撑审计定位。
2. **档位来自工具元数据**：调用点不再自行决定档位，`redactionProfileFor` 是唯一决策点；未知工具 fail-closed 到 strict。
3. **default 档不扫描自由文本**：与 `pkg/redact` 既有口径一致（键名判定）；需要值级保护的场景必须显式选 strict。已在测试与本文档登记，避免误读为"内容级 DLP"。
4. **原语与策略分层**：`pkg/redact` 保持叶子包零依赖；`service/bot` 承担策略（档位、可配名单、信封格式），供后续消息与工件链路复用。

## 5. 未闭环

- `mcp/provider` 与 `service/tool_queue` 的结果摘要仍走 `pkg/redact.ValueSummary`（default 口径）；**strict 档工具的最终结果**已在审计写入层全掩码，但若要连摘要生成也按档位收敛，需在 B1 把 `Redactor` 下推到执行侧（登记为后续项，不阻塞本任务）；
- 消息与工件（`bot_artifacts`）链路的统一入口接入归 B1-*；
- 敏感键默认名单的运营可配（配置文件/数据库）未做（当前为构造参数）。

## 6. 变更记录

| 日期 | 变更 | 说明 |
| --- | --- | --- |
| 2026-09-27 | 初稿 | B0-06 交付证据：两档统一入口、审计四类字段接线、MCP 档位解析缺陷修复记录、口径边界登记 |
