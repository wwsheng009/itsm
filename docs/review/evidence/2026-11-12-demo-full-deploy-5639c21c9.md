# 238 Demo 全量部署证据（2026-11-12 · 1.8.0-dev.5639c21c9）

> **Status: current**
> **环境**：`http://172.18.3.238:8089`（`itsm-demo`，`saas_msp`）
> **部署来源**：HEAD `5639c21c92e831ca35a5251cef87ddfe234e49f9`
> **部署 worktree**：`/home/n9e/itsm/repo-e2e-6745c998`，branch `deploy/full-5639c21c9`
> **中间版本**：`c651362e2` → `ce9323644` → `9aa18e13c` → `08426a0c2` → `5639c21c9`
> **上一稳定版本**：`1.8.0-dev.bf131540`

## 1. 本轮变更

### 1.1 接收人租户校验

- 工单 CC、BPMN CC、审批委派、BPMN 任务 actor/target 统一为
  “home tenant 兼容 / 跨租户必须有 active membership”；
- provider 客户租户身份可接收 CC、接任审批、发起/接受委托；
- 无客户租户 membership 的外部账号写入 `ticket_ccs`/`ticket_approvals` 时 fail-closed。

### 1.2 权限链修复

- CC 写/删路由权限从 `workflow:write` 改为 `ticket:update`，查询改为 `ticket:read`；
- 重新生成 `middleware/rbac_precheck_gen.go`；
- RBAC 路径预检在 `SmartCheckPermission` 未命中时回落到
  `AuthorizeResource`（JWT 主角色 + membership 合并角色的 DB 权限），与路由守卫同源；
- 前端抄送入口改为 `ticket:update` 或 `workflow:write` 任一即可。

### 1.3 错误映射与测试稳定性

- 工单流转业务错误映射为 403 / 404 / 400，基础设施错误仍为 500 且不泄露原始错误；
- E2E 断言器只忽略“同一接口失败后出现后续成功响应”的瞬时 4xx 控制台噪音，
  持久 4xx 仍失败；
- 新增 B4b：provider 深切客户租户后合法 CC 200、外部租户用户 CC 400。

## 2. 发布结果

```text
release_status=success
deployed_at=2026-11-12T00:13:08Z
version=1.8.0-dev.5639c21c9
git_branch=deploy/full-5639c21c9
git_commit=5639c21c92e831ca35a5251cef87ddfe234e49f9
readyz=ready
```

| 项 | 结果 |
|---|---|
| demo 容器 | 6/6 healthy |
| `/api/v1/version` | `1.8.0-dev.5639c21c9` |
| `/api/v1/readyz` | `ready=true`，schema `20261111_service_request_delete_grants` |
| 发布备份 | `backups/demo-release-1.8.0-dev.5639c21c9-20261112T000746Z` |
| 数据库 dump | `backups/demo-release-1.8.0-dev.5639c21c9-20261112T000746Z/itsm_demo.dump` |
| 发布前 env | `backups/demo-release-1.8.0-dev.5639c21c9-20261112T000746Z/.env.demo.before` |

镜像：

```text
backend   sha256:27697a990c26438c98cdd5f963d10f20dfbdc465108fd2eb3ec4c068aeeafae6
frontend  sha256:1323789e9ba747a00ee96b3a53fbb66b0b6594c2d27d16035c92d56a496cd0cb
ai        sha256:f99367604826ed7fa1541bfa8868dee1eb7492b4307d4e55f050d35b6a2d3301
```

AI 镜像复用 `itsm-ai-service:1.8.0-dev.ce9323644` 并重打 `5639c21c9` 标签；
digest 未变化。

## 3. E2E

按 `tests/e2e/demo/README.md` 的分组独立进程执行：

```text
00-smoke                     6 passed
01-permissions               8 passed
02-associations              8 passed
03-workflows                 8 passed（含 B4b）
04-ui-ux                    11 passed
05-multi-tenant-business     4 passed
06-attachment-access         1 passed
合计                        46/46
```

关键运行态抽查：

```text
provider 深切客户租户后合法 CC -> HTTP 200
无客户租户 membership 的外部用户 CC -> HTTP 400
```

单进程全量跑曾出现一次 CSRF token 轮换竞态：首次 `403 CSRF token mismatch`，
`http-client` 取新 token 重试后 200。最终测试断言器只在“失败后后续成功”时忽略
该瞬时噪音，持久 403 仍会失败；验收采用分组独立进程结果 **46/46**。

## 4. 数据巡检

2026-11-12T00:13:08Z 后新增行：

```text
ticket_ccs            5/5 双方 membership
attachments           3/3
ticket_comments      18/18
notifications         3/3
ticket_notifications 173/173
```

租户错配：

```text
ticket_ccs user_membership          0
ticket_ccs added_by_membership      0
ticket_approvals approver           0
ticket_approvals delegate_to        0
```

`[E2E-*]` 工单巡检：active 0 / soft-deleted 389；未改动 `[DEMO]` 种子数据。

## 5. 关联文档

- [2026-11-12-tenant-recipient-validation.md](../2026-11-12-tenant-recipient-validation.md)
- [membership-tenant-identity-c-plan.md](../../multi-tenant/plan/membership-tenant-identity-c-plan.md)
