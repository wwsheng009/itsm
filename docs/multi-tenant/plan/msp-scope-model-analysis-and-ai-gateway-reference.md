# 服务方 / 客户方作用域模型分析（参考 ai-gateway 多租户设计）

> 状态：Draft（待评审）｜日期：2026-09-29
> 关联：[用户生命周期与租户切换方案](./msp-user-lifecycle-and-tenant-switching-plan.md)（下称"主方案"）、[ADR-004](../../architecture/adr-004-multi-customer-tenant-model-selection.md)、[07 已知缺口](../07-known-gaps.md)
> 参考实现：`E:\projects\ai\ai-gateway`（`tenant_membership` 迁移 127、`docs/plan/shop-multi-tenant-architecture-implementation-plan-20260608.md`）
> 目的：① 正式回答主方案 Q1（服务方/客户方权限边界）；② 论证"**一个账号、多个作用域（scope）**"目标模型；③ 给出可借鉴/不可照搬清单与对主方案的分期调整建议。

---

## 0. 结论先行

### 0.1 业务规则确认（Q1 正式回答）

| 维度 | **客户方账号** | **服务方账号** | 平台账号 |
|---|---|---|---|
| 账号数量 | 一租户一账号（同一人服务多家客户 → 多个账号，或改用服务方身份） | **一个账号** | 一个账号 |
| 跨租户 | ❌ **不允许**（作用域恒等于其所属客户租户） | ✅ 允许，但**仅限被授权的客户租户集合**（= 有效 `MSPAllocation`）+ 自身 provider 租户 | ✅ 全域（受 RBAC + 审计 + 高危二次确认） |
| 作用域来源 | 账号创建时绑定（home membership，唯一） | `provider 租户` + 每个有效分配生成一条 membership | 平台身份（`IsPlatformAdmin` 式能力位） |
| 租户内角色 | 客户租户模板角色（admin/manager/agent/technician/end_user），**拥有大部分业务级功能** | 每个作用域独立角色：primary→`msp_manager`、backup→`msp_tech`、specialist→`msp_specialist`；客户租户内的业务权限按 §3.3 基线收敛 | 由平台 RBAC 决定 |
| 会话行为 | 登录即锁定唯一租户；**前端不出现租户选择器**（多命中即数据异常，fail-closed） | 登录后可在已授权作用域间**显式选择/切换**；前端提供作用域选择器与"当前客户"指示 | 治理模式：先选目标租户再操作 |

### 0.2 目标模型（一句话）

> **账号（Account）只有一个；"作用域"（Membership/Scope）可以有多个；每个作用域有独立角色；会话中只有一个"当前生效作用域"（Active Tenant），由登录解析或显式切换产生，且必须能被服务端按 membership 复核。**

这正是 ai-gateway 的 `tenant_membership` + `TenantContext` 模型；区别在于我们把"客户方单作用域、服务方多作用域"上升为**账号类型（account_kind）级别的强约束**。

### 0.3 对主方案的影响（摘要）

1. **路线 B（memberships）不再是"可选演进"，而是目标模型的正式部分**，建议由 P2 提前到 **P1 首批**（主方案 F7 的定位随之更新）；
2. **无需改动 `users.email/username` 全局唯一约束**（原 P1-2 的"邮箱租户内唯一"可**取消**）：客户方不跨租户 → 一邮箱一账号即可；服务方多作用域通过 membership 表达，与邮箱唯一性无关。这同时消除了主方案 §9.1 的一项中风险迁移；
3. **服务方作用域 = `MSPAllocation` 的物化**（分配变化 → membership 同步），客户方作用域 = 创建账号时写入的唯一 home membership；
4. 主方案的登录解析顺序（§5.3.1）需按 ai-gateway 的 fail-closed 原则**收紧**：客户方多作用域不自动选、直接报错；服务方允许"上次作用域/显式选择"，但不允许请求参数越权指定。

### 0.4 可借鉴 / 不可照搬（速览）

**可借鉴（9 条）**：① membership 表结构与唯一约束；② `TenantContext`（含 `Source`/`MembershipID`/`RequestTenantID`）；③ 解析优先级 8 级与"非平台管理员不得用参数指定租户"；④ 会话租户与 membership 复核（JWT 不是授权事实）；⑤ 权限 = RBAC × membership × 租户状态 × 资源属主；⑥ 迁移期 `default_compat` 显式标记 + 审计 + 关闭计划；⑦ 登录多候选 → `409 + 候选列表`（一次性选择后锁定）；⑧ JWT 三件套（`tenant_id`/`source`/`membership_id`）；⑨ Admin 通道 header 选择仅限平台管理员且不覆盖上下文。

