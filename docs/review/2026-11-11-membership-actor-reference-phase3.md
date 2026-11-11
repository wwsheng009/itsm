# C 方案 Phase 3：actor/recipient membership 引用（2026-11-11）

> **Status: current (deployed & verified on 238)**
> **范围**：工单评论、附件、CC、审批、通知、审计、BPMN 审批决策
> **设计基线**：[membership-tenant-identity-c-plan.md](../multi-tenant/plan/membership-tenant-identity-c-plan.md)
> **当前运行态**：`http://172.18.3.238:8089` / `1.8.0-dev.bf131540`
> **部署证据**：[2026-11-11-demo-full-deploy-bf131540.md](./evidence/2026-11-11-demo-full-deploy-bf131540.md)

## 1. 本轮落地内容

| 表 | 新增 membership 引用 | 主要写路径 |
|---|---|---|
| `ticket_comments` | `user_membership_id` | 工单评论创建 |
| `attachments` | `uploaded_by_membership_id` | 通用附件上传 |
| `ticket_attachments` | `uploaded_by_membership_id` | 旧工单附件上传 |
| `ticket_ccs` | `user_membership_id`、`added_by_membership_id` | 工单 CC / BPMN CC |
| `ticket_approvals` | `approver_membership_id`、`delegate_to_membership_id` | 工单审批委派 |
| `approval_records` | `approver_membership_id` | 审批链委派 |
| `notifications` | `user_membership_id` | 通用通知 / MSP provider 双投递 / 投递命令 |
| `ticket_notifications` | `user_membership_id` | 工单通知 / MSP provider 双投递 / 投递命令 |
| `audit_logs` | `membership_id` 写入 | HTTP 审计中间件、登录/切换/租户拒绝审计、附件直写审计、MSP 直写审计 |
| `process_approval_decisions` | `actor_membership_id` | BPMN 人工审批、委派、加签 |

API 响应增量：

- `TicketCommentResponse.userMembershipId`
- `TicketAttachmentResponse.uploadedByMembershipId`
- `AttachmentRef.uploadedByMembershipId`
- `WorkbenchTicketCommentItem.userMembershipId`
- `Notification.userMembershipId`
- `TicketNotificationResponse.userMembershipId`
- `AuditLog.membershipId`（并支持 `membershipId` 查询过滤）

## 2. 迁移与约束

- 新迁移：`itsm-backend/migrations/20261111_membership_actor_references.sql`
- 回退：`itsm-backend/migrations/20261111_membership_actor_references_down.sql`
- 回填规则：`(user_id, tenant_id) -> user_tenant_memberships(id, tenant_id)`，
  仅匹配未软删 membership；无匹配保持 NULL。
- 复合外键：`(membership_id, tenant_id) -> user_tenant_memberships(id, tenant_id)`，
  全部 `NOT VALID` 上线，避免历史脏数据阻塞发布。
- **关键修复**：`ent.Schema.Create` 会重建受管表并丢弃未知约束，因此 Phase 2 的三个
  `tickets.*_membership_id` 外键与 Phase 3 新外键都已补入
  `internal/bootstrap/post_schema_constraints.sql`，每次启动重建后自动补齐。

## 3. 验证

```text
go test -p=1 -gcflags=all=-l ./service -count=1                  PASS
go test -p=1 -gcflags=all=-l ./middleware ./dto -count=1         PASS
go test -p=1 -gcflags=all=-l ./service/bpmn -count=1            PASS
go test -p=1 -gcflags=all=-l ./handlers/common ./handlers/auth  PASS
go test -p=1 -gcflags=all=-l ./handlers/ticket_comment \
  ./handlers/notification ./handlers/approval ./handlers/bpmn    PASS
npx tsc --noEmit                                                 PASS
npx eslint <changed frontend files>                              PASS
pglast parse(migration + post_schema_constraints.sql)            PASS
238 全量部署 + 迁移 + 数据巡检                                  PASS
238 demo E2E 00–06                                             45/45
```

新增测试：

- `service/ticket_comment_membership_test.go`
- `service/ticket_cc_membership_test.go`
- `service/attachment_service_test.go`（上传人 membership 断言）
- `service/notification_service_test.go`（收件人 membership 断言）
- `service/bpmn_process_engine_ext_test.go`（BPMN 决策 actor membership 断言）
- `middleware/audit_test.go`（审计行 membership 断言）
- `tests/e2e/demo/03-workflows.spec.ts`（评论、CC 记录、provider 回复 membership 运行态断言）
- `tests/e2e/demo/06-attachment-access.spec.ts`（上传响应 membership 运行态断言）

说明：本机 Go 编译仍受内存限制，使用 `-gcflags=all=-l` 并单包串行执行；这是测试运行器
资源约束，不是产品失败。

## 4. 238 发布结果

```text
version=1.8.0-dev.bf131540
commit=bf13154089aadda6ecfdf5e163840c4ff7c57329
deployed_at=2026-11-11T16:25:33Z
containers=6/6 healthy
readyz=ready
migration=20261111_membership_actor_references applied
membership_fks=15（NOT VALID）
tenant_mismatch=0
```

运行态新增数据：评论 9/9、附件 2/2、CC 1/1（双方 membership）、工单通知 78/78、
通知 4/4。审计窗口 224 行中 174 行有 membership；50 行为登录前 /
切换拒绝 / 平台管理员跨租户（目标租户无 membership），不伪造身份。

## 5. 剩余

1. 问题/变更/服务请求的 actor/assignee 引用迁移；
2. 视数据质量执行 `VALIDATE CONSTRAINT`；
3. 前端对评论/附件/审批/通知 membership 身份的显式展示；
4. 平台管理员跨租户审计的 membership 归属口径（当前保留 NULL + target_tenant 可追溯）。