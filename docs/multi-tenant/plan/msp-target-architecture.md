# MSP 多租户目标架构方案

> 状态：**Draft v0.1（待评审）**｜日期：2026-09-29｜范围：目标架构（路线 A → 路线 B）、身份与作用域、租户上下文、隔离与权限、建号/邀请/首登、数据模型与迁移、前端架构、审计与演进
> 上位决策：[ADR-004 多客户管理场景租户模型选型](../../architecture/adr-004-multi-customer-tenant-model-selection.md)
> 关联文档：[现状架构 01](../01-architecture.md)｜[用户生命周期与租户切换方案](./msp-user-lifecycle-and-tenant-switching-plan.md)（下称"主方案"）｜[作用域模型分析](./msp-scope-model-analysis-and-ai-gateway-reference.md)（下称"分析文档"）｜[07 已知缺口](../07-known-gaps.md)（G1–G10）｜[通知模块设计方案](../../plan/notification-module-design-plan-2026-09-29.md)（平台邮件通道）
> 配套：[用户交互流程图](./msp-user-interaction-flows.md)
> 定位：`01-architecture.md` 描述**现状架构**（as-is）；本文给出**目标架构**（to-be）与 A→B 演进路径，作为实现评审与验收的架构依据。

---

## 0. 摘要（TL;DR）

**目标一句话**：**账号只有一个，作用域可以有多个；会话中只有一个"当前生效作用域"，且必须能被服务端按 membership 复核。**

- **客户方**：一个账号一个作用域（数据库级强约束），永不跨租户；
- **服务方**：一个账号 N 个作用域（provider 自身 + 每个有效 `MSPAllocation`），可显式选择/切换，全程审计；
- **平台**：全域作用域，治理模式（先选目标租户再操作，高危二次确认）；
- **隐私红线（2026-09-29 新增）**：登录页/未认证面**不得出现租户选择器或任何租户信息**（泄露客户关系）；多客户信息只在认证后展示——服务方登录落 **provider 家**、应用内顶栏切换（见[登录与切换细化方案](./msp-login-and-switching-refinement-plan.md)）；
- **落地路线**：P0 走路线 A（最小闭环：统一 `AccessibleTenants` 抽象 + 修复 F1–F13），P1 首批转正路线 B（membership 表 + 上下文扩展 + 4 角色模板）；
- **不变量**：不引入 `customer_id` 业务维度；不迁移 `users.email/username` 唯一约束；跨租户写入必须显式授权 + 可审计；"当前租户"判定收敛到**单一函数**。

---

## 1. 文档定位与设计原则

### 1.1 读者与用途

| 读者 | 用途 |
|---|---|
| 平台架构师 / 研发 | 评审目标模型、接口契约与迁移边界；实现与测试的架构依据 |
| 服务商管理员 / 运维 | 理解作用域、分配、切换与审计的语义边界 |
| 安全 / 合规 | 隔离边界、越权反例、审计与 fail-closed 策略 |
| 产品 / 交付 | A→B 演进范围与验收口径 |

### 1.2 设计原则（不可变）

| # | 原则 | 说明 |
|---|---|---|
| P1 | **账号唯一、作用域多元** | `Account` 只有身份；`Membership/Scope` 承载"在某租户内是谁、什么角色" |
| P2 | **客户方单作用域（DB 级）** | `account_kind=customer` 的作用域上限 = 1，用部分唯一索引强约束，不靠应用自觉 |
| P3 | **会话声明 + 服务端复核** | JWT 的 `tenant_id` 是**声明**而非授权事实；每次请求按 membership/分配复核 |
| P4 | **fail-closed** | 解析失败、歧义、越权一律拒绝（401/403/409），禁止静默回退默认租户、禁止自动选择 |
| P5 | **单一真相源** | "可访问租户集合 / 当前租户"只允许一个实现（`service/tenant_access.go`），handler 不得各自推导 |
| P6 | **显式跨租户 + 可审计** | 跨租户写入必须携带 actor/reason 的显式 bypass，且写审计；不得扩大 `IsSystemBypass` 使用面 |
| P7 | **渐进演进** | A→B 分阶段，每阶段可独立回滚；不破坏既有唯一约束与历史数据 |
| P8 | **平台治理优先** | 事件目录/角色模板/必达清单等平台级配置由平台治理，租户只读或有限覆盖 |