**不可照搬（3 条）**：① ai-gateway 的租户是**商家/SaaS 客户**语义（`tenant` 同时承载平台租户与业务租户），ITSM 的 `msp_provider/msp_customer` 是**服务关系**语义，作用域推导必须走分配；② 其 `tenant_resolution_rule`（域名/Header 规则）在 ITSM 的多客户场景下**不应开放**给客户侧（会造成"静默切租户"）；③ 其 `subject_type` 泛化（admin/user/merchant/access_key/service_account）对 ITSM 过重，第一期只保留 `user` + `service_account` 两类主体。

---

## 1. ai-gateway 多租户设计剖析

### 1.0 术语澄清（重要）

`ai-gateway` 中 **tenant 是唯一的作用域实体**；`workspace` **不是后端实体**，只是 `apps/portal-modern` 前端的"页面工作区"命名（布局/路由守卫/表单组件），`migrations/` 与 `internal/` 中不存在 workspace 表或服务。本文所有"作用域"讨论均对应其 `tenant_membership`。

### 1.1 数据模型

| 表 | 作用 | 关键字段/约束 |
|---|---|---|
| `tenant` | 租户主表（扩展 `display_name/slug/description/metadata`） | `slug` 部分唯一（未删除）；`status/kind` 索引（迁移 127:1-32） |
| `tenant_membership` | **主体↔租户**关系与租户内角色 | `tenant_id / subject_type / subject_id / role / status / is_default / deleted_at`；唯一 `(tenant_id, subject_type, subject_id) WHERE deleted_at IS NULL`；唯一默认 `(subject_type, subject_id) WHERE status='active' AND is_default`（迁移 127:34-64） |
| `tenant_resolution_rule` | 域名/Header 等**自动**解析规则（带优先级） | `rule_type/rule_key/rule_value/priority/status`（迁移 127:66-80） |
| `tenant_config` | 租户级配置覆盖 | 平台默认 + 租户覆盖 |

要点：membership **软删除**（`deleted_at`）保留历史；"默认作用域"有唯一性约束；角色挂在 membership 上（租户内角色），而不是用户全局。

### 1.2 租户上下文（TenantContext）

```go
type TenantContext struct {
  TenantID        string // 后端最终生效租户
  ActorID         string
  ActorType       string // admin | user | merchant | service_account | system
  IsPlatformAdmin bool   // 是否允许跨租户管理/显式选择
  Source          string // admin_selected | membership | access_key | domain_rule | default_compat
  MembershipID    string // 命中的 membership（审计）
  RequestTenantID string // 请求显式传入的租户（审计非法尝试）
}
```
（plan 文档 §5.3:330-354）

配套机制：

- **JWT 会话声明三件套**：`shop_tenant_id` + `shop_tenant_source` + `shop_tenant_membership_id`（`internal/gateway/handlers/user_auth.go:2133-2136`），供服务端复核与审计；
- **Admin 通道**：仅平台管理员可用 `X-Shop-Tenant-ID` / `X-Tenant-ID` / `tenant_id` 显式选择，且不覆盖已有上下文（`internal/gateway/middleware/shop_tenant_context.go:20-23, 47-84`）；
- **机器通道**：Access Key 用独立绑定表 `tenant_access_key_binding` 解析租户（`migrations/127:82-96`）。

### 1.3 解析优先级（plan §7.1:497-516）

1. 后台作业显式任务参数（job schema 声明）→ 2. 平台管理员显式选择 → 3. 租户管理员 membership → 4. 商家 membership → 5. Portal 用户 membership → 6. Access Key 绑定 → 7. 启用后的域名/Header 规则 → 8. `default_compat`（仅迁移期，需审计标记）。

**禁止**：非平台管理员用 query/body/header 指定租户；普通用户多 active membership 时自动选择（必须报错）；租户 `suspended/archived` 时的写操作；解析失败回退全量数据。

### 1.4 失败策略（fail-closed 清单）

| 场景 | 行为 |
|---|---|
| 登录时命中多个 active membership | `409 SHOP_TENANT_SELECTION_REQUIRED` + 候选列表（登录页渲染选择器，选定后会话锁定；`user_auth.go:2607-2625`） |
| 登录后会话解析命中多个 active membership | `SHOP_TENANT_SESSION_AMBIGUOUS`（**不**用 `is_default` 自动选；`shop_user_tenant_context.go:139-160`） |
| JWT 会话租户与当前 membership 不一致 | `SHOP_TENANT_ACCESS_DENIED`（不信任旧 JWT、不静默切换） |
| 请求参数租户与会话租户冲突 | `SHOP_TENANT_SELECTION_CONFLICT` |
| 缺会话租户 | `SHOP_TENANT_SESSION_REQUIRED` |
| 租户暂停 | 写拒绝、读按权限 |

（plan §7.2:531-536、§7.4:591-599、§11.4:1199-1204）

### 1.5 权限模型

判定顺序：`AuthN → 系统 RBAC permission → tenant membership / platform admin scope → tenant status policy → resource owner scope → service operation policy`（plan §11.4:1163-1172）。

