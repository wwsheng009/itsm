# 02 · 数据模型：单租户原则与表设计

> 状态：**Draft v0.2（最佳实践裁决完成）**
> 更新日期：2026-10-17
> 目标读者：架构、后端研发
> 前置阅读：[01 模块蓝图](./01-module-blueprint.md)、[README](./README.md) §决策记录

---

## 1. 设计原则（硬约束）

1. **单租户优先**（D-01）：所有 PM 实体 `tenant_id = owner`；**不建任何指向他租户的字段、外键或关联表**。跨租户场景（MSP）后置且只走"受控访问目标租户数据"（见 [05](./05-msp-delivery.md)）。
2. **权限在目标租户内计算**：沿用本仓 membership 单源口径；PM 不引入任何跨租户权限码进客户租户词表。
3. **RLS + tenant guard 全覆盖**：新表入 `cmd/rls-apply` 表清单与 `internal/schema/tenant_guard.go`；参考现状 `projects` 已入 RLS，`project_tags` 为平台级豁免（衍生表）。
4. **状态机 + 乐观锁**：关键流转走专用端点（`POST /:id/<action>`），CAS 更新；冲突返回 409（对齐 incident/change 既有语义）。
5. **软删与审计**：项目、任务、成员关系支持软删/失效标记；关键动作写 `auditlog`。
6. **命名消歧**：任务实体 `ProjectTask`（表 `project_tasks`），避免与 `process_tasks`、`provisioning_tasks` 混淆。
7. **基线不可变**：计划批准时冻结快照（`project_baselines`），偏差实时计算；**不做挣值（EVM）**。
8. **复杂度收敛**：WBS 深度 ≤3 层；任务依赖 **v1 仅 FS（finish-to-start）**；资源只做只读负荷计算，不做自动均衡。

## 2. 现有表升级：`projects`

在 `ent/schema/project.go` 基础上增补（P0）：

| 字段 | 类型 | 说明 |
|---|---|---|
| `type` | string（`internal\|improvement\|delivery`） | 项目类型，默认 `internal` |
| `parent_project_id` | int，可空 | 父子项目（如子项目/工作流） |
| `health` | string（`green\|yellow\|red`） | 健康度（手动 + P1 可自动汇总） |
| `progress` | int（0–100） | 进度（P1 起由任务自动汇总；`complete` 强制 100） |
| `actual_start` / `actual_end` | time，可空 | 实际起止 |
| `closed_at` | time，可空 | 结项时间 |
| `version` | int，默认 1 | 乐观锁 |
| `budget_amount` | float，可空 | **P2（PE-22）轻量预算金额**（≥0；NULL=未设置；不做财务级核算） |
| `budget_currency` | string(8)，可空 | **P2（PE-22）预算币种**（空=租户默认；对齐 `msp_contract.currency` 先例） |
| `default_weekly_capacity_hours` | float，可空 | **P2（PE-23）项目周容量默认**（小时；NULL=内置 40h；资源热图口径，D-37） |
| ~~`customer_tenant_id`~~ | — | **明确不做**（D-01；MSP 场景见 05） |

> 保留：`name/code/description/manager_id/department_id/start_date/end_date/status/tenant_id`；`status` 从自由字符串升级为受控值：`planning\|active\|suspended\|completed\|cancelled`（沿用现有默认 `active` 的兼容策略：存量数据映射 `active→active`、`completed→completed`、`suspended→suspended`）。

> 编号策略：沿用 `code`（租户内唯一 + 服务层校验）；可选自动编号 `PRJ-xxxx`（对齐 `ticket_number`/`change_number` 生成先例）。基线不落在本表：见 §3.7 `project_baselines`。

> **进度汇总口径（2026-10-18 修复）**：任务创建/更新/流转/软删与项目进度重算同事务执行。只统计未软删、非 `cancelled` 的**叶子任务**（父任务由子任务汇总，避免模板阶段行稀释）；任务 `done` 恒按 100 计，其余取 `progress`；叶子任务取算术平均并四舍五入，无有效叶子任务时为 0。派生更新不递增 `projects.version`。存量回填见 `migrations/20261029_project_management_progress_backfill.sql`（completed→100；有叶子任务→平均值；无任务保留原值）。