---

## 2. 目标架构总览

```text
┌──────────────────────────────────────────────────────────────────────────────┐
│ 客户端                                                                        │
│  客户用户（单作用域，无切换器）│ 服务方员工（多作用域 + 切换器）│ 平台管理员（治理）│
└───────────────┬──────────────────────────────┬───────────────────────────────┘
                │ JWT{tenant_id, tenant_source, membership_id}
                ▼
┌──────────────────────────────────────────────────────────────────────────────┐
│ 接入层                                                                        │
│  AuthN → TenantContext 解析（§4，单一函数）→ 租户状态校验（active/expires）      │
│  MSPMiddleware（服务方）→ RequireMSPPermission（RBAC × membership × 分配）      │
└───────────────┬──────────────────────────────────────────────────────────────┘
                ▼
┌──────────────────────────────────────────────────────────────────────────────┐
│ 应用层（领域服务）                                                             │
│  TenantAccess（可访问集合/复核）│ Provisioning（建号通道收口）│ Invitation（邀请）│
│  业务域 handlers：按"当前生效租户"收窄（工单/ITIL/CMDB/知识/SLA/…）              │
└───────────────┬──────────────────────────────────────────────────────────────┘
                ▼
┌──────────────────────────────────────────────────────────────────────────────┐
│ 数据层（共享 PostgreSQL，共享 Schema）                                          │
│  租户维度表（tenant_id）│ memberships（新增）│ invitations（新增）│ audit_logs     │
│  平台级表：TenantExemptTables 显式登记（§6.3）；RLS 灰度 off/shadow/enforce     │
└──────────────────────────────────────────────────────────────────────────────┘
```

**与现状（`01-architecture.md`）的差异**：① 身份模型从 `users.tenant_id` 1:1 演进为 membership 多作用域；② 租户解析从"静默降级"改为 fail-closed 优先级；③ 跨租户通道从"两条并存"统一为 membership/分配语义；④ 新增建号通道收口与邀请生命周期；⑤ 前端新增作用域选择器与上下文指示。

---

## 3. 身份与作用域模型

### 3.1 账号类型（`account_kind`）与作用域上限

| `account_kind` | 判定来源 | 作用域上限 | 跨租户 |
|---|---|---|---|
| `customer` | 创建于 `msp_customer` 租户且无 provider 身份 | **1（强约束）** | ❌ 永不跨租户 |
| `provider` | 创建于 `msp_provider` 租户且 `msp_role` 非空 | N = 1（provider 自身）+ 每个有效分配 1 个客户作用域 | ✅ 仅限被授权集合 |
| `platform` | `super_admin` / `sysadmin`（平台租户） | 全域 | ✅ 治理模式 + 审计 |

> 边界：同一自然人既服务方又客户方 → **使用不同账号**（禁止双重身份，避免审计与权限歧义）。

### 3.2 Membership（`user_tenant_memberships`）

| 字段 | 说明 |
|---|---|
| `user_id` / `tenant_id` | 主体 ↔ 作用域（FK users / tenants） |
| `account_kind` | 冗余快照（默认 `customer`），由服务层与 `users.account_kind` 同步 + 定期巡检 |
| `subject_type` | `user`（第一期仅此值；`service_account` P2 扩展） |
| `source` | `home / allocation / platform / invite / migration`（来源可追溯） |
| `role_id` / `msp_role` | **作用域内角色**（客户方：租户模板角色；服务方客户作用域：`msp_observer/msp_tech/msp_manager/msp_full`） |
| `allocation_id` | 服务方作用域来源分配（可追溯） |
| `status` | **`active / suspended`**（邀请态由邀请流承载，不占用 status；移除=软删） |
| `is_default` | 主作用域标记（客户方恒 true；服务方 provider 作用域 true） |
| `expires_at` | 作用域到期（P1-3 落地） |
| `invited_by` / `joined_at` | 邀请人与加入时间（`source=invite` 时） |
| `deassigned_at` / `deleted_at` | 分配撤销时间 / 软删（保留历史，回收即失效） |

