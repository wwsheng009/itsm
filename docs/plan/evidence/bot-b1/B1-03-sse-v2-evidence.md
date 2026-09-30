# B1-03 实施证据（SSE 契约 v2、单一事件注册表与预算中止）

> 文档类型：实施证据（任务 B1-03）
> Status: draft
> 编制日期：2026-09-27
> 任务：B1-03（SSE 契约 v2 服务端与兼容层；依赖 B1-02、BP3）
> 关联文档：`docs/plan/ai-bot-capability-landing-implementation-plan-2026-09-27.md`（§4.2 B1-03、§5.2 AB1-02/AB1-03）、`docs/plan/itsm-mcp-external-tool-integration-implementation-plan-2026-09-27.md`（M1-03 同表）
> 核查方式：真实 `/api/v1/ai/chat/stream`（HTTP + SSE 帧解析，mock provider 确定性驱动）+ 注册表契约 UT

## 1. 结论

| 项 | 判定 | 说明 |
| --- | --- | --- |
| B1-03 | **`integration_verified`** | 单一事件注册表落地；v1 事件名与载荷**字节级不变**；v2 事件（`run_started`/`step`/`confirmation_required`）带 `v:2`；`approval_pending` 与 `confirmation_required` 双发；**预算超限在执行点拒绝并中止主链路**（流以 `error{errorCode=budget_exceeded}` 结束、不再发 `done`、运行收口 failed） |
| 与 MCP M1-03 同表 | ✅ | `tool_call_started/finished/failed`、`approval_pending` 与 MCP 线共用同一注册表（`sse_events.go`），MCP 不另立事件名 |
| BP3（事件名冻结） | ✅ 本任务范围内完成 | 注册表即冻结清单；新增事件走「登记 + 测试」流程（`TestSSERegistry_AllEmittedEventsRegistered` 失败即阻断） |
| 遗留 | 2 项 | ①前端尚未消费 v2 事件（B1-04）；②`artifact` 已登记未发送（B3-06） |

## 2. 交付物

| # | 文件 | 说明 |
| --- | --- | --- |
| 1 | `itsm-backend/handlers/ai/sse_events.go`（新建） | **单一事件注册表**：12 个事件（v1×8 + v2×4）含版本/家族/别名/载荷字段；`IsKnownSSEEvent`；v2 信封 `sseEnvelopeV2`（对象载荷追加 `v:2`）；运行事件映射 `writeSSERunEvent`（run_started/step 外发；tool_call/run_finished 不外发避免双份）；`sseErrorPayload`（预算超限与外部错误码）；未知事件忽略策略写入文件头契约 |
| 2 | `itsm-backend/handlers/ai/tool_events_sse.go`（修改） | `writeToolEvent` 引用注册表常量；`pending` 双发：先 `approval_pending`（v1 原样）再 `confirmation_required`（v2 + `v:2`） |
| 3 | `itsm-backend/handlers/ai/service.go`（修改） | `ChatStream`/`ChatStreamWithProviderInfo`/`chatStream` 新增 `onRun` 回调；`run_started`/`step` 在**落库成功之后**广播；新增 `broadcastRunEvent`（统一补 `runId`）；**执行点预算闸门 `admitTool`**（超限拒绝执行、首次拒绝才落库/外发、收口 failed、取消上下文）；`botToolObserver` 去掉事后记账式预算判定，只负责步骤与 `tool_call` 事件 |
| 4 | `itsm-backend/handlers/ai/handler.go`（修改） | 事件清单注释改以注册表为准；发送点全部引用注册表常量；`onRun` 接线；`error` 帧改走 `sseErrorPayload`（预算超限带 `errorCode=budget_exceeded`） |
| 5 | `itsm-backend/handlers/ai/tool_events.go`（修改） | `toolEventErrorCode` 识别 `bot.ErrBudgetExceeded` → `budget_exceeded` |
| 6 | `itsm-backend/handlers/ai/sse_events_test.go`（新建） | 注册表完整性/唯一性/家族自洽；v2 信封；运行事件映射；error 载荷；**旧客户端模拟**（只认 v1 名字的解析器在混入 v2/未来事件后行为不变） |
| 7 | `itsm-backend/handlers/ai/bot_observer_test.go`（重写） | 观察者职责（透传 + 步骤 + `tool_call` 事件 + v2 `step` 广播）；收口后拒绝步骤写入；闸门错误码映射 |
| 8 | `itsm-backend/tests/botintegration/b1_chat_run_test.go`（修改） | 帧契约断言（首帧 `run_started` v2 + `v/runId`；末帧 `done`；v1 帧无 `v`；事件名全部已登记）；**预算中止端到端**：mock provider 连续工具调用 → 第 2 次在执行点被拒 → 流以 `error{errorCode=budget_exceeded}` 结束且无 `done`；审计：超限仅 1 条、工具 done/failed 各 1 条 |
| 9 | `itsm-backend/tests/botintegration/b0_flow_test.go`（修改） | 桩 provider 增加只读工具 `stub__list_notes`（供预算中止用例驱动读路径） |
| 10 | `itsm-backend/mcp/provider/provider.go`（修改） | **回归修复**：MCP 工具定义显式声明 `RedactionProfile=default`（见 §6） |