- 平台管理员也必须过 RBAC：`IsPlatformAdmin` 只代表 scope 能力，不替代动作权限；
- membership 与 RBAC 是**与**关系，不是或关系；
- 移除 owner 需保留至少一个 active owner（防孤儿租户）。

### 1.6 迁移与审计

- 迁移分期 P0–P6：现状盘点 → 数据模型 → 解析与上下文注入 → 业务链路强制化 → Admin 前端 → 默认租户迁移灰度 → 关闭 fallback（plan §12）；
- 兼容期一切"默认租户"行为必须打 `default_compat` 标记并计入指标（`shop_tenant_default_fallback_total`），有明确的关闭计划；
- 审计必填：租户生命周期、membership 变更、解析规则变更、租户级批量任务、fallback 触发；字段含 `tenant_id/actor/action/target/reason/before/after/result/trace`（plan §11.3:1135-1158）。

### 1.7 前端模式（与本文目标最相关）

- **Admin/治理侧**：先选租户（全局选择器）→ 再进入配置/成员/诊断；请求必须绑定具体 `tenant_id`；
- **普通用户侧**：登录时若账号有多个租户候选，登录页**一次性选择**（`apps/portal-modern/src/app/auth/LoginPage.tsx:127-129, 913-935`）；进入会话后**锁定单一租户、无运行期切换**，页面不提供切换器；历史参数只做一致性校验（plan §4:255-257、§7.2:525-536；`user_shop_tenants.go:26-28` 明确"多 membership 是配置冲突，不是 UI 选项"）。

---

## 2. ITSM 现状与 ai-gateway 对照

| 维度 | ai-gateway | ITSM 现状 | 差距/动作 |
|---|---|---|---|
| 主体↔租户关系 | `tenant_membership`（N:N，带租户内角色） | `users.tenant_id` 1:1（`Required().Unique()`）+ `msp_allocations`（运营侧）+ `user_roles`（**平台级**、无 tenant_id） | **新增 membership**（§4.2），把分配物化为作用域 |
| 角色挂载 | membership 行上的 `role`（租户内） | `users.role` 单值（全局）+ `roles.tenant_id` 的 M2M | membership 承载"作用域内角色"，平台角色保留在 `users.role/user_roles` |
| 请求上下文 | `TenantContext{TenantID, ActorID, ActorType, IsPlatformAdmin, Source, MembershipID, RequestTenantID}` | gin context：`user_id/username/role/tenant_id`（无来源、无成员、无平台位） | 扩展上下文（至少加 `tenant_source`、`membership_id`、`is_platform_admin`） |
| 租户解析 | 8 级优先级 + 严格禁止参数越权 | 静默降级（`tenantCode` 查不到忽略）、签发恒 home | 按 §4.3 重写解析（主方案 F5/F6/F8） |
| 会话与 JWT | JWT 是**会话声明**，须与当前 membership 复核 | JWT claim 被直接信任；refresh 按 home 重签（回退） | 复核 + refresh 按 claim 租户（主方案 F10） |
| 多作用域歧义 | 普通用户多命中 → `SESSION_AMBIGUOUS`（fail-closed，**不自动选**） | 无此概念（客户用户本就 1 租户） | 客户方保持 fail-closed；服务方允许显式选择 |
| 前端 | 治理侧有租户选择器；用户侧**无**选择器 | 两侧都无（`tenants[0]` 固定） | 客户侧无选择器；服务方/平台加作用域选择器（主方案 F13） |
| 兼容期标记 | `default_compat` + 指标 + 关闭计划 | 无标记（默认租户/静默忽略） | 新逻辑不引入默认租户兜底；历史数据仅在迁移脚本中显式标记 |

---

## 3. 业务规则确认（Q1 正式回答）

### 3.1 账号类型（account_kind）与跨租户能力

| account_kind | 判定来源 | 作用域上限 | 说明 |
|---|---|---|---|
| `customer` | 账号创建于 `msp_customer` 租户（且无 provider 身份） | **1（强约束）** | 客户账号永不跨租户；同一自然人服务多家客户 → 多账号（或改由服务方身份承接） |
| `provider` | 账号创建于 `msp_provider` 租户且 `msp_role` 非空 | N（= 1 个 provider 作用域 + 每个有效分配 1 个客户作用域） | "一个账号、多作用域"的主体；分配撤销 → 作用域失效 |
| `platform` | `super_admin` / `sysadmin`（default/平台租户） | 全域 | 治理模式：先显式选择目标租户，全程审计 + 高危二次确认 |

### 3.2 能力矩阵（细化到功能）

**客户方（在自有租户内）**——满足"客户 A 可以使用大部分业务级别的功能"：

| 功能域 | 客户方权限 |
|---|---|
| 工单/事件/服务请求 | 全功能（创建、处理、指派、SLA、满意度） |
| 问题/变更/发布 | 按租户模板角色供给（admin/manager 完整，agent 受限） |
| 知识库/CMDB/服务目录 | 按租户模板角色供给 |
| 用户与角色管理 | 租户内 admin 可管理本租户用户与角色（受 rank 约束） |
| 报表/审计 | 本租户 |
| 跨租户 | **无**（任何形式） |

