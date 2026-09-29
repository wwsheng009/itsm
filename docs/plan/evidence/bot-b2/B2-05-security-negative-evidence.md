# B2-05 实施证据（授权负向安全测试集）

> 文档类型：实施证据（任务 B2-05）
> Status: draft
> 编制日期：2026-09-27
> 任务：B2-05（授权负向安全测试集；依赖 B2-02）
> 关联文档：`docs/plan/ai-bot-capability-landing-implementation-plan-2026-09-27.md`（§4.4 B2-05、§5.2 AB2-05、§2.2 BP6）
> 核查方式：Go 单元/集成测试（表驱动 + ent/sqlite 真库）+ 回归（handlers/ai、service/bot、tests/botintegration）

## 1. 结论

| 项 | 判定 | 说明 |
| --- | --- | --- |
| B2-05 | **`integration_verified`** | 四类负向全部落地为**可执行断言**（不只是文档口径）：①未授权不可见 + 不可调用（双端）；②跨租户 fail-closed；③黑名单永不下发/不可执行；④身份/租户/权限参数注入被剥离并审计 |
| AB2-05 | ✅ | 负向集全过（见 §3 运行记录） |
| 兼容默认回归 | ✅ | 关闭态（`bot.enabled=false`）逐字节不变：黑名单对现行 14 个内置工具零误伤（已用注册表快照锁定）；Bot 归属校验在未注入 Policy 时不做（既有行为） |
| 既有测试影响 | ✅ | 2 处测试夹具改名（见 §5-2），语义不变；无生产行为回归 |

## 2. 本轮新增的生产代码（最小充分改动）

| # | 文件 | 改动 | 动机 |
| --- | --- | --- | --- |
| 1 | `itsm-backend/service/bot/blacklist.go`（新建） | `BlacklistRule(name, provider, resource) string` + `IsBlacklisted`：三面规则（`admin_surface` / `destructive` / `arbitrary_io`），`delete_ci_relationship` 显式例外，`mcp__`/provider=mcp 豁免 | 黑名单此前只是文档要求，**无代码**：管理员若为 `admin_*`/`http_*` 类工具建授权即可下发并执行 |
| 2 | `itsm-backend/service/bot/policy.go` | `Decide` 新增 **⓪ 黑名单步**（先于兼容默认与严格交集）→ `tool_blacklisted:<rule>` | 执行面：显式授权也不能放行；兼容默认（只读）也不能放行 |
| 3 | `itsm-backend/handlers/ai/service.go` | ①新增 `chatToolDecision`（自 ChatStream 内联逻辑抽出，**黑名单优先**，两分支统一）；②新增 `ValidateBotSelection`（显式 `botId` 必须命中本租户模板，未注入 Policy 时跳过以保持关闭态行为）；③新增 `ErrBotSelectionNotFound`/`ErrBotSelectionUnavailable`；④`ExecuteToolWithOptions` 入口剥离身份/租户/权限键并把 `args_stripped:<keys>` 写入所有路径的 `permission_reason` | 下发面：黑名单工具模型看不见；跨租户 `botId` 此前会**静默退化到兼容默认**（放大权限面）；模型可经 args 伪造 `tenant_id`/`user_id`/`role` |
| 4 | `itsm-backend/handlers/ai/args_guard.go`（新建） | `sanitizeReservedArgs`（17 个保留键的归一化匹配 + 排序留痕）、`argsStrippedMarker`、`respondBotSelectionError`（404 `BOT_NOT_FOUND` / 503 `BOT_SELECTION_UNAVAILABLE`） | 剥离 + 留痕 + HTTP 语义单一来源 |
| 5 | `itsm-backend/handlers/ai/handler.go` | `Chat` / `ChatStream` 在 `WithBotID` 后调用 `ValidateBotSelection`：同步路径 404、SSE 路径发统一 error 事件后返回 | 入口层 fail-closed，不再静默降级 |

> 语义边界：黑名单**不适用于 MCP 外部工具**（`mcp__` 前缀/provider=mcp）——它们由 M1 治理面负责（服务器默认禁用、工具默认不启用、quarantine、风险标注、管理员显式启用），此边界写入 `blacklist.go` 文件头。

## 3. 负向用例矩阵（B2-05 四类）