## 3. 运行记录（本机，pwsh）

```text
go test ./handlers/ai/ -run 'TestSSE|TestWriteToolEvent|TestBotToolObserver|TestToolEventErrorCode' -count=1
→ ok  itsm-backend/handlers/ai  1.081s
go test ./tests/botintegration/ -run 'TestB1ChatStreamBudgetAbortEndToEnd|TestB1ChatStreamRunManagerSequenceMatchesDB' -count=1 -v
→ PASS TestB1ChatStreamRunManagerSequenceMatchesDB 0.33s
→ PASS TestB1ChatStreamBudgetAbortEndToEnd 0.34s
跨线修复验证（§6.1）
go test ./tests/mcpintegration/ -run 'TestM1WriteApproval_EndToEnd|TestM1Flow_FailureInjection_CallTimeout' -count=1
→ ok  itsm-backend/tests/mcpintegration  8.515s（修复前失败）
全量回归（11 包，-count=1 -timeout 25m）
→ ok handlers/ai 35.8s | ok tests/botintegration 21.3s | ok tests/mcpintegration 33.4s | ok service/bot 13.1s
→ ok config 1.5s | ok mcp/provider 15.8s | ok mcp/registry 0.6s | ok mcp/transport 0.9s
→ ok mcp/client 6.3s | ok mcp/admin 24.0s | ok mcp/manager 3.2s    （总 exit 0）
go build ./... → exit 0；go vet ./handlers/ai/ ./tests/botintegration/ → exit 0；gofumpt -l → 无输出
```

## 4. 事件注册表（冻结清单）

| 事件名 | 版本 | 家族 | 载荷 | 发送点 |
| --- | --- | --- | --- | --- |
| `sources` | v1 | — | `[{objectType,id,title,snippet,score}]`（数组） | RAG 检索 |
| `delta` | v1 | — | `{content}` | 正文增量 |
| `done` | v1 | — | `{conversationId, provider?, providerSource?}` | 流正常结束 |
| `error` | v1 | — | `{message, errorCode?}` | 流错误结束（预算/Provider 解析/主链路） |
| `tool_call_started` | v1 | tool_call | `{id?,tool,provider,server?,phase,status}` | 工具开始 |
| `tool_call_finished` | v1 | tool_call | `{...,status:"done",summary,durationMs}` | 工具成功 |
| `tool_call_failed` | v1 | tool_call | `{...,status:"failed",errorCode}` | 工具失败/被预算拒绝 |
| `approval_pending` | v1（别名） | confirmation | 同下一行（无 `v`） | 写工具待审批 |
| `confirmation_required` | **v2** | confirmation | `{v:2,id,tool,...}` | 写工具待审批（与上行双发） |
| `run_started` | **v2** | — | `{v:2,runId,entrypoint,conversationId?}` | 运行开始（有运行档案时） |
| `step` | **v2** | — | `{v:2,runId,stepIndex,type,payloadRef?,durationMs}` | llm/tool 步骤落库后 |
| `artifact` | **v2** | — | `{v:2,artifactId,kind,title,...}` | **本期不发送**（B3-06） |

