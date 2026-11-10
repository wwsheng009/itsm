# MSP 深切换后按角色 data_scope 取数修复方案

> 状态：Phase 1-3 完成；Phase 4 列表接入完成，写面/角色 UI 待后续（2026-11-10）
> 范围：provider 员工深度切换到客户租户后，工单/问题等租户内单据的列表与统计口径
> 关联：MSW-4、canon I2/I3/I9、C2 客户租户角色基线

## 1. 现象

provider 员工通过 `POST /api/v1/auth/switch-tenant` 切到客户租户后：

| 接口 | 现象 |
|---|---|
| `GET /api/v1/tickets` | `items: []`、`tickets: []`、`total: 0` |
| `GET /api/v1/tickets/stats` | `total: 30`、open/inProgress/resolved/pending 均有数字 |
| `GET /api/v1/msp/workbench/tickets` | 403，`msp_role=provider_admin`、`rbac_role=msp_manager`、`required_action=read`、`required_resource=msp_ticket` |

即：**列表为空、统计有数、跨租户工作台被拒**。

## 2. 根因

### 2.1 角色其实已经分配

分配链路 `MSPAllocationService.Create` 会调用
`MembershipSyncService.SyncAllocationMembership`，在客户租户创建
`user_tenant_memberships`：

- `service/msp_allocation_service.go:123-142`
- `service/membership_sync.go:162-249`
- `service/membership_sync.go:483-489`
  - `allocation.role=specialist` → `msp_specialist`
  - 其他（primary/backup）→ `msp_tech`

因此**不需要在 switch-tenant 时再分配角色**。切换授权本身也要求
active allocation 或客户侧 invite（`internal/authz/switch.go:74-105`）。

### 2.2 请求期 RBAC 已能通过

`middleware/rbac.go:1084-1101` 已把 membership 指向的角色 code 并入
`c.Set("roles", ...)`。所以 `GET /tickets` 的
`RequirePermission("ticket","read")` 在客户租户内可以通过
（C2 基线含 `ticket:read`），请求确实进入 handler。

### 2.3 列表 data scope 用了错误的角色来源

- `handlers/ticket/handler.go:273`：`currentRole := c.GetString("role")`
  - 切换后 JWT 主角色仍是 `users.role`（例如 `end_user`）
  - 这是 `SwitchTenantWithRevoke` 的既定设计：只在切回 home/provider 时提级
    （`handlers/auth/service.go:197-204`）
- `handlers/ticket/service.go:141-146`：
  ```go
  dataScope := datascope.DataScopeAll
  if !IsDataScopeAllRole(currentRole) {
      dataScope = datascope.DataScopeOwnedOrAssigned
  }
  ```
- `IsDataScopeAllRole` 只认硬编码 `super_admin/admin/manager/sysadmin`
  （`domain/role/role.go:43-50`），不读 `roles.data_scope`，也不认 membership
  角色 `msp_tech`。
- 结果：客户租户内 `end_user` → `owner` → 列表 0 条。

### 2.4 统计接口完全不受 data scope 约束

`handlers/ticket/repository_impl.go:304-343` 的 `GetStats` 直接调用
`CountByStatus(ctx, tenantID)` / `CountByPriority(ctx, tenantID)`，没有
`DataScope`/`CurrentUserID` 参数，因此返回租户全量 30 条。

### 2.5 为什么 `role.data_scope` 当前不可直接信任

- `ent/schema/role.go:34-37`：`data_scope` 默认值是 `all`
- `pkg/seeder/seeder.go:1169-1174`：创建内置角色时未设置 `data_scope`
- `pkg/seeder/tenant_provisioner.go:202-204`：克隆角色时未复制 `data_scope`
- `service/role_service.go:143-150`：创建角色时未设置 `data_scope`
- 当前没有迁移回填 `data_scope`

因此当前库中 `end_user` / `agent` 等角色大概率也是 `all`。如果只把列表
改为读 `role.data_scope` 而不先修正数据，会让普通用户看到全租户单据。

### 2.6 `/msp/workbench/*` 403 是设计边界

- `middleware/msp_rbac.go:140-189`：`provider_admin` 映射 `msp_manager`，
  按**当前租户**（客户租户）的 roles 校验 `msp_ticket:read`
- `internal/authz/roles.go:452-472`：客户租户 C2 基线明确剥离跨租户
  `msp_*` 权限码
- 因此客户作用域下 `/msp/*` 必然 403；深度模式应使用 `/tickets`、
  `/problems` 等租户内接口。