**服务方（provider 租户内 + 客户作用域内）**：

| 场景 | 允许 | 禁止（默认） |
|---|---|---|
| provider 租户 | MSP 客户列表、分配管理、报表、上下文自检（现有 9 端点） | — |
| 客户作用域（切换/头通道） | 工单读写、知识只读、CMDB 只读、服务目录只读、客户侧账号开通（仅 `msp_manager`） | 变更/发布审批、CMDB 写、系统配置、角色权限管理、客户审计导出 |
| 作用域外客户 | — | 全部拒绝（403，含头通道与切换通道） |

> 权限边界理由：MSP 的核心价值是**代客处理工单**，写权限集中在工单域；其余业务域默认只读，避免服务方越权修改客户资产与配置。若某客户合同要求更宽，走"作用域扩展"（`membership.metadata.scope_grants`，P1 预留，需审计）。

### 3.3 边界与反例（必须拒绝的请求）

1. 客户 A 账号 → 客户 B 任意数据：401/403（现状已保证，回归保持）；
2. 客户账号被邀请/加入到第二个租户：**拒绝**（`account_kind=customer` 单作用域约束，`CUSTOMER_SCOPE_CONFLICT`）；
3. 服务方未分配客户 → 该客户作用域不存在：403（头通道与切换通道同口径）；
4. 服务方以"客户租户本地账号"身份拥有第二身份：**不允许**（同一人如需既是服务方又是客户方，使用不同账号；避免双重身份导致审计与权限歧义）；
5. 任何非平台身份用 query/body/header 指定目标租户：拒绝（仅保留"一致性校验"语义）。

### 3.4 与现有 ITSM 模型的映射（迁移口径）

| 现有 | 迁移为 | 说明 |
|---|---|---|
| `users.tenant_id` | home membership（`source=home`，`is_default=true`） | 保留列作为兼容与快速查询；membership 为权威 |
| `msp_allocations`（有效） | provider 侧 membership（`source=allocation`，`role` 由 `primary/backup/specialist` 映射） | 分配增删 → membership 同步（同一事务） |
| `users.role`（RBAC 主角色） | 平台/租户内角色的**默认值**；membership.role 优先 | 客户方两值一致（回填时校验） |
| `user_roles`（M2M，平台级） | 平台角色保留；租户内角色改挂 membership | 收敛时机：P1（见主方案 §6.3） |

---

## 4. 目标模型设计（ITSM 版 Membership）

### 4.1 概念模型

```text
Account(users)                         Membership(user_tenant_memberships)
  id / username / email / account_kind    user_id / tenant_id / source
        │                                 role_id(租户内) / msp_role / status
        │                                 is_default / allocation_id / deassigned_at
        │
        ├─(customer)  恰好 1 条 active membership ──→ 客户租户（作用域固定）
        ├─(provider)  1 条 provider + N 条客户 ─────→ 可切换作用域集合
        └─(platform)  由 RBAC 决定，全域 ──────────→ 治理选择

Session: JWT { user_id, tenant_id(active scope), role } + 服务端 membership 复核
```

### 4.2 数据模型（建议 DDL）

```sql
-- 账号类型（跨租户能力的唯一判据）
ALTER TABLE users ADD COLUMN account_kind varchar(16) NOT NULL DEFAULT 'customer';
-- 取值：customer | provider | platform；由创建通道决定，禁止自助修改

CREATE TABLE user_tenant_memberships (
  id            bigserial PRIMARY KEY,
  user_id       int  NOT NULL REFERENCES users(id),
  tenant_id     int  NOT NULL REFERENCES tenants(id),
  subject_type  varchar(16) NOT NULL DEFAULT 'user',      -- user | service_account
  source        varchar(16) NOT NULL,                     -- home | allocation | platform | invite | migration
  role_id       int  NULL REFERENCES roles(id),           -- 该租户内的角色（租户模板角色）
  msp_role      varchar(32) NULL,                         -- provider 员工在客户作用域的有效 MSP 角色
  allocation_id int  NULL REFERENCES msp_allocations(id), -- 服务方作用域来源（可追溯）
  status        varchar(16) NOT NULL DEFAULT 'active',    -- active | suspended
  is_default    boolean NOT NULL DEFAULT false,           -- 服务方默认作用域（每人最多 1 条）
  invited_by    int  NULL REFERENCES users(id),
  joined_at     timestamptz NOT NULL DEFAULT now(),
  deassigned_at timestamptz NULL,
  deleted_at    timestamptz NULL,
  created_at    timestamptz NOT NULL DEFAULT now(),
  updated_at    timestamptz NOT NULL DEFAULT now()
);

CREATE UNIQUE INDEX uq_membership_live
  ON user_tenant_memberships (user_id, tenant_id) WHERE deleted_at IS NULL;
CREATE UNIQUE INDEX uq_membership_default
  ON user_tenant_memberships (user_id) WHERE deleted_at IS NULL AND is_default AND status = 'active';
-- 客户方单作用域：靠 account_kind 冗余列做部分唯一（服务层保证冗余列与 users.account_kind 一致）
ALTER TABLE user_tenant_memberships ADD COLUMN account_kind varchar(16) NOT NULL DEFAULT 'customer';
CREATE UNIQUE INDEX uq_customer_single_scope
  ON user_tenant_memberships (user_id) WHERE deleted_at IS NULL AND status = 'active' AND account_kind = 'customer';
```

