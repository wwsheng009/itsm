# 238 Demo 全量部署证据（2026-11-11 · 1.8.0-dev.af3cba9c）

> **Status: historical** — 已被 `1.8.0-dev.a0d4240a` 取代，见
> [2026-11-11-demo-full-deploy-a0d4240a.md](./2026-11-11-demo-full-deploy-a0d4240a.md)。

> **状态**：success
> **环境**：`http://172.18.3.238:8089`（`itsm-demo`，`saas_msp`）
> **部署来源**：当前工作区 HEAD `af3cba9cb`（fix attachment + E2E regression）
> **部署 worktree**：`/home/n9e/itsm/repo-e2e-6745c998`，branch `deploy/full-af3cba9c`
> **上一版本**：`1.8.0-dev.5699b5a8`

## 1. 本轮修复

- D18：工单附件列表对 `end_user` / `it_admin` / `security_admin` / provider 深度态返回 HTTP 500「获取附件列表失败」。
- 根因：旧附件列表在路由 `ticket:read` 之外硬编码 staff 角色白名单，且 handler 将权限错误统一映射为 500。
- 修复：授权跟随 `ticket:read`，只校验当前用户 active；新增 forbidden/not-found 错误映射 403/404；不再按目标租户过滤 provider 用户行。

## 2. 发布结果

```text
VERSION=1.8.0-dev.af3cba9c
git_commit=af3cba9cb
release_status=success
```

| 项 | 结果 |
|---|---|
| `/api/v1/version` | `1.8.0-dev.af3cba9c` |
| `/readyz` | `ready=true`；schema `20261111_service_request_delete_grants` |
| backend / worker | `itsm-backend:1.8.0-dev.af3cba9c`，healthy |
| frontend | `itsm-frontend:1.8.0-dev.af3cba9c`，healthy |
| ai-service | `itsm-ai-service:1.8.0-dev.af3cba9c`，healthy |
| nginx / redis | healthy |
| init/migration | `itsm-init-demo` exit=0 |
| 附件冒烟 | 上传/预览/SHA256/删除通过 |
| 全量 E2E | **44 passed / 0 expected failure**（1 worker，2.6m） |

## 3. 镜像与回滚

```text
backend    sha256:505269ad6035f0488b675ffc9ebad8d908c62c3b73cd77945aa69f9801af1c8f
frontend   sha256:53e5b5878b37c57724c7a6d251160d1293e23e53e739b7ee5d140dc30cfac9cf
ai-service sha256:f99367604826ed7fa1541bfa8868dee1eb7492b4307d4e55f050d35b6a2d3301
```

- 备份/回滚根目录：`/home/n9e/itsm/repo-e2e-6745c998/backups/demo-release-1.8.0-dev.af3cba9c-20261111T071224Z`
- 环境备份：`backups/demo-release-1.8.0-dev.af3cba9c-20261111T071224Z/.env.demo.before`
- 数据库备份：`backups/demo-release-1.8.0-dev.af3cba9c-20261111T071224Z/itsm_demo.dump`
- 发布证据：`backups/demo-release-1.8.0-dev.af3cba9c-20261111T071224Z/release.txt`

## 4. 附件回归

新增/更新：

- `itsm-backend/service/ticket_attachment_service.go`：活跃用户校验 + sentinel errors。
- `itsm-backend/handlers/ticket_attachment/handler.go`：403/404 映射。
- `itsm-backend/service/ticket_attachment_access_test.go`：end_user / it_admin / security_admin / provider / inactive / unknown ticket。
- `itsm-frontend/tests/e2e/demo/06-attachment-access.spec.ts`：上传附件后，多角色及 provider 深度态列出附件。

现场复测 ticket 74：

```text
demo.mfg.user GET /api/v1/tickets/74/attachments -> HTTP 200
```

全量套件：**44/44 passed**。

## 5. 开放项

provider 深度切到客户作用域后直接 `POST /api/v1/tickets` 仍返回 404「申请人不存在或不可用」；当前主路径为客户建单、provider 深度处理。修复方向待产品决策。

## 6. 关联证据

- [E2E 报告](../2026-11-10-msp-demo-e2e-validation.md)
- [运行摘要](./2026-11-10-msp-demo-e2e/run-summary.md)
- [上一轮 5699b5a8](./2026-11-11-demo-full-deploy-5699b5a8.md)
- 测试套件：`itsm-frontend/tests/e2e/demo/`
