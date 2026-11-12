# C 方案：membership 作为租户内身份（2026-11-12）

> **状态**：implemented（Phase 1–3 工单域已发布 238；Phase 4 第一批接收人校验已发布；
> 第二批问题/变更/服务请求 actors 迁移与约束 VALIDATE 已发布 238 `1.8.0-dev.47bd56dc5`，
> E2E 46/46）
> **背景**：provider 深度切换到客户租户后，JWT 的 `tenant_id` 已指向客户租户，但工单仍以
> `users.id` 作为申请人引用；provider 的 `users.tenant_id` 仍在 provider 租户，导致
> “权限通过、申请人 404”。
> **关联**：`docs/multi-tenant/01-architecture.md`、`docs/multi-tenant/02-deployment-and-configuration.md`、
> `docs/review/2026-11-11-role-whitelist-audit.md`、`docs/multi-tenant/plan/msp-provider-switch-role-data-scope-plan.md`

## 1. 结论

客户租户内的 provider 身份应是 `user_tenant_memberships` 中的一条 provider membership，
而不是复制一条 `users` 记录，也不是把 provider 的 `users.id` 直接写进客户工单。

目标身份四元组：

```text
user_id        全局登录账号（users.id）
tenant_id      当前请求作用域（JWT/context）
membership_id  当前租户内成员身份（user_tenant_memberships.id）
account_kind   customer | provider | platform
```

所有**租户内**业务引用使用 `membership_id`；`user_id` 只用于登录、全局资料和跨租户审计。

## 2. 现状与缺口

当前系统已有：

- `user_tenant_memberships`：`user_id + tenant_id` 唯一、`account_kind`、`source`、
  `role_id`、`msp_role`、`allocation_id`、`status`、`expires_at`、`deleted_at`；
- `uq_membership_id_tenant`：`(id, tenant_id)` 复合唯一，可作为业务表复合外键目标；
- `ResolvePermissions` / `RBACMiddleware` 已按目标租户 membership 解析权限；
- `SwitchTenant` 已支持 provider → customer 作用域切换。

缺口：

1. 工单等业务表仍引用 `users.id`，隐含 `users.tenant_id == 业务表.tenant_id`；
2. 请求上下文没有把 `membership_id` 作为一等身份传递；
3. `/auth/me`、登录、切换响应没有暴露 `membershipId/accountKind`；
4. provider 代客建单没有显式申请人模型；
5. 其他租户内引用（评论人、附件上传人、审批人、CC、通知收件人）仍待迁移。

## 3. 目标模型

### 3.1 Membership 上下文

请求经过认证和 RBAC 后，服务端解析：

```text
(user_id, tenant_id) -> active membership
```

并把以下值放入请求上下文：

```text
membership_id
membership_account_kind
membership_source
```

JWT 仍只带 `user_id` / `tenant_id`；`membership_id` 每次请求由数据库解析并校验 active/未过期。
这样 allocation 撤销、membership 软删后，旧 JWT 不会继续携带可用 membership。

### 3.2 工单字段

目标字段：

```text
tickets.requester_membership_id
tickets.assignee_membership_id
tickets.created_by_membership_id
```

保留旧字段用于兼容与回滚：

```text
tickets.requester_id
tickets.assignee_id
```

约束目标：

```text
(tenant_id, requester_membership_id) -> user_tenant_memberships(tenant_id, id)
(tenant_id, assignee_membership_id)  -> user_tenant_memberships(tenant_id, id)
(tenant_id, created_by_membership_id)-> user_tenant_memberships(tenant_id, id)
```

过渡期允许 membership 字段为空，读路径回退 `user_id`；新写入必须填写 membership。

### 3.3 申请人 vs 建单人

| 场景 | requester_membership_id | created_by_membership_id |
|---|---|---|
| 客户用户自己建单 | 客户用户 membership | 客户用户 membership |
| provider 代客建单 | 客户用户 membership | provider 在客户租户的 membership |
| provider 给自己建单 | provider membership | provider membership |

要求：

- provider 代客建单必须显式提供 `requesterId`；
- 服务端解析为目标客户租户内的 active membership；
- 未提供申请人时返回 400，不再使用当前 provider 用户兜底；
- 客户用户自己建单仍忽略客户端伪造的 `requesterId`。

## 4. 分阶段实施

### Phase 1：身份上下文（本轮）

- 新增 `ResolveActiveMembership`；
- `RBACMiddleware` 解析 membership 并写入 `membership_id/account_kind/source`；
- `/auth/me`、Login、Refresh、SwitchTenant 返回 `membershipId/accountKind`；
- 前端 `User` 类型与 `mapServerUser` 同步字段。

