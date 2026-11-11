# 238 Demo 全量部署证据（2026-11-11 · 1.8.0-dev.91eb6e82）

> **Status: current**
> **环境**：`http://172.18.3.238:8089`（`itsm-demo`，`saas_msp`）
> **部署来源**：当前工作区 HEAD `91eb6e82e1678885d5c37e8ba5ed5e0e7e040548`
> **部署 worktree**：`/home/n9e/itsm/repo-e2e-6745c998`，branch `deploy/full-91eb6e82e`
> **上一版本**：`1.8.0-dev.a0d4240a`

## 1. 本轮变更

硬编码角色白名单审计的穷尽复核补漏：

- `service/ai_health.go`：AI 健康告警收件人清除无效用户角色码 `tenant_admin`，
  改为合法 `admin/super_admin/sysadmin`；
- `service/approval_service.go`：审批人 fallback 清除无效用户角色码
  `tenant_admin` / `workflow_admin`，收敛为合法管理类用户角色；
- `service/msp_record_write.go`：provider 写门禁已通过时，`allowedActions`
  不再叠加家租户 `users.role` 的 owner/assignee 行级白名单，修复
  `demo.msp.manager`（`users.role=end_user`、`msp_role=provider_admin`）在客户
  问题/变更/服务请求上“UI 不可写、域服务实际可写”的不一致；
- 新增/补强 Go 单测与 `05-multi-tenant-business.spec.ts` 运行态断言。

## 2. 发布结果

```text
release_status=success
deployed_at=2026-11-11T09:25:31Z
version=1.8.0-dev.91eb6e82
git_branch=deploy/full-91eb6e82e
git_commit=91eb6e82e1678885d5c37e8ba5ed5e0e7e040548
```

| 项 | 结果 |
|---|---|
| `/api/v1/version` | `1.8.0-dev.91eb6e82` |
| `/api/v1/healthz` | HTTP 200，`status=ok` |
| `/api/v1/readyz` | `ready=true`；schema `20261111_service_request_delete_grants` |
| backend / worker | `itsm-backend:1.8.0-dev.91eb6e82`，healthy |
| frontend | `itsm-frontend:1.8.0-dev.91eb6e82`，healthy |
| ai-service | `itsm-ai-service:1.8.0-dev.91eb6e82`，healthy（复用已验证镜像） |
| nginx / redis | healthy |
| init/migration | `itsm-init-demo` exit=0 |
| 附件冒烟 | 上传/预览/SHA256/删除通过；ticket 85，`sha256Match=true` |
| 后端错误日志 | 发布后 15 分钟无 `level=error/fatal`、无 `panic` |

## 3. 镜像与回滚

```text
backend    sha256:4eadd5b3d80f574e80f727284a2ac378bf47a52d08a8fa18d02acf980f10171a
frontend   sha256:e338294088be66b56ece854c51c6022efed78150343655694c146fa7a365055c
ai-service sha256:f99367604826ed7fa1541bfa8868dee1eb7492b4307d4e55f050d35b6a2d3301
```

- 备份/回滚根目录：`/home/n9e/itsm/repo-e2e-6745c998/backups/demo-release-1.8.0-dev.91eb6e82-20261111T092029Z`
- 环境备份：`backups/demo-release-1.8.0-dev.91eb6e82-20261111T092029Z/.env.demo.before`
- 数据库备份：`backups/demo-release-1.8.0-dev.91eb6e82-20261111T092029Z/itsm_demo.dump`
- 发布证据：`backups/demo-release-1.8.0-dev.91eb6e82-20261111T092029Z/release.txt`

## 4. E2E 复跑

按 spec 独立进程执行（避免本机 Windows Chrome 同时持有 44 条用例上下文导致 OOM）：

| Spec | 结果 |
|---|---|
| `00-smoke` | 6/6 |
| `01-permissions` | 8/8 |
| `02-associations` | 8/8 |
| `03-workflows` | 7/7 |
| `04-ui-ux` | 11/11 |
| `05-multi-tenant-business` | 3/3（新增 provider `allowedActions` 断言后复跑） |
| `06-attachment-access` | 1/1 |
| **合计** | **44/44** |

定向运行态断言：`demo.msp.manager` 读取制造租户问题详情时，
`allowedActions[action=status].allowed=true`；该账号在 provider 租户的
`users.role=end_user`，因此该断言覆盖了“家租户角色误判 provider 行级写权限”的回归。

## 5. 开放项

- provider 深度切到客户作用域后直接 `POST /api/v1/tickets` 仍返回 404「申请人不存在或不可用」；
  当前主路径为客户建单、provider 深度处理，修复方向待产品决策。
- `ticket_workflow_service.go` 的 CC 查看/管理角色白名单、旧前端角色守卫、平台租户页
  `super_admin` 条件仍待产品权限模型确认。

## 6. 关联证据

- [E2E 报告](../2026-11-10-msp-demo-e2e-validation.md)
- [角色白名单审计](../2026-11-11-role-whitelist-audit.md)
- [运行摘要](./2026-11-10-msp-demo-e2e/run-summary.md)
- [上一轮 a0d4240a](./2026-11-11-demo-full-deploy-a0d4240a.md)
- 测试套件：`itsm-frontend/tests/e2e/demo/`