**关键约束（3 个部分唯一索引）**：

1. `uq_membership_live (user_id, tenant_id) WHERE deleted_at IS NULL`；
2. `uq_membership_default (user_id) WHERE deleted_at IS NULL AND is_default AND status='active'`（唯一默认作用域）；
3. **`uq_customer_single_scope (user_id) WHERE deleted_at IS NULL AND status='active' AND account_kind='customer'`**（客户单作用域，DB 级强约束）；

**同步规则**：服务方作用域 = `MSPAllocation` 的**物化**（分配变更与 membership 同事务同步；`deassigned_at` 置位 → membership 软删）；角色挂 membership，平台角色（`user_roles`）保持平台级。

### 3.3 作用域与既有模型的关系

| 既有 | 目标 | 迁移口径 |
|---|---|---|
| `users.tenant_id`（1:1） | home membership | 回填为 `is_home=true` 的 membership；`users.tenant_id` 保留为兼容字段（读路径逐步切换） |
| `msp_allocations` | 服务方客户作用域来源 | 有效分配 → `active` membership；`deassigned_at` → `removed`（保留历史） |
| `users.role` / `user_roles` | 平台级角色保留 | 租户内角色以 membership.role 为准，双读期以 membership 优先 |

---

## 4. 租户上下文与解析

### 4.1 TenantContext（扩展）

| 字段 | 说明 |
|---|---|
| `TenantID` | 本次请求最终生效租户 |
| `ActorID` / `ActorKind` | 主体与类型（`user` / `service_account` / `system`） |
| `MembershipID` | 命中的 membership（审计与复核） |
| `Source` | `membership` / `switch` / `header` / `platform_selected` / `job`（**无 default 兜底**） |
| `RequestTenantID` | 请求显式传入的租户（仅用于一致性校验与非法尝试审计） |
| `IsPlatformAdmin` | 是否具备治理能力（**不替代 RBAC**） |

### 4.2 解析优先级（fail-closed）

1. 后台作业显式任务参数（job schema 声明）；
2. 平台管理员显式选择（治理通道，审计 + 高危二次确认）；
3. 会话声明（JWT `tenant_id`）+ **membership 复核**；
4. 服务方显式切换结果（`switch-tenant` 换发 JWT）；
5. 服务方头通道 `X-Customer-Tenant-ID`（必须命中有效分配）；
6. ~~域名/Header 自动规则~~（**不开放**，避免静默切租户）；
7. ~~`default_compat` 默认租户兜底~~（**不引入**；历史数据仅在迁移脚本中显式标记）。

**禁止**：非平台身份用 query/body/header 指定目标租户；客户方多作用域自动选择；租户 `suspended/expired` 时的写操作；解析失败回退全量数据。

**登录落地（2026-09-29 修订）**：`customer` → 唯一租户；`provider` → **provider 家**（不因 `last_active` 在登录时进入客户作用域）；`platform` → 控制台。**登录页不展示任何租户信息**；服务方的作用域切换只发生在认证后的顶栏（隐私红线，见[登录与切换细化方案](./msp-login-and-switching-refinement-plan.md)）。

### 4.3 失败策略（fail-closed 清单）

| 场景 | 行为 |
|---|---|
| 客户方命中多个 active membership（数据异常） | 拒绝（`CUSTOMER_SCOPE_CONFLICT`）+ 告警，不自动选 |
| 服务方登录命中多个作用域 | **不弹选择器**（隐私）：登录落 provider 家，认证后由顶栏切换器选择（细化方案 §4） |
| 会话租户与 membership 不一致 | 拒绝（不信任旧 JWT、不静默切换） |
| 请求参数租户与会话冲突 | 拒绝（`TENANT_SELECTION_CONFLICT`） |
| 缺会话租户 / 解析不到 | 401 |
| 租户暂停/过期 | 403（读按权限策略，写一律拒绝） |

### 4.4 JWT 与会话

- JWT 三件套：`tenant_id` + `tenant_source` + `membership_id`；
- **refresh 按 claim 租户重签**（修 F10 静默回退），且重签前复核 membership；
- 切换/回收/停用 → 旧令牌撤销 + 审计（修 F12）。

