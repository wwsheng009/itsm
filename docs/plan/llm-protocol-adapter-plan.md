# LLM 协议适配层方案（4 种 API 形态归一化）

> 文档类型：技术方案 + 实施计划（Stub / v0.7：其余 3 协议预留，启动门禁已达成、待排期）
> 适用范围：`itsm-backend`（Go）、`itsm-frontend`
> 编制日期：2026-09-24
> 版本：v0.7（占位：其余 3 协议范围与边界、参考实现详证、启动门禁对齐主计划 §5.4、首适配器基线对齐主计划 BE-9；主计划 B5 出口各项已达成、启动门禁已满足（见 §0 状态注记），待排期升 v1.0 并补全详细设计）
> 目标读者：后端、前端、测试
> 关联文档：`docs/plan/multi-llm-provider-plan.md`（主计划；本计划的槽位定义见其 §2.2 D13、§3.1.4、§11，排期与启动门禁见其 §5.4）、`docs/documentation-governance.md`
> 外部参考实现（只读参考，不引入依赖）：`E:\projects\ai-agent-runtime\backend\internal\llm\adapter\`

---

## 0. 状态与目的

- 本文件是**槽位占位（Stub）**：主计划《多 LLM Provider 支持与可切换方案》v1.6（槽位自 v1.2 起）已预留 5 类槽位（枚举 / `variant` + `adapter_options` / 协议层 / 能力位 / 错误码），并**已将首个适配器纳入 P0 交付范围**（BE-9：`ProtocolAdapter` 接口 + 注册表 + `openai_chat_completions` 适配器）；其中**协议包与接线均已落地工作树**——`itsm-backend/internal/llm/protocol`（桩服务对照测试 `go test ./internal/llm/protocol/... -count=1` 全绿）+ `service/llm_registry.go` 构建分派与 `LLM_PROTOCOL_ADAPTER_ENABLED` 开关（默认关，`service/llm_registry_test.go` 对照测试证明与既有 `OpenAIProvider` 等价）；其余 3 协议的正式设计、实施与验收在本文件推进。
- **已就绪可复用的前置件（v0.6 注记）**：主计划 BE-8 槽位校验已落地 `itsm-backend/service/llm_protocol_slot.go`（4 值枚举常量与 `(protocol, variant) → 既有分支` 映射、variant 白名单、能力位、`adapter_options` 校验与哨兵错误；单测 `service/llm_protocol_slot_test.go`），本计划启动时直接复用，不重复定义协议常量与校验逻辑。
- **启动条件（门禁，主计划 §5.4）**：主计划 **B5 出口**达成（P0 全链路可开箱即用、开关默认关、BE-8 槽位随 B2、BE-9 首适配器随 B5 合入）后启动；启动时把本文件升为 v1.0 完整方案。**2026-09-25 状态注记（门禁已达成）**：主计划 B5 出口各项已全部满足——BE-8 随 B2、BE-9 与 BE-5 随 B5 落地工作树；B6 前端批次完成（主计划 v1.13）；QA-3 开关关闭全链路回归 13/13 维度通过（证据 `docs/testing/multi-llm-provider-qa3-regression-2026-09-24.md`，主计划 v1.14）；Postgres 空库/存量库 up+down 全绿且 prod 账本阻塞已解除（主计划 v1.11/v1.12）。本文件自此具备升 v1.0 条件，启动时点由排期决定；升版前冻结 `NewProviderFromConfig`、既有 4 个实现分支与 `internal/llm/protocol/` 的改动。
- **不变量（启动前置约束）**：不改主计划已定的 DB 结构（零迁移）、不改主计划 §3.4 API 契约、前端仅把"待接入"置灰选项打开。

## 1. 目标（待细化）

对 4 种 API 形态提供统一适配层：

| 协议（枚举值） | 形态 | 参考实现坐标 |
|:---|:---|:---|
| `openai_chat_completions` | OpenAI Chat Completions（**P0 交付：BE-9**） | `adapter/openai.go:1111`（`/v1/chat/completions`） |
| `openai_responses` | OpenAI Responses | `adapter/codex.go:876`（`/v1/responses`，参考实现以 `codex` 命名承载） |
| `anthropic_messages` | Anthropic Messages | `adapter/anthropic.go:795`（`/v1/messages`） |
| `google_gemini` | Google Gemini | `adapter/gemini.go:591-595`（v1beta，路径由调用侧拼装） |

归一化范围：请求构建、响应解析、流式事件、工具调用（含流式累积）、推理链（thinking/reasoning）、错误映射、能力声明与探测。

P0 交付的基线（主计划 BE-9，协议包 + 接线均已落地工作树）：`internal/llm/protocol/` 的接口与注册表 + `openai_chat_completions` 适配器（对照测试锁定与既有实现等价、开关可回退）+ `service/llm_registry.go` 的构建分派；本计划在此基础上补齐其余 3 协议并收敛 `variant` 语义。

## 2. 主计划承诺交付的槽位（本计划的输入）

| # | 槽位 | 主计划位置 | 本计划要做的事 |
|:---|:---|:---|:---|
| 1 | 协议枚举 4 值 | D3、§3.1.1、§3.1.4 | 用真实适配器替换 P0 的"映射到既有分支"（`openai_chat_completions` 已由 BE-9 适配器承载；其余 3 协议与 `azure`/`ollama`/`minimax` 变体仍走旧分支或置灰，由本计划替换） |
| 2 | `variant` + `adapter_options` | §3.1.1、§3.5 | 落地语义：azure `api_version`、Ollama 原生形态、Anthropic 兼容端点、兼容网关差异 |
| 3 | 协议层：接口 + 注册表 + 首适配器 | §3.2、§11.2 | P0（BE-9）已交付并接线 `openai_chat_completions`（工作树）；本计划复用该基线，新增其余 3 适配器并收敛 `variant` 语义 |
| 4 | 能力位 | §3.4 `available`、§3.7 | `supportsStream` / `supportsTools` / `supportsReasoning` 的真实探测与缓存 |
| 5 | 错误码 `AI_PROTOCOL_NOT_IMPLEMENTED` | §3.4 | 随适配器逐个就绪而收敛（枚举值从"置灰"变为"可用"） |

## 3. 参考实现要点（`E:\projects\ai-agent-runtime`，只读）

- **接口形状**：`ProtocolAdapter`（`backend/internal/llm/adapter/adapter.go:70-114`）：`Name` / `BuildRequest` / `BuildHeaders` / `ExtractResponse` / `ExtractReasoning` / `ExtractStreamContent` / `ExtractStreamReasoning` / `BuildAssistantMessage` / `HandleResponse` / `ProcessResponse` / `IsReasoningModel` / `GetAPIPath`。
- **注册表**：`NewAdapter(providerType)` switch 分派，未知类型报错（对比主计划 P0 的缺陷修复：不允许静默退化）；`GetAdapterOrDefault` 仅供兜底（`adapter/factory.go:9-31`）。
- **统一流式回调**：`StreamCallbacks{OnText, OnReasoning, OnImage}` + `HandleResponse(isStream, respBody, callbacks)`（`adapter/adapter.go:39-114`）；Anthropic 为事件驱动 SSE（`adapter/anthropic.go:509-560`）。
- **厂商差异示例**：Anthropic `x-api-key` + `anthropic-version` 头（`adapter/anthropic.go:204-214`）；thinking 与 temperature 互斥、`max_tokens` 必填（`adapter/anthropic.go:35-95`）。
- **厂商兼容层（与协议适配器正交，参考实现命名 `profile`）**：`providercompat/` 以 `NewChain(Context)` 构建兼容管道；`Context{ProviderName, Protocol, BaseURL, APIPath, Profile, Model, ConfiguredCapabilities, ResponseMarkers}` 把"协议"与"厂商 profile"作为两个独立维度（`providercompat/providercompat.go:10-42`）——与本方案 `protocol` + `variant` 建模一致（`variant` ≈ 参考实现的 `Profile`）。
- **兼容管道职责**：`Adapter` 接口覆盖 OpenAI/Anthropic 两套消息归一、请求体预处理、assistant 消息归一、`ProcessResult`/流式 chunk 归一、reasoning 重放、`SupportsMaxOutputTokens` 等约 14 个挂点（`providercompat/adapter.go:9-26`）；`BaseAdapter` 提供 no-op 默认（各 profile 只覆写关注点）。已注册 profile 示例：`opencode-console-go` / `sensenova` / `nvidia` / `deepseek` / `chatgpt-codex` / `codex-path` / `codex-default` / `openai-default`（`providercompat/registry.go:3-12`）。
- **能力声明与推理档位**：profile 可提供能力默认值（如 DeepSeek：`ReasoningModel=true`、`ReasoningEfforts=[high,max]`，`providercompat/openai_deepseek.go:22-31`）；模型能力解析为精确匹配 + `*` 通配兜底（`internal/llm/model_capability.go:16-36`），能力规格含 `ReasoningModel` / `ReasoningEfforts` / `InputModalities` / `ReasoningEffortBudgets`。
- **协议规格文档（可直接对标产出）**：参考实现为每个协议维护 `adapter/*_parser.md`（`openai_parser.md` / `anthropic_parser.md` / `codex_parser.md` / `gemini_parser.md`）；流式 / 推理 / 工具容错各有专项文件（`adapter/sse.go`、`adapter/thinking.go`、`adapter/malformed_tool_call.go`、`adapter/mcp_meta_tools.go`）。

## 4. 交付物（待启动时细化）

- `internal/llm/protocol/`：**P0（BE-9）交付接口 + 注册表 + `openai_chat_completions` 适配器（已落地工作树，含 `service/llm_registry.go` 接线）**；本计划新增其余 3 适配器（`anthropic_messages` / `openai_responses` / `google_gemini`）并收敛 `variant` 语义。
- `variant` 语义实现与文档（`azure` / `ollama` / `minimax` / 自定义）。
- 能力矩阵 + 探测（含缓存与失效策略）。
- 测试：4 协议桩服务矩阵（连通 / 流式 / 工具调用 / 推理链 / 错误映射）。
- 每协议规格文档（对标参考实现 `adapter/*_parser.md`）：报文示例、流式事件序列、工具调用与推理链差异清单。
- 文档同步：`CHANGELOG.md [Unreleased]`、`docs/api-reference.md`（若契约有变化）、主计划回填。

## 5. 与主计划的边界（交接清单）

- **主计划 → 本计划**：§3.1.4 映射表、§11.2 槽位定义、§9.1 参考坐标、BE-8 交付的枚举/校验/错误码、BE-9 交付的协议层基线（接口/注册表/`openai_chat_completions` 适配器与对照测试）。
- **本计划 → 主计划**：其余 3 适配器逐个就绪后，把对应协议从"置灰/422"改为"可选/可用"（`anthropic_messages` 由旧分支切换为适配器承载）；能力位改为真实探测值；不引入新迁移、不破坏既有 API 契约。
- **启动门禁**：主计划 §5.4 的 **B5 出口**达成方可开工；B1–B5 期间主计划不得并行修改 `NewProviderFromConfig`、既有 4 个实现分支与 `internal/llm/protocol/`（避免双线冲突）。

## 6. 待决问题（启动时确认）

| # | 问题 | 备选 |
|:---|:---|:---|
| Q1 | `openai_responses` 的载体与鉴权（官方 `api.openai.com/v1/responses` vs ChatGPT backend 形态） | 参考实现 `codex.go` 同时覆盖两种 endpoint 兼容逻辑 |
| Q2 | Ollama 原生 `/api/chat` 是否归一到 `openai_chat_completions`（还是独立 variant 语义） | 倾向归一（P0 现状行为不变的可演进路径） |
| Q3 | 适配层与 `NewProviderFromConfig` 的收敛策略（替换 / 并存） | 并存过渡，稳定后再议 |
| Q4 | 能力探测：静态声明 + 运行时记忆缓存（按 `(protocol, variant, model)` 维度） | 参考实现：profile 提供能力默认值（如 `deepseek`）+ 模型级能力表（精确 + `*` 通配，`internal/llm/model_capability.go`）；建议本计划 P1 先静态声明，后续按调用结果记忆 |

## 7. 修订记录

| 版本 | 日期 | 说明 |
|:---|:---|:---|
| v0.1 | 2026-09-24 | 槽位占位创建（主计划 v1.2 §11 交接）：范围、参考实现坐标、边界与待决问题；详细设计待启动 |
| v0.2 | 2026-09-24 | 按参考实现一手核验补充 §3 详证（providercompat profile 层与协议正交、能力/推理档位、协议规格文档坐标）、§4 交付物（parser 规格文档）与 Q4 备选 |
| v0.3 | 2026-09-24 | 启动门禁对齐主计划 v1.3 §5.4（B5 出口 + 双线冲突约束）：更新 §0 启动条件、§5 边界新增启动门禁条目 |
| v0.4 | 2026-09-24 | 首适配器基线对齐主计划 v1.4 BE-9：P0 交付接口/注册表/`openai_chat_completions` 适配器；本计划范围收敛为"其余 3 协议 + `variant` 语义收敛"（§0/§1/§2/§4/§5 同步） |
| v0.5 | 2026-09-24 | 措辞校准：首适配器交付时点统一为"P0 交付、随主计划 B5 合入"，消除"已交付"误读（§0/§1/§2/§4 同步） |
| v0.6 | 2026-09-24 | BE-9 首适配器协议包先行落地（`itsm-backend/internal/llm/protocol` + 对照测试全绿）；接线与开关仍随主计划 B5，本计划启动门禁不变 |
| v0.7 | 2026-09-25 | 启动门禁达成注记：主计划 B5 出口全部满足（BE-8/BE-9/BE-5 落地、B6 前端完成、QA-3 全链路回归通过、Postgres 迁移实测与 prod 账本阻塞解除，主计划 v1.11–v1.14）；本文件待排期升 v1.0，期间冻结 `NewProviderFromConfig` 与协议包改动 |
