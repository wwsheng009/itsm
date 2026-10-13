# 238 演示栈部署证据（第二轮）：跨租户权限分类收口（2026-10-13）

> 状态：**已上线**｜版本：`1.8.0-dev.bba84fc2` → **`1.8.0-dev.de337963`**
> 代码：`feat/msp-p0-hardening` @ `1e78d08e`（后端）+ `de337963`（前端）
> 关联：第一轮证据见同目录 `run-summary.md`（分区与落点）

## 1. 缺陷根因（客户可见「跨租户总览」的结构性原因）

| 层 | 问题 |
|---|---|
| 权限词表 | `authz.PermissionDef` 只有 Code/Resource/Action/Name，**无 scope/归属元数据**；"跨租户 MSP 数据面"不是一等分类 |
| 供给契约 | 模板克隆 = 「managed grants 源→目标等价下发」，例外只有 ① 平台治理资源注册表（resource 级跳过）② 客户侧 C2 基线（**按 msp_\* 角色名**覆盖 5 个角色） |
| 结果 | `msp_ticket/msp_customer/msp_allocation/msp_report:*` 既不在 ① 也不在 ② → 客户租户照常获得（实测 IT总监/系统运维/运维总监/服务商租户管理员），且 `validateTenantClone` 反向要求这些授权存在——任何前缀硬编码剥离都会与校验器冲突（fail 示例：`managed grant sysadmin -> msp_customer:read missing`） |
| 前端 | 区域守卫含 `msp:read`（客户 admin 基线持有）→ 客户可进 `/msp/*`（页内必然 403） |

## 2. 修复（单一源 + 克隆/校验/对账同口径）

| 提交 | 内容 |
|---|---|
| `1e78d08e`（后端） | `service` 新增 `mspCrossTenantPermissionResources`（4 个数据面资源，客户租户不下发）与 `mspAreaPermissionResources`（`msp` 区域码）；`cloneRolePermissions` customerScope 时**从源头跳过**；`validateTenantClone` 同口径跳过（消除契约矛盾）；存量对账剥离改用注册表判定；新增守卫测试 `router/msp_resource_scope_guard_test.go`（MSP 路由资源必须已分类，新增资源未登记即 CI 红） |
| `de337963`（前端） | `MSP_AREA_PERMISSIONS` 收敛到数据面操作码（去除 `msp:read`）——客户 admin 不再放行 `/msp/*` |

## 3. 部署与数据对账

1. 增量 bundle（`bba84fc2..de337963`）→ 远端 `deploy-demo-b6` checkout；
2. `itsm-backend` + `itsm-frontend` 重建（`BUILD_EXIT=0`），`itsm-ai-service` retag；
3. `docker compose --env-file .env.demo -f docker-compose.demo.yml up -d` → 6/6 healthy；
4. **存量对账**（`cmd/reconcile_customer_scope_roles` 交叉编译 linux/amd64，连 `127.0.0.1:5433/itsm_demo`，`-apply` 幂等复跑两次 exit 0）：

| 指标 | 对账前 | 对账后 |
|---|---|---|
| 客户租户跨租户 MSP 授权（4 资源族） | demo-mfg/med/edu 各 29 | **各 0** |
| 客户角色残留 msp 码 | msp_ticket/customer/allocation/report 全族 | 仅 `msp:read`（+ 部分 `msp:write`） |
| provider 租户（default/demo-provider） | 各 60 | 各 60（不受影响） |

## 4. 线上复验（浏览器，`1.8.0-dev.de337963`）

| 场景 | 结果 |
|---|---|
| 服务商 `demo.msp.manager` | 落 `/msp/workbench`；`/dashboard` 跨租户分区渲染（客户 3 / 未关闭 22 / 超期 5 / 临近 6 / 未指派 0） |
| 客户 `demo.mfg.admin` | 落 `/dashboard`；**无跨租户分区**；侧栏**无 MSP 菜单**；手动访问 `/msp/workbench` → **重定向 `/403`** |
| 过度授权回归（第一轮构造 `tmp.opstest`：客户租户 + 系统运维角色） | 新门禁下不渲染分区、不发请求（单测锁定）；且对账后该角色已无 `msp_ticket:read`（数据层根治） |
| 单测 | `go test ./pkg/seeder/`（含客户侧回归：非 msp_\* 角色零跨租户码 + provider 不受影响）全绿；`go test ./router/ -run TestMSPRouteResourcesClassified` 绿；前端 guards/route-permissions + tsc/eslint 绿 |

## 5. 扩展协议（回答「后续新增 msp_\* 资源怎么处理」）

1. 新增 MSP 资源 → 在 `service` 注册表登记（跨租户数据面 / 区域码二选一）；
2. 守卫测试 `TestMSPRouteResourcesClassified` 强制该步骤（未登记即红，防 fail-open）；
3. 存量租户重跑 `cmd/reconcile_customer_scope_roles -apply`（幂等）；
4. 前端入口码清单（`MSP_WORKBENCH_PERMISSIONS` / `MSP_AREA_PERMISSIONS`）同步；
5. 中期方向：给 `authz.PermissionDef` 增加 scope 字段并 codegen 落库，前端由后端权限目录下发 scope 驱动（免手工清单）。

## 6. 回滚

```bash
cd /home/n9e/itsm/repo
sudo sed -i 's/^VERSION=.*/VERSION=1.8.0-dev.bba84fc2/' .env.demo
sudo docker compose --env-file .env.demo -f docker-compose.demo.yml up -d
```

> 镜像 `1.8.0-dev.bba84fc2` 及更早标签保留；`.env.demo` 备份见部署日志（`.env.demo.bak-20261013-155353`）。
> 对账为幂等增量（只删跨租户 MSP 授权），回滚镜像不会自动恢复已剥离授权；如需恢复演示数据可重建演示库（见 demo-environment.md §4）。
