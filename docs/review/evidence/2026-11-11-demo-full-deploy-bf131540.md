# 238 Demo 全量部署证据（2026-11-11 · 1.8.0-dev.bf131540）

> **Status: historical** — 已被 `1.8.0-dev.5639c21c9` 取代，见
> [2026-11-12-demo-full-deploy-5639c21c9.md](./2026-11-12-demo-full-deploy-5639c21c9.md)。
> **环境**：`http://172.18.3.238:8089`（`itsm-demo`，`saas_msp`）
> **部署来源**：当前工作区 HEAD `bf13154089aadda6ecfdf5e163840c4ff7c57329`
> **部署 worktree**：`/home/n9e/itsm/repo-e2e-6745c998`，branch `deploy/full-bf131540`
> **中间版本**：`1.8.0-dev.ef1b5010`（首轮 45/45；运行态审计发现附件/MSP 直写审计缺 membership，修复后最终以 `bf131540` 重发并复跑）
> **上一稳定版本**：`1.8.0-dev.e1599ffd`

## 1. 本轮变更

### 1.1 C 方案 Phase 3：actor/recipient membership

- 工单评论作者：`ticket_comments.user_membership_id`
- 附件上传人：`attachments.uploaded_by_membership_id`、
  `ticket_attachments.uploaded_by_membership_id`
- 工单 CC：`ticket_ccs.user_membership_id / added_by_membership_id`
- 工单审批：`ticket_approvals.approver_membership_id / delegate_to_membership_id`、
  `approval_records.approver_membership_id`
- 通知收件人：`notifications.user_membership_id`、
  `ticket_notifications.user_membership_id`
- BPMN 决策：`process_approval_decisions.actor_membership_id`
- 审计：HTTP 审计中间件、登录/切换/租户拒绝审计、附件直写审计、MSP 工作台/任务/
  记录写/模板下发/帮助空间/工单类型直写审计写 `audit_logs.membership_id`

### 1.2 迁移与约束

- 迁移：`20261111_membership_actor_references`（含 `_down`）
- 回填：按 `(user_id, tenant_id) -> user_tenant_memberships`，无匹配保持 NULL
- 复合外键：15 个 `fk_*membership*` 全部存在，先 `NOT VALID`；同时写入
  `internal/bootstrap/post_schema_constraints.sql`，schema rebuild 后自动重建
- Phase 2 的 3 个 `tickets.*_membership_id` 外键也补入 post-schema，避免 ent 重建丢失

## 2. 发布结果

```text
release_status=success
deployed_at=2026-11-11T16:25:33Z
version=1.8.0-dev.bf131540
git_branch=deploy/full-bf131540
git_commit=bf13154089aadda6ecfdf5e163840c4ff7c57329
tag=demo-1.8.0-dev.bf131540
```

| 项 | 结果 |
|---|---|
| `/api/v1/version` | `1.8.0-dev.bf131540` |
| `/api/v1/readyz` | `ready=true`；schema `20261111_service_request_delete_grants`（迁移台账另行核验） |
| backend / worker | `itsm-backend:1.8.0-dev.bf131540`，healthy |
| frontend | `itsm-frontend:1.8.0-dev.bf131540`，healthy |
| ai-service | `itsm-ai-service:1.8.0-dev.bf131540`，healthy（复用已验证镜像） |
| nginx / redis | healthy |
| init/migration | `itsm-init-demo` exit=0 |
| 附件冒烟 | 上传/预览/SHA256/删除通过 |
| 后端日志 | 最终发布后 45 分钟无 5xx、`level=error`、panic |

## 3. 数据核验

迁移台账：

```text
20261111_membership_actor_references  2026-11-11 15:36:25
20261111_membership_tenant_identity   2026-11-11 11:14:53
20261111_service_request_delete_grants 2026-11-11 00:40:41
```

全量存量核验（最终发布后）：

```text
ticket_comments          224 / 224 有 membership
attachments               61 /  61 有 membership
ticket_ccs                 2 /   2 有接收人+添加人 membership
notifications            558 / 558 有 membership
ticket_notifications    2417 /2417 有 membership
process_approval_decisions 18 / 18 有 actor membership
tenant mismatch            0（全部表）
```

最终发布后新增行：

```text
ticket_comments            9 / 9
attachments                2 / 2
ticket_ccs                 1 / 1（接收人 + 添加人均有）
ticket_notifications      78 / 78
notifications              4 / 4
audit_logs               224 行，174 行有 membership
```

审计 NULL 说明：本窗口 50 行无 membership，主要是 `auth.login`（43，登录前无租户身份）、
`tenant.switch_denied`（3，目标租户无 membership）、平台管理员跨租户清理（4，目标租户无
membership）。这些行保留 `tenant_id/user_id`，不伪造目标租户 membership。

## 4. E2E 复跑

按 spec 独立进程执行，最终版本累计 **45/45**：

| Spec | 结果 |
|---|---|
| `00-smoke` | 6/6 |
| `01-permissions` | 8/8 |
| `02-associations` | 8/8 |
| `03-workflows` | 7/7（B3 新增评论 `userMembershipId`、CC 记录断言；B4 provider 回复 membership） |
| `04-ui-ux` | 11/11 |
| `05-multi-tenant-business` | 4/4 |
| `06-attachment-access` | 1/1（新增上传响应 `uploadedByMembershipId > 0`） |
| **合计** | **45/45** |

## 5. 镜像与回滚

```text
backend    sha256:b4e7e3a5a77f9d91d1b7f2d3b91a19898f36ff31e0ba2ff312c9bbb4e1351a4b
frontend   sha256:dc4b4798c3cdc27c664d4c8a6e42633649e81c895be300591183efecb120aee9
ai-service sha256:f99367604826ed7fa1541bfa8868dee1eb7492b4307d4e55f050d35b6a2d3301
```

- 备份/回滚根目录：`/home/n9e/itsm/repo-e2e-6745c998/backups/demo-release-1.8.0-dev.bf131540-20261111T162032Z`
- 环境备份：`.../.env.demo.before`
- 数据库备份：`.../itsm_demo.dump`
- 发布证据：`.../release.txt`
- 中间版本备份：`backups/demo-release-1.8.0-dev.ef1b5010-20261111T151930Z`

## 6. 关联证据

- [C 方案设计](../../multi-tenant/plan/membership-tenant-identity-c-plan.md)
- [Phase 3 实现与验证](../2026-11-11-membership-actor-reference-phase3.md)
- [E2E 报告](../2026-11-10-msp-demo-e2e-validation.md)
- [运行摘要](./2026-11-10-msp-demo-e2e/run-summary.md)
- [上一轮 e1599ffd](./2026-11-11-demo-full-deploy-e1599ffd.md)
- 测试套件：`itsm-frontend/tests/e2e/demo/`