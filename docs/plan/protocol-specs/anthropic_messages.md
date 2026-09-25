# anthropic_messages 协议规格（parser spec）

> 文档定位：本规格对应适配器文件 `itsm-backend/internal/llm/protocol/anthropic_messages.go`（注册项与默认端点见 `registry.go`），口径索引 §4.3 PA-1。
> 口径来源：`docs/plan/llm-protocol-adapter-plan.md` §4.2、§4.3 PA-1（6 条）、§4.4、§4.5。字段名 / 函数名 / 用例名以源码为准。

## 1. 元数据

### 1.1 协议与变体

| 项 | 值 | 源码坐标 |
|:---|:---|:---|
| 协议枚举 | `anthropic_messages`（`ProtocolAnthropicMessages`） | `adapter.go` 常量区 |
| 承载实现 | `AnthropicMessagesAdapter{variant string}`（无状态，可并发复用；**变体构造期固化进实例**） | `anthropic_messages.go:25-32` |
| 变体白名单 | `""`（官方）、`minimax`（MiniMax Anthropic 兼容端点） | `registry.go:97-100` |
| 变体归一 | `normalizeAnthropicVariant`：`TrimSpace` + `ToLower`（service 层 `NormalizeLLMVariant` 同口径） | `anthropic_messages.go:409-412` |
| 构造期选项 | `NewAnthropicMessagesAdapter(variant)`；变体只决定 `max_tokens` 字段名（`max_tokens` / `maxTokens`） | `anthropic_messages.go:251-256` |
| 资源路径 | `/v1/messages`（两变体同值；endpoint 已带 `/v1` 版本段时由 service 只补资源段） | `anthropic_messages.go:39` |
| 默认端点 | 官方 → `https://api.anthropic.com`；`minimax` → `https://api.minimaxi.com/anthropic/v1`（与旧 `MiniMaxProvider.baseURL` 同值） | `registry.go:134-138`、`anthropic_messages.go:269-271` |
| 鉴权头 | `x-api-key: <apiKey>`（去空白后非空才下发）+ `anthropic-version: 2023-06-01`；两变体同口径 | `anthropic_messages.go:53-65`、`:260` |
| 其他头 | `Content-Type: application/json`；调用方 headers 大小写不敏感覆盖 | 同上 |
| `max_tokens` 缺省 | 恒回填 `4096`（`anthropicDefaultMaxTokens`，Anthropic 必填语义；旧 `MiniMaxProvider` 线上同值） | `anthropic_messages.go:81-85`、`:263` |
| 等价基线 | `service.MiniMaxProvider`（minimax 变体）；对照测试 `service/llm_anthropic_equivalence_test.go` | `anthropic_messages.go:17-18` |

### 1.2 能力位（PA-5，§4.5）

| 协议 / 变体 | 开关关闭 | 开关开启 |
|:---|:---|:---|
| `anthropic_messages` / 默认、`minimax` | 三项 ✗（旧分支仅非流式 `Chat`，`implemented=true`） | 三项全真（流式 / 工具 / 推理链，R-3 放量解除） |

## 2. 请求构建

### 2.1 请求体字段下发规则

| 字段 | 下发条件 | 值口径 |
|:---|:---|:---|
| `model` | 恒下发 | `cfg.Model` 原样 |
| `messages` | 恒下发 | 数组（system 已被剔除，可为空数组） |
| `max_tokens`（默认变体）/ `maxTokens`（minimax） | **恒下发** | `cfg.MaxTokens > 0` 取原值，否则回填 `4096` |
| `system` | 提取结果非空 | 多条 system 取**最后一条非空**（§2.3） |
| `stream` | 仅 `cfg.Stream == true` | `true`；非流式不下发 |
| `temperature` | 仅 `!cfg.ReasoningModel` | 恒下发（含 0 值）；minimax 缺省 1.0 / 官方缺省 0.3 由 service 层解析（§4.4） |
| `tools` | 合并结果非空 | Anthropic 形态（见 §2.4） |
| `tool_choice` | `cfg.ToolChoice != nil` 且归一结果非 nil | 见 §2.4 |

### 2.2 会话项映射表（内部消息 → 线上形态）

| 内部消息 | 线上形态 | 关键字段 |
|:---|:---|:---|
| `Role == "system"` | **跳过**（不进入 messages） | 由顶层 `system` 承载 |
| `Role == "tool"` **或** `ToolCallID != ""` | `{"role":"user","content":[{"type":"tool_result","tool_use_id":<ToolCallID>,"content":<Content>}]}` | `content` 为原样字符串（非 JSON 对象） |
| `len(ToolCalls) > 0` | `{"role":<Role>,"content":[?text 块, tool_use 块…]}` | text 块仅当 `Content != ""`；`tool_use`：`id` / `name` / `input = decodeToolInput(arguments)`（解析失败或 nil → `{}`） |
| 其余 | `{"role":<Role>,"content":<Content 字符串>}` | 角色原样透传（不做大小写归一） |

