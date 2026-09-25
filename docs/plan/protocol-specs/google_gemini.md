# google_gemini 协议规格（parser spec）

> 文档定位：本规格对应适配器文件 `itsm-backend/internal/llm/protocol/google_gemini.go`（注册项与默认端点见 `registry.go`），口径索引 §4.3 PA-4。
> 口径来源：`docs/plan/llm-protocol-adapter-plan.md` §4.2、§4.3 PA-4（8 条）、§4.4、§4.5。字段名 / 函数名 / 用例名以源码为准。

## 1. 元数据

### 1.1 协议与变体

| 项 | 值 | 源码坐标 |
|:---|:---|:---|
| 协议枚举 | `google_gemini`（`ProtocolGoogleGemini`） | `adapter.go` 常量区 |
| 承载实现 | `GoogleGeminiAdapter{variant string}`（无状态，可并发复用） | `google_gemini.go:36-43` |
| 变体白名单 | 仅 `""`（`VariantDefault`）；`variant` 经 `normalizeProtocolName` 归一 | `registry.go:105-108` |
| 构造期选项 | `NewGoogleGeminiAdapter(variant)`；当前**无事实差异**（`a.variant` 除赋值外无引用，见「存疑」第 1 条） | `google_gemini.go:41-43` |
| 资源路径（静态回退） | `/v1beta`（`GetAPIPath`）；模型为空时由 `APIPathFor` 直接回退该值，不构造半成品路径 | `google_gemini.go:50,57-60` |
| 资源路径（动态，`ModelPathAdapter`） | 非流式：`/v1beta/models/{model}:generateContent`；流式：`/v1beta/models/{model}:streamGenerateContent?alt=sse` | `google_gemini.go:52-66` |
| 模型名归一 | `geminiModelName`：`TrimSpace` + 去单个 `models/` 前缀 + 再次 `TrimSpace`（两种写法都接受） | `google_gemini.go:87-93` |
| 默认端点 | `https://generativelanguage.googleapis.com`（协议包登记；endpoint 已带 `/v1beta` 时由 service 只补资源段） | `registry.go:139-140`、`google_gemini.go:28-29` |
| 鉴权头 | `x-goog-api-key: <apiKey>`（去空白后非空才下发）；**决策：不复刻 `?key=` 查询参数形态**（密钥不进 URL / 代理日志） | `google_gemini.go:76-85`、PA-4 #1 |
| 其他头 | `Content-Type: application/json`；调用方 headers 大小写不敏感覆盖 | 同上 |
| 能力位（PA-5） | 开关关闭：三项 ✓（适配器为唯一承载，无旧分支）；开关开启：三项全真（不变） | §4.5 |

## 2. 请求构建

### 2.1 请求体字段下发规则

| 字段 | 下发条件 | 值口径 |
|:---|:---|:---|
| `contents` | 恒下发 | 项数组 `{role, parts[]}`；无消息时为空数组 |
| `systemInstruction` | 前导 system 拼接结果非空 | `{"parts":[{"text": <拼接文本>}]}`（`\n\n` 连接） |
| `generationConfig` | 内部字段集非空才下发 | 见 §2.6；为空则整键省略 |
| `generationConfig.maxOutputTokens` | 仅 `cfg.MaxTokens > 0` | 接线层缺省 4096（§4.4） |
| `generationConfig.temperature` | 仅 `!cfg.ReasoningModel` | 缺省 0.3 由 service 层解析；`IsReasoningModel` 恒 false（见「存疑」第 2 条） |
| `tools` | 合并去重后声明非空 | `[{"functionDeclarations":[...]}]` |
| `toolConfig` | `cfg.ToolChoice != nil` 且归一结果非 nil | 见 §2.4 |
| 不下发项 | — | `topP` / `topK` / `candidateCount` 等参考实现独有默认值**不下发**（PA-4 #6） |

### 2.2 会话项映射表（内部消息 → `contents[]`）