---

## 5. 隔离与权限

### 5.1 隔离防线（在现状四层上演进）

| 层 | 现状（`01-architecture.md`） | 目标演进 |
|---|---|---|
| 请求层 | JWT `claims.tenant_id` > `X-Tenant-Code` > 子域名 > 路径参数；解析不到 401 | 增加 **membership 复核** 与 `tenant_source` 标记；客户方禁止参数指定租户（仅一致性校验）；解析顺序见 §4.2 |
| 数据层 | Ent 显式 `TenantIDEQ` + `GuardDelete/GuardUpdate` | 不变；`user_tenant_memberships` 带 `tenant_id`，纳入同口径 |
| 治理层 | `tenant_guard` 启动扫描，未豁免缺 `tenant_id` 表 fatal 拒启 | 不变；新表全部带 `tenant_id`（**不申请豁免**） |
| 数据库层 | RLS `off/shadow/enforce` 灰度，`app.current_tenant` 单维度 | 不变；membership 表可被 RLS 覆盖（自带租户列） |

**唯一合法的跨租户通道**（保持收敛）：`tenantctx.WithSystemBypass`（seeder/provisioner）、`itsm_admin`（BYPASSRLS）、以及 §6 的显式建号通道（携带 actor+reason 的 `WithProvisioningBypass`）。

### 5.2 权限判定式

```text
Allow = AuthN
      ∧ RBAC(permission, 按作用域内角色解析)
      ∧ Membership(active ∧ tenant_id = 当前作用域 ∧ 未过期)
      ∧ TenantStatus(active；写操作额外校验)
      ∧ ResourceOwner(如适用)
```

- RBAC 与 membership 是**与**关系，不是或关系；平台管理员也必须过 RBAC（`IsPlatformAdmin` 只代表 scope 能力）；
- 角色/授权变更需**重登或重切**后生效（避免会话内权限漂移）。

### 5.3 服务方在客户作用域内的有效权限（Q7）

| 角色模板 | 默认权限集 | 适用合同 |
|---|---|---|
| `msp_observer` | `ticket:read` + 评论 | 只读协办 |
| `msp_tech` | `ticket:read/write`、`knowledge:read`、`cmdb:read`、`service_catalog:read` | 只代工单（**默认**） |
| `msp_manager` | `msp_tech` + `user:write`（限客户侧角色）+ `report:read` | 代工单 + 开号 |
| `msp_full` | `msp_manager` + `cmdb:write`、`change:write`（**默认不分配**） | 全托管（客户显式确认） |

- 授予方 = **客户 admin 为主**（调整本租户 `msp_*` 角色权限集），平台应急兜底；变更审计；
- **服务方不可自我扩权**（含 `msp_manager`）；权限上限 = 该租户内角色权限；
- 分配角色映射：`primary → msp_manager`、`backup → msp_tech`、`specialist → msp_specialist`；
- 客户方在自有租户内拥有**大部分业务级功能**（工单/ITIL/知识/CMDB/服务目录/用户管理/报表），仅平台治理能力除外。

### 5.4 必须拒绝的请求（安全反例）

1. 客户 A 账号访问客户 B 数据 → 401/403；
2. 客户账号被加入第二个租户 → 拒绝（`uq_customer_single_scope` 兜底 + `CUSTOMER_SCOPE_CONFLICT`）；
3. 服务方访问未分配客户（头通道/切换通道同口径）→ 403；
4. 同一人同时持有服务方与客户方本地双重身份 → 拒绝（不同账号）；
5. 非平台身份用 query/body/header 指定目标租户 → 拒绝；
6. 服务方在客户作用域执行 `msp_full` 未授予的写操作（CMDB 写/变更审批/系统配置/角色管理/审计导出）→ 拒绝。

---

## 6. 建号 / 邀请 / 首登

### 6.1 建号通道矩阵