> 约束设计说明：
> - `uq_membership_live` 保证"同一租户不重复授权"；
> - `uq_membership_default` 保证服务方"默认作用域"唯一；
> - `uq_customer_single_scope` 用**部分唯一索引**把"客户账号不跨租户"落成**数据库级强约束**（不依赖应用自觉）；
> - `account_kind` 冗余进 membership 是为了让上述索引可判定；服务层在写入时从 `users.account_kind` 同步，另有定期一致性巡检（P1）。

### 4.3 会话与作用域解析（ITSM 版优先级）

| 优先级 | 来源 | 适用 | 约束 |
|---|---|---|---|
| 1 | 平台管理员显式选择（治理接口） | platform | 需 `platform:tenant_scope` 权限 + 审计 |
| 2 | 服务方显式选择（登录选择 / 切换 API） | provider | 目标必须在其 active 作用域集合内 |
| 3 | 会话声明（JWT `tenant_id`） | 全部 | **必须**被当前 active membership 复核；不一致 → 401 |
| 4 | 服务方 `last_active` 作用域 | provider | 仅当仍是 active 作用域 |
| 5 | 客户方唯一作用域 | customer | 直接进入，无选择步骤 |
| 6 | 歧义/无作用域 | 全部 | 客户方 → `SESSION_AMBIGUOUS`；服务方 → 要求显式选择 |

**禁止**（对齐 ai-gateway §7.1 的禁止规则）：

- 客户方请求携带 `tenantCode/tenantId/X-Tenant-Code/X-Customer-Tenant-ID` 切换范围：只允许与会话租户一致的校验，不一致 → 401；
- 服务方头通道（`X-Customer-Tenant-ID`）仅用于**单请求只读**，且必须命中作用域；连续操作一律走切换；
- 任何解析失败不得回退默认租户或全量数据（不引入 `default_compat` 式的隐式兜底）。

**登录时的作用域选择（服务方）**：provider 员工命中多个 active 作用域时，登录返回 `409 SCOPE_SELECTION_REQUIRED` + 候选列表（provider 租户 + 已分配客户），由登录页选择后完成签发——与 ai-gateway 的登录选择器一致；选定后写入 JWT，并由服务端按 membership 复核。

**运行期切换（服务方，与 ai-gateway 的差异点）**：ai-gateway 对"普通用户"禁止运行期切换（其普通用户 = 终端客户）。ITSM 的服务方员工是**运营者**（对应其 `admin_user` 主体类型，允许显式选择），因此允许在**自己的作用域集合内**运行期切换；每次切换 = 重签 JWT + 旧 refresh 撤销 + 审计（主方案 §5.4）。客户方**不适用**：任何运行期切换一律拒绝。

### 4.4 权限解析

```text
Allow = AuthN
      ∧ RBAC(permission)  // 按作用域内角色解析（DB role_permissions，回退静态表）
      ∧ Membership(active ∧ tenant_id = 当前作用域)
      ∧ TenantStatus(active，写操作额外校验)
      ∧ ResourceOwner(资源属主/分配)
```

与现状差异：现 `middleware.RequirePermission` 只看 RBAC；新模型下**RBAC 与 membership 是"与"关系**（客户方恒真，服务方构成约束），并在服务方跨租户时额外校验作用域。

### 4.5 前端模式

| 侧 | 交互 |
|---|---|
| 客户方 | 无租户选择器；仅显示当前租户名（只读徽标）；登录页无租户字段 |
| 服务方 | 登录后顶部"作用域切换器"（provider 租户 + 已分配客户）；MSP 页面顶部"当前客户"指示 + 单请求模式开关（头通道） |
| 平台 | 治理模式：进入租户管理前先选目标租户；高危操作二次确认 + 原因 |

### 4.6 审计

| 事件 | 字段 |
|---|---|
| `membership.grant` / `membership.revoke` / `membership.suspend` | actor、subject、tenant、role、source、reason、before/after |
| `tenant.scope_switch` | actor、from/to、source（explicit/last_active）、result、ip、ua |
| `tenant.scope_denied` | actor、requested tenant、命中原因（未分配/状态异常/参数冲突） |
---

## 5. 对主方案的影响与修订建议