| 内部消息 | 线上形态 | 关键字段 |
|:---|:---|:---|
| 前导 `system` | **不进入 contents** | 文本 trim 后收集，`\n\n` 连接为顶层 `systemInstruction` |
| 非前导 `system` | `{"role":"user","parts":[{"text":...}]}` | 文本 trim 后非空才下发；空文本整项跳过 |
| `tool` / `function` | `{"role":"user","parts":[{"functionResponse":{...}}]}` | `name` 由会话历史反查；`response` 固定为 JSON 对象（§2.5） |
| `assistant` / `model` | `{"role":"model","parts":[...]}` | `text`（trim 非空）+ 每条 `functionCall{name,args}`（name 为空跳过）；parts 为空则整项跳过 |
| 其它（默认，含 `user`） | `{"role":"user","parts":[{"text":...}]}` | 文本 trim 后非空才下发 |

- 角色判定：`strings.ToLower(strings.TrimSpace(role))`；出现首个非 system 消息后前导标记关闭。
- Gemini 的 `contents[].role` 只有 `user` / `model` 两种（PA-4 #4），适配器不下发其它角色。

### 2.3 system 提升与降级

| 位置 | 规则 |
|:---|:---|
| 前导 system | 全部提升合并为顶层 `systemInstruction`；每段 `TrimSpace`，空段跳过 |
| 非前导 system | **降级为 `user` 文本项**（Gemini 无 system 角色）；trim 后为空则跳过 |

### 2.4 工具声明与 `tool_choice` 归一

- 声明形态：`tools[].functionDeclarations[]` = `{name, parameters, description?}`；`Parameters == nil` 回填 `{"type":"object","properties":{}}`（**含** properties，与 anthropic 的 `{"type":"object"}` 不同）；description 取 trim 后非空值。
- 合并与去重：消息携带的 `Message.Tools` 在前、`RequestConfig.Tools` 在后；按 `name` **首次出现优先**（`seen` 去重），空 name 跳过（与 chat / responses / anthropic 的「不去重」不同）。
- `tool_choice` 归一（`geminiToolConfig`）：

| 输入形态 | 输出（`toolConfig`） |
|:---|:---|
| `"auto"` | `{"functionCallingConfig":{"mode":"AUTO"}}` |
| `"none"` | `{"functionCallingConfig":{"mode":"NONE"}}` |
| `"required"` / `"any"`（大小写、空白不敏感） | `{"functionCallingConfig":{"mode":"ANY"}}` |
| `{"functionCallingConfig":{...}}`（非空对象） | **原样透传**（Gemini 原生形态） |
| `{"function":{"name":"x"}}`（**不校验 type**） | `{"functionCallingConfig":{"mode":"ANY","allowedFunctionNames":["x"]}}`；name 为空 → 不下发 |
| `{"mode":"ANY"}`（简写） | `{"functionCallingConfig":{"mode":"ANY"}}` |
| 不可识别字符串 / 对象 / nil | 不下发 `toolConfig` |

### 2.5 工具结果回填（函数名反查 + 缺失占位）

| 环节 | 规则 |
|:---|:---|
| 函数名反查 | `collectGeminiToolNames` 建立 `ToolCallID → name` 索引（遍历全部消息的 `ToolCalls`；**后者覆盖同 id**）；`ToolCallID` trim 后查表 |
| 缺失占位 | 反查不到或为空 → 占位名 `"tool"`（`geminiFunctionResponseFallbackName`），保证请求结构合法 |
| `response` 构造 | `Content` trim 非空 → 尝试 `json.Unmarshal` 为对象；非法或 nil → `{"result": <原文>}` |
| 空结果 | 内容为空或对象为空 → `{"result":""}` |
| 线上形态 | `{"functionResponse":{"name":<name>,"response":<对象>}}`，挂在 `role:"user"` 的 parts 中 |

### 2.6 采样参数与推理模型抑制

