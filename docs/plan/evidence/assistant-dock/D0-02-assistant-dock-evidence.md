# D0-02 AssistantDock 常驻容器 · 交付证据

- 日期：2026-10-18
- 任务：D0-02（方案 §7 P0 / §3.1 基础形态）
- 状态：`unit_verified`（浏览器级全栈 E2E 归联调）
- 变更文件：
  - 新增 `src/components/ai/dock/AssistantDock.tsx`（FAB ↔ 面板 + 快捷键 + 移动端）
  - 新增 `src/components/ai/dock/AssistantChatPanel.tsx`（紧凑聊天视图，复用 chat-core）
  - 新增 `src/lib/store/assistant-store.ts`（open/width/lastRoute，zustand persist）
  - 新增 `src/components/ai/dock/__tests__/{AssistantDock,AssistantChatPanel}.test.tsx`（6 例）
  - 挂载 `src/layouts/MainLayout.tsx`（`<AssistantDock />`，RequireAuth 布局内全域可用）
  - `src/components/common/GlobalShortcutProvider.tsx`（快捷键帮助新增「ctrl+i / cmd+i 唤起/收起 AI 助手」）
  - `jest.config.js`（transformIgnorePatterns 白名单新增 `react-hotkeys-hook`，使容器可被真实渲染测试）

## 1. 交付内容

1. **常驻入口**：右下角 FAB（48px 圆形，zIndex 980）↔ 右侧面板（top 64 / right 0 / bottom 0，
   宽度取自持久化 store，默认 400；zIndex 981，低于 dropdown 1000 与 modal 1050）。
2. **开合与快捷键**：点击 FAB / 面板关闭按钮；`Ctrl/Cmd+I` 全局切换（输入框内同样生效）；
   `ESC` 收起（面板打开时）；快捷键帮助弹窗已登记。
3. **权限门禁**：无 `ai:read` 整组件不渲染（含 store 已展开时）。
4. **懒挂载**：面板未打开时不挂载聊天视图 → 未展开时零会话/候选请求（避免挂载于 MainLayout
   后每个页面都触发 `listConversations` / Bot 候选请求）。
5. **移动端**：<768px 面板全宽（底部抽屉形态）；桌面端右侧面板。
6. **复用 chat-core**：面板即 `useAIChatSession()` + `ChatMessageItem` 的紧凑装配（建议卡、SSE、
   工具过程、确认闭环、埋点均与工作区一致，不重复实现）。

## 2. 验证证据（可复跑）

```powershell
cd itsm-frontend
npx jest src/components/ai/dock --coverage=false --runInBand   # 2 suites / 6 tests passed
npx jest src/components/ai --coverage=false --runInBand        # 10 suites / 68 tests passed（含 D0-01 内核）
npx tsc --noEmit                                               # 0 错误
npx eslint src/components/ai/dock src/lib/store/assistant-store.ts src/layouts/MainLayout.tsx
```

覆盖行为：
- 无 `ai:read` 不渲染（含强制展开 store 的负向断言）；
- FAB 打开 → 面板 + 聊天视图出现；关闭按钮收回；未展开时聊天视图不存在（懒挂载）；
- `Ctrl+I` 唤起/收起、`ESC` 收起（经真实 `react-hotkeys-hook`）；
- 面板空态渲染建议卡、输入发送走同一 SSE 回调链并渲染回答、点击建议卡直接发送。

## 3. 边界与遗留

- **页面上下文/智能提示**归 D0-03（页面注册表）与 D0-04（`pageContext` 协议）；
- **拖拽调宽 / 收纳为边缘竖条**未实现（FAB 即收起态；store 已预留 `width`，拖拽交互随后续批次）；
- **移动端动画/安全区**未打磨（当前为全宽固定面板）；
- **全局快捷键归属**：实际 hotkey 注册在容器内（自包含），帮助弹窗仅登记条目；
  如需统一到 GlobalShortcutProvider 的注册表可在后续批次迁移；
- 提供 `assistant-store` 的 `lastRoute/setLastRoute` 预留给 D0-03 上下文感知（当前未写入）。