### 2.3 system 提升规则

- system 消息**全部提升**到顶层 `system` 字符串，且 messages 中不保留（与 `openai_responses` 的「非前导降级」不同）。
- 多条 system 时取**最后一条非空**（`strings.TrimSpace(content) != ""` 判定，写入值为原始未 trim 文本），对齐旧 `MiniMaxProvider` 的 `systemPrompt = m.Content` 覆盖口径。
- 全部为空 → 不下发 `system` 键。

### 2.4 工具声明与 `tool_choice` 归一

- 声明形态：`{"name":...,"input_schema":...,"description":?}`；`Parameters == nil` 时 `input_schema` 回填空对象 `{"type":"object"}`（**不含** `properties`，与 gemini 的补全形态不同）；`Description != ""` 才带 description。
- 合并顺序：消息携带的 `Message.Tools` 在前、`RequestConfig.Tools` 在后；不去重。
- `tool_choice` 归一（`anthropicToolChoice`）：

| 输入形态 | 输出 |
|:---|:---|
| map 且 `type == "function"` | `{"type":"tool","name":<name>}`；`function` 子对象存在时 **name 取 `function.name` 覆盖顶层 `name`**（子对象 name 为空时结果为 `""`） |
| map 且 `type == "required"` / `"any"` | `{"type":"any"}` |
| map 且 `type == "none"` | `nil` → **不下发** |
| map 其它 type（如 Anthropic 原生 `auto`） | 原样透传 |
| 非 map（含字符串 `"auto"`） | 原样透传（不做形态改写） |

### 2.5 工具结果回填

- 逐条映射为 `user` 消息的 `tool_result` 块，`tool_use_id` = `Message.ToolCallID`、`content` = `Message.Content` 原样字符串。
- **无函数名反查、无占位名**：Anthropic 侧以 `tool_use_id` 配对即可，无需 name（与 `google_gemini` 的 name 反查 + `"tool"` 占位形成对照）。
- `ToolCallID` 为空但 `Role != "tool"` 的消息同样会走 tool_result 分支（判定为 `Role=="tool" || ToolCallID != ""`）。

### 2.6 采样参数与推理模型抑制

| 规则 | 口径 |
|:---|:---|
| `temperature` | `cfg.ReasoningModel == true` 时整个键省略；普通 `claude-*` / `MiniMax-*` **不抑制**（旧 `MiniMaxProvider` 恒下发 1.0，适配路径保持线上行为） |
| `IsReasoningModel` | 模型名 trim + lower 后含 `thinking` 或 `reasoning` 子串 → `true`（`normalizeAnthropicVariant` 复用） |
| 范围外 | 扩展思考参数（`thinking.budget_tokens`）不在本轮范围；适配器只做响应侧 `thinking` 块解析（§4.4、PA-5） |

## 3. 非流式响应解析

| 抽取项 | 规则 | 容错 |
|:---|:---|:---|
| 结束原因 | `stop_reason` 优先，回退 `stopReason`（`stopReason` 辅助函数；minimax camelCase 兼容） | 均缺失 → 空串 |
| 正文 `Content` | 遍历 `content[]`：`text` 块取 `text` 字段，**全部块按序拼接**（旧分支只取首个 text 块，PA-1 #3） | 无 text 块 → 空串（`tool_use`-only 响应合法，旧分支报错 `MiniMax: no text content in response`） |
| 推理链 `Reasoning` | `thinking` 块取 `thinking` 字段，按序拼接 | 非对象块跳过；未知块类型忽略 |
| 工具调用 `ToolCalls` | `tool_use` 块：`id` / `name` / `Arguments = encodeToolInput(input)` | `input` 为 nil / 空对象 / 序列化失败 → `Arguments = ""`（`encodeToolInput`) |
| 带内错误 | 顶层 `error` 对象或字符串 → `*ProtocolError`（`errorFromPayload`，`StatusCode=0`） | 解码失败 → 普通 error（非 `ProtocolError`） |

## 4. 流式事件序列（SSE，官方与 MiniMax 兼容端点同事件名）

