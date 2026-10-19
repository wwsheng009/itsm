# I1-03 主动协助（FAB 徽标 + 内联建议条 + 通知订阅） · 交付证据

- 日期：2026-10-18
- 任务：I1-03（方案 §3.6 主动协助/不打扰原则｜验收：默认静默开关生效；「不再提示」记忆可用）
- 状态：`integration_verified`（前端集成 + 单测；浏览器 E2E 与真机通知链路归联调）
- 交付物：
  - 偏好与记忆：`lib/store/assistant-proactive-store.ts`（`enabled` 默认 true / `silent` 默认 **true** /
    `dismissedScopes` 持久化；`dismissScope` 幂等、`restoreDismissed` 清空）
  - 信号注册表：`lib/ai/proactive/signals.ts`（approval / notification / page_suggestion 三类构建器 +
    高价值通知判定 + `selectProactiveSignals` 汇总/过滤/计数）
  - 数据面 hook：`lib/ai/proactive/use-proactive-signals.ts`（待审批轮询 60s + 未读高价值通知 +
    `notificationWS` 只读订阅；`enabled=false`/无权限 → **零请求、零输出**）
  - UI：`components/ai/dock/ProactiveSuggestionBar.tsx`（`role=status`、关闭/「不再提示」、动作按钮）+
    `AssistantDock` 接线（FAB 徽标、建议条、设置 Popover：启用/静默开关 + 恢复已忽略 + 通知订阅设置入口）
  - 埋点：事件字典新增 `proactive_shown / proactive_clicked / proactive_dismissed`（§13.2 已同步）

## 1. 形态与口径（实现即契约）

| 允许形态（§3.6） | 实现 | 静默语义 |
|---|---|---|
| FAB 状态点/徽标 | 待审批 + 高价值未读数求和上球（`9+` 封顶，`aria-label` 可读） | 静默不影响徽标（非打断） |
| 页面内联建议条 | 当前页面 L1 声明首个任务卡；「让 AI 看」= 预填 + 开面板（**不自动发送**） | `silent=true`（默认）→ 不出现；用户关闭静默后按页出现 |
| 通知中心投递 | 复用 `GET /notifications` + `notificationWS`；订阅/退订走现有 `/profile` 通知偏好 | 与通知中心同源，不新增投递通道 |

- 计数口径：`badgeCount = Σ 待办类信号`（approval + notification）；页面建议 `count=0` 不计数（建议≠待办）；
- 「不再提示」记忆键：`approval:pending` / `notification:<type>` / `page:<pageKey>`（同页建议一次忽略即长期静默）；
- 频率与打扰约束：同一页面只出一条建议；会话内「关闭」不写记忆；`enabled=false` 为完全静默（无徽标/无条/零请求）；
- 高价值通知判定：类型白名单（`sla_warning/sla_violated/approval_required/ai_*/bot_*/agent_*`）或深链 `/ai/**`。

## 2. 验证证据（可复跑）

```powershell
cd itsm-frontend
npx jest src/components/ai src/lib/ai src/lib/store --coverage=false --watchAll=false --runInBand
# → 32 suites / 230 tests 全绿（新增 12 例：信号构建器 6 + 容器集成 6）
npx tsc --noEmit
npx eslint src/lib/store/assistant-proactive-store.ts src/lib/ai/proactive \
  src/components/ai/dock/AssistantDock.tsx src/components/ai/dock/ProactiveSuggestionBar.tsx \
  src/components/ai/dock/__tests__/AssistantProactiveAssist.test.tsx
```

关键断言（`AssistantProactiveAssist.test.tsx` / `proactive/__tests__/signals.test.ts`）：
- 徽标：2 项 pending → `2` + `aria-label="2 项 AI 待处理"`；默认静默下**无**建议条；
- 默认静默：`silent` 默认 true 且不出条；`setSilent(false)` 后出现「建议：分诊建议（分类与优先级）」；
- 「不再提示」：点击后条消失且 `dismissedScopes` 含 `page:ticket.detail`，**localStorage 持久化可复跑**；
- 「让 AI 看」：`pendingPrompt` 写入、面板打开、**未发送**（负向断言：不自动提交/不自动发送）；
- 通知订阅：WS 推送 `sla_warning` 未读 → 徽标 `1`；`commented`（普通）→ 不计入；
- 总开关关闭：无徽标、无建议条，且 `aiGetToolApprovals` / `getUserNotifications` **零调用**；
- 设置面板：静默开关可切换、「恢复已忽略提示（1）」清空记忆；
- 选择器口径：已忽略 scope 过滤后徽标/建议各自生效，通知按类型聚合去重、上限 3。

## 3. 边界与条件项（不阻塞）

- 审批计数 = `GET /agent/tools/invocations?state=pending`（`ai:read`，与审批页同码）：后端语义为**租户内待审批**，
  未区分「指派给当前审批人」；如需按人过滤需后端扩展参数（下一迭代候选）；
- 通知订阅为只读复用：WS 单例的 connect/disconnect 仍由 Header 负责（助手只 `onNotification`，不干扰连接生命周期）；
- 高价值通知类型白名单为前端口径，后端新增类型时同步扩展即可（`HIGH_VALUE_NOTIFICATION_TYPES`）；
- 徽标刷新：首屏 + 60s 轮询 + WS 推送即时；多标签页共享 localStorage 记忆（与既有 store 一致）；
- 浏览器 E2E（徽标/条的真实视觉与移动端位置）归联调。