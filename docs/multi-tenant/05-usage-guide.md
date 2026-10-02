# 05 · 使用指南：客户端、服务商端与平台端

> **状态**：当前
> **更新日期**：2026-09-28
> **适用**：全体使用者；前置阅读 [03](./03-customer-dimension.md)、[04](./04-provider-dimension.md)

> **定位（as-is）**：本文为**现状使用指南**（2026-09-28 实测）；其中"连续操作走切换"的表述已被[工作台方案](./plan/msp-cross-customer-workbench-and-filter-plan.md)（`REV-1`/`WB2`）修订——日常跨客户操作用工作台+过滤器，切换仅深度操作。目标口径以 [canon](./plan/msp-concept-model-and-architecture-canon.md) 为准（见[一致性审计](./plan/msp-docs-consistency-audit.md) C2）。

## 1. 三类使用者与入口

| 使用者 | 登录租户 | 主要入口 | 能做什么 |
|---|---|---|---|
| 客户用户 | 自己的客户租户 | Web 客户端（工单/知识库/CMDB/服务目录） | 提交与处理本租户业务 |
| 客户管理员 | 自己的客户租户 | 客户端管理页 | 管理本租户用户/角色/配置 |
| 服务商管理员 | provider 租户 | `/msp`、`/msp/management` | 客户列表、分配管理、报表 |
| 服务商工程师 | provider 租户 | `/msp`、客户工单视图 | 处理被分配客户的工单 |
| 平台运维 | 任意（通常 super_admin） | `/admin/tenants`、运维接口 | 租户开通/状态/平台配置 |

## 2. 客户用户：日常使用

1. **登录**：输入账号密码；如系统配置了独立域名则直接访问域名，否则在登录请求中携带租户代码（`tenantCode`，`dto/auth_dto.go:13`）；
2. 登录后 JWT 内固定携带本租户 `tenant_id`，后续请求无需重复指定租户；
3. 正常使用工单、知识库、服务目录、CMDB 等模块——所有数据自动限定在本租户内；
4. 若提示"租户已被暂停或过期"（403），联系服务商处理（见 03 §8）。

## 3. 服务商端：跨客户运营

**第一步：确认 MSP 身份可用**

- `GET /api/v1/msp/status`（登录即可）：返回当前用户是否 MSP 员工/管理员；
- `GET /api/v1/msp/context`（`msp.read`）：返回 MSP 角色与可访问客户集合。

**第二步：查看可访问客户**

- `GET /api/v1/msp/customers`（`msp_customer.read`）——只列出被分配（`MSPAllocation`）的客户；管理页对应 `/msp`。

**第三步：跨客户处理**

| 任务 | 接口 | 权限 |
|---|---|---|
| 查看某客户工单 | `GET /api/v1/msp/customers/:customer_tenant_id/tickets` | `msp_ticket.read` |
| 指派 MSP 技术员 | `POST /api/v1/msp/tickets/:id/assign` | `msp_ticket.write` |
| 客户服务报表 | `GET /api/v1/msp/reports/customers` | `msp_report.read` |
| 员工绩效报表 | `GET /api/v1/msp/reports/performance` | `msp_report.read` |

**跨客户两种方式**：

- 单次请求：请求头带 `X-Customer-Tenant-ID: <客户租户ID>`（必须命中分配列表，否则 403）；
- 连续操作：调用租户切换（`SwitchTenant`）换发目标客户上下文的 JWT 后再操作（`handlers/auth/service.go:114`）。

> **目标口径（2026-09-30，canon v1.0；本文其余内容为现状 as-is）**：日常跨客户处理走"**跨客户工作台 + `CustomerFilter` + 条目级操作**"（无需切换会话）；头通道为单请求只读；仅"深度操作"（客户内配置/用户/连续多步）走会话切换（`POST /api/v1/auth/switch-tenant`）。目标步骤见[实施方案 `IP-P0-7`/`IP-P0-8`](./plan/msp-implementation-plan.md)。
>
> **实现状态（2026-09-30，IP-P0-7 + IP-P0-8）**：工作台列表/徽标/条目级回复/改状态四个端点已上线（见 §9）；每条返回 `allowedActions[]`，暂停客户条目整条只读（`CUSTOMER_INACTIVE`）；显式请求未分配客户 403 `MSP_ALLOCATION_REQUIRED`（审计 `tenant.scope_denied`）。前端已落地：顶栏 `CustomerFilter`（多选/全部+搜索+徽标，只改视图，provider-only）、`/msp/workbench` 页（客户列 + 行内操作 + 双态指示）、"进入客户"深度切换（`POST /auth/switch-tenant`，切换后数据缓存清空）；客户账号不渲染过滤器/工作台入口。
>
> **审计看板（2026-09-30，IP-P1-8）**：`/msp/audit` 提供窗口内（7/30/90 天）跨租户审计聚合与"越权尝试/冲突告警"面板——未分配客户访问（`tenant.scope_denied`）与 header/JWT 冲突探测（`tenant.probe_denied`）落库后可直接检索；头/JWT 冲突自本批起为 **401 + `TENANT_MISMATCH_REJECTED`**（此前被静默忽略，`07:G9` 已复核更正）。

**分配管理（服务商管理员）**：

- 列表 `GET /api/v1/msp/allocations`；新建 `POST /api/v1/msp/allocations`；解除 `POST /api/v1/msp/allocations/deallocate`（`docs/acl-manifest.yaml:2605-2620`）；
- 管理页 `/msp/management`；角色建议：主责 `primary`、备份 `backup`、专项 `specialist`。