| 事件名 | 语义 | 处理动作 |
|:---|:---|:---|
| 空 `data` | 心跳 / 分隔 | 跳过 |
| `message_start` | 消息开始 | **no-op**（不建立状态，`message_start` 内的 usage 等字段忽略） |
| `content_block_start` | 块开始 | 仅 `content_block.type == "tool_use"` 时建累积槽（按 `index`）：写入 `id` / `name`；`input` 序列化非空且非 `"{}"` 时写入 `arguments`；其它块类型 no-op |
| `content_block_delta` + `delta.type == "text_delta"` | 正文增量 | `text` 非空 → 追加 `Content` + `EmitText` |
| `content_block_delta` + `delta.type == "thinking_delta"` | 推理增量 | `thinking` 非空 → 追加 `Reasoning` + `EmitReasoning` |
| `content_block_delta` + `delta.type == "input_json_delta"` | 工具参数分片 | `partial_json` 非空 → 按 `index` 追加到 `arguments` |
| `content_block_delta` 其它 delta 类型 | 未知增量 | 忽略 |
| `content_block_stop` | 块结束 | **no-op**（无收口动作，参数已由分片累积） |
| `message_delta` | 结束原因 | `delta` 内 `stop_reason` 优先、回退 `stopReason`；非空即覆盖 `finishReason` |
| `message_stop` | 消息结束 | **no-op**（终点由 SSE EOF 决定） |
| `ping` | keep-alive | **no-op**（注释行由 `scanSSEFrames` 忽略，`ping` 事件本身不进 switch） |
| `error` | 带内错误 | `errorFromPayload` 前置拦截（有 error 体时走映射）；裸 `{"type":"error"}` 兜底 `*ProtocolError{Message:"upstream stream error", StatusCode:0}` |
| 事件类型判定 | 帧名来源 | **事件体 `type` 优先**，为空才回退 `frame.Event`（与 `openai_responses` 相反） |
| `index` 解析 | 分片槽位 | `intValue`：仅接受 `float64`（JSON 数字），其它类型按 `0` |
| 非法 JSON 的 `data` | 坏帧 | 立即返回 `malformed stream event` 错误 |

## 5. 错误语义

### 5.1 HTTP 层错误（`NewHTTPError`）

| 项 | 口径 |
|:---|:---|
| 触发 | service 侧对非 2xx HTTP 响应调用 `protocol.NewHTTPError(ProtocolAnthropicMessages, resp, body)` |
| 重试 | `Retryable()`：`429` 或 `>=500` → `true`；其余 4xx → `false` |

### 5.2 带内错误（`*ProtocolError`，一律不重试）

| 形态 | 示例 | 结果 |
|:---|:---|:---|
| 非流式顶层 `error` 对象 | `{"type":"error","error":{"type":"overloaded_error","message":"Overloaded"}}` | `errorFromPayload` 映射，`StatusCode=0`，`Retryable()==false` |
| 流式 `error` 事件（带 error 体） | `{"type":"error","error":{"message":"...","code":"x"}}` | 同左（任一事件先过 `errorFromPayload`，不等同于 switch 分支） |
| 流式裸 `error` 事件 | `{"type":"error"}` | `ProtocolError{Message:"upstream stream error", StatusCode:0}` |
| 解码失败 | 空 body / 非法 JSON | 普通 error（`response body is required` / `decode response: ...`） |

## 6. 边缘项与容错清单（与单测一一对应）

| # | 边缘项 | 适配器行为 | 单测锚点 |
|:---|:---|:---|:---|
| 1 | 默认变体请求体：`max_tokens` 恒在、非流式不发 `stream`、无 tools | 字段存在性 + 缺省 4096 | `TestAnthropicBuildRequestDefaultVariant` |
| 2 | minimax 变体：字段名 `maxTokens`；响应 `stopReason` camelCase；`temperature` 原样下发 | 变体构造期固化 | `TestAnthropicBuildRequestMiniMaxVariant` |
| 3 | 工具声明 `input_schema` / 无参工具补 `{"type":"object"}` / `tool_choice=none` 不下发 | 逐字段断言 | `TestAnthropicBuildRequestToolsAndToolResults` |
| 4 | 历史工具往返：assistant `tool_use` 块 + `tool` 角色 → `user` + `tool_result` | `tool_use_id` 配对，无 name 反查 | `TestAnthropicBuildRequestToolsAndToolResults` |
| 5 | 鉴权头：`anthropic-version` 恒在、`x-api-key` 按 key 存在性、附加头覆盖 | — | `TestAnthropicBuildHeaders` |
| 6 | 非流式解析：text 全拼接 / thinking / tool_use（`input` → JSON 串）；未知块忽略 | 空 content → 空串 | `TestAnthropicProcessResponse` |
| 7 | 非流式带内错误映射 | 不重试 | `TestAnthropicHandleResponseNonStream` |
| 8 | 流式：text / thinking / `input_json_delta` 累积 / `message_delta` 结束原因 | 事件体 `type` 优先 | `TestAnthropicHandleResponseStream` |
| 9 | `IsReasoningModel`（thinking / reasoning 标记）与默认 endpoint 回退 | 普通 claude / minimax 不抑制 | `TestAnthropicIsReasoningModelAndEndpoint` |
| 10 | 跨层对照（service 目录）：同桩服务下与旧 `MiniMaxProvider` 逐项比对、开关开启分派 | 路径 / 鉴权头 / 请求体字段 / 返回文本 | `TestAnthropicAdapterMatchesLegacyMiniMaxProvider`、`TestRegistryBuildProviderUsesAnthropicAdapterForMiniMax`、`TestNewProviderFromConfigSwitchOnUsesAnthropicAdapterForMiniMax`、`TestProtocolProviderVariantDefaults`、`TestAnthropicAdapterStreamingWithTools` |