| 通道 | 调用方 | 目标租户 | 授权校验 | 写路径 |
|---|---|---|---|---|
| `platform` | `super_admin` / `sysadmin` | 任意 `active` 租户 | `user:write` + 平台角色 | `WithProvisioningBypass(ctx, actor, "platform")` |
| `msp` | `provider_admin`（有效角色 `msp_manager`） | **仅**其有效 allocation 的客户租户 | `msp_customer:write` + allocation | 同上，`reason="msp:<providerTenantID>"` |
| `tenant` | 租户内 `admin`（含客户管理员） | 本租户 | `roleRank` + `CanGrantRoles` | 不需要 bypass |
| `invite` | 持邀请 token 者 | 邀请指定租户 | token 一次性、未过期 | 一次性、限定角色 |

**服务层收口**（关键设计）：`service/provisioning.go` 的 `ProvisionUser(actor, target, input)` 内部完成 ① `CanAccessTenant` 复核 → ② 通道授权与角色白名单 → ③ 构造 bypass（必须带 actor+reason）→ ④ `WithTenantID(target)` 保证 RLS 按目标租户。**handler 不得自行拼装 bypass。**

**错误码**：`CROSS_TENANT_FORBIDDEN` / `MSP_ALLOCATION_REQUIRED` / `ROLE_NOT_GRANTABLE` / `MSP_ROLE_NOT_ALLOWED` / `USERNAME_EXISTS` / `EMAIL_EXISTS` / `TENANT_NOT_FOUND` / `TENANT_SUSPENDED`。
**灰度**：`USER_PROVISIONING_CHANNELS_ENABLED` 默认关 → 按租户灰度开启。

### 6.2 邀请生命周期（P1-1）

```text
创建邀请（actor 有权限）
  → 生成一次性 token（租户 + 角色 + 有效期 + invited_by）
  → 投递：平台 SMTP 发信（emailSent=true）或 API 返回 inviteUrl（emailSent=false，线下传递）
  → 被邀请人打开落地页 → 校验 token（一次性/未过期/未撤销）
  → 设置密码 → 创建账号 + home membership（source=invite）
  → 首登审计（user.invite_accept）→ 强制改密标记（如适用）
  → 撤销：邀请人在有效期内可撤销（写审计）
```

- **交付口径（Q4/D1）**：平台 SMTP 未配置时 API 固定返回 `inviteUrl` + `emailSent=false`，**不阻塞邀请流**；配置后自动发信（同一契约，见[通知模块设计方案](../../plan/notification-module-design-plan-2026-09-29.md) §4.5/§7 P0-1）；
- **角色白名单（修 F3）**：邀请与注册只允许目标租户内可授予角色；显式拒绝 `super_admin/sysadmin/admin`；`msp_role` 仅 `platform`/`msp` 通道可设置；
- **待定**：token TTL、`invitations` 表 DDL、撤销 API 契约（实现评审时冻结）。

### 6.3 首个用户引导（修 G2/F2）

- 引导身份**租户化**：`admin-<tenantCode>` / `admin+<tenantCode>@<domain>`，由 `cmd/initialize`、HTTP bootstrap、`provision_tenant` 支持覆盖；
- 创建后置 `must_change_password=true`，登录响应带 `mustChangePassword` 并强制改密；
- bootstrap token 机制保留（一次性、bcrypt 存储、每租户唯一未用），但**必须按 token 所属租户创建**（修 CLI 硬编码 `default` 缺陷）；
- 邮件/链接通道：与邀请共用平台 SMTP（未配置时输出 token/链接由管理员线下传递）。

---

## 7. 数据模型与迁移

### 7.1 P0 新增列/索引（路线 A）

| 变更 | 说明 |
|---|---|
| `users.last_active_tenant_id` | 服务方作用域记忆（登录/切换成功时更新，使用前必须 `CanAccessTenant` 复核） |
| `users.must_change_password` | 首登强制改密 |
| `msp_allocations` 唯一索引 | `(msp_user_id, customer_tenant_id) WHERE deassigned_at IS NULL` |

### 7.2 P1 新增表（路线 B）

| 表 | 关键点 |
|---|---|
| `user_tenant_memberships` | 见 §3.2；带 `tenant_id`（不申请 tenant_guard 豁免、RLS 可覆盖） |
| `invitations`（待冻结） | 一次性 token（哈希存储）、目标租户、角色、有效期、`invited_by`、`revoked_at` |
| 审计事件 | 复用 `audit_logs`，事件类型见 §10 |

