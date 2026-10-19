# I1-01 Inline Apply（分诊/字段建议一键应用 · diff + undo） · 交付证据

- 日期：2026-10-18
- 任务：I1-01（方案 I 系列：Inline Apply——分诊/字段建议一键应用（diff + undo）｜验收：工单创建/详情接入；负向断言"不自动提交"）
- 状态：`integration_verified`（状态机 + 弹窗 + 双页面接入 + 埋点；页面级浏览器 E2E 归联调）
- 交付物：
  - 状态机：`components/ai/use-inline-apply.ts`（`preview → confirm → applied/undo`；未确认绝不调用 `onApply`；
    失败保留待确认可重试；埋点 `inline_preview/inline_applied/inline_undone/error_shown`）
  - 弹窗：`components/ai/InlineApplyDiff.tsx`（字段 before→after、空值占位「（空）」、**不自动提交**提示、
    确认/取消）
  - 工单**详情**接入（`TicketDetail.tsx`）：`AISuggestionPanel.onAccept` 从「直接 updateTicket 写库」改为
    **diff 预览 → 用户确认 → 写回（既有 updateTicket 审计链）→ 「撤销」提示条写回旧值**；
  - 工单**创建**接入（`tickets/create/index.tsx`）：AI 分类建议从「自动回填表单」改为 **diff 预览 → 确认后仅
    `form.setFieldValue`（不提交）→ 「撤销」恢复原值**；分类仍仅在匹配后端主数据时进入 diff。

## 1. 行为口径（验收映射）

| 要求 | 实现 | 证据 |
|---|---|---|
| 应用前 diff 预览 | `InlineApplyDiff` 展示 `before → after`（分类/优先级），含空值占位 | 组件 3 例 |
| **负向：不自动提交** | 详情：仅 `confirm()` 后调用 `updateTicket`（写回也仅是字段更新，非提交）；创建：仅写表单值，不触达任何提交 API | hook 负向用例（preview 后 `onApply` 未被调用）+ 代码路径 |
| 可撤销 | 详情：`undo()` 把旧值写回（`updateTicket` 反向变更）；创建：`undo()` 恢复表单原值 | hook 撤销用例 + 两页面「撤销」入口（`inline-applied-undo` / `ticket-ai-applied-undo`） |
| 失败可见 | `onApply` 抛错 → 页面提示（handleError / message）+ `error_shown`，待确认保留可重试 | hook 失败用例 |
| 审计留痕 | 详情走既有 `updateTicket`（服务端更新/审计链）；AI 审计沿用 `AISuggestionPanel.recordAIAudit` | 既有链路（未改动） |

埋点（O0-01 SDK）：`inline_preview/inline_applied/inline_undone` 的 `extra = {scope, action?, fields}`（fields 为
逗号连接的字段名；scope=`triage_detail`/`triage_create`）——与 V0-01/V0-03 的 Inline 口径一致，可聚合采纳/撤销率。

## 2. 验证证据（可复跑）

```powershell
cd itsm-frontend
npx jest src/components/ai src/lib/ai --coverage=false --watchAll=false --runInBand
# → 21 suites / 151 tests（新增：hook 5 例 + 弹窗 3 例）
npx tsc --noEmit ; npx eslint src/components/ai/use-inline-apply.ts src/components/ai/InlineApplyDiff.tsx src/components/ticket/TicketDetail.tsx "src/pages/(main)/tickets/create/index.tsx"
```

关键断言：
- hook：preview 后 `onApply` **未被调用**（负向）；confirm → `onApply(changes)` + `inline_applied` + `applied`；
  cancel → 清空且不调用；onApply reject → `error_shown` + pending 保留 + 未标记 applied；空变更集不进入预览；
- 弹窗：差异渲染与空值占位、确认/取消回调、自定义标题/testId、"保存/提交仍由你在页面上完成"提示。

## 3. 边界与条件项（不阻塞）

- 页面级交互测试：详情页既有测试存在**前置 mocking 问题**（`TicketDetailAssignSearch`，前批已隔离确认）；
  本批以「状态机 + 弹窗」单测 + tsc 覆盖接线，完整页面流程（点击采纳 → diff → 确认 → 撤销）归联调 E2E；
- 撤销对「无旧值」字段跳过（`before` 为空时不写回空值，避免非法更新）——当前分诊字段（分类/优先级）在真实
  工单上必有值，属防御性分支；
- 创建页撤销恢复的是**表单原值**（未提交，无服务端回滚语义）；详情页撤销为反向 update（审计可见）；
- 「字段建议」范围：当前为分诊字段（category/priority）；`draft_ticket_fields` 的创建页字段草案接入归后续
  （同一 `useInlineApplyFlow` 通道，可直接复用）。