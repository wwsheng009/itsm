# 238 Demo 全量部署证据（2026-11-11 · 1.8.0-dev.a0d4240a）

> **状态**：success
> **环境**：`http://172.18.3.238:8089`（`itsm-demo`，`saas_msp`）
> **部署来源**：当前工作区 HEAD `a0d4240ad78ea32212236b6680bbe5f1bb3ac8b3`
> **部署 worktree**：`/home/n9e/itsm/repo-e2e-6745c998`，branch `deploy/full-a0d4240a`
> **上一版本**：`1.8.0-dev.af3cba9c`

## 1. 本轮变更

- 硬编码角色白名单审计与修复（详见 [角色白名单审计](../2026-11-11-role-whitelist-audit.md)）：
  - Header AI 入口移除 `isAdmin()` 旁路，仅保留 `ai:read`；
  - AI autopilot 通知收件人修正无效 `tenant_admin` 用户角色码；
  - 工单数据范围、MSP 记录写权限复用中央 `IsAdminLike` / `datascope` 判定。
- 保留“内部备注 staff 白名单”等有明确产品语义的领域限制，未擅自放宽；CC 管理角色白名单与旧前端角色守卫登记为待产品决策风险。
- 全量构建 backend / worker / frontend，ai-service 复用当前镜像。

## 2. 发布结果

```text
VERSION=1.8.0-dev.a0d4240a
git_branch=deploy/full-a0d4240a
git_commit=a0d4240ad78ea32212236b6680bbe5f1bb3ac8b3
release_status=success
deployed_at=2026-11-11T08:14:50Z
```

| 项 | 结果 |
|---|---|
| `/api/v1/version` | `1.8.0-dev.a0d4240a` |
| `/readyz` | `ready=true`；schema `20261111_service_request_delete_grants` |
| backend / worker | `itsm-backend:1.8.0-dev.a0d4240a`，healthy |
| frontend | `itsm-frontend:1.8.0-dev.a0d4240a`，healthy |
| ai-service | `itsm-ai-service:1.8.0-dev.a0d4240a`，healthy |
| nginx / redis | healthy |
| init/migration | `itsm-init-demo` exit=0 |
| 附件冒烟 | 上传/预览/SHA256/删除通过 |
| E2E | 按 spec 分组累计 **44 passed / 0 expected failure** |

## 3. 镜像与回滚

```text
backend    sha256:fe50a2aca7a45f600cd1abe1c77583ca44312f6db4823a42f8c3214507f9eb97
frontend   sha256:e338294088be66b56ece854c51c6022efed78150343655694c146fa7a365055c
ai-service sha256:f99367604826ed7fa1541bfa8868dee1eb7492b4307d4e55f050d35b6a2d3301
```

- 备份/回滚根目录：`/home/n9e/itsm/repo-e2e-6745c998/backups/demo-release-1.8.0-dev.a0d4240a-20261111T080506Z`
- 环境备份：`backups/demo-release-1.8.0-dev.a0d4240a-20261111T080506Z/.env.demo.before`
- 数据库备份：`backups/demo-release-1.8.0-dev.a0d4240a-20261111T080506Z/itsm_demo.dump`
- 发布证据：`backups/demo-release-1.8.0-dev.a0d4240a-20261111T080506Z/release.txt`

## 4. E2E 复跑

分 spec 独立进程执行（避免本机 Windows Chrome 同时持有 44 条用例上下文导致 OOM）：

| Spec | 结果 |
|---|---|
| `00-smoke` | 6/6 |
| `01-permissions` | 8/8 |
| `02-associations` | 8/8 |
| `03-workflows` | 7/7 |
| `04-ui-ux` | 11/11 |
| `05-multi-tenant-business` | 3/3 |
| `06-attachment-access` | 1/1 |
| **合计** | **44/44** |

> 单进程一次跑全量时曾出现本机 Chrome OOM（页面崩溃），分组复跑全部通过；这是测试运行器资源问题，不是产品失败。审计报告已如实记录该口径。

## 5. 开放项

- provider 深度切到客户作用域后直接 `POST /api/v1/tickets` 仍返回 404「申请人不存在或不可用」；当前主路径为客户建单、provider 深度处理，修复方向待产品决策。
- `ticket_workflow_service.go` 的 CC 查看/管理角色白名单、旧前端角色守卫、平台租户页 `super_admin` 条件仍待产品权限模型确认，未在本轮修改。

## 6. 关联证据

- [E2E 报告](../2026-11-10-msp-demo-e2e-validation.md)
- [角色白名单审计](../2026-11-11-role-whitelist-audit.md)
- [运行摘要](./2026-11-10-msp-demo-e2e/run-summary.md)
- [上一轮 af3cba9c](./2026-11-11-demo-full-deploy-af3cba9c.md)
- 测试套件：`itsm-frontend/tests/e2e/demo/`
