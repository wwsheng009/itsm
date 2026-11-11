# 238 Demo 全量部署证据（2026-11-11 · 1.8.0-dev.44faf311）

> **历史状态**：本文件记录第一轮全量部署 `44faf311`；当前生产基线已更新为
> `1.8.0-dev.5699b5a8`，见 [2026-11-11-demo-full-deploy-5699b5a8.md](./2026-11-11-demo-full-deploy-5699b5a8.md)。

> **状态**：success
> **环境**：`http://172.18.3.238:8089`（`itsm-demo`，`saas_msp`）
> **部署来源**：当前工作区 HEAD `44faf31100eefd0ba49643f4ef76ebe31ba6fb4b`
> **部署 worktree**：`/home/n9e/itsm/repo-e2e-6745c998`，branch `deploy/full-44faf311`

## 1. 为什么重部署

此前 8089 运行 `1.8.0-dev.f7dd2974`，来自独立裁剪部署分支（`f7dd29743`），不是当前工作区全量代码。
该版本缺少 `DeepTenantReturn` 等当前分支修复，导致 provider 进入客户租户后 Header 的“返回工作台”入口消失。

本次按用户要求，从当前工作区 HEAD 重新打包并全量构建 backend/frontend/ai-service，切换整个 demo 栈。

## 2. 发布结果

```text
VERSION=1.8.0-dev.44faf311
git_commit=44faf31100eefd0ba49643f4ef76ebe31ba6fb4b
release_status=success
```

| 项 | 结果 |
|---|---|
| `/api/v1/version` | `1.8.0-dev.44faf311` |
| `/readyz` | `ready=true`；schema `20261111_service_request_delete_grants` |
| backend | `itsm-backend:1.8.0-dev.44faf311`，healthy |
| worker | `itsm-backend:1.8.0-dev.44faf311`，healthy |
| frontend | `itsm-frontend:1.8.0-dev.44faf311`，healthy |
| ai-service | `itsm-ai-service:1.8.0-dev.44faf311`，healthy |
| nginx / redis | healthy |
| init/migration | `itsm-init-demo` exit=0 |
| 附件冒烟 | 上传/预览/SHA256/删除通过 |
| 全量 E2E | **40 passed / 0 expected failure**（1 worker，1.9m） |

## 3. 镜像与回滚

```text
backend   sha256:79ff20479cfa588ff758e1745da8758af72971532fb837dd13ffdc48753fe164
frontend  sha256:53e5b5878b37c57724c7a6d251160d1293e23e53e739b7ee5d140dc30cfac9cf
ai-service sha256:f99367604826ed7fa1541bfa8868dee1eb7492b4307d4e55f050d35b6a2d3301
```

- 备份/回滚根目录：`/home/n9e/itsm/repo-e2e-6745c998/backups/demo-release-1.8.0-dev.44faf311-20261111T043741Z`
- 上一版本：`1.8.0-dev.f7dd2974`
- 环境备份：`backups/demo-release-1.8.0-dev.44faf311-20261111T043741Z/.env.demo.before`
- 数据库备份：`backups/demo-release-1.8.0-dev.44faf311-20261111T043741Z/itsm_demo.dump`

## 4. 多租户返回入口回归

新增用例：`itsm-frontend/tests/e2e/demo/04-ui-ux.spec.ts` → `多租户跳转：provider 进入客户后 Header 保留返回工作台入口`。

验证链路：

1. `demo.msp.manager` 登录 provider 租户；
2. `switch-tenant` 到客户租户 `3`（华辰智能制造）；
3. Header 显示 `deep-tenant-indicator` 与 `back-to-workbench`；
4. 点击“返回工作台”，先切回 provider 租户，再打开 `/msp/workbench`。

结果：通过；全量 40 条 E2E 与附件冒烟均通过。

## 5. 关联证据

- [E2E 报告](../2026-11-10-msp-demo-e2e-validation.md)
- [运行摘要](./2026-11-10-msp-demo-e2e/run-summary.md)
- 测试套件：`itsm-frontend/tests/e2e/demo/`
