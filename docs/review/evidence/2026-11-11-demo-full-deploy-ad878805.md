# 238 Demo 全量部署证据（2026-11-11 · 1.8.0-dev.ad878805）

> **Status: historical** — 已被 `1.8.0-dev.e1599ffd` 取代，见
> [2026-11-11-demo-full-deploy-e1599ffd.md](./2026-11-11-demo-full-deploy-e1599ffd.md)。

> **状态**：success
> **环境**：`http://172.18.3.238:8089`（`itsm-demo`，`saas_msp`）
> **部署来源**：当前工作区 HEAD `ad8788051e75e81ecc5085759a5b737566cc267e`
> **部署 worktree**：`/home/n9e/itsm/repo-e2e-6745c998`，branch `deploy/full-ad878805`
> **上一版本**：`1.8.0-dev.49a2ad11`

## 1. 本轮变更

### 1.1 C 方案：membership 作为租户内身份

- 新增 `middleware.ResolveActiveMembership`，RBAC 请求上下文写入
  `membership_id/account_kind/source`；
- Login / Refresh / SwitchTenant / `/auth/me` 返回目标租户的
  `membershipId/accountKind/membershipSource`；
- `tickets` 增加 `requester_membership_id`、`assignee_membership_id`、
  `created_by_membership_id`；
- 迁移 `20261111_membership_tenant_identity` 补齐缺失 home membership 并回填工单；
- provider 深度态支持显式客户 `requesterId` 代客建单；
- 前端 provider 深度态建单页增加“客户申请人”选择器；
- 旧 `requesterId/assigneeId` 保留，membership 字段为兼容增量。

### 1.2 回归修复

- `jwt_tenant_source`：TenantMiddleware 的 `tenant_source` 表示租户解析来源（jwt），
  不能用于判断显式切换；改由 AuthMiddleware 从 JWT claim 写入
  `jwt_tenant_source`，provider 代客建单只在 `switch + customer tenant` 生效。
- 变更重复删除：删除不存在/已被并发删除的变更由 500 改为 404。

## 2. 发布结果

```text
release_status=success
deployed_at=2026-11-11T11:51:53Z
version=1.8.0-dev.ad878805
git_branch=deploy/full-ad878805
git_commit=ad8788051e75e81ecc5085759a5b737566cc267e
```

| 项 | 结果 |
|---|---|
| `/api/v1/version` | `1.8.0-dev.ad878805` |
| `/api/v1/readyz` | `ready=true`；schema `20261111_service_request_delete_grants` |
| migration | `20261111_membership_tenant_identity` 已应用 |
| backend / worker | `itsm-backend:1.8.0-dev.ad878805`，healthy |
| frontend | `itsm-frontend:1.8.0-dev.ad878805`，healthy |
| ai-service | `itsm-ai-service:1.8.0-dev.ad878805`，healthy（复用已验证镜像） |
| nginx / redis | healthy |
| init/migration | `itsm-init-demo` exit=0 |
| 附件冒烟 | 上传/预览/SHA256/删除通过 |
| 后端错误日志 | 发布后 30 分钟无 5xx、`level=error`、panic |

## 3. 数据回填核验

```text
migration_row = 20261111_membership_tenant_identity
active_tickets = 72
requester_membership_not_null = 72
created_by_membership_not_null = 72
assignee_membership_not_null = 62
tenant_membership_mismatch = 0
```

发布前还用事务回滚方式在 238 `itsm_demo` 演练过迁移：
`332` 张历史工单完成 requester/creator 回填，rollback 后数据库未变更。

## 4. E2E 复跑

按 spec 独立进程执行，最终版本累计 **45/45**：

| Spec | 结果 |
|---|---|
| `00-smoke` | 6/6 |
| `01-permissions` | 8/8 |
| `02-associations` | 8/8 |
| `03-workflows` | 7/7 |
| `04-ui-ux` | 11/11 |
| `05-multi-tenant-business` | 4/4（含 MT-4 provider 代客建单） |
| `06-attachment-access` | 1/1 |
| **合计** | **45/45** |

关键回归：

- MT-4：provider 深度态显式客户 `requesterId` 建单成功，返回
  `requesterMembershipId > 0`、`createdByMembershipId > 0`；
- MT-4：provider 缺失 `requesterId` 返回 400；
- MT-4：`/auth/me` 返回目标租户 `membershipId`、`accountKind=provider`；
- MT-4：provider 提交其他客户租户的 `requesterId` 被拒绝（400/404）；
- 04：重复/并发删除变更返回 404，不再 500。

> `04-ui-ux` 在连续多轮整库分组运行时出现过瞬时页面等待/宽度抖动，单独完整复跑
> 11/11 通过；最终版本按 spec 独立复跑全绿。

## 5. 镜像与回滚

```text
backend    sha256:a610565d62fc835db698c214f853f44c91f781b9637cb4425e2fe1ee1a018f9f
frontend   sha256:f22a65fe0675a307ed2f47085eff73e6e6ddad3a8cc242e96a77954b3400fa96
ai-service sha256:f99367604826ed7fa1541bfa8868dee1eb7492b4307d4e55f050d35b6a2d3301
```

- 备份/回滚根目录：`/home/n9e/itsm/repo-e2e-6745c998/backups/demo-release-1.8.0-dev.ad878805-20261111T114926Z`
- 环境备份：`backups/demo-release-1.8.0-dev.ad878805-20261111T114926Z/.env.demo.before`
- 数据库备份：`backups/demo-release-1.8.0-dev.ad878805-20261111T114926Z/itsm_demo.dump`
- 发布证据：`backups/demo-release-1.8.0-dev.ad878805-20261111T114926Z/release.txt`

## 6. 关联证据

- [C 方案设计](../../multi-tenant/plan/membership-tenant-identity-c-plan.md)
- [E2E 报告](../2026-11-10-msp-demo-e2e-validation.md)
- [运行摘要](./2026-11-10-msp-demo-e2e/run-summary.md)
- [上一轮 49a2ad11](./2026-11-11-demo-full-deploy-49a2ad11.md)
- 测试套件：`itsm-frontend/tests/e2e/demo/`
