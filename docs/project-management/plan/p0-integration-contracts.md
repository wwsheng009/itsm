# P0 集成契约设计（Integration Contracts）

> 状态：**Draft v1.0（2026-10-17 按最佳实践裁决落稿）**
> 范围：P0 集成点接口级契约（含 P1 要点）；P2/后置项指路
> 前置阅读：[03 集成点](../03-integration.md)、[04 身份与成员](../04-membership-and-external-users.md)、[07 裁决](../07-completeness-review.md)
> 配套：[P0 数据 DDL](./p0-data-ddl.md)、[P0 API 与页面](./p0-api-and-pages.md)

---

## 1. 决策收敛（最佳实践）

| # | 决策点 | 结论 | 依据 |
|---|---|---|---|
| 1 | 单据关联方式（Q-04） | **单据表加可空 `project_id` + 索引**；不做统一多态关联表（边缘场景后续再引 `project_links`） | 查询/RLS/聚合简单；全仓既有范式 |
| 2 | team 归口 | `projects.owner_team_id BIGINT NULL` + 索引；membership_org 继续承载"人"的组织归属 | 单一归口字段，查询与通知解析最简 |
| 3 | 项目↔CI | P1 经 `application`/`microservice` 间接（既有边）；直连 CI 用 P2 `project_ci_links`（**不给 CI 表加 project_id**，CI 可服务多项目） | 避免把"共享资产"绑死到单项目 |
| 4 | SLA | P1 只做**轻量交付时限**（到期提醒/逾期通知）；SLA 引擎级绑定 **P2 评估**——现状 `sla_monitor_service.go` 仅扫描票据（`Ticket` + `SLADefinitionIDNEQ`） | 域扩展成本明确后才做，不隐性耦合 |
| 5 | 结项满意度 | `survey_response` 加可空 `project_id`（与既有 `ticket_id` 并列） | 最小改动复用 survey 全链 |
| 6 | 按项目过滤单据 | 各域列表接口统一加 `projectId` 过滤参数（不另开聚合端点） | 过滤参数是既有列表能力的自然扩展 |
| 7 | 通知事件词表 | 双词表：事件总线 topic（点式）+ 通知偏好 event_type（下划线式），见 §6 | 对齐 `common/event` 与 `notification_preference` 既有两套词表 |
| 8 | BPMN 模板 | P1 预置"立项审批""结项审批"两个流程模板（走既有流程模板库机制） | 阶段门零散编排 → 模板化复用 |
| 9 | 审批链绑定 | 复用 `process_binding.approval_chain_id`；**不新增绑定表** | processbinding 字段已存在 |
| 10 | 权限码粒度 | P0 沿用 `project:read/write/delete`；任务/成员/关联动作统一归 `project:write`；不新增码（未来拆分走 `internal/authz/catalog.go` 流程） | 避免死码与守卫失败；粒度后置 |
| 11 | 项目可见性 | 谓词 =「管理角色全量 **OR** 项目成员」；成员判定用 `project_members`，管理角色判定复用 `handlers/common/datascope` 的 `IsDataScopeAllRole`，**不扩展 datascope 枚举** | 可见性是数据级关系，非角色级枚举 |
| 12 | 全局搜索 | 复用 `SearchResult` 结构，新增 `type=project/project_task`；**搜索强制可见性谓词** | 防止搜索结果泄露不可见项目 |
| 13 | 指标公式 | 全部派生计算（见 §7）；不落库、不新增填报 | 度量数据来自执行事实 |
| 14 | Q-03（成员细节） | 成员/候选接口返回 `expiresAt`，前端 ≤7 天预警；外部标签用 `source` 过滤参数 | 低成本、体验与合规兼顾 |

## 2. 契约总则