## 3. 新增表清单

| 表 | 阶段 | 说明 |
|---|---|---|
| `project_members` | P0 | 项目成员与项目角色 |
| `project_tasks` | P0 | 项目任务（WBS） |
| `project_task_dependencies` | P1 | 任务依赖（v1 仅 FS） |
| `project_milestones` | P1 | 里程碑 |
| `project_baselines` | P1 | 计划基线快照（不可变） |
| `project_templates` | P1 | 项目模板（阶段/任务/检查单/里程碑定义，一键生成；见 [plan/p1-template-design.md](./plan/p1-template-design.md)） |
| `project_risks` | P2 | 风险/问题台账 |
| `project_asset_links` | P2 | 采购/资产结项登记（结项清单 → 既有台账 `asset`/`asset_license`/`vendor`/`contract` 同租户弱引用链接） |
| `project_documents` | P2（可选） | 交付物索引（附件走通用 attachments） |

> 实现进度（2026-10-19）：**P1 四表（milestones / task_dependencies / baselines / templates）已建**（ent schema + `migrations/20261018_project_management_p1.sql`、`migrations/20261019_project_management_pe11.sql` 兜底；RLS 015 覆盖清单已含四表）；PE-14 里程碑/依赖 API、PE-13 计划基线 API（冻结/列表/偏差）与 PE-11 项目模板 API（CRUD + 一键生成幂等）均已落地。
> **P2 进度（2026-10-24）**：`project_risks` 已建（ent schema + `migrations/20261024_project_management_pe21.sql` 兜底；RLS 015 与 `rls-apply` 受管清单同步）；PE-21 风险/问题台账 API（CRUD + 状态机 + 转问题单/变更）已落地。设计：[plan/p2-risk-design.md](./plan/p2-risk-design.md)。
> **P2 进度（2026-10-25）**：PE-22 预算与工时已落地——`projects` 增 `budget_amount`/`budget_currency`（`migrations/20261025_project_management_pe22.sql` 兜底；无新表故 RLS/清单无变更）；任务级 `actual_hours` 补齐校验/UI/汇总端点（`GET /projects/:id/hours-summary`）；基线快照 v2 冻结预算。设计/证据：[plan/p2-budget-hours-design.md](./plan/p2-budget-hours-design.md)。
> **P2 进度（2026-10-26）**：PE-23 资源负荷已落地——周容量两级配置（`projects.default_weekly_capacity_hours` + `project_members.weekly_capacity_hours`，`migrations/20261026_project_management_pe23.sql` 兜底；**无新表**故 RLS/清单无变更）+ 只读热图 `GET /projects/:id/resource-heatmap`（实时计算不落库；v1 自然日口径并显式标注）。设计/证据：[plan/p2-resource-heatmap-design.md](./plan/p2-resource-heatmap-design.md)。
> **P2 进度（2026-10-28）**：PE-26 采购/资产结项登记已落地——新增 `project_asset_links`（`link_type=asset|asset_license|vendor|contract` + 同租户弱引用 `target_id` + 备注 + 软删；未删行唯一 partial index；`migrations/20261028_project_management_pe26.sql` 兜底；RLS 015 与 `rls-apply` 受管清单同步）；结项清单「资产/许可登记」由人工留痕升级为结构化链接（**门禁语义不变**：仍以确认/豁免判定）。设计/证据：[plan/p2-procurement-design.md](./plan/p2-procurement-design.md)。

### 3.1 `project_members`（P0）

```
project_id      int  必填      → projects.id（同租户）
user_id         int  必填      → users.id（见 04：必须是该租户活跃 membership）
project_role    string         pm | member | stakeholder
status          string         active | removed（软失效，保留历史）
weekly_capacity_hours float 可空  成员周容量覆盖（P2/PE-23；NULL=继承项目默认，D-37）
joined_at       time
created_by      int
唯一索引： (project_id, user_id) 存活行唯一；索引 (tenant_id, user_id)
```

