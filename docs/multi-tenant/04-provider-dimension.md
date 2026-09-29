# 04 · 服务商维度：provider 租户、分配与跨客户运营

> **状态**：当前
> **更新日期**：2026-09-28
> **适用**：服务商管理员；前置阅读 [01-architecture.md](./01-architecture.md)

> **定位（as-is）**：本文为**现状/运营快照**（2026-09-28 实测），**不是目标口径**；目标以 [canon](./plan/msp-concept-model-and-architecture-canon.md) 为准，差异与整改见[一致性审计](./plan/msp-docs-consistency-audit.md) §3（C11）。

## 1. 服务商租户（provider）

- **一个服务商 = 一个 `msp_provider` 租户**；服务商管理员/工程师账号全部落在该租户；
- 服务商员工必须满足：`users.msp_role` 非空（`ent/schema/user.go:69-72`），且租户类型为 `msp_provider`（`pkg/tenantmode/tenantmode.go`）；
- 创建方式：`POST /api/v1/tenants`（`tenant.write`，`docs/acl-manifest.yaml:1735-1737`），`type` 填 `msp_provider`；
- **注意**：普通 `admin` 角色不会自动获得 MSP 访问权，必须有真实的 `msp_role` 与分配（`middleware/msp_middleware.go:25-26`）。

## 2. `msp_role` 枚举与语义

| 取值 | 语义 | 典型归属 |
|---|---|---|
| `provider_admin` | MSP 管理员：管理分配、查看全部客户、运营报表 | 服务商租户 |
| `provider_agent` | MSP 客服/工程师：处理被分配客户的工单 | 服务商租户 |
| `customer_user` | 客户用户标记 | 客户租户 |

> 枚举定义：`ent/schema/user.go:69-72`。MSP 身份判定只认"provider 租户 + msp_role 非空"（`middleware/msp_middleware.go:19-27`）。

## 3. 分配模型（MSPAllocation）

| 字段 | 说明 |
|---|---|
| `msp_user_id` | 服务商员工（属于 provider 租户） |
| `customer_tenant_id` | 客户租户 ID |
| `role` | `primary` / `backup` / `specialist`（默认 `primary`） |
| `assigned_at` / `deassigned_at` | 分配与解除时间（解除后不再计入可访问集合） |

定义见 `ent/schema/msp_allocation.go:19-51`。该表是**平台级豁免表**（不属于任何租户，`internal/schema/tenant_guard.go:92`），语义上位于租户维度之上。

**分配决定可访问集合**：`MSPMiddleware` 拉取有效分配写入 `AllowedCustomers`；请求头 `X-Customer-Tenant-ID` 未命中 → 403（`middleware/msp_middleware.go:23, 133`）。

## 4. 分配操作

| 操作 | 接口 | 权限 |
|---|---|---|
| 查看分配 | `GET /api/v1/msp/allocations` | `msp_allocation.read` |
| 创建分配 | `POST /api/v1/msp/allocations` | `msp_allocation.write` |
| 解除分配 | `POST /api/v1/msp/allocations/deallocate` | `msp_allocation.write` |

（路由来源：`docs/acl-manifest.yaml:2605-2620`，实现在 `router/msp_routes.go`、`handlers/msp/handler.go`；前端管理页 `/msp/management` 与 `itsm-frontend/src/lib/services/msp-service.ts` 提供对应操作。）

**人员变更/离场流程（建议）**：

1. 解除或调整分配（写 `deassigned_at`），立即回收客户数据访问权；
2. 交接未结工单（`POST /api/v1/msp/tickets/:id/assign` 重新指派）；
3. 定期复核 `msp_allocations` 与实际团队一致性；
4. 员工 `msp_role` 清空（如离场）可整体撤销 MSP 身份。

## 5. 权限矩阵（MSP 面）

| 接口 | 权限 | 用途 |
|---|---|---|
| `GET /api/v1/msp/status` | 登录即可 | 当前用户 MSP 状态自检 |
| `GET /api/v1/msp/context` | `msp.read` | MSP 上下文（角色、可访问客户） |
| `GET /api/v1/msp/allocations` | `msp_allocation.read` | 分配列表 |
| `POST /api/v1/msp/allocations` | `msp_allocation.write` | 新建分配 |
| `POST /api/v1/msp/allocations/deallocate` | `msp_allocation.write` | 解除分配 |
| `GET /api/v1/msp/customers` | `msp_customer.read` | 可访问客户列表 |
| `GET /api/v1/msp/customers/:customer_tenant_id/tickets` | `msp_ticket.read` | 指定客户工单（MSP 视角） |
| `POST /api/v1/msp/tickets/:id/assign` | `msp_ticket.write` | 指派 MSP 技术员 |
| `GET /api/v1/msp/reports/customers` | `msp_report.read` | 客户服务报表 |
| `GET /api/v1/msp/reports/performance` | `msp_report.read` | 员工绩效报表 |

联合校验链：MSP 身份 → RBAC 资源权限 → 客户分配（`RequireMSPPermission`，`middleware/msp_rbac.go:125-127`）。

## 6. 跨客户操作的两种方式

| 方式 | 机制 | 适用 |
|---|---|---|
| 请求头 `X-Customer-Tenant-ID` | 单次请求指定目标客户，必须命中分配列表（否则 403） | 管理面/接口调用、按需切换 |
| 租户切换 `SwitchTenant` | 换发带目标租户 `tenant_id` 的 JWT；允许自身租户 / `super_admin` / 有效分配的服务商员工 | 需要连续在客户上下文内操作 |

（`handlers/auth/service.go:114`；目标租户须 `active` 且未过期。）

## 7. 边界与注意事项

- MSP 路由受部署模式门控：`private` 下整族 404，仅 `saas`/`saas_msp` 可用（`middleware/msp_gate.go:21-33`）；
- 分配是访问客户数据的**必要条件**，不可用 `admin` 角色或后端旁路替代；
- `msp_allocations` 不参与租户过滤（平台级表），修改权限仅限 `msp_allocation.write` 持有者；
- 服务商员工访问客户数据时，一切业务查询仍按目标客户 `tenant_id` 收窄（不因 MSP 身份而放开）。

## 8. 证据索引

- `ent/schema/user.go:69-72`、`ent/schema/msp_allocation.go:19-51`
- `middleware/msp_middleware.go:19-27, 133`、`middleware/msp_rbac.go:125-127`、`middleware/msp_gate.go:21-33`
- `handlers/msp/handler.go:100-308`、`handlers/auth/service.go:114`
- `router/msp_routes.go`、`docs/acl-manifest.yaml:2593-2650`
- `itsm-frontend/src/lib/services/msp-service.ts`、前端页面 `/msp`、`/msp/management`
