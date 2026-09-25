# openai_responses 协议规格（parser spec）

> 文档定位：本规格对应适配器文件 `itsm-backend/internal/llm/protocol/openai_responses.go`（注册项见 `registry.go`），口径索引 §4.3 PA-3。
> 口径来源：`docs/plan/llm-protocol-adapter-plan.md` §4.2、§4.3 PA-3（9 条）、§4.4、§4.5。字段名 / 函数名 / 用例名以源码为准。

## 1. 元数据

### 1.1 协议与变体

| 项 | 值 | 源码坐标 |
|:---|:---|:---|
| 协议枚举 | `openai_responses`（`ProtocolOpenAIResponses`） | `adapter.go` 常量区 |
| 承载实现 | `OpenAIResponsesAdapter`（空结构体、无状态，可并发复用） | `openai_responses.go:28` |
| 变体白名单 | 仅 `""`（`VariantDefault`） | `registry.go:101-104` |
| 构造期选项 | **无**：`New` 忽略 variant，`NewOpenAIResponsesAdapter()` 无参 | `registry.go:103` |
| 资源路径 | `/v1/responses`（`GetAPIPath` 静态值；不实现 `ModelPathAdapter`） | `openai_responses.go:37` |
| 默认端点 | **不登记** → service 回退 `https://api.openai.com`（与 `openai_chat_completions` 标准形态同址） | `registry.go:122-123` |
| 鉴权头 | `Authorization: Bearer <apiKey>`（key 去空白后非空才下发）；`Content-Type: application/json`；调用方 headers 大小写不敏感覆盖 | `openai_responses.go:46-55` |
| 载体决策 | 取官方 Responses API；**不复刻** ChatGPT backend 形态（依赖 OAuth/session 凭证与专有头部，不适配「endpoint + api_key」实例模型） | PA-3 #1、§8 Q1 |
| 状态语义 | 无状态：`store` 恒 `false`，不实现 `previous_response_id` / conversation 续接，每次重放完整历史 | PA-3 #2 |

### 1.2 能力位（PA-5，§4.5）

| 协议 / 变体 | 开关关闭 | 开关开启 |
|:---|:---|:---|
| `openai_responses` / 默认 | 流式 ✓ / 工具 ✓ / 推理 ✓（适配器为唯一承载，无旧分支） | 三项全真（不变） |

## 2. 请求构建

### 2.1 请求体字段下发规则

| 字段 | 下发条件 | 值口径 |
|:---|:---|:---|
| `model` | 恒下发 | `cfg.Model` 原样 |
| `input` | 恒下发 | 项数组，可为空数组（无 messages 时仍下发 `[]`） |
| `store` | **恒下发** | `false`（关闭服务端响应存储，PA-3 #2） |
| `instructions` | 前导 system/developer 文本拼接后非空 | `\n\n` 连接（见 §2.3） |
| `stream` | 仅 `cfg.Stream == true` | `true`；非流式不下发 false |
| `max_output_tokens` | 仅 `cfg.MaxTokens > 0` | 接线层缺省 4096（§4.4） |
| `temperature` | 仅 `!cfg.ReasoningModel` | 缺省 0.3 由 service 层解析（§4.4） |
| `tools` | 合并结果非空 | 扁平结构（见 §2.4） |
| `tool_choice` | `cfg.ToolChoice != nil` 且归一结果非 nil | 见 §2.4 |

### 2.2 会话项映射表（内部消息 → `input` 项）

| 内部消息 | 线上项 | 关键字段 |
|:---|:---|:---|
| 前导 `system` / `developer` | **不进入 input** | 合并进顶层 `instructions`（每段 trim，空段跳过） |
| 非前导 `system` / `developer` | `message` 项 | `role:"developer"`，`content:[{"type":"input_text","text":...}]` |
| `assistant`（有正文） | `message` 项 | `role:"assistant"`，`content:[{"type":"output_text","text":...}]`；正文 trim 后为空则整项省略 |
| `assistant` 的每条 `ToolCalls` | `function_call` 项 | `call_id`（= `call.ID`）/ `name` / `arguments`；**逐条独立项**，不并入 message |
| `tool` | `function_call_output` 项 | `call_id`（= `ToolCallID`）/ `output`（= `Content`）；无缺名占位、无过滤 |
| 其它（默认，含 `user`） | `message` 项 | `role:"user"`，`content:[{"type":"input_text","text":...}]`；空文本 → `content: []` |

- 角色判定：`strings.ToLower(strings.TrimSpace(role))`；出现首个非 system/developer 消息后 `inLeadingInstructions` 关闭。
- 工具往返为独立项（PA-3 #4）：`call_id` 缺失时响应侧回退项 `id`；两者都缺的 `function_call` 在**响应解析侧**丢弃（请求侧按原样下发）。