- **权限链**：端点权限码（`project:read/write/delete`）+ 可见性谓词（§1-11）+ 状态机/门禁校验；fail-closed。
- **错误语义**：400 参数、403 权限/可见性、404 资源不存在（不泄露存在性）、409 状态/门禁冲突（响应体带 `blockers`）。
- **错误响应**：沿用 `common.AppError` 风格（UPPER_SNAKE `code` + `message` + 可选 `metadata`，HTTPStatus 映射）；关键业务码：`PROJECT_CLOSURE_BLOCKED`、`STATE_CONFLICT`、`TASK_TICKET_EXISTS`、`PROJECT_NOT_VISIBLE`。
- **审计动作**（`auditlog`，`SetResource(resource).SetAction(action)` 两字段惯例）：
  - resource=`project`：`create | update | status_transition | member_add | member_remove | record_attach | record_detach | gate_reject`
  - resource=`project_task`：`create | update | status | ticket_generate | ticket_unbind`
  - resource=`project_baseline`：`freeze`（P1）；resource=`project_template`：`apply`（P1）
- **异步**：所有通知经事务 outbox（`notification` 投递链），不 fire-and-forget。
- **路径规范**：子资源统一挂 `/api/v1/projects/:id/*`；单据侧挂接动作挂各域 `/api/v1/<domain>/:id/project`。

## 3. P0 集成契约（逐项）

### 3.1 业务单据关联（ticket/incident/problem/change/service_request）

**字段**：五表 `project_id BIGINT NULL` + `(tenant_id, project_id)` 索引（DDL 见配套文档）。

**端点（各域侧）**：

| 方法 | 路径 | 权限 | 说明 |
|---|---|---|---|
| POST | `/api/v1/tickets/:id/project` | `ticket:update` ∩ `project:write` | body `{projectId}`；幂等（已是该项目返回 200） |
| DELETE | `/api/v1/tickets/:id/project` | 同上 | 解除关联；body/query `projectId` |
| POST | `/api/v1/incidents/:id/project` | `incident:write` ∩ `project:write` | 同上 |
| POST | `/api/v1/problems/:id/project` | `problem:write` ∩ `project:write` | 同上 |
| POST | `/api/v1/changes/:id/project` | `change:write` ∩ `project:write` | 同上 |
| POST | `/api/v1/service-requests/:id/project` | `service_request:write` ∩ `project:write` | 同上 |

**端点（项目侧聚合，只读）**：
`GET /api/v1/projects/:id/records?type=ticket|incident|problem|change|service_request&status=&cursor=&limit=`（权限 `project:read` + 可见性谓词；按域批量查询防 N+1；`limit` ≤100，cursor 分页）。

**时序（attach）**：校验权限（端点码 ∩ 可见性谓词）→ 校验项目与单据同租户且项目状态 ∈ {planning, active, suspended} → 写 `project_id` → 审计 → 事务 outbox（可选通知 PM）。
**错误**：项目终态（completed/cancelled）attach → 409；跨租户 → 404/403。

### 3.2 任务 → 工单生成

`POST /api/v1/projects/:id/tasks/:taskId/tickets`（权限 `project:write` ∩ `ticket:create`）

- 请求：`{ticketTypeId?, title?, priority?}`；默认映射：title=任务名、description=任务描述 + 项目引用、priority=任务优先级（词表 `low|medium|high|critical`，与票据同码直传）、requester=当前用户、`project_id`=项目；
- 回写：`project_tasks.linked_ticket_id`（P0 一任务一主工单；重复调用 409 `TASK_TICKET_EXISTS`）；
- **解绑**：`DELETE /api/v1/projects/:id/tasks/:taskId/tickets`（权限同上；置空 `linked_ticket_id` + 审计 `ticket_unbind`；解绑后方可重建）；
- 审计：resource=`project_task` action=`ticket_generate`/`ticket_unbind`；工单创建走既有工单服务（不绕过工单类型/表单/SLA 机制）。

### 3.3 结项门禁（status transition）

`POST /api/v1/projects/:id/status` `{action: activate|suspend|resume|complete|cancel}`（权限 `project:write`）