### Phase 2：工单垂直切片（本轮）

- `tickets` 增加 `requester_membership_id`、`assignee_membership_id`、
  `created_by_membership_id`；
- 迁移回填现有工单；
- 工单创建链路解析并写入 membership；
- provider 客户深度态支持显式 `requesterId` 代客建单；
- 前端 provider 深度态建单页显示“客户申请人”选择器；
- 新增单测与 238 E2E。

### Phase 3：其他业务引用（已发布验证）

按风险顺序迁移：

1. 工单评论作者、附件上传人；
2. 工单 CC、通知收件人；
3. 审批人/审批链；
4. 审计 actor；
5. 问题/变更/服务请求创建人、处理人。

迁移采用双写 + 读回退 + 一致性巡检，避免一次性切换。

本轮 Phase 3 已落地的增量（2026-11-11，238 `1.8.0-dev.bf131540` 验证）：

| 表 | membership 引用 | 写路径 |
|---|---|---|
| `ticket_comments` | `user_membership_id` | 工单评论创建 |
| `attachments` / `ticket_attachments` | `uploaded_by_membership_id` | 通用附件上传、旧工单附件上传 |
| `ticket_ccs` | `user_membership_id` / `added_by_membership_id` | 工单 CC、BPMN CC |
| `ticket_approvals` | `approver_membership_id` / `delegate_to_membership_id` | 工单审批委派 |
| `approval_records` | `approver_membership_id` | 审批链委派 |
| `notifications` / `ticket_notifications` | `user_membership_id` | 通知创建、工单通知、MSP provider 双投递、投递命令 |
| `audit_logs` | `membership_id` | HTTP 审计中间件、登录/切换/租户拒绝审计 |
| `process_approval_decisions` | `actor_membership_id` | BPMN 人工审批/委派/加签 |

新增迁移：`20261111_membership_actor_references`（含 `_down`），回填按
`(user_id, tenant_id) -> user_tenant_memberships`，无匹配行保持 NULL。
复合外键先以 `NOT VALID` 上线，并同步写入
`internal/bootstrap/post_schema_constraints.sql`，避免 `ent.Schema.Create` 重建表后丢失。
当前未做：membership 身份的前端显式展示、BPMN `ccType=group/role` 对 provider
客户租户组织/角色的选人支持、home tenant 无 membership 兼容窗口收口。
`VALIDATE CONSTRAINT` 与问题/变更/服务请求的同口径迁移已由 Phase 4 第二批补齐。

### Phase 4：约束与清理（后续）

- [x] 补齐复合外键并 `VALIDATE CONSTRAINT`（第二批，21 条约束）；
- [x] 问题/变更/服务请求创建人、处理人 membership 迁移（第二批）；
- [x] 新增业务域 cross-tenant 行为矩阵与真实 PostgreSQL 约束巡检；
- [ ] 删除旧 `user_id` 业务引用或最终确定兼容列去留；
- [ ] 扩展 17 张 tenant 表的全量行为矩阵（当前为结构性占位 + 本批三域实测）；
- [ ] 统一 `allowedActions`、列表、通知、统计、审计按 membership 口径。

#### Phase 4 第一批：接收人租户校验（2026-11-12）

- 工单 CC、BPMN CC、审批委派、BPMN 任务 actor/target 统一为“active 账号 +
  home tenant 兼容 / 跨租户 active membership fail-closed”；
- provider 持有客户租户 membership 时可以接收 CC、接任审批、发起/接受 BPMN 委托；
- 无客户租户 membership 的外部账号不再能写入 `ticket_ccs` 或 `ticket_approvals`；
- CC 查询/管理权限改为 membership 优先，provider 角色读 `membership.msp_role`；
- CC 路由改用 `ticket:read/ticket:update`，避免 `workflow:write` 把 provider 与客户技术员
  挡在工单协作之外；前端入口同步改为 `ticket:update` 或 `workflow:write`；
- RBAC 路径预检在 `SmartCheckPermission` 未命中时，按同一映射回落到
  `AuthorizeResource`（JWT 主角色 + membership 合并角色的 DB 权限），与路由守卫同源；
- 审批记录已有 `approver_membership_id` 时，请求人 membership 必须一致；
- 无新增数据库迁移，复用 Phase 3 已落地的 membership 列与复合外键；
- 验证与边界见
  [2026-11-12-tenant-recipient-validation.md](../../review/2026-11-12-tenant-recipient-validation.md)。

#### Phase 4 第二批：业务创建人/处理人 membership 与约束 VALIDATE（2026-11-12）

