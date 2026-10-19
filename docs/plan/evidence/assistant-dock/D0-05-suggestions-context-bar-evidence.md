# D0-05（完整）页面级建议卡与上下文条 · 交付证据

- 日期：2026-10-18
- 任务：D0-05（方案 §7 P0 / §3.3 任务卡 / §3.1 面板结构）
- 状态：`unit_verified`（浏览器级全栈 E2E 归联调）
- 变更文件：
  - 新增 `src/components/ai/chat-core/SuggestionCards.tsx`（页面级建议卡：注册表驱动 + 展示/点击埋点 + 通用回退）
  - `src/components/ai/dock/AssistantChatPanel.tsx`（空态改用建议卡，compact 形态）
  - `src/components/ai/AIChat.tsx`（工作区空态改用建议卡，页面注册为 `ai.chat`）
  - `src/components/ai/dock/AssistantDock.tsx`（面板头部上下文条：当前页面标题 chip）
  - 测试：`AssistantChatPanel.test.tsx`（+1 例：注册路由声明卡片 + 点击发送 + `suggestion_clicked` 埋点断言）、
    `AssistantDock.test.tsx`（+1 断言：上下文条）

## 1. 交付内容

1. **建议卡组件（共用）**：
   - 数据源：D0-03 页面注册表（`task` 任务卡 / `question` 问题卡）；未注册路由回退 `chat-core` 通用建议；
   - 交互：点击即发送 `prompt`（任务卡全部只读，写动作仍走对话确认闭环）；`disabled` 随流式态；
   - 埋点：`suggestion_shown`（随页面每挂载一轮，面板/工作区均为懒挂载 → 无后台噪声）与
     `suggestion_clicked`（carry `pageKey/cardId/cardKind/rank`）——满足"建议点击率可观测"验收；
   - 形态：工作区两列（中间态）/ Dock 面板单列 compact。
2. **上下文条**：Dock 面板头部展示当前页面标题 chip（来自注册表声明；`data-testid="assistant-context-chip"`），
   与 D0-03 的 `data-page-key` 一起构成面板的"我在哪一页"表达；工作区保留既有入口上下文条（B3-02 scope chip）。
3. **与既有埋点的衔接**：D0-05 子集交付的 `chat-telemetry`（会话级事件）继续生效；
   建议卡事件直接使用统一 SDK（`assistant_opened` 类事件由 D0-02 容器/后续接入）。

## 2. 验证证据（可复跑）

```powershell
cd itsm-frontend
npx jest src/components/ai/dock src/components/ai/chat-core --coverage=false --runInBand  # 3 suites / 13 tests
npx jest src/components/ai src/lib/ai --coverage=false --runInBand                        # 15 suites / 113 tests
npx jest src/pages/__tests__/knowledge-new-article-prefill.test.tsx --coverage=false --runInBand  # 9 tests
npx tsc --noEmit                                                                          # 0 错误
npx eslint src/components/ai/chat-core src/components/ai/dock src/components/ai/AIChat.tsx
```

关键断言：
- `/dashboard` 路由下建议卡渲染注册声明（"今天最需要关注什么？"，`suggestion-card-attention_today`）；
- 点击卡片 → `suggestion_clicked{pageKey:'dashboard',cardId:'attention_today',cardKind:'task',rank:0}` +
  发送声明 `prompt`（`aiChatStream` 收到 `query` 含"今天最需要关注"）；
- 面板打开时上下文条展示 `仪表盘`；未注册路由回退通用建议（既有用例保持通过）。

## 3. 边界与遗留

- 建议文案为静态中文（注册表口径；i18n 化归文案批次）；
- ~~`suggestion_ignored` 未采集~~ → **已闭环**（D1-02 补全：会话级"展示后未点击"结算，2026-10-18；衰减与参与打分见 O2-01 补全）；
- 上下文条目前展示页面标题；携带 target/summary 的富上下文条（页面显式注入）归 D1-02 context provider；
- 建议卡视觉为文本按钮（任务卡 title 提示"只读"）；图标/分组等视觉增强归 UI 打磨批次。