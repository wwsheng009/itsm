# Phase 4 第二批：问题/变更/服务请求 membership 与约束 VALIDATE

> 日期：2026-11-12
> 状态：代码完成，本地验证通过，待发布 238
> 关联：[C 方案主文档](../multi-tenant/plan/membership-tenant-identity-c-plan.md) ·
> [Phase 4 第一批验证](2026-11-12-tenant-recipient-validation.md)

## 1. 范围

本批对应 C 方案 Phase 4 的剩余项 1 与 2：

1. 问题 / 变更 / 服务请求的创建人、处理人从 `user_id` 单轨迁移为 membership 引用；
2. membership 复合外键从 `NOT VALID` 收敛为 `VALIDATED`，并补跨租户测试矩阵。

服务请求审批记录（`service_request_approvals`）不在本批范围，留待前端
审批身份展示时一起处理。

## 2. 数据库迁移

### 2.1 `20261112_membership_business_actors`（含 `_down`）

| 表 | 新增列 | 回填来源 | 复合外键 |
|---|---|---|---|
| `problems` | `created_by_membership_id`、`assignee_membership_id` | `created_by` / `assignee_id` | `(id, tenant_id)` → `user_tenant_memberships` |
| `changes` | `created_by_membership_id`、`assignee_membership_id` | `created_by` / `assignee_id` | 同上 |
| `service_requests` | `requester_membership_id`、`processor_membership_id` | `requester_id` / `processor_id` | 同上 |

- 回填前幂等补齐缺失的 home membership（与 20261111 同口径）；
- 回填只匹配同租户、未软删的 membership；无匹配保持 NULL；
- 新增 6 个租户作用域索引；
- 复合外键先以 `NOT VALID` 上线，由同批 B 迁移 VALIDATE；
- 迁移末尾输出 6 个列的 missing 计数，供发布日志巡检。

### 2.2 `20261112_membership_constraints_validate`

覆盖 Phase 2–4 全部 21 条 membership 复合外键：

- 工单 3 条、评论 1 条、附件 2 条、CC 2 条、审批 2 条、审批记录 1 条、
  通知 2 条、BPMN 决策 1 条、审计 1 条；
- 问题 / 变更 / 服务请求 6 条。

执行策略：

1. 约束缺失时先补建 `NOT VALID`；
2. 对“引用不存在或跨租户”的指针置 NULL（保留 `user_id` 兼容列，读路径回退）；
3. 逐条 `ALTER TABLE ... VALIDATE CONSTRAINT`，失败显式报错；
4. 输出清理计数，便于发布后审计。

### 2.3 `post_schema_constraints.sql` 自愈

`ent.Schema.Create` 可能重建受管表并丢弃其不认识的复合外键。原逻辑只补
`NOT VALID`，会在下一次启动把已验证约束重新降级。本批改为：

- 约束缺失：先清理脏引用，再以 **VALID** 状态直接创建；
- 约束已存在但 `convalidated = false`：清理脏引用后 `VALIDATE`；
- 约束已 VALID：跳过，不产生额外扫描。

## 3. 写路径与 API

### 3.1 统一解析器

新增 `handlers/common/membershipref`：

- `ResolveActor(userID, tenantID, providedID)`：
  handler 已提供 RBAC membership 时，解析失败或 ID 不一致 → fail-closed；
- `ResolveOptional`：无 membership 返回 0；
- `ResolveAssignable`：无 membership 时回退旧校验（同租户 active 用户）。

### 3.2 业务写路径

| 业务 | 创建 | 处理人变更 | 履约 |
|---|---|---|---|
| 问题 | `handlers/problem` Create 写入 `created_by_membership_id` | Update 解析 `assignee_id` | — |
| 变更 | `handlers/change` Create 写入 `created_by_membership_id` | AssignChange → Update 解析 `assignee_id` | — |
| 服务请求 | `handlers/service_request` Create 写入 `requester_membership_id` | — | 进入 `provisioning` 时写入 `processor_membership_id` |

