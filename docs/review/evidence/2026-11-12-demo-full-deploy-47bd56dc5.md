# 2026-11-12 238 全量部署证据：Phase 4 第二批

> 版本：`1.8.0-dev.47bd56dc5`
> 提交：`47bd56dc5f2f7707fcdb015d9a7ae25acd40e072`
> 分支：`deploy/full-47bd56dc5`
> 部署时间：2026-11-12T02:59:39Z
> 关联：[Phase 4 第二批验证报告](../2026-11-12-membership-business-actors.md)

## 1. 运行态

```text
URL              http://172.18.3.238:8089
version          1.8.0-dev.47bd56dc5
runtime commit   47bd56dc5f2f7707fcdb015d9a7ae25acd40e072
readyz           {"ready":true,"schemaVersion":"20261112_membership_constraints_validate"}
demo containers  6/6 healthy
```

| 容器 | 镜像 | 状态 |
|---|---|---|
| `itsm-backend-demo` | `itsm-backend:1.8.0-dev.47bd56dc5` | healthy |
| `itsm-worker-demo` | `itsm-backend:1.8.0-dev.47bd56dc5` | healthy |
| `itsm-frontend-demo` | `itsm-frontend:1.8.0-dev.47bd56dc5` | healthy |
| `itsm-ai-service-demo` | `itsm-ai-service:1.8.0-dev.47bd56dc5` | healthy |
| `itsm-nginx-demo` | `nginx:alpine` | healthy |
| `itsm-redis-demo` | `redis:7-alpine` | healthy |

## 2. 备份与回滚

最终发布备份：

```text
/home/n9e/itsm/repo-e2e-6745c998/backups/demo-release-1.8.0-dev.47bd56dc5-20261112T025423Z
├── .env.demo.before
├── itsm_demo.dump
├── itsm_demo.dump.sha256
├── rollback.txt
├── release.txt
└── smoke-attachment.json
```

前序版本：`1.8.0-dev.4847f7c03`。
中间发布备份（首版）：`backups/demo-release-1.8.0-dev.4847f7c03-20261112T023051Z`。

## 3. 构建与发布

```text
itsm-backend:1.8.0-dev.47bd56dc5    sha256:d7d2e395bea3c0647029ca7c650470736fdacf8278db06809d5220a70d3c5e5d
itsm-frontend:1.8.0-dev.47bd56dc5   sha256:1323789e9ba747a00ee96b3a53fbb66b0b6594c2d27d16035c92d56a496cd0cb
itsm-ai-service:1.8.0-dev.47bd56dc5 复用 4847f7c03 镜像
```

- `itsm-init-demo` 退出码 0；
- 六个 demo 容器全部 healthy；
- 版本端点返回 `1.8.0-dev.47bd56dc5`；
- 认证附件上传/预览/删除冒烟通过（sha256 match）。

## 4. 迁移账本

| version | applied_at |
|---|---|
| `20261112_membership_business_actors` | 2026-11-12 02:44:11 |
| `20261112_membership_constraints_validate` | 2026-11-12 02:44:11 |
| `20261112_membership_business_actors_reconcile` | 2026-11-12 02:58:55 |

## 5. 约束 VALIDATE

`pg_constraint` 中 membership 复合外键：

```text
21 rows, convalidated = true for all
```

覆盖工单（3）、评论（1）、附件（2）、CC（2）、审批（2）、审批记录（1）、
通知（2）、BPMN 决策（1）、审计（1）、问题（2）、变更（2）、服务请求（2）。

## 6. 数据巡检

最终发布后：

| 引用 | missing | total |
|---|---:|---:|
| `problems.created_by_membership_id` | 0 | 195 |
| `problems.assignee_membership_id` | 0 | 195 |
| `changes.created_by_membership_id` | 0 | 14 |
| `changes.assignee_membership_id` | 0 | 14 |
| `service_requests.requester_membership_id` | 0 | 126 |
| `service_requests.processor_membership_id` | 0 | 126 |

跨租户 membership 错配：6 类引用全部 `0`。

## 7. 运行时 API 抽查

使用 `demo.mfg.admin`（tenant 3，membership 4）在 238 上实测：

| 操作 | 结果 |
|---|---|
| 登录 `/auth/me` | `membershipId=4`, `accountKind=customer` |
| 创建问题 | `id=189`, `createdBy=5`, `createdByMembershipId=4` |
| 创建变更 | `id=215`, `createdByMembershipId=4` |
| 分配变更给 `demo.mfg.agent`（user 6） | `assigneeId=6`, `assigneeMembershipId=5` |
| 创建服务请求 | `id=123`, `requesterMembershipId=4` |

## 8. E2E

```text
00-smoke                    6 passed
01-permissions              8 passed
02-associations             8 passed
03-workflows                8 passed（含 B4b）
04-ui-ux                   11 passed
05-multi-tenant-business    4 passed
06-attachment-access        1 passed
合计                       46/46
```

最终版本 `47bd56dc5` 复跑结果：**7/7 分组通过，46/46**。

## 9. E2E 发现并修复的缺陷

| 缺陷 | 修复 |
|---|---|
| 事件转问题走 `RootCauseAnalysisService` 直接 ent 创建，绕过 HTTP 写路径，`created_by_membership_id` 为 NULL | 创建时解析 `(created_by, tenant_id)` membership；新增 `20261112_membership_business_actors_reconcile` 幂等回填发布窗口内的 NULL 行 |

- 首版 `4847f7c03` 发布后，E2E 生成的 6 条新问题中 3 条事件转问题行缺少 creator membership；
- 修复版 `47bd56dc5` 发布后，reconcile 迁移将这 3 行补齐；
- 复跑 E2E 新生成的事件转问题行（如 problem 191–194）全部带 `created_by_membership_id=4`。

## 10. 发布后观察

- 发布后 backend 日志无 `level=error` / panic / fatal；
- 无 5xx 记录（E2E 期间验证）；
- 发布后 45 分钟观察窗待补记。

## 11. 回滚

```text
previous_version = 1.8.0-dev.4847f7c03
rollback_env     = backups/demo-release-1.8.0-dev.47bd56dc5-20261112T025423Z/.env.demo.before
db_dump          = backups/demo-release-1.8.0-dev.47bd56dc5-20261112T025423Z/itsm_demo.dump
```

`20261112_membership_business_actors_down` 可回滚新增列、索引与 6 条外键；
`constraints_validate` 与 `reconcile` 为前向收敛迁移，保持 VALID/回填状态更安全。