### 2.3 system / instructions 提升与降级

| 位置 | 规则 |
|:---|:---|
| 前导（出现在任何非 system/developer 之前） | 逐条 `TrimSpace` 后按出现顺序收集，`\n\n` 连接为顶层 `instructions`；空串跳过 |
| 非前导（更靠后出现） | 降级为 `role="developer"` 的 message 项（Responses 无 `role=system` 的 message 项），文本原样 |
| 全部为空 | 不下发 `instructions` 键 |

### 2.4 工具声明与 `tool_choice` 归一

- 声明形态（扁平，无嵌套 `function`）：`{"type":"function","name":...,"description":?,"parameters":?}`；`Description != ""` 才带 description（不 trim），`Parameters != nil` 才带 parameters。
- 合并顺序：消息携带的 `Message.Tools` 在前、`RequestConfig.Tools` 在后；**不去重**。
- `tool_choice` 归一（`normalizeResponsesToolChoice`）：

| 输入形态 | 输出 |
|:---|:---|
| 字符串（`auto` / `required` / `none` …） | 原样透传（仅判类型，不看取值） |
| `{"name":"x", ...}`（顶层 name 非空） | `{"type":"function","name":"x"}`（**不校验 type**） |
| `{"type":"function","function":{"name":"x"}}` | `{"type":"function","name":"x"}`（展平） |
| 空对象 / 无 name / 非 map 非 string | `nil` → **不下发** |

### 2.5 工具结果回填

- `function_call_output` 直接携带 `call_id` + `output`，本协议**不做函数名反查**，也不构造占位名（与 `google_gemini` 的 `functionResponse` 口径不同）。
- `ToolCallID` 为空时仍下发 `call_id: ""`（无缺失保护；上游语义决定是否报错）。

### 2.6 采样参数与推理模型抑制

| 规则 | 口径 |
|:---|:---|
| `max_output_tokens` | 仅 `> 0` 下发（接线层缺省 4096） |
| `temperature` | `cfg.ReasoningModel == true` 时整个键省略；否则**恒下发**（含 0 值） |
| `IsReasoningModel` | 与 `openai_chat_completions` 同源 `isOpenAIReasoningModel`：lower+trim、去单个 `models/` 前缀；命中 `codex` 子串或 `gpt-5` / `o1`–`o5` 前缀 |

## 3. 非流式响应解析

| 抽取项 | 规则 | 容错 |
|:---|:---|:---|
| 输出项遍历 | `output[]` 仅保留对象项，按序处理；未知 `type` 忽略（PA-3 #9） | 无 `output` / 非数组 → 各项为空 |
| 正文 `Content` | `message` 项 `content[]`：`output_text` → `text`；`refusal` → `refusal` 优先、回退 `text`；多片段按序拼接 | 非对象片段跳过 |
| 推理链 `Reasoning` | `reasoning` 项 `summary[]`：`text` 优先、回退 `content`，按序拼接 | summary 缺失 → 空串 |
| 工具调用 `ToolCalls` | `function_call` 项：`name` 必须非空，`call_id` 优先、回退项 `id`，两者都缺 → **丢弃该项**（不构造半成品）；`arguments` 原样字符串 | 保序 |
| `finishReason` | ① 顶层 `stop_reason`（Codex 兼容上游）优先；② `status=completed` → 有工具调用 `tool_calls`，否则 `stop`；③ `status=incomplete` → `incomplete_details.reason`，缺省 `incomplete`；④ 其它 → 空串 | 大小写/空白不敏感 |
| 带内失败 | `handleCompletion` 依次：解码 → `errorFromPayload`（顶层 error）→ `status=failed` 时 `responsesFailureError` | `response.error` 子文档优先，回退顶层 `error`；`code` 缺省 `response_failed`、`message` 缺省 `responses request failed` |

## 4. 流式事件序列（事件驱动 SSE）