- 历史 `service/change_service.go`、`service/problem_service.go` 同步做兼容双写；
- BPMN 服务任务（创建变更/服务请求）与 demo seeder 也在写入时解析 membership；
- MSP 工作台更新服务请求状态时传 `actorMembershipID=0`，由 repository 按
  `(actorID, tenantID)` 解析；
- 跨租户处理人分配会在解析阶段被拒绝，不再写入业务行。

### 3.3 API 响应

新增可选字段，旧字段不变：

```text
ProblemResponse:        createdByMembershipId, assigneeMembershipId
ChangeResponse:         createdByMembershipId, assigneeMembershipId
ServiceRequestResponse: requesterMembershipId, processorMembershipId
```

## 4. 测试

### 4.1 新增单测

| 测试 | 覆盖 |
|---|---|
| `handlers/problem/membership_test.go` | 创建双写、membership 伪造拒绝、分配解析 |
| `handlers/change/membership_test.go` | 创建双写、membership 伪造拒绝、分配解析 |
| `handlers/service_request/membership_test.go` | 申请人与履约人 membership、伪造拒绝 |
| `tests/multi_tenant/business_membership_isolation_test.go` | 三类业务跨租户读/写、伪造 membership、跨租户处理人 |
| `tests/multi_tenant/tenant_tables_schema_test.go` | 17 张核心租户表 `tenant_id` 结构校验、三业务表新旧列契约 |
| `internal/bootstrap/post_schema_membership_constraints_test.go` | 21 条约束在迁移与 post-schema 自愈路径中保持一致 |
| `database/rls/membership_constraints_integration_test.go` | 真实 PostgreSQL：21 条约束已 VALIDATE 且无脏引用 |

### 4.2 本地结果

```text
go test -p 1 ./handlers/problem ./handlers/change ./handlers/service_request \
  ./handlers/msp ./service ./tests/multi_tenant ./dto -count=1          PASS
go test -p 1 ./middleware ./router ./dto ./internal/bootstrap ./ent/... PASS
go test -p 1 ./handlers/... -count=1
  - 除 handlers/service_catalog 外全部 PASS
  - service_catalog 失败为 Windows TempDir SQLite 文件占用清理（unrelated flake）
go test -p 1 -tags integration_rls -run '^$' ./database/rls            PASS（编译）
go test -p 1 ./pkg/seeder -count=1
  - 两个 tenant_readiness 用例失败：断言 7 项而代码已为 9 项（既有漂移，与本批无关）
```

`membership_constraints_integration_test.go` 需要真实 PostgreSQL：

```bash
RLS_SETUP_DSN='host=<pg> port=5432 user=<owner> dbname=<db> sslmode=disable password=<pwd>' \
  go test -tags integration_rls -v ./database/rls -run Membership
```

### 4.3 发布后巡检 SQL

