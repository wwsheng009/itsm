# 硬编码角色白名单审计（2026-11-11）

**Status: current** — 审计基线：8089 `1.8.0-dev.e1599ffd`；代码锚点 `e1599ffdb5fdf56b44aa783261fe6535bf5714f2`；当前运行态已更新为 `1.8.0-dev.bf131540`（Phase 3 审计 membership 归因修复）。

> **审计口径**：授权决策应以权限码、资源归属和统一角色能力（`domain/role` / `middleware`）为准；
> 角色字符串只允许出现在角色定义、种子、展示映射、通知收件人筛选等非授权场景。
> 本次重点排查类似 `ticket_attachment_service.go` 的 staff 角色 switch 造成误拒绝/500 的模式。

## 1. 已修复

| 位置 | 问题 | 修复 |
|---|---|---|
| `itsm-frontend/src/components/layout/header/Header.tsx` | AI 入口 `hasPermission('ai:read') || isAdmin()`：admin 角色可绕过资源权限看到入口，后端仍 403 | 改为仅 `hasPermission('ai:read')`；新增 Header 回归用例 |
| `itsm-backend/handlers/ai/service.go` | autopilot rollback 通知使用 `user.RoleIn("admin","super_admin","tenant_admin")`；`tenant_admin` 不是 `users.role` 枚举，收件人筛选存在无效角色码 | 改为合法的 `RoleAdmin/RoleSuperAdmin/RoleSysadmin` |
| `itsm-backend/handlers/ticket/service.go` | 重复维护 `IsDataScopeAllRole`，且漏 `sysadmin`，与中央 `datascope` 漂移 | 委托 `datascope.IsDataScopeAllRole` |
| `itsm-backend/service/ticket_service.go` | 重复维护管理角色白名单 | 改为 `domain/role.IsAdminLike` |
| `itsm-backend/service/msp_record_write.go` | 重复维护管理角色白名单 | 改为 `domain/role.IsAdminLike` |

### 1.1 穷尽复核补漏（同一审计口径）

| 位置 | 问题 | 修复 |
|---|---|---|
| `itsm-backend/service/ai_health.go` | AI 健康告警收件人使用 `user.RoleIn("admin","super_admin","tenant_admin")`；`tenant_admin` 不是 `users.role` 枚举，管理员筛选静默漏人 | 改用合法的 `user.RoleAdmin/RoleSuperAdmin/RoleSysadmin`，并用 `TestAIHealthService_AlertRecipientsUseValidUserRoleEnum` 锁定 |
| `itsm-backend/service/approval_service.go` | 审批人 fallback 词表含无效用户角色 `tenant_admin` / `workflow_admin`，动态审批人解析失败时可能找不到租户管理员 | 收敛为 `user.RoleSuperAdmin/RoleAdmin/RoleSysadmin`，保留既有优先级；新增 `TestResolveTenantAdminApprover_UsesValidUserRoleEnum` |
| `itsm-backend/service/msp_record_write.go` | 工作台 `allowedActions` 在 provider 写门禁已通过后，仍用家租户 `users.role` 做 owner/assignee 行级白名单；provider 深切换时家租户 `end_user` 会误显示不可写，而域服务按目标租户 `data_scope=all` 放行 | provider 调用方（`MSPRole` 非空）不再叠加家租户角色行级守卫；写门禁 `msp_ticket:write` / 客户 active / 终态校验仍 fail-closed，非 MSP 调用方保持原行级守卫。三张矩阵测试补 provider 断言；`05-multi-tenant-business.spec.ts` MT-3 增加运行态 `allowedActions.status.allowed=true` 断言 |

## 2. 复核为“有意设计”，不改

