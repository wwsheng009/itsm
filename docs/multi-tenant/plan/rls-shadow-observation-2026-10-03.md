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
| run10/11（`saas_msp` 模式，覆盖 MSP 面） | 27 → 11 | 186 | MCP store 修复（工具缓存/状态回写）；登录链路收口 |
| run13/14（含 caller 归因） | 2 → 0 | 192 | 仅剩 `msp_allocation:toDTO`（1 对 DISTINCT 查询）→ 修复后归零 |
| **run15（终验，窗口含启动）** | **0** | **192** | `DEPLOYMENT_MODE=saas_msp` + `RLS_MODE=shadow` + debug；三类身份全端点 + 启动序列零缺租户告警 |

**已修复（本轮，代码）**：

1. `database/rls/driver.go`：shadow/enforce 告警与 debug 增补 `query`（单行化、截断 160 字符、仅占位符）——可归因到具体 SQL；此前只有 `op+SELECT/INSERT`，无法定位调用方。
2. `middleware/smart_permission.go` → `loadACLsFromDB`：按 tenant 过滤但未补 ctx；现显式 `tenantctx.WithTenantID(ctx, tenantID)`（预检可能先于租户中间件）。**实测告警 21→0**。
3. `middleware/audit.go`（3 个写入点：`RecordAuthAudit`、`RecordTenantDeniedAudit`、gin 审计中间件）：写入 ctx 由 `context.Background()` 派生且未带租户；现已知租户 → `WithTenantID`，未知（登录失败/预认证）→ `WithSystemBypass`（审计不丢）。**run8 实测 0**。
4. `middleware/rbac.go`：RBAC 预检先于租户中间件（router.go 先挂 `RBACMiddleware`），且 auth-scoped 路由（`/auth/me|menus|tenants`）不挂租户中间件——预检在 JWT 解析出 tenantID 后**显式注入请求 ctx**，并对 `loadPermissionsFromDB`/`DBOnlyState` 两处加载器补租户 ctx。**roles/permissions/role_permissions 告警归零**。
5. `middleware/membership_permission.go`（`ResolvePermissions`）、`service/menu_service.go`（`GetUserMenus`）、`handlers/common/service.go`（`GetUserScoped`）补租户 ctx；`GetUserTenants`（跨租户聚合，IP-P0-6）显式 **system bypass**（平台面白名单）。**用户/菜单链路告警归零**。

**残余 warn 收口（run10→run15，全部清零）**：

driver 告警增补 `caller` 归因（栈回溯跳过 `ent/` 生成码与 `database/` 拦截器帧），逐条定位并修复：

| 站点 | 处置 |
| --- | --- |
| 登录落 home 后租户视图加载（`handlers/common/service.go`） | `WithTenantID`（home 租户已知）；`last_active` 回写同 |
| 登录/注册/找回/密码策略、租户代码解析（预认证跨租户） | `tenantctx.SystemContext(...)` 显式平台作用域 |
| MCP 组件启动（`bootstrap:mcp-startup`）+ 工具缓存 `List/Replace` + 服务状态回写（`mcp/admin/store.go`） | system 作用域（平台组件运行态） |
| 连接器重水合（bootstrap）+ 连接器管理器内部 ctx（`connector/manager.go`） | system 作用域播种 |
| 工具队列启动恢复扫描（`service/tool_queue_durable.go`，跨租户运维语义） | system 作用域 |
| allocation 展示名解析（`service/msp_allocation_service.go:toDTO`，跨 provider/客户） | system 作用域 |

> MSP 专属路径已覆盖：run10 起实例以 `DEPLOYMENT_MODE=saas_msp` 运行，`/msp/*` 全端点 200（`mspagent` provider_agent 低权身份 + allocation 数据就绪），与 private/平台面合并观察至 run15 清零。

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

### 4.1 RLS 迁移与低权校验（无 psql 环境的替代路径）

`database/rls/migrations/*.sql` 可用仓库内工具执行（读取 `.env` 的管理连接；脚本幂等）：

```powershell
cd itsm-backend
go run -tags rlsapply ./cmd/rls-apply -files 001_roles.sql,002_pilot_policies.sql  # 应用
go run -tags rlsapply ./cmd/rls-apply -set-passwords                               # 用 DB_APP_ROLE_*/DB_ADMIN_ROLE_* 替换占位密码
go run -tags rlsapply ./cmd/rls-apply -verify                                      # 角色/策略状态 + itsm_app 低权探针（空表自动播种/清理）
go run -tags rlsapply ./cmd/rls-apply -rollback                                    # 回滚（002→001）
```

