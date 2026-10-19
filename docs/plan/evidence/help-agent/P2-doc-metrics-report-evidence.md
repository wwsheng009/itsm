# P2 · 文档命中率/解决率运营报表（§9 帮助度量 / §13.3 运营层 / §13.6）· 交付证据

- 日期：2026-10-18
- 任务：P2「智能化与运营」第 3 项：**文档命中率/解决率运营报表**（帮助闭环运营口径）
- 状态：`integration_verified`（后端聚合 + 端点 + 事件面接线 + 报表卡 + 导出；真实 PG 冒烟归联调）
- 交付物：
  - 后端聚合：`itsm-backend/service/ai_doc_metrics.go`
    （三分段 PG 聚合：总量/会话分母 → 按页 Top20 → 按天趋势；比率在 Go 侧计算，**零分母 → null 不虚报 0%**）
  - 端点：`GET /api/v1/ai/observability/doc-metrics?days=`（`ai:read`；`handlers/ai/doc_metrics.go`
    合并 `helpindex` 账本覆盖率；埋点未装配/查询失败 → **200 + telemetry.available=false**（仅覆盖率，不 5xx））
  - 事件面：新增 **`doc_feedback`**（前端字典 `lib/ai/telemetry.ts` + 后端白名单 `handlers/ai/telemetry.go`）；
    **`help_opened` 首次接线**（此前只在字典里、无任何发送点 → 报表分母恒为 0）
  - 前端采集：`AssistantHelpPanel`（`help_opened` 按 pageKey 去重上报 + 「已解决 / 未解决」反馈 UI → `doc_feedback`，
    每条文档视图最多一次）
  - 前端消费：`aiGetDocMetrics` + 类型（`lib/api/ai-api.ts`）、`DocMetricsCard`（挂 `/ai/observability`）、
    Markdown 导出 `lib/ai/doc-metrics-markdown.ts`（复用 I2-02 下载工具 + `handoff(doc_metrics_export)` 埋点）、i18n zh/en
  - RBAC：`go run ./cmd/authz-gen` 再生（新路由 `observability/doc-metrics`）

## 1. 口径（实现即契约）

| 指标 | 口径 | 数据源 |
|---|---|---|
| 文档命中率 hitRate | 1 − 有 `help_gap` 的会话 / 有 `help_opened` 的会话（**会话级**，抗"同页多次打开"放大） | `ai_telemetry_events`（session_id 非空） |
| 解决率 resolveRate（未升级人工率） | 1 − **打开帮助且升级人工**的会话 / 有 `help_opened` 的会话（handoff 只取与帮助会话的交集，避免全量 handoff 误伤分母） | 同上 |
| 反馈解决率 feedbackResolveRate | `doc_feedback(extra.helpful=true)` / 全部 `doc_feedback`（用户显式反馈口径） | `payload->'extra'->>'helpful'` |
| 缺口收敛趋势 trend | 按天（UTC）：opened / gaps / drafts / helpful / unhelpful | `date_trunc('day', occurred_at)` |
| 缺口页面 Top20 | 按缺口数降序（同级按总事件、pageKey），含打开/草案/反馈明细 | 同按页聚合 |
| 账本覆盖率 coverage | 与 `/ai/help/coverage` 同源（helpindex：pages/ok/missing/stale/orphan） | 内嵌索引（零遥测也有基础面） |

- 红线：无有效分母的比率输出 **null（前端「—」）**；窗口缺省 30 天、上限 90 天（越界钳制）。
- 降级：埋点未装配 → `telemetry.available=false` + 零值统计 + 覆盖率照常渲染（复用 H1-01 的降级范式）。

## 2. 验证证据（可复跑）

```powershell
# 后端
cd itsm-backend
go run ./cmd/authz-gen                                   # 再生 RBAC 预检（新路由）
go test ./service/ -run "DocMetrics|RatioOrNil" -count=1   # 4 pass（nil 仓储/钳制+null 比率/聚合/ratio 边界）
go test ./handlers/ai/ -run "GetDocMetrics" -count=1       # 3 pass（遥测聚合+覆盖率/无埋点降级/缺租户 401）
go test ./service/ ./handlers/ai/ ./router/ ./middleware/ ./service/bot/ -count=1  # 5 包全绿（service 59.3s）

# 前端
cd itsm-frontend
npx jest observability --coverage=false --watchAll=false --runInBand   # 4 suites/27 tests（含卡片与看板）
npx jest src/components/ai/dock/__tests__/AssistantHelpPanel.test.tsx  # 11 pass（含 help_opened/doc_feedback）
npx jest src/lib/ai/__tests__/telemetry.test.ts                        # 20 pass（事件字典契约随 doc_feedback 更新）
npx jest src/components/ai src/lib/ai src/lib/store src/lib/utils --coverage=false --watchAll=false --runInBand
#   → 57 suites / 486 tests 全绿（基线 56 suites/477 → 新增 DocMetricsCard 套件 + 面板/看板用例）
npx tsc --noEmit                                        # 通过
npx eslint <本批 10 个文件>                              # 通过
npm run docs:validate                                   # OK：17 篇文档，0 错误
npm run docs:ledger:check                               # OK（ratchet 未回退 + 注册表 in sync）
```

## 3. 关键断言

- **help_opened 去重**：同一 pageKey 每次挂载只上报一次（面板测试断言 `help_opened` 调用恰 1 次）；
  missing/stale 页面同时产生 `help_opened` + `help_gap` → 命中率正确计为「未命中」。
- **反馈固化**：点击「已解决/未解决」后两档按钮禁用且 `doc_feedback` 只上报一次；无文档页面不渲染反馈区。
- **零分母**：`rates` 字段缺省（omitempty）→ 前端 `fmtDocMetricsRate` 渲染「—」；后端单测覆盖 0/0、0/4、5/4、-1/4 边界（钳制 [0,1]）。
- **降级**：`telemetry.available=false` 时卡片提示「仅展示账本覆盖率」，覆盖率仍正常渲染；查询失败时不抛错。
- **导出一致性**：Markdown 快照含概览/缺口页 Top/按天趋势与口径注释，导出动作伴 `handoff(source=doc_metrics_export)` 埋点。

## 4. 条件项（不阻塞）

- **真实 PG 冒烟归联调**：SQL 使用 PG 专有语法（`jsonb ->>`、`date_trunc`、相关子查询、`INTERVAL '1 day' * $n`），
  sqlmock 只验证契约；`payload->'extra'->>'helpful'` 依赖 SDK 信封结构（`extra.helpful` 布尔保留，telemetry 单测已锁脱敏行为）。
- **匿名/关闭模式无会话 id**：`session_id` 仅在 `on` 模式携带 → 会话级比率退化为「—」（事件计数仍可见），符合合规三档语义。
- **文档起草采纳率**未进本报表：`drafts` 只统计 `doc_draft_created` 事件；"草稿→发布采纳"缺信号（归 H1 产物链路/后续单独立项）。
- **时区**：趋势按 `date_trunc('day', occurred_at)` 的会话时区（部署默认 UTC）；如需本地时区运营口径，联调期评估。
- `handoffs` 计数为窗口内全部 handoff 事件（含轨迹复制等非升级动作），仅作上下文；解决率分子使用会话交集口径。