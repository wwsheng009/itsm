# C 方案：membership 作为租户内身份（2026-11-11）

> **状态**：implemented-deployed（Phase 1 + Phase 2 工单垂直切片已完成并发布 238）
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

### Phase 3：其他业务引用（已本地实现，待 238 发布验证）

按风险顺序迁移：

1. 工单评论作者、附件上传人；
2. 工单 CC、通知收件人；
3. 审批人/审批链；
4. 审计 actor；
5. 问题/变更/服务请求创建人、处理人。

迁移采用双写 + 读回退 + 一致性巡检，避免一次性切换。

本轮 Phase 3 已落地的增量（2026-11-11）：

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
当前未做：`VALIDATE CONSTRAINT`、问题/变更/服务请求的同口径迁移、membership 身份的
前端显式展示与租户级 RLS 全矩阵。

### Phase 4：约束与清理（后续）

- 补齐复合外键并 `VALIDATE CONSTRAINT`；
- 删除旧 `user_id` 业务引用或保留为兼容列；
- 完成 RLS/cross-tenant 测试矩阵；
- 统一 `allowedActions`、列表、通知、统计、审计按 membership 口径。

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

本轮已落地 Phase 1 + Phase 2 工单主链路，Phase 3/4 保留为后续迁移计划。

```text
runtime_commit=e1599ffdb5fdf56b44aa783261fe6535bf5714f2
version=1.8.0-dev.e1599ffd
deploy_branch=deploy/full-e1599ffd
e2e=45/45
backend=go test ./service ./handlers/ticket ./handlers/auth ./handlers/common ./middleware ./dto ./repository/ticket ./migration
frontend=npx tsc --noEmit + eslint
```

实现提交：

- `5c8f32a62` feat(multi-tenant): use membership as tenant identity
- `49a2ad115` fix(ticket): detect provider switch scope from JWT source
- `ad8788051` fix(change): return 404 when deleting missing change
- `e1599ffdb` fix(ticket): persist assignee membership on assignment
- `ae0709619` feat(multi-tenant): add membership refs for ticket actors

发布与运行态证据：
[2026-11-11-demo-full-deploy-e1599ffd.md](../../review/evidence/2026-11-11-demo-full-deploy-e1599ffd.md)。

Phase 3 剩余（未完成）：

- 问题/变更/服务请求创建人、处理人的 membership 引用迁移；
- 复合外键 `VALIDATE CONSTRAINT` 与全量 RLS/cross-tenant 测试矩阵；
- 前端对评论/附件/审批/通知 membership 身份的显式展示（当前仅下发字段）。

Phase 3 本轮验证（本地，尚未发布到 238）：

- `go test ./service -count=1` 全包通过（含评论/附件/CC/通知/BPMN 决策 membership 断言）；
- `go test ./middleware ./dto ./service/bpmn ./handlers/common ./handlers/auth ./handlers/ticket_comment ./handlers/notification ./handlers/approval ./handlers/bpmn` 通过；
- `npx tsc --noEmit`、改动文件 ESLint 通过；
- 新增迁移与 `post_schema_constraints.sql` 已通过 PostgreSQL AST 语法解析（pglast）。
