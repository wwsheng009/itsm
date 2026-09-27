# M1-04 证据（AIChat 工具调用时间线）

> 验收项：A1-04（对应任务 M1-04，与 M1-10 联调）｜DoD：`flow_verified`
> 实际状态：**`integration_verified`**（组件级：数据配对/降级/安全/渲染全绿；`flow_verified` 待 M1-10 —— 真实浏览器中「模型触发工具 → SSE 事件 → 时间线渲染」的端到端联调，E2E 归 M2-05）
> 执行人 / 环境：AI 辅助执行；Windows + pwsh，jest（jsdom + @testing-library/react），基线 `2eaf2fac`（分支 `feat/bot-mcp-integration`）
> 时间：2026-09-27

## 交付物

| 文件 | 变更 |
| --- | --- |
| `itsm-frontend/src/components/ai/tool-call-timeline.tsx`（新增） | `mergeToolEvents`（纯函数：事件配对）+ `ToolCallTimeline`（折叠块渲染：来源徽标 / 工具名 / 状态 / 耗时 / 写操作标记 / 脱敏摘要 / 错误码 / 待审批提示） |
| `itsm-frontend/src/components/ai/AIChat.tsx` | `ChatMessage.toolEvents` 字段、`appendToolEvent`（含 50 条上限兜底）、`aiChatStream` 的 `onToolEvent` 接线、消息区渲染时间线（error 之后、来源之前） |
| `itsm-frontend/src/components/ai/__tests__/tool-call-timeline.test.tsx`（新增） | 8 用例：正常序列 / 失败序列 / 待审批 / 无事件降级 / 超长截断折叠 / 同名多次调用 / 事件丢失自建条目 / 纯文本渲染（XSS 载荷不入 DOM） |

## 关键设计（实现事实）

1. **数据只来自 SSE**（M1-03 事件），不额外轮询、不读审计表——审计真源仍是 `tool_invocations`。
2. **按序配对而非按名合并**：`started` 开一条记录，同工具的下一终态事件闭合「最近一条未闭合」记录。同名工具被调用两次 → 两条记录，互不覆盖（用例锁定）。
3. **事件丢失降级**：只有终态事件（缺 `started`）时自建一条记录；一条事件都没有时组件返回 `null`（无空白块），内容仍由最终消息承载。
4. **工具输出按纯文本渲染**：工具返回值是不可信输入（外部 MCP 服务器可控），**不**交给 Markdown/HTML 解析链，而用 `<pre>` + React 文本转义（用例以 `<img onerror>`/`<script>` 载荷断言 DOM 中无 `img`/`script` 元素）。
5. **长输出折叠**：默认收起；展开后 `max-height: 180px` + 滚动；摘要带 `…(truncated)` 时展示「已截断」标记（与后端 `pkg/redact.TruncatedMarker` 同字面量）。
6. **状态/来源展示**：来源徽标 `内置` / `MCP · <服务器名>`；状态标签 执行中/已完成/失败/待审批（未知状态按「进行中」展示，不隐藏）；`phase=write` 追加「写操作」标记；`pending` 展开显示 invocation 与「可在『AI 审批』页处理」。

## 与计划文本的偏差（如实登记）

| 计划表述 | 实际实现 | 原因 |
| --- | --- | --- |
| 「来源徽标 → **参数摘要（脱敏）** → 结果摘要 / 耗时 / 状态」 | 展示工具名 + 结果摘要（脱敏）+ 耗时/状态；**不展示参数摘要** | M1-03 冻结的事件契约只承载结果摘要 `summary`（计划 §M1-03 的字段集 `id/tool/provider/server/phase/status/summary/duration_ms/error_code` 不含参数）。参数维度由工具名 + 写操作标记 + 审批卡片（M1-05）承载；如需参数级摘要须走契约变更评审 |
| 改动文件含 `lib/types/mcp.ts`、`lib/i18n/translations.ts` | 未新增这两个文件；类型定义随组件/`ai-api.ts` 就近放置，文案为内联中文 | 仓库无 `lib/types/` 目录；`AIChat` 现状即内联中文（无 `useTranslation`），新增组件沿用同口径，避免同一组件内两套文案机制 |

## 执行记录

| # | 命令（workdir=`itsm-frontend`） | 结果 |
| --- | --- | --- |
| 1 | `npx jest src/components/ai/__tests__/tool-call-timeline.test.tsx` | **8/8 passed**（命令退出码 1 仅来自单文件运行的全局覆盖率阈值告警，非测试失败） |
| 2 | `npx tsc --noEmit` | exit 0 |

## 未覆盖 / 已知缺口

1. **真实浏览器联调**（模型真实触发工具 → SSE 事件 → 时间线渲染；含「无工具调用的普通问答不出现时间线」）：归 **M1-10**，完整 E2E 归 M2-05。
2. **历史回放**：刷新页面后旧消息不重放工具时间线（事件不落库，历史消息仅有最终内容）。这是「事件=过程信号，审计=真源」的既定取舍；如需回放，应由审计页（M1-07）按 `tool_invocations` 展示，而非把事件持久化进消息。
3. `AIChat.tsx` 的**非流式回退路径**（`AIApi.chat`）没有事件来源，时间线自然为空（降级已覆盖）。