### 7.3 回填与回滚

| 步骤 | 内容 |
|---|---|
| 回填 1 | `users.tenant_id` → home membership（`source=home, is_default=true`） |
| 回填 2 | 有效 `msp_allocations` → 服务方客户作用域 membership（`source=allocation`，角色按 primary/backup/specialist 映射） |
| 巡检 | 客户账号恰好 1 条 active 作用域；服务方作用域集合 = provider + 有效分配；角色双源一致性 |
| 读路径切换 | `AccessibleTenants/CanAccessTenant` 接口不变：P0 由 home+allocations 计算，P1 改读 membership（调用方零改动） |
| 回滚 | 新端点/新列可独立下线；bypass 仅新路径使用；加列/加索引在线 DDL，每步可独立回滚；回填脚本幂等 + 差异清单 |

---

## 8. API 契约（新增/调整摘要）

| 端点 | 变更 | 批次 |
|---|---|---|
| `POST /api/v1/auth/login` | 响应新增 `tenantSelection{mode,autoSelected,reason}` + `availableTenants[]`；多作用域 → `409 SCOPE_SELECTION_REQUIRED`；租户无效 → `400 TENANT_NOT_FOUND`；状态异常 → `TENANT_SUSPENDED/TENANT_EXPIRED` | P0 |
| `GET /api/v1/auth/tenants` | 语义修正为 **home ∪ 有效分配 ∪ 平台全量**（修 F9） | P0 |
| `POST /api/v1/auth/switch-tenant` | 响应 `user.tenantId` 与 `tenant` 一致（修 F11）；权限按目标租户重算；旧 refresh 撤销 + 审计（修 F12）；错误码细分 | P0 |
| `POST /api/v1/auth/refresh` | 按 `claims.TenantID` 重签 + membership 复核（修 F10，`TENANT_ACCESS_REVOKED`） | P0 |
| `POST /api/v1/auth/register` | 角色白名单（修 F3） | P0 |
| `POST /api/v1/users`（扩展）/ `POST /api/v1/tenants/:id/users` / `POST /api/v1/msp/customers/:id/users` | 建号三通道（§6.1），走 `ProvisionUser` 收口 | P0 |
| `POST /api/v1/users/invitations`、`GET/POST /api/v1/auth/invitations/:token` | 邀请创建/落地页（P1-1） | P1 |
| `GET /api/v1/msp/allocations/history` | 补齐前端已调用但后端缺失的端点 | P1 |
| 事件目录/通知 | 邀请/密码重置/安全告警走平台 SMTP（见通知方案） | P0/P1 |

---

## 9. 前端架构（itsm-frontend）

| 能力 | 目标设计 | 对应缺口 |
|---|---|---|
| 租户上下文 | 统一 `tenant-context`：当前作用域（id/code/type/role/source）+ 注入 `X-Tenant-ID/X-Tenant-Code`；服务方头通道注入 `X-Customer-Tenant-ID`（仅单请求只读） | F13/F15 |
| 登录页 | **无任何租户选择器/列表**（隐私红线）；仅可选"企业代码"文本输入或专属域名（定位手段，非选择器） | F8/F13 |
| 登录落地 | customer → 唯一租户；provider → provider 家；platform → 控制台（认证后才出现治理选择器） | F8 |
| 作用域切换器 | 仅服务方与平台可见（顶栏）；切换走 `POST /auth/switch-tenant`，成功后刷新上下文/菜单/权限缓存 | F13 |
| 上下文指示 | 全局可见"当前客户/作用域"标识（顶栏 + 页面标题），避免"在错误客户下操作" | F13 |
| 路由守卫 | 未解析作用域 → 登录/选择页；无权限路由 → 403 页；切换中 → 阻塞业务请求 | — |
| API 对齐 | `tenant-api.ts` 的 `switchTenant` 修正为 `/api/v1/auth/switch-tenant`（现打到不存在的 `/api/v1/tenants/switch`） | F11/F13 |
| 记忆 | `localStorage.current_tenant_id/code` 仅作提示，**登录时以服务端为准覆盖** | — |

