# LLM 协议适配层方案（4 种 API 形态归一化）

> 文档类型：技术方案 + 实施计划（v1.0 定稿：启动门禁已达成，PA-1 `anthropic_messages` 已落地）
> 适用范围：`itsm-backend`（Go）、`itsm-frontend`
> 编制日期：2026-09-24（v1.0：2026-09-25）
> 版本：v1.0（详细设计 + PA 任务拆解 + 变体语义表 + 差异清单 + 验收证据；PA-1 交付见 §5/§6）
> 目标读者：后端、前端、测试
> 关联文档：`docs/plan/multi-llm-provider-plan.md`（主计划；槽位定义见其 §2.2 D13、§3.1.4、§11，排期与启动门禁见其 §5.4）、`docs/documentation-governance.md`
> 外部参考实现（只读参考，不引入依赖）：`E:\projects\ai-agent-runtime\backend\internal\llm\adapter\`

---

## 0. 状态与目的

- 本文件在 v0.1–v0.7 期间是**槽位占位（Stub）**，自 **v1.0 起为正式方案**：主计划《多 LLM Provider 支持与可切换方案》预留的 5 类槽位（枚举 / `variant` + `adapter_options` / 协议层 / 能力位 / 错误码）在此逐项落地。
- **P0 基线（主计划 BE-9，已交付）**：`itsm-backend/internal/llm/protocol`（`ProtocolAdapter` 接口 + `Registry` + `openai_chat_completions` 适配器 + 对照测试 `equivalence_test.go`）与 `service/llm_registry.go` 构建分派（`LLM_PROTOCOL_ADAPTER_ENABLED` 默认关，关闭时构建路径完全不经过协议包）。
- **前置件（主计划 BE-8，已交付）**：`service/llm_protocol_slot.go`（4 值协议枚举常量、`(protocol, variant) → 既有分支` 映射、variant 白名单、能力位、`adapter_options` 校验与哨兵错误）+ 单测；本计划直接复用，不重复定义协议常量与校验逻辑。
- **启动门禁（主计划 §5.4，已达成）**：B5 出口各项已满足——BE-8 随 B2、BE-9/BE-5 随 B5 落地；B6 前端批次完成（主计划 v1.13）；QA-3 开关关闭全链路回归 13/13 维度通过（`docs/testing/multi-llm-provider-qa3-regression-2026-09-24.md`，主计划 v1.14）；Postgres 空库/存量库 up+down 全绿、prod 账本阻塞解除（主计划 v1.11/v1.12）。**2026-09-25 起本计划解冻**，`NewProviderFromConfig`、既有 4 个实现分支与 `internal/llm/protocol/` 的改动按本文件 PA 任务执行。
- **不变量（全程约束）**：不改主计划已定的 DB 结构（零迁移）、不改主计划 §3.4 API 契约、开关关闭时行为与单 provider **逐字节一致**、前端仅把"待接入"置灰选项打开。

## 1. 目标与非目标

对 4 种 API 形态提供统一适配层：

| 协议（枚举值） | 形态 | 参考实现坐标 |
|:---|:---|:---|
| `openai_chat_completions` | OpenAI Chat Completions（**P0 已交付：BE-9**） | `adapter/openai.go:1111`（`/v1/chat/completions`） |
| `openai_responses` | OpenAI Responses（参考实现以 `codex` 命名承载） | `adapter/codex.go:876`（`/v1/responses`） |
| `anthropic_messages` | Anthropic Messages（**PA-1 已交付**） | `adapter/anthropic.go:795`（`/v1/messages`） |
| `google_gemini` | Google Gemini | `adapter/gemini.go:591-595`（v1beta，路径由调用侧拼装） |

归一化范围：请求构建、响应解析、流式事件、工具调用（含流式累积）、推理链（thinking/reasoning）、错误映射、能力声明与探测。

**非目标（本计划不做）**：不新增 DB 字段与迁移；不改 `docs/api-reference.md` 的既有契约（仅数值型能力位随实现收敛）；不引入参考实现的 `providercompat` 兼容管道（vendor profile 层）——itsm 侧以 `variant` 表达同协议下的厂商差异，复杂度按需引入。

## 2. 主计划承诺交付的槽位（本计划的输入）

| # | 槽位 | 主计划位置 | 本计划要做的事 | 状态 |
|:---|:---|:---|:---|:---|
| 1 | 协议枚举 4 值 | D3、§3.1.1、§3.1.4 | 用真实适配器替换"映射到既有分支" | `openai_chat_completions` ✅ BE-9；`anthropic_messages` ✅ PA-1；其余 2 协议待 PA-3/PA-4 |
| 2 | `variant` + `adapter_options` | §3.1.1、§3.5 | 落地语义：Azure `api_version`、Ollama 原生形态、Anthropic 兼容端点（minimax camelCase） | minimax ✅ PA-1；azure/ollama 待 PA-2 |
| 3 | 协议层：接口 + 注册表 + 首适配器 | §3.2、§11.2 | 复用 BE-9 基线，新增其余适配器 | 3/4 适配器（协议枚举维度） |
| 4 | 能力位 | §3.4 `available`、§3.7 | `supportsStream` / `supportsTools` / `supportsReasoning` 的真实值收敛 | 待 PA-5 |
| 5 | 错误码 `AI_PROTOCOL_NOT_IMPLEMENTED` | §3.4 | 随适配器逐个就绪而收敛（枚举值从"置灰"变为"可用"） | 随 PA-3/PA-4 |

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

- **适配器实例即 (protocol, variant)**：`Registry.Register(protocol, variant, adapter)` 静态注册（启动期一次、运行期只读，读写加锁）。厂商/部署差异**在构造期固化进实例**（如 `NewAnthropicMessagesAdapter(variant)` 决定 `max_tokens` vs `maxTokens`），请求期不查表、不分支；适配器无状态、可被多 goroutine 并发复用。
- **纯函数边界**：`BuildRequest` / `BuildHeaders` 不触网、不读全局配置；`HandleResponse` 只消费 `io.Reader` + 回调，不持有 ctx/HC——HTTP 由 `service.protocolProvider` 承担（`service/llm_registry.go`）。
- **不 import service 包**：协议包内 `Message` / `Tool` / `ToolCall` 是 `service.LLMMessage` 的最小镜像，接线层（`toProtocolMessages` / `toProtocolTools` / `toLLMToolCalls`）负责转换，避免循环依赖。
- **开关语义**：`LLM_PROTOCOL_ADAPTER_ENABLED`（env 优先，其次 `config.yaml llm.protocol_adapter_enabled`，默认 false）。开关关闭时构建路径**完全不经过协议包**（QA-3 已归档零 diff 证据）；开启时 `(protocol, variant)` 未注册 → 回退旧分支（静态配置路径）或 `ErrProviderUnavailable`/`AI_PROTOCOL_NOT_IMPLEMENTED`(422)（DB 实例路径）。
- **错误双身份**：适配器 HTTP 错误经 `protocol.NewHTTPError` 映射 `*protocol.ProtocolError`，service 侧包装为 `*openai.APIError` + `*protocol.ProtocolError` 双 `Unwrap`，网关 `isTransientLLMError` 的 429/5xx 重试口径不变。
- **带内错误事件**（无 HTTP 语义，`StatusCode=0`）一律不重试：Anthropic `error` 事件、OpenAI `[DONE]` 之前的错误体。

### 4.2 变体语义表（协议 × 变体 → 承载与线上口径）

| 协议 | 变体 | 承载 | 缺省 endpoint（endpoint 为空时） | 关键字段口径 | 鉴权头 | 状态 |
|:---|:---|:---|:---|:---|:---|:---|
| `openai_chat_completions` | `""` | 适配器（BE-9） | `https://api.openai.com` | `max_tokens` / `temperature` / `tools[].function` | `Authorization: Bearer` | ✅ |
| `openai_chat_completions` | `azure` | 旧 `AzureProvider` | —（必须显式配置） | Azure `api-version` 查询参数 + deployment 路径 | `api-key` | 待 PA-2 |
| `openai_chat_completions` | `ollama` | 旧 `LocalProvider` | —（本机地址） | 原生 `/api/chat` 或 OpenAI 兼容 `/v1` | 无 | 待 PA-2 |
| `anthropic_messages` | `""` | 适配器（PA-1） | `https://api.anthropic.com` | `max_tokens` / `stop_reason` / `system` 顶层 / `tools[].input_schema` / 工具结果走 `user` + `tool_result` | `x-api-key` + `anthropic-version: 2023-06-01` | ✅ |
| `anthropic_messages` | `minimax` | 适配器（PA-1） | `https://api.minimaxi.com/anthropic/v1` | 同上，字段名 camelCase：`maxTokens` / `stopReason`；`temperature` 默认 **1.0**（旧分支取值） | 同上 | ✅ |
| `openai_responses` | `""` | 待 PA-3 | 待定（Q1） | Responses 事件模型（`response.output_text.delta` 等） | `Authorization: Bearer` | 待排期 |
| `google_gemini` | `""` | 待 PA-4 | 待定（v1beta） | `generateContent` / `streamGenerateContent`、`functionCall`、`parts` | `x-goog-api-key`（或 `?key=`） | 待排期 |

