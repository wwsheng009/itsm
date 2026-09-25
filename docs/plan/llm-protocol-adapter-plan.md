# LLM 协议适配层方案（4 种 API 形态归一化）

> 文档类型：技术方案 + 实施计划（v1.3：PA-1 / PA-2 / PA-3 / PA-4 已落地；适配器维度收敛为「协议」）
> 适用范围：`itsm-backend`（Go）、`itsm-frontend`
> 编制日期：2026-09-24（v1.0 / v1.1 / v1.2 / v1.3：2026-09-25）
> 版本：v1.3（**一协议一实现**：`variant` 降为构造期选项；PA-4 交付 `google_gemini` 适配器，4/4 协议适配器化收官）
> 目标读者：后端、前端、测试
> 关联文档：`docs/plan/multi-llm-provider-plan.md`（主计划；槽位定义见其 §2.2 D13、§3.1.4、§11，排期与启动门禁见其 §5.4）、`docs/documentation-governance.md`
> 外部参考实现（只读参考，不引入依赖）：`E:\projects\ai-agent-runtime\backend\internal\llm\adapter\`

---

## 0. 状态与目的

- 本文件在 v0.1–v0.7 期间是**槽位占位（Stub）**，自 **v1.0 起为正式方案**：主计划《多 LLM Provider 支持与可切换方案》预留的 5 类槽位（枚举 / `variant` + `adapter_options` / 协议层 / 能力位 / 错误码）在此逐项落地。
- **P0 基线（主计划 BE-9，已交付）**：`itsm-backend/internal/llm/protocol`（`ProtocolAdapter` 接口 + `Registry` + `openai_chat_completions` 适配器 + 对照测试 `equivalence_test.go`）与 `service/llm_registry.go` 构建分派（`LLM_PROTOCOL_ADAPTER_ENABLED` 默认关，关闭时构建路径完全不经过协议包）。
- **v1.1 口径收敛（2026-09-25，本次）**：适配器维度 = **协议本身**（至多 4 个，与协议枚举一一对应）；`variant` 仅作为**构造期选项**（endpoint 归一、字段口径、参数差异），不构成独立适配器身份、不进注册表键。原 PA-2「azure / ollama 变体适配器化」随之收敛：不新增 `openai_chat_variants.go` 等变体文件，两变体由 `openai_chat_completions` 同一适配器承载（azure：endpoint 归一 + model 回退 deployment；ollama：统一走 OpenAI 兼容端点）。
- **v1.2 PA-3 交付（2026-09-25，本次）**：`openai_responses` 从"预留槽位"变为**已实现**——适配器 `internal/llm/protocol/openai_responses.go`（官方 Responses API 载体，Q1 结论见 §8）+ 注册表登记 `{"", }` 变体白名单 + 接线层槽位 `implemented=true`；协议包已适配 **3/4**（仅 `google_gemini` 待 PA-4，未实现协议仍走 `AI_PROTOCOL_NOT_IMPLEMENTED`(422)）。无旧分支对照，差异/决策逐条登记 §4.3 PA-3 表。
- **v1.3 PA-4 交付（2026-09-25，本次）**：`google_gemini` 从"预留槽位"变为**已实现**——适配器 `internal/llm/protocol/google_gemini.go`（Gemini API v1beta：`/v1beta/models/{model}:generateContent` 与 `:streamGenerateContent?alt=sse`，`x-goog-api-key` 鉴权）+ 注册表登记 `{""}` 变体白名单 + 接线层槽位 `implemented=true`；协议包 **4/4** 协议全部适配器化，`AI_PROTOCOL_NOT_IMPLEMENTED`(422) 仅剩"协议未注册（枚举外）/变体不在白名单"两种来源。资源路径随模型与流式形态变化，新增可选接口 `protocol.ModelPathAdapter` 在请求期解析（差异/决策登记 §4.3 PA-4 表）。
- **前置件（主计划 BE-8，已交付）**：`service/llm_protocol_slot.go`（4 值协议枚举常量、`(protocol, variant) → 既有分支` 映射、variant 白名单、能力位、`adapter_options` 校验与哨兵错误）+ 单测；本计划直接复用，不重复定义协议常量与校验逻辑。
- **启动门禁（主计划 §5.4，已达成）**：B5 出口各项已满足——BE-8 随 B2、BE-9/BE-5 随 B5 落地；B6 前端批次完成（主计划 v1.13）；QA-3 开关关闭全链路回归 13/13 维度通过（`docs/testing/multi-llm-provider-qa3-regression-2026-09-24.md`，主计划 v1.14）；Postgres 空库/存量库 up+down 全绿、prod 账本阻塞解除（主计划 v1.11/v1.12）。**2026-09-25 起本计划解冻**，`NewProviderFromConfig`、既有 4 个实现分支与 `internal/llm/protocol/` 的改动按本文件 PA 任务执行。
- **不变量（全程约束）**：不改主计划已定的 DB 结构（零迁移）、不改主计划 §3.4 API 契约、开关关闭时行为与单 provider **逐字节一致**、前端仅把"待接入"置灰选项打开。

## 1. 目标与非目标

对 4 种 API 形态提供统一适配层：

| 协议（枚举值） | 形态 | 参考实现坐标 |
|:---|:---|:---|
| `openai_chat_completions` | OpenAI Chat Completions（**P0 已交付：BE-9**） | `adapter/openai.go:1111`（`/v1/chat/completions`） |
| `openai_responses` | OpenAI Responses（参考实现以 `codex` 命名承载；**PA-3 已交付**） | `adapter/codex.go:876`（`/v1/responses`） |
| `anthropic_messages` | Anthropic Messages（**PA-1 已交付**） | `adapter/anthropic.go:795`（`/v1/messages`） |
| `google_gemini` | Google Gemini（**PA-4 已交付**） | `adapter/gemini.go:591-595`（v1beta，资源路径含模型名与流式操作） |

归一化范围：请求构建、响应解析、流式事件、工具调用（含流式累积）、推理链（thinking/reasoning）、错误映射、能力声明与探测。

**非目标（本计划不做）**：不新增 DB 字段与迁移；不改 `docs/api-reference.md` 的既有契约（仅数值型能力位随实现收敛）；不引入参考实现的 `providercompat` 兼容管道（vendor profile 层）——itsm 侧以 `variant` 表达同协议下的厂商差异，复杂度按需引入。

## 2. 主计划承诺交付的槽位（本计划的输入）

| # | 槽位 | 主计划位置 | 本计划要做的事 | 状态 |
|:---|:---|:---|:---|:---|
| 1 | 协议枚举 4 值 | D3、§3.1.1、§3.1.4 | 用真实适配器替换"映射到既有分支"（**一协议一实现**） | ✅ 全覆盖：`openai_chat_completions`（BE-9；含 `azure` / `ollama` 变体，PA-2 收敛）、`anthropic_messages`（PA-1，含 `minimax` 变体）、`openai_responses`（PA-3）、`google_gemini`（PA-4） |
| 2 | `variant` + `adapter_options` | §3.1.1、§3.5 | 落地语义：Azure endpoint 归一 + model 回退 deployment、Ollama 归一到 OpenAI 兼容形态、Anthropic 兼容端点（minimax camelCase） | minimax ✅ PA-1；azure/ollama ✅ PA-2（同一适配器的构造期选项，不新增变体适配器） |
| 3 | 协议层：接口 + 注册表 + 首适配器 | §3.2、§11.2 | 复用 BE-9 基线，新增其余适配器 | 4/4 协议已适配（`openai_chat_completions` ✅ BE-9/PA-2、`anthropic_messages` ✅ PA-1、`openai_responses` ✅ PA-3、`google_gemini` ✅ PA-4）；另新增可选接口 `protocol.ModelPathAdapter`（路径依赖模型的协议在请求期给出资源路径） |
| 4 | 能力位 | §3.4 `available`、§3.7 | `supportsStream` / `supportsTools` / `supportsReasoning` 的真实值收敛 | 待 PA-5 |
| 5 | 错误码 `AI_PROTOCOL_NOT_IMPLEMENTED` | §3.4 | 随适配器逐个就绪而收敛（枚举值从"置灰"变为"可用"） | PA-4 收官：4 值枚举全部 `implemented=true`、变体白名单就位（422 仅剩"枚举外协议 / 白名单外变体"）；`supportsStream/supportsTools/supportsReasoning` 的**开关感知**收敛仍随 PA-5 |

## 3. 参考实现要点（`E:\projects\ai-agent-runtime`，只读）

- **接口形状**：`ProtocolAdapter`（`backend/internal/llm/adapter/adapter.go:70-114`）：`Name` / `BuildRequest` / `BuildHeaders` / `ExtractResponse` / `ExtractReasoning` / `ExtractStreamContent` / `ExtractStreamReasoning` / `BuildAssistantMessage` / `HandleResponse` / `ProcessResponse` / `IsReasoningModel` / `GetAPIPath`。itsm 侧收敛为 7 个方法（无 `BuildAssistantMessage`，会话历史由 service 侧组装的 `LLMMessage` 表达）。
- **注册表**：`NewAdapter(providerType)` switch 分派，未知类型报错（不允许静默退化）；`GetAdapterOrDefault` 仅供兜底（`adapter/factory.go:9-31`）。itsm 侧对应 `protocol.Registry`（精确命中，未命中返回 `ErrAdapterNotFound` 由 service 决定回退或 422）。
- **统一流式回调**：`StreamCallbacks{OnText, OnReasoning, OnImage}` + `HandleResponse(isStream, respBody, callbacks)`（`adapter/adapter.go:39-114`）；Anthropic 为事件驱动 SSE（`adapter/anthropic.go:509-560`）。itsm 侧 `StreamCallbacks` 只保留 `OnText` / `OnReasoning`（无图片事件消费方）。
- **Anthropic 事件序列（PA-1 对标）**：`message_start` → `content_block_start` → `content_block_delta`（`text_delta` / `thinking_delta` / `input_json_delta`）→ `content_block_stop` → `message_delta`（`stop_reason`）→ `message_stop`，另有 `ping` 与带内 `error` 事件；鉴权 `x-api-key` + `anthropic-version`（`adapter/anthropic.go:204-214`）；`max_tokens` 必填、thinking 与 temperature 互斥（`adapter/anthropic.go:35-95`）。
- **厂商差异示例**：MiniMax 的 Anthropic 兼容端点为 camelCase（`maxTokens` / `stopReason`），itsm 既有 `service.MiniMaxProvider` 即按此口径实现（`service/llm_providers.go:399-424`）——本计划把它登记为 `anthropic_messages` 的 `minimax` 变体，而非独立协议。
- **厂商兼容层（与协议适配器正交，参考实现命名 `profile`）**：`providercompat/` 以 `NewChain(Context)` 构建兼容管道；`Context{ProviderName, Protocol, BaseURL, APIPath, Profile, Model, ConfiguredCapabilities, ResponseMarkers}` 把"协议"与"厂商 profile"作为两个独立维度（`providercompat/providercompat.go:10-42`）——与本方案 `protocol` + `variant` 建模一致（`variant` ≈ 参考实现的 `Profile`）。
- **能力声明与推理档位**：profile 可提供能力默认值（如 DeepSeek：`ReasoningModel=true`、`ReasoningEfforts=[high,max]`，`providercompat/openai_deepseek.go:22-31`）；模型能力解析为精确匹配 + `*` 通配兜底（`internal/llm/model_capability.go:16-36`）。
- **协议规格文档（可直接对标产出）**：参考实现为每个协议维护 `adapter/*_parser.md`（`openai_parser.md` / `anthropic_parser.md` / `codex_parser.md` / `gemini_parser.md`）；流式 / 推理 / 工具容错各有专项文件（`adapter/sse.go`、`adapter/thinking.go`、`adapter/malformed_tool_call.go`、`adapter/mcp_meta_tools.go`）。

## 4. 详细设计

### 4.1 契约与不变式

- **一协议一实现（v1.1 收敛）**：注册表按**协议**索引（每协议一个 `AdapterSpec{Variants, New}`，`Registry.Register(protocol, spec)` 静态注册，启动期一次、运行期只读，读写加锁）；`variant` 只声明该协议支持哪些变体并作为**构造期选项**传给 `New(variant)`（如 `NewAnthropicMessagesAdapter(variant)` 决定 `max_tokens` vs `maxTokens`，`openai_chat_completions` 的 azure / ollama 只影响 endpoint 与调用参数），不构成独立适配器身份、不进注册表键。厂商/部署差异在构造期固化进实例，请求期不查表、不分支；适配器无状态、可被多 goroutine 并发复用。
- **纯函数边界**：`BuildRequest` / `BuildHeaders` 不触网、不读全局配置；`HandleResponse` 只消费 `io.Reader` + 回调，不持有 ctx/HC——HTTP 由 `service.protocolProvider` 承担（`service/llm_registry.go`）。
- **不 import service 包**：协议包内 `Message` / `Tool` / `ToolCall` 是 `service.LLMMessage` 的最小镜像，接线层（`toProtocolMessages` / `toProtocolTools` / `toLLMToolCalls`）负责转换，避免循环依赖。
- **开关语义**：`LLM_PROTOCOL_ADAPTER_ENABLED`（env 优先，其次 `config.yaml llm.protocol_adapter_enabled`，默认 false）。开关关闭时构建路径**完全不经过协议包**（QA-3 已归档零 diff 证据）；开启时协议未注册、或变体不在该协议的支持范围内 → 回退旧分支（静态配置路径）或 `ErrProviderUnavailable`/`AI_PROTOCOL_NOT_IMPLEMENTED`(422)（DB 实例路径）。
- **错误双身份**：适配器 HTTP 错误经 `protocol.NewHTTPError` 映射 `*protocol.ProtocolError`，service 侧包装为 `*openai.APIError` + `*protocol.ProtocolError` 双 `Unwrap`，网关 `isTransientLLMError` 的 429/5xx 重试口径不变。
- **带内错误事件**（无 HTTP 语义，`StatusCode=0`）一律不重试：Anthropic `error` 事件、OpenAI `[DONE]` 之前的错误体。

### 4.2 变体语义表（协议 × 变体 → 承载与线上口径）

| 协议 | 变体 | 承载 | 缺省 endpoint（endpoint 为空时） | 关键字段口径 | 鉴权头 | 状态 |
|:---|:---|:---|:---|:---|:---|:---|
| `openai_chat_completions` | `""` | 适配器（BE-9） | `https://api.openai.com` | `max_tokens` / `temperature` / `tools[].function` | `Authorization: Bearer` | ✅ |
| `openai_chat_completions` | `azure` | 适配器（PA-2 收敛） | —（必须显式配置；为空则回退 OpenAI 兼容默认地址） | 主机名 endpoint 自动补 `/openai` 后拼 `/v1/chat/completions`（与旧分支同址）；`model` 缺省回退 `deployment`；旧分支 `apiVersion` 字段从未使用，**不引入** `api-version` 查询参数 | `Authorization: Bearer`（旧 go-openai 默认口径） | ✅ |
| `openai_chat_completions` | `ollama` | 适配器（PA-2 收敛） | `http://localhost:11434`（旧 `LocalProvider` 同值） | OpenAI 兼容 `/v1/chat/completions`（**有意差异**：旧分支走 Ollama 原生 `/api/chat`，见 §4.3） | 无（apiKey 为空时不下发 `Authorization`） | ✅ |
| `anthropic_messages` | `""` | 适配器（PA-1） | `https://api.anthropic.com` | `max_tokens` / `stop_reason` / `system` 顶层 / `tools[].input_schema` / 工具结果走 `user` + `tool_result` | `x-api-key` + `anthropic-version: 2023-06-01` | ✅ |
| `anthropic_messages` | `minimax` | 适配器（PA-1） | `https://api.minimaxi.com/anthropic/v1` | 同上，字段名 camelCase：`maxTokens` / `stopReason`；`temperature` 默认 **1.0**（旧分支取值） | 同上 | ✅ |
| `openai_responses` | `""` | 适配器（PA-3） | `https://api.openai.com`（不单独登记 → 与 `openai_chat_completions` 标准形态同址回退；资源路径 `/v1/responses` 由适配器给出） | `input` 项数组（message / function_call / function_call_output）、`instructions` 顶层、`max_output_tokens`、`store:false` 恒下发、tools 扁平结构（无嵌套 `function`） | `Authorization: Bearer` | ✅ |
| `google_gemini` | `""` | 适配器（PA-4） | `https://generativelanguage.googleapis.com`（协议包登记；资源路径 `/v1beta/models/{model}:generateContent` / `:streamGenerateContent?alt=sse` 由适配器按请求给出） | `contents[]`（`user` / `model`）+ 顶层 `systemInstruction`、`tools[].functionDeclarations`、响应 `parts[].functionCall`（无 id → 适配器生成 `call_N`）、`finishReason` 原生值透传 | `x-goog-api-key`（**决策：不复刻 `?key=` 形态**，密钥不进 URL/日志） | ✅ |

**端点解析规则**（`service.resolveProtocolURL` + `service.normalizeVariantEndpoint`，与既有 config.yaml 写法对齐）：endpoint 为空 → 取 `protocol.DefaultEndpoint(protocol, variant)`（协议包登记值：anthropic 官方 / minimax、ollama 本机地址、gemini 官方地址），未登记组合回退 `https://api.openai.com`（BE-9 行为不变）；azure 变体在 endpoint 只给主机名（无路径）时先补 `/openai`（旧分支固定 `{endpoint}/openai/v1`），已带路径时原样透传；endpoint 已带版本段（`/v1`、`/v1beta`）→ 只补资源段（`/v1/messages` → `/messages`、`/v1beta/models/…` → `/models/…`）；已含完整路径 → 原样使用。**动态资源路径**：适配器可实现可选接口 `protocol.ModelPathAdapter`（`APIPathFor(model, stream)`），接线层 `protocolProvider` 在请求期优先取该方法、未命中回退静态 `GetAPIPath()`——当前仅 `google_gemini` 需要（路径含模型名与操作）。

### 4.3 差异清单（适配路径 vs 旧分支，逐条登记）

**PA-1（`anthropic_messages` / 含 `minimax` 变体）**：

| # | 差异 | 性质 | 处置 |
|:---|:---|:---|:---|
| 1 | 请求体 JSON 键序不同（适配器用 `map` 序列化，Go 按字典序；旧分支用 struct 序列化） | 无线上影响（HTTP 报文语义不含键序） | 对照测试按**字段集合与取值**逐项锁定 |
| 2 | 支持流式 + 工具调用（旧 `MiniMaxProvider` 只有非流式 `Chat`） | 能力增益 | PA-5 收敛能力位；前端置灰项随之打开 |
| 3 | 非流式正文：拼接全部 `text` 块；无 `text` 块返回空串（旧分支取首个 `text` 块，无文本时返回错误 `MiniMax: no text content in response`） | 行为差异（超集） | 已登记；`tool_use`-only 响应是合法形态，返回空串由上层决定展示 |
| 4 | 客户端超时由调用方 ctx 控制（旧分支 `http.Client{Timeout: 120s}`） | 有意差异（与 BE-9 openai 路径同口径，避免长流被客户端级超时截断） | 部署侧如需超时可经 `HTTPClient` 注入 |
| 5 | `model` 覆盖只对本次调用生效（不写回实例） | 并发安全修正（旧分支 `p.model = model` 写共享字段） | 已登记（BE-9 同口径） |
| 6 | `temperature` 默认值按变体解析：minimax 1.0 / 官方 anthropic 0.3 | 等价 + 契约化（旧分支硬编码 1.0） | `protocolProviderVariantTemperature`，单测锁定 |

**PA-2（`openai_chat_completions` 的 `azure` / `ollama` 变体；v1.1 收敛为同一适配器承载，不新增变体适配器）**：

| # | 差异 | 性质 | 处置 |
|:---|:---|:---|:---|
| 1 | azure：endpoint 只给主机名时，适配层自动补 `/openai` 再拼 `/v1/chat/completions`（旧 `AzureProvider` 固定 `{endpoint}/openai/v1` + `/chat/completions`） | 等价（同址） | `service.normalizeVariantEndpoint` + `TestAzureAdapterMatchesLegacyAzureProvider` 对照锁定 |
| 2 | azure：`model` 缺省回退 `deployment`（旧分支 `actualModel := deploymentID`） | 等价 | `service.llmProviderModelForVariant`；单测 + registry 用例锁定 |
| 3 | azure：鉴权保持 `Authorization: Bearer`；旧分支 `apiVersion` 字段从未参与请求（无 `api-version` 查询参数） | 等价（不新增语义） | 不引入 `api-version`；`adapter_options.api_version` 仍只存储/回显（BE-8 边界） |
| 4 | ollama：线上形态由 Ollama 原生 `/api/chat`（请求 `options` / 响应 `response`）归一为 OpenAI 兼容 `/v1/chat/completions`（Ollama 官方支持） | **有意差异（协议归一）** | 以「一协议一实现」为准，不复刻原生形态；开关关闭时仍走旧 `LocalProvider`，可回退 |
| 5 | ollama：请求体下发 `max_tokens` 4096 / `temperature` 0.3（旧分支下发 `options` 零值、无 `max_tokens`） | 有意差异（沿用 BE-9 默认值表） | 如需原生采样参数，后续经 `adapter_options` 扩展并登记 |
| 6 | azure / ollama：支持流式与工具调用（旧 `AzureProvider` / `LocalProvider` 只有非流式 `Chat`） | 能力增益 | PA-5 收敛能力位；前端置灰项随之打开 |

**PA-3（`openai_responses`；itsm 侧无旧分支对照——此前为 422 预留槽位，故下表是「载体决策 + 与参考实现 `codex.go` 的口径对照」逐条登记）**：

| # | 差异 / 决策 | 性质 | 处置 |
|:---|:---|:---|:---|
| 1 | 载体取**官方 Responses API**（`POST /v1/responses` + `Authorization: Bearer`），不复刻参考实现的 ChatGPT backend 形态（`chatgpt.com/backend-api/codex` 一类，依赖 OAuth/session 凭证与专有头部） | 有意差异（Q1 结论，§8） | itsm 实例模型是「endpoint + api_key」，官方形态与 `openai_chat_completions` 鉴权/端点解析规则同构；backend 形态如后续需要，按新 `variant` 构造期选项引入（不改协议枚举） |
| 2 | `store` 恒为 `false`（关闭服务端响应存储）；不实现 `previous_response_id` / conversation 续接 | 有意差异（无状态口径，与参考实现 stateless 声明一致） | itsm 每次重放完整历史（`input` 数组）；如需服务端存储，后续经 `adapter_options` 扩展并登记 |
| 3 | 前导 system/developer 消息合并为顶层 `instructions`（`\n\n` 连接）；其余（含更靠后的）system 保留为 `role=developer` 的 message 项 | 形态归一（Responses 无 `role=system` 的 message 项） | 单测逐字段锁定；Chat 适配器的 system 消息语义由此映射 |
| 4 | 历史工具往返改为**独立项**：`function_call`（`call_id` / `name` / `arguments`）与 `function_call_output`（`call_id` / `output`），不再用 chat 的 `assistant.tool_calls` + `role=tool` | 形态归一 | `call_id` 缺失回退项 `id`；两者都缺的 `function_call` 项丢弃（不构造半成品工具调用）；项顺序按会话历史次序 |
| 5 | `tools` 为**扁平**结构（`{type:function,name,description,parameters}`，无嵌套 `function`）；`tool_choice` 归一：Chat 风格 `{"type":"function","function":{"name":…}}` 展平，Responses 风格与 `auto`/`required`/`none` 原样透传，不可识别不下发 | 形态归一 | 与参考实现 codex 适配器同口径；单测覆盖三种形态 |
| 6 | 采样参数：`max_output_tokens` 仅在 >0 时下发（接线层缺省 4096）；推理模型（`isOpenAIReasoningModel` 同源判定：`codex` / `gpt-5` / `o1`–`o5` 前缀）抑制 `temperature`（缺省 0.3 沿用 BE-9 默认值表） | 等价口径（与 chat 适配器同表） | 见 §4.4；单测锁定"推理模型不下发采样参数" |
| 7 | 流式事件：`response.output_text.delta` → 文本增量；`response.reasoning_summary_text.delta` → 推理增量；工具参数按 `response.function_call_arguments.delta` 累积（`output_item.added/done` 建/收口项）；`response.completed` / `response.incomplete` / `response.done` 先由快照补齐未落的 done 增量，再定 `finishReason`（有工具调用 → `tool_calls`，否则 `stop`）；`incomplete_details.reason` 作为 finishReason 透出 | 事件驱动 SSE（与 chat 的 `choices[].delta` 形态不同） | 单测覆盖增量顺序、工具累积与快照恢复（增量为准、不重复） |
| 8 | 失败语义：`response.failed` 与顶层 `error` 事件 → `*ProtocolError{StatusCode:0}`（**带内错误，一律不重试**）；`response.status=failed` 时优先取 `response.error` 子文档，否则回退顶层 `error`；事件顶层 `code`/`message` 形态兼容 | 错误双身份 + 不重试口径（与 PA-1 带内错误一致） | 非流式 `response.failed` 与流式 `error` 事件单测各覆盖；HTTP 层 4xx/5xx 仍走 `NewHTTPError`（重试口径不变） |
| 9 | 容错：未知 `input`/事件项类型忽略；`output` 中非 `message`/`function_call` 项跳过（如内置工具产物）；多 `output_text` 片段按序拼接，`refusal`/空输出按空正文处理 | 容错口径（对齐参考实现解析器的"忽略未知"原则） | 单测覆盖边缘项；不引入内置工具（image_generation 等）语义 |

**PA-4（`google_gemini`；itsm 侧无旧分支对照——此前为 422 预留槽位，故下表是「载体决策 + 与参考实现 `gemini.go` 的口径对照」逐条登记）**：

| # | 差异 / 决策 | 性质 | 处置 |
|:---|:---|:---|:---|
| 1 | 载体取官方 **Gemini API v1beta**（`POST /v1beta/models/{model}:generateContent`；流式 `:streamGenerateContent?alt=sse`），不复刻参考实现的 `?key=` 查询参数形态 | 有意差异（密钥不进 URL/日志） | 鉴权固定 `x-goog-api-key` 头（参考实现两种都支持）；`?key=` 形态会把密钥写进 URL 与代理访问日志，与 itsm 密钥治理相悖 |
| 2 | 资源路径含模型名与流式操作 → 新增可选接口 `protocol.ModelPathAdapter`（`APIPathFor(model, stream)`）；`GetAPIPath()` 保留 `/v1beta` 静态回退（模型为空时交上游语义报错，不构造半成品路径） | 能力增益（路径依赖模型） | 接线层 `service.protocolProvider` 请求期优先取 `APIPathFor`、未命中回退 `GetAPIPath`；`resolveProtocolURL` 对已带 `/v1beta` 的 endpoint 只补资源段 |
| 3 | 缺省 endpoint 登记官方地址 `https://generativelanguage.googleapis.com`（协议包 `DefaultEndpoint`） | 等价口径（官方同址） | endpoint 留空即回退；自填端点（代理/网关）原样使用，资源路径仍由适配器给出 |
| 4 | 会话归一：前导 system → 顶层 `systemInstruction`（`\n\n` 连接）；非前导 system 降级为 `user` 文本项；assistant → `model`；工具结果 → `user` + `functionResponse`（函数名按会话历史 `functionCall` 反查，缺失用占位名；`response` 固定 JSON 对象，空结果回退 `{"result":""}`） | 形态归一（Gemini 的 `contents[].role` 只有 `user`/`model`） | 单测逐字段锁定；不下发 Gemini 不接受的角色 |
| 5 | 工具：`tools[].functionDeclarations[]`；`parts[].functionCall` 发起（无 id → 适配器按序生成 `call_N`）；`args` 兼容对象/字符串两种入参并归一为 JSON 对象串 | 形态归一 + 容错 | 与参考实现同口径；单测覆盖无参工具、无 id 工具调用与字符串 args |
| 6 | 采样：`generationConfig.maxOutputTokens`（>0 才下发，接线缺省 4096）/ `temperature`（缺省 0.3，BE-9 表）；`IsReasoningModel` **恒 false**（Gemini 请求侧无"推理模型拒绝采样参数"约束，与参考实现同口径）；参考实现独有默认值（topP 等）不下发 | 等价口径（与 chat 适配器同表） | 见 §4.4；单测锁定字段集合、不多发 |
| 7 | 流式：`:streamGenerateContent?alt=sse` 逐帧解析；`finishReason` 原生值透传（`STOP` / `MAX_TOKENS` / `SAFETY`…）；`error` 帧与 `promptFeedback.blockReason` → `*ProtocolError{StatusCode:0}`（**带内错误，一律不重试**）；`[DONE]` 与 keep-alive 注释行忽略 | 事件驱动 SSE（与 chat 的 `choices[].delta` 形态不同） | 单测覆盖增量顺序、工具调用与带内失败；HTTP 层 4xx/5xx 仍走 `NewHTTPError`（重试口径不变） |
| 8 | 响应：`candidates[0].content.parts[].text` 按序拼接正文（`thought=true` → 推理链）；多候选/未知字段忽略；非流式与流式共用同一 parts 归一 | 容错口径（对齐参考实现"忽略未知"） | 单测覆盖边缘项；不引入内置工具（如 google_search）语义 |

### 4.4 参数默认值解析（service 层职责）

- **默认参数表**：`maxTokens` 4096（本批次全部变体同值）；`temperature` = 1.0（`anthropic_messages`/`minimax`）/ 0.3（其余，BE-9 口径）。调用方（`ProtocolProviderOptions.Temperature/MaxTokens`）显式值优先，未给时按 `(protocol, variant)` 查表。
- **为什么不放在适配器**：适配器只对"线上格式"负责；部署默认值（超时、温度、端点）属接线层，DB 实例（BE-8）与静态配置两条路径共享同一张表，避免双份默认值漂移。
- **推理模型口径**：`anthropic_messages` 的 `IsReasoningModel` 仅命中模型名含 `thinking` / `reasoning` 标记者（命中时不下发 `temperature`，符合 thinking 与 temperature 互斥）；普通 `claude-*` / `MiniMax-*` 不抑制，保证与旧分支逐字段一致。扩展思考参数（`thinking.budget_tokens`）属 PA-5 能力位范围。

### 4.5 能力位与置灰收敛（PA-5）

`service/llm_protocol_slot.go` 按"承载实现事实"声明能力（PA-4 收官后：4 值协议全部 `Implemented=true`、变体白名单就位；`openai_responses` 与 `google_gemini` 唯一承载为适配器，能力位三项全真；`azure` / `ollama` / anthropic 两变体的 `supportsStream/supportsTools/supportsReasoning` 仍按旧分支事实留空，待 PA-5 收敛）。PA-1..PA-4 落地后适配路径已具备流式/工具/推理能力，PA-5 需：

1. 能力位改为按"承载实现 + 部署开关"计算（开关关闭时保持旧分支事实值，避免管理 API 与前端提前放量）；
2. 前端（FE-2/FE-4 已交付）把对应协议/变体的置灰项打开；
3. `docs/api-reference.md` 中能力位字段的**取值**随实现更新（字段与形状不变，非破坏性）。

## 5. 任务拆解与排期

| ID | 任务 | 交付物（文件级） | 依赖 | 验收 | 状态 |
|:---|:---|:---|:---|:---|:---|
| **PA-1** | `anthropic_messages` 适配器（官方 + minimax 变体）与承载切换 | `internal/llm/protocol/anthropic_messages.go`、`registry.go`（注册 + `DefaultEndpoint`）、`anthropic_messages_test.go`、`service/llm_registry.go`（变体默认值 + 端点回退 + 构建分派泛化为注册表优先，anthropic 两变体改由适配器承载）、`service/llm_anthropic_equivalence_test.go` | BE-9 | ① 单测全绿；② 对照测试：同桩服务下请求路径/鉴权头/请求体字段/返回文本与旧 `MiniMaxProvider` 一致；③ 开关关闭路径零 diff（既有 QA-3 回归复跑） | ✅ 2026-09-25 |
| **PA-2** | `openai_chat_completions` 变体承载（v1.1 收敛：`azure` / `ollama` 归入同一适配器，不新增变体适配器） | `internal/llm/protocol/registry.go`（`AdapterSpec{Variants, New}` 一协议一实现 + ollama 默认端点登记）、`service/llm_registry.go`（`normalizeVariantEndpoint` + `llmProviderModelForVariant`）、`internal/llm/protocol/openai_chat_test.go`、`service/llm_openai_variant_equivalence_test.go`、`service/llm_registry_test.go` | BE-9 / PA-1 | ① 单测全绿；② azure 与旧 `AzureProvider` 对照测试等价（请求路径 / 鉴权头 / 请求体字段 / 返回文本）；③ ollama 归一差异逐条登记（§4.3，有意差异、开关关闭可回退） | ✅ 2026-09-25 |
| **PA-3** | `openai_responses` 适配器（含 Q1 载体决策：官方 Responses API） | `internal/llm/protocol/openai_responses.go` + `openai_responses_test.go`；`registry.go`（注册 `ProtocolOpenAIResponses`，变体白名单 `[""]`）；`service/llm_protocol_slot.go`（`implemented=true`、白名单 `[""]`，422 仅剩 `google_gemini`）；受影响断言同步（`internal/llm/protocol/openai_chat_test.go`、`service/llm_protocol_slot_test.go`、`service/llm_registry_test.go`、`handlers/ai/llm_provider_admin_test.go`） | PA-1（注册表与承载切换） | ① 单测全绿；② 事件序列（`response.output_text.delta` / `reasoning_summary_text.delta` / `function_call_arguments.delta` / `response.completed` / `incomplete` / `done`）与工具调用累积归一；③ 带内失败（`response.failed` / `error` 事件）映射 `*ProtocolError` 且不重试；④ 未登记协议 422 断言改用 `google_gemini` | ✅ 2026-09-25 |
| **PA-4** | `google_gemini` 适配器（v1beta `generateContent` / `streamGenerateContent`） | `internal/llm/protocol/google_gemini.go` + `google_gemini_test.go`；`registry.go`（注册 `ProtocolGoogleGemini`、变体白名单 `[""]`、`DefaultEndpoint` 官方地址）；`adapter.go`（可选接口 `ModelPathAdapter`）；`service/llm_registry.go`（请求期动态路径 `APIPathFor`）；`service/llm_protocol_slot.go`（`implemented=true`、白名单 `[""]`）；受影响断言同步（`service/llm_registry_test.go` 等） | PA-3 | ① 单测全绿；② `parts` / `functionCall` / `finishReason` 归一与流式 SSE 增量；③ 鉴权 `x-goog-api-key`（决策：不复刻 `?key=`，§4.3 PA-4 第 1 条）；④ 带内失败（`error` 帧 / `promptFeedback.blockReason`）映射 `*ProtocolError` 且不重试 | ✅ 2026-09-25 |
| PA-5 | 能力位真实值收敛 + 前端置灰打开 + `available` 口径 | `service/llm_protocol_slot.go` + 单测；前端选项映射 | PA-1..PA-4（均已交付） | 能力位与承载实现一致；管理 API 与前端展示同步 | 待排期 |
| PA-6 | 每协议 parser 规格文档 + 文档同步 | `docs/plan/protocol-specs/*.md`、`CHANGELOG.md`、`docs/api-reference.md`（如涉及）、主计划回填 | 各适配器 | `make docs-gate` 通过（5 gates / 0 failed） | 随批次 |

**排期建议**：PA-1..PA-4 已完成（协议包 **4/4** 适配，任务表中已无阻塞项）；PA-5 可即刻启动——4 协议承载实现均已具备流式/工具/推理能力，仅剩能力位取值与前端展示收敛；PA-6（parser 规格文档 + 主计划/CHANGELOG 回填）随批次执行。

## 6. 验收与证据

| 维度 | 命令 / 证据 | 期望 |
|:---|:---|:---|
| 协议包单测 | `go test ./internal/llm/protocol/... -count=1` | 全绿（含 PA-1..PA-4 新增请求/响应/流式/错误/注册表/变体用例） |
| 等价性对照 | `go test ./service/ -run "Anthropic|Azure|Ollama" -count=1` | anthropic/minimax 与旧 `MiniMaxProvider`、azure 与旧 `AzureProvider` 的请求路径/字段/返回文本一致；ollama 归一差异按 §4.3 登记 |
| `openai_responses` 事件序列 | `go test ./internal/llm/protocol/ -run Responses -count=1` | 文本 / 推理 / 工具参数增量与 `completed` / `incomplete` / `done` / `failed` / `error` 全绿；无旧分支对照，口径证据为 §4.3 PA-3 表 |
| `google_gemini` 归一 | `go test ./internal/llm/protocol/ -run Gemini -count=1` / `go test ./service/ -run "Gemini|Registry" -count=1` | `parts` / `functionCall` / `finishReason` 归一与流式增量全绿；鉴权取 `x-goog-api-key`（口径证据为 §4.3 PA-4 表）；DB 实例经 Gemini 适配器承载 |
| 协议槽位与 422 收敛 | `go test ./service/ -run "ProtocolSlot|Registry|ResolveProtocolURL" -count=1` / `go test ./handlers/ai/ -run Provider -count=1` | 4 值协议全部 `implemented=true`、白名单就位（422 仅剩枚举外协议 / 白名单外变体，如 `openai_completions` / `unknown-variant`）；能力位真实值随 PA-5 |
| 构建与静态检查 | `go build ./...`、`go vet ./...`、`gofmt -l` | 无输出 / exit 0 |
| 零破坏回归 | QA-3 报告 `docs/testing/multi-llm-provider-qa3-regression-2026-09-24.md`；开关关闭路径不经过协议包 | 开关关闭时行为逐字节一致 |
| 文档门禁 | `bash scripts/docs-gate/run-all.sh`（Git Bash） | 5 gates / 0 failed |
| 契约 | `docs/api-reference.md` 不变（能力位仅取值收敛，字段/形状不变） | 非破坏性 |

## 7. 风险与回滚

| # | 风险 | 缓解 |
|:---|:---|:---|
| R-1 | 适配路径与旧分支产生线上行为漂移（变体字段名、默认温度、端点） | 差异清单（§4.3）+ 同桩服务对照测试；开关默认关，可随时回退 |
| R-2 | 双线冲突（主计划与独立计划同时改 `NewProviderFromConfig` / 协议包） | 启动门禁已明确解冻时点；PA 任务串行提交、单文件聚焦 |
| R-3 | 能力位提前放量导致前端暴露未适配组合 | PA-5 单列，且必须在其后于 PA-2..PA-4 完成 |
| R-4 | MiniMax 兼容端点的 camelCase 口径与官方文档不一致 | 以**既有线上实现**（`MiniMaxProvider`）为准绳，对照测试锁定 |

**回滚**：关闭 `LLM_PROTOCOL_ADAPTER_ENABLED` 即回到既有 4 分支（零迁移、零数据变更）；协议包与接线层新增代码在开关关闭时不被执行。

## 8. 待决问题（v1.0 状态更新）

| # | 问题 | 结论 / 备选 |
|:---|:---|:---|
| Q1 | `openai_responses` 的载体与鉴权（官方 `api.openai.com/v1/responses` vs ChatGPT backend 形态） | **已定（PA-3）：官方 Responses API**——`POST /v1/responses` + `Authorization: Bearer`；缺省 endpoint 不单独登记，与 `openai_chat_completions` 标准形态同址回退 `https://api.openai.com`（资源路径由适配器给出）。ChatGPT backend 形态**不复刻**（依赖 OAuth/session 凭证与专有头部，不适配 itsm「endpoint + api_key」实例模型）；如后续需要，按新 `variant` 构造期选项引入、不改协议枚举（§4.3 PA-3 第 1 条） |
| Q2 | Ollama 原生 `/api/chat` 是否归一到 `openai_chat_completions` | **已定（PA-2 收敛）：归一**——不新增 Ollama 原生适配器，`ollama` 变体由 `openai_chat_completions` 适配器以 OpenAI 兼容 `/v1/chat/completions` 承载；旧原生分支保留给开关关闭与回退（差异见 §4.3） |
| Q3 | 适配层与 `NewProviderFromConfig` 的收敛策略（替换 / 并存） | **已定：并存过渡**——4/4 协议均由适配器承载（`openai_chat_completions` 含 azure / ollama、`anthropic_messages` 含 minimax、`openai_responses`、`google_gemini`，PA-1..PA-4）；旧分支（`OpenAIProvider` / `AzureProvider` / `LocalProvider` / `MiniMaxProvider`）保留给开关关闭的静态配置路径与回退；`openai_responses` / `google_gemini` 无等价旧分支，注册表未命中时显式失败（422 / UNAVAILABLE），不静默退化 |
| Q4 | 能力探测：静态声明 + 运行时记忆缓存 | P1 先静态声明（PA-5），后续按调用结果记忆（按 `(protocol, variant, model)` 维度） |

## 9. 修订记录

| 版本 | 日期 | 说明 |
|:---|:---|:---|
| v0.1 | 2026-09-24 | 槽位占位创建（主计划 v1.2 §11 交接）：范围、参考实现坐标、边界与待决问题；详细设计待启动 |
| v0.2 | 2026-09-24 | 按参考实现一手核验补充 §3 详证（providercompat profile 层与协议正交、能力/推理档位、协议规格文档坐标）、§4 交付物（parser 规格文档）与 Q4 备选 |
| v0.3 | 2026-09-24 | 启动门禁对齐主计划 v1.3 §5.4（B5 出口 + 双线冲突约束）：更新 §0 启动条件、§5 边界新增启动门禁条目 |
| v0.4 | 2026-09-24 | 首适配器基线对齐主计划 v1.4 BE-9：P0 交付接口/注册表/`openai_chat_completions` 适配器；本计划范围收敛为"其余 3 协议 + `variant` 语义收敛"（§0/§1/§2/§4/§5 同步） |
| v0.5 | 2026-09-24 | 措辞校准：首适配器交付时点统一为"P0 交付、随主计划 B5 合入"，消除"已交付"误读（§0/§1/§2/§4 同步） |
| v0.6 | 2026-09-24 | BE-9 首适配器协议包先行落地（`itsm-backend/internal/llm/protocol` + 对照测试全绿）；接线与开关仍随主计划 B5，本计划启动门禁不变 |
| v0.7 | 2026-09-25 | 启动门禁达成注记：主计划 B5 出口全部满足（BE-8/BE-9/BE-5 落地、B6 前端完成、QA-3 全链路回归通过、Postgres 迁移实测与 prod 账本阻塞解除，主计划 v1.11–v1.14）；本文件待排期升 v1.0，期间冻结 `NewProviderFromConfig` 与协议包改动 |
| v1.0 | 2026-09-25 | **正式方案定稿 + PA-1 交付**：① §4 详细设计（契约与不变式、变体语义表、PA-1 差异清单、参数默认值解析、能力位收敛方案）；② §5 PA-1..PA-6 任务拆解与排期；③ §6 验收证据表、§7 风险与回滚、§8 待决问题结论化；④ PA-1 落地：`anthropic_messages` 默认/minimax 变体适配器 + 注册 + 变体级默认参数与端点回退 + 对照测试（见 §5 行 PA-1）；⑤ 承载切换泛化：DB 实例路径构建分派由「openai 默认变体硬编码」改为「协议包注册表优先、未注册回退旧分支」，`TestLLMProviderRegistryProtocolVariantMapping` 同步断言适配器类型与工具能力位 |
| v1.1 | 2026-09-25 | **适配器维度收敛「一协议一实现」+ PA-2 交付**：① 口径收敛（用户决策，§0/§2/§4.3）：适配器 = 协议本身（与 4 值协议枚举一一对应），`variant` 降为构造期选项、不进注册表键；原 PA-2「azure / ollama 变体适配器化」随之收敛为「同一适配器承载」，不新增 `openai_chat_variants.go` 等变体文件；② 协议包：`internal/llm/protocol/registry.go` 注册表键由 `(protocol, variant)` 改为每协议一个 `AdapterSpec{Variants, New}`（`Supports` / `NewAdapter` 按「协议 spec + 变体白名单」判定，构造期固化变体），默认注册表登记 `openai_chat_completions{"", azure, ollama}` 与 `anthropic_messages{"", minimax}`，`DefaultEndpoint` 增补 ollama 缺省地址 `http://localhost:11434`（与旧 `LocalProvider` 同值）；③ 接线层：`service/llm_registry.go` 新增 `normalizeVariantEndpoint`（azure 仅主机名 endpoint 补 `/openai`，与旧 `AzureProvider` 同址）与 `llmProviderModelForVariant`（azure `model` 缺省回退 `deployment`），静态配置与 DB 实例两条路径共用；④ §4.3 新增 PA-2 差异清单 6 条（azure 等价四项；ollama 协议归一等有意差异两项）；⑤ 测试：`internal/llm/protocol/openai_chat_test.go`（注册表 / `Supports` / 名称归一化 / ollama 端点）、`service/llm_openai_variant_equivalence_test.go`（azure 对照旧 `AzureProvider` 等价 / model 回退 / endpoint 归一 / ollama OpenAI 兼容端点）、`service/llm_registry_test.go`（未登记变体 422 用例由 azure 改为 `unknown-variant`）；⑥ 零破坏：开关默认关；ollama 归一差异登记在案，关闭开关即回退旧分支（Q2 结论化「归一」） |
| v1.2 | 2026-09-25 | **PA-3 交付：`openai_responses` 适配器 + 槽位放量**：① 协议包新增 `internal/llm/protocol/openai_responses.go`（官方 Responses API 载体：`/v1/responses` + `Authorization: Bearer`；前导 system/developer 合并为顶层 `instructions`、`input` 项数组（message / function_call / function_call_output）、`store:false` 恒下发、`max_output_tokens` 与推理模型抑制 `temperature`、tools 扁平结构与 `tool_choice` 归一；事件驱动 SSE 覆盖文本 / `reasoning_summary_text` / 工具参数增量与 `completed` / `incomplete` / `done`；`response.failed` 与 `error` 事件映射 `*ProtocolError` 不重试）；② `registry.go` 登记 `openai_responses{"", }`；`service/llm_protocol_slot.go` 槽位 `implemented=true`、变体白名单 `[""]`（422 仅剩 `google_gemini`）；`service/llm_registry.go` 构建路径经注册表承载（endpoint 为空 → 回退 OpenAI 兼容默认地址 + `/v1/responses`）；③ §4.3 新增 PA-3 差异/决策 9 条（无旧分支对照：载体决策 / 无状态口径 / instructions 归一 / 工具项形态 / 扁平 tools / 采样参数 / 事件序列 / 失败语义 / 未知项容错）；§8 Q1 结论化；④ 测试：新增 `internal/llm/protocol/openai_responses_test.go`（请求构建 / 工具与 `tool_choice` / 推理模型 / 非流式 / 流式事件与快照恢复 / 失败与坏帧 / 边缘项），同步受影响断言（`openai_chat_test.go`、`service/llm_protocol_slot_test.go`、`service/llm_registry_test.go`、`handlers/ai/llm_provider_admin_test.go`——未实现协议 422 断言改用 `google_gemini`）；⑤ 零破坏：无 DB / API 契约改动；`LLM_PROTOCOL_ADAPTER_ENABLED` 默认关，关闭时构建路径仍不经过协议包；⑥ 证据：`go test ./internal/llm/protocol/... -count=1`、service / handlers 定向集全绿，`go build ./...` / `go vet` exit 0，改动文件 `gofmt -l` 无输出，文档门禁 `bash scripts/docs-gate/run-all.sh` → 5 gates / 0 failed |
| v1.3 | 2026-09-25 | **PA-4 交付：`google_gemini` 适配器 + 4/4 协议适配器化收官**：① 协议包新增 `internal/llm/protocol/google_gemini.go`（Gemini API v1beta 载体：`/v1beta/models/{model}:generateContent` 与 `:streamGenerateContent?alt=sse`；`x-goog-api-key` 鉴权、**不复刻** `?key=`；前导 system → 顶层 `systemInstruction`、`contents[]` 仅 `user`/`model`、工具 `functionDeclarations` / `functionResponse`（函数名按历史反查、无 id → `call_N`）、`generationConfig.maxOutputTokens`（>0 才发）与 `temperature`（`IsReasoningModel` 恒 false）、`thought=true` parts → 推理链、`finishReason` 原生值透传、帧内 `error` / `promptFeedback.blockReason` → `*ProtocolError` 不重试）；② 新增可选接口 `protocol.ModelPathAdapter`（`APIPathFor(model, stream)`），接线层 `service.protocolProvider` 请求期优先取动态路径、回退静态 `GetAPIPath()`；`registry.go` 登记 `google_gemini{"", }` + `DefaultEndpoint` 官方地址 `https://generativelanguage.googleapis.com`；`service/llm_protocol_slot.go` 槽位 `implemented=true`、白名单 `[""]`（4 值协议全部实现，422 仅剩枚举外协议 / 白名单外变体）；③ §4.3 新增 PA-4 差异/决策 8 条（无旧分支对照：载体与鉴权选择 / 动态资源路径 / 官方缺省端点 / 会话与工具形态归一 / 采样口径 / 事件驱动 SSE / 容错）；④ 测试：新增 `internal/llm/protocol/google_gemini_test.go`（元数据 / 请求构建 / 边缘 / 工具配置 / 非流式 / 流式 / 带内错误 7 用例），同步 `service/llm_registry_test.go` gemini 期望（UNAVAILABLE → 适配器承载）；⑤ 证据：`go test ./internal/llm/protocol/... -count=1` ok、service 定向集与 `handlers/ai` 全绿、`go build ./...` / `go vet` exit 0、改动文件 `gofmt -l` 无输出、文档门禁 `bash scripts/docs-gate/run-all.sh` → 5 gates / 0 failed |
