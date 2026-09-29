# 01 · 场景与总体架构：多客户单一服务商（MSP）

> **状态**：当前
> **更新日期**：2026-09-28
> **上位决策**：[ADR-004](../architecture/adr-004-multi-customer-tenant-model-selection.md)
> **目标架构（to-be）**：[目标架构方案](./plan/msp-target-architecture.md)｜**用户交互流程图**：[msp-user-interaction-flows.md](./plan/msp-user-interaction-flows.md)

> **定位（as-is）**：本文为**现状架构快照**（2026-09-28 实测），**不是目标口径**；目标以 [canon](./plan/msp-concept-model-and-architecture-canon.md) 为准，差异与整改见[一致性审计](./plan/msp-docs-consistency-audit.md) §3（C11）。

## 1. 场景定义

**角色与诉求**

| 角色 | 诉求 |
|---|---|
| 客户方用户 | 登录系统处理自己的业务：创建工单、维护知识库、使用服务目录/CMDB 等 |
| 客户方管理员 | 管理本客户租户内的用户、角色、菜单与配置 |
| 服务商管理员 | 开通并维护多个客户租户；管理服务商工程师与分配关系 |
| 服务商工程师 | 同时处理多个客户的工单/事件；按分配访问对应客户数据 |
| 平台运维 | 部署、初始化、升级、监控一套系统服务全部租户 |

**非目标**：单企业内部部署（用 `private` 模式）；面向公众的自助注册 SaaS（用 `saas` 模式）；跨服务商的租户联合。

## 2. 总体架构

```
┌─────────────────────────────────────────────────────────────────────┐
│ 客户端：客户用户（租户 A/B/C…）│ 服务商员工（provider 租户）          │
└───────────────┬───────────────────────────────┬─────────────────────┘
                │ JWT(tenant_id=客户租户)        │ JWT(tenant_id=provider)
                ▼                               ▼
┌─────────────────────────────────────────────────────────────────────┐
│ 一套 itsm-api / itsm-worker / Web（同镜像，多实例）                  │
│  ├─ TenantMiddleware：租户识别（fail-closed）                        │
│  ├─ MSPMiddleware + RequireMSPPermission：跨客户授权                  │
│  └─ 业务域 handlers：按 tenant_id 收窄                                │
└───────────────┬─────────────────────────────────────────────────────┘
                ▼
┌─────────────────────────────────────────────────────────────────────┐
│ 共享 PostgreSQL（共享 Schema + tenant_id 隔离；RLS 可灰度 enforce）  │
│  tenants / users / tickets / knowledge / cmdb / bpmn / …             │
│  （平台级表登记于 TenantExemptTables，见 §7）                        │
└─────────────────────────────────────────────────────────────────────┘
```

要点：**一套部署、一个数据库实例、共享 Schema**；隔离靠 `tenant_id` 维度与四层防线（§4），跨客户访问靠 MSP 授权（§5）。

## 3. 租户模型

租户是最高级数据归属与商业化单元（`ent/schema/tenant.go`）：

| 字段 | 说明 |
|---|---|
| `code` / `name` / `domain` | 租户代码（登录/接口可携带）、名称、独立域名（可空） |
| `type` | `internal` / `saas_customer` / `msp_provider` / `msp_customer`（保留 `standard`/`msp`/`customer` 兼容历史） |
| `status` / `expires_at` | `active` 等状态与到期时间；暂停/过期请求返回 403 |
| `parent_tenant_id` | 父租户；MSP 客户指向服务商租户 |
| `msp_provider_id` | 服务商租户 ID |
| `plan_code` / `billing_enabled` / `currency` / `service_tier` | 套餐与计费维度 |
| `timezone` / `owner_contact` / `legal_entity_code` / `cost_center_code` | 运营与合规字段 |

类型判定统一走 `pkg/tenantmode/tenantmode.go`：`IsMSPProviderTenantType`（`msp_provider` 或历史 `msp`）、`IsCustomerTenantType`（`msp_customer` / `saas_customer` / 历史 `customer`）、`IsInternalTenantType`。

## 4. 隔离机制（四层防线）

1. **请求层**：`middleware/tenant.go` 解析优先级 = JWT `claims.tenant_id` > `X-Tenant-Code` > 子域名 > 路径参数；解析不到 → 401（fail-closed，不回退默认租户）；JWT 与请求来源不一致 → 401；租户暂停/过期 → 403（`middleware/tenant.go:138-169`）。
2. **数据层**：Ent 查询显式 `TenantIDEQ` 过滤 + `TenantAwareRepository.GuardDelete/GuardUpdate` 防漏。
3. **治理层**：`internal/schema/tenant_guard.go` 启动扫描 `information_schema`；未登记豁免且缺 `tenant_id` 的表按 `fatal` 策略拒绝启动（`tenant_guard.go:117-127, 222-229`）。
4. **数据库层（灰度中）**：PostgreSQL RLS 以 `app.current_tenant` 为唯一维度，模式 `off`（默认）/`shadow`/`enforce`（`config/config.go:107-114`）。