**端点解析规则**（`service.resolveProtocolURL`，与既有 config.yaml 写法对齐）：endpoint 为空 → 取 `protocol.DefaultEndpoint(protocol, variant)`（协议包登记值），未登记组合回退 `https://api.openai.com`（BE-9 行为不变）；endpoint 已带版本段（`/v1`、`/v1beta`）→ 只补资源段（`/v1/messages` → `/messages`）；已含完整路径 → 原样使用。

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

### 4.4 参数默认值解析（service 层职责）

- **默认参数表**：`maxTokens` 4096（两变体同值）；`temperature` = 1.0（minimax）/ 0.3（其余，BE-9 口径）。调用方（`ProtocolProviderOptions.Temperature/MaxTokens`）显式值优先，未给时按 `(protocol, variant)` 查表。
- **为什么不放在适配器**：适配器只对"线上格式"负责；部署默认值（超时、温度、端点）属接线层，DB 实例（BE-8）与静态配置两条路径共享同一张表，避免双份默认值漂移。
- **推理模型口径**：`anthropic_messages` 的 `IsReasoningModel` 仅命中模型名含 `thinking` / `reasoning` 标记者（命中时不下发 `temperature`，符合 thinking 与 temperature 互斥）；普通 `claude-*` / `MiniMax-*` 不抑制，保证与旧分支逐字段一致。扩展思考参数（`thinking.budget_tokens`）属 PA-5 能力位范围。

