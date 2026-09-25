# openai_chat_completions 协议规格（parser spec）

> 文档定位：本规格对应适配器文件 `itsm-backend/internal/llm/protocol/openai_chat.go`（含 `registry.go` 注册项与 `errors.go` / `sse.go` 共用件），口径索引 §4.3 PA-2。
> 口径来源：`docs/plan/llm-protocol-adapter-plan.md` §4.2（变体语义表）、§4.3 PA-2（6 条差异/决策）、§4.4（参数默认值）、§4.5（能力位）。字段名 / 函数名 / 用例名一律以源码为准。

## 1. 元数据

### 1.1 协议与变体

| 项 | 值 | 源码坐标 |
|:---|:---|:---|
| 协议枚举 | `openai_chat_completions`（`ProtocolOpenAIChatCompletions`） | `adapter.go:23` |
| 承载实现 | `OpenAIChatAdapter`（无状态，可并发复用；`BuildRequest` / `BuildHeaders` 不触网） | `openai_chat.go:17-20` |
| 变体白名单 | `""`（标准形态）、`azure`、`ollama` —— `AdapterSpec{Variants}` 登记 | `registry.go:93-96` |
| 构造期选项 | **无**：`New` 忽略 variant，三个变体返回同一实现（`NewOpenAIChatAdapter()`） | `registry.go:95` |
| 资源路径 | `/v1/chat/completions`（`GetAPIPath` 静态值，不实现 `ModelPathAdapter`） | `openai_chat.go:26` |
| 默认端点 | `ollama` → `http://localhost:11434`（`openAIOllamaDefaultEndpoint`）；标准 / `azure` → 不登记（service 回退 `https://api.openai.com`） | `registry.go:11,126-133` |

### 1.2 鉴权头与端点归一（变体差异的实际落点）

| 项 | 标准变体 | `azure` | `ollama` |
|:---|:---|:---|:---|
| 鉴权头 | `Authorization: Bearer <apiKey>`（key 去空白后非空才下发） | 同左（旧 go-openai 默认口径） | 同左；key 为空则不下发 |
| 其他头 | `Content-Type: application/json`；调用方 `AdapterConfig.Headers` 大小写不敏感覆盖（可覆写 Authorization） | 同左 | 同左 |
| 端点归一 | 无特殊处理 | endpoint 仅主机名 → service 补 `/openai` 后再拼资源路径；已带版本段 → 只补资源段（§4.3 PA-2 #1） | 统一按 OpenAI 兼容 `/v1/chat/completions` 调用（§4.3 PA-2 #4，有意差异） |
| model 缺省 | 无 | `model` 为空回退 `deployment`（§4.3 PA-2 #2） | 无 |
| 明确不复刻 | — | `api-version` 查询参数（旧 `apiVersion` 从未参与请求，§4.3 PA-2 #3） | Ollama 原生 `/api/chat` 形态（§4.3 PA-2 #4） |
| 能力位（PA-5） | 开关关闭：流式 ✓ / 工具 ✓ / 推理 ✓ | 开关关闭：三项 ✗；开启且注册表命中：三项 ✓ | 同 `azure` |

## 2. 请求构建

### 2.1 请求体字段下发规则

| 字段 | 下发条件 | 值口径 |
|:---|:---|:---|
| `model` | 恒下发 | `RequestConfig.Model` 原样 |
| `messages` | 恒下发 | 见 §2.2（数组，可为空数组） |
| `stream` | 仅 `cfg.Stream == true` | `true`（不下发 `false`，对齐 go-openai omitempty） |
| `max_tokens` | 仅 `cfg.MaxTokens > 0` | 整数；接线层缺省 4096（§4.4） |
| `temperature` | 非推理模型**恒下发**（`0` 也下发） | `cfg.Temperature`；缺省 0.3 由 service 层解析（§4.4） |
| `tools` | 合并结果非空 | 见 §2.4 |
| `tool_choice` | `cfg.ToolChoice != nil` | 原样透传（不做形态归一） |

### 2.2 会话项映射表（内部 role → 线上形态）

| 内部消息 | 线上形态 | 字段存在性 |
|:---|:---|:---|
| 任意 `Message.Role` | `{"role": <role>}`，**不做角色归一**（`system` / `user` / `assistant` / `tool` 原样线上） | `role` 恒在 |
| `Message.Content` | `"content": <string>` | 仅非空串下发（空串省略键） |
| `Message.ToolCalls`（assistant） | `"tool_calls": [{"id","type":"function","function":{"name","arguments"}}]` | `len(ToolCalls) > 0` 才下发 |
| `Message.ToolCallID`（tool 结果） | `"tool_call_id": <string>` | 仅非空下发 |

