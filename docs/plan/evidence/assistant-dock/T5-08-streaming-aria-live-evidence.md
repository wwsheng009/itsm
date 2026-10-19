# T5-08 流式内容 `aria-live` 行为验收 · 交付证据

- 任务：T5-08（方案 §14.2 v5 补任务｜验收：行为断言，不限于 axe 静态扫描）
- 来源缺口：评审 §12 第 12 项（消息区无 `role="log"/aria-live`）；UX 差距 B9/F2（屏幕阅读器无播报）
- 交付日期：2026-10-19（代码 + 测试 + 文档同批提交）

## 1. 实现（双表面，同口径）

| 表面 | 文件（消息滚动容器） | 变更 |
|---|---|---|
| 工作区 | `itsm-frontend/src/components/ai/AIChat.tsx` | `role="log"` + `aria-label="AI 助手会话消息"` + `aria-live="polite"` + `aria-busy={streaming}` |
| Dock 面板 | `itsm-frontend/src/components/ai/dock/AssistantChatPanel.tsx` | 同上 |

口径：

- 消息区为**持久 live region**（空态即存在；新增消息即可被礼貌播报，不依赖焦点）；
- 流式期间 `aria-busy="true"`：抑制逐字/逐块播报（避免读屏刷屏）；
- 生成结束 `aria-busy="false"`：由读屏一次性读出完整回答；
- `role="log"` 自带 polite 语义，显式声明 `aria-live` 便于断言与回归。

## 2. 行为断言（`src/components/ai/__tests__/streaming-a11y-behavior.test.tsx`，3 例）

1. 工作区：空会话也持久持有一个 log（live region 不随消息出现/消失，且 `aria-busy=false`）；
2. 工作区：流式期间 `aria-busy=true`、完成后复位 `false`，回答文本位于 log 内；
3. Dock 面板：同语义（流式 busy → 完成复位，回答在 log 内）。

内核侧前提（既有覆盖，非本批新增）：`use-ai-chat-session` 内核测试已断言「流式期间 streaming 置位、结束后复位」，
与本批 DOM 状态机断言组合成完整证据链。

## 3. 验证命令与结果

- `npx --no-install jest --silent --coverage=false streaming-a11y-behavior AssistantChatPanel AIChat.diagnostics a11y-surfaces`
  → **4 suites / 26 tests 全过**（含新增 3 例 + 面板 14 例 + 工作区诊断 2 例 + axe 扫描 7 例；24.8s）；
- `npx --no-install eslint <本批 3 个 tsx 文件>` → 全绿；
- `npx --no-install tsc --noEmit` → 全绿（11.2s）。

## 4. 局限 / 后续

- jsdom 无法验证真实读屏（NVDA/VoiceOver）的播报节奏；`aria-busy` 语义按 WAI-ARIA 规范实施，
  真机读屏走查可并入联调或后续无障碍专项（不阻塞本任务验收口径 = 行为断言）；
- UX F2 其余条目（输入框 `aria-label`、Dock `aria-modal`/初始聚焦、`RunStatusBar role="status"` 等）不在本任务范围，维持原口径。