### 4.5 能力位与置灰收敛（PA-5）

`service/llm_protocol_slot.go` 当前按"旧分支事实"声明能力（anthropic 变体 `Implemented=true` 但 `supportsStream/supportsTools=false`；`openai_responses`/`google_gemini` 为未实现槽位）。PA-1 落地后适配路径已具备流式/工具能力，PA-5 需：

1. 能力位改为按"承载实现 + 部署开关"计算（开关关闭时保持旧分支事实值，避免管理 API 与前端提前放量）；
2. 前端（FE-2/FE-4 已交付）把对应协议/变体的置灰项打开；
3. `docs/api-reference.md` 中能力位字段的**取值**随实现更新（字段与形状不变，非破坏性）。

## 5. 任务拆解与排期

| ID | 任务 | 交付物（文件级） | 依赖 | 验收 | 状态 |
|:---|:---|:---|:---|:---|:---|
| **PA-1** | `anthropic_messages` 适配器（官方 + minimax 变体）与承载切换 | `internal/llm/protocol/anthropic_messages.go`、`registry.go`（注册 + `DefaultEndpoint`）、`anthropic_messages_test.go`、`service/llm_registry.go`（变体默认值 + 端点回退 + 构建分派泛化为注册表优先，anthropic 两变体改由适配器承载）、`service/llm_anthropic_equivalence_test.go` | BE-9 | ① 单测全绿；② 对照测试：同桩服务下请求路径/鉴权头/请求体字段/返回文本与旧 `MiniMaxProvider` 一致；③ 开关关闭路径零 diff（既有 QA-3 回归复跑） | ✅ 2026-09-25 |
| PA-2 | `azure` / `ollama` 变体适配器化（variant 语义：Azure `api-version` + deployment 路径、Ollama 原生 `/api/chat` 与 `/v1` 归一） | `internal/llm/protocol/openai_chat_variants.go` + 测试；`service/llm_registry.go` 变体表 | PA-1 | 与 `AzureProvider` / `LocalProvider` 对照测试等价；`adapter_options` 校验复用 BE-8 | 待排期 |
| PA-3 | `openai_responses` 适配器（含 Q1 载体决策） | `internal/llm/protocol/openai_responses.go` + 测试；`llm_protocol_slot.go` 置灰打开 | PA-2 | 事件序列（`response.created` / `response.output_text.delta` / `response.completed`）与工具调用归一 | 待排期 |
| PA-4 | `google_gemini` 适配器（v1beta、`generateContent` / `streamGenerateContent`） | `internal/llm/protocol/google_gemini.go` + 测试；`llm_protocol_slot.go` 置灰打开 | PA-3 | `parts` / `functionCall` / `finishReason` 归一；`?key=` 与 `x-goog-api-key` 两种鉴权 | 待排期 |
| PA-5 | 能力位真实值收敛 + 前端置灰打开 + `available` 口径 | `service/llm_protocol_slot.go` + 单测；前端选项映射 | PA-2..PA-4 | 能力位与承载实现一致；管理 API 与前端展示同步 | 待排期 |
| PA-6 | 每协议 parser 规格文档 + 文档同步 | `docs/plan/protocol-specs/*.md`、`CHANGELOG.md`、`docs/api-reference.md`（如涉及）、主计划回填 | 各适配器 | `make docs-gate` 通过（5 gates / 0 failed） | 随批次 |

