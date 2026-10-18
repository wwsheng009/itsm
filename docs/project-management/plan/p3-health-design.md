# P3 · 健康度派生与解释（PE-27）

> 状态：**设计 v1.0（2026-10-18）**；实现同日落地，验证证据见 §7。
> 上游：02 §2（`health` 行「手动 + P1 可自动汇总」从未落地）、01 §P0 能力矩阵、07 §2-1；
> 触发：2026-10-18 模块评审结论——「先不要急着加功能，先把健康度这一个字段打通（进度偏差+预算偏差+风险数量+逾期任务数），做成可展开的解释」。
> 定位：把 `health` 从手工存储字段升级为**读时派生 + 可解释**的治理信号；零新表、零新端点、零迁移。

---

## 1. 问题与目标

**问题**：`projects.health` 默认 `green`、`PUT /projects/:id` 手工可写、无任何算法与口径——
列表/组合视图/报表分组展示的都是不可信数字；AI 风险信号（D-45）已有确定性事实源，但没有统一到健康度。

**目标**：

1. `health` 值 = 由事实实时派生的确定性结论（进度偏差/预算偏差/风险数量/逾期任务 + 既有信号词表）；
2. 每个非 green 结论带 `healthReasons[]` 可展开解释（哪条规则、什么明细、计数多少）；
3. 列表/详情/组合（行 + `byHealth` 桶）/报表（`data` + 分组）/AI facts 全部同源，口径不再漂移。

## 2. 决策记录（D-54~D-59）

| 编号 | 决策 | 依据 |
|---|---|---|
| D-54 | **只读派生**：健康度读时实时计算、不落库；`projects.health` 列保留但不再读写（存量忽略、不回填、不迁移）；`PUT /projects/:id` 移除 `health` 入参（前端本未发送） | 单一事实来源；跟随 D-38/D-39「热图/度量实时计算不落库」先例；避免双写漂移 |
| D-55 | **规则集 9 条**（§3）：评审四输入（进度偏差/预算偏差/风险数量/逾期任务）为骨架，其余复用 D-45 AI 信号词表（`overdue_project`/`overdue_task`/`blocked_task`/`overdue_milestone`/`gate_rejected` 同名） | 保证列表健康度、AI 风险预警、组合度量使用同一批事实与同一套词表 |
| D-56 | **终态语义**：`cancelled` 恒 green（无信号）；`completed` 仅保留 `missed_milestone`（yellow）；`planning/active/suspended` 应用全量规则 | 健康度 = 「是否仍需关注」；终态不再有未来动作面，但已完成项目的里程碑错过仍应可见 |
| D-57 | **可解释契约**：`ProjectResponse` 新增 `healthReasons[]`（`{rule,level,message,count?}`），red 优先、按规则名排序；`health` = 命中规则的最高级；无信号 = green 且省略数组；报告 `data` 同步携带 | 评审要求「做成可展开的解释」；不新增端点 |
| D-58 | **装配同源**：列表/详情/组合（行 + `byHealth` 桶）/报表（`data` + 分组 counts）/AI facts 全部调用同一 `deriveHealth`；列表/组合按页**批量取数**（tasks/milestones/gates/risks/baselines），单项目路径同函数单发 | D-40「组合与列表口径同源」延伸到健康度；避免 N+1 |
| D-59 | **取数失败不降级**：事实源查询失败 → 读失败（不展示假绿）；基线快照解码失败仅跳过预算规则并留日志 | 「可信」优先于「可用」；快照是辅助输入，单条损坏不应使整个列表不可用 |

## 3. 派生规则（D-55）

`now` = 请求时刻；**逾期类判定沿用 D-45 AI 信号/报表统计的统一时间戳口径（`Before(now)`，不额外做自然日截断，保证与风险预警计数逐条一致）**；进度偏差的日历差取 `dayUTC` 自然日粒度（与报表/度量同口径）。

| rule | 级别 | 条件 |
|---|---|---|
| `overdue_project` | red | `end_date < today` 且 status 非终态（planning/active/suspended） |
| `schedule_deviation` | yellow ≥20pp；red ≥40pp | status ∈ {active, suspended} 且 start/end 齐备：`expected = clamp((today−start)/(end−start), 0, 1) × 100`；`deviation = expected − progress` |
| `overdue_task` | yellow 1~2；red ≥3 | 未终结任务（非 done/cancelled）且 `plan_end < now` |
| `blocked_task` | yellow | 任务 `status = blocked` |
| `overdue_milestone` | yellow | 里程碑 `planned` 且 `due_date < now` |
| `missed_milestone` | yellow | 里程碑 `status = missed`（completed 项目同样保留） |
| `open_high_risk` | yellow 1~2；red ≥3 | `kind = risk`、`level = high`、`status ∈ {open, mitigating}` |
| `gate_rejected` | yellow | 任一阶段门 `status = rejected`（可重新提交） |
| `budget_deviation` | yellow | 最新基线 `schemaVersion ≥ 2` 且冻结预算（金额/币种归一后）与当前预算不一致（含设置↔清空） |