| 事件名 | 语义 | 处理动作 |
|:---|:---|:---|
| 空 `data` | 心跳 / 分隔 | 跳过 |
| `response.output_text.delta` | 正文增量 | `delta` 非空 → 追加 `Content` + `EmitText` |
| `response.reasoning_summary_text.delta` / `response.reasoning_text.delta` | 推理增量 | `delta` 非空 → 追加 `Reasoning` + `EmitReasoning` |
| `response.output_item.added` | 工具项建立 | 仅 `item.type=="function_call"`：按 `index` 建累积槽，`call_id`/`id`、`name`、非空 `arguments` 写入 |
| `response.function_call_arguments.delta` | 工具参数分片 | 按 `index` 追加 `delta` 到 `arguments` |
| `response.output_item.done` | 工具项收口 | `id` / `name` 非空即覆盖；`arguments` **仅在累积为空且 done 值非空**时回填（增量优先、不重复） |
| `response.completed` / `response.incomplete` / `response.done` | 终态 | 先快照补齐（仅补空位），再 `finishReason = firstNonEmpty(已有, 归一值)` |
| `response.failed` | 带内失败 | 立即返回 `responsesFailureError`（`StatusCode=0`，不重试） |
| `error` | 带内错误 | `responsesInBandError`：`response.error` 优先，兼容顶层平铺 `code`/`message`；缺省 `stream_error` / `responses stream error` |
| 非法 JSON 的 `data` | 坏帧 | 立即返回 `malformed stream chunk` 错误 |
| 事件类型判定 | 帧名来源 | **`frame.Event` 优先**，为空才回退事件体 `type`（与 `anthropic_messages` 相反） |
| `index` 解析 | 分片槽位 | `float64` / `json.Number` / 字符串数字均可；解析失败按 `0` |
| 快照补齐 | 终态兜底 | `message`/`reasoning` 仅在对应累积为空时补并回调；`function_call` 按 `call_id`→`name` 复用已有槽，未命中追加新槽，字段仅在空位回填 |
| 注释行 / 无 `data` 帧 / `event:` 名 | 非数据帧 | `scanSSEFrames` 忽略注释行、不派发无 `data` 帧；EOF 派发未终结帧 |

## 5. 错误语义

### 5.1 HTTP 层错误（`NewHTTPError`）

| 项 | 口径 |
|:---|:---|
| 触发 | service 侧对非 2xx HTTP 响应调用 `protocol.NewHTTPError(ProtocolOpenAIResponses, resp, body)` |
| 重试 | `Retryable()`：`429` 或 `>=500` → `true`；其余 4xx → `false`（口径与其它协议一致） |

### 5.2 带内错误（映射 `*ProtocolError`，一律不重试）

| 形态 | 帧 / 示例 | 结果 |
|:---|:---|:---|
| 流式 `response.failed` | `{"type":"response.failed","response":{"error":{"code":"server_error","message":"..."}}}` | `ProtocolError{Code:"server_error", StatusCode:0}` |
| 流式顶层 `error` | `{"type":"error","code":"rate_limit_exceeded","message":"..."}` | `ProtocolError{Code:"rate_limit_exceeded", Message:"..."}` |
| 流式 `error` 空体 | `{"type":"error"}` | 缺省 `stream_error` / `responses stream error` |
| 非流式顶层 `error` | `{"error":{"message":"bad request"}}` | `errorFromPayload` 映射 |
| 非流式 `status=failed` | `{"status":"failed","error":{"code":"x"}}` | `responsesFailureError` 映射 |
| 解码失败 | 空 body / 非法 JSON | 普通 error（`response body is required` / `decode response: ...`），非 `ProtocolError` |

## 6. 边缘项与容错清单（与单测一一对应）

| # | 边缘项 | 适配器行为 | 单测锚点 |
|:---|:---|:---|:---|
| 1 | 元数据 | `Name()` / `GetAPIPath()` = `/v1/responses` | `TestOpenAIResponsesAdapterMetadata` |
| 2 | 无 apiKey / 有 apiKey / 附加头覆盖 | 分别无 Authorization / Bearer / 大小写不敏感覆盖 | `TestOpenAIResponsesAdapterBuildHeaders` |
| 3 | 前导 system 合并 instructions；非前导 system 降级 developer；`store=false` 恒下发 | — | `TestOpenAIResponsesBuildRequest` |
| 4 | 历史工具往返：assistant 正文 + `function_call` 独立项；`tool` → `function_call_output` | 项顺序按历史次序 | `TestOpenAIResponsesBuildRequest` |
| 5 | 工具扁平结构 / 三种 `tool_choice` 形态 / 不可识别不下发 | Chat 风格展平；Responses 风格与字符串透传 | `TestOpenAIResponsesBuildRequestToolsAndToolChoice` |
| 6 | 推理模型抑制 `temperature`，仍下发 `max_output_tokens` | 字段存在性逐项断言 | `TestOpenAIResponsesBuildRequestReasoningModelSuppressesSampling` |
| 7 | 输出项抽取：message/reasoning/function_call；未知项（如 `web_search_call`）忽略；缺 name 或缺 id 的 `function_call` 丢弃 | 拼接顺序与 `finishReason` 推断 | `TestOpenAIResponsesProcessResponse` |
| 8 | 非流式：`status=failed` 与顶层 `error` 映射带内错误；正常文本路径 | `StatusCode=0`、不重试 | `TestOpenAIResponsesHandleResponseNonStream` |
| 9 | 流式：正文/推理增量与工具参数分片累积 | 增量顺序 | `TestOpenAIResponsesHandleResponseStream` |
| 10 | 流式快照恢复：仅空位补齐（增量为准、不重复） | 正文 / 推理 / 工具调用 | `TestOpenAIResponsesHandleResponseStreamSnapshotRecovery` |
| 11 | 流式失败：`response.failed` / `error` 事件 / 非法帧 | 带内错误 + 不重试 | `TestOpenAIResponsesHandleResponseStreamErrors` |