- `complete` 前置校验（P0 集合）：① 无未终态任务（`todo/in_progress/blocked`）；② 无未终态关联单据（变更终态集合 `{closed,cancelled,rejected,rolled_back}`，其余域按各自终态）；③ 无未完成里程碑（P1 起）；
- 阻塞响应：`409 {code:"PROJECT_CLOSURE_BLOCKED", blockers:[{type,id,number,status}], total}`；
- `cancel` 不校验门禁但需 `reason`；全部流转 CAS + `version`，冲突 409；审计 `project.status_transition` / `project.gate.reject`。

### 3.4 附件

- 复用通用附件端点（`handlers/attachment`、`router/attachment_routes.go`）；
- **必做登记**：在 `handlers/attachment/handler.go` 的 `attachmentHostPolicies`（§4.2 权威表）新增宿主 `project`、`project_task` → `{resource: "project", read: "read", write: "write", remove: "delete"}`；**未登记的 biz_type 会跳过宿主鉴权（`authorizeHost` 未命中即放行）**，属越权放大面，必须登记；
- `biz_id` 必须属于同租户项目/任务（服务层校验）；不新建附件表。

### 3.5 审计

按 §2 动作清单写 `auditlog`；门禁拒绝记录原因与阻塞项摘要；成员撤销（membership 回收联动）复用 `membership.revoke` 既有动作。

### 3.6 成员

接口与语义见 [04](../04-membership-and-external-users.md) §3；REST 路径见配套 API 文档；候选查询必须走"目标租户活跃 membership"（含 `expiresAt`，供 ≤7 天预警）；候选接口权限收紧为 `project:write`，仅返回最小字段（userId/name/team/expiresAt）。

### 3.7 项目经理唯一权威（一致性规则）

- `projects.manager_id` 是项目经理的**唯一权威**：审批链 `project_manager` 解析（`service/approver/project_mgr_resolver.go`）、通知收件人、报表归属均取它；
- `project_members.project_role=pm` 表示协同 PM/展示分工，**不参与审批解析**；
- 移除/失效成员时若 `user_id = manager_id` → 返回 409，要求先变更项目经理（附引导文案）；
- 前端"项目经理"徽标按 `manager_id` 判定，不依赖成员角色字段。

## 4. P1 集成契约要点

| 集成点 | 契约要点 |
|---|---|
| BPMN / 阶段门 | `process_binding.business_type=project`；`business_sub_type=initiation|plan|acceptance|closure`；`business_key=project:<id>`；流程变量 `{tenantId, projectId, managerId, gate, operatorId}`；预置两个审批模板；**流程任务与项目任务不混用**——阶段门触发流程，项目任务由 PM 域管理 |
| 审批链 | 复用 `process_binding.approval_chain_id` 与 `project_manager` resolver；新增 resolver 仅在真实需求出现时按既有接口扩展 |
| 计划基线 | 计划门通过时冻结 `project_baselines`（不可变）；偏差查询实时对比当前任务/里程碑/预算 |
| 里程碑/依赖 | 到期事件（§6）；依赖 v1 仅 FS、同项目、无环（服务层校验，拒绝 400/409） |
| SLA（轻量） | 任务/里程碑 `due_soon`（默认提前 3 天）/`overdue` 触发通知与升级；不动 SLA 引擎 |
| 知识库 | 结项生成复盘草稿（`knowledge_articles` 草稿态，经知识库域服务生产链路：分类校验 + 消毒 + 新建即草稿），**P1 落 `project_closures.retro_kb_article_id`**（`project_documents` 属 P2 可选表，落地后由其引用）；不自动发布 |
| 满意度 | 结项后触发 CSAT 调研（`survey_response.project_id` 关联，P1 已补齐该列）；**缺失不阻塞结项**（尽力触发 + 幂等手动重试） |
| 全局搜索 | `SearchResult.Type` 增加 `project`/`project_task`；结果按可见性谓词过滤；项目编号/任务名可检索 |
| 我的项目任务 | `GET /api/v1/projects/my-tasks?status=&due=`（权限 `project:read`，跨可见项目聚合，供个人工作台） |
| 团队归口 | `owner_team_id` 参与可见范围默认值、通知候选、报表分组；team 软删时降级为无归口并在 UI 提示 |
| 状态报告 | 模板 + 周期由配置驱动，数据派生（§7），先导出后 AI |
| 变更双轨 | 交付变更走 ITSM Change 关联（§3.1）；计划/基线变更走基线版本 + `reason` + 审计，两轨不混 |

