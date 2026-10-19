# D0-05（埋点子集）聊天埋点接线 · 交付证据

- 日期：2026-10-18
- 任务：D0-05 子集（方案 §7 P0；同时为 §7 V 系列"基线采集"提供前置能力）
- 状态：`unit_verified`（浏览器级 E2E 归后续联调）
- 变更文件：
  - `itsm-frontend/src/lib/ai/chat-telemetry.ts`（新增：聊天流 → 观测事件映射器）
  - `itsm-frontend/src/lib/ai/__tests__/chat-telemetry.test.ts`（新增，6 例）
  - `itsm-frontend/src/components/ai/AIChat.tsx`（接线：流回调 + 确认单三事件）

## 1. 交付内容

1. **可测试映射器**（`createChatTelemetry`）：把 AIChat 流回调映射为 §13.2 事件，
   内部管理一次性/去重/幂等（首 token 去重、结束/失败后不再产生事件）；
2. **AIChat 接线**（最小侵入，5 处流回调 + 2 处确认单）：
   - `message_sent`（发送时；**只报 queryLength，不采集问题原文**）；
   - `first_token`（首个 delta，含首 token 时延）；
   - `tool_call`（tool/status/durationMs/errorCode）；
   - `stream_end`（会话 id、端到端时延、providerSource、provider）；
   - `error_shown`（error 帧 / 网络异常 / 降级失败；用户主动停止不计）；
   - `confirmation_shown` / `confirmation_accepted` / `confirmation_rejected`（对话内确认抽屉）。
3. **失败静默**：映射器 + SDK 双层静默，埋点异常不影响对话链路；
4. **隐私边界**：问题原文与错误详情分别以"长度"与"hash（SDK 敏感 key）"落库。

## 2. 验证证据（可复跑）

```powershell
cd itsm-frontend
npx jest src/lib/ai/__tests__/telemetry.test.ts src/lib/ai/__tests__/chat-telemetry.test.ts --coverage=false --runInBand
npx jest src/components/ai --coverage=false --runInBand
npx tsc --noEmit
npx eslint src/lib/ai/chat-telemetry.ts src/lib/ai/__tests__/chat-telemetry.test.ts src/components/ai/AIChat.tsx
```

结果：
- lib 单测 **21 例全绿**（telemetry 15 + chat-telemetry 6）；
- AI 组件回归 **58 例全绿**（7 套件；console 中的 antd 弃用告警为既有噪音，非失败）；
- `tsc --noEmit` 0 错误；`eslint` 0 问题。

## 3. 与基线采集的关系

- 本次接线打通「前端 SDK → `POST /ai/telemetry/events` → `ai_telemetry_events` → `/ai/telemetry/summary`」链路；
- AIChat（当前唯一对话入口）的消息/首 token/工具/确认指标自本日起可采集，作为 §11 对照的**现状流程基线**；
- 页面级任务（V0-01 工单三件套等）的埋点随试点任务一并接入，避免为不存在的能力提前埋点。

## 4. 条件项与遗留

- **D0-05 其余范围**（静态建议卡 + 上下文条）未开始，归 Dock 底座批次；
- **浏览器级 E2E**（真实登录态 → 事件落库 → summary 可查）未在本机全栈验证，归 O0-03 联调；
- **停止生成**未单独埋点（事件字典暂未定义）；如需统计"用户中断率"，在 O 系列增补事件名并同步前后端契约。\n