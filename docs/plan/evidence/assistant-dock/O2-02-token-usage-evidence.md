# O2-02 补全：诊断抽屉 token 段（估算口径）证据（§13.4 / §13.2）

日期：2026-10-18 · 状态：`integration_verified`（真实 handler + 真实 service/RAG 端到端帧断言）
范围：把「网关内部限流用的字符/4 估算」接线为**可见的回合用量**：后端 `usage` v2 事件 → 前端解析/埋点 → 诊断抽屉展示（恒标注估算）。

## 1. 目标与成功判据（来自方案）

| 判据 | 出处 | 证据 |
| --- | --- | --- |
| 用户侧诊断抽屉展示当前会话的**运行轨迹/耗时/token** | §13.4 | 抽屉新增 `diagnostics-usage` 段（输入/输出/合计 ≈）+ 口径说明；轨迹/耗时沿用 O2-02 既有 |
| `message_sent / first_token / stream_end` 关键字段含 `token_in/out` | §13.2 | `stream_end` 落 `tokenIn/tokenOut` + `extra.tokenEstimated=true`（前端单测锁定） |
| 诚实降级优先（不冒充真实 usage） | 全局约束 | 全链路恒带 `estimated=true`、界面恒写「估算」、文档显式声明非账单口径 |

## 2. 交付物与关键决策

### 2.1 后端：估算单一来源 + 请求级上报通道

| # | 文件（后端） | 变更 |
| --- | --- | --- |
| 1 | `service/llm_usage.go`（新建） | `LLMUsageEstimate{TokenIn,TokenOut,Estimated}`；`EstimateTextTokens`（字符/4）、`EstimatePromptTokens`（逐条求和）；**请求级 sink 通道**：`WithUsageSink(ctx, sink)` / `reportUsageEstimate(ctx, n)`（未注入 = no-op；sink panic 被 recover 隔离，观测通道绝不冒泡） |
| 2 | `service/llm_gateway.go`（修改） | 三条调用路径 **6 个门禁点**统一改为 `EstimatePromptTokens` + `reportUsageEstimate`（Chat / ChatStream / ChatStreamWithTools 旧分支 + ChatWithRequest / chatStreamWithProvider / ChatStreamWithToolsRequest）；删除私有 `estimateTokens`（口径单源，杜绝漂移）。**上报发生在限流判定之前**：被限流的一次同样计入成本 |
| 3 | `handlers/ai/service.go`（修改） | `chatStreamInner` 注入 sink 累加 `TokenIn`（工具循环多次 LLM 调用累加，口径 = 一次回合总输入）；回合结束时 `TokenOut = EstimateTextTokens(已流式正文)`；`chatStream / ChatStream / ChatStreamWithProviderInfo` 逐层回带 usage（仅 2 个 handler 调用点受影响） |
| 4 | `handlers/ai/sse_events.go`（修改） | 注册 v2 事件 `SSEEventUsage = "usage"`（`{v:2, tokenIn, tokenOut, estimated:true, scope:"turn"}`，注册表即文档）；`writeUsageEvent` **零值也发送**（「无 LLM 调用」必须可与「未接线」区分） |
| 5 | `handlers/ai/handler.go`（修改） | 两条成功分支在 `done` **之前**发送 `usage`（多 Provider 与默认链同口径）；事件清单文档同步 |

**为什么用 context sink 而非改签名**：估算点在网关（RAG 层之下），消费点在聊天层；改 RAG/网关公开签名会波及 A2UI/agent/BPMN 等既有调用方。context 携带 sink 使**未注入者零行为变化**（QA-3 兼容承诺）。

**为什么是 v2 新事件而非扩 `done` 载荷**：`sse_events.go` 的兼容承诺是「v1 载荷字节级不变」；新增 `usage` 事件对旧客户端按注册表契约静默忽略。

### 2.2 前端：解析 → 埋点 → 抽屉

| # | 文件（前端） | 变更 |
| --- | --- | --- |
| 6 | `src/lib/api/ai-api.ts` | `AIUsageEstimate`；SSE 解析新增 `case 'usage'`（非法载荷丢弃、**不虚构数字**）；`AIChatDoneInfo.usage`；`onUsage` 回调。`done` 第二参数仅在 provider/usage 有值时才补（无值时保持单参数旧签名） |
| 7 | `src/lib/ai/chat-telemetry.ts` | `onDone` 支持 `tokenIn/tokenOut` → `stream_end` 顶层字段 + `extra.tokenEstimated=true`；缺席时字段不落（旧后端零变化） |
| 8 | `src/components/ai/chat-core/use-ai-chat-session.ts` | done 时把 usage 写入 `telemetry.onDone` 与助手消息（`ChatMessage.usage`，诊断抽屉数据源） |
| 9 | `src/components/ai/chat-core/types.ts` | `ChatMessage.usage?: AIUsageEstimate`（旧后端缺省 → 抽屉不显示用量段） |
| 10 | `src/components/ai/diagnostics/diagnostics-model.ts` | `latestUsage/usageMessageId`（**独立于 run 扫描**——纯问答也有回合用量）+ `formatTokenUsage`（恒带 `≈`） |
| 11 | `src/components/ai/diagnostics/DiagnosticsDrawer.tsx` | 新增 `diagnostics-usage` 段（Tag「用量（估算）」+ `输入 ≈ n · 输出 ≈ m · 合计 ≈ n+m` + 口径说明）；页脚文案按有无数据分流；文件头诚实边界改写 |

