# D0-01 chat-core 抽离 · 交付证据

- 日期：2026-10-18
- 任务：D0-01（方案 §7 P0；Dock 与工作区共用会话内核的关键前置）
- 状态：`unit_verified`（行为等价重构；浏览器级 E2E 归 D0-02/联调）
- 变更文件：
  - 新增 `itsm-frontend/src/components/ai/chat-core/`：
    - `use-ai-chat-session.ts`（会话内核，624 行）
    - `ChatMessageItem.tsx`（消息项，含确认单埋点）
    - `constants.ts` / `types.ts` / `chat-css.ts`
    - `__tests__/use-ai-chat-session.test.tsx`（4 例内核冒烟）
  - 重写 `itsm-frontend/src/components/ai/AIChat.tsx`：**1563 → 589 行**（页面壳：整页布局锁 + 工作区 JSX）
  - 更新 `src/pages/__tests__/knowledge-new-article-prefill.test.tsx`（源码契约改为跨三处文件合并校验）

## 1. 交付内容

1. **会话内核** `useAIChatSession()`：会话列表/切换/删除、消息状态、SSE 流式（`aiChatStream` + 降级一次性回答）、
   工具事件与 v2 步骤、确认闭环、Provider 选择与个人默认、Bot 选择器、入口上下文（`ask-ai-scope`）、
   自动滚动开关、埋点（`message_sent`/`first_token`/`tool_call`/`stream_end`/`error_shown`）。
2. **消息项** `ChatMessageItem`：用户气泡 / 助手 Markdown + 引用来源 + 工具时间线 + 待审批卡片 +
   确认抽屉（`confirmation_shown/accepted/rejected` 埋点随迁）。
3. **共用常量/类型/样式**：`SUGGESTIONS`、`CONTENT_MAX_WIDTH`、`nextId`、`ChatMessage`、`buildChatCss`。
4. **页面壳** `AIChat.tsx` 仅保留：整页布局锁（页面级外壳改造，Dock 不需要）+ 工作区 JSX。

## 2. 方法（行为等价）

- 由**锚点校验的一次性转换脚本**做逐字切片（脚本与注释放 `.tmp`，不入库；锚点不匹配即中止，不猜行号）；
- 转换后仅 3 处人工修正：`export` 前缀 ×2（`ChatMessageItem`/`buildChatCss`）、
  移除内核中未使用的 `useI18n()` 调用（`t` 仅 JSX 使用）、页面补 `CONTENT_MAX_WIDTH` 导入；
- 未做任何语义改写（layout effect 按设计留在页面壳）。

## 3. 验证证据（可复跑）

```powershell
cd itsm-frontend
npx tsc --noEmit                                                   # 0 错误（结构等价的硬门禁）
npx eslint src/components/ai/AIChat.tsx src/components/ai/chat-core # 0 问题
npx jest src/components/ai --coverage=false --runInBand            # 8 suites / 62 tests passed
npx jest src/lib/ai --coverage=false --runInBand                   # 3 suites / 32 tests passed
npx jest src/pages/__tests__/knowledge-new-article-prefill.test.tsx --coverage=false --runInBand  # 9 tests passed
```

- 组件套件由 58 → **62** 例（新增 4 例内核冒烟：挂载加载会话列表、发送走 SSE 增量渲染并回填会话 id、
  流式期间 `streaming` 置位/复位、清空复位）；
- 源码契约（知识文章 prefill 防回退）改为「页面壳 + 内核 + 消息项」三处合并校验，9 例保持通过；
- 既有 AI 组件用例（ask-ai-launcher / tool-call-timeline / confirmation-drawer / evidence-panel /
  tool-approval-card / bot-selector / MarkdownMessage）全部保持绿色。

## 4. 边界与遗留

- **D0-02 Dock 尚未消费内核**（本任务只做抽离；Dock 容器归 D0-02）；
- 左侧会话历史栏仍在页面壳内（Dock 为紧凑面板、不展示历史栏；如全屏形态复用再抽 `ConversationSidebar`）；
- AIChat 页面本身无既有渲染级测试：结构等价由 `tsc` 兜底，行为等价由逐字搬运 + 内核冒烟 + 源码契约共同保证；
- 浏览器级全栈 E2E 归后续联调（D0-02/试点）。