| 规则 | 口径 |
|:---|:---|
| `generationConfig` | 由 `maxOutputTokens` / `temperature` 两键组成；两键皆不下发时整键省略（如 `MaxTokens<=0` 且 `ReasoningModel=true`） |
| `maxOutputTokens` | 仅 `> 0` 下发（接线层缺省 4096） |
| `temperature` | `cfg.ReasoningModel == true` 时省略；否则恒下发（含 0 值） |
| `IsReasoningModel` | **恒 `false`**：Gemini 请求侧没有与 OpenAI 推理模型等价的「拒绝采样参数」约束；思考能力由模型自身决定，响应侧 `thought=true` part 归入推理链（PA-4 #6、§4.4） |

## 3. 非流式响应解析

| 抽取项 | 规则 | 容错 |
|:---|:---|:---|
| 候选选择 | 仅 `candidates[0]`（网关按单候选调用）；额外候选忽略 | 无 `candidates` / 首项非对象 → 零值 `ProcessResult{}` |
| 结束原因 | `candidates[0].finishReason` **原生值透传**（`STOP` / `MAX_TOKENS` / `SAFETY` / `RECITATION`…），不做值域映射 | 键缺失 / 非字符串 → 空串 |
| 正文 `Content` | `content.parts[]` 中无 `functionCall` 且 `thought != true` 的 `text` 按序拼接 | 非对象 part 跳过；空 text 跳过 |
| 推理链 `Reasoning` | `thought == true` 的 `text` 按序拼接（Gemini 2.5+/3.x 思考 part） | 同上 |
| 工具调用 `ToolCalls` | `functionCall` 完整对象：`name` trim 非空才产出；`ID` 由适配器生成 `call_N`（N = 当前已累积条数 + 1）；`Arguments` 归一为 JSON 对象串 | `name` 为空 → 丢弃该 part；`args` 为 nil / 空串 / 非法 → `"{}"`；对象 → `json.Marshal`；字符串 → trim 后原样 |
| part 优先级 | `functionCall` 存在时**直接返回**，同 part 的 `text` 不再消费 | — |
| 安全拦截 | `promptFeedback.blockReason` → `*ProtocolError{Code:<blockReason>, Message:"prompt blocked: <blockReason>"}` | 拦截判定在 `HandleResponse` 层（见「存疑」第 5 条） |
| 未知字段 | 忽略（额外候选、内置工具产物、其它顶层键） | PA-4 #8 容错口径 |

## 4. 流式事件序列（`:streamGenerateContent?alt=sse`）

| SSE 帧 / 字段 | 语义 | 处理动作 |
|:---|:---|:---|
| 空 `data` | 心跳 / 分隔 | 跳过 |
| `data: [DONE]` | 非数据哨兵 | **跳过继续读**（不终止；终点由 EOF 决定） |
| `candidates[0].content.parts[].text`（`thought != true`） | 正文增量 | 追加 `Content` + `EmitText` |
| `parts[].text` 且 `thought == true` | 推理增量 | 追加 `Reasoning` + `EmitReasoning` |
| `parts[].functionCall` | 工具调用 | 完整对象，**无跨帧参数拼接**（与 OpenAI 的 `arguments` 分片不同）；直接追加 `ToolCall`，`ID = call_N` 按序生成 |
| `candidates[0].finishReason` | 结束原因 | **首个非空值生效**，后续非空值忽略（与 chat 的「后帧覆盖」不同） |
| 顶层 `error` | 带内错误 | `errorFromPayload` → `*ProtocolError{StatusCode:0}`，立即返回（不重试） |
| `promptFeedback.blockReason` | 安全拦截 | `geminiPromptBlockedError` → `*ProtocolError{Code:<blockReason>}`，立即返回 |
| 非法 JSON 的 `data` | 坏帧 | 立即返回 `decode stream chunk` 错误 |
| 注释行（`:` 前缀）/ 无 `data` 帧 / `event:` 字段 | keep-alive | `scanSSEFrames` 忽略注释行；无 `data` 帧不派发；事件名不使用 |
| 非流式复用 | — | 非流式与流式共用 `geminiConsumeChunk` + `state.result()`（非流式 = 单帧消费） |
| 累积输出 | — | `ToolCalls` 为空时保持 `nil`（与其它适配器一致） |