真实库集成测试（低权角色 + 策略隔离；两个 DSN 可同为超管，测试内部 `SET ROLE itsm_app`）：

```powershell
$env:RLS_TEST_DSN='host=... port=... user=itsm dbname=... password=... sslmode=disable'
$env:RLS_SETUP_DSN=$env:RLS_TEST_DSN
go test -tags integration_rls -v ./database/rls/...
```

## 5. enforce 推进清单（按序）

1. ✅ **DB 侧（2026-10-03 完成）**：`001_roles.sql` → `itsm_app`（login、非 superuser、非 BYPASSRLS，740 表授权）/ `itsm_admin`（BYPASSRLS），密码经 `rls-apply -set-passwords` 注入（`.env` 的 `DB_APP_ROLE_*`）；`002_pilot_policies.sql` → `changes`/`vectors` `rowsecurity+forcerowsecurity=true` + `tenant_isolation` 策略。
2. ⬜ **连接侧（enforce 真实强制的前置，未完成）**：请求池切 `itsm_app`、平台/后台池走 `itsm_admin`——需在 `InitDatabaseWithRLS` 双池化（启动期 schema 兼容 DDL / 水合仍走管理连接），bootstrap 按用途分发 client；当前应用仍为 `itsm` 超管连接（policy 不生效，保留为回滚路径）。
3. ✅ **调用点收口（2026-10-03 完成）**：按 §3 残余表逐条补齐 ctx 或显式 system 作用域；`LOG_LEVEL=debug` 复跑至 **warn=0**（run15，含启动窗口与 MSP 面；带租户 192 条）。
4. ◐ **enforce 灰度演练（driver 语义层，2026-10-03 完成；DB 强制待第 2 条）**：`RLS_MODE=enforce` 起服 + 三类身份全端点流量——逐请求与 `RLS_MODE=off` 基线**完全一致（diff=0；22×200 + 1×400 报表参数 + 1×404 视图 flag 关闭）**，日志仅 1 行 `rls: driver installed mode=enforce`、无 `enforce mode requires tenant_id` 错误 → fail-closed 不变量成立。
5. ⬜ **监控**：`Driver.Snapshot()`（`QueriesShadow/MissingTenant/SystemBypass/EnforceApplied`）接入指标/告警；`MissingTenant` 非零即回滚。enforce 演练期间该值为 0。
6. ⬜ **灰度扩展**：连接侧落地后，pilot 两表真实强制观察 → 逐步扩展策略表（R2 里程碑）。

**DB 级证据（联调库，2026-10-03）**：

- `rls-apply -verify`：`changes`/`vectors` RLS+FORCE 开启、`tenant_isolation` 策略存在；
- 低权探针（`SET LOCAL ROLE itsm_app`）：tenant=990001 可见 **1** 行（播种的自身探针）、无租户 **0** 行、tenant=999999 **0** 行（探针行自动清理）；
- 集成测试 `integration_rls`（真实库）：通过——session 变量注入/发放/回收、无租户拒绝（`ErrNoTenant`）、system bypass 跳过 SET；租户可见性正例因 `changes` 表空按设计 SKIP，由 `rls-apply` 播种探针覆盖。

## 6. 变更记录

| 日期 | 变更 |
| --- | --- |
| 2026-10-03 | 首轮 shadow 观察：driver 告警增补 query 预览；修复 ACL/审计 4 处 ctx；产出剩余分类与 enforce 清单 |
| 2026-10-03 | run8 复跑（最终构建）：warn 131→**25**、带租户 28→**131**；审计/ACL/RBAC/用户/菜单链路归零；残余 25 = Tx 12 + MCP/连接器 7 + 预认证/会话 6。补充 RBAC 预检 ctx 注入与 `/auth/me`/menus/tenants 收口 |
| 2026-10-03 | run10–15（`saas_msp` 全表面）：driver 告警增补 **caller 归因**；修复 MCP store/组件启动、连接器管理器/重水合、工具队列启动恢复、allocation 展示名、登录落 home 租户视图与预认证 system 作用域；**run15 warns=0 / dbg=192（含启动窗口）** |
| 2026-10-03 | **enforce 前置推进**：`rls-apply` 工具落地（角色/策略应用、密码注入、状态校验与低权探针）；DB 侧完成（roles + pilot 策略 + 探针实证）；集成测试 `integration_rls` 通过；`RLS_MODE=enforce` 演练与 `off` 基线逐请求一致（diff=0，无 fail-closed 错误）。剩余：连接侧双池化（请求=itsm_app / 平台=itsm_admin）与监控接入 |
