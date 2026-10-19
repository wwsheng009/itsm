# B4-02 真实 usage 解析：provider 协议回包 → 回合口径收口（2026-10-18）

- 任务：方案 `docs/plan/ai-assistant-dock-plan-2026-10-18.md` §13.4「轨迹/耗时/token 可见」的
  最后一段诚实性缺口：token 不再只有字符/4 估算，provider 回包带 usage 时以**实测**收口；
  同时保持既有 `usage` v2 事件语义（同字段、`estimated` 标记区分口径，零破坏）。
- 状态：`integration_verified`（四协议适配器解析 + 适配器路径端到端 + SSE 事件 + 前端展示/埋点全绿；
  真实 provider 联调待环境）。

## 1. 交付物

| 层 | 路径 | 说明 |
| --- | --- | --- |
| 协议适配 | `itsm-backend/internal/llm/protocol/adapter.go` | `ProcessResult.Usage` + `Usage` 类型 + 四个容错解析器 |
| 协议适配 | `protocol/openai_chat.go` | 非流式 `usage`；流式 usage 末帧（先于 choices 判空）；`stream_options.include_usage` 请求 |
| 协议适配 | `protocol/anthropic_messages.go` | `message_start.message.usage`（输入）+ `message_delta.usage`（输出）分帧累加 |
| 协议适配 | `protocol/google_gemini.go` | `usageMetadata.promptTokenCount/candidatesTokenCount`（流式/非流式共用累积状态） |
| 协议适配 | `protocol/openai_responses.go` | `response.completed.response.usage`（流式）与顶层 `usage`（非流式） |
| 上报通道 | `service/llm_usage.go` | `WithProviderUsageSink` / `reportProviderUsage`（context 携带、nil=no-op、panic 隔离）+ `ResolveTurnUsage` 口径收口 |
| 接线 | `service/llm_registry.go` | `protocolProvider.call` 解析到 `Usage` 时上报 |
| 接线 | `service/llm_providers.go` | SDK 路径：`stream_options.include_usage` + 末帧/非流式 usage 上报（4 处：Chat/ChatStream/ChatStreamWithTools/StreamingOpenAIProvider） |
| 聊天层 | `handlers/ai/service.go` | 双 sink 累加（估算 + 实测），`finalUsage()` 收口（全覆盖才置 `estimated=false`） |
| SSE | `handlers/ai/sse_events.go` | `estimated` 由 `LLMUsageEstimate.Estimated` 透出；零值仍按估算标注（不冒充实测） |
| 前端 | `src/lib/api/ai-api.ts`、`chat-telemetry.ts`、`use-ai-chat-session.ts`、`diagnostics/*` | `estimated=false` 原样保留；诊断抽屉显示「用量（provider 实测）」与实测口径说明；埋点 `extra.tokenEstimated` 透传 |

## 2. 契约（口径与边界）

- **同字段、加标记**：`usage` v2 事件字段不变（`tokenIn/tokenOut/estimated/scope`）；
  `estimated=true|缺省` = 字符/4 估算（与限流同源）；`estimated=false` = provider 回包实测原值
  （prompt/completion；不做账单映射、不做缓存/推理细分）。旧后端/旧前端零行为变化。
- **覆盖判定**：`ResolveTurnUsage(estimate, actualIn, actualOut, actualReports, llmCalls)` ——
  provider 实测报告数 > 0 且 ≥ 网关门禁上报的调用次数（含重试）→ 实测收口；
  否则保持估算（部分覆盖不冒充全量）。
- **容错**：任一协议解析不到 usage / 字段类型不符 / 值为 0 占位 → `Usage=nil`，sink 静默，
  回落估算；未注入 sink 的既有调用方（A2UI/agent/BPMN）零变化。
- **请求侧**：openai_chat 适配器与 SDK 流式请求新增 `stream_options.include_usage`（B4-02 需要）；
  非流式请求体不变。DeepSeek/OpenAI 类端点原生支持；不支持该字段的网关按未知字段忽略（回归测试锁定本地桩）。

## 3. 复跑命令与实测结果

```powershell
# 后端 itsm-backend
gofmt -l -w internal/llm/protocol/{adapter,openai_chat,anthropic_messages,google_gemini,openai_responses}.go service/{llm_usage,llm_registry,llm_providers}.go handlers/ai/{service,sse_events,sse_events_test}.go
go build ./service/ ./handlers/ai/ ./internal/bootstrap/ ./internal/llm/...   # exit 0
go test ./internal/llm/... ./handlers/ai/ -count=1                            # protocol ok 2.5s / handlers/ai ok 6.6s
go test ./service/ -run 'TestProviderUsageSink|TestResolveTurnUsage|TestProtocolProvider' -count=1 -v  # 13/13 PASS
go test ./service/ -count=1                                                   # ok 54.9s（全包）

# 前端 itsm-frontend
npx tsc --noEmit                                                              # exit 0
npx eslint <9 changed files>                                                  # exit 0
npx jest src/components/ai src/lib/ai src/lib/api --coverage=false --watchAll=false --runInBand --silent
# 128 suites / 1672 tests 全绿（含新增：诊断文案实测分支、drawer 实测段、chat-telemetry 口径透传、ai-api estimated=false 解析）
```

新增测试（21 例）：

- `internal/llm/protocol/usage_test.go`（4 例）：四协议非流式 + 流式 usage 解析；OpenAI 兼容无 usage → nil。
- `service/llm_registry_usage_test.go`（4 例）：适配器流式端到端（末帧 usage → sink 原值）；请求体含
  `stream_options.include_usage`；无 usage 时 sink 静默；非流式有/无 usage 两态。
- `service/llm_provider_usage_test.go`（4 例）：sink nil/no-op/panic 隔离；多轮累加；`ResolveTurnUsage`
  五态（全覆盖/多轮全覆盖/部分覆盖回落/零实测/零调用兜底）。
- `handlers/ai/sse_events_test.go`：零值与估算帧保持 `estimated=true`；实测帧 `estimated=false` 原样透出。
- 前端 4 例：`formatTokenUsage` 实测文案；drawer 实测段渲染（tag/口径/页脚）；`tokenEstimated=false` 埋点；ai-api 解析保真。

## 4. 未覆盖 / 诚实边界

- **真实 provider 联调未做**（本机 Docker/外网受限）：各协议 usage 字段按官方文档与本地桩锁定；
  真实 DeepSeek/Anthropic/Gemini 回包形状差异待联调验证（已在方案保留条件项）。
- **非流式 SDK `OpenAIProvider.Chat` 的 usage** 仅在回包存在字段时上报；静态回退端点若不回 usage，
  保持估算 —— 与流式 `include_usage` 的兼容性风险由「网关忽略未知字段」假设兜底，联调时复核。
- 不做账单映射：`total_tokens`、缓存命中、推理 token 等细分字段不入本事件（避免伪造成本口径）。
- 部分覆盖（如重试成功但无 usage）保持估算，可能出现「同会话不同回合口径不同」——界面按回合标注，
  由 `estimated` 显式区分。
- 观察到的既有 flake：`service` 包全量测试两次出现 `TestBiz_MultipleInstancesIndependent`
  （`process_tasks.task_id` 唯一约束）失败，单测与随后两次全量均通过；与本次改动无交集
  （process_tasks vs LLM usage），登记为既有测试稳定性问题。