| # | 负向面 | 用例（测试名） | 断言要点 | 判定 |
| --- | --- | --- | --- | --- |
| ①a | 未授权不可见 | `TestChatToolDecisionBlacklistNeverDispatched` | 关闭态与开启态下：黑名单工具均 `allowed=false` 且原因含 `tool_blacklisted`；未授权业务工具 → `tool_not_granted`；已授权业务工具 → 放行；快照不可用 → `snapshot_error`（fail-closed） | ✅ |
| ①b | 未授权不可调用 | `TestExecuteTool_BotPolicyDeniesUngrantedTool`（B2-02 既有）+ `TestDecide_StrictIntersection`| 执行面 Gate2.5 独立复判：未授权工具 `denied` + 审计 `permission_reason` 含 `tool_not_granted` | ✅ |
| ② | 跨租户 fail-closed | `TestValidateBotSelectionCrossTenantFailClosed` | 同租户命中 → 通过；跨租户引用 → `ErrBotSelectionNotFound`（404 语义）；不存在 ID → 同上（不降级）；未注入 Policy → 跳过（关闭态零变化） | ✅ |
| ③ | 黑名单永不下发 | `TestBlacklistRule`（30 子例：admin/permission/rbac/role/tenant/system、delete/drop/purge/truncate、http/fetch/shell/exec/sql/db、resource 通道、例外、MCP 豁免、大小写空白、空名） | 三类规则 id 稳定；`delete_ci_relationship` 放行（管理员场景）；`mcp__` 豁免 | ✅ |
| ③b | 黑名单不可执行 | `TestDecide_BlacklistPrecedence` | 显式授权 `admin_*` 仍被拒（`tool_blacklisted:admin_surface`）；兼容默认下只读 `http_request` 被拒；例外工具在授权内正常放行 | ✅ |
| ③c | 零误伤 | `TestBlacklistRule_RegistryToolsAreNotBlocked` | B0-01 注册的 14 个内置工具（名称 + resource 快照）全部放行——黑名单不改变既有下发面 | ✅ |
| ④ | 注入覆写 | `TestSanitizeReservedArgs` + `TestExecuteToolStripsIdentityArgsAndAudits` | 17 个保留键（大小写/驼峰变体）被剥离且**排序留痕**；业务键（`title`/`assignee_id`/`target_id`/`priority`）保留；执行器收到的 args 不含身份键；审计 `permission_reason` 含 `args_stripped:...`、`args_redacted` 不含伪造值 | ✅ |

## 4. 运行记录（本机，pwsh，Go 1.25.13）

| # | 命令 | 结果 |
| --- | --- | --- |
| 1 | `go test ./service/bot/ -count=1 -run 'TestBlacklist\|TestDecide_Blacklist' -v` | **全绿**（30 子例 + 注册表零误伤 + 判定优先级） |
| 2 | `go test ./handlers/ai/ -count=1 -run 'TestChatToolDecision\|TestValidateBotSelection\|TestSanitizeReservedArgs\|TestExecuteToolStrips' -v` | **4 用例全绿** |
| 3 | `go test ./service/bot/ -count=1` | `ok 4.759s` |
| 4 | `go test ./handlers/ai/ ./tests/botintegration/ ./service/ -count=1 -timeout 25m` | 回归（handlers/ai `ok`、botintegration `ok`、service 见提交记录） |

## 5. 既有测试的两处夹具调整（语义不变，登记备查）

| # | 文件 | 调整 | 理由 |
| --- | --- | --- | --- |
| 1 | `service/bot/policy_test.go` | 用「非白名单写工具」的代表名 `delete_ticket` → `bulk_archive_tickets` | `delete_ticket` 现已命中 `destructive` 黑名单，原因码从 `tool_not_granted` 变为 `tool_blacklisted:destructive`；该用例的原意是验证**未授权**分支，故换用中性名（黑名单优先级由新增用例专门覆盖） |
| 2 | 同上（`TestDecide_StrictIntersection/未授权工具拒绝`） | 同上改名 | 同上 |

## 6. 遗留与登记

| # | 项 | 归属/处置 |
| --- | --- | --- |
| 1 | 「目标参数」覆写（`target_type`/`target_id` 等入口上下文）由 `ScopeResolver` 负责，本轮只覆盖身份/租户/权限三类键 | **B3-01**（依赖入口协议定义）；已在 `args_guard.go` 注释显式登记边界 |
| 2 | 黑名单规则为「名称前缀 + resource」双通道，非穷举语义；新增工具若命名规避（如 `purge` 拼写变体）仍可能漏网 | 规则 id 与命中点在审计/日志可见；后续可评测引入 schema 语义标签（列为 P2 观察项） |
| 3 | 跨租户用例为 Service 级（`ValidateBotSelection`），HTTP 层 404 契约未单独断言 | B2-06（集成验收）补 handler 级 404 断言 |
| 4 | O-5（既有不稳定用例）：全 `./service/` 包运行时 `TestBiz_ListProcessInstancesByTenant` 失败 1 次；**单独运行通过**（`ok 0.654s`）。本次 diff 未触及 `service` 根包（改动仅在 `service/bot` 与 `handlers/ai`） | 归入既有 flake 治理；B2-06 复跑时继续观察 |

## 7. 变更记录

| 日期 | 变更 | 说明 |
| --- | --- | --- |
| 2026-09-27 | 初稿 | B2-05 交付：黑名单代码化（`service/bot/blacklist.go` + `Decide` ⓪ 步 + 下发面过滤）、跨租户 `botId` fail-closed（404/503 语义）、身份/租户/权限参数剥离与审计留痕；四类负向用例全绿，回归通过；判定 `integration_verified`（目标参数覆写归 B3-01） |
