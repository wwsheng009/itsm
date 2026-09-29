# B1-04 实施证据（前端 SSE v2 解析升级）

> 文档类型：实施证据（任务 B1-04）
> Status: draft
> 编制日期：2026-09-27
> 任务：B1-04（前端 SSE 解析升级；依赖 B1-03）
> 关联文档：`docs/plan/ai-bot-capability-landing-implementation-plan-2026-09-27.md`（§4.2 B1-04）、`docs/plan/itsm-mcp-external-tool-integration-implementation-plan-2026-09-27.md`（M1-03 同表）
> 核查方式：`tsc --noEmit` + Jest 单测（v2 全事件 / 仅 v1 / 未知事件 / 双发去重 / 畸形载荷五类）

## 1. 结论

| 项 | 判定 | 说明 |
| --- | --- | --- |
| B1-04 | **`unit_verified`** | `ai-api.ts` 支持 v2 事件解析（`run_started` / `step` / `confirmation_required`）；未知事件静默忽略且可经 `onUnknownEvent` 观测上报；**双发去重**（v1 `approval_pending` + v2 `confirmation_required` 同 id 只回调一次）；CSRF 轮换重试 / 同源回退 / 降级横幅路径**未改动**（回归用例保持绿） |
| 兼容 | ✅ | 仅 v1 的旧后端：行为与改动前逐字一致（`done` 单/双参回调签名未动）；仅 v2 的新后端：`confirmation_required` 可独立接住 |
| 遗留 | 2 项 | ①v2 运行事件的 UI 呈现（进度/步骤）尚无契约，本任务只做解析与回调，不落地渲染；②真实浏览器联调截图待补（B1-10/M2-05 类） |

## 2. 交付物

| # | 文件 | 说明 |
| --- | --- | --- |
| 1 | `itsm-frontend/src/lib/api/ai-api.ts`（修改） | 新增 `AIRunStartedEvent`/`AIRunStepEvent` 类型与 `onRunStarted`/`onStep`/`onUnknownEvent` 回调；分发新增三分支；`seenPendingIds` 按 invocation id 去重；未知事件默认静默 |
| 2 | `itsm-frontend/src/lib/api/__tests__/ai-api.test.ts`（扩展） | 新增 3 例（v2 运行事件、双发去重 + v2-only、v2 畸形载荷），并强化既有"未知事件"用例（断言 `onUnknownEvent` 上报） |

## 3. 契约细节（前端侧解析口径）

| 事件 | 载荷校验 | 回调 | 缺省/降级 |
| --- | --- | --- | --- |
| `run_started` | `runId` 必须是 number | `onRunStarted({v?,runId,entrypoint?,conversationId?})` | 缺 `runId` → 忽略，不中断流 |
| `step` | `runId`/`stepIndex` number、`type` 非空字符串 | `onStep({v?,runId,stepIndex,type,payloadRef?,durationMs?})` | 任一不合法 → 忽略 |
| `confirmation_required` | 同 `approval_pending`（`tool` 必需） | `onToolEvent({...status:'pending'})` | 与 v1 同名事件**同 id 去重**（先到者胜） |
| `approval_pending` | 同上 | 同上 | 记入 `seenPendingIds` |
| 未知事件 | — | `onUnknownEvent?.(event, data)` | **不影响渲染、不中断流**（仅观测用途） |

**未改动**（B1-04 明确要求"保留并复测"）：CSRF 403 → 取新 token 原地重试一次；同源代理 `candidate` 轮换与 `credentials` 口径；`done` 回调单参/双参签名兼容；非流式回退与降级横幅。（这些路径的既有用例全绿即回归证据。）

## 4. 运行记录（本机，pwsh）

```text
npx tsc --noEmit → exit 0
npx jest src/lib/api/__tests__/ai-api.test.ts --coverage=false
→ PASS；Test Suites: 1 passed；Tests: 47 passed（44 既有 + 3 新增）
npx jest tool-approval-card.test.tsx tool-call-timeline.test.tsx --coverage=false
→ PASS；18 passed（工具事件消费方回归）
npx jest --testPathPattern "pages/.*ai/(audit|approval)/__tests__" --coverage=false
→ PASS；7 passed（审批页/审计页回归）
```

## 5. 遗留与归属

| # | 项 | 归属 |
| --- | --- | --- |
| 1 | v2 `run_started`/`step` 的 UI 呈现（如"思考中/步骤进度"） | 未定，需先冻结 UI 契约（B1-06 或单独任务） |
| 2 | `confirmation_required` 的对话内确认动作（内联确认） | B1-05 确认状态机 + 后续 |
| 3 | 真实浏览器联调（v2 事件实际到达） | B1-10 / M2-05 类 E2E |

## 6. 变更记录

| 日期 | 变更 | 说明 |
| --- | --- | --- |
| 2026-09-27 | 初稿 | B1-04 交付：v2 解析 + 未知事件观测 + 双发去重；tsc 干净、47/47 单测绿、消费方（时间线/卡片/审批页/审计页）25 例回归绿；判定 `unit_verified` |
