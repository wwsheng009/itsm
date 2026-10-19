# 运行时修复批 2 · 交付证据（补登记）：会话连续性 / 证据面板折叠 / Dock 宽度与会话交接

- 日期：2026-10-19（交付提交 `cd61ce7f` / `26512d69` / `5d87a831`，2026-10-19 13:50:58–13:51:00 +0800；本文件为 v5 补任务 T5-01 补登记）
- 范围：评审快照 `29edea81`（13:16）之后提交的运行时修复批 2（评审 §6「在途改动快照」/ §7-E 的收口）
- 状态：`unit + integration_verified`（代码审阅 + 定向测试实跑；浏览器级 flow / e2e 未在本轮复跑，见 §5）
- 补登记说明：三提交交付时均未附证据、未更新 §7（评审 §2.1–2.2）；本文件按方案 §14.2 T5-01 补登记。

## 1. `cd61ce7f` 流式会话回放历史消息 + 稳定前缀（多轮「失忆」修复 · 后端）

- 根因：ChatStream 仅把当前 query 送入 LLM，conversationId 只用于落库 / 工具归属 → 模型每轮失忆。
- 交付（7 文件，+674/−17）：
  - `handlers/ai/chat_history.go`：租户 + 用户归属校验（fail-closed）、assistant 落库信封确定性解包、块式水位淘汰、`created_at + id` 稳定排序；
  - `handlers/ai/service.go` / `repository_impl.go`：链路接线；
  - `service/rag_service.go`：新增 `WithHistory`，固定 `[system → history → current]` 布局；工具面按名排序（保证 provider 前缀缓存可命中）。
- 验证（`itsm-backend/`，2026-10-19 实跑）：

```
go test -v -count=1 -run 'TestBuildChatHistory|TestLoadChatHistory|TestChatMessagesWithHistory|TestSortedToolFace|TestChatStreamReplaysStableConversationHistory' ./handlers/ai ./service ./tests/botintegration
```

  → **13/13 PASS**（handlers/ai 9 / service 3 / botintegration 1）。其中 `TestChatStreamReplaysStableConversationHistory` 走真实 ChatStream 链路（真实 ent/SQLite + 真实 RAG + 捕获型 LLM 替身），断言第二轮请求带上第一轮 user/assistant 消息、且同会话消息序列为稳定前缀（append-only）。
- 备注：本机并行链接 3 个测试二进制时 msys2 ld 报 `memory exhausted`（环境限制，非代码问题），以 `-p 1` 串行链接后全部通过。

## 2. `26512d69` 过程证据面板默认折叠可展开（前端）

- 交付（6 文件，+245/−129）：
  - `components/ai/evidence-panel.tsx`：标题行改为开关（`role="button"` / `aria-expanded` / Enter/Space），**默认折叠**并保留「步骤 N · 工具调用 M」计数；加载错误不随折叠隐藏；
  - `components/ai/tool-call-timeline.tsx`（旧后端回退路径）同步默认折叠，保留条目级展开；
  - `tests/e2e/flows/flow-bot-workspace.spec.ts` / `flow-mcp-chat-chain.spec.ts`：先展开再断言工具明细。
- 代码审阅（工作区现状）：`useState(false)` 默认折叠 + `data-testid="evidence-toggle"` / `"tool-call-timeline-toggle"` 稳定定位（evidence-panel.tsx L85–107、tool-call-timeline.tsx L150–190）。
- 验证：`evidence-panel.test.tsx` + `tool-call-timeline.test.tsx` PASS（计入 §4 的 6 suites / 72 tests）。

## 3. `5d87a831` Dock 宽度三档 + 会话历史 + 工作区一次性交接（前端）

- 交付（10 文件，+776/−35）：
  - **宽度三档** `ASSISTANT_WIDTH_TIERS = [400, 560, 720]`（assistant-store.ts L21）+ 持久化 + 标题栏「加宽」循环按钮（键盘可达，aria-label 带当前宽度）+ 视口收敛 `Math.min(720, viewport-320)` + ≥560px 推挤内容区；
  - **设计决策（显式）**：以「离散三档 + 键盘循环」替代自由拖拽——档位可持久化 / 可测试 / 无纯指针操作（满足 WCAG 2.1.1）；方案条款回写见 T5-11；
  - `/ai/chat` 下不渲染 Dock（工作区抑制）；**一次性交接**：Dock → 工作区传递 convId + 页面 scope（target/summary），工作区 `useAIChatSession({ consumeWorkspaceHandoff: true })` 消费（use-ai-chat-session.ts L59/L309、AIChat.tsx L161）；
  - 会话历史下拉：切换 / 搜索 / 导出 / 删除 / 新建（复用 chat-core 动作）。
- 验证：`AssistantDock.test.tsx` / `AssistantChatPanel.test.tsx` / `use-ai-chat-session.test.tsx` / `assistant-store.test.ts` PASS——含「在工作区打开写入交接、关闭面板并导航」「交接带 L2 目标上下文」「历史下拉切换 / 新建 / 搜索 / 导出与删除」等用例。

## 4. 验证汇总（2026-10-19 补登记实跑）

- 前端定向 6 文件（上列各提交测试文件）：**6 suites / 72 tests 全过（0 失败，约 14.3s）**；
- 前端相关表面全量（`src/lib/ai/` + `src/components/ai/` + `src/lib/store/`）：**54 suites / 417 tests 全过（0 失败）**；
- 后端（见 §1）：**13/13 PASS**。

## 5. 局限与后续

- 本轮为**补登记**：静态审阅 + 单测 / 组件测 / 集成测实跑；未复跑 Playwright e2e（含 `26512d69` 同步的两条规格）与浏览器级 flow；如需 `flow_verified` 口径随联调窗口补记。
- 本批含「工作区抑制 + 一次性交接」实现；与方案 §3.7 L119「双入口并存」条款的差异属 §14.4-① 待决策（T5-12），不影响本补登记。
- 登记：交付台账 §E/§G/§H 与 T5 段；方案 §14.2 T5-01。
