# 238 Demo 全量部署证据（2026-11-11 · 1.8.0-dev.5699b5a8）

> **历史状态**：本文件记录第二轮全量部署 `5699b5a8`；当前生产基线已更新为
> `1.8.0-dev.af3cba9c`，见 [2026-11-11-demo-full-deploy-af3cba9c.md](./2026-11-11-demo-full-deploy-af3cba9c.md)。

> **状态**：success
> **环境**：`http://172.18.3.238:8089`（`itsm-demo`，`saas_msp`）
> **部署来源**：当前工作区 HEAD `5699b5a8a526bda2c1030052de4a3810aeacd261`
> **部署 worktree**：`/home/n9e/itsm/repo-e2e-6745c998`，branch `deploy/full-5699b5a8`
> **上一版本**：`1.8.0-dev.44faf311`

## 1. 本轮变更

在 `44faf311` 全量部署基础上新增：

- Round 5 多租户业务操作 E2E（3 条，全量套件 43 条）。
- D17 工单号全局唯一修复：Redis/DB 序列共用 `nextFreeTicketNumber` 全局探针，跳过他租户已占用号。
- 发布文档与测试证据回填。

## 2. 发布结果

```text
VERSION=1.8.0-dev.5699b5a8
git_commit=5699b5a8a526bda2c1030052de4a3810aeacd261
release_status=success
```

| 项 | 结果 |
|---|---|
| `/api/v1/version` | `1.8.0-dev.5699b5a8` |
| `/readyz` | `ready=true`；schema `20261111_service_request_delete_grants` |
| backend / worker | `itsm-backend:1.8.0-dev.5699b5a8`，healthy |
| frontend | `itsm-frontend:1.8.0-dev.5699b5a8`，healthy |
| ai-service | `itsm-ai-service:1.8.0-dev.5699b5a8`，healthy |
| nginx / redis | healthy |
| init/migration | `itsm-init-demo` exit=0 |
| 附件冒烟 | 上传/预览/SHA256/删除通过 |
| 全量 E2E | **43 passed / 0 expected failure**（1 worker，2.3m） |

## 3. 镜像与回滚

```text
backend    sha256:6e04f25142168a3ca4fa57a86ac8efe23a083e38bf97921435bbc9d3eb362ad6
frontend   sha256:53e5b5878b37c57724c7a6d251160d1293e23e53e739b7ee5d140dc30cfac9cf
ai-service sha256:f99367604826ed7fa1541bfa8868dee1eb7492b4307d4e55f050d35b6a2d3301
```

- 备份/回滚根目录：`/home/n9e/itsm/repo-e2e-6745c998/backups/demo-release-1.8.0-dev.5699b5a8-20261111T052801Z`
- 环境备份：`backups/demo-release-1.8.0-dev.5699b5a8-20261111T052801Z/.env.demo.before`
- 数据库备份：`backups/demo-release-1.8.0-dev.5699b5a8-20261111T052801Z/itsm_demo.dump`
- 发布证据：`backups/demo-release-1.8.0-dev.5699b5a8-20261111T052801Z/release.txt`

## 4. 多租户业务验证（Round 5）

新增 `itsm-frontend/tests/e2e/demo/05-multi-tenant-business.spec.ts`：

1. **MT-1**：客户侧建单 → provider 深度切换 mfg/med/edu → 客户作用域读取/评论/改状态 → 其他客户不可见 → 返回 provider 工作台指派/回复/改状态 → 客户侧回看。
2. **MT-2**：provider manager/agent 未分配客户切换 403；agent1 在已分配 mfg 处理工单后返回 provider 可读。
3. **MT-3**：mfg 问题可由 provider 工作台读取；med 不可见；provider 处于客户作用域时 `/msp/*` 403。

结果：**3/3 passed**；全量 **43/43 passed**。

## 5. 开放缺口（待产品决策）

provider 深度切换到客户作用域后直接 `POST /api/v1/tickets` 仍返回：

```json
{"code":404,"message":"申请人不存在或不可用"}
```

原因：权限清单含 `ticket:create`，但 handler 强制 requester=当前 provider 用户，服务端按目标客户租户校验 requester。需产品确认：

- 客户 requester 选择器并允许 `requesterId` 覆盖；
- 允许 provider 员工作为客户租户 requester；
- 或在深度客户态禁用建单并返回明确 403/文案。

当前已验证主路径为客户侧建单、provider 深度切换后处理。

## 6. 关联证据

- [E2E 报告](../2026-11-10-msp-demo-e2e-validation.md)
- [运行摘要](./2026-11-10-msp-demo-e2e/run-summary.md)
- [上一轮全量部署 44faf311](./2026-11-11-demo-full-deploy-44faf311.md)
- 测试套件：`itsm-frontend/tests/e2e/demo/`