> 外部人员不建实体：成员的"外部"标签由 membership 派生（`source=invite/allocation`、`account_kind=provider`），详见 [04](./04-membership-and-external-users.md)。
> 项目经理唯一权威：`projects.manager_id`（审批解析/通知/报表）；`project_role=pm` 仅协同分工、不参与审批解析，规则见 [plan 集成契约 §3.7](./plan/p0-integration-contracts.md)。

### 3.2 `project_tasks`（P0）

```
tenant_id       int  必填
project_id      int  必填
parent_task_id  int  可空      WBS 树
name            string
description     text 可空
assignee_id     int  可空      → users.id
status          string         todo | in_progress | blocked | done | cancelled
priority        string         low | medium | high | critical（对齐 common/validator.go 词表）
plan_start      time 可空
plan_end        time 可空
actual_start    time 可空
actual_end      time 可空
progress        int 0–100
estimate_hours  float 可空      估算工时（P2 展示）
actual_hours    float 可空      实际工时（P2 已落地：任务级实际，≥0 校验 + 汇总端点；worklog 评估结论见 PE-22 D-36）
sort_order      int
version         int 默认 1
created_by / created_at / updated_at / deleted_at
索引： (tenant_id, project_id, status)、(tenant_id, assignee_id, status)
约束： WBS 深度 ≤3 层（parent_task_id 链路校验）
```

### 3.3 `project_task_dependencies`（P1）

```
task_id              int  必填
depends_on_task_id   int  必填
dep_type             string   fs（v1 仅 FS；ss/ff 后置评估）
唯一： (task_id, depends_on_task_id)；服务层校验同项目、无环
```

> v1 只做 finish-to-start；SS/FF 与关键路径（CPM）在依赖落地后评估，不进 P0/P1 范围（见 [07 §2](./07-completeness-review.md)）。

### 3.4 `project_milestones`（P1）

```
tenant_id / project_id / name / description?
due_date / status(planned|reached|missed) / owner_id / reached_at / created_at
索引： (tenant_id, project_id, status)
```

### 3.5 `project_risks`（P2）

```
tenant_id / project_id / kind(risk|issue) / title / description?
level(low|medium|high) / probability? / impact?
owner_id / status(open|mitigating|closed) / resolution?
related_ticket_id 可空（同租户）
related_problem_id / related_change_id 可空（转换产物回写）
created_by / version（乐观锁）/ deleted_at（软删）
索引： (tenant_id, project_id, status)
```

> 落地（PE-21，2026-10-24）：状态机 `open →(mitigate) mitigating`、`open|mitigating →(close) closed`（**close 必填 resolution**）、`closed →(reopen) open`；
> 转换（D-28/D-29）经端口 + 适配器复用问题/变更域服务（未装配 503），成功后 CAS 回写 `related_problem_id`/`related_change_id`，同目标二次转换 409 + `existingId`；
> 终态项目拒绝写（400）；负责人须项目活跃成员；`related_ticket_id` 弱引用做同租户存在性校验。

### 3.6 `project_documents`（P2 可选）

```
tenant_id / project_id / name / kind(deliverable|doc) / attachment_ref?
kb_article_id 可空（同租户）
```
> 附件不新建表：复用通用 `attachments`（`biz_type=project|project_task`，多态 bk 已在 `ent/schema/attachment.go` 实现）。

### 3.7 `project_baselines`（P1，不可变快照）

```
tenant_id     int  必填
project_id    int  必填
version       int             项目内递增（1,2,3…）
snapshot      json            冻结内容：任务计划日期/里程碑/预算/范围说明引用
reason        string 可空     基线变更原因（配合 B6 双轨制：项目内变更日志）
created_by    int
created_at    time
唯一： (project_id, version)；不可更新/不可删除（偏差 = 当前数据 vs 最新基线实时计算）
```

