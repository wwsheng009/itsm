# ADR-004：多客户管理场景租户模型选型——采用多租户（MSP 模式）

## 状态

**Accepted（2026-09-30）**——固化"多个客户需登录操作、服务方需同时维护多个客户"场景的选型结论与建议行动项；2026-09-30 按最佳实践完成评审确认（D1–D11 / E1–E6 定稿，见 [canon v1.0](../multi-tenant/plan/msp-concept-model-and-architecture-canon.md) §10），行动项执行跟踪见 [实施方案](../multi-tenant/plan/msp-implementation-plan.md)（`IP-P0-*` / `IP-P1-*` / `IP-P2-*`）。

## 背景

业务需求同时包含两类角色，要求在同一套系统内闭环：

- **客户方**：多个客户需要登录系统进行业务操作（创建工单、维护知识库、使用服务目录/CMDB 等），客户之间数据不可互见；
- **服务方**：管理员/工程师需要同时对接并维护多个客户，不为每个客户维护独立部署与独立账号体系。

需要决策的问题：**为每个客户开独立租户（多租户），还是所有客户放在同一租户内、用业务字段（如 `customer_id` / 客户档案）做客户隔离？**

本仓库租户模型已预留两类租户：`msp_provider`（服务方）与 `msp_customer`（客户），且 `parent_tenant_id` 注释为"MSP 客户指向 MSP 提供商"（`ent/schema/tenant.go:29-41`）。

## 决策

**采用多租户，落地为系统内置的 MSP 模式；不采用"同租户内客户隔离"方案。**

1. **客户 = 独立租户**：每个客户开通一个 `msp_customer` 租户；客户用户建在自己租户内，正常登录并操作工单/知识库等业务，租户间由既有隔离机制保证不可见。
2. **服务方 = 一个 `msp_provider` 租户**：服务方管理员/工程师账号集中在服务方租户；通过 `MSPAllocation`（primary/backup/specialist）授权到多个客户租户，经 `X-Customer-Tenant-ID` 或切换租户操作客户数据。
3. **部署模式 = `saas_msp`**：`DEPLOYMENT_MODE=saas_msp`；`private` 模式下 `/api/v1/msp/*` 整族 404。
4. **租户内客户目录（ServiceCustomer）仅作运营档案**：用于邮件报障/合同/分支/别名匹配等运营场景，不作为隔离边界；需要时用 `linked_customer_tenant_id` 关联真正的客户租户。
5. **禁止**在共享租户内新增 `customer_id` 维度承载客户隔离（原因见"被否方案"）。

## 理由（代码实证）

### 1. 现有隔离以 tenant_id 为唯一边界，无法覆盖"客户"维度

- 运行时：`TenantMiddleware` 解析租户并 fail-closed——解析不到租户返回 401（`middleware/tenant.go:138-141`），租户不匹配 401（:153-156），租户暂停/过期 403（:160-169）；
- 数据层：Ent 显式 `TenantIDEQ` 过滤 + `TenantAwareRepository.GuardDelete/GuardUpdate`；
- 治理层：`internal/schema/tenant_guard.go` 在生产启动时扫描 `information_schema`，未登记且缺 `tenant_id` 的表按 `fatal` 策略拒绝启动（`tenant_guard.go:117-127, 222-239`）；
- 数据库层：RLS 以 `app.current_tenant` 为唯一维度，模式 `off`（默认）/`shadow`/`enforce`（`config/config.go:107-114`）。

同租户内客户 A/B 共用这些表，上述防线全部不覆盖新维度；要让 `customer_id` 达到同等强度，等于重建一整套过滤/门禁/RLS。

### 2. 登录身份与 RBAC 按租户克隆

- `users.tenant_id` 为必填（Positive，`ent/schema/user.go:59-61`）；租户开通从 `default` 模板克隆角色/权限/菜单/审批组/SLA/CI 类型/审批工作流/流程绑定（`pkg/seeder/tenant_provisioner.go`，入口 `cmd/provision_tenant/main.go:17-58`）；
- 同租户内客户 A/B 的用户会落在同一张 `users` 表、同一套角色/菜单/组体系，无法天然隔离。

### 3. 既有设计假设会被破坏

- CMDB 为 tenant-wide（ADR-003）：同租户内所有客户会互相看到 CI/拓扑；
- 计费、配额、到期时间、时区、域名、service_tier 均为租户级字段（`ent/schema/tenant.go`），同租户内无法按客户区分。

### 4. 退租与合规切割

