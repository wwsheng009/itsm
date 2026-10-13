# 客户侧「跨租户总览」可见性排查与修复（2026-10-13）

> 触发：客户账号不应看到 dashboard「跨租户总览」。
> 结论：**已修复并上线**（`1.8.0-dev.bba84fc2`，238 演示栈 8089）；
> 权限数据层面另发现一处**客户租户角色过度授权**（不泄数据，后端 fail-closed），见 §4。

## 1. 现象与定位（三处证据）

| 层面 | 检查 | 结果 |
|---|---|---|
| 前端门控 | 首版（`75cefde7`）按 `MSP_AREA_PERMISSIONS`（含 `msp:read`）判定 | 客户租户 admin（角色基线含 `msp:read`）**会被放行** → 缺陷窗口 15:03–15:13 |
| 前端门控（二版） | `33a373c4` 收敛为 `msp_ticket:read` | 客户 demo 账号全部被拦；但**客户租户角色克隆残留该码**时仍会放行 |
| 权限数据 | `itsm_demo` 客户租户角色 `msp_*` 码分布 | `IT总监`/`系统运维`/`运维总监`/`服务商租户管理员` 携带跨租户 msp 码（含 `msp_ticket:read`）→ 见 §4 |
| 后端 | `middleware/msp_rbac.go` / `msp_middleware.go` | `IsMSP = 租户类型 msp_provider + msp_role≠''`；非 MSP 一律 403（fail-closed，**无数据泄露**，前端呈现错误态分区） |

## 2. 修复（`bba84fc2`，双条件 fail-closed）

- `area-permissions.ts` 新增 `isMspProviderTenant()`（镜像后端 `tenantmode`：`msp_provider` + 历史别名 `msp`）；
- 新增 `components/auth/MspProviderGate.tsx`：**租户上下文非服务商 → 不渲染（不发请求）**；
  服务商再按数据面操作码 `msp_ticket:read` 判定；dashboard 分区改用它；
- 登录默认落点 `resolveDefaultLanding(permissions, tenantType)` 叠加租户类型参数；
- 回归测试：`MspProviderGate` 5 例（含「客户租户残留 msp_ticket:read」）、`landing` 7 例、
  `CrossTenantOverview` 门控 4 例（客户租户零请求）；tsc/eslint 全绿。

## 3. 线上验证（`1.8.0-dev.bba84fc2`，浏览器实测）

| 场景 | 结果 |
|---|---|
| 服务商 `demo.msp.manager` | 登录落 `/msp/workbench`；`/dashboard` 分区渲染正常（客户 3 / 未关闭 22 / 超期 5 / 临近 6），无错误态 |
| 客户租户 admin `demo.mfg.admin`（仅 `msp:read`） | 落 `/dashboard`，无分区、无 MSP 菜单 |
| **过度授权回归**：临时账号 `tmp.opstest`（客户租户 + `系统运维` 角色，权限集含 `msp_ticket:read`） | 落 `/dashboard`；**无跨租户分区**、侧栏仅「服务台」；测后账号已删除 |
| 容器/版本 | 6/6 healthy；`/api/v1/version = 1.8.0-dev.bba84fc2` |

## 4. 遗留发现：客户租户角色过度授权（后端/种子侧，建议单独任务）

演示库实测（`itsm_demo`）：客户租户 `demo-mfg/med/edu` 的内置角色
`IT总监`/`系统运维`/`运维总监`/`服务商租户管理员` 携带跨租户 `msp_*` 码
（含 `msp_ticket:read`/`msp_customer:read`/`msp_report:read` 等）。

- 根因：内置角色定义（`internal/authz/roles.go`：`it_director`/`ops_director` 走 `allExcept`
  仅排除部分 msp 写码；`provider_tenant_admin = admin ∪ msp_admin`）随租户模板克隆进客户租户；
  现有客户侧对账 `applyCustomerScopeMSPRoleBaseline` 只覆盖 `msp_*` 命名的 5 个角色；
- 影响：菜单/入口层会按权限码放行（如 `/msp/*` 区域、本分区修复前），但后端 `IsMSP` 判定
  fail-closed → 表现为「入口可见、数据 403」；
- 建议：扩展客户租户角色对账（供给期 + 存量回填）剥离跨租户 `msp_*` 码（保留 `msp:read`
  视产品口径），或按租户类型裁剪内置角色克隆；并补充回归守卫测试。

## 5. 回滚

```bash
cd /home/n9e/itsm/repo
sudo sed -i 's/^VERSION=.*/VERSION=1.8.0-dev.33a373c4/' .env.demo
sudo docker compose --env-file .env.demo -f docker-compose.demo.yml up -d
```