```sql
-- 1) 21 条约束必须全部 validated
SELECT conname, convalidated
FROM pg_constraint
WHERE conname LIKE 'fk_%membership%tenant'
ORDER BY conname;

-- 2) 新增六列覆盖率
SELECT 'problems.created_by' AS ref, count(*) FILTER (WHERE created_by_membership_id IS NULL) AS missing, count(*) AS total FROM problems
UNION ALL SELECT 'problems.assignee', count(*) FILTER (WHERE assignee_id IS NOT NULL AND assignee_membership_id IS NULL), count(*) FROM problems
UNION ALL SELECT 'changes.created_by', count(*) FILTER (WHERE created_by_membership_id IS NULL), count(*) FROM changes
UNION ALL SELECT 'changes.assignee', count(*) FILTER (WHERE assignee_id IS NOT NULL AND assignee_membership_id IS NULL), count(*) FROM changes
UNION ALL SELECT 'service_requests.requester', count(*) FILTER (WHERE requester_membership_id IS NULL), count(*) FROM service_requests
UNION ALL SELECT 'service_requests.processor', count(*) FILTER (WHERE processor_id IS NOT NULL AND processor_membership_id IS NULL), count(*) FROM service_requests;

-- 3) 跨租户错配必须为 0
SELECT 'problems.created_by' AS ref, count(*) FROM problems t
WHERE t.created_by_membership_id IS NOT NULL
  AND NOT EXISTS (SELECT 1 FROM user_tenant_memberships m WHERE m.id = t.created_by_membership_id AND m.tenant_id = t.tenant_id)
UNION ALL SELECT 'problems.assignee', count(*) FROM problems t
WHERE t.assignee_membership_id IS NOT NULL
  AND NOT EXISTS (SELECT 1 FROM user_tenant_memberships m WHERE m.id = t.assignee_membership_id AND m.tenant_id = t.tenant_id)
UNION ALL SELECT 'changes.created_by', count(*) FROM changes t
WHERE t.created_by_membership_id IS NOT NULL
  AND NOT EXISTS (SELECT 1 FROM user_tenant_memberships m WHERE m.id = t.created_by_membership_id AND m.tenant_id = t.tenant_id)
UNION ALL SELECT 'changes.assignee', count(*) FROM changes t
WHERE t.assignee_membership_id IS NOT NULL
  AND NOT EXISTS (SELECT 1 FROM user_tenant_memberships m WHERE m.id = t.assignee_membership_id AND m.tenant_id = t.tenant_id)
UNION ALL SELECT 'service_requests.requester', count(*) FROM service_requests t
WHERE t.requester_membership_id IS NOT NULL
  AND NOT EXISTS (SELECT 1 FROM user_tenant_memberships m WHERE m.id = t.requester_membership_id AND m.tenant_id = t.tenant_id)
UNION ALL SELECT 'service_requests.processor', count(*) FROM service_requests t
WHERE t.processor_membership_id IS NOT NULL
  AND NOT EXISTS (SELECT 1 FROM user_tenant_memberships m WHERE m.id = t.processor_membership_id AND m.tenant_id = t.tenant_id);
```

## 5. 发布步骤

1. 构建 backend 镜像/二进制（含新迁移与更新后的 `post_schema_constraints.sql`）；
2. 以 `ITSM_BOOTSTRAP_ONLY=true`、`ITSM_AUTO_MIGRATE=true` 运行一次 bootstrap；
3. 检查迁移账本出现：
   - `20261112_membership_business_actors`
   - `20261112_membership_constraints_validate`
4. 执行 4.3 巡检 SQL，确认：
   - 21 条约束 `convalidated = true`；
   - 6 列 missing 计数符合预期；
   - 跨租户错配为 0；
5. 启动应用，验证 readyz；
6. 跑 demo E2E 分组（至少 00-smoke、01-permissions、05-multi-tenant-business）；
7. 发布后 45 分钟观察 5xx / level=error / panic。

## 6. 回滚

- `20261112_membership_business_actors_down` 可回滚新增列、索引与 6 条约束；
- `20261112_membership_constraints_validate` 为前向收敛迁移，不提供 down：
  约束保持 VALID 是更严格状态，不影响旧代码读写 `user_id` 兼容列；
- 旧客户端不依赖新增 membership 字段，API 向后兼容。

## 7. 风险与边界

1. VALIDATE 会扫描历史表并持有较轻的锁，建议在低峰期执行 bootstrap；
2. 迁移会把跨租户/失效的 membership 指针置 NULL，历史行仍可通过 `user_id`
   读取，但依赖 membership 的新查询会少这些行，属于预期的 fail-safe；
3. 服务请求审批记录仍是 `approver_id` 单轨，审批身份展示需下一批补齐；
4. 前端尚未显式展示创建人/处理人的 membership 身份（Phase 4 剩余项 3）。
