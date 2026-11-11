# 238 Demo 全量部署证据（2026-11-11 · 1.8.0-dev.e1599ffd）

> **Status: current**
> **环境**：`http://172.18.3.238:8089`（`itsm-demo`，`saas_msp`）
> **部署来源**：当前工作区 HEAD `e1599ffdb5fdf56b44aa783261fe6535bf5714f2`
> **部署 worktree**：`/home/n9e/itsm/repo-e2e-6745c998`，branch `deploy/full-e1599ffd`
> **上一版本**：`1.8.0-dev.ad878805`

## 1. 本轮变更

### 1.1 工单分配 membership 同步

- 单张指派：`POST /api/v1/tickets/:id/assign` 解析目标租户 active membership，
  原子更新 `assignee_id + assignee_membership_id`；
- 批量指派：同一 assignee membership 批量写入；
- 智能分配/重新分配/负载均衡路径同步 `assignee_membership_id`；
- 生产 `TicketService.AssignTicket` 优先用 membership 校验处理人，
  无 membership 时保留旧同租户用户校验（迁移窗口兼容）。

### 1.2 关联修复

- C 方案 provider 代客建单与 membership 回填沿用 `ad878805`；
- 变更重复删除 404 沿用 `ad878805`。

## 2. 发布结果

```text
release_status=success
deployed_at=2026-11-11T12:59:59Z
version=1.8.0-dev.e1599ffd
git_branch=deploy/full-e1599ffd
git_commit=e1599ffdb5fdf56b44aa783261fe6535bf5714f2
```

| 项 | 结果 |
|---|---|
| `/api/v1/version` | `1.8.0-dev.e1599ffd` |
| `/api/v1/readyz` | `ready=true`；schema `20261111_service_request_delete_grants` |
| backend / worker | `itsm-backend:1.8.0-dev.e1599ffd`，healthy |
| frontend | `itsm-frontend:1.8.0-dev.e1599ffd`，healthy |
| ai-service | `itsm-ai-service:1.8.0-dev.e1599ffd`，healthy（复用已验证镜像） |
| nginx / redis | healthy |
| init/migration | `itsm-init-demo` exit=0 |
| 附件冒烟 | 上传/预览/SHA256/删除通过 |
| 后端错误日志 | 发布后 20 分钟观测窗口（截至 13:22Z）无 5xx、`level=error`、panic |

## 3. 数据核验

```text
active_tickets=72
active_with_assignee=62
active_with_assignee_but_no_membership=0
requester_membership_not_null=72
tenant_membership_mismatch=0
```

## 4. E2E 复跑

按 spec 独立进程执行，最终版本累计 **45/45**：

| Spec | 结果 |
|---|---|
| `00-smoke` | 6/6 |
| `01-permissions` | 8/8 |
| `02-associations` | 8/8 |
| `03-workflows` | 7/7（B3 新增 `assigneeMembershipId > 0` 断言） |
| `04-ui-ux` | 11/11 |
| `05-multi-tenant-business` | 4/4 |
| `06-attachment-access` | 1/1 |
| **合计** | **45/45** |

## 5. 镜像与回滚

```text
backend    sha256:af1cc2efa5cee2a9f53f29e1170e24c708d123c4dae5f1f37fc0d7a01289a1e6
frontend   sha256:f22a65fe0675a307ed2f47085eff73e6e6ddad3a8cc242e96a77954b3400fa96
ai-service sha256:f99367604826ed7fa1541bfa8868dee1eb7492b4307d4e55f050d35b6a2d3301
```

- 备份/回滚根目录：`/home/n9e/itsm/repo-e2e-6745c998/backups/demo-release-1.8.0-dev.e1599ffd-20261111T125504Z`
- 环境备份：`backups/demo-release-1.8.0-dev.e1599ffd-20261111T125504Z/.env.demo.before`
- 数据库备份：`backups/demo-release-1.8.0-dev.e1599ffd-20261111T125504Z/itsm_demo.dump`
- 发布证据：`backups/demo-release-1.8.0-dev.e1599ffd-20261111T125504Z/release.txt`

## 6. 关联证据

- [C 方案设计](../../multi-tenant/plan/membership-tenant-identity-c-plan.md)
- [E2E 报告](../2026-11-10-msp-demo-e2e-validation.md)
- [运行摘要](./2026-11-10-msp-demo-e2e/run-summary.md)
- [上一轮 ad878805](./2026-11-11-demo-full-deploy-ad878805.md)
- 测试套件：`itsm-frontend/tests/e2e/demo/`
