# 2026-11-12 238 全量部署证据：服务请求审批人 membership

> 版本：`1.8.0-dev.16c5c453c`
> 提交：`16c5c453c4ac1a770c5147cf1929685975edeb3a`
> 分支：`deploy/full-16c5c453c`
> 部署时间：2026-11-12T04:59:32Z
> 关联：[Phase 4 第二批验证报告](../2026-11-12-membership-business-actors.md)

## 1. 运行态

```text
URL              http://172.18.3.238:8089
version          1.8.0-dev.16c5c453c
runtime commit   16c5c453c4ac1a770c5147cf1929685975edeb3a
readyz           {"ready":true,"schemaVersion":"20261112_service_request_approval_membership"}
demo containers  6/6 healthy
```

前序版本：`1.8.0-dev.8558ecfcc`。

## 2. 备份与回滚

```text
/home/n9e/itsm/repo-e2e-6745c998/backups/demo-release-1.8.0-dev.16c5c453c-20261112T044250Z
├── .env.demo.before
├── itsm_demo.dump
├── itsm_demo.dump.sha256
├── rollback.txt
├── release.txt
└── smoke-attachment.json
```

## 3. 本版变更

- `service_request_approvals` 新增 `approver_membership_id`、`delegated_to_membership_id`；
- 回填 `(approver_id, tenant_id)` → membership；
- 新增 `20261112_service_request_approval_membership` 迁移：
  - 清理跨租户脏指针；
  - 新增两条复合外键并以 VALID 状态创建；
  - 新增租户作用域索引；
- 审批动作 `ApplyApprovalWithMembership` 记录 actor 在当前租户内的 membership；
- `ServiceRequestApprovalResponse` 新增 `approverMembershipId` / `delegatedToMembershipId`；
- 服务请求详情审批时间线展示 `MembershipBadge`；
- `post_schema_constraints.sql` 与约束清单同步扩展到 23 条。

## 4. 数据验证

```text
migration 20261112_service_request_approval_membership applied 2026-11-12 04:58:44

service_request_approvals:
  total=405, with_approver=265, with_membership=265, missing=0
  cross-tenant invalid=0

pg_constraint:
  membership 复合外键 23/23 convalidated=true
```

样例：

```text
id 402  SR 134  L3 security  approver_id=10  membership_id=9  approved
id 401  SR 134  L2 it        approver_id=9   membership_id=8  approved
id 400  SR 134  L1 manager   approver_id=7   membership_id=6  approved
```

## 5. UI 验证

`07-membership-identity.spec.ts` 扩展为 5 个用例，新增：

```text
✓ 服务请求审批人展示 membership 身份（lead membership 6）
```

测试流程：

1. `demo.mfg.user` 创建服务请求；
2. `demo.mfg.lead` 审批 L1；
3. 客户管理员打开服务请求详情，显示 `membership-badge-6`；
4. 取消失败兜底后可删除，测试数据清理。

运行时 API 抽查（`demo.mfg.admin`，tenant 3）：

```text
GET /api/v1/service-requests/11
  level 1 approved: approverId=5, approverMembershipId=4
  DB: membership 4 -> role=admin, tenant_id=3
```

## 6. 全量 E2E

```text
00-smoke                    6 passed
01-permissions              8 passed
02-associations             8 passed
03-workflows                8 passed
04-ui-ux                   11 passed
05-multi-tenant-business    4 passed
06-attachment-access        1 passed
07-membership-identity      5 passed
合计                       51/51
```

首次全量复跑时 `00-smoke` 因 dashboard 懒加载图表请求在导航瞬间被 abort，应用层打印
`TypeError: Failed to fetch` 而误报；trace 为 `net::ERR_ABORTED`，后端无 5xx/error。
修复落在测试辅助函数 `gotoAndReady`：`networkidle` 后增加 500ms settle 窗口，避免下一页
导航中断仍在启动的懒加载请求。修复后 `00-smoke` 6/6，完整分组复跑 51/51。

## 7. 检查与门禁

```text
go test ./handlers/service_request（审批 membership 用例） PASS
go test ./internal/bootstrap ./migration                   PASS
go build ./handlers/service_request ./service ./dto ./ent   PASS
npx tsc --noEmit                                          PASS
npx eslint（改动文件）                                      PASS
npm run build                                             PASS
scripts/docs-gate/run-all.sh                              6/6 PASS
git diff --check                                          PASS
```

## 8. 发布后观察

45 分钟完整观察窗（2026-11-12T05:44:47Z）：

```text
demo containers          6/6 healthy（frontend/backend/worker/AI 均 Up 45m+）
backend error/fatal      0
backend panic            0
nginx 5xx                0
service_request_approvals  432 total / 280 with membership / 0 missing
cross-tenant invalid     0
membership 复合外键       23/23 convalidated=true
E2E-MEM-SR active        0
```

观察窗内 E2E 新增的 27 条审批记录均带 membership，无租户错配。

## 9. 仍未完成

- 变更审批 `change_approvals` 为 legacy 表，当前变更审批已由审批链/BPMN 决策驱动；
- 全量 17 表行为矩阵；
- BPMN `ccType=group/role` 的 provider 客户租户选人支持；
- home tenant 无 membership 兼容窗口收口。
