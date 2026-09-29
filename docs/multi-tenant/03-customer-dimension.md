# 03 · 客户维度：客户租户生命周期与配置

> **状态**：当前
> **更新日期**：2026-09-28
> **适用**：服务商管理员、客户管理员；前置阅读 [01-architecture.md](./01-architecture.md)

> **定位（as-is）**：本文为**现状/运营快照**（2026-09-28 实测），**不是目标口径**；目标以 [canon](./plan/msp-concept-model-and-architecture-canon.md) 为准，差异与整改见[一致性审计](./plan/msp-docs-consistency-audit.md) §3（C11）。

## 1. 客户 = 独立租户

每个客户开通一个 `msp_customer` 租户；客户用户、角色、菜单、工单、知识库、CMDB、流程等全部落在该租户内。客户之间不可互见；服务商通过分配（见 04 文档）跨客户访问。

**生命周期**：

```
创建租户记录 → 配置类型/关联/套餐 → 模板开通（provisioning） → readiness 校验
→ active 运行 → （可选）暂停/过期 → 退租（软删除+审计）
```

## 2. 创建客户租户

**接口**：`POST /api/v1/tenants`（权限 `tenant.write`，`docs/acl-manifest.yaml:1735-1737`）

**关键字段**（`itsm-backend/dto/tenant_dto.go:5-24`）：

| 字段 | 必填 | 说明 |
|---|---|---|
| `name` / `code` | ✅ | 租户名称/代码（code 用于登录与接口识别） |
| `type` | ✅ | 客户填 `msp_customer`（枚举含 `standard/internal/saas_customer/msp_provider/msp_customer` 及历史值） |
| `status` | — | `active` / `suspended` / `expired` / `deleted`，默认 `active` |
| `parentTenantId` | 建议 | 父租户 = 服务商租户 ID |
| `mspProviderId` | 建议 | MSP 提供方租户 ID |
| `domain` | — | 独立域名（用于按子域名解析租户） |
| `expiresAt` | — | 到期时间；过期后请求 403 |
| `planCode` / `billingEnabled` / `currency` / `serviceTier` | — | 套餐与计费维度 |
| `costCenterCode` / `legalEntityCode` / `ownerContact` | — | 成本中心/法人实体/负责人 |
| `settings` / `quota` | — | 租户配置与资源配额 |

> 实测（2026-09-28）：`tenants` 表已含 `parent_tenant_id` / `msp_provider_id` 列，创建客户租户时经 `POST /api/v1/tenants` 传入即落库（MSPCUSTA / MSPCUSTB → provider `3`），无需额外迁移。

更新：`PUT /api/v1/tenants/:id`；状态变更：`PUT /api/v1/tenants/:id/status`（`docs/acl-manifest.yaml:1747-1761`）。

## 3. 模板开通（provisioning）

租户记录创建后执行模板克隆（角色/权限/角色权限/菜单/审批组/SLA/CI 类型/审批工作流/流程定义部署绑定）：

```bash
go run ./cmd/provision_tenant -tenant-id <客户租户ID> \
  [-template-version <版本>]   # 默认 seeder.CurrentTenantTemplateVersion
```

（`itsm-backend/cmd/provision_tenant/main.go:17-58`；幂等，可重复执行且保留客户自定义。）

**readiness 校验**（任一为 0 即失败，`pkg/seeder/tenant_provisioner.go:345-373`）：roles、permissions、role permissions、menus、groups、SLA definitions、CI types。

## 4. 客户用户与 RBAC

- `users.tenant_id` 为必填（Positive，`ent/schema/user.go:59-61`）；
- 每个租户有独立的一套角色/权限/菜单/组（开通时克隆，客户可自行调整）；
- 客户用户 `msp_role` 可标记为 `customer_user`（`ent/schema/user.go:69-72`）；该标记不赋予任何跨租户能力；
- 客户管理员在客户租户内管理用户与角色，不需要（也无法）访问其他租户。

## 5. 登录与租户解析

- 登录/注册请求可携带 `tenantCode`（`dto/auth_dto.go:13`），也可依赖独立域名；
- 请求租户解析优先级：JWT `claims.tenant_id` > `X-Tenant-Code` > 子域名 > 路径参数；
- 解析不到租户 → 401（fail-closed）；JWT 与请求来源不一致 → 401；租户 `suspended`/`expired` → 403（`middleware/tenant.go:138-169`）。

## 6. 客户侧可见范围

- 业务数据默认仅本租户可见（四层隔离，见 01 文档 §4）；
- **CMDB 为租户内 tenant-wide**：本客户租户内所有角色可读全量 CI/拓扑（ADR-003），不跨租户；
- 平台级共享数据（标签云、市场模板、AI prompt 模板等）跨租户共享，属既有设计（`internal/schema/tenant_guard.go:65-115`），签约前需确认可接受。

## 7. 客户档案（ServiceCustomer，可选）

面向邮件报障/合同运营的租户内客户档案，与"租户"是两回事：

- 实体：`ServiceCustomer` / `CustomerBranch` / `SupportContract` / `SourceOrganization`，均带 `tenant_id`（`ent/schema/service_customer.go:12-42`）；
- 用途：邮件 intake 的客户/分支/合同匹配与值班（`handlers/email_intake/service.go:136-158, 400-421`），菜单"邮件报障 → 客户资料/支持合同/来源组织/值班排班"（`pkg/seeder/seeder.go:1739-1742`）；
- `linked_customer_tenant_id` 可关联到真正的客户租户（`ent/schema/service_customer.go:24`）；
- **不得**用它替代租户隔离。

## 8. 暂停、过期与退租

| 动作 | 方式 | 效果 |
|---|---|---|
| 暂停 | `PUT /api/v1/tenants/:id/status` = `suspended` | 该租户请求 403 |
| 到期 | 设置 `expiresAt` | 到期后请求 403 |
| 退租 | 软删除 + 审计（见 `docs/articles/05-multi-tenant-msp-operations.md` §7.5） | 数据保留可审计；按租户切割导出/备份 |

退租检查清单：结清未结工单 → 解除该客户的 `MSPAllocation` → 导出/归档数据 → 状态置 `deleted`。

## 9. 客户维度验证清单

1. 客户租户 `type=msp_customer` 且 `parentTenantId`/`mspProviderId` 指向服务商；
2. `provision_tenant` 执行成功且 7 项 readiness 全部非 0；
3. 客户用户可登录（携带 `tenantCode` 或经域名）；
4. 客户 A 的用户无法看到客户 B 的任何数据（401/403）；
5. 暂停/过期后请求返回 403。

## 10. 证据索引

- `dto/tenant_dto.go:5-53`、`docs/acl-manifest.yaml:1729-1761`
- `cmd/provision_tenant/main.go:17-58`、`pkg/seeder/tenant_provisioner.go:341-374`
- `ent/schema/tenant.go`、`ent/schema/user.go:59-72`、`ent/schema/service_customer.go:12-42`
- `middleware/tenant.go:138-169`、`dto/auth_dto.go:13`
- `handlers/email_intake/service.go:136-158, 400-421`、`pkg/seeder/seeder.go:1739-1742`