## 5. 错误语义

### 5.1 HTTP 层错误（`NewHTTPError`）

| 项 | 口径 |
|:---|:---|
| 触发 | service 侧对非 2xx HTTP 响应调用 `protocol.NewHTTPError(ProtocolGoogleGemini, resp, body)` |
| 重试 | `Retryable()`：`429` 或 `>=500` → `true`；其余 4xx → `false`（口径与其它协议一致） |

### 5.2 带内错误（`*ProtocolError{StatusCode:0}`，一律不重试）

| 形态 | 示例 | 结果 |
|:---|:---|:---|
| 顶层 `error` 对象 | `{"error":{"code":400,"message":"API key not valid","status":"INVALID_ARGUMENT"}}` | `errorFromPayload` 映射（非流式与流式同口径） |
| `promptFeedback.blockReason` | `{"promptFeedback":{"blockReason":"SAFETY"}}` | `ProtocolError{Code:"SAFETY", Message:"prompt blocked: SAFETY"}` |
| 流内 `error` 帧 | 同顶层 `error` 形态 | 立刻终止读取并返回错误 |
| 解码失败 | 空 body / 非法 JSON | 普通 error（`response body is required` / `decode response: ...`），非 `ProtocolError` |

## 6. 边缘项与容错清单（与单测一一对应）

| # | 边缘项 | 适配器行为 | 单测锚点 |
|:---|:---|:---|:---|
| 1 | 元数据：`Name()` / 静态回退 `/v1beta` / `APIPathFor` 双形态 / 模型名 `models/` 前缀归一 | 流式含 `?alt=sse`；空模型回退版本段 | `TestGoogleGeminiAdapterMetadata` |
| 2 | contents 角色映射：前导 system → `systemInstruction`；assistant → `model`（text + functionCall）；tool → `user` + `functionResponse`（name 反查 / 缺失占位 `tool`） | 逐字段断言 | `TestGoogleGeminiBuildRequestContents` |
| 3 | 边缘：非前导 system 降级 user、空文本跳过、无参工具补 schema、`args` 字符串兼容、`MaxTokens<=0` 且 `ReasoningModel=true` → 无 `generationConfig` | 字段集合不多发 | `TestGoogleGeminiBuildRequestEdgeCases` |
| 4 | `toolConfig` 归一矩阵（10 个子用例：`auto` / `required` / ` any ` / `none` / openai chat 风格 / gemini 原生形态透传 / mode 简写 / 不可识别字符串 / 不可识别对象 / nil） | 不可识别一律不下发 | `TestGoogleGeminiBuildRequestToolConfig` |
| 5 | 非流式：正文拼接 / thought 推理链 / functionCall 无 id 生成 `call_N` / `promptFeedback` 拦截 | 未知字段忽略 | `TestGoogleGeminiHandleResponseNonStream` |
| 6 | 流式：多帧增量顺序 + 工具调用 + `[DONE]` / 注释行容错 | `finishReason` 首个非空 | `TestGoogleGeminiHandleResponseStream` |
| 7 | 流内失败：`error` 帧与 `blockReason` 均映射带内错误且不重试 | `StatusCode=0` | `TestGoogleGeminiStreamInBandErrors` |

## 7. 决策 / 差异索引（§4.3 PA-4）