## 5. P2+ 指路

问题/Known Error 升级链、服务请求/开通任务进度汇总、CMDB 直连（`project_ci_links`）、报表与 AI、MSP 交付——分别见 [03 §2](../03-integration.md)、[05](../05-msp-delivery.md)、[06 P2](../06-implementation-roadmap.md)。

## 6. 事件与通知清单（新增；双词表）

| 事件总线 topic | 通知偏好 event_type | 触发 | 收件人（目标租户内解析） |
|---|---|---|---|
| `project.created` | —（可订阅） | 立项成功 | 可选：归口 team |
| `project.status.changed` | `project_status_changed` | 状态流转 | PM + 干系人 |
| `project.task.assigned` | `project_task_assigned` | 任务分派/改派 | 负责人 |
| `project.task.due_soon` | `project_task_due_soon` | 到期前 3 天（可配） | 负责人 + PM |
| `project.task.overdue` | `project_task_overdue` | 逾期 | 负责人 + PM（可按升级配置） |
| `project.milestone.due` | `project_milestone_due` | 里程碑到期前/逾期 | PM + 干系人 |
| `project.gate.rejected` | `project_gate_rejected` | 阶段门拒绝 | 发起人 + PM |
| `project.closure.blocked` | —（仅审计） | 结项门禁拒绝 | — |
| `project.member.changed` | `project_member_changed` | 成员增删/失效 | PM |
| `project.survey.invited` | `project_survey_invited`（P1） | 结项 CSAT 邀请 | 干系人 |

> 实现落点：`common/event`（topic 常量与事件结构）+ `notification_preference_service.go` 默认词表与 `dto.ListNotificationEventTypes`（偏好）+ 前端 `EVENT_TYPES` 同步。

## 7. 指标公式（派生）

| 指标 | 公式 |
|---|---|
| 按时交付率 | 结项日期 ≤ 最新基线计划结束日的项目数 / 结项项目数 |
| 里程碑达成率 | `reached` 且 `reached_at ≤ due_date` 的里程碑数 / 到期里程碑数 |
| 阶段门一次通过率 | 首次申请即通过的门次数 / 全部门次申请数 |
| 任务逾期率（P2） | 逾期完成任务数 / 完成任务数 |
| 资源利用率（P2） | Σ 任务估算工时（按周） / Σ（工作日 × 标准工时）；日历限制标注随 B7 |

## 8. 测试要点（集成层）

| 集成点 | 必测 |
|---|---|
| 单据关联 | 跨租户拒绝；终态项目拒绝；幂等重复 attach；解除后聚合消失；审计落库 |
| 任务→工单 | 权限组合（有项目写无工单创建 → 403）；重复生成 409；解绑后重建成功；生成工单出现在项目聚合与工单列表 |
| 结项门禁 | 未终态任务/单据阻塞并返回 blockers；全部终结后可结项；并发结项 CAS 409 |
| 附件 | 宿主策略已登记（project/project_task → project:read/write/delete 映射，跑既有附件鉴权用例）；跨租户 biz_id 拒绝 |
| 通知 | outbox 落库与投递审计；偏好匹配（长/短词表归一）；收件人仅目标租户 |
| 搜索（P1） | 不可见项目不出现在结果（反例用例） |
| 指标（P1） | 抽样对账（基线、里程碑、门禁数据） |

## 9. 回写与对应关系

- 本文档落地后：03 §2.8 SLA 口径、06 §2 与验收要点同步微调（已完成）；
- Q-03/Q-04 已在本文档 §1 收敛，README 开放问题同步更新；
- 实施时以本文档为接口验收基准，偏差须回改本文档与 03。