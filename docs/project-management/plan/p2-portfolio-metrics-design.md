# P2 · PE-24 组合视图与度量 — 详细设计（D-40~D-43）

- 状态：**已实现 v1.0**（2026-10-27）
- 路线图：`docs/project-management/06-implementation-roadmap.md` §4.3 PE-24「组合视图、按时交付率、里程碑达成率、阶段门一次通过率、资源利用率」
- 上游依据：
  - `03-integration.md` §2.13「报表/仪表盘」：项目组合视图（按状态/健康度/负责人聚合）、交付准时率（计划 vs 实际）、复用既有报表与图表组件；§2.13 附则第 5 条：**度量口径统一：全部由执行数据派生，不新增手工填报；口径以基线为参照**；
  - `07-completeness-review.md` P2 验收：**度量指标（按时交付率/里程碑达成率/阶段门一次通过率）与明细数据抽样对账一致**、**项目组合报表数据与列表口径一致（对账用例）**；
  - PE-23 `plan/p2-resource-heatmap-design.md` D-39：**PE-24 资源利用率复用热图同一口径（planned/capacity）**。
- 关联决策：[D-40](#d-40-组合视图与列表口径同源) / [D-41](#d-41-四项度量口径全周期累计--可选时间窗) / [D-42](#d-42-阶段门-reject_count-列一次通过率的唯一事实来源) / [D-43](#d-43-前端落点独立组合视图页--报表中心入口)

---

## 1. 范围

| 交付项 | 说明 |
|---|---|
| 组合视图 | 租户内可见项目的**聚合分布**（状态/健康度/负责人/部门/类型）+ 与列表一致的项目行 |
| 按时交付率 | 已完成项目的 `actual_end` vs 计划 `end_date`（分子/分母 + 明细） |
| 里程碑达成率 | 已判定里程碑的 reached 比例 + 按期 reached 子指标（分子/分母 + 明细） |
| 阶段门一次通过率 | `passed` 且 `reject_count=0` 的门占比（分子/分母 + 明细） |
| 资源利用率 | 复用 PE-23 热图口径，按项目窗口汇总 planned/capacity（含未排期提示） |
| 前端 | 独立页面 `/projects/portfolio`：度量卡片 + 分布表 + 明细抽屉（抽样对账） |

**非目标**：EVM/挣值（07 §4.2-B2 明确不做）；跨项目成员级 FTE 利用率（依赖 worklog，D-36 触发条件未出现，见 §5.4）；度量快照表/物化视图（实时派生，详见 D-40）；导出 PDF/Excel（后续增量）。

## 2. 决策记录

### D-40 组合视图与列表口径同源

- 组合视图与项目列表**共用同一 `ListFilter` 与同一可见性收窄路径**（`ListForViewer` 的 `MemberProjectIDs` 谓词、软删过滤、全部过滤字段），聚合在服务层对**同一批行**完成——不存在第二套查询谓词，从结构上消除"报表与列表不一致"的可能。
- **实时派生、不落库**（无快照表/物化视图），与 D-39 一致：口径只有一处实现，随时可对账。
- **不给组合视图单独增加列表没有的过滤维度**（如 health 过滤只做聚合列，不做筛选），保证「同一 params → 组合行 == 列表行」这一对账等式成立。
- **取数上限（显式、不静默）**：聚合上限 `PortfolioAggregateMaxProjects=2000`、行上限 `PortfolioMaxProjects=500`；任一超限 `truncated=true`，且 `summary.total` = **实际取到并聚合的行数**（不冒充全量）。v1 面向租户项目数 << 500 的真实规模；超过后按需把聚合下推为 SQL 分组查询（接口不变）。
- 只读查询**不写审计**（无数据变更；对齐读取类端点惯例）。

### D-41 四项度量口径（全周期累计 + 可选时间窗）

四项度量全部由执行数据派生（无手工填报），精确口径见 §5。要点：

- **按时交付率**：分母=已完成（`status=completed` 且 `actual_end` 非空）项目；分子=`actual_end ≤ end_date`；**`end_date` 为空的项目不计入分母，单独计数 `noPlanDate`**（不掩盖、不臆测）。
- **里程碑达成率**：分母=**已判定**里程碑 = `status ∈ {reached, missed}` ∪ `planned ∧ due_date ≤ today`；分子=`reached`；另出 `onTimeReached`（`reached_at ≤ due_date`）与 `missed` 子计数。
- **阶段门一次通过率**：分母=`passed` 门；分子=`passed ∧ reject_count=0`（D-42）；`passed` 门携带 `rejectCount` 明细。
- **资源利用率**：窗口内 planned/capacity 汇总（PE-23 同口径同函数）；`unplanned`（有负责人+估算但无计划日期）单独提示。
- **时间窗（可选）**：`from`/`to`（含端点，UTC 日粒度）分别作用于：
  - 交付：`actual_end`；
  - 里程碑：`reached_at`（reached）或 `due_date`（missed / 逾期 planned）；
  - 门：`decided_at`。
  未传 = 全周期累计。**资源利用率的窗口独立**（`start`/`weeks`，缺省本周/6 周，1..12），响应 `window` 显式回显。
- **rate 空值语义**：分母为 0 时 `rate=null`（前端显示「—」），不返回 0 造成"全部不准时"的误读。

### D-42 阶段门 `reject_count` 列（一次通过率的唯一事实来源）

- `project_gates` 增 `reject_count` int，默认 0；**仅在 reject 裁决的 CAS 更新内原子 `+1`**（与状态置 `rejected` 同一事务/同一条 UPDATE）。
- 为什么不用 `version`：version 也因「提交被门禁阻塞」「重新提交」等递增，无法表达"曾被打回"；审计表可推导但属跨域耦合且受审计保留策略影响，不适合作为业务度量事实来源。
- 历史行默认 0 = 视为未被打回（对存量数据语义保守：不追溯制造"二次通过"证据）。
- **无新表 → RLS/`rls-apply` 受管清单无变更**；迁移兜底 `migrations/20261027_project_management_pe24.sql`（+`_down`）。

### D-43 前端落点：独立组合视图页 + 报表中心入口

- 新页面 `(main)/projects/portfolio.tsx`，路由 `/projects/portfolio`（`route-permissions.ts` 登记 `project:read`；`route-paths.ts` 登记路径）。
- 入口：项目列表页「组合视图」按钮 + `/projects/reports`（报表中心）页内互链。
- 页面结构：过滤条（与列表同参数）→ 4 张度量卡片（含 rate=null 显示「—」）→ 分布表（状态/健康度/负责人/部门/类型）→ 明细 Tab（交付/里程碑/阶段门/资源，直接支撑"抽样对账"）。
- 资源卡片与明细复用 PE-23 的「自然日」口径标注文案（单一来源：响应 `note`）。

## 3. 数据层

| 变更 | 内容 |
|---|---|
| 新列 | `project_gates.reject_count` int not null default 0（ent schema `project_gate.go`） |
| 迁移兜底 | `itsm-backend/migrations/20261027_project_management_pe24.sql`（+ `_down`） |
| RLS | 无新表/无表结构语义变化 → `015_project_management_policies.sql` 与 `rls-apply` 清单不变 |
| ent | `go generate ./ent` 重生成（`ent/projectgate*`） |

## 4. API 契约

统一挂载于 `router/common_system_routes.go` 的 `/projects` 组；权限 `project:read`；可见性收窄同列表（管理员=全租户，非管理员=参与项目）。

### 4.1 `GET /projects/portfolio`

Query（与项目列表完全一致）：`status` `type` `ownerTeamId` `managerId` `q`。

响应：

```json
{
  "generatedAt": "2026-10-27T08:00:00Z",
  "truncated": false,
  "summary": {
    "total": 12,
    "byStatus":   [{ "key": "active", "count": 5 }],
    "byHealth":   [{ "key": "green", "count": 7 }],
    "byType":     [{ "key": "delivery", "count": 4 }],
    "byManager":  [{ "key": "3", "count": 3, "name": "张三" }],
    "byDepartment": [{ "key": "9", "count": 2, "name": "交付部" }]
  },
  "projects": [ /* 与 GET /projects 相同的 ProjectResponse 行（含 ownerTeam） */ ]
}
```

- 聚合按 `count desc, key asc` 稳定排序；`byManager`/`byDepartment` 附带名称（缺失降级空串，不阻塞）。
- `projects` 行上限 500、聚合取数上限 2000；超出时 `truncated=true`（`summary.total`=实际聚合行数）。
- 名称解析：manager → `users`；department → 部门表（批量 2 查询，无 N+1）。

### 4.2 `GET /projects/metrics`

Query：`from` `to`（`YYYY-MM-DD`，可选）+ 资源窗口 `start` `weeks`（可选，缺省本周/6）+ 与列表相同的项目过滤参数。

响应（节选结构）：

```json
{
  "generatedAt": "...",
  "projectScope": { "total": 12, "filters": { "status": "", "type": "", "managerId": 0, "ownerTeamId": 0, "q": "" } },
  "window": { "from": "", "to": "" },
  "onTimeDelivery": {
    "denominator": 6, "numerator": 4, "rate": 0.6667, "noPlanDate": 1,
    "details": [{ "projectId": 7, "code": "PRJ-0007", "name": "…", "planEnd": "2026-09-30", "actualEnd": "2026-09-28", "onTime": true }]
  },
  "milestoneAchievement": {
    "judged": 10, "reached": 7, "missed": 2, "onTimeReached": 5, "rate": 0.7, "onTimeRate": 0.5,
    "details": [{ "projectId": 7, "milestoneId": 3, "name": "UAT 通过", "dueDate": "…", "status": "reached", "reachedAt": "…", "achieved": true, "onTime": true, "dateUsed": "…" }]
  },
  "gateFirstPass": {
    "passed": 9, "firstPass": 7, "rate": 0.7778,
    "details": [{ "projectId": 7, "gate": "plan", "status": "passed", "rejectCount": 1, "firstPass": false, "decidedAt": "…" }]
  },
  "resourceUtilization": {
    "plannedHours": 320, "capacityHours": 480, "rate": 0.6667,
    "unplanned": { "taskCount": 3, "estimateHours": 24 },
    "calendar": { "mode": "natural_days", "businessDaysOnly": false, "note": "v1 按自然日均摊（含周末），未接入租户工作日日历；周容量为整周口径" },
    "window": { "start": "2026-10-26", "end": "2026-12-06", "weeks": 6 },
    "projects": [{ "projectId": 7, "code": "…", "name": "…", "plannedHours": 120, "capacityHours": 240, "rate": 0.5 }]
  }
}
```

- 明细列表上限 `MetricsDetailMaxRows=200/组`，另附 `detailsTotal`；截断只影响明细展示，**不影响分子/分母**（分子/分母基于全量计算）——保证"抽样对账"始终成立。
- 明细行携带 `dateUsed`（口径日期）与 `achieved`/`onTime`/`firstPass` 布尔勾稽字段，可与汇总直接对账。
- 分母为 0 → `rate=null`。
- 非法 `weeks`（<1 或 >12）→ 400；非法 `from`/`to` 日期格式 → 400。

## 5. 计算口径（权威）

### 5.1 按时交付率

```
scope  = 过滤 + 可见性收窄后的项目集
den    = { p ∈ scope | status = completed ∧ actual_end ≠ null ∧ end_date ≠ null
                        ∧ (from/to 未传 ∨ actual_end ∈ [from,to]) }
num    = { p ∈ den | actual_end(UTC 日) ≤ end_date(UTC 日) }
rate   = |num| / |den|（|den|=0 → null）
noPlanDate = |{ p ∈ scope | status = completed ∧ actual_end ≠ null ∧ end_date = null ∧ 时间窗命中 }|
```

- `end_date` = 计划结束日；`actual_end` = 完成/取消动作写入的实际结束（`cancelled` 不计入）。
- 计划口径可对照基线（`project_baselines`），但 v1 以项目主表 `end_date` 为准（列表/详情同源，保证列表-报表一致性）。

### 5.2 里程碑达成率

```
today  = 服务端 UTC 日
judged = { m | status ∈ {reached,missed} } ∪ { m | status = planned ∧ due_date ≤ today }
num    = { m ∈ judged | status = reached }
onTime = { m ∈ judged | status = reached ∧ reached_at ≤ due_date }
missed = { m ∈ judged | status = missed }
rate       = |num| / |judged|（0 → null）
onTimeRate = |onTime| / |num|（0 → null）
```

- `planned ∧ due_date ≤ today` 视为逾期未达成（判定为"未达成"）；`due_date` 未来且 `planned` 不判定（不污染分母）。
- 时间窗：reached → `reached_at`；其余 → `due_date`。

### 5.3 阶段门一次通过率

```
passed  = { g | status = passed ∧ (from/to 未传 ∨ decided_at ∈ [from,to]) }
num     = { g ∈ passed | reject_count = 0 }
rate    = |num| / |passed|（0 → null）
```

### 5.4 资源利用率（复用 PE-23）

- 逐项目：`plannedHours` = 该项目任务的 `estimate_hours` 在 `[plan_start, plan_end]` 自然日均摊后落入窗口 `[windowStart, windowEnd]` 的部分（与热图 `distributeHours` 同一实现）；`capacityHours` = Σ 存活成员容量（`weekly_capacity_hours ?? default_weekly_capacity_hours ?? 40`）× `weeks`。
- 汇总：`Σplanned / Σcapacity`；`unplanned` = 有负责人+估算但无计划日期的任务（不进分母，单独提示，与热图一致）。
- **明确标注（v1 限制）**：容量按项目各自计——同一成员参与 N 个项目会重复计 N 份容量；跨项目成员级利用率需按人按日工时（worklog，D-36 触发条件），届时升级口径并同步 02/03/06。
- 对账：单项目 `plannedHours/capacityHours` 与 `GET /projects/:id/resource-heatmap` 同窗口求和一致（测试覆盖）。

## 6. 前端设计

| 区块 | 内容 |
|---|---|
| 过滤条 | 状态/类型/负责人（用户选择）/归口团队/关键字（与列表同参数）；资源窗口（start/weeks） |
| 度量卡片 | 按时交付率 / 里程碑达成率 / 阶段门一次通过率 / 资源利用率；`rate=null` → 「—」；卡片副标题带分子分母（可对账） |
| 分布表 | 5 张聚合表（状态/健康度/负责人/部门/类型），count 降序 |
| 明细 Tabs | 交付明细（计划/实际/是否按期）、里程碑明细（到期/状态/达成/按期）、阶段门明细（门/结论时间/打回次数/是否一次通过）、资源明细（项目 planned/capacity/rate） |
| 提示 | 顶部口径 Alert（数据实时派生、资源利用率为项目口径 + 自然日标注、未排期提示） |

服务层新增 `projectService.portfolio(params)` / `projectService.metrics(params)` 与类型定义；纯展示辅助（rate 格式化、分布排序）独立为前端纯函数并单测。

## 7. 测试与验证计划（对账用例）

| # | 用例 | 断言 |
|---|---|---|
| 1 | 组合 vs 列表对账 | 同过滤参数下 `portfolio.projects` 行集合 == `ListForViewer` 全量行（id 集合 + 顺序）；`summary.total == total` |
| 2 | 可见性对账 | 非管理员（成员）仅见参与项目；与 `ListForViewer` 同结果 |
| 3 | 按时交付率 | 按期/逾期/无计划日/no actual_end 四类组合下分子分母与明细逐条勾稽 |
| 4 | 里程碑达成率 | reached/missed/逾期 planned/未来 planned 四类；onTime 子指标 |
| 5 | 阶段门一次通过率 | 直接通过 vs 打回后通过（`reject_count=1`）；reject 后 `reject_count` 原子 +1 |
| 6 | 资源利用率对账 | 与 `resource-heatmap` 同窗口逐项目求和一致；未排期只计数不进分母 |
| 7 | 时间窗过滤 | from/to 精确边界（含端点）命中/未命中 |
| 8 | 参数校验 | weeks 越界 400；非法日期 400 |
| 9 | 闸门 | `go test` 全包 + `middleware` + `router` + `migration`；authz-gen + ACL 重生成；前端 tsc/eslint/单测/`check-api-paths`/build；文档链接检查 |

## 8. 兼容与影响

- 新增只读端点，不改既有响应；`project_gates.reject_count` 为附加列，旧客户端无感。
- 路由新增 `GET /projects/portfolio`、`GET /projects/metrics`（静态段与既有 `/:id` 共存，gin 允许）；ACL 清单与 `rbac_precheck_gen` 按惯例重生成。
- 度量口径全部落在本文档 §5，前端文案不再复述公式（避免双份口径漂移）。

## 9. 验证证据（落地后登记）

**后端（2026-10-27）**

- `go test -count=1 ./handlers/project/ -run 'Portfolio|Metrics'` → ✅ **5 用例**：
  1. `TestPortfolioReconcilesWithList`：组合行集合/顺序/`summary.total` 与 `ListForViewer` 全量一致（含 status 过滤与非管理员可见性收窄两组对账 + 负责人姓名富化 + 无参与项目空集）；
  2. `TestMetricsDeliveryAndMilestones`：交付率 den/num/noPlanDate 与明细 `onTime` 勾稽、里程碑 judged/reached/missed/onTimeReached 与明细 `achieved/onTime` 勾稽、`from/to` 时间窗命中；
  3. `TestMetricsGateFirstPass`：真实裁决路径 reject 两次 → `reject_count=2` 累加、最终通过保留计数；一次通过率分子分母与明细勾稽；时间窗；
  4. `TestMetricsResourceMatchesHeatmap`：与 `GET /projects/:id/resource-heatmap` 同窗口逐周求和一致（planned=21h / capacity=40h / rate=0.525）、未排期只提示不进分母、`weeks` 越界 400；
  5. `TestMetricsHandlerEndpoints`：两端点 200 + `weeks=99` 400 + `from=2026-13-01` 400。
- 全包 + `middleware` + `router` + `migration` 回归 → ✅ 4 包全绿（含预检新鲜度 `TestPrecheckMapIsFresh`）。
- `cmd/authz-gen` + `node scripts/generate-acl-manifest.js` → ✅ **939 路由**（+2，与设计预期一致）。
- Swagger：`make swagger-gen`（swag 版本锁 go.mod）→ ✅ 项目模块全部路由首次与注解对齐（恢复 CI `swagger-docs-freshness` 门禁；含 `/projects/portfolio`、`/projects/metrics`）。
- 迁移：`migrations/20261027_project_management_pe24.sql`(+`_down`) 幂等兜底 `project_gates.reject_count`；无新表 → RLS 015 / `rls-apply` 受管清单无变更。

**前端（2026-10-27）**

- `npm run type-check` → ✅；改动文件 `npx eslint` → ✅ 0 告警。
- `npx jest project-service --coverage=false` → ✅ **54/54**（新增 portfolio/metrics 2 例）；`npx jest ProjectPortfolioMetrics --coverage=false` → ✅ **8/8**（比率格式化/桶标签/部门树映射）。
- `node scripts/check-api-paths.js` → ✅ **1067/1067**（+2 前端路径）。
- `npm run build` → ✅。

**未覆盖项（转 P2 收口）**：真实 PostgreSQL 上的迁移与 RLS `-verify`、HTTP E2E、浏览器 UI 冒烟（与 PE-21~PE-24 一并在 P2 收口轮执行，见 `06-implementation-roadmap.md` §4.3 收口项）。