**兼容承诺**：v1 事件名与载荷字节级不变；v2 事件对旧客户端是未知事件，按契约静默忽略（`TestSSERegistry_LegacyClientIgnoresUnknownEvents` 固化）。客户端不得因未知事件中断流。

## 5. 预算中止链路（B1-03 的核心修复）

```text
execTool(第 N 次调用)
  → admitTool()  ── ReserveToolCall() 失败
      ├─ 首次：run.BudgetExceeded()（写 error{budget_exceeded} + 收口 failed）+ 取消上下文
      └─ 返回 (错误, 是否首次)
  → 未通过：不执行工具（无副作用）、只发一条 tool_call_failed{errorCode=budget_exceeded}
  → 错误抛回模型循环 → 主链路中止
handler → error 帧 {message, errorCode:"budget_exceeded"}（不再发 done）
```

与 B1-02 的差异（**修正了此前的设计缺陷**）：原实现在 `botToolObserver` 中对 `started` 事件做预算判定——而观察者是在执行决定**之后**才被调用的记录者，判定结果只能"记账"，无法阻止工具真正执行（实测：5 轮工具全部执行完才收口）。B1-03 把判定上移到执行点闸门，并补上"首次拒绝才落库/外发"的去重，端到端用例已证明：仅 1 次真实执行、第 2 次被拒绝、流以 error 结束。

## 6. 遗留与归属

### 6.1 回归中发现并修复的跨线缺陷（2026-09-27）

全量回归发现 MCP 侧两处集成用例失败（`TestM1WriteApproval_EndToEnd`、`TestM1Flow_FailureInjection_CallTimeout`），断言 `args_redacted` 应含 `"token":"****"` 而实际得到 strict 档的 keys-only 摘要。**归因验证**：`git stash` 去掉本轮改动后基线同样失败 → **非 B1-03 引入**；根因是 Bot 线 B0-06 的归一化口径（`normalizeDefinition`：脱敏档留空视为未标注 → 取最保守 strict）叠加 MCP provider 从未标注该字段，使所有 MCP 工具的审计入参快照静默退化为「只留键名」，与 MCP 线 A0-11/A1-09 的验收证据（敏感键掩码 + 其余值保留）矛盾。

**修复**：在 MCP 工具投影处显式声明 `RedactionProfile: service.ToolRedactionDefault`（`mcp/provider/provider.go`）。理由：MCP 协议不提供脱敏档字段、ITSM 侧亦无 MCP 工具标注面，留空属「遗漏」而非「决策」；default 仍对口令/token 类键强制掩码并截断长值，既满足 MCP 验收口径，也不放宽敏感键保护。修复后 MCP 两用例恢复通过，`handlers/ai` 全包保持绿。

**登记为已知缺口**：MCP 工具暂无更严档位（strict）的可配置入口；若后续需要，应在 MCP 工具标注面补字段（不建议再用「留空即 strict」隐式生效）。

| # | 项 | 归属 |
| --- | --- | --- |
| 1 | 前端消费 v2 事件（`run_started`/`step`/`confirmation_required`）+ 未知事件忽略策略落地 | **B1-04** |
| 2 | `artifact` 事件发送（计划类工具产物） | B3-06 |
| 3 | token 预算仍未接 provider 用量（`AddTokens` 无调用方） | B1-04 之后 / B2（与用量口径合并） |
| 4 | `MaxOutputBytes` 未接工具结果 → 模型消息路径 | B1 后续小步 |

## 7. 变更记录

| 日期 | 变更 | 说明 |
| --- | --- | --- |
| 2026-09-27 | 初稿 | B1-03 交付：单一事件注册表 + v2 信封 + 双发兼容 + 运行事件广播 + 执行点预算闸门（中止主链路）；UT 6 例 + 端到端 2 例全绿；判定 `integration_verified` |