### 2.3 system 提升与降级

- **无提升**：system 消息原样保留为 `{"role":"system","content":...}`（Chat Completions 原生支持 system 角色）。
- **不降级 / 不合并**：多条 system 消息按序全部下发；无“取最后一条”语义（与 `anthropic_messages` / `google_gemini` 的顶层抽取口径不同）。

### 2.4 工具声明与 `tool_choice`

- 声明线上形态：`{"type":"function","function":{"name":...,"description":?,"parameters":?}}`；`Description == ""` 省略 `description`，`Parameters == nil` 省略 `parameters`（`collectOpenAITools`）。
- 合并顺序：先遍历各消息的 `Message.Tools`，再追加 `RequestConfig.Tools`；**不去重**（与既有 `toOpenAIMessages` + `ChatStreamWithTools` 口径一致）。
- `tool_choice`：`nil` 不下发；非 nil **原样透传**（字符串 `auto` / `required` / `none`，或任意对象形态均不改写）。

### 2.5 工具结果回填

- 直接以 `role=tool` + `tool_call_id` 表达，本协议**不做函数名反查**、不构造占位名。
- `ToolCallID` 为空时该消息仅下发 `role` 与 `content`（缺 `tool_call_id`），是否被上游拒绝由上游语义决定。

### 2.6 采样参数与推理模型抑制

| 规则 | 口径 |
|:---|:---|
| `max_tokens` | 仅 `> 0` 下发；缺省 4096 属接线层职责（§4.4） |
| `temperature` | `cfg.ReasoningModel == false` 时**恒下发**（含 0 值）；为 `true` 时整个键省略 |
| `IsReasoningModel(model)` | 模型名 lower + trim，去掉单个 `models/` 前缀；命中 `codex` 子串或 `gpt-5` / `o1` / `o3` / `o4` / `o5` 前缀 → `true`（`isOpenAIReasoningModel`，与 `openai_responses` 同源） |

## 3. 非流式响应解析

| 抽取项 | 规则 | 空值 / 未知字段容错 |
|:---|:---|:---|
| 候选选择 | 取 `choices[0]`，其余候选忽略（`firstOpenAIChoice`） | 无 `choices` / 非数组 / 首项非对象 → 返回零值 `ProcessResult{}` |
| `finishReason` | `choices[0].finish_reason` 原样透传（`stop` / `length` / `tool_calls` / `content_filter` …），不做值域映射 | 键缺失 / 非字符串 → 空串 |
| 正文 `Content` | `choices[0].message.content` 字符串 | message 非对象 → 只保留已取的 `finishReason`，其余为空 |
| 推理链 `Reasoning` | `message.reasoning_content` 优先，回退 `message.reasoning`（`reasoningText`，DeepSeek 风格兼容） | 两者皆空 → 空串 |
| 工具调用 `ToolCalls` | `message.tool_calls[]`：`id` / `function.name` / `function.arguments`；**保序** | 非对象项、无 `function` 对象项跳过；某字段非字符串按空串 |
| 带内错误 | 顶层 `error` 对象或字符串 → `*ProtocolError`（`errorFromPayload`） | 解析失败：`json.Decoder` 报错直接返回普通 error，不静默降级为空结果 |

## 4. 流式事件序列（SSE）

| 帧 / 字段 | 语义 | 处理动作 |
|:---|:---|:---|
| 空 `data` 行 | 分隔 / 心跳 | 跳过，不产生回调 |
| `data: [DONE]` | 流结束哨兵 | 立即停止读取，返回已累积结果（不视为错误） |
| `choices[0].delta.content` | 正文增量 | 追加 `Content` + `callbacks.EmitText`（空串不下发回调） |
| `delta.reasoning_content` / `delta.reasoning` | 推理增量 | 追加 `Reasoning` + `callbacks.EmitReasoning` |
| `delta.tool_calls[]` | 工具调用分片 | 按 `index` 累积（`index` 缺失按 `0`）；`id` / `function.name` 非空即覆盖，`function.arguments` 顺序拼接；输出顺序按 index 首次出现（`accumulateOpenAIToolCalls` + `order`） |
| `choices[0].finish_reason` | 结束原因 | 非空即记录（后出现的非空值覆盖先前者） |
| 顶层 `error` | 带内错误 | 立即返回 `*ProtocolError`（`StatusCode=0`，不重试） |
| 非法 JSON 的 `data` | 坏帧 | 立即返回解码错误（`malformed stream chunk`），不吞帧继续 |
| 注释行（`:` 前缀）/ 无 `data` 的帧 / `event:` 字段 | 非数据帧 | `scanSSEFrames` 忽略注释行；无 `data` 的帧不派发；`event` 名本适配器不使用 |
| EOF 未终结帧 | 流结束 | EOF 时派发未终结帧；无 `[DONE]` 亦正常返回 |
| 快照补齐 | — | **本协议无快照恢复**：正文 / 推理 / 工具调用完全依赖增量帧；终态快照不参与补齐 |

