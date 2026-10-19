# V1-01 列表页 AI 批量助手（建议分类 / 批量草稿 / 批量分派）· 交付证据

- 日期：2026-10-18
- 任务：V1-01（方案 §7「V 系列 · 用户价值补齐」：列表页批量 AI——选中 N 行 → 建议分类 / 批量草稿 / 批量分派；坐席的真实工作单元是「一批单」）
- 状态：`integration_verified`
- 交付物：
  - 后端：`itsm-backend/handlers/ai/batch.go`（`BatchTriage` + `BatchReplyDrafts`）、
    `handlers/ai/batch_test.go`、`router/ai_routes.go`（两条路由：`ai:read` ∩ `ticket:read`）
  - 前端：`src/lib/api/ai-api.ts`（`aiBatchTriage` / `aiBatchReplyDrafts` + 契约类型 + 上限常量）、
    `src/components/ticket/TicketBatchAIPanel.tsx`、`src/components/ticket/TicketList.tsx`（选中非空即渲染）、
    `src/components/ticket/__tests__/TicketBatchAIPanel.test.tsx`、`src/lib/api/__tests__/ai-api.test.ts`（+2 例）

## 1. 任务映射（三件事与写边界）

| V1-01 子项 | 实现 | 写边界 |
|---|---|---|
| 建议分类 | `POST /ai/tickets/batch-triage`（≤20 张）：逐单返回 `category/priority/urgency/assigneeId/confidence/explanation`（复用 `TriageService.SuggestForTenant`，租户上下文） | 端点只读；面板「采纳 / 全部采纳」逐行调用既有 `PUT /tickets/:id`（`ticket:update`），**不自动提交** |
| 批量草稿 | `POST /ai/tickets/batch-reply-drafts`（≤10 张）：逐单返回 `draft + KB citations`（复用 V0-01 `replyDraftCitations` / `buildReplyDraftPrompt` / 系统提示词） | 端点只读；面板仅「复制草稿」，**不自动评论/发送** |
| 批量分派 | 采用分诊建议处理人（`TriageService` `defaultAssignees` 口径）：面板逐行「分派」/「按建议分派」 | 走既有 `POST /tickets/:id/assign`（`ticket:assign`），需用户显式点击 |

面板整体以 `ai:read` 门控（无权限不渲染）；「采纳」按 `ticket:update`、「分派」按 `ticket:assign` 二次收敛。

## 2. 工程口径（后端）

- **租户隔离**：`ticket.IDIn(ids) + ticket.TenantID(tenantID)` 同查；缺失/跨租户 id 逐项
  `status=not_found`（不泄露存在性），与 `ReplyDraft` 的 404 同口径；
- **上限 fail-visible**：空集/非法 400；分诊 >20、草稿 >10 → 400（去重后计数，非正数丢弃）；
- **有界并发保序**：3 个 worker + 无缓冲 jobs 通道；`items[i]` 按请求顺序落位；
- **单条失败只降级该条**：草稿生成失败 → 该项 `status=failed` + 用户可见 `reason`，
  整体 `degraded=true`，HTTP 仍 200（与单条端点的 503 fail-visible 不同：批量的语义是部分成功）；
- **未装配 fail-visible**：`entClient` 缺失 → 503；分诊服务缺失 → 503；LLM Provider 缺失（草稿）→ 503；
- **审计**：`ticket_batch_triage` / `ticket_batch_reply_draft` 落 `ai_analysis_results`
  （含 `ticketIds/processed|succeeded/latencyMs`，best-effort 不影响响应）；
- **埋点**：复用既有事件（`task_started/task_completed/task_failed/inline_applied`，
  `pageKey=ticket.list`、`taskCardId=batch_triage|batch_draft`）——**事件字典零变更**（§13.2 三处无需同步）。

## 3. 验证证据（可复跑）

```powershell
cd itsm-backend
gofmt -l -w handlers/ai/batch.go handlers/ai/batch_test.go router/ai_routes.go
go test ./handlers/ai/ -run 'TestBatchTriage|TestBatchReplyDrafts' -count=1   # ok（7 例：保序/隔离/去重/上限/未装配/草稿/Provider 失败降级）
go test -p 2 ./handlers/ai/ ./router/ ./middleware/ -count=1                  # 3 包全绿
go run ./cmd/authz-gen                                                        # 路由预检再生 OK
go test ./middleware/ -run 'TestPrecheckMapIsFresh|TestRoutePrecheckAlignment' -count=1  # OK

cd ../itsm-frontend
npx jest src/components/ticket/__tests__/TicketBatchAIPanel.test.tsx src/lib/api/__tests__/ai-api.test.ts --coverage=false --watchAll=false --runInBand
# → 2 suites / 63 tests 全绿（面板 6 例 + 批量 API 契约 2 例）
npx jest src/components/ai src/lib/ai src/lib/store src/lib/utils --coverage=false --watchAll=false --runInBand
# → 57 suites / 489 tests 全绿（AI 表面回归不受影响）
npx tsc --noEmit ; npx eslint src/components/ticket/TicketBatchAIPanel.tsx src/components/ticket/TicketList.tsx src/components/ticket/__tests__/TicketBatchAIPanel.test.tsx src/lib/api/ai-api.ts
```

面板关键断言（`TicketBatchAIPanel.test.tsx` 6 例）：无 `ai:read` 不渲染；分诊请求体 = `{ticketIds}`、
`not_found` 行显示「不可用（工单不存在或无权访问）」；采纳写回 `updateTicket(id,{category,priority})` 且
`inline_applied` 带 `targetId`/`taskCardId`；「全部采纳 / 按建议分派」逐行串行调用写端点并触发 `onApplied` 刷新；
批量草稿只读（零 `updateTicket/assignTicket` 调用）、复制走 `navigator.clipboard` 并记 `inline_applied(copy)`；
>20 张拦截请求并提示；草稿失败上报 `task_failed(errorCode=batch_draft_failed)` 且不渲染结果区。

## 4. 边界与条件项（不阻塞）

- 面板文案未走 i18n：与工单模块（`TicketListToolbar`/`TicketBatchOperations` 等）现状一致；若后续统一 i18n 批次再收敛；
- 草稿仅提供「复制」：不自动填入评论框（评论面板在详情页；批量场景跨页复用 Inline 通道归后续，可复用 V0-01 `externalDraft`）；
- 「按建议分派」为 triage `defaultAssignees` 口径（按分类映射的建议处理人），非 LLM 指派；团队/负载均衡分派维持既有非 AI 通道；
- 真实 Provider E2E（配置 LLM 后批量出中文草稿/分类）与移动端入口归联调与 V1-02；
- 前端非本批回归（当前工作树既有失败，已核对无本批文件引用）：
  `src/pages/(main)/tickets/types/__tests__/index.test.tsx`（「新建类型」按钮找不到）、
  `src/pages/(main)/msp/dashboard/__tests__/index.test.tsx`、`src/pages/(main)/dashboard/components/__tests__/CrossTenantOverview.test.tsx`、
  `src/components/ticket/__tests__/TicketDetailAssignSearch.test.tsx`（V0-01 已登记：auth-store mock 缺 `getState`）。