客户数据混存时，"删除/导出某个客户全部数据"难以做干净；租户级天然支持按租户软删除+审计与备份/导出切割。

### 5. MSP 链路已完整落地，开箱即用

- 身份判定：租户类型为 `msp_provider` 且 `msp_role` 非空才认定 MSP 身份（`middleware/msp_middleware.go:19-27`）；
- 分配模型：`MSPAllocation`（`msp_user_id` / `customer_tenant_id` / `role` / `deassigned_at`，`ent/schema/msp_allocation.go:19-35`）；
- 跨租户校验：`X-Customer-Tenant-ID` 必须命中分配列表（`middleware/msp_middleware.go:23, 133`）；
- 管理面：`/api/v1/msp/customers`、`/api/v1/msp/customers/:customer_tenant_id/tickets`、`/api/v1/msp/reports/customers`（`docs/acl-manifest.yaml:2623-2643`）；
- 租户切换：`SwitchTenant` 换发带目标租户 `tenant_id` 的 JWT（`handlers/auth/service.go:114`）；
- 部署门控：`private` 关闭 MSP，`saas`/`saas_msp`/空值开启（`middleware/msp_gate.go:21-33`）。

## 被否方案：同租户内客户隔离

| 维度 | 问题 |
|---|---|
| 隔离强度 | `customer_id` 维度不在 TenantMiddleware / Ent 过滤 / tenant_guard / RLS 任何一层内，需要重建整套防线 |
| 身份与 RBAC | `users` 与角色/菜单/组是租户级克隆，同租户内客户 A/B 用户混布，RBAC 无法自然区分 |
| 既有设计假设 | CMDB tenant-wide（ADR-003）、计费/时区/域名/到期均为租户级字段，无法按客户区分 |
| 合规与退租 | 数据混存导致按客户删除/导出/审计切割困难 |
| 安全风险 | 新增维度处于 tenant_guard 与 RLS 校验之外，漏加过滤即跨客户越权且无兜底（fail-open） |

## 补充：租户内客户目录（ServiceCustomer）的正确定位

- 实体：`ServiceCustomer`（注释原文 "a tenant-owned customer served by a NOC/MSP"，`ent/schema/service_customer.go:12`）、`CustomerBranch`、`SupportContract`、`SourceOrganization`；均带 `tenant_id`，按 `(tenant_id, normalized_name)` 唯一；
- 用途：邮件报障 intake 的客户/分支/合同匹配与值班（`handlers/email_intake/service.go:136-158`；CRUD 见 :400-421），对应菜单"邮件报障 → 客户资料/支持合同/来源组织/值班排班"（`pkg/seeder/seeder.go:1739-1742`）；
- 与租户的关系：`linked_customer_tenant_id` 预留字段可关联客户租户（`ent/schema/service_customer.go:24`）；
- 结论：可作为"客户档案"运营，但**不能作为"客户隔离"边界**。

## 决策规则

| 客户形态 | 推荐 |
|---|---|
| 客户要登录、独立做业务（本场景） | `msp_customer` 租户 + MSP 分配 |
| 客户只是"被服务对象"（邮件来源/合同主体），不登录 | 不建租户，用租户内 ServiceCustomer 档案 |
| 客户是同一集团/法人内部部门 | department/group 组织，不按客户处理 |
| 客户要求独立域名/独享数据库/合规隔离 | 仍走租户级能力（`domain` 字段；Schema 级隔离为企业版选项） |

## 建议行动项

