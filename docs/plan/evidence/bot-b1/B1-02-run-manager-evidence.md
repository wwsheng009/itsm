# B1-02 实施证据（RunManager：生命周期、预算护栏、先落库后广播）

> 文档类型：实施证据（任务 B1-02）
> Status: draft
> 编制日期：2026-09-27
> 任务：B1-02（RunManager 与运行态广播）
> 关联文档：`docs/plan/ai-bot-capability-landing-implementation-plan-2026-09-27.md`（§4.2 B1-02、§5.2 AB1-02）、`docs/plan/evidence/bot-b1/BP8-budget-config-evidence.md`
> 核查方式：真实 ent + SQLite（预算/状态/落库顺序）+ 真实 `/api/v1/ai/chat/stream` HTTP 链路（广播顺序 == 落库顺序）

## 1. 结论

| 项 | 判定 | 说明 |
| --- | --- | --- |
| B1-02 | **`unit_verified`** | 状态机（running→completed/failed，收口幂等）、三类预算护栏（step/tool_call/token）+ 单工具超时 ctx、`budget_exceeded` 收口与事件落库、**先落库后广播**（Observer 回调时可反查）、工具事件旁路（步骤 + `tool_call` 事件）；集成：真实 chat/stream 的广播顺序与 DB seq 顺序一致 |
| 未达 `integration_verified` 的原因 | — | 主链路**尚未**把预算超限/输出上限变成「中止条件」（见 §5 遗留①③）：当前只做到「记事件 + 不写库 + 不重复计数」，中止策略需与 B1-03（SSE 契约）/B1-05（确认态机）一并定型，避免现在就把错误语义钉死 |
| 遗留（4 项） | — | ①预算超限不中断主链路；②`AddTokens` 未接 provider 用量回报；③`MaxOutputBytes`/`TruncateOutput` 未接入工具结果路径（MCP 侧已有自身 `MaxResultBytes`）；④`cancelled` 状态预留未启用 |

## 2. 交付物

| # | 文件 | 说明 |
| --- | --- | --- |
| 1 | `itsm-backend/service/bot/run_manager.go`（新建） | `Budget`（五参数 + `normalize` fail-safe）、`Manager`（`Start`/`WithObserver`/`WithClock`/`Budget`）、`Run` 句柄（`Emit` 先落库后广播、`RecordStep` 计数先行+失败回滚、`ReserveToolCall`/`BeginToolCall`（单工具超时 ctx）、`AddTokens`、`Finish`（幂等，首个终态胜出）、`BudgetExceeded`（error 事件 + failed 收口））、`TruncateOutput`、`Observer`/`ObserverFunc`、常量 `RunStatus*`/`ErrorCodeBudgetExceeded`/`EventTypeError` |
| 2 | `itsm-backend/handlers/ai/service.go`（修改） | `botRuns` → `botRunner *bot.Manager`；`SetBotRunner`（BP8 预算注入入口）与 `SetBotRunStore`（默认预算适配层，保持 B1-01 兼容）；`chatStream` 收口改走 `Run` 句柄；新增 `botToolObserver`：工具事件**原样透传**给前端，同时 `started` 计入预算（超限记一次 `error{budget_exceeded,reason=max_tool_calls}` 且不再重复）、`done/failed` 落 `tool` 步骤 + `tool_call` 事件 |
| 3 | `itsm-backend/internal/bootstrap/app.go`（修改） | `bot.enabled=true` 时以 `cfg.Bot.Budget`（BP8）构造 Manager 注入：`MaxSteps/MaxTokens/MaxToolCalls/ToolTimeout=BotToolTimeout()/MaxOutputBytes` |
| 4 | `itsm-backend/service/bot/run_manager_test.go`（新建） | UT 6 例：状态转换与收口幂等、三类预算、超限收口与事件载荷、先落库后广播（回调内反查）、输出截断边界、预算归一化 |
| 5 | `itsm-backend/handlers/ai/bot_observer_test.go`（新建） | UT 2 例：事件透传 + 步骤/事件落库（`payload_ref` 取 `tool_invocation:<id>` 或工具名）、预算超限只记一次且不再计数 |
| 6 | `itsm-backend/tests/botintegration/b1_chat_run_test.go`（修改） | 集成 1 例：真实 chat/stream 下 **广播顺序 == DB seq 顺序**、Observer 回调时事件必已落库、步骤恰为 1 条 `llm`（step_index=0） |

