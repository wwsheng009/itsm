# RLS shadow 观察记录（联调环境）· 2026-10-03

> 状态：**观察完成（shadow，一轮）**｜范围：联调库（`127.0.0.1:15433/itsm_prod`）+ 8090 实例
> 关联：`database/rls/driver.go`（三档开关）、`database/rls/migrations/001_roles.sql`、`002_pilot_policies.sql`、方案 §7 灰度表（`RLS off→shadow→enforce`）
> 工具：`scripts/msp/rls-shadow-observation.ps1`（本页流量脚本；只发 GET 读请求）

---

## 1. 目标与判据

enforce 前置的"阴影观察"：以 `RLS_MODE=shadow` 运行真实实例，采集**缺租户上下文**（`rls: query without tenant scope`）与**已带租户**（`rls: shadow query`）两类日志，产出：

1. 需在 enforce 前补齐 ctx 的调用点清单（可归因到具体查询）；
2. 可白名单化的路径（预认证 / 平台面 / 系统任务——按设计走 system bypass 或 BYPASSRLS 连接）；
3. DB 侧与连接侧的差距清单（角色 / 策略 / 应用连接身份）。

判据：warn 归零，或剩余项全部落入白名单分类并有对应 system 路径。

## 2. 环境事实（2026-10-03 实测，psycopg2 只读探针）

| 项 | 实测 | 结论 |
| --- | --- | --- |
| 应用连接角色 `itsm` | `rolsuper=true, rolbypassrls=true` | 当前连接**天然绕过** RLS；enforce 只能触发装饰器的客户端 fail-closed，无法验证策略本身 |
| `itsm_app` / `itsm_admin` 角色 | **不存在** | 需按 `001_roles.sql` 建立（密码策略部署时注入） |
| `changes` / `vectors` 策略 | `rowsecurity=false`，策略 0 条 | 需执行 `002_pilot_policies.sql`（幂等） |
| `messages.tenant_id` | `nullable=NO`、空值 0 行（v1.46 收尾） | 与 RLS 无关的一致性核对项 |

## 3. 观察结果

运行方式：实例以 `RLS_MODE=shadow` + `LOG_LEVEL=debug` 启动；流量 = `admin`（超管）+ `mspagent`（**低权**：provider_agent/msp_tech）+ `custa_admin`（客户管理员），覆盖 `auth/me|menus|tenants`、`users`、`tickets`、`tickets/stats`、`dashboard/stats`（MSP 端点在 `DEPLOYMENT_MODE=private` 下 404，未覆盖）。

| 轮次 | 缺租户 warn | 带租户 debug | 说明 |
| --- | --- | --- | --- |
| 首轮（driver 仅 op+首词） | 131 | 28 | 无法归因 → 先补 `query` 预览字段 |
| 复跑（含 ACL/审计修复） | 100 | 52 | `endpoint_ac_ls` 21→0；`audit_logs` 4→1 |
| run8（最终构建，含全部 ctx 修复） | **25** | **131** | `audit_logs`=0、`endpoint_ac_ls`=0、roles/permissions/role_permissions=0、users 列表/菜单链路=0；残余见下表 |

**已修复（本轮，代码）**：

1. `database/rls/driver.go`：shadow/enforce 告警与 debug 增补 `query`（单行化、截断 160 字符、仅占位符）——可归因到具体 SQL；此前只有 `op+SELECT/INSERT`，无法定位调用方。
2. `middleware/smart_permission.go` → `loadACLsFromDB`：按 tenant 过滤但未补 ctx；现显式 `tenantctx.WithTenantID(ctx, tenantID)`（预检可能先于租户中间件）。**实测告警 21→0**。
3. `middleware/audit.go`（3 个写入点：`RecordAuthAudit`、`RecordTenantDeniedAudit`、gin 审计中间件）：写入 ctx 由 `context.Background()` 派生且未带租户；现已知租户 → `WithTenantID`，未知（登录失败/预认证）→ `WithSystemBypass`（审计不丢）。**run8 实测 0**。
4. `middleware/rbac.go`：RBAC 预检先于租户中间件（router.go 先挂 `RBACMiddleware`），且 auth-scoped 路由（`/auth/me|menus|tenants`）不挂租户中间件——预检在 JWT 解析出 tenantID 后**显式注入请求 ctx**，并对 `loadPermissionsFromDB`/`DBOnlyState` 两处加载器补租户 ctx。**roles/permissions/role_permissions 告警归零**。
5. `middleware/membership_permission.go`（`ResolvePermissions`）、`service/menu_service.go`（`GetUserMenus`）、`handlers/common/service.go`（`GetUserScoped`）补租户 ctx；`GetUserTenants`（跨租户聚合，IP-P0-6）显式 **system bypass**（平台面白名单）。**用户/菜单链路告警归零**。