| 主方案条目 | 原内容 | 修订建议 |
|---|---|---|
| §4.1 路线 A/B | 路线 B（memberships）为 P2 可选 | **改为：membership 是目标模型的正式部分，P1 首批落地**；P0 仍保持"最小闭环"不变 |
| F7（缺少多租户身份模型） | P1/P2 | 重述为"**membership 未落地**"；客户方单作用域、服务方多作用域由 membership 表达 |
| §5.3.1 登录解析顺序 | "多租户身份 → last_active 自动选择" | **收紧**：客户方多命中 → fail-closed（`SESSION_AMBIGUOUS`）；仅服务方允许 last_active/显式选择（§4.3） |
| §5.5.2 MSP 客户租户权限（F14） | 待评审 | **Q1 已答**：工单读写 + 知识/CMDB/服务目录只读 + `msp_manager` 可开通客户侧账号；其余默认禁止（§3.2） |
| P1-2（邮箱租户内唯一） | 允许同一邮箱多租户 | **取消**：客户方不跨租户 → 保持 `username/email` 全局唯一即可，避免唯一约束迁移风险 |
| P1-3（客户租户 MSP 角色模板） | 模板内置 msp 角色 | 保留，并与 membership.role 对齐（作用域角色指向租户模板角色） |
| 新增 P1-5 | — | `user_tenant_memberships` 表 + 回填（`users.tenant_id` → home；有效 `msp_allocations` → provider 作用域）+ 一致性巡检 |
| 新增 P1-6 | — | 请求上下文扩展：`tenant_source` / `membership_id` / `is_platform_admin`（对齐 ai-gateway `TenantContext`） |

**P0 与 P1 的衔接**：P0-1 引入的 `AccessibleTenants/CanAccessTenant` 抽象保持不变——P0 阶段由 `home + allocations` 计算，P1 改为读 membership 表，**接口与调用方零改动**。

---

## 6. 可借鉴清单（逐条）

| # | 借鉴点 | ai-gateway 出处 | 落到 ITSM |
|---|---|---|---|
| 1 | `tenant_membership` 表结构与两个唯一索引（live 唯一 / default 唯一）+ 软删除 | 迁移 127:34-64 | §4.2（另加客户单作用域部分唯一索引） |
| 2 | `TenantContext` 携带 `Source / MembershipID / RequestTenantID / IsPlatformAdmin` | plan §5.3:328-354 | P1-6 上下文扩展（审计与排障的关键字段） |
| 3 | 解析优先级 8 级 + "非平台管理员不得用参数指定租户" | plan §7.1:497-516 | §4.3 解析表与禁止项 |
| 4 | 会话租户须与当前 membership 复核；JWT 不是授权事实 | plan §7.2:531-536、§7.4:591-599 | 主方案 F10/F11 的修复口径 + P1-5 复核逻辑 |
| 5 | 权限 = RBAC ∧ membership ∧ 租户状态 ∧ 属主 | plan §11.4:1159-1204 | §4.4（现只有 RBAC 一维） |
| 6 | 迁移期"默认租户"必须显式标记 + 指标 + 关闭计划 | plan §7.1:508、§12:1206-1241 | 不引入新的隐式兜底；历史数据仅在迁移脚本显式标记 |
| 7 | 登录多候选 → `409 + 候选列表`（前端一次性选择，选定即锁定会话） | `user_auth.go:2607-2625`；`LoginPage.tsx:913-935` | §4.3 登录选择（服务方），客户方不触发 |
| 8 | JWT 三件套：`tenant_id` + `tenant_source` + `membership_id` | `user_auth.go:2133-2136` | P1-6 上下文扩展的落地形态 |
| 9 | Admin 通道 header 选择"仅平台管理员 + 不覆盖已有上下文" | `shop_tenant_context.go:20-23, 47-84` | 平台治理选择器；服务方禁用 header 越权 |

## 7. 不建议照搬清单

| # | 不照搬 | 原因 |
|---|---|---|
| 1 | `tenant_resolution_rule`（域名/Header 自动解析规则） | ITSM 多客户场景下会造成"静默切租户"；域名仅用于**登录时**的租户定位提示，不作为运行期自动切换依据 |
| 2 | 平台租户与业务租户同表、`subject_type` 泛化到 merchant/access_key | ITSM 的作用域来源是**服务关系**（MSPAllocation），不是主体类型；第一期只保留 `user` / `service_account` 两类主体 |
| 3 | "普通用户登录后无任何租户概念"的完全隐藏 | ITSM 服务方员工本质是"运营者"，必须提供作用域切换器；只有**客户方**沿用"无选择器"模式 |

---

## 8. 决策项更新

