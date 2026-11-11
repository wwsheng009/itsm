# C 方案 Phase 3：actor/recipient membership 引用（2026-11-11）

> **Status: current (implemented-local, pending 238 deploy)**
> **范围**：工单评论、附件、CC、审批、通知、审计、BPMN 审批决策
> **设计基线**：[membership-tenant-identity-c-plan.md](../multi-tenant/plan/membership-tenant-identity-c-plan.md)
> **上一运行态**：`http://172.18.3.238:8089` / `1.8.0-dev.e1599ffd`
> **本轮未发布**：需全量构建、迁移、demo E2E 后才能改标 `deployed/verified`

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
| `audit_logs` | `membership_id` 写入 | HTTP 审计中间件、登录/切换/租户拒绝审计 |
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

## 3. 验证（本地）

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
```

新增测试：

- `service/ticket_comment_membership_test.go`
- `service/ticket_cc_membership_test.go`
- `service/attachment_service_test.go`（上传人 membership 断言）
- `service/notification_service_test.go`（收件人 membership 断言）
- `service/bpmn_process_engine_ext_test.go`（BPMN 决策 actor membership 断言）
- `middleware/audit_test.go`（审计行 membership 断言）

说明：本机 Go 编译仍受内存限制，使用 `-gcflags=all=-l` 并单包串行执行；这是测试运行器
资源约束，不是产品失败。

## 4. 剩余与发布门槛

1. 238 全量构建与发布新 tag；确认 `itsm-init-demo` 执行
   `20261111_membership_actor_references` 且六容器 healthy。
2. 运行数据巡检：各新列 NULL 率、`(membership_id, tenant_id)` 错配数、复合外键是否已建。
3. 复跑 45 条 demo E2E；补充评论/附件/CC/审批/通知/审计 membership 的运行态断言。
4. 视数据质量执行 `VALIDATE CONSTRAINT`。
5. 后续再迁移问题/变更/服务请求的 actor/assignee 引用与前端显式展示。