**run8 残余 warn 25 条分类（enforce 前需处置）**：

| 类别 | 代表查询 | 处置方向 |
| --- | --- | --- |
| 无租户事务（12） | `Tx`（op=Tx，无 query） | 逐点定位事务开启方（登录/审计/组件状态链路），补 ctx 或 system |
| MCP/连接器元数据（7） | `mcp_server_tools` / `mcp_servers` / `connector_configs` / `tool_invocations` | 组件就绪/状态链路的平台级读取：按平台面显式 system bypass（或按 tenant_id 收窄） |
| 预认证/会话残余（6） | `tenants`（多租户选择）、`users`（用户名查找） | 设计上无租户上下文：enforce 前改为 **system bypass ctx**（或该路径专用 admin 连接） |

> MSP 专属路径（`/api/v1/msp/*`）本轮未覆盖：联调实例当前为 `DEPLOYMENT_MODE=private`（MSP 路由 404）。多 provider 场景的阴影观察应切 `saas_msp` 后复跑本工具。

## 4. 复现步骤

```powershell
# 1) 以 shadow + debug 启动（勿改 .env，仅进程级覆盖）
$env:RLS_MODE='shadow'; $env:LOG_LEVEL='debug'
Start-Process -FilePath 'E:\projects\itsm\itsm-backend\main.exe' -WorkingDirectory 'E:\projects\itsm\itsm-backend' -WindowStyle Hidden `
  -RedirectStandardOutput "$env:TEMP\itsm-rls.out.log" -RedirectStandardError "$env:TEMP\itsm-rls.err.log"

# 2) 低权 + 平台 + 客户三类会话发流（只读）
pwsh -NoProfile -File scripts/msp/rls-shadow-observation.ps1

# 3) 归因统计（日志在 itsm-backend/logs/itsm.log）
Get-Content itsm-backend\logs\itsm.log -Tail 20000 | Where-Object { $_ -match 'query without tenant scope' } |
  ForEach-Object { ($_ | ConvertFrom-Json).query } | Group-Object { $_.Substring(0,[Math]::Min(60,$_.Length)) } |
  Sort-Object Count -Descending | Select-Object -First 15 Count,Name
```

## 5. enforce 推进清单（按序）

1. **DB 侧**：执行 `database/rls/migrations/001_roles.sql`（itsm_app / itsm_admin，密码注入）+ `002_pilot_policies.sql`（changes/vectors pilot）。
2. **连接侧**：应用常规连接切 `itsm_app`；迁移/后台/平台面走 `itsm_admin`（BYPASSRLS）。当前 `itsm` 超管连接需在灰度完成前保留为回滚路径。
3. **调用点收口**：按 §3 剩余分类补齐 ctx 或显式 system bypass；`LOG_LEVEL=debug` 复跑本工具至 warn 归零/白名单化。
4. **enforce 灰度**：`RLS_MODE=enforce` 先在 pilot 两表验证（变更/知识检索路径），观察 `rls: enforce ...` 计数器与错误率；再扩展策略表。
5. **监控**：`Driver.Snapshot()`（`QueriesShadow/MissingTenant/SystemBypass/EnforceApplied`）接入指标/告警；`MissingTenant` 非零即回滚 shadow。

## 6. 变更记录

| 日期 | 变更 |
| --- | --- |
| 2026-10-03 | 首轮 shadow 观察：driver 告警增补 query 预览；修复 ACL/审计 4 处 ctx；产出剩余分类与 enforce 清单 |
| 2026-10-03 | run8 复跑（最终构建）：warn 131→**25**、带租户 28→**131**；审计/ACL/RBAC/用户/菜单链路归零；残余 25 = Tx 12 + MCP/连接器 7 + 预认证/会话 6。补充 RBAC 预检 ctx 注入与 `/auth/me`/menus/tenants 收口 |
