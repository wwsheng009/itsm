# 2026-11-12 238 全量部署证据：Phase 4 第二批最终版

> 版本：`1.8.0-dev.cb55a4e69`
> 提交：`cb55a4e695644515c6f4191e06cb398c6bc8ec5f`
> 分支：`deploy/full-cb55a4e69`
> 部署时间：2026-11-12T03:22:50Z
> 关联：[Phase 4 第二批验证报告](../2026-11-12-membership-business-actors.md)

## 1. 运行态

```text
URL              http://172.18.3.238:8089
version          1.8.0-dev.cb55a4e69
runtime commit   cb55a4e695644515c6f4191e06cb398c6bc8ec5f
readyz           {"ready":true,"schemaVersion":"20261112_membership_constraints_validate"}
demo containers  6/6 healthy
```

| 容器 | 镜像 | 状态 |
|---|---|---|
| `itsm-backend-demo` | `itsm-backend:1.8.0-dev.cb55a4e69` | healthy |
| `itsm-worker-demo` | `itsm-backend:1.8.0-dev.cb55a4e69` | healthy |
| `itsm-frontend-demo` | `itsm-frontend:1.8.0-dev.cb55a4e69` | healthy |
| `itsm-ai-service-demo` | `itsm-ai-service:1.8.0-dev.cb55a4e69` | healthy |
| `itsm-nginx-demo` | `nginx:alpine` | healthy |
| `itsm-redis-demo` | `redis:7-alpine` | healthy |

## 2. 备份与回滚

```text
/home/n9e/itsm/repo-e2e-6745c998/backups/demo-release-1.8.0-dev.cb55a4e69-20261112T031731Z
├── .env.demo.before
├── itsm_demo.dump
├── itsm_demo.dump.sha256
├── rollback.txt
├── release.txt
└── smoke-attachment.json
```

前序版本：`1.8.0-dev.47bd56dc5`。
中间版本：`1.8.0-dev.4847f7c03`（首版）、`1.8.0-dev.47bd56dc5`（事件转问题修复）。

## 3. 构建产物

```text
itsm-backend:1.8.0-dev.cb55a4e69    sha256:37f8e91344bbfc31267c01306f3bd317edb7fbb156fb312060d0307361989bb5
itsm-frontend:1.8.0-dev.cb55a4e69   sha256:1323789e9ba747a00ee96b3a53fbb66b0b6594c2d27d16035c92d56a496cd0cb
itsm-ai-service:1.8.0-dev.cb55a4e69 复用 47bd56dc5 镜像
```

- `itsm-init-demo` 退出码 0；
- 六个 demo 容器全部 healthy；
- 版本端点返回 `1.8.0-dev.cb55a4e69`；
- 附件上传/预览/删除冒烟通过（sha256 match）。

## 4. 迁移与约束

迁移账本（保持不变，本版仅代码修复）：

```text
20261112_membership_business_actors            applied 2026-11-12 02:44:11
20261112_membership_constraints_validate       applied 2026-11-12 02:44:11
20261112_membership_business_actors_reconcile  applied 2026-11-12 02:58:55
```

`pg_constraint`：21 条 membership 复合外键全部 `convalidated = true`。

## 5. 数据巡检（最终）

| 引用 | missing | total |
|---|---:|---:|
| `problems.created_by_membership_id` | 0 | 204 |
| `problems.assignee_membership_id` | 0 | 204 |
| `changes.created_by_membership_id` | 0 | 14 |
| `changes.assignee_membership_id` | 0 | 14 |
| `service_requests.requester_membership_id` | 0 | 132 |
| `service_requests.processor_membership_id` | 0 | 132 |

跨租户 membership 错配：6 类引用全部 `0`。

## 6. E2E

最终版 `cb55a4e69` 复跑：

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

## 7. 实际业务操作验证

通过真实登录、CSRF、业务 API 与 DB 回查完成：

| 场景 | 操作 | 结果 |
|---|---|---|
| 客户问题 | `demo.mfg.admin` 创建问题 | `id=198`, `createdByMembershipId=4` |
| 客户变更 | 创建并分配给 `demo.mfg.agent` | `id=226`, creator=4, assignee=5 |
| 变更持久化 | 新建 228 后回查 DB | creator=4, assignee=5 |
| 客户服务请求 | `demo.mfg.user` 创建 | `id=128`, `requesterMembershipId=7` |
| SR 三级审批 | lead → itadmin → secadmin | `manager_approved → it_approved → security_approved` |
| SR 进入履约 | `demo.mfg.admin` 置 `provisioning` | `processorId=5`, `processorMembershipId=4` |
| provider 深度切换 | `demo.msp.manager` 切入 demo-mfg | `membershipId=22`, `accountKind=provider`, `mspRole=provider_admin` |
| provider 建问题 | 在客户租户创建 | `id=199`, `createdByMembershipId=22` |
| provider 建变更 | 在客户租户创建 | `id=227`, `createdByMembershipId=22` |
| provider 分配 | 分配给客户 `demo.mfg.agent` | `assigneeMembershipId=5` |
| 跨租户处理人 | 分配给医疗 `demo.med.agent`（user 12） | HTTP 400 / code 4000「处理人不存在或不可用」 |
| provider 建 SR | 在客户租户创建 | `id=129`, `requesterId=2`, `requesterMembershipId=22` |

所有验证数据均已按业务 API 清理。

## 8. 本版修复的缺陷

| 缺陷 | 修复 |
|---|---|
| 事件转问题直连 ent 创建导致 creator membership NULL | `RootCauseAnalysisService` 解析 membership；reconcile 迁移幂等回填 |
| 变更跨租户处理人返回 500 | `membershipref` 返回 typed `AppError`；实测 400 / code 4000 |
| provider 深度切换后创建服务请求失败 | `GetUserContext` 回落 active membership；实测 provider SR 创建成功 |

## 9. 发布后观察

- 发布后 backend 日志无 `level=error` / panic / fatal；
- 无 5xx 标记；
- 45 分钟完整观察窗待补记。