## 4. 平台端：租户与平台运维

| 任务 | 接口/入口 |
|---|---|
| 租户列表/详情 | `GET /api/v1/tenants`、`GET /api/v1/tenants/:id`（`tenant.read`） |
| 创建/更新租户 | `POST /api/v1/tenants`、`PUT /api/v1/tenants/:id`（`tenant.write`） |
| 状态变更（暂停/恢复） | `PUT /api/v1/tenants/:id/status` |
| 模板开通 | `go run ./cmd/provision_tenant -tenant-id <ID>` |
| 管理页 | `/admin/tenants` |

## 5. 典型流程 A：新客户接入（8 步）

1. 平台/服务商创建租户：`POST /api/v1/tenants`，`type=msp_customer`，填 `parentTenantId`/`mspProviderId`/`planCode`/`expiresAt` 等；
2. 执行模板开通：`go run ./cmd/provision_tenant -tenant-id <ID>`；
3. 校验 readiness（roles/permissions/role permissions/menus/groups/SLA/CI types 均非 0）；
4. 在客户租户内创建客户用户并分配客户侧角色；
5. 为服务商工程师创建 `MSPAllocation`（`POST /api/v1/msp/allocations`）；
6. 验证：客户用户登录可见本租户数据；服务商工程师 `GET /msp/customers` 能看到该客户；
7. 验证隔离：客户 token 访问其他客户数据被拒（401/403）；
8. 交付客户（告知登录方式：域名或 `tenantCode`）。

## 6. 典型流程 B：服务商工程师日常处理工单

1. 登录 provider 租户 → `/msp` 查看客户列表；
2. 选择客户（或对后续请求携带 `X-Customer-Tenant-ID`）；
3. 查看该客户工单（`GET /msp/customers/:id/tickets`），按需指派技术员（`POST /msp/tickets/:id/assign`）；
4. 需要连续操作时切换租户上下文；
5. 周期性查看客户报表（`GET /msp/reports/customers`）。

## 7. 典型流程 C：工程师换岗/离场

1. 解除/调整分配：`POST /api/v1/msp/allocations/deallocate`（写 `deassigned_at`）；
2. 重新指派未结工单；
3. 清空离场人员的 `msp_role`（撤销 MSP 身份）；
4. 定期复核 `msp_allocations` 与团队实际一致。

## 8. 典型流程 D：客户退租

1. 结清未结工单/流程；
2. 解除该客户的全部 `MSPAllocation`；
3. 按租户导出/归档数据；
4. `PUT /api/v1/tenants/:id/status` 置 `suspended`（缓冲期）→ 最终 `deleted`（软删除 + 审计）。

## 9. 接口速查

| 场景 | 方法与路径 | 权限 |
|---|---|---|
| MSP 状态自检 | `GET /api/v1/msp/status` | 登录即可 |
| MSP 上下文 | `GET /api/v1/msp/context` | `msp.read` |
| 分配列表/新建/解除 | `GET/POST /api/v1/msp/allocations`、`POST /api/v1/msp/allocations/deallocate` | `msp_allocation.read/write` |
| 客户列表 | `GET /api/v1/msp/customers` | `msp_customer.read` |
| 客户工单 | `GET /api/v1/msp/customers/:customer_tenant_id/tickets` | `msp_ticket.read` |
| 指派技术员 | `POST /api/v1/msp/tickets/:id/assign` | `msp_ticket.write` |
| 工作台列表（跨客户） | `GET /api/v1/msp/workbench/tickets?customerTenantIds=all`（`status/priority/assigneeId/q/updatedAfter/sort/cursor/limit`） | `msp_ticket.read` |
| 工作台徽标 | `GET /api/v1/msp/workbench/summary`（open/slaRisk/unassigned） | `msp_ticket.read` |
| 工作台回复/改状态 | `POST /api/v1/msp/tickets/:id/reply`、`POST /api/v1/msp/tickets/:id/status` | `msp_ticket.write` |
| 审计看板聚合（IP-P1-8） | `GET /api/v1/msp/audit/summary?days=30` | `msp_report.read` |
| 客户/绩效报表 | `GET /api/v1/msp/reports/customers`、`/reports/performance` | `msp_report.read` |
| 审计查询（作用域） | `GET /api/v1/audit-logs?targetTenantId=&source=&actorAccount=`（`source=legacy` 查历史 NULL 行） | `audit_log.read` |
| 租户管理 | `GET/POST /api/v1/tenants`、`PUT /api/v1/tenants/:id(/status)` | `tenant.read/write` |

（来源：`docs/acl-manifest.yaml:1729-1761, 2593-2650`。）

## 10. 注意事项

- MSP 面仅在 `saas`/`saas_msp` 模式可用；`private` 下相关页面/接口 404（见 02 文档）；
- 所有 MSP 接口都经过"身份 + RBAC + 客户分配"三重校验，缺一不可；
- 未在分配列表中的客户，任何带 `X-Customer-Tenant-ID` 的请求都会被 403 拒绝。
- 审计作用域（IP-P0-10）：`source` 枚举 `login|switch|header|workbench|platform_selected|job|system`（历史行为 NULL，读侧显示 `legacy`）；跨租户操作审计行归属 **actor 家租户**，`target_tenant_id` 指向被操作客户，可按 `targetTenantId` 过滤。
