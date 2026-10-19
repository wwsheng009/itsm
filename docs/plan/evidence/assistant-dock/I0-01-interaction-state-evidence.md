# I0-01 交互状态机 + 错误字典（Dock 接入） · 交付证据（补登记）

- 日期：2026-10-19（交付提交 `4a1fd4f8`，2026-10-19 13:28:33 +0800；本文件为 v5 补任务 T5-01 补登记）
- 任务：I0-01（方案 §3.2 交互状态机 / §7 I 系列；原验收要点：「组件按状态机实现；8 类状态组件测试覆盖」）
- 状态：`unit + integration_verified`（代码审阅 + 定向测试实跑；浏览器级 flow 未在本轮复跑，见 §4）
- 提交：`4a1fd4f8`「feat(ai): I0-01 交互状态机 + 错误字典（Dock 接入）」——5 文件，+929/−11
- 补登记说明：该提交在评审快照 `29edea81` 之后完成，交付时未附证据文件（评审 §2.1–2.2 证据缺口）；本文件按方案 §14.2 T5-01 补登记，核查方式 = 代码审阅（提交信息 + 工作区现状关键实现）+ 定向测试实跑（2026-10-19）。

## 1. 交付物（提交清单）

- 新增 `itsm-frontend/src/lib/ai/interaction-state.ts`（261 行）：10 态纯函数状态机 + 错误字典 + 归类 / 解析函数
- 新增 `itsm-frontend/src/components/ai/dock/AssistantInteractionBanner.tsx`（215 行）：面板交互状态条
- 接入 `itsm-frontend/src/components/ai/dock/AssistantChatPanel.tsx`（Dock 面板接线）
- 测试：`itsm-frontend/src/lib/ai/__tests__/interaction-state.test.ts`（140 行）；`itsm-frontend/src/components/ai/dock/__tests__/AssistantInteractionBanner.test.tsx`（107 行）

## 2. 实现要点（代码审阅）

### 2.1 状态机（§3.2）

- 10 态：`closed / opening / ready / streaming / tool_running / awaiting_confirmation / awaiting_approval / degraded / done / handoff`。
- 判定优先级（`deriveInteractionState`，纯函数、零副作用、零请求）：closed → degraded（错误优先，避免"已失败仍显示生成中"）→ opening → awaiting_confirmation → awaiting_approval → tool_running → streaming → handoff → done → ready；Dock 与工作区共用同一判定。
- 信号全部由现有 store / 会话状态映射（`panelOpen / contextReady / streaming / runningTool / pendingConfirmation / pendingApprovalCount / errorCode / hasError / runStatus / outcomeCount / handoff`），零额外数据请求；`hasMessages` 只在"有产物/完成"时参与 done 判定，避免误触发。

### 2.2 错误字典（原因码 → 文案 → 恢复动作）

- 12 条：`AI_PROVIDER_DISABLED / AI_PROVIDER_KEY_MISSING / AI_PROVIDER_NOT_FOUND / AI_PROVIDER_UNAVAILABLE / AI_PROTOCOL_NOT_IMPLEMENTED / budget_exceeded / rate_limited / offline / stream_failed / tool_failed / approval_expired / aborted`；每条含 title / description / action + actionLabel（动作共 5 类：retry / open_settings / open_approval / switch_provider / dismiss）。
- `classifyAssistantError`：AbortError → `aborted`；已知 errorCode 原样保留；429 → `rate_limited`；status 0 / 离线 → `offline`；5xx → `stream_failed`；其余 `unknown`（honest fallback，不编造原因码）。
- `resolveAssistantErrorCopy`：字典命中即返回；未命中但有后端 detail 时保留原文案作描述兜底，不丢错误、不暴露内部细节。

### 2.3 Banner（可见反馈 + 无障碍）

- `closed/ready` 不渲染占位；`opening` 骨架屏；`streaming / tool_running / awaiting_confirmation / awaiting_approval / done / handoff` 各自文案；`degraded` 用字典文案 + 恢复按钮。
- `role="status"` + `aria-live="polite"`（§3.7）；未注入 `onAction` 时不渲染假按钮（不假装可操作）；每态带稳定 `data-state`（供 E2E / 埋点定位）。

## 3. 验证（2026-10-19 补登记实跑）

命令（`itsm-frontend/`）：

```
npx jest --silent --coverage=false 'src/lib/ai/__tests__/interaction-state.test.ts' 'src/components/ai/dock/__tests__/AssistantInteractionBanner.test.tsx'
```

结果：**2 suites / 33 tests 全部通过**（约 4.3s，0 失败）——覆盖状态机判定与优先级、错误字典完整性、错误归类与兜底、Banner 各状态渲染 / 无假按钮 / data-state 稳定等。

同命令族的相关表面全量（`src/lib/ai/` + `src/components/ai/` + `src/lib/store/`）：**54 suites / 417 tests 全过（0 失败）**。

## 4. 对照原验收要点与局限

- 「组件按状态机实现」→ ✅：纯函数状态机 + Banner 全状态映射 + Dock 面板接入（从现有消息/流式信号推导，零额外请求）。
- 「8 类状态组件测试覆盖」→ ✅ 超出：10 态全覆盖（组件测试 + 状态机 / 字典单测共 33 例）。
- 局限：本轮为**补登记**（静态审阅 + 单测 / 组件测实跑）；未复跑浏览器级 flow（handoff 跳转、审批动作等端到端），如需 `flow_verified` 口径随联调窗口补记。