**唯一合法的跨租户通道**：`tenantctx.WithSystemBypass`（迁移/seed/定时任务/运维，需评审注释）+ `itsm_admin`（BYPASSRLS）+ MSP 分配授权（§5）。

## 5. MSP 授权模型

- **身份标记**：`users.msp_role` 枚举 `provider_admin` / `provider_agent` / `customer_user`（`ent/schema/user.go:69-72`）；`admin` 角色**不会**自动获得 MSP 访问权。
- **身份判定**：租户类型为 `msp_provider` 且 `msp_role` 非空才认定 MSP 身份（`middleware/msp_middleware.go:19-27`）。
- **分配**：`MSPAllocation`（`msp_user_id` ↔ `customer_tenant_id`，角色 `primary/backup/specialist`，含 `deassigned_at`；`ent/schema/msp_allocation.go:19-51`）决定该员工可访问的客户集合（`AllowedCustomers`）。
- **跨客户请求**：请求头 `X-Customer-Tenant-ID` 必须命中分配列表，否则 403（`middleware/msp_middleware.go:23, 133`）。
- **联合检查**：`RequireMSPPermission` = MSP 身份 → RBAC 资源权限 → 客户分配（`middleware/msp_rbac.go:125-127`）。
- **租户切换**：`SwitchTenant` 换发带目标租户 `tenant_id` 的 JWT；允许自身租户、`super_admin`、或存在有效分配的服务商员工（`handlers/auth/service.go:114`）。

## 6. 跨客户数据流（服务商视角）

```
服务商员工请求（JWT: tenant_id = provider）
  → JWT 认证
  → MSPMiddleware：provider 租户 + msp_role + 有效 AllowedCustomers
  → X-Customer-Tenant-ID（或先 SwitchTenant 换 JWT）
  → RequireMSPPermission：RBAC + 分配校验
  → 业务查询按目标客户 tenant_id 收窄（MSP 视角接口）
```

## 7. 共享与隔离边界（摘要）

- **共享基础设施**：同一数据库实例、连接池、缓存、消息总线、对象存储、监控；AI 服务无租户态（按参数传 `tenantId`）。
- **平台级共享表**（`TenantExemptTables` 显式登记，`tenant_guard.go:65-115`）：`tenants`、`schema_migrations`、`user_roles`、`ai_llm_calls`、标签云关联表、`msp_allocations`、初始化台账、纯关联表、协作快照、`marketplace_items`、`messages`（待评估）、`prompt_templates`（待评估）、`password_reset_tokens`。
- **按租户隔离**：users/roles/permissions/menus/groups、工单/服务目录、ITIL 四域、CMDB、知识库、流程引擎、会话、附件、审计、AI 结果、`system_configs` 等。
- **CMDB 口径**：租户内全员可读（tenant-wide，ADR-003），**不跨租户**。

## 8. 设计约束与已知风险

| 项 | 说明 |
|---|---|
| 缓存租户维度 | `itsm-backend/cache/` 未发现租户处理；多客户场景必须保证 key 带租户（行动项 A8） |
| 共享表语义 | `messages` / `prompt_templates` 标注"待评估"；标签云/市场模板跨租户共享需业务确认 |
| RLS 默认 off | 当前主要依赖应用层；强合规场景按 02 文档灰度到 `enforce` |
| 部署门控 | `private` 模式整族 404；未知模式按 SaaS 默认开启 MSP（typo 不会关闭 MSP，见 02 §9） |
| 开通成本 | 每客户克隆一套 RBAC/SLA/审批组等（自动化，但需纳入交付流程） |

## 9. 证据索引

- `ent/schema/tenant.go`、`ent/schema/user.go:69-72`、`ent/schema/msp_allocation.go:19-51`
- `pkg/tenantmode/tenantmode.go`、`middleware/tenant.go:138-169`
- `middleware/msp_middleware.go:19-27, 133`、`middleware/msp_rbac.go:125-127`、`middleware/msp_gate.go:21-33`
- `internal/schema/tenant_guard.go:65-115, 117-127`、`config/config.go:107-114`
- `handlers/auth/service.go:114`、`docs/acl-manifest.yaml:2593-2650`
- 关联文档：[ADR-004](../architecture/adr-004-multi-customer-tenant-model-selection.md)、`docs/articles/05-multi-tenant-msp-operations.md`