- 级别合并：`red > yellow > green`；每条规则最多产出一行 reason（`count` 记明细数）。
- 阈值常量化：`HealthDeviationYellowPP=20`、`HealthDeviationRedPP=40`、`HealthOverdueTaskRedCount=3`、`HealthHighRiskRedCount=3`。
- 预算归一：`amount ≤ 0 / nil` = 未设置；`currency "" / nil` 视为等价；`schemaVersion < 2` 的旧快照不判定（对齐 D-34）。

## 4. 契约影响

| 面 | 变更 |
|---|---|
| `GET /projects`、`GET /projects/:id`、创建/更新/流转响应 | `health` 语义改为派生值；新增 `healthReasons[]`（无信号省略） |
| `GET /projects/portfolio` | 行 `health`/`healthReasons` 派生；`summary.byHealth` 桶按派生值计数（D-40 对账等式保持） |
| `GET /projects/report-groups` | 绿/黄/红计数改按派生值（与列表逐行一致） |
| `POST /projects/:id/reports/generate` | `data.health` 派生；新增 `data.healthReasons[]`；markdown 概览追加「 健康度信号」行 |
| `PUT /projects/:id` | **移除 `health` 入参**（手工写入退役；存量列只读保留） |
| AI facts | `facts.health` 使用派生值 |
| 端点/权限/迁移/RLS | 零变更（无新端点、无表结构变更；`health` 列保留） |

前端：`Project` 类型新增 `healthReasons?: HealthReason[]`；`UpdateProjectRequest` 移除 `health`；
列表/详情/组合表/状态报告共用 `ProjectHealthTag` 组件（Tag + Popover 展示解释）。

## 5. 实现落点

| 层 | 文件 | 内容 |
|---|---|---|
| 后端 | `handlers/project/health_entity.go`（新增） | 词表/阈值/`HealthReason` DTO/文案映射 |
| 后端 | `handlers/project/health_service.go`（新增） | `deriveHealth` + 批量事实装载 `HealthMap` |
| 后端 | `repository.go` / `repository_impl.go` | 新增 `ListRisksByProjects`、`ListBaselinesByProjects`、`ListProjectsForReportGroups`（替换原聚合方法，聚合上移服务层） |
| 后端 | `handler.go` | 列表/详情/创建/更新/流转响应统一装配派生健康度 |
| 后端 | `p2_metrics_service.go` | 组合行 + `byHealth` 桶使用派生值 |
| 后端 | `report_service.go` / `report_repository_impl.go` / `report_entity.go` | 报告 data/分组/ markdown 使用派生值 |
| 后端 | `ai_service.go` | AI facts 健康度派生 |
| 前端 | `lib/services/project-service.ts` | `HealthReason` 类型；`Project.healthReasons`；更新请求移除 `health` |
| 前端 | `pages/(main)/projects/components/ProjectHealthTag.tsx`（新增） | Tag + Popover 解释（列表/详情/组合/报告共用；各页移除本地健康度词表） |

## 6. 测试矩阵

| 层 | 用例 |
|---|---|
| 单元 | 每条规则触发/不触发（含阈值边界 19/20、39/40、2/3 条）、最严级别合并、reasons 排序、终态语义（cancelled/completed）、预算归一（设置↔清空、旧快照不判定） |
| 集成（服务） | 存量 `health=red` 被忽略（无事实 → green）；逾期任务 → yellow + reason；项目逾期 → red + reason 且 red 排在 yellow 前 |
| HTTP | `GET /projects` 与 `GET /projects/:id` 返回派生 `health`/`healthReasons` |
| 对账 | 组合行 `health` == 列表同行；`byHealth` 桶计数 == 逐行统计；报表分组同口径 |
| 回归 | `pe17`（报表分组按事实构造黄/红）、`pe24`（组合对账改事实构造）更新为派生口径 |

## 7. 验证证据

| 项 | 结果 |
|---|---|
| `go test ./handlers/project/ -count=1` | ✅（`pe27_test.go` 3 用例：`TestDeriveHealthRules` 9 条规则/阈值边界（19/20、39/40、2/3）/终态语义/排序、`TestHealthReadPathIgnoresStoredColumn` 存量列忽略 + 预算偏差 + 报告/markdown 同源、`TestHealthEndpoints` 端点同源 + `PUT` 拒绝 health；`pe17`/`pe24` 改事实构造回归） |
| `go vet ./handlers/project/` | ✅ |
| 前端 `npm run type-check` / 改动文件 `eslint` / `project-service` 单测 **62/62** / `npm run build` | ✅ |
| `node scripts/check-api-paths.js` | ✅（**1075/1075** 路径对账，无新增端点） |
| 迁移/RLS/ACL/Swagger | 无变更（零新表、零新端点、无注解变更） |

## 8. 修订记录

| 日期 | 变更 |
|---|---|
| 2026-10-18 | 初版 v1.0：D-54~D-59 + 9 条规则 + 同源装配 + 测试矩阵（评审「健康度打通」落地） |
| 2026-10-18 | §3 时间口径对齐 D-45（逾期判定 `Before(now)`、偏差日历差 `dayUTC`）；§7 回填验证证据（后端 3 用例 + `go vet`、前端 `tsc`/`eslint`/单测 62/62/`npm run build`、`check-api-paths` 1075/1075）；模块文档（02/06/07/README/CHANGELOG/ROADMAP）同步登记 |