## 5. 错误语义

### 5.1 HTTP 层错误（`NewHTTPError`，有 HTTP 语义）

| 项 | 口径 |
|:---|:---|
| 映射入口 | `protocol.NewHTTPError(protocolName, resp, body)`（service 侧对非 2xx 响应调用） |
| 提取来源 | 响应体 `{"error":{"message","code"|"type"}}` 或 `error` 字符串；非 JSON 体回退 `resp.Status` |
| 兜底 | `Message` 为空 → `"upstream request failed"`；`Body` 截断至 2048 字节（`protocolErrorBodyLimit`） |
| 重试判定 | `Retryable()`：`429` 或 `StatusCode >= 500` → `true`；其余 4xx（含 401/403/404）→ `false` |

### 5.2 带内错误（无 HTTP 语义，一律不重试）

| 形态 | 示例（测试事实） | 结果 |
|:---|:---|:---|
| 非流式顶层 `error` | `{"error":{"message":"context length exceeded","type":"invalid_request_error","code":"context_length_exceeded"}}` | `ProtocolError{Code:"context_length_exceeded", Message:"context length exceeded", StatusCode:0}`，`Retryable()==false` |
| 流式 `data` 帧 `error` | `data: {"error":{"message":"upstream stream failed","code":"stream_error"}}` | `ProtocolError{Code:"stream_error", StatusCode:0}`，不重试 |
| 缺 body / 非法 JSON | `HandleResponse(..., nil, ...)` / `{"choices":` | 普通 error（`response body is required` / `decode response: ...`），非 `ProtocolError` |

## 6. 边缘项与容错清单（与单测一一对应）

| # | 边缘项 | 适配器行为 | 单测锚点 |
|:---|:---|:---|:---|
| 1 | 空 `content`（纯工具调用消息） | 省略 `content` 键 | `TestOpenAIChatAdapterBuildRequest` |
| 2 | 消息携带工具 + 显式工具合并 | 消息在前、显式在后，共 2 项 | `TestOpenAIChatAdapterBuildRequest` |
| 3 | 非流式 / 无工具调用 | 不下发 `stream` / `tools` / `tool_choice` | `TestOpenAIChatAdapterBuildRequest` |
| 4 | 推理模型 | 抑制 `temperature`，仍下发 `stream` / `tools` / `tool_choice` | `TestOpenAIChatAdapterBuildRequest`、`TestOpenAIChatAdapterIsReasoningModel` |
| 5 | `choices` 为空 | 返回零值 `ProcessResult{}` | `TestOpenAIChatAdapterProcessResponse` |
| 6 | `reasoning_content` 抽取 | Reasoning 取 `reasoning_content`；`content` 为空时正文留空 | `TestOpenAIChatAdapterProcessResponse` |
| 7 | 流式工具参数分片 | 按 `index` 累积为完整 `{"tz":"UTC"}` | `TestOpenAIChatAdapterHandleStream` |
| 8 | 流内错误体 | 映射 `ProtocolError` 且不重试（非流式 / 流式各一） | `TestOpenAIChatAdapterHandleErrorPayload` |
| 9 | HTTP 429 / 500 / 401 | 前两者 `Retryable()==true`，401 为 `false` | `TestNewHTTPError` |
| 10 | 注册表变体白名单 / 名归一小写去空白 / 未登记变体 | `Supports` 与 `NewAdapter` 命中规则；`bogus` 变体 → `ErrAdapterNotFound` | `TestDefaultRegistryScope` |
| 11 | 默认端点按变体 | ollama=`http://localhost:11434`；默认 / azure 为空串 | `TestOpenAIChatDefaultEndpointByVariant` |
| 12 | 旧分支等价性（P0 基线） | 非流式文本、流式 + 工具、工具调用一致性、错误映射与旧 `OpenAIProvider` 对照 | `equivalence_test.go`：`TestEquivalenceNonStreamChat` / `TestEquivalenceStreamWithTools` / `TestEquivalenceToolCallConsistency` / `TestEquivalenceErrorMapping` |