**排期建议**：PA-1 已完成；PA-2 与 PA-3 可并行（不同文件、无共享接口变更），PA-4 依赖 PA-3 的 Responses 事件模型经验（事件驱动 SSE 的容错口径可复用）；PA-5 必须在其后（能力位一旦提前放量，前端会暴露尚未适配的组合）。

## 6. 验收与证据

| 维度 | 命令 / 证据 | 期望 |
|:---|:---|:---|
| 协议包单测 | `go test ./internal/llm/protocol/... -count=1` | 全绿（含 PA-1 新增请求/响应/流式/错误/变体用例） |
| 等价性对照 | `go test ./service/ -run "Anthropic" -count=1` | 适配路径与旧 `MiniMaxProvider` 请求字段 / 返回文本一致 |
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
| Q1 | `openai_responses` 的载体与鉴权（官方 `api.openai.com/v1/responses` vs ChatGPT backend 形态） | 待 PA-3 启动时定；参考实现 `codex.go` 同时覆盖两种 endpoint 兼容逻辑 |
| Q2 | Ollama 原生 `/api/chat` 是否归一到 `openai_chat_completions` | **倾向归一**（P0 现状行为不变的可演进路径），PA-2 决策时确认 |
| Q3 | 适配层与 `NewProviderFromConfig` 的收敛策略（替换 / 并存） | **已定：并存过渡**——PA-1 只切换 `anthropic_messages` 的 minimax 承载，旧分支保留给开关关闭与未适配变体 |
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
