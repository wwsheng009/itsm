# P0 设计审查记录（Design Review）

> 状态：**v1.0（2026-10-17 审查完成，发现项已处置/记录）**
> 审查对象：[P0 集成契约](./p0-integration-contracts.md)、[P0 数据 DDL](./p0-data-ddl.md)、[P0 API 与页面](./p0-api-and-pages.md)
> 方法：逐条把设计断言与仓内代码事实对账（词表/约定/既有实现/守卫机制），不是文档互读。

---

## 1. 结论

设计整体成立、可按 PE-1 开工；审查发现 **5 项需修正（已当场修正）**、**3 项一致性规则（已补文档）**、**3 项信息级确认（无需改动）**、**1 项实施前复核清单**。

## 2. 发现与处置

| ID | 级别 | 发现 | 代码事实/证据 | 处置 |
|---|---|---|---|---|
| DR-01 | 高 | 设计用任务优先级 `urgent`，与票据词表不一致 | `common/validator.go` `validPriorities = low/medium/high/critical`；`common/constants.go` `PriorityCritical`；票据/事件均用 `critical`（`connector/builtin/feishu` 有 critical↔urgent 映射，属外部系统适配） | ✅ 已改为 `low\|medium\|high\|critical`（02/DDL/契约/API 四处） |
| DR-02 | 高 | 附件只写"扩展 biz_type 白名单"——实际机制是宿主策略表，**未登记的 biz_type 会跳过宿主鉴权（直接放行）** | `handlers/attachment/handler.go:447-460`：`attachmentHostPolicies`（§4.2 权威表）；`authorizeHost` 未命中返回 true | ✅ 契约改为"在 `attachmentHostPolicies` 登记 `project`/`project_task`（resource=project，read/write/delete 动作）"；登记清单补充该行 |
| DR-03 | 中 | 审计动作写成点式 `project.task.create`，不符合仓储惯例 | `auditlog` 为 `SetResource(...)`+`SetAction(...)` 两字段；实例：`SetResource("connector_inbound")`、`SetResource("operational_command").SetAction(action)` | ✅ 改为 resource/action 对：`project`: create/update/status_transition/member_add/member_remove/record_attach/record_detach/gate_reject；`project_task`: create/update/status/ticket_generate/ticket_unbind |
| DR-04 | 中 | 任务→工单只有生成，没有解绑端点与重建规则 | 设计自身缺口 | ✅ 补 `DELETE /projects/:id/tasks/:taskId/tickets`（解绑置空 + 审计 `ticket_unbind`）；重建=解绑后再生成 |
| DR-05 | 中 | 成员候选接口用 `project:read`，暴露租户用户清单（PII）给只读用户 | 候选=租户内用户检索；最小权限原则 | ✅ 收紧为 `project:write`，返回最小字段（userId/name/team/expiresAt） |
| DR-06 | 低 | `project_members` 存活唯一索引未含 `tenant_id` | 仓内先例：`uq_membership_live`、`uk_msp_allocation_active` 均含租户维度 | ✅ 改为 `(tenant_id, project_id, user_id) WHERE status='active'` |
| DR-07 | 低 | PM 双事实源：`projects.manager_id` 与 `project_members.project_role=pm` | 审批解析器读 `manager_id`（`service/approver/project_mgr_resolver.go`） | ✅ 补一致性规则（契约 §3.7）：`manager_id` 唯一权威；移除/失效时改 409 引导先换人 |
| DR-08 | 信息 | 结项门禁变更终态集合与 MSP 写面口径是否一致 | MSP IP-P2-14/15 终态 `{closed,cancelled,rejected,rolled_back}`；`completed`/`failed` 仍可继续流转 | ✅ 保留该集合并注明：**completed/failed 故意阻塞至关闭**（收尾口径，比写面更严） |
| DR-09 | 信息 | `GET /projects/my-tasks` 与 `/projects/:id` 是否路由冲突 | Gin 先例：`/tickets/stats`、`/search` 与 `/:id` 共存（`router/ticket_routes.go`） | ✅ 无冲突，无需调整（已在 API 文档注明） |
| DR-10 | 信息 | RLS 批次编号与 survey 覆盖 | `database/rls/migrations` 最新 014；`surveys`/`survey_responses` 已在 `cmd/rls-apply/main.go:95` 受管清单 | ✅ 015 可用；survey 无需新增 RLS（仅加列） |
| DR-11 | 低 | `GET /projects` 关键字搜索性能 | `code` 有唯一索引可前缀命中；`name` 无索引 | ✅ 备注：P1 评估 `pg_trgm`；P0 限制 `q` 行为前缀优先 |

## 3. 实施前复核清单（进 PE-1 前必须再确认）

- [ ] `common/pagination.go` 的 `domainAliases/singularAliases` 实际映射路径（落地时按最新代码补别名）；
- [ ] `attachmentHostPolicies` 登记后跑既有附件鉴权用例（`handlers/attachment/handler_test.go`）确保无回归；
- [ ] `internal/schema/tenant_guard.go` 对 5 张新表的扫描通过（`internal/schema` 一致性测试）；
- [ ] 变更终态集合实现取 `dto.ChangeStatus*` 常量（不硬编码字符串）；
- [ ] 通知词表落地时同步 `dto.ListNotificationEventTypes` + 前端 `EVENT_TYPES`（双处，缺一会静默丢弃偏好）。

## 4. 修订记录

| 日期 | 变更 |
|---|---|
| 2026-10-17 | 初版：11 项发现（5 修正/3 规则/3 信息）+ 复核清单；同步修订契约、DDL、API 三份文档与 02/README |