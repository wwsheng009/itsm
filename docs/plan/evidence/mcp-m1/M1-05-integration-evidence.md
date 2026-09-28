# M1-05 证据（对话内待审批卡片与跳转，R2 退化形态）

> 验收项：A1-05（对应任务 M1-05）｜DoD：`flow_verified`
> 实际状态：**`integration_verified`**（组件级四态 + 跳转 + 禁止重复操作全绿，后端详情接口同口径并有用例锁定；`flow_verified` 待 M1-10 联调：真实对话中「写工具提交审批 → 卡片出现 → 跳转审批页 → 审批后卡片收敛」）
> 执行人 / 环境：AI 辅助执行；Windows + pwsh，go1.25.13 / jest（jsdom），基线 `2d75b3e5`（分支 `feat/bot-mcp-integration`）
> 时间：2026-09-27

## 交付物

| 文件 | 变更 |
| --- | --- |
| `handlers/ai/handler.go` | 抽出 `toolInvocationItem`（列表与详情**同一份字段装配**）；`GetToolInvocation` 由 10 个字段扩展为完整详情（来源三元组 `provider/serverName/rawToolName/callableName`、`risk`、`argsRedacted`、`roleSnapshot`、`approvedBy/approvedAt/approvalReason`、`durationMs/errorCode/outputSummary`），仍**不回显原始参数** |
| `itsm-frontend/src/components/ai/tool-approval-card.tsx`（新增） | `deriveApprovalState`（纯函数四态判定）+ `ToolApprovalCard`（来源徽标/风险/状态、按需拉取 + 手动刷新、跳转审批页） |
| `itsm-frontend/src/components/ai/AIChat.tsx` | `ChatMessage.pendingApprovals`（由 `approval_pending` 事件派生、去重）、`handleOpenApproval`（跳 `/ai/approval`）、`ChatMessageItem` 渲染卡片；时间线传 `hideStatuses={['pending']}` 避免同一件事两处展示 |
| `itsm-frontend/src/components/ai/tool-call-timeline.tsx` | 新增 `hideStatuses` 属性（隐藏后若无可见条目仍返回 `null`） |
| `itsm-frontend/src/lib/api/ai-api.ts` | 新增 `aiGetToolInvocation` + `ToolInvocationDetail`；**补齐 `ToolApproval` 缺失的 M1-02 字段**（provider/serverName/rawToolName/callableName/risk/roleSnapshot/approvedBy/approvedAt/durationMs/errorCode/outputSummary） |

## 状态语义与「禁止重复操作」

| 卡片状态 | 判定 | 操作面 |
| --- | --- | --- |
| `pending` | `approvalState=pending` 且创建时间在保留期内 | 「前往审批」+ 刷新 |
| `approved` | `approvalState=approved`，或 `status=done/failed` 的终态记录 | **只读**（仅「前往审批页确认」+ 刷新） |
| `rejected` | `approvalState=rejected`（展示 `approvalReason`） | **只读** |
| `expired` | 记录不可得（404/查询失败），或 `pending` 超过保留期（前端常量 `PENDING_TTL_DAYS=7`，**仅提示**） | **只读**，保留人工确认入口 |
| `unknown` | 其他未识别组合 | 只读 + 前往确认 |

- **一期边界（R2 退化形态）**：卡片只做提示与跳转，**不提供内联确认**（内联确认依赖阶段一 B1 的确认状态机，属二期；分析报告 §6.5-6）。
- **数据来源**：`GET /api/v1/agent/tools/:id` 挂载时拉取一次 + 手动「刷新状态」，**不轮询**（与 M1-04 同一取向）。
- **降级**：SSE 事件缺失时不产生卡片（回答内容不受影响）；接口失败时卡片显示「已过期/不可用」而**不是**误判为待审批。

## 执行记录

| # | 命令 | 结果 |
| --- | --- | --- |
| 1 | `go test ./handlers/ai/ -run TestListToolInvocations_ExposesMCPSourceAndRisk -v` | PASS（含新增的详情接口断言：三元组/风险/脱敏参数齐备；响应体无 `"arguments"`、无明文） |
| 2 | `go test ./handlers/ai/ -count=1` | ok（20.2s） |
| 3 | `npx jest src/components/ai/__tests__/tool-approval-card.test.tsx src/components/ai/__tests__/tool-call-timeline.test.tsx` | **18/18 passed**（卡片 9 + 时间线 9） |
| 4 | `npx jest src/lib/api/__tests__` | `ai-api.test.ts` 等 12 个套件通过；`ticket-attachment-api.test.ts` 1 例失败——**与本任务无关**（见下） |
| 5 | `npx tsc --noEmit` | exit 0 |

## 顺带修复的真实缺口

**前端审批类型陈旧**：`ToolApproval` 只有 12 个字段，缺少 M1-02 后端已返回的来源三元组与风险等 11 个字段——审批页与卡片无法类型安全地展示来源/风险。本次补齐（并以 `ToolInvocationDetail` 复用），使 M1-06（审批页来源增强）可直接消费。

## 观察与已知缺口

| # | 项 | 说明 |
| --- | --- | --- |
| 1 | `ticket-attachment-api.test.ts` 预存在失败 | 该用例期望 `/api/v1/tickets/10/attachments/7/preview`，实际得到 `.../attachments/7`；属**他人附件 WIP**（本次未触碰 `ticket-attachment-api.ts` 及其测试，`git status` 亦无涉），已按现状如实记录，不代改 |
| 2 | 「过期」为前端提示 | 后端当前无过期状态机（阶段一 G3 未落地）；卡片 TTL 仅提示「可能已过期」，不改变记录状态 |
| 3 | 跳转不带定位参数 | 审批页暂无按 invocation id 定位/筛选能力，卡片展示 `#id` 供人工对照；M1-06 可增强（深链/筛选） |
| 4 | 内联确认 | 二期（依赖阶段一 B1）；本任务按 R2 退化形态验收 |