| # | 行动项 | 落地方式 / 验证 |
|---|---|---|
| A1 | 部署模式切换为 MSP | `DEPLOYMENT_MODE=saas_msp`（`itsm-backend/config.yaml:25-28`；`middleware/msp_gate.go:21-33`）；验证 `/api/v1/msp/*` 不再 404 |
| A2 | 创建服务方租户 | `POST /api/v1/tenants`（`tenant.write`，`docs/acl-manifest.yaml:1735-1737`），类型 `msp_provider`；员工账号落在该租户且 `msp_role` 非空 |
| A3 | 开通客户租户 | 先建 `msp_customer` 租户（`parent_tenant_id`/`msp_provider_id` 指向服务方租户），再执行 `go run ./cmd/provision_tenant -tenant-id <ID>`（`cmd/provision_tenant/main.go:17-58`）；以 readiness 校验通过为准 |
| A4 | 分配服务方人员 | `POST /api/v1/msp/allocations`（`msp_allocation.write`，`docs/acl-manifest.yaml:2611-2616`）；调整/离职用 `POST /api/v1/msp/allocations/deallocate`（:2617-2620）或写 `deassigned_at`（`ent/schema/msp_allocation.go:29-31`） |
| A5 | 授予 MSP 权限 | 服务方角色授予 `msp_customer.read` / `msp_ticket.read` / `msp_report.read`（`docs/acl-manifest.yaml:2623-2643`） |
| A6 | 创建客户用户 | 在各自客户租户内建用户并授予客户侧角色（模板已含角色/菜单），无需跨租户机制 |
| A7 | （可选）客户档案 | 邮件报障/合同运营需要时维护 ServiceCustomer/CustomerBranch/SupportContract，并用 `linked_customer_tenant_id` 关联租户 |
| A8 | 缓存租户维度核查 | `itsm-backend/cache/` 当前无 tenant 相关代码；必须确认缓存 key 带租户维度，防止跨客户串数据 |
| A9 | 共享表影响评估 | 确认跨租户共享表（标签云、`marketplace_items`、`messages`、`prompt_templates`，`internal/schema/tenant_guard.go:65-115`）在 MSP 多客户运营下可接受；`messages`/`prompt_templates` 的"待评估"需收口 |
| A10 | 隔离回归验证 | ① 无租户上下文 → 401；② `X-Customer-Tenant-ID` 未分配 → 403；③ `private` 模式 MSP 路由 → 404；④ 客户 A token 访问客户 B 数据 → 401/403；⑤ 切换租户后 JWT 的 `tenant_id` 正确 |
| A11 | （合规场景）RLS 灰度 | `off → shadow → enforce`（`config/config.go:107-114`）；enforce 前先补齐上下文缺失点 |

> **行动项执行跟踪（2026-09-30 确认）**：A1→`IP-P0-1`；A2/A3→`IP-P0-5`（bootstrap 首管为 `IP-P1-5`）；A4→`IP-P0-2`（allocation 唯一索引 + 归属校验）；A5→`IP-P0-9`（五角色词表与权限行）；A6→`IP-P0-5`；A7→运营档案（无独立工作流，随 `IP-P1-5` 复核）；A8→`IP-P0-2`（缓存租户维度审查，`07:G8`）；A9→`IP-P2-3`；A10→`IP-P0-1/2/6`（隔离回归）；A11→`IP-P2-2`。缺口映射见实施方案 §3.0-F。

## 风险与注意事项

- **开通成本**：每客户克隆一套 RBAC/SLA/审批组等，已自动化（provisioner + readiness），但仍需纳入交付流程；
- **共享表语义**：标签云/市场模板等跨租户共享是既有设计（`TenantExemptTables` 显式登记），MSP 多客户运营下需业务确认；
- **外部路径租户标识**：AI 服务按参数接收 `tenantId`；缓存层待补租户维度（见 A8）；
- **权限回收**：`MSPAllocation.deassigned_at` 是服务方人员离场/换岗的唯一回收入口，需纳入流程；
- 本 ADR 状态：**Accepted（2026-09-30）**；行动项状态由实施方案（§3.0-F 缺口映射 / §6 DoD）与 [07-known-gaps](../multi-tenant/07-known-gaps.md) 持续跟踪。

## 与既有 ADR / 文档的关系

- ADR-001 模块化单体：本决策不改变部署形态；
- ADR-002 事件驱动：跨租户事件广播机制不因 MSP 改变；
- ADR-003 CMDB tenant-wide：本决策下 CMDB 仍是"租户内共享、租户间隔离"；
- `docs/articles/05-multi-tenant-msp-operations.md`：MSP 模式技术/运营全解析（§6.3 案例、§7.3 部署门控）。

## 落地证据（代码索引）

- `ent/schema/tenant.go:29-41`、`ent/schema/msp_allocation.go:19-51`、`ent/schema/service_customer.go:12-42`、`ent/schema/user.go:59-61`
- `middleware/tenant.go:138-169`、`middleware/msp_middleware.go:19-27`、`middleware/msp_gate.go:21-33`
- `handlers/msp/handler.go:100-308`、`handlers/auth/service.go:114`、`handlers/email_intake/service.go:136-158, 400-421`
- `internal/schema/tenant_guard.go:65-115, 117-127`、`config/config.go:107-114`、`itsm-backend/config.yaml:25-28`
- `cmd/provision_tenant/main.go:17-58`、`pkg/seeder/seeder.go:1739-1742`
- `docs/acl-manifest.yaml:1729-1761, 2604-2643`
- `docs/architecture/adr-003-cmdb-tenant-wide-default.md`、`docs/articles/05-multi-tenant-msp-operations.md`