## 7. 决策 / 差异索引（§4.3 PA-2）

| §4.3 编号 | 差异 / 决策 | 本规格落点 |
|:---|:---|:---|
| PA-2 #1 | azure endpoint 只给主机名时补 `/openai`，与旧 `AzureProvider` 同址 | §1.2「端点归一」 |
| PA-2 #2 | azure `model` 缺省回退 `deployment` | §1.2「model 缺省」 |
| PA-2 #3 | 鉴权保持 `Authorization: Bearer`；不引入 `api-version` | §1.2、§5.1 |
| PA-2 #4 | ollama 由原生 `/api/chat` 归一为 OpenAI 兼容 `/v1/chat/completions`（**有意差异**） | §1.1 资源路径、§1.2 |
| PA-2 #5 | ollama 下发 `max_tokens` 4096 / `temperature` 0.3（沿用 BE-9 默认值表） | §2.1、§2.6 |
| PA-2 #6 | azure / ollama 支持流式与工具调用（旧分支仅非流式 `Chat`） | §4、§6 |
| §4.2 行 | 三变体缺省 endpoint / 关键字段 / 鉴权头 | §1.1、§1.2 |
| §8 Q2 | Ollama 原生形态不新增适配器，归一承载 | §1.2、§7 PA-2 #4 |

## 8. 测试索引（`openai_chat_test.go` + `equivalence_test.go`）

| 用例 | 覆盖点 |
|:---|:---|
| `TestOpenAIChatAdapterMetadata` | `Name()`、`GetAPIPath()` |
| `TestOpenAIChatAdapterIsReasoningModel` | 命中集（gpt-5* / o1 / o3 / gpt-5-codex / `models/o4-mini`）与未命中集 |
| `TestOpenAIChatAdapterBuildRequest` | 字段存在性、推理抑制、工具合并、工具调用消息线上字段 |
| `TestOpenAIChatAdapterBuildHeaders` | Bearer 鉴权、附加头大小写不敏感覆盖 |
| `TestOpenAIChatAdapterProcessResponse` | 推理链 / 工具调用抽取、空 `choices` 零值 |
| `TestOpenAIChatAdapterHandleStream` | 正文 / 推理增量、工具参数累积、`finish_reason` |
| `TestOpenAIChatAdapterHandleErrorPayload` | 非流式与流式带内错误映射、不重试 |
| `TestNewHTTPError` | 429 / 500 / 401 的状态码、消息与 `Retryable()` |
| `TestDefaultRegistryScope` | 一协议一实现、变体白名单、名归一、未命中语义 |
| `TestOpenAIChatDefaultEndpointByVariant` | 三变体默认端点登记矩阵 |
| `TestEquivalenceNonStreamChat` / `TestEquivalenceStreamWithTools` / `TestEquivalenceToolCallConsistency` / `TestEquivalenceErrorMapping` | 同桩服务下与旧 `service.OpenAIProvider` 的对照等价（P0 基线锁） |

## 存疑 / 待澄清

1. **「变体差异在构造期固化进实例」的措辞落差**：§4.1 称变体差异构造期固化，但 `openai_chat_completions` 的 `New` 忽略 variant，`NewOpenAIChatAdapter()` 无参、实例不携带变体；azure / ollama 的实际差异由 service 层（`normalizeVariantEndpoint` / `llmProviderModelForVariant` / 默认值表）承担，适配器只承载 OpenAI 兼容线上形态。建议在计划或本规格中明确「本协议变体差异属接线层职责」。
2. **ollama 采样参数的归口**：§4.3 PA-2 #5 描述 ollama 下发 `max_tokens` 4096 / `temperature` 0.3，但适配器在 `MaxTokens == 0` 时不下发 `max_tokens`、`Temperature` 为 0 时下发 `0`。该差异只在 service 层完成默认值解析后才与描述一致，本规格按「缺省值由接线层解析」记录。
3. **`temperature` 恒下发与「未给时不发」的差异**：非推理模型下适配器对 `0` 值也下发 `temperature`，与 go-openai 侧「未设置即 omitempty 省略」在“未设置”语义上不同（值语义相同）。如需严格逐字节对齐旧分支，需由调用方保证显式传值。
