# B1-08 实施证据（EvidencePanel + RunStatusBar）

> 文档类型：实施证据（任务 B1-08）
> Status: draft
> 编制日期：2026-09-27
> 任务：B1-08（证据/时间线面板与运行状态条；依赖 B1-04、B1-02）
> 关联文档：`docs/plan/ai-bot-capability-landing-implementation-plan-2026-09-27.md`（§4.2 B1-08）
> 核查方式：`tsc --noEmit` + Jest 组件测试 13 例 + AI 组件回归 46 例

## 1. 结论

| 项 | 判定 | 说明 |
| --- | --- | --- |
| B1-08 | **`unit_verified`** | 新增 `RunStatusBar`（run 状态/实例/步骤/耗时/计时/预算）与 `EvidencePanel`（工具调用配对 + 运行步骤 + 目标跳转 + 依据引用 + 空态/错误态）；接入 `AIChat`：v2 事件驱动，旧后端无 v2 事件时回退既有 `ToolCallTimeline`，渲染零影响 |
| 诚实边界 | 已登记 | ①SSE 过程事件**不含** `targetType/targetId/supportRef`：面板只从调用方汇总的 invoke 详情渲染（AIChat 由待审批卡片 `onLoaded` 零额外请求获得），读路径无 id → 不显示不臆造；②预算段仅在调用方已知上限时渲染（事件不带预算计数） |
| 遗留 | 3 项 | ①读路径目标/依据需后端在工具事件内补字段（已登记为跨线缺口，B3 页面启用时闭环）；②手工 E2E 截图（真实后端会话）；③`providerLabel` 依赖 done 事件的 providerInfo（运行中只显示 run/步骤） |

## 2. 交付物

| # | 文件 | 说明 |
| --- | --- | --- |
| 1 | `itsm-frontend/src/components/ai/run-status-bar.tsx`（新建） | `RunStatusBar` + `RunSnapshot` + `formatDuration`/`formatElapsed`；running 态 1s 计时（关闭即销毁定时器）；预算段有上限才渲染 |
| 2 | `itsm-frontend/src/components/ai/evidence-panel.tsx`（新建） | `EvidencePanel`：工具事件经 `mergeToolEvents` 配对（与 M1-05 卡片同一规则）；`targetUrl` 仅对已核实路由（`/tickets/:id`、`/incidents/:id`、`/cmdb/cis/:id`）生成跳转，未知类型退化为纯文本；步骤按 `stepIndex` 排序展示 |
| 3 | `itsm-frontend/src/components/ai/AIChat.tsx`（修改） | 消息模型增 `run`/`steps`；`onRunStarted`/`onStep` 捕获（按 `stepIndex` 去重 + 上限 100）；`done`/`error` 置 run 终态；渲染状态条 + 证据面板（有 v2 步骤时替代时间线，保留降级路径）；卡片 `onLoaded` 汇总 `detailsByInvocation` |
| 4 | `itsm-frontend/src/lib/api/ai-api.ts`（修改） | `ToolApproval` 补 `targetType/targetId/supportRef`（后端 `handlers/ai/entity.go:76-78` 已有同名字段，前端仅补齐类型） |
| 5 | `itsm-frontend/src/components/ai/__tests__/evidence-panel.test.tsx`（新建） | 13 例：空态、工具配对与来源/状态/耗时/摘要、目标跳转 + 依据、无 id 不臆造、无跳转处理器退化纯文本、步骤排序与 payloadRef、错误态；状态条 running/done/中断 + 预算可见性；`targetUrl`/格式化纯函数 |

## 3. 数据流与降级矩阵

| 场景 | 后端行为 | 前端表现 |
| --- | --- | --- |
| 开启 v2（默认） | 发 `run_started` + `step`（llm/tool） | 状态条 + 证据面板（步骤与工具行）；`done` → 已完成，`error` → 已中断 |
| 旧后端 / 事件丢失 | 无 v2 事件 | `run`/`steps` 保持 undefined → 无状态条；有 `tool_call_*` 事件时仍走 M1-04 时间线 |
| 写工具待审批 | `confirmation_required` 双发 + 卡片拉取详情 | 卡片 `onLoaded` → `detailsByInvocation` → 证据面板显示「目标 ticket#1001 / 依据 kb:…」并可跳转 |
| 读工具 | `tool_call_*` 无 invocation id | 面板只展示名称/来源/状态/耗时/摘要，不显示目标与依据（不臆造） |

## 4. 运行记录（本机，pwsh）

```text
npx tsc --noEmit → exit 0
npx jest src/components/ai/__tests__/evidence-panel.test.tsx --coverage=false → PASS；13 passed
npx jest src/components/ai --coverage=false → 5 suites / 46 passed（含卡片 9、抽屉 10、时间线 9、Markdown 既有回归）
```

## 5. 缺口登记

| # | 缺口 | 影响 | 处置 |
| --- | --- | --- | --- |
| G-B1-08-1 | 工具事件不含 `targetType/targetId/supportRef`（读路径无 invocation id） | 读路径证据行无目标跳转/依据 | 后端在 `ToolStreamEvent`/emit 处补字段（需确认读路径 invocation 是否落库；若读路径不落库则仅写路径可获得）——跨线登记，B3 场景页启用时闭环 |
| G-B1-08-2 | 事件不含预算计数 | 状态条预算段默认不显示 | 若需展示，后端在 `done`/`step` 载荷附加预算快照（评审后定） |
| G-B1-08-3 | 手工 E2E 截图 | 证据集不完整 | 归 B1-10 / CI E2E |

## 6. 变更记录

| 日期 | 变更 | 说明 |
| --- | --- | --- |
| 2026-09-27 | 初稿 | B1-08 交付：RunStatusBar + EvidencePanel + AIChat 接线（v2 事件驱动、旧后端回退时间线）；13 例组件测试 + 46 例 AI 组件回归全绿，tsc 干净；判定 `unit_verified` |