| # | 原问题 | 更新 |
|---|---|---|
| Q1 | MSP 员工在客户租户内的权限边界 | ✅ **已答**（§3.2）：工单读写 + 知识/CMDB/服务目录只读 + `msp_manager` 开通客户侧账号；变更/发布/CMDB 写/系统配置默认禁止 |
| Q3 | 是否允许同一邮箱多租户 | ✅ **建议改为不允许**（客户方不跨租户）；保持全局唯一，取消邮箱租户内唯一迁移 |
| Q5 | 路线 B 排期 | ✅ **建议提前到 P1**（membership 作为目标模型正式部分） |
| Q6（新） | 客户账号"转移"到另一租户（合并/迁移） | 建议：仅平台通道；软删旧 membership + 新建，保留审计与历史数据归属 |
| Q7（新） | 服务方写权限是否按客户合同差异化 | 建议：P1 预留 `membership.metadata.scope_grants`，默认不开；开启需审计与客户确认 |
| Q8（新） | `service_account`（API Key/集成）是否纳入作用域 | 建议：第一期不纳入，仅 `user`；后续按 ai-gateway 的 access key binding 模式扩展 |

---

## 9. 分期调整建议（增量）

| 批次 | 增量任务 | 依赖 | 验收 |
|---|---|---|---|
| P0 | 不变（建号通道 / 登录解析 / 切换修复 / 前端入口） | — | 主方案 §8 既有矩阵 |
| P1-a | `user_tenant_memberships` 表 + 回填脚本 + 巡检 | P0-2 | 客户账号恒 1 作用域（DB 约束验证）；服务方作用域 = 分配集合 |
| P1-b | `TenantContext` 扩展（`tenant_source/membership_id/is_platform_admin`）+ 审计落库 | P1-a | 每个请求可回答"租户来源"；越权尝试落 `tenant.scope_denied` |
| P1-c | 客户租户 msp 角色模板与作用域角色对齐 | P1-a | 服务方进入客户作用域后按 §3.2 基线通过/拒绝 |
| P1-d | 作用域切换器前端（服务方/平台） | P1-a/P0-4 | 客户侧无选择器；服务方切换后数据不串租户 |
| P2 | `service_account` 作用域、`scope_grants` 扩展、域名登录定位 | P1-* | 单独立项 |

---

## 10. 附录 A：ai-gateway 证据索引

| 主题 | 位置 |
|---|---|
| 设计原则（tenant_id 非安全边界、普通用户 scope 只能来自会话） | `docs/plan/shop-multi-tenant-architecture-implementation-plan-20260608.md:248-257` |
| 目标架构链路（Admin 选择 / 用户注册-会话锁定） | 同上 `:259-304` |
| `TenantContext` 字段定义 | 同上 `:328-354` |
| `tenant_membership` 设计 | 同上 `:398-431`；`migrations/127_create_shop_tenant_management_postgres.sql:34-64` |
| 解析优先级与禁止规则 | 同上 `:495-516` |
| Admin / 普通用户请求规则（含 fail-closed 错误码） | 同上 `:517-537`、`:589-606` |
| 角色模型与 membership-RBAC 映射 | 同上 `:1103-1113`、`:1159-1204` |
| 审计要求 | 同上 `:1133-1158` |
| 迁移分期与默认租户兼容标记 | 同上 `:1206-1241`；`docs/implementation-progress/risk-decision-log.md:2036, 2102, 2206-2209` |
| 全局用户表（无租户列）与 `email_hash` 全局唯一 | `migrations/046_create_auth_registration_postgres.sql:9`；`internal/model/entity/user.go:11-18` |
| 注册选项 / 注册即建 membership（同事务） | `internal/gateway/handlers/user_auth.go:785, 2862-2884, 3163-3177` |
| 登录解析（0/1/N membership）与 409 选择 | `internal/gateway/handlers/shop_user_tenant_context.go:139-160`；`user_auth.go:1480-1486, 2607-2625` |
| JWT 租户三件套 | `internal/gateway/handlers/user_auth.go:2133-2136` |
| 会话锁定（普通用户不暴露可选租户列表） | `internal/gateway/handlers/user_shop_tenants.go:26-28` |
| Admin 通道 header 选择（仅平台管理员、不覆盖上下文） | `internal/gateway/middleware/shop_tenant_context.go:20-23, 47-84` |
| Access Key 租户绑定表 `tenant_access_key_binding` | `migrations/127_create_shop_tenant_management_postgres.sql:82-96` |
| 前端登录页租户选择器 | `apps/portal-modern/src/app/auth/LoginPage.tsx:127-129, 913-935` |
| membership 枚举（subject/role/status） | `internal/model/entity/shop_tenant.go:15-28` |

---

## 附录 B：Q7 详细设计（服务方写权限差异化）

### B.1 问题原文

> **服务方写权限是否按客户合同差异化？**
> 建议：P1 预留，默认不开；开启需审计与客户确认。

背景：§3.2 的基线是"工单读写 + 知识/CMDB/服务目录只读 + `msp_manager` 开通客户侧账号"。但真实 MSP 合同差异很大：