## 3. 运行记录（本机，pwsh）

```text
go test ./service/bot/ -count=1 -run 'TestRunManager' -v -timeout 10m
→ PASS StateTransitions / BudgetGuards / BudgetExceededClosesRun / EmitPersistsBeforeBroadcast / TruncateOutput / BudgetNormalize
  ok itsm-backend/service/bot 2.235s
go test ./handlers/ai/ -count=1 -run 'TestBotToolObserver' -v -timeout 10m
→ PASS ForwardsAndRecords / BudgetExceededStopsCounting   ok itsm-backend/handlers/ai 1.081s
go test ./tests/botintegration/ -count=1 -run 'TestB1ChatStreamRunManagerSequenceMatchesDB' -v
→ PASS ok itsm-backend/tests/botintegration 0.836s
go test ./config/ ./service/bot/ ./handlers/ai/ ./tests/botintegration/ -count=1 -timeout 25m
→ ok config 1.337s | ok service/bot 8.340s | ok handlers/ai 9.622s | ok tests/botintegration 5.107s   （exit 0）
go vet ./internal/bootstrap/ ./config/ → exit 0；gofumpt -l → 无输出
```

## 4. 设计口径（供 B1-03/B1-05 复用）

| 项 | 口径 |
| --- | --- |
| 计数与落库顺序 | **先计数后落库**；落库失败回滚计数（避免预算被吃掉而审计缺失）。超限判定发生在写库之前，绝不先花预算 |
| 收口幂等 | `Finish` 只接受首个终态；后续调用返回 nil 且不改写（防「先 failed 后 completed」的竞态翻转） |
| 广播失败 | Observer 抛错/panic 不影响已落库事实（先落库后广播的必然推论）；调用方负责 SSE 写失败兜底 |
| 工具预算计数点 | 以 `started` 为准（一次真实发起的调用），`done/failed` 只落步骤与事件 |
| 超限事件去重 | `max_tool_calls` 超限只在「首次越界」记一条 error 事件（不随每次 started 重复），避免事件风暴 |
| 单工具超时 | `BeginToolCall` 返回带 deadline 的 ctx（默认 30s，来自 BP8）；调用方必须 `defer cancel()` |

## 5. 遗留与归属

| # | 项 | 归属 |
| --- | --- | --- |
| 1 | 预算超限**中止主链路**（当前仅记事件 + 停止写入，不打断模型/工具循环） | **B1-03**（SSE 错误语义与前端降级一并定型） |
| 2 | `AddTokens` 接 provider 用量回报（需 gateway 暴露 usage） | B1-03/B2（与多 Provider 用量口径合并） |
| 3 | `MaxOutputBytes` 接入工具结果 → 模型消息路径（MCP 侧已有自身上限） | B1-02 后续小步 或 B2 工具面收口 |
| 4 | `cancelled` 状态启用（用户中止） | B1-05（确认态机与中止语义） |
| 5 | 事件名 `run_started`/`run_finished`/`tool_call`/`error` 冻结 | **BP3 评审 → B1-03 单一事件注册表**（与 MCP M1-03 同表） |

## 6. 变更记录

| 日期 | 变更 | 说明 |
| --- | --- | --- |
| 2026-09-27 | 初稿 | B1-02 交付：RunManager（生命周期 + 预算 + 先落库后广播）+ 工具事件旁路 + UT 8 例 + 集成 1 例；判定 `unit_verified` |
