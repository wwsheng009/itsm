# O0-01 前端埋点 SDK 与事件字典 · 交付证据

- 日期：2026-10-18
- 任务：O0-01（观测性与分析 O 系列，方案见 `docs/plan/ai-assistant-dock-plan-2026-10-18.md` §13.2/§13.7）
- 状态：`unit_verified`（真实端到端归 O0-02 后端端点落地后复核）
- 变更文件：
  - `itsm-frontend/src/lib/ai/telemetry.ts`（新增，约 19KB）
  - `itsm-frontend/src/lib/ai/__tests__/telemetry.test.ts`（新增，约 12.6KB，15 例）

## 1. 交付内容

1. **事件字典契约**：28 个事件名与方案 §13.2 逐项对齐（`assistant_opened` … `telemetry_disabled`），
   以 `ASSISTANT_EVENT_NAMES` 冻结并配套 `isAssistantEventName` 运行时守卫（未知名丢弃、不抛错）。
2. **归属字段白名单**：26 个结构化顶层字段（`pageKey/route/entrypoint/toolName/runId/latencyMs/errorCode` 等）
   显式复制，禁止任意对象整体上报。
3. **脱敏**：`extra` 唯一承载自由文本入口——敏感 key（query/message/note/error 等）整体 FNV-1a 哈希；
   非敏感长文本截断 120/200 字符；复杂结构哈希；数字/布尔原样保留。
4. **不阻塞交互**：批量（默认 20 条 / 5s 定时）+ 采样（默认全量）+ 全链路 try/catch；
   transport 失败仅回调 `onError`（默认吞掉）并丢弃批次，不做重试队列。
5. **合规三档**：`on` / `anonymous`（剥离 `sessionId/conversationId/runId/targetId`）/ `off`（停止采集并持久化）；
   关闭动作本身留痕 `telemetry_disabled`（best-effort）。
6. **默认传输**：`POST /api/v1/ai/telemetry/events`，`credentials: 'include'` + CSRF header + `keepalive: true`；
   页面 `visibilitychange(hidden)` / `pagehide` 时尽力冲刷。
7. **应用级入口**：`assistantTelemetry` 单例 + `trackAssistantEvent` / `getTelemetryMode` / `setTelemetryMode`。

## 2. 验证证据（可复跑）

```powershell
cd itsm-frontend
npx jest src/lib/ai/__tests__/telemetry.test.ts --coverage=false --watchAll=false --runInBand
npx tsc --noEmit
npx eslint src/lib/ai/telemetry.ts src/lib/ai/__tests__/telemetry.test.ts
```

- Jest：`15 passed / 15 total`（契约一致性、批量、定时、采样、脱敏、失败静默、off/anonymous、
  缓冲上限、模式持久化、默认传输 CSRF/keepalive/非 2xx 抛错）；
- `tsc --noEmit`：0 错误；
- `eslint`：0 问题（`lib/ai/**` 不在「禁止直接 fetch」glob 内，理由已写入文件头注释）。

## 3. 边界与遗留（条件项）

- **O0-02 未落地**：`/api/v1/ai/telemetry/events` 端点尚不存在，默认传输当前会 404 →
  由客户端静默丢弃（符合"失败静默"契约），不影响业务；
- **接线未做**：SDK 尚未在 AIChat/Dock/试点页面调用（归 D0-05 / I 系列 / V 系列试点）；
- **采样与保留策略**：默认全量 + 无保留期配置，待 O0-02 与合规评审确定；
- **匿名模式语义**：以"剥离可关联字段"实现，服务端侧聚合口径需在 O0-02 对齐。