> 用途：计划门"基线冻结"、计划 vs 实际偏差展示、结项复盘对照。基线变更不改历史：新增 version + 变更日志留痕（见 03 §2.3 双轨制）。
> **PE-22（2026-10-25）**：snapshot `schemaVersion` 1→2——`project` 节点增 `budgetAmount`/`budgetCurrency`（冻结预算）；v1 旧快照解码容忍，diff 输出 `budget.frozen=false` 且不参与变更判定（D-34，见 [plan/p2-budget-hours-design.md](./plan/p2-budget-hours-design.md)）。

### 3.8 `project_gates`（P1/PE-12 阶段门）

```
project_gates
  id                    bigserial
  tenant_id             int 非空          -- RLS 谓词列
  project_id            int 非空
  gate                  string(32)        -- initiation|plan|acceptance|closure
  status                string(32)        -- pending|in_review|passed|rejected
  process_instance_id   int 默认 0        -- BPMN 流程实例（0=未触发/降级自动通过）
  approval_chain_id     string(128) 可空
  submitted_by/at       int / time 可空
  decided_by/at         int / time 可空
  decision_comment      string(512) 可空  -- 拒绝原因（必填）或 auto_approved_no_flow
  blockers              jsonb             -- 最近一次门禁阻塞明细（类型/明细/总量）
  reject_count          int 默认 0        -- 累计被拒绝次数（PE-24 一次通过率度量：passed 且 0 = 一次通过）
  version               int 默认 1        -- 状态 + version CAS
  created_at/updated_at time
 唯一： (tenant_id, project_id, gate)；索引： (project_id, status)
```

> 用途：四道阶段门的当前关口状态（一项目一门一行；历史由 `auditlog` 承载）。门禁规则、状态机、
> 流程绑定与降级、状态机强制点（`activate`/`complete`）见 [plan/p1-gate-design.md](./plan/p1-gate-design.md)。
> **PE-24（2026-10-27）**：增 `reject_count`（reject 裁决 CAS 内原子 +1；历史行默认 0 = 视为未被打回，不追溯制造"二次通过"证据），
> 作为**阶段门一次通过率**的唯一事实来源（D-42，见 [plan/p2-portfolio-metrics-design.md](./plan/p2-portfolio-metrics-design.md) §2/§3）。

### 3.9 `project_closures`（P1/PE-15 结项清单）

```
project_closures
  id                    bigserial
  tenant_id             int 非空          -- RLS 谓词列
  project_id            int 非空          -- 一项目一行
  retro_kb_article_id   int 默认 0        -- 复盘 KB 草稿（knowledge_articles，不自动发布）
  retro_generated_by/at int / time 可空
  assets_confirmed_by/at int / time 可空  -- 资产/许可登记确认（人工留痕；P2 升级为台账链接）
  assets_note           string(1024) 可空
  assets_waived_by/at   int / time 可空   -- 豁免（与确认互斥，后写清除先写）
  assets_waive_reason   string(512) 可空  -- 豁免原因（必填）
  members_recycled_by/at int / time 可空  -- 成员回收留痕（回收 member 角色；保留 pm/stakeholder）
  members_recycled_count int 默认 0
  csat_survey_id        int 默认 0        -- CSAT 调研（surveys，结项时尽力触发）
  csat_triggered_at     time 可空
  version / created_by / created_at / updated_at
  唯一： (tenant_id, project_id)
```

> 用途：结项清单的人工/产物项留痕；自动项（任务清零/关联单据终态/结项门 passed）**实时派生不入库**（D-18）。
> 门禁顺序与端点见 [plan/p1-closure-design.md](./plan/p1-closure-design.md)；
> CSAT 答卷经 `survey_responses.project_id`（P1 补齐 P0 契约遗漏列）关联项目。

### 3.10 `project_asset_links`（P2/PE-26 采购/资产登记）