| 编号 | 决策 / 差异 | 本规格落点 |
|:---|:---|:---|
| PA-4 #1 | 官方 Gemini API v1beta；鉴权固定 `x-goog-api-key`，不复刻 `?key=` | §1.1、§5.1 |
| PA-4 #2 | 新增 `ModelPathAdapter.APIPathFor`；`GetAPIPath()` 保留 `/v1beta` 静态回退 | §1.1 |
| PA-4 #3 | 缺省 endpoint 登记官方地址；自填端点资源路径仍由适配器给出 | §1.1 |
| PA-4 #4 | 会话归一：前导 system → `systemInstruction`；非前导降级 user；assistant → model；工具结果 → user + `functionResponse`（name 反查 / 缺失占位） | §2.2、§2.3、§2.5 |
| PA-4 #5 | 工具声明 `functionDeclarations`；响应侧 `functionCall` 无 id → 按序生成 `call_N`；`args` 对象/字符串兼容 | §2.4、§3 |
| PA-4 #6 | 采样：`maxOutputTokens` / `temperature`；`IsReasoningModel` 恒 false；`topP` 等不下发 | §2.1、§2.6 |
| PA-4 #7 | 流式 `alt=sse`；`finishReason` 原生透传；`error` 帧与 `blockReason` → 带内错误；`[DONE]` 与注释行忽略 | §4、§5.2 |
| PA-4 #8 | 响应 `candidates[0].content.parts[].text` 按序拼接（`thought=true` → 推理链）；多候选 / 未知字段忽略；非流式与流式共用归一 | §3、§4 |

## 8. 测试索引（`google_gemini_test.go`）

| 用例 | 覆盖点 |
|:---|:---|
| `TestGoogleGeminiAdapterMetadata` | `Name()` / `GetAPIPath()`（`/v1beta` 回退）/ `APIPathFor` 非流式与流式 / `models/` 前缀归一 |
| `TestGoogleGeminiBuildRequestContents` | contents 角色映射、`systemInstruction`、model parts、`functionResponse` 反查与占位 |
| `TestGoogleGeminiBuildRequestEdgeCases` | 非前导 system、空文本、无参工具 schema、`args` 字符串、`generationConfig` 省略 |
| `TestGoogleGeminiBuildRequestToolConfig` | `toolConfig` 归一矩阵（10 个子用例，名称见 §6 第 4 条） |
| `TestGoogleGeminiHandleResponseNonStream` | 非流式正文 / 推理链 / 工具调用 / 拦截 |
| `TestGoogleGeminiHandleResponseStream` | 流式增量、工具调用、`[DONE]` 与注释行 |
| `TestGoogleGeminiStreamInBandErrors` | `error` 帧 / `promptFeedback.blockReason` 带内错误 |

## 存疑 / 待澄清

1. **`variant` 字段存而未用**：`GoogleGeminiAdapter.variant` 仅在构造时赋值，全文件无其它引用；`New` 忽略差异的注释（「标准形态只有一个变体」）与「变体差异在构造期固化」的通用表述之间暂无事实差距，但该字段目前是死字段，后续引入非标准变体时需补齐语义。
2. **`IsReasoningModel` 恒 false 与 `cfg.ReasoningModel` 抑制分支并存**：`BuildRequest` 保留 `if !cfg.ReasoningModel` 分支，`TestGoogleGeminiBuildRequestEdgeCases` 以显式 `ReasoningModel=true` 锁定「AI 不产生 `generationConfig`」；若接线层误置该标志，会出现与「Gemini 请求侧恒下发 temperature」口径不同的线上行为。二者关系建议在计划文档中明确（当前仅由单测锚定）。
3. **`geminiToolConfig` 的 function 分支不校验 `type`**：任意含非空 `function.name` 的对象都会归一到 `ANY` + `allowedFunctionNames`；不可识别对象则静默不下发，未把「形态可疑」上报为错误。
4. **`finishReason` 取首个非空**：多帧携带不同 `finishReason` 时以第一帧为准（chat 为「后帧覆盖」、anthropic 为「覆盖」）；若上游首帧给 `STOP` 后帧追加工具调用结束语义，适配器不会更新。
5. **非流式拦截的调用层级**：`promptFeedback.blockReason` 判定在 `HandleResponse` 内、非 `ProcessResponse` 内；直接调用 `ProcessResponse(map)` 的调用方不会得到拦截错误（`ProtocolAdapter` 契约入口为 `HandleResponse`，风险有限）。
6. **`[DONE]` 语义为「跳过」而非「终止」**：与 chat 适配器的终止语义不同；若上游在 `[DONE]` 之后继续发送数据帧，Gemini 适配器会继续消费。