**原则**：前端不得成为租户归属的唯一判定者；任何"当前租户"展示必须来自服务端上下文（§4）。

> **产品视角 FAQ**：服务商如何在同一前端处理多个客户（跨客户总览 / 头通道只读 / 作用域切换三种方式，及"是否需要切换系统"的结论）见[用户交互流程图 §0.5](./msp-user-interaction-flows.md)。

> **前端细化**：页面/权限/菜单/上下文现状与改造清单见[前端页面与权限分析](./msp-frontend-pages-and-permissions-analysis.md)；登录页隐私约束与切换器细化见[登录与切换细化方案](./msp-login-and-switching-refinement-plan.md)。

---

## 10. 审计与合规

| 事件 | 触发 | 关键字段 |
|---|---|---|
| `auth.login` | 登录成功/失败 | `target_tenant`、`selection_mode`、`failure_reason` |
| `tenant.scope_switch` | 切换/头通道 | `actor`、`from/to`、`source(explicit\|header\|last_active)`、`result`、`ip`、`ua` |
| `tenant.scope_denied` | 越权尝试 | `actor`、`requested_tenant`、命中原因（未分配/状态异常/参数冲突） |
| `user.provision` | 各通道建号 | `actor_user_id/actor_tenant/target_tenant/channel/role/msp_role` |
| `user.invite` / `user.invite_accept` | 邀请创建/接受 | `token_id`、目标租户、角色 |
| `membership.grant/revoke/suspend/role_change` | 作用域生命周期 | `actor`、`subject`、`tenant`、`role`、`source`、`reason`、`before/after` |
| `tenant.lifecycle` | 租户开通/暂停/过期/退租 | `actor`、`target`、`before/after` |

- 审计写入与业务变更**同事务**；跨租户操作必须能回答"谁、以什么身份、在哪个作用域、对哪个租户做了什么"；
- **现状覆盖**：登录成功/失败审计与 bootstrap 首管创建审计**已实现**（`itsm-backend/middleware/audit.go:24-41`；`pkg/bootstrap/token.go:164-174`）；建号仅有通用 AuditMiddleware（🟡）；**切换与分配增删无审计（F12）**，是本节主要补齐项；
- 保留策略：审计按合规策略（≥1 年），不随通知清理任务删除；
- 与通知方案联动：邀请/密码重置/安全告警的投递审计在 `notification_deliveries`，业务审计在 `audit_logs`，两者以 `trace`/`request_id` 关联。

---

## 11. 部署与运行

| 项 | 设计 |
|---|---|
| 拓扑 | **不变**：一套 `itsm-api`/`itsm-worker`/Web 服务全部租户；共享 PostgreSQL（共享 Schema） |
| 模式门控 | `saas_msp` 才开放 `/api/v1/msp/*`（`private` 整族 404；未知模式按 SaaS 默认开启，见 02 §9） |
| 迁移 | 应用不自动迁移；发布前 bootstrap/migration 建表建索引（在线 DDL） |
| RLS | `off/shadow/enforce` 灰度；`enforce` 前先跑影子差异清单 |
| 监控 | 解析失败率、越权拒绝（`scope_denied`）、切换成功率、`last_active` 兜底命中率、membership 巡检差异、租户 provisioning 队列 |
| 运行手册 | 开通客户租户、首个管理员引导、分配/回收、租户暂停/退租、RLS 灰度（见 02/06 文档 + 新增章节） |

---

## 12. 演进路径与分期

| 阶段 | 范围 | 交付物 | 对应缺口 |
|---|---|---|---|
| **P0（路线 A，2–3 批次）** | 统一 `AccessibleTenants/CanAccessTenant`；登录解析与租户状态校验；切换修复（响应/权限/审计/令牌）；refresh 修复；注册角色白名单；建号通道收口；前端上下文与切换入口；`last_active_tenant_id`/`must_change_password` | 最小闭环（客户可登录、服务方可切换、越权 fail-closed） | F1–F6、F8–F13、G1–G3 |
| **P1（路线 B + 体验）** | `user_tenant_memberships` + 回填巡检；上下文扩展（`tenant_source/membership_id/is_platform_admin`）；4 个 MSP 角色模板（Q7）；邀请流（P1-1）；首登引导租户化（修 G2/F2）；前端作用域选择器/指示器；分配回收编排 + 历史接口 | 目标模型落地 | F7、F14、F15、G2 |
| **P2（演进）** | `service_account` 主体；作用域扩展 grants（触发条件满足时）；到期回收自动化；跨租户报表 | 规模化运营 | — |