```
tenant_id / project_id            int  必填
link_type                         varchar(16)  asset|asset_license|vendor|contract
target_id                         int  必填（同租户弱引用：既有台账行 ID，不建外键）
note                              varchar(512) 默认 ''
created_by / created_at           int / timestamptz
deleted_at / deleted_by           软删留痕（DELETE 语义；软删后允许重新登记）
索引： (tenant_id, project_id, created_at)
      UNIQUE (tenant_id, project_id, link_type, target_id) WHERE deleted_at IS NULL
```

> **PE-26（2026-10-28）**：写入时校验目标存在且同租户（400 `invalid_target`），重复（未删行同目标）409；
> 目标被删除后链接保留并标记 `targetMissing=true`（不阻断读）；**不做跨租户引用**（MSP `msp_contract` 不在链接目标内）；
> 结项清单「资产/许可登记」仍以人工确认/豁免判定门禁（D-51），链接为登记明细。设计：[plan/p2-procurement-design.md](./plan/p2-procurement-design.md)。

## 4. 业务单据关联（同租户）

P0 采用**单据侧字段**（比多态表查询快、易索引；Q-04 待最终确认）：

| 表 | 新增字段 | 索引 |
|---|---|---|
| `tickets` | `project_id` 可空 | `(tenant_id, project_id)` |
| `incidents` | 同上 | 同上 |
| `problems` | 同上 | 同上 |
| `changes` | 同上 | 同上 |
| `service_requests` | 同上 | 同上 |

约束：`project_id` 必须属于同租户（服务层校验 + tenant guard 覆盖）；项目删除时不做级联，改为服务层校验（有未结单据阻止删除/结项）。

## 5. 关系总览（文字图）

```
Department ─┐
Application ─┼─ Project ─┬─ project_members ── User（租户活跃 membership）
Tag ────────┘            ├─ project_tasks ─┬─ project_task_dependencies
                         │                └─ (assignee) User
                         ├─ project_milestones
                         ├─ project_risks ── (related) Ticket（同租户）
                         └─ project_documents ── Attachment / KnowledgeArticle

Tickets / Incidents / Problems / Changes / ServiceRequests ── project_id ─→ Project（同租户）
ProcessInstance（BPMN business_key=project:<id>）
```

## 6. 工程约定（落地清单）

| 项 | 位置/动作 |
|---|---|
| Ent schema | `itsm-backend/ent/schema/` 新增文件；`go generate` 重生成（流程见 `docs/entc-codegen-runbook.md`） |
| 迁移 | `itsm-backend/migrations/` 新增 SQL（建表 + 索引 + 约束）；`projects` 变更用 ALTER |
| RLS | `cmd/rls-apply` 表清单登记 + `database/rls` 策略；`internal/schema/tenant_guard.go` 登记业务表 |
| 权限码 | 只改 `internal/authz/catalog.go`；新增码需同时进角色绑定，否则守卫报死码 |
| 分页白名单 | `common/pagination.go` 增加新资源映射（如 `projectTaskResponse → project_tasks`） |
| 状态机 | 专用端点 + CAS + 409；handler 层错误语义对齐既有域 |
| 前端 | 路由 `route-paths.ts` / `route-permissions.ts` / 菜单 seed 同步；页面在 `pages/(main)/projects/` |
| 测试 | service 单测 + 跨租户 RLS 用例 + handler 契约测试；覆盖率守卫（`scripts/test-coverage-guard.js`） |

## 7. 验收要点（数据层）

- [ ] `projects` 升级后存量数据可读、code 唯一性约束不破；
- [ ] 新表全部有 `tenant_id` 且 RLS 生效：跨租户查询返回空/拒绝（附用例）；
- [ ] 状态流转非法路径返回 409，并发更新不丢写（CAS 用例）；
- [ ] 单据 `project_id` 跨租户写入被 service 层与 tenant guard 双重拒绝；
- [ ] 删除/结项门禁：存在未结关联单据时拒绝并给出明确错误码；
- [ ] 基线快照不可变（不可更新/删除），偏差计算与最新基线一致（P1）；
- [ ] 任务实际工时字段落库并被报表引用（P2）。
