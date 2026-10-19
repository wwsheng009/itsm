# V0-03 变更影响分析任务卡（plan 产物） · 交付证据

- 日期：2026-10-18
- 任务：V0-03（方案 §7 V 系列：变更影响分析任务卡（plan 产物）｜复用 plan 工具 / artifact / dry-run｜验收：计划采纳率、分析耗时）
- 状态：`integration_verified`（Inline 直达执行 + 产物预览/采纳 + 度量埋点；真实浏览器 E2E 归联调）
- 交付物：
  - 前端 API：`lib/api/ai-api.ts` 新增 `aiExecuteTool()`（`POST /agent/tools/execute`；opts `conversationId/dryRun` 透传）
  - 组件：`components/change/ChangeAIInlinePlanCard.tsx`（目标 CI 选择 + 生成 + 产物预览 + 采纳）
  - 接线：`components/change/ChangeDetail.tsx`（页内渲染 + D1-02 动作桥注册 `impact_plan` handler：点击建议卡**页内直达**生成）
  - 后端验证：`handlers/ai/reply_draft_inline_plan_test.go`（HTTP e2e：plan 工具直执行 + 产物归属）

## 1. 执行链路

```
变更详情页
 ├─ CMDB 影响摘要（ChangeApi.getCMDBImpactSummary）→ affectedCIs 一键选择；无列表时可手动输入 CI ID
 ├─ POST /agent/tools/execute {name: analyze_ci_impact_plan, args:{ciId, change, summary}}
 │    └─ 服务端：只读工具 → 注入发起人上下文（WithToolActor）→ 执行 → 落 bot_artifacts（analysis 产物）
 ├─ 产物预览：title/target/checklist/note/artifactId（面板内直接渲染，无需跳工作区）
 └─ 「采纳计划」：复制为可粘贴文本（**不写业务字段**，提交仍由变更流程完成）
```

- plan 工具为只读：**不进审批队列**（Gate3 仅写工具）；产物归属 = 调用者 + 租户（HTTP e2e 已断言）。
- 动作桥（D1-02）：`useRegisterAssistantAction('impact_plan', …)` 在变更页注册；点击建议卡时递增 `triggerNonce`
  → 面板自动生成（不再发送提示词）。`impact_plan` id 在工单/事件/CI 页复用，页面互斥挂载、卸载即注销。

## 2. 度量口径（验收：计划采纳率、分析耗时）

| 指标 | 事件/口径 |
|---|---|
| 分析耗时 | `task_completed.durationMs`（`taskCardId=impact_plan`、`toolName=analyze_ci_impact_plan`、`outcomeType=artifact`） |
| 计划展现 | `inline_preview`（`extra.panel=ci_impact_plan`） |
| 计划采纳率 | `inline_applied(action=adopt)` ÷ `inline_preview`（`extra.artifactId` 可回溯产物） |
| 失败率 | `task_failed` + `error_shown(errorCode=ci_impact_plan_failed)` |

数据经 O0-01 SDK → O0-02 事件表 → O0-03 看板；对照报告自动化归 O2-01。

## 3. 验证证据（可复跑）

```powershell
cd itsm-backend
gofmt -l -w handlers/ai/reply_draft_inline_plan_test.go
go test ./handlers/ai/ -run 'ExecuteTool_RunsCIImpactPlanInline' -count=1   # ok（HTTP e2e）
go test ./handlers/ai/ -count=1                                            # 全包 ok

cd ../itsm-frontend
npx jest "src/components/change/__tests__/ChangeAIInlinePlanCard.test.tsx" src/lib/api/__tests__/ai-api.test.ts --runInBand
# → 2 suites / 60 tests（面板 5 例 + ai-api 新增 2 例）
npx jest src/components/ai src/lib/ai --runInBand                          # 20 suites / 147 tests（回归）
npx tsc --noEmit ; npx eslint src/components/change/ChangeAIInlinePlanCard.tsx src/components/change/ChangeDetail.tsx src/lib/api/ai-api.ts
```

关键断言：
- 后端 HTTP e2e：`status=success`（不进审批）、`data.artifactId>0`、`kind=analysis`、`checklist` 非空、
  产物 `tenant_id/owner_user_id` 与调用者一致；
- 面板 5 例：默认选中首个受影响 CI 并生成（含 `task_completed.durationMs`）／无 CI 列表时手动输入／
  采纳 → `inline_applied(adopt)`／失败 → 可见错误 + `task_failed` + `error_shown`／`triggerNonce` 动作桥直达；
- ai-api 2 例：`/agent/tools/execute` 请求体（缺省 opts 不落字段；`conversationId/dryRun` 透传）。

## 4. 边界与条件项（不阻塞）

- `dryRun` 入口已支持但本卡未使用：plan/analysis 工具零业务写入，dry-run 语义用于写工具预览（B0-04）；
- 「采纳」当前为复制文本，不回写变更字段（写回需页面表单与字段映射，属 I1 系列 Inline Apply 批次）；
- CAB 证据链/回滚方案文本沿用既有 `risk_draft` 建议 + 对话链；本卡聚焦分析产物；
- 真实浏览器 E2E（变更详情打开 → 建议卡点击 → 页内产物出现）归联调批。