## 3. 决策

| 方案 | 结论 |
|---|---|
| switch-tenant 时创建/更新角色 | **不采用**：非幂等、与 allocation 生命周期冲突、撤销后可能残留 |
| 在 ticket 域特判 provider + allocation | **不采用**：绕过角色模型，形成第二授权真源 |
| 放宽客户租户 `msp_ticket:read` | **不采用**：破坏 C1/C2 隔离 |
| **membership 角色 + `roles.data_scope`** | **采用** |

核心口径：

1. **角色来源**：当前租户的存活 membership → `role_id` → 同租户 `roles`；
   RBACMiddleware 已物化的 `roles`（主角色 + m2m + membership）参与取最宽范围。
2. **data_scope 来源**：`roles.data_scope`；角色行缺失时回退
   `datascope.DataScopeFromRole(code)`。
3. **不要求当前租户是 provider**：深切换正是客户租户，必须按目标租户角色解析。
4. **super_admin 直通**：保持 `["*"]` 与全范围语义。
5. **统计与列表同口径**：任何列表收窄都必须同时作用于 stats。
6. **写权限 ⊆ 读权限**：写路径使用同一解析结果，避免“看得到改不了”或“读不到能写”。

## 4. 内置角色 data_scope 矩阵

| 角色 code | data_scope | 说明 |
|---|---|---|
| `super_admin`, `admin`, `manager`, `sysadmin` | `all` | 现有 `IsAdminLike` 口径 |
| 客户租户内 `msp_viewer`, `msp_tech`, `msp_specialist`, `msp_manager`, `msp_admin` | `all` | 服务商员工在客户租户内应看到客户全部单据 |
| `provider_tenant_admin` | `all` | provider 首管 |
| `dept_manager`, `sd_manager`, `l1_support`, `l2_support` | `department` | 工单仓储暂只实现 all/owner；department 落库后按安全收窄处理 |
| `agent`, `technician`, `end_user`, `guest` | `owner` | 与当前硬编码行为一致 |
| 其他 config 角色 / 自定义角色 | `owner`（新角色默认）或保留已有配置 | 安全默认 |

> 注：`it_admin`、`security_admin`、`ops_manager` 等目前不在
> `IsAdminLike` 中，本次先保持 `owner`，避免行为回归；后续可按业务调整。

## 5. 实施清单

### Phase 1：角色数据修正（本批）

- [x] `pkg/seeder/seeder.go`：`RoleSeed` 增加 `DataScope`；内置角色写入规范值；
      已有内置角色按 code 幂等校正；内置角色标记 `is_system=true`
- [x] `pkg/seeder/tenant_provisioner.go`：克隆角色时复制 `data_scope`
- [x] `service/role_service.go` + `dto/role_dto.go`：创建/更新角色支持 `dataScope`，
      默认 `owner`；校验枚举
- [x] 新增迁移 `migrations/20261110_role_data_scope_backfill.sql`（含 down）
- [x] 迁移只更新保留内置角色 code，不覆盖自定义角色

### Phase 2：请求期解析器（本批）

- [x] `handlers/common/datascope/datascope.go`：
      - `ResolveForActor(ctx, client, tenantID, primaryRole, roleCodes)`
      - `CanWriteResourceWithScope(actorID, scope, ownerID, assigneeID)`
- [x] 新增 `handlers/common/scopectx`：从 Gin context 读取 `client`/`roles`，
      无 client 时安全回退
- [x] 单测：msp_tech=all、end_user=owner、super_admin=all、无 membership 回退、
      自定义 department/owner、写权限 ⊆ 读权限

### Phase 3：工单 / 问题读面与统计（本批）

- [x] `handlers/ticket/handler.go`：List / GetTicketStats 使用 `scopectx.Resolve`
- [x] `handlers/ticket/service.go`：List 接收 `dataScope`；新增 `GetStatsScoped`
- [x] `handlers/ticket/repository_impl.go`：stats 按 scope 统计
- [x] `handlers/problem/handler.go`：List / GetStats 使用同一解析
- [x] `handlers/problem/service.go`：List/GetStats 接收 `dataScope`
- [x] 单测：provider 深切换（membership=msp_tech）→ 全量；原生 end_user → 本人；
      列表与统计一致

### Phase 4：其他域与写面（后续）

- [x] incident / change / service_request / release 列表接入同一解析
- [x] C2 读权限补齐：`problem:read` / `change:read` / `service_request:read` /
      `incident:read` / `release:read`（修复 `/problems` 等 GET 列表 2003）