**A. 问题 / 变更 / 服务请求 actors 迁移**

新增 `20261112_membership_business_actors`（含 `_down`）：

| 表 | membership 引用 | 写路径 |
|---|---|---|
| `problems` | `created_by_membership_id` / `assignee_membership_id` | 问题创建 / 处理人分配 |
| `changes` | `created_by_membership_id` / `assignee_membership_id` | 变更创建 / 分配处理人 |
| `service_requests` | `requester_membership_id` / `processor_membership_id` | 服务请求创建 / 进入履约 |

- 回填按 `(user_id, tenant_id) -> user_tenant_memberships`；无匹配保持 NULL；
- 新写入由 `handlers/common/membershipref` 统一解析：
  - handler 已从 RBAC 上下文取得 membership 时，解析失败或与目标租户不一致 → fail-closed；
  - 旧 service/repository 直接调用没有 membership 上下文时保持兼容，能解析则双写；
- 处理人/履约人无 membership 时回退旧校验（同租户 active 用户），保留迁移窗口；
- API 响应新增 `createdByMembershipId` / `assigneeMembershipId` /
  `requesterMembershipId` / `processorMembershipId`（`omitempty`），旧字段不变。

**B. 复合外键 VALIDATE 与跨租户矩阵**

新增 `20261112_membership_constraints_validate`：

- 覆盖 Phase 2–4 全部 21 条 membership 复合外键（工单/评论/附件/CC/审批/通知/审计 +
  问题/变更/服务请求）；
- 先清理“引用不存在或跨租户”的脏指针（置 NULL，保留 user_id 兼容列），再逐条
  `VALIDATE CONSTRAINT`，失败即报错，不静默降级；
- `post_schema_constraints.sql` 同步升级：Schema.Create 重建后补建约束时先清理脏引用，
  直接以 VALID 状态创建；已有 NOT VALID 约束自动清理并 VALIDATE；
- 新增 `tests/multi_tenant/business_membership_isolation_test.go`：覆盖问题/变更/
  服务请求的跨租户读、跨租户写、伪造 membership 引用与跨租户处理人分配；
- 新增 `database/rls/membership_constraints_integration_test.go`（`integration_rls`）：
  对真实 PostgreSQL 断言 21 条约束全部 `convalidated = true` 且无跨租户脏引用。
- 238 发布后发现事件转问题路径（`RootCauseAnalysisService` 直接 ent 创建）绕过 HTTP
  写路径，3 条 E2E 问题行缺少 creator membership；已在该服务解析 membership，并新增
  `20261112_membership_business_actors_reconcile` 幂等回填。最终版
  `1.8.0-dev.47bd56dc5` 复跑 E2E 46/46，21/21 约束 VALIDATED，6 类业务引用 missing=0。

**本批不做**

- 前端对创建人/处理人 membership 身份的显式展示；
- 服务请求审批记录（`service_request_approvals`）的 membership 迁移；
- PostgreSQL RLS policy 的全表启用（当前仍为应用层租户隔离 + 约束矩阵）。

## 5. 兼容与迁移

### 5.1 数据回填

1. 为缺少 home membership 的存量用户创建 `source=migration` 的 home membership；
2. 按 `(requester_id, tenant_id)` 回填 `requester_membership_id`；
3. 按 `(assignee_id, tenant_id)` 回填 `assignee_membership_id`；
4. 存量 `created_by_membership_id` 暂时回填为 requester membership；
5. 无匹配 membership 的行保持 NULL，进入巡检清单，不阻塞发布。

### 5.2 API 兼容

- 响应继续返回 `requesterId` / `assigneeId`（`users.id`）；
- 新增 `requesterMembershipId` / `assigneeMembershipId`；
- 客户端不需要提交 membership ID；
- provider 代客建单提交客户 `requesterId`，由服务端解析 membership；
- 前端只在 `accountKind=provider && tenantId != homeTenantId` 时显示申请人选择器。

## 6. 安全与 RLS

- membership 必须是 active、未删除、未过期，且 tenant 匹配；
- provider membership 必须来自 allocation/invite（现有切换授权链）；
- 跨租户 membership 引用必须 400/403，不能返回目标资源存在性；
- 所有查询带 `tenant_id`，复合外键最终保证 `membership.tenant_id == row.tenant_id`；
- 撤销 allocation 后，membership 软删，下一次请求 fail-closed；
- 不允许客户端直接提交 `membershipId` 作为授权依据。

## 7. 验收标准

