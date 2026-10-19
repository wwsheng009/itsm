# I0-03 快捷键与命令面板整合 · 交付证据

- 日期：2026-10-18
- 任务：I0-03（方案 I 系列：快捷键与命令面板整合（Ctrl+I；Ctrl+K 增"问 AI"区）｜验收：键盘全流程可达；不与现有快捷键冲突）
- 状态：`integration_verified`（快捷键通道 + 搜索「问 AI」区 + 面板预填；真实键盘 E2E 归联调）
- 交付物：
  - `lib/store/layout-store.ts`：`searchRequestNonce` + `requestGlobalSearch()`（Ctrl+K 的跨组件打开通道）
  - `components/common/GlobalShortcutProvider.tsx`：`openSearch` 真正接通（此前为空状态）；`ctrl+k` 增加
    `enableOnFormTags`（**助手面板输入框内 Ctrl+K 同样可达搜索**）；帮助清单文案更新「打开全局搜索（可问 AI）」
  - `components/layout/header/Header.tsx`：订阅 `searchRequestNonce` → 打开搜索弹窗（与 Header 既有 Ctrl+K
    监听幂等共存，双保险）
  - `components/layout/header/GlobalSearch.tsx`：新增**「问 AI」区**（关键词非空常驻首项；点击或 Ctrl+Enter →
    预填并打开助手面板 + `handoff` 埋点）；Enter 仍走搜索（行为不冲突）
  - `lib/store/assistant-store.ts`：`pendingPrompt` + `consumePendingPrompt`；`partialize` 仅持久化 UI 偏好
    （**pendingPrompt 不落 localStorage**）
  - `components/ai/dock/AssistantChatPanel.tsx`：消费预填（**只填不发送**）并聚焦输入框

## 1. 键盘路径（全流程可达，互不冲突）

| 按键 | 行为 | 实现 |
|---|---|---|
| Ctrl/Cmd+I | 唤起/收起 AI 助手面板 | `AssistantDock`（D0-02，已交付） |
| Ctrl/Cmd+K | 打开全局搜索（表单元素内同样生效） | `GlobalShortcutProvider` → `layout-store` → `Header` |
| 搜索内 Enter | 执行全局搜索（现状不变） | `GlobalSearch` 输入框 `onKeyDown` 普通分支 |
| 搜索内 Ctrl/Cmd+Enter | **问 AI**：关键词预填到助手面板并打开 | 同上 Ctrl 分支 → `setPendingPrompt` + `openPanel` |
| 面板内 Ctrl/Cmd+K | 从助手面板跳到全局搜索 | `enableOnFormTags`（textarea 聚焦时仍触发） |
| ESC | 收起助手面板 | `AssistantDock`（已交付） |

## 2. 验证证据（可复跑）

```powershell
cd itsm-frontend
npx jest src/lib/store src/components/common/__tests__/GlobalShortcutProvider.test.tsx "src/components/layout/header/__tests__/GlobalSearch.test.tsx" src/components/ai src/lib/ai --coverage=false --watchAll=false --runInBand
# → 25 suites / 196 tests 全绿
npx tsc --noEmit ; npx eslint src/lib/store/assistant-store.ts src/lib/store/layout-store.ts src/components/common/GlobalShortcutProvider.tsx src/components/layout/header/Header.tsx src/components/layout/header/GlobalSearch.tsx src/components/ai/dock/AssistantChatPanel.tsx …
```

关键断言：
- **store（2 例）**：`setPendingPrompt` 去空白/可清除；`consumePendingPrompt` 返回并清空；持久化 payload 仅
  `['lastRoute','open','width']`（pendingPrompt 确认不落 localStorage）；
- **快捷键（2 例）**：document 上 Ctrl+K → `searchRequestNonce` 递增；**textarea 聚焦时 Ctrl+K 同样递增**
  （enableOnFormTags 生效，覆盖"助手面板内 Ctrl+K"）；
- **搜索问 AI（3 例）**：关键词非空显示首项「问 AI：…」；点击 → `pendingPrompt` 写入 + 面板打开 + `onClose` +
  `handoff(from=global_search)` 且**不触发搜索请求**；Enter=搜索、Ctrl+Enter=问 AI；关键词为空不渲染该区；
- **面板预填（1 例）**：面板挂载即消费 `pendingPrompt` → 输入框预填、store 清空、**不自动发送**。

## 3. 既有测试问题（登记，非本批回归）

- `src/components/ticket/__tests__/TicketDetailAssignSearch.test.tsx`：4 failed（auth-store mock 缺 `getState`；
  前批已用对照实验确认，见 V0-01 证据）；
- `src/pages/(main)/tickets/types/__tests__/index.test.tsx`：1 failed / 3（权限门控下「新建类型」按钮断言；
  该目录文件 `git status` 未改动，非本批回归）——建议与上条一并治理（补齐权限 mock）。

## 4. 边界与条件项（不阻塞）

- 「问 AI」当前为**预填**（不自动发送）：与 §3.5「划词问 AI 带入面板并预填」同口径；I1-02 划词入口复用同一
  `pendingPrompt` 通道（无需新增协议）；
- 命令面板内的深度整合（Ctrl+K 在助手内直接搜索并回填结果）维持现状（全局搜索弹窗已可从面板内唤起），
  如需「搜索结果直接问 AI」再按需扩展；
- 真实浏览器键盘 E2E（Ctrl+K → Ctrl+Enter → 面板预填 → Enter 发送）归联调批。