| 合同形态 | 需要的权限 |
|---|---|
| 只代工单（最常见） | 基线即可 |
| 只读协办（客户自己处理，服务方看单/评论） | 基线再降级：`ticket:read` + 评论 |
| 全托管（CMDB/资产/变更也交给服务方） | 需要 `cmdb:write`、`change:write` 等 |
| 临时项目（迁移/上线支持） | 需要在**限期内**临时提权，到期回收 |

问题本质：**"一刀切"满足不了合同差异；但放开来又会破坏"最小权限"与"服务方不得越权"的边界。**

### B.2 三个选项

| 选项 | 机制 | 优点 | 缺点 |
|---|---|---|---|
| A 全局一刀切 | 所有客户作用域同一套权限 | 最简、无新表 | 不满足差异化；只能靠"给服务方建客户本地账号"绕过（破坏单账号模型） |
| B 作用域授权表（grants） | `membership_scope_grants`（resource/action/有效期/授予人） | 精确到单权限、可到期、可审计、可绑定合同号 | 新表 + 新判定逻辑；与 RBAC 形成"双源" |
| **C 客户侧自定义角色（推荐首选）** | 客户租户 admin 创建/调整角色（如"MSP 全托管"），服务方作用域的 `membership.role` 指向该角色 | **零新表**，复用 RBAC/菜单/审计；与"角色即权限包"一致 | 粒度是"角色"而非单权限；临时提权需建临时角色（可用 membership 的到期时间兜底） |

**推荐：C 为主、B 为辅**——先用"客户租户内的角色"表达合同差异；仅当出现"限时/单次/必须绑定合同号"的提权需求时，再引入 B。

### B.3 通用安全约束（无论选 B 或 C）

1. **上限约束**：服务方在客户作用域的权限 ≤ 客户租户内该角色的权限；grants（B）只能是角色权限的**子集**，不得突破；
2. **授予方**：客户租户 `admin`（本租户内，针对服务方作用域）或平台管理员；**服务方不得自我扩权**（`msp_manager` 也不行）；
3. **生效时机**：角色/授权变更后需**重新登录或重新切换作用域**才生效（对齐 ai-gateway"membership 变更需重新签发会话"，避免运行期权限漂移）；
4. **审计**：`membership.role_change` / `scope_grant.grant|revoke`，必填 reason（合同号/工单号）与 before/after；
5. **到期（B）**：`expires_at` 到期自动失效（惰性判定 + 后台回收任务），避免"离职/合同结束后仍持有权限"；
6. **错误码**：`SCOPE_GRANT_REQUIRED`（403，响应附"所需能力 + 当前角色"），前端可提示"联系客户管理员授权"。

### B.4 B 方案 DDL（备选，触发时启用）

```sql
CREATE TABLE membership_scope_grants (
  id            bigserial PRIMARY KEY,
  membership_id bigint NOT NULL REFERENCES user_tenant_memberships(id),
  resource      varchar(64) NOT NULL,   -- ticket | cmdb | change | knowledge | user | report ...
  action        varchar(32) NOT NULL,   -- read | write | approve | export
  granted_by    int  NOT NULL REFERENCES users(id),
  reason        text NOT NULL,          -- 合同号 / 工单号（审计必填）
  granted_at    timestamptz NOT NULL DEFAULT now(),
  expires_at    timestamptz NULL,
  revoked_at    timestamptz NULL,
  revoked_by    int  NULL REFERENCES users(id)
);
CREATE UNIQUE INDEX uq_scope_grant_live
  ON membership_scope_grants (membership_id, resource, action) WHERE revoked_at IS NULL;
```

### B.5 默认基线（不启用差异化时）

`ticket:read/write`、`knowledge:read`、`cmdb:read`、`service_catalog:read`；`msp_manager` 额外 `user:write`（限客户侧角色）。

### B.6 验收用例（B/C 通用）

1. 默认：服务方尝试 `cmdb:write` → 403 `SCOPE_GRANT_REQUIRED`；
2. 授予后（C：换角色；B：加 grant）：切换作用域重签会话 → 通过；
3. 撤销/过期后 → 再次 403；
4. 服务方尝试给自己授予 → 403（授予方校验）；
5. 审计事件含 reason 与 before/after。

### B.7 待确认

- 选 **C（推荐）** 还是 B？
- 授予方是否包含平台管理员（建议包含，用于应急）；
- 是否需要"到期自动回收"（若是，则 B 成为必须项）。

---

## 修订记录

| 版本 | 日期 | 变更 |
|---|---|---|
| v0.1 | 2026-09-29 | 首版：ai-gateway 设计剖析、ITSM 对照、Q1 正式回答、membership 目标模型（含 DDL/解析/权限/审计）、对主方案的修订建议与分期增量 |
| v0.2 | 2026-09-29 | 补充代码级证据（登录 409 选择器、JWT 三件套、会话锁定、Admin 通道 header 规则、Access Key 绑定）；新增术语澄清（workspace 仅为前端命名）；可借鉴清单扩至 9 条；明确"服务方允许运行期切换、客户方禁止"的差异与理由 |