- [x] provider 深度态使用客户 `requesterId` 建单成功，返回 200/201；
- [x] 未提供 `requesterId` 的 provider 建单返回 400，不再出现 404「申请人不存在或不可用」；
- [x] 客户用户不能通过 `requesterId` 伪造他人建单；
- [x] 跨租户 requester membership 被拒绝；
- [x] 工单单张/批量/智能分配同步更新 `assignee_membership_id`；
- [x] `/auth/me` 返回目标租户 `membershipId/accountKind`；
- [x] 工单响应包含 membership 引用，旧 `requesterId/assigneeId` 保持兼容；
- [x] 回填 SQL 在 238 demo 执行成功，无不可解释 NULL（active tickets 72/72，mismatch=0）；
- [x] Go 单测、前端 tsc/eslint、demo E2E、文档门禁全绿；
- [x] 238 部署后 6/6 容器 healthy，发布证据归档。

## 8. 风险与决策

| 风险 | 处理 |
|---|---|
| 存量工单无匹配 membership | 回填 + 巡检 + 读路径回退，不阻塞发布 |
| 复合外键导致历史脏数据失败 | 过渡期只建索引/`NOT VALID` FK，验证后再 VALIDATE |
| provider 代客建单权限过宽 | 要求 provider membership + 目标租户 `ticket:create` + 客户 active membership |
| 前端误显示申请人选择器 | 只在 provider 深度态显示；普通客户忽略 `requesterId` |
| 后续表迁移范围过大 | Phase 3 分表双写/回退，不在本轮强行全量切换 |

## 9. 本轮实现状态

本轮已落地 Phase 1 + Phase 2 工单主链路、Phase 3 工单域 actor/recipient 引用，
以及 Phase 4 第一批接收人租户校验与 RBAC 预检同源；其他业务域迁移保留为后续计划。

```text
runtime_commit=5639c21c92e831ca35a5251cef87ddfe234e49f9
version=1.8.0-dev.5639c21c9
deploy_branch=deploy/full-5639c21c9
e2e=46/46
backend=go test ./service ./handlers/ticket ./handlers/auth ./handlers/common ./middleware ./dto ./repository/ticket ./migration
frontend=npx tsc --noEmit + eslint（含 E2E 断言器）
```

实现提交：

- `5c8f32a62` feat(multi-tenant): use membership as tenant identity
- `49a2ad115` fix(ticket): detect provider switch scope from JWT source
- `ad8788051` fix(change): return 404 when deleting missing change
- `e1599ffdb` fix(ticket): persist assignee membership on assignment
- `ae0709619` feat(multi-tenant): add membership refs for ticket actors
- `bf1315408` fix(audit): attribute direct service audits to membership
- `c651362e2` feat(ticket): enforce tenant membership for CC and delegation recipients
- `ce9323644` fix(ticket): authorize CC with ticket:update instead of workflow:write
- `9aa18e13c` chore(authz): regenerate precheck for CC ticket permissions
- `08426a0c2` fix(ticket): map workflow validation errors to stable HTTP status
- `5639c21c9` fix(rbac): align precheck with membership role DB permissions

发布与运行态证据：
[2026-11-12-demo-full-deploy-5639c21c9.md](../../review/evidence/2026-11-12-demo-full-deploy-5639c21c9.md)。

Phase 3/4 剩余（未完成）：

- 问题/变更/服务请求创建人、处理人的 membership 引用迁移；
- 复合外键 `VALIDATE CONSTRAINT` 与全量 RLS/cross-tenant 测试矩阵；
- 前端对评论/附件/审批/通知 membership 身份的显式展示（当前仅下发字段）；
- 平台管理员跨租户审计的 membership 归属口径（当前保留 NULL + `target_tenant_id`）。

已关闭：

- 工单 CC、BPMN CC、审批委派、BPMN 任务 actor/target 的跨租户 membership 校验；
- CC 路由错误的 `workflow:write` 前置门槛；
- RBAC 预检只看 JWT 主角色导致 membership 角色 DB 权限不生效。

Phase 3 本轮验证（238）：

- `go test ./service -count=1` 全包通过（含评论/附件/CC/通知/BPMN 决策 membership 断言）；
- `go test ./middleware ./dto ./service/bpmn ./handlers/common ./handlers/auth` 通过；
- `go test` 定向编译 `handlers/ticket_comment/notification/approval/bpmn` 通过；
- `npx tsc --noEmit`、改动文件 ESLint 通过；
- 新增迁移与 `post_schema_constraints.sql` 通过 PostgreSQL AST 语法解析（pglast）；
- 238 部署 `1.8.0-dev.5639c21c9`，6/6 healthy、readyz ready、
  租户错配 0；demo E2E **46/46**（含 B4b 跨租户 CC 边界）。