**事件字典同步**：本批**未新增事件名**（复用 `stream_end` + 既有 `tokenIn/tokenOut` 字段白名单），故无需四处同步；前端 `TOKEN_FIELDS` 早已包含两字段（O0-01 契约测试已锁定）。

## 3. 测试与门禁（本机，pwsh；2026-10-18）

```powershell
# 后端（itsm-backend）
go test ./service/ -count=1                 # ok 54.0s（含新增 llm_usage_test.go）
go test ./handlers/ai/ ./router/ ./middleware/ -count=1   # ok 7.4s / 5.4s / 2.3s
go test ./tests/botintegration/ -count=1    # ok 4.1s（真实 handler → 真实 RAG → SSE 帧断言）
# 前端（itsm-frontend）
npx jest src/lib/ai/__tests__/chat-telemetry.test.ts src/lib/api/__tests__/ai-api.test.ts src/components/ai/diagnostics --coverage=false --watchAll=false --runInBand --silent
# → 4 suites / 81 tests 全绿
npx jest src/components/ai src/lib/ai src/lib/api --coverage=false --watchAll=false --runInBand --silent
# → 127 suites / 1663 tests 全绿（较上批 +8，全部为本批新增）
npx tsc --noEmit        # exit 0
npx eslint（11 个改动文件）  # exit 0
```

新增测试用例（关键断言）：

| 层 | 用例 | 断言 |
| --- | --- | --- |
| service | `TestEstimateTextTokens_Heuristic` / `TestEstimatePromptTokens_SumsMessages` | 字符/4 口径；rune 计数（中文 4 字 = 1）；空串/不足 4 字符 = 0 |
| service | `TestChat_ReportsUsageEstimateToSink` | 真实网关调用把**与限流同源**的估算上报 sink（40+20 字符 → 15） |
| service | `TestReportUsageEstimate_NoSinkAndPanicContainment` | 未注入 = no-op；sink panic 不冒泡；nil sink 不注入 |
| handlers/ai | `TestWriteUsageEvent_EnvelopeAndZeroValue` | v2 信封；`estimated=true` 与 `scope=turn` 恒在；**零值也发送** |
| handlers/ai | `TestSSERegistry_AllEmittedEventsRegistered` | `usage` 进入发送点清单（新增事件必须已登记） |
| 集成 | `TestB1ChatStreamWritesRunArchive`（增强） | 真实链路 1 个 usage 帧、`estimated=true`、**usage 索引 < done 索引**（顺序契约） |
| 前端 api | 新增 2 例 | usage 帧 → `onUsage` + `done.info.usage`；**畸形载荷丢弃**且 done 回退单参数旧形态 |
| 前端埋点 | 新增 2 例 | `stream_end` 落 tokenIn/Out + `extra.tokenEstimated`；无 usage 时字段不落 |
| 前端模型/抽屉 | 新增 4 例 | 用量取最近一条带 usage 的助手消息（无 run 也行）；文案恒带 `≈`；抽屉显示「用量（估算）」与「非账单口径」；无数据 → 不渲染该段并回退说明 |

## 4. 端到端样例（真实链路帧，节选）

```text
event: run_started   data: {"v":2,"runId":1,...}
event: step          data: {"v":2,"stepIndex":0,"type":"llm",...}
event: delta         data: {"content":"..."}
event: usage         data: {"estimated":true,"scope":"turn","tokenIn":0,"tokenOut":0,"v":2}   ← 本批新增
event: done          data: {"conversationId":1}
```

> 上例 `tokenIn=0` 为「无网关（关键字检索路径）」的真实估值——**零值如实呈现**，不伪造调用量。

## 5. 边界条件与诚实声明（未竟项）

1. **口径 = 估算**：字符数 ÷ 4（含系统提示/历史上下文/工具循环累加），与限流同源；**非** provider 真实 usage、**非**账单口径。真实 usage（按 provider 协议解析 `usage` 回包）归 B4-02 专项；届时以新增字段扩展，不改 `usage` 事件语义。
2. **失败回合不发送 usage**：流以 `error` 结束时当前不补发 usage（成功路径 `done` 前才发）。失败回合成输入成本可见性归错误路径专项。
3. **非 SSE 回退路径**（环境不支持流式时的 JSON 回退）不含 usage。
4. **仅 Dock 面板入口**：工作区 `/ai/chat` 的诊断入口复用仍为 O2-02 条件项（本批未动）。
5. **provider 粒度**：usage 为回合级（`scope=turn`），未按单次 LLM 调用/模型拆分。

## 6. 附带修复（轮次外发现，如实登记）

`tests/botintegration` 三例（`TestB1ChatStreamWritesRunArchive` / `...RunManagerSequenceMatchesDB` / `...BudgetAbortEndToEnd`）在本批开跑时**已是红灯**（与 token 改动无关：`git show HEAD:handlers/ai/service.go` 的 `botEnabled` 已同语义——策略未注入 = 关闭态，而测试夹具从未注入策略）。本批为其注入 `bot.Policy`（空模板库 → 兼容默认）后三例转绿（4.1s），并顺势补上 usage 帧的端到端断言。

## 7. 变更记录

| 日期 | 变更 |
| --- | --- |
| 2026-10-18 | 初稿：O2-02 token 段接线（后端 6 个门禁点统一估算 + v2 `usage` 事件；前端解析/埋点/抽屉三段）；11 个前端文件 + 5 个后端文件 + 2 个测试文件改动；后端 4 包 + 前端 127 suites/1663 tests 全绿；登记 5 项边界条件 |