## 7. 决策 / 差异索引（§4.3 PA-1）

| 编号 | 差异 / 决策 | 本规格落点 |
|:---|:---|:---|
| PA-1 #1 | 请求体 JSON 键序不同（map 序列化按字典序） | §2.1（无线上语义影响，对照测试按字段集合锁定） |
| PA-1 #2 | 支持流式 + 工具调用（旧 `MiniMaxProvider` 仅非流式 `Chat`） | §1.2、§4 |
| PA-1 #3 | 非流式正文拼接全部 text 块；无 text 块返回空串（旧分支取首块 / 报错） | §3 |
| PA-1 #4 | 客户端超时由调用方 ctx 控制（旧分支 120s 硬超时） | §2.1（适配器不设超时；部署侧经 `HTTPClient` 注入） |
| PA-1 #5 | `model` 覆盖只对本次调用生效（并发安全修正） | §2.1（`cfg.Model` 逐请求读取，无实例写入） |
| PA-1 #6 | `temperature` 默认值按变体解析：minimax 1.0 / 官方 0.3 | §2.1、§2.6（`protocolProviderVariantTemperature`，service 层） |

## 8. 测试索引

| 文件 | 用例 | 覆盖点 |
|:---|:---|:---|
| `anthropic_messages_test.go` | `TestAnthropicBuildRequestDefaultVariant` | 默认变体请求体字段与缺省 `max_tokens` |
| | `TestAnthropicBuildRequestMiniMaxVariant` | minimax `maxTokens` 字段与 camelCase 口径 |
| | `TestAnthropicBuildRequestToolsAndToolResults` | 工具声明 / `tool_choice` / 工具结果回填 |
| | `TestAnthropicBuildHeaders` | `x-api-key` + `anthropic-version` + 覆盖 |
| | `TestAnthropicProcessResponse` | text / thinking / tool_use 块抽取 |
| | `TestAnthropicHandleResponseNonStream` | 非流式解码与带内错误 |
| | `TestAnthropicHandleResponseStream` | 流式三类增量与结束原因 |
| | `TestAnthropicIsReasoningModelAndEndpoint` | 推理模型判定与默认端点 |
| `service/llm_anthropic_equivalence_test.go` | `TestAnthropicAdapterMatchesLegacyMiniMaxProvider` | 同桩服务逐字段等价（P0 基线锁） |
| | `TestRegistryBuildProviderUsesAnthropicAdapterForMiniMax` | 注册表承载分派 |
| | `TestNewProviderFromConfigSwitchOnUsesAnthropicAdapterForMiniMax` | 开关开启侧承载切换 |
| | `TestProtocolProviderVariantDefaults` | 变体默认值（temperature 等） |
| | `TestAnthropicAdapterStreamingWithTools` | 流式 + 工具（能力增益） |

## 存疑 / 待澄清

1. **字符串 `tool_choice` 原样透传**：`anthropicToolChoice` 对非 map 值直接返回，`"auto"` 会以字符串形态写入请求体；Anthropic 官方要求对象形态 `{"type":"auto"}`，该路径存在上游 400 风险。现有用例覆盖了 map 形态与 `none`，未见字符串形态用例。
2. **`type == "function"` 分支的 name 覆盖**：`function` 子对象存在时无条件用 `function.name` 覆盖顶层 `name`，子对象 name 为空会产出 `{"type":"tool","name":""}`（空名 tool_choice），未做非空校验。
3. **`none` 无语义落点**：`tool_choice.type == "none"` 归一为 `nil` → 不下发；Anthropic 无对应「禁止调用工具」形态，调用方的禁用意图在此协议下无法表达（与 Gemini 的 `NONE` 模式不同）。
4. **role 判定不做归一**：`buildAnthropicMessages` 用精确比较 `Role == "system"` / `Role == "tool"`；`"System"`、`" system "` 之类不会命中提取/回填分支，会以原样 role 进入 messages（上游可能报错）；同时任何携带 `ToolCallID` 的非 tool 消息都会被改写成 `user`+`tool_result` 项。
5. **minimax 变体差异范围**：适配器内仅 `maxTokens` 字段名一处差异；`temperature` 默认 1.0、`system` 顶层键名等口径由 service 层变体默认值表承担（§4.4、PA-1 #6），本规格按此记录。