- [x] 各域写路径改用 `CanWriteResourceWithScope`（请求 context 注入 DataScope）
- [x] incident / change / release 统计接入 scoped 统计
- [x] 新增 `scopectx.Middleware`：租户组请求期统一解析一次 DataScope
- [x] 角色管理 UI 暴露 `dataScope` 配置（all/department/owner）
- [x] `/readyz` 使用磁盘发现迁移的最新版本（不再固定 027）
- [x] 部署脚本显式构建 AI sidecar；`--skip-build` 预检三类应用镜像
- [ ] 清理 `IsDataScopeAllRole` 硬编码回退（保留迁移期兜底）

## 6. 验收口径

provider 员工切到客户租户后：

| 场景 | 期望 |
|---|---|
| `GET /tickets` | 返回客户租户全部工单 |
| `GET /tickets/stats` | total 与列表同口径 |
| `GET /problems` | 返回客户租户全部问题 |
| `GET /msp/workbench/tickets` | 仍 403（客户作用域不使用工作台接口） |
| 原生客户 `end_user` | 仍仅本人创建/被指派；统计同口径 |
| 客户租户 `msp_viewer` | 可看全部，无写权限 |
| 客户租户 `msp_tech` | 可看全部，可处理工单/问题 |

## 7. 风险与回滚

| 风险 | 处置 |
|---|---|
| 迁移误改自定义角色 | 只按保留内置 code 更新；自定义角色不动 |
| `data_scope` 数据未回填前上线解析器 | 解析器保留 `DataScopeFromRole` 回退；迁移与代码同批发布 |
| 统计口径变更导致看板数字变化 | 属预期修复；验收时对比列表 total |
| 客户管理员自定义内置角色范围被覆盖 | 迁移只把 `all` 收敛为规范值；后续如需自定义，走角色更新接口并登记 |
| 回滚 | down 迁移恢复 `all`；代码回退解析器调用点 |

## 8. 跟踪记录

| 日期 | 变更 | 状态 | 备注 |
|---|---|---|---|
| 2026-11-10 | 方案落库 | 完成 | 根因、决策、矩阵、验收 |
| 2026-11-10 | Phase 1 角色数据 | 完成 | seeder/provisioner/role service/migration |
| 2026-11-10 | Phase 2 解析器 | 完成 | datascope.ResolveForActor + scopectx + 单测 |
| 2026-11-10 | Phase 3 工单/问题 | 完成 | 列表 + 统计同口径 |
| 2026-11-10 | Phase 4 其他域 | 列表完成 | incident/change/request/release 读面接入；写面/UI 待后续 |
| 2026-11-10 | C2 读权限补齐 | 完成 | 客户租户 msp_* 增加 problem/change/request/incident/release read |

## 9. 实施记录（2026-11-10）

### 9.1 变更文件

| 文件 | 变更 |
|---|---|
| `domain/role/role.go` | data_scope 词表、规范值、rank、内置 code 集合 |
| `handlers/common/datascope/datascope.go` | `ResolveForActor`、`CanWriteResourceWithScope` |
| `handlers/common/scopectx/scopectx.go` | Gin context → DataScope 统一入口 |
| `router/router.go` | 租户组挂载 `scopectx.Middleware`，请求期注入 DataScope |
| `pkg/seeder/seeder.go` | `RoleSeed.DataScope`、内置角色幂等校正 |
| `pkg/seeder/tenant_provisioner.go` | 克隆/存量角色 data_scope 收敛 + `is_system` |
| `dto/role_dto.go` / `service/role_service.go` | 角色创建/更新支持 dataScope |
| `handlers/ticket/*` | List/GetStatsScoped/仓储 scoped count |
| `handlers/problem/*` | List/GetStatsScoped/仓储 scoped stats |
| `handlers/{incident,change,service_request,release}/*` | 列表接入 `scopectx.Resolve` |
| ticket/problem/incident/change/service_request/release 写服务 | 写守卫改用 `CanWriteResourceFromContext` |
| incident/change/release stats | 新增 `GetStatsScoped`，与列表同口径 |
| `migrations/20261110_role_data_scope_backfill*.sql` | 存量角色 data_scope 回填/回滚 |
| `migrations/20261110_c2_read_permissions*.sql` | 客户租户 msp_* 角色读权限回填/回滚 |
| `itsm-frontend/.../admin/roles/index.tsx` | 角色编辑暴露 dataScope 选择 |
| `migration/discovery.go` + `router/initialization_readiness.go` | `/readyz` 使用磁盘发现最新迁移版本 |
| `scripts/deploy-prod.sh` | 显式构建 AI sidecar；skip-build 镜像预检 |