| 位置 | 结论 |
|---|---|
| `service/ticket_comment_service.go` `canManageInternalComments` | `super_admin/admin/manager/agent/technician/security` 白名单与 XFD8 一致：内部备注租户内隔离，provider 深度态明确非特权。仍属角色白名单，但当前产品语义明确，未擅自放宽 |
| `service/tenant_user_admin.go` / `TenantUsersDrawer.tsx` 最后一个 admin 保护 | 业务规则（避免租户失去管理员），不是通用授权白名单 |
| `internal/authz/roles.go`、`middleware/rbac.go` RoleRank/平台角色词表 | 角色 → 权限的权威定义/派生表，集中维护是正确的 |
| 角色标签、用户列表 admin 计数、profile 颜色 | 展示/业务统计，不构成授权判定 |

## 3. 仍需产品决策的风险

| 位置 | 风险 | 建议 |
|---|---|---|
| `service/ticket_workflow_service.go` `ensureCanViewTicketCC` / `ensureCanManageTicketCC` | 服务层仍有 `super_admin/admin/manager/technician` 角色白名单。路由已是 `workflow:read` / `workflow:write`；自定义角色即使获得权限，也可能因角色 code 不在白名单而被拒绝 | 将 handler 的权限判定结果或 actor permission 传入服务层，移除服务层角色白名单；或明确将 CC 管理限定为这些角色并补文档 |
| `AuthGuard` / `RoleGuard` / `RouteGuard` / `route-config.meta.roles` | 前端存在角色模式路由守卫，但当前活跃路由使用 `RequireArea`/`RequireCapability`/`PermissionGate`，这些旧守卫基本未接线；未来误用会把“有权限码但角色不同”的用户挡住 | 删除/标记 legacy，或统一改为权限码门控 |
| `lib/store/auth-store.ts` `canViewTickets` 等 `|| isAdmin()` 兼容 helper；`use-permissions.isAdmin` | 主要是遗留/未使用 helper；若后续被使用，会形成前端角色旁路 | 标记 deprecated，新增代码禁止使用；逐步替换为 `useCan/PermissionGate` |
| `pages/(main)/admin/tenants/index.tsx` `canManageTenantUsers = tenant:write && hasRole('super_admin')` | 平台租户用户管理入口叠加硬编码 super_admin，可能挡住平台自定义治理角色 | 以后端平台路由权限单源为准，确认后移除 role 条件 |

## 4. 验证

- 前端：`npx tsc --noEmit`、改动文件 ESLint、`Header.tenant-scope` Jest 4/4 通过；新增 E2E 断言后 `05-multi-tenant-business` 4/4 通过。
- 后端：`go test ./service -count=1` 全包通过（86.474s）；远端 Docker 发布构建通过；8089 六容器 healthy、`/api/v1/healthz` ok、`/api/v1/readyz` ready。
- E2E：按 spec 分组累计 **45/45** 通过（00 smoke 6、01 permissions 8、02 associations 8、03 workflows 7、04 UI 11、05 多租户 4、06 附件 1）。
  - 本机单进程一次跑 45 条时 Chrome 因 Windows 内存不足 OOM；单个 spec 独立进程复跑均通过，属于本机测试运行器资源问题，不是产品失败。
- 部署：当前 `1.8.0-dev.bf131540`（审计基线版本 `e1599ffd`）；备份 `demo-release-1.8.0-dev.bf131540-20261111T162032Z`；C 方案变更见 [`membership-tenant-identity-c-plan.md`](../multi-tenant/plan/membership-tenant-identity-c-plan.md)。

## 5. 建议门禁

新增静态检查（CI）：
1. 禁止在 `handlers/` / `service/` 的授权分支中出现角色字面量集合；允许 `domain/role`、`internal/authz`、`middleware` 的定义文件例外。
2. 前端新增授权门控必须使用 `useCan` / `PermissionGate` / `RequireCapability`；`RoleGuard` / `isAdmin()` 旁路标记 legacy。
3. 对“角色白名单 + 路由权限”双门禁做 parity 测试，防止自定义角色被误拒绝。