**验收基线**：主方案 §8 测试矩阵 + 越权矩阵；本文档 §5.4 反例全部自动化覆盖；架构评审以本文档为基准。

---

## 13. 风险与开放问题

| # | 风险/问题 | 处置 |
|---|---|---|
| R1 | `users.tenant_id` 与 membership 双源漂移 | 巡检任务 + 读路径以 membership 为准 + 差异清单告警 |
| R2 | 服务方会话在作用域回收后仍有效 | 每次请求复核 membership；回收即软删 + 旧令牌撤销 |
| R3 | RLS `enforce` 后跨租户 bypass 误用 | bypass 仅 seeder/provisioner/显式建号通道；使用点审计 + 静态检查 |
| R4 | 前端缓存（菜单/权限）未随切换刷新 | 切换成功后强制刷新上下文与缓存；切换中阻塞业务请求 |
| R5 | 邀请 token 泄漏 | 一次性 + 短 TTL + 哈希存储 + 撤销 + 审计；落地页不在日志中回显 token |
| R6 | 平台管理员误操作客户租户 | 治理模式显式选择 + 高危二次确认 + 审计告警 |
| R7 | `must_change_password` 与 SSO/API 客户端冲突 | 仅交互式登录强制；API token 场景另行策略（P1 评审） |
| R8 | `invitations` 表 DDL/撤销契约未冻结 | P1-1 实现评审前冻结（§6.2 待定项） |

---

## 14. 附录 A：证据索引

| 主题 | 位置 |
|---|---|
| 现状架构 | `docs/multi-tenant/01-architecture.md`（全文）；`itsm-backend/middleware/tenant.go:138-169`；`middleware/msp_middleware.go:19-27, 133`；`internal/schema/tenant_guard.go:65-127` |
| 目标模型与决策 | 主方案 `docs/multi-tenant/plan/msp-user-lifecycle-and-tenant-switching-plan.md`（§4.1–4.3:255-282、§5:284-512、§6:513-575）；分析文档 `.../msp-scope-model-analysis-and-ai-gateway-reference.md`（§0:10-40、§3:136-260） |
| 权限模板与 Q7 | 主方案 §5.5.2:451-466；分析 §3.3/B.8:419-484 |
| 建号通道与邀请 | 主方案 §5.2:311-389、§5.3:390-428；通知方案 §4.5/§7 P0-1 |
| 审计事件 | 主方案 §5.5.3:467-474；分析 §1.6:108-112 |
| 现状缺口 | `docs/multi-tenant/07-known-gaps.md`（G1–G10） |
| 前端现状 | `itsm-frontend/src/lib/auth/tenant-context.ts`；`lib/api/http-client.ts:170-195`；`lib/api/tenant-api.ts:42-44` |
| ADR | `docs/architecture/adr-004-multi-customer-tenant-model-selection.md` |

## 附录 B：与既有文档的关系

- 本文是**目标架构**；`01-architecture.md` 是**现状架构**；两者差异即演进范围（§12）；
- 主方案是**实现计划**（批次/任务/验收）；分析文档是**模型论证**（Q1 回答 + 参考实现）；本文是**架构依据**（评审与验收口径）；
- 冲突时优先级：ADR-004 > 本文 > 主方案 > 分析文档（文档状态均为 Draft/Proposed，评审后同步）。

## 修订记录

| 版本 | 日期 | 变更 |
|---|---|---|
| v0.1 | 2026-09-29 | 首版：目标架构（账号唯一/作用域多元）、身份与 membership 模型、租户上下文与 fail-closed 解析、隔离与权限（Q7 角色模板）、建号/邀请/首登、数据模型与迁移、API 契约、前端架构、审计、部署与演进（A→B）、风险与开放问题 |