### 9.2 验证记录

| 命令/范围 | 结果 |
|---|---|
| `go build ./domain/... ./dto/... ./pkg/seeder ./service ./handlers/... ./repository/... ./middleware/...` | 通过 |
| `go test ./handlers/common/datascope` | 通过 |
| `go test ./handlers/ticket ./handlers/problem ./repository/ticket` | 通过 |
| `go test ./handlers/ticket -run TestTicketStatsScopedForEndUser` | 通过（end_user 4→2、admin 全量，列表/统计同口径） |
| `go test ./handlers/problem -run TestProblemListAndStatsRespectDataScope` | 通过（owner 2 / all 3） |
| `go test ./handlers/incident ./handlers/change` | 通过 |
| `go test ./pkg/seeder -run TestProvisionTenant_CustomerScopeMSPBaseline` | 通过（含 msp_tech=all / end_user=owner 断言） |
| `go test ./service -run TestCreateRole_DefaultIsActiveTrue` | 通过（默认 owner + 更新 all + 非法值拒绝） |
| `handlers/service_request` 全量 | 既有 Windows 临时文件清理失败，与本次改动无关 |
| 238 实测（provider 深切换） | `/tickets` total=2 returned=2；stats total=2；`/msp/workbench` 403；切回 home 成功 |
| 238 实测（C2 读权限修复前） | `/problems` 返回 2003（缺 `problem:read`） |
| 238 实测（C2 读权限修复后） | `/problems` code=0 total=1 returned=1；版本 `1.8.0-dev.a6281950` |
| 本地单测（P1） | ticket/problem/incident/change/datascope 全通过；release scoped stats 通过 |
| 238 实测（P1 写权限） | provider 客户作用域 `PUT /tickets/:id/status` 非法状态返回 400（非 403），说明写守卫已按 all 放行 |
| 238 实测（P1 原生隔离） | 客户 `custa.smoke1`（end_user）：`/tickets` total=1，`/tickets/stats` total=1 |
| 本地前端类型检查 | `tsc --noEmit` 通过；roles/api/i18n eslint 通过 |
| 本地 migration/router 测试 | `LatestAvailableVersion` + readiness 测试通过 |

### 9.3 待确认/后续

1. 生产发布时建议同批执行 `20261110_role_data_scope_backfill.sql`；
   即使迁移暂未执行，`ResolveForActor` 也会按内置词表规范值收敛旧默认 all
   （`end_user/agent → owner`、`msp_tech → all`），不会产生越权；
   迁移用于修正库中展示值并支撑后续管理员自定义；
2. 角色管理 UI 增加 dataScope 选择（当前后端已支持，前端未暴露）；
3. 各域写路径切换到 `CanWriteResourceWithScope`（当前读面已按角色解析，写面仍主要用 JWT 主角色）；
4. 观察 `/msp/workbench/*` 在客户作用域仍 403——这是 C1/C2 边界，不是回归。
5. `department` 档位在 ticket/problem/incident/change/service_request/release 仓储中暂按
   owner 安全收窄；需要部门维度可见性时再补部门树过滤。

### 9.4 238 发布记录

| 项 | 值 |
|---|---|
| 目标 | `http://172.18.3.238:8088` |
| 版本 | `1.8.0-dev.7c0ca10f`（含 P1 写路径与统计收口） |
| 镜像 | backend/frontend/ai-service/worker 同 tag `1.8.0-dev.7c0ca10f` |
| 迁移 | `20261110_role_data_scope_backfill`、`20261110_c2_read_permissions` 均已登记 |
| 验证账号 | `prov.smoke.agt1012232309`（role=end_user，mspRole=provider_agent，home=2，客户=3） |
| 深切换结果 | 切客户 200；`/tickets` total=2；`/tickets/stats` total=2；`/problems` total=1；`/msp/workbench` 403；切回 home 200 |
| 角色数据 | 客户租户 `msp_tech` data_scope=all；`end_user` data_scope=owner；`is_system=true` |
| C2 读码 | 租户 3 `msp_tech` 已具备 problem/change/service_request/incident/release read |
| 写路径 | provider 员工非法状态流转返回 400（通过行级守卫）；原生 end_user 仍仅本人 |