## 7. 决策 / 差异索引（§4.3 PA-3）

| 编号 | 决策 / 差异 | 本规格落点 |
|:---|:---|:---|
| PA-3 #1 | 载体取官方 Responses API，不复刻 ChatGPT backend 形态 | §1.1「载体决策」、§1.2 |
| PA-3 #2 | `store=false` 恒下发；不实现服务端存储 / 续接 | §1.1「状态语义」、§2.1 |
| PA-3 #3 | 前导 system/developer → 顶层 `instructions`；后续 → `role=developer` 项 | §2.2、§2.3 |
| PA-3 #4 | 工具往返改独立项 `function_call` / `function_call_output`；缺 id 的项丢弃 | §2.2、§3 |
| PA-3 #5 | `tools` 扁平结构；`tool_choice` 三形态归一 | §2.4 |
| PA-3 #6 | `max_output_tokens` >0 才发；推理模型抑制 `temperature` | §2.1、§2.6 |
| PA-3 #7 | 事件驱动 SSE + 快照补齐 + `finishReason` 推断 | §4 |
| PA-3 #8 | `response.failed` / `error` 事件 → `*ProtocolError{StatusCode:0}`，不重试 | §5.2 |
| PA-3 #9 | 未知项类型忽略；`refusal` / 空输出容错 | §3（见「存疑」第 5 条） |

## 8. 测试索引（`openai_responses_test.go`）

| 用例 | 覆盖点 |
|:---|:---|
| `TestOpenAIResponsesAdapterMetadata` | `Name()` / `GetAPIPath()` / `IsReasoningModel` |
| `TestOpenAIResponsesAdapterBuildHeaders` | Bearer 鉴权与附加头覆盖 |
| `TestOpenAIResponsesBuildRequest` | instructions 提升、input 项映射、`store=false`、采样参数 |
| `TestOpenAIResponsesBuildRequestToolsAndToolChoice` | 扁平工具结构与 `tool_choice` 归一 |
| `TestOpenAIResponsesBuildRequestReasoningModelSuppressesSampling` | 推理模型抑制 `temperature` |
| `TestOpenAIResponsesProcessResponse` | 三类输出项抽取、未知项忽略、`finishReason` 推断 |
| `TestOpenAIResponsesHandleResponseNonStream` | 非流式解码、失败态与带内错误 |
| `TestOpenAIResponsesHandleResponseStream` | 增量与工具参数累积 |
| `TestOpenAIResponsesHandleResponseStreamSnapshotRecovery` | 终态快照补齐（仅空位） |
| `TestOpenAIResponsesHandleResponseStreamErrors` | `response.failed` / `error` / 坏帧 |

## 存疑 / 待澄清

1. **`[DONE]` 帧不被识别**：`handleStream` 只跳过空 `data`，`data: [DONE]` 会进入 `json.Unmarshal` 并返回 `malformed stream chunk` 错误；与 `openai_chat_completions`（`[DONE]` 为终止帧）和 `google_gemini`（显式跳过 `[DONE]`）口径不一致。现有用例未覆盖该帧。
2. **流式 `response.completed` 内的 `status=failed` 不映射错误**：非流式由 `responsesPayloadError` 兜底 `status=failed`，流式仅识别显式 `response.failed` / `error` 事件；两条路径的失败语义存在不对称。
3. **PA-3 #9「refusal/空输出按空正文处理」与实现措辞落差**：`responsesMessageText` 对 `refusal` 片段取 `refusal`（回退 `text`）并拼入 `Content`，即 refusal 文本会作为正文可见内容返回，而非按空正文处理。本规格按代码事实记录。
4. **`New` 忽略 variant、实例无变体字段**：`openai_responses` 当前仅标准变体，PA-3 #1 提及「backend 形态如后续需要按新 variant 构造期选项引入」；当前 `NewOpenAIResponsesAdapter()` 无参，尚无构造期选项落点。
5. **`output_item.done` 的参数回填条件**：仅当累积器 `arguments` 为空且 done 事件值非空时回填；若上游只发 done 且值为空串，工具参数保持空（不报错）。
