# MSP 建号与注册流程（租户创建之后）

> 状态：**Draft v0.2（2026-09-30 决策冻结同步）**｜日期：2026-09-30｜基准：仓库 HEAD `337558e3`
> 定位：**账号生命周期的流程规范**——`msp_provider`/`msp_customer` 租户创建完成后，provider 侧与 customer 侧"如何建号、邀请、注册、首登"的端到端流程、授权边界与过渡期操作。
> 上位（权威顺序）：[canon](./msp-concept-model-and-architecture-canon.md)（概念/边界/决策）→ [目标架构 §6](./msp-target-architecture.md)（建号/邀请/首登设计）→ [主方案 §5.2/F1–F4](./msp-user-lifecycle-and-tenant-switching-plan.md)（通道矩阵与缺口）→ [实施方案 `IP-P0-5`/`IP-P1-4`/`IP-P1-5`](./msp-implementation-plan.md)（落地步骤）。
> 相关：[登录与切换细化](./msp-login-and-switching-refinement-plan.md)｜[工作台方案](./msp-cross-customer-workbench-and-filter-plan.md)｜[三角色演练剧本](./msp-three-persona-operation-simulation.md)

---

## 0. 术语与范围

| 概念 | 定义 | 备注 |
|---|---|---|
| **建号（Provision）** | 管理员直接创建账号（指定/生成初始凭据） | 本流程主路径 |
| **邀请（Invite）** | 一次性 token → 被邀请人自行设置密码（P1 落地） | 推荐用于入职/客户开通 |
| **注册（Register）** | 用户自助注册 | **仅直客 SaaS** 场景；MSP 场景默认关闭 |
| **账号 vs 成员身份** | 账号（`users`，username/email **全局唯一**）≠ 成员身份（home 租户 + membership） | P1 起 `user_tenant_memberships` 为唯一载体 |

**范围**：租户已创建（`POST /tenants`，平台通道）之后的账号流程；不含租户自身的供给（`provision_tenant`，见 02/03）。

---

## 1. 总原则（5 条）

1. **只有 4 个建号通道**：`platform` / `msp` / `tenant` / `invite`；**SQL 直写只是过渡期兜底**（`07:G1/G2`，目标降级，见 §7）。
2. **服务层唯一入口** `ProvisionUser(actor, target, input)`：`CanAccessTenant` → 通道授权 + 角色白名单 → `WithProvisioningBypass(actor, reason)` → `WithTenantID(target)` → `createUser`；**handler 不得自行拼装 bypass**（目标架构 §6.1）。
3. **角色白名单**：显式拒绝 `super_admin/sysadmin/admin`；`msp_role` 仅 `platform`/`msp` 通道可设置（修 F3 提权）。
4. **首登强制改密**：新建/引导账号 `must_change_password=true`；bootstrap 身份**租户化**（`admin-<tenantCode>`）。
5. **登录落 home**：provider 员工登录落 **provider 家**；客户用户落本租户；未认证面（登录页）**无任何租户信息**（I8）。

---

## 2. 通道矩阵（权威定义）

| 通道 | 调用方 | 目标租户 | 前置授权 | 写路径 |
|---|---|---|---|---|
| `platform` | 平台管理员（`super_admin/sysadmin`） | 任意 `active` 租户 | `user:write` + 平台角色 | `WithProvisioningBypass(actor, "platform")` |
| `msp` | `provider_admin`（有效角色 `msp_manager`） | **仅**其有效 allocation 的客户租户 | `msp_customer:write` + **allocation 命中** | 同上，`reason="msp:<providerTenantID>"` |
| `tenant` | 租户内 admin（含客户管理员） | 本租户 | `roleRank` + `CanGrantRoles` | 不需要 bypass |
| `invite` | 持邀请 token 者 | 邀请指定租户 | token 一次性、未过期、未撤销 | 一次性、限定角色 |

**错误码**：`CROSS_TENANT_FORBIDDEN` / `MSP_ALLOCATION_REQUIRED` / `ROLE_NOT_GRANTABLE` / `MSP_ROLE_NOT_ALLOWED` / `USERNAME_EXISTS` / `EMAIL_EXISTS` / `TENANT_NOT_FOUND` / `TENANT_SUSPENDED`。
**灰度**：`USER_PROVISIONING_CHANNELS_ENABLED` 默认关 → 按租户灰度开启（IP-P0-5）。

---

## 3. 账号画像（建号后应满足）

| 主体 | account_kind | home | 家角色 | msp_role | 客户内角色 |
|---|---|---|---|---|---|
| 平台管理员 | platform | `internal` | 平台角色 | — | —（治理通道，不挂工作台，D11） |
| provider 管理员 | provider | provider 租户 | `msp_manager` | `provider_admin` | 按 allocation 映射 |
| provider 员工 | provider | provider 租户 | `msp_tech`/`msp_viewer`/`msp_specialist` | `provider_agent` | 按 allocation 映射 |
| 客户管理员 | customer | 客户租户 | 客户内 admin | —（legacy 读映射） | 本租户角色 |
| 客户用户 | customer | 客户租户 | `end_user`/`agent` 等 | — | 本租户角色 |

- `msp_role` 收敛为 2 值（`provider_admin/provider_agent`，D10）；`customer_user` 仅 legacy 读映射；
- 客户租户内的 `msp_*` 角色（`msp_viewer/tech/specialist/manager/admin`）是**服务商员工在客户内的第三重约束**，由分配角色映射（`primary→msp_manager`、`backup→msp_tech`、`specialist→msp_specialist`），客户管理员可编辑；
- 客户账号（`account_kind=customer`）**恰好 1 条 active membership**（DB 级兜底，canon B5/A4）；
- **同一人不得同时具备 provider 与 customer 身份**（不同账号）。

---

## 4. 场景流程

### 4.1 Provider 侧

```text
[平台] POST /tenants (type=msp_provider)              ← platform 通道建租户（canon §7.3）
   ↓
[平台] POST /tenants/:id/users                        ← platform 通道建首个管理员
       身份：admin-<tenantCode>；must_change_password=true
   ↓
[provider 管理员] 首登 → 强制改密                      ← 登录落 provider 家
   ↓
[provider 管理员] 建员工（tenant 通道，本租户内）        ← 设 msp_role=provider_agent
       默认家角色映射：provider_admin→msp_manager；provider_agent→msp_tech
   ↓
[provider 管理员] POST /msp/allocations                ← 员工 → 客户（primary/backup/specialist）
   ↓
[员工] 登录（provider 家）→ 工作台按分配看多客户          ← 未分配客户不可见（WB5）
```

**规则**：
- **allocation 是跨客户开关**：无 allocation = 仅 Home 面（provider 家）；
- 建客户侧账号（msp 通道）要求**调用方**对该客户有有效 allocation，且具备 `msp_customer:write`；
- 员工停用/转岗：停用账号 + 解除 allocation（同一事务或先后顺序见 IP-P1-1 巡检）。

### 4.2 Customer 侧

```text
[平台] POST /tenants (type=msp_customer, provider_tenant_id)     ← platform 通道建租户（归属唯一 provider）
   ↓
首个客户管理员（二选一）：
   A. [平台] POST /tenants/:id/users                              ← platform 通道
   B. [provider_admin] POST /msp/customers/:id/users              ← msp 通道（allocation + msp_customer:write）
   ↓
[客户管理员] 首登 → 强制改密（本租户 admin）
   ↓
[客户管理员] 日常建号（tenant 通道）/ 邀请（P1）                   ← 角色白名单，默认 end_user
   ↓
[客户用户] 登录 → 本租户（唯一作用域；无过滤器/切换器/工作台）
```

**规则**：
- **客户租户由平台创建**（`tenant:write`）；"服务商自助开租户"不在当前设计内（产品决策项，见 §8）；
- 客户管理员不能建 `msp_*` 角色（客户内角色权限由客户管理员维护，但授予上限 = 本租户角色权限）；
- 客户用户无跨租户能力；`/msp/workbench/*` 与头通道对客户账号一律 403（A9）。

### 4.3 邀请流程（P1，推荐用于客户用户/员工入职）

```text
创建邀请（actor 有权限）
  → 一次性 token（目标租户 + 角色 + TTL + invited_by）
  → 投递：SMTP 发信（emailSent=true）或 API 返回 inviteUrl（emailSent=false，线下传递）
  → 被邀请人打开落地页 → 校验 token（一次性/未过期/未撤销）
  → 设置密码 → 建账号 + home membership（source=invite）
  → 审计 user.invite_accept；邀请人可在有效期内撤销
```

- 平台 SMTP 未配置时**不阻塞**：固定返回 `inviteUrl` + `emailSent=false`（通知方案 §4.5）；
- 邀请受同一角色白名单约束；`msp_role` 仅 platform/msp 通道可设。

### 4.4 自助注册（受控）

- 仅直客 SaaS 场景可开启；`POST /api/v1/auth/register` 角色**默认 `end_user`**，白名单外拒绝（现状 F3：可注册 `super_admin`，**P0 必修**）；
- `msp_role` 不可通过注册设置；MSP 场景建议关闭公开注册，统一走邀请/建号。

### 4.5 首登与 bootstrap

- 新建/引导账号 → `must_change_password=true` → 登录响应带 `mustChangePassword` → 强制改密页；
- bootstrap token 租户化（`admin-<tenantCode>` / `admin+<tenantCode>@<domain>`），一次性、bcrypt 存储、按 token 所属租户创建（修 `07:G2`）。

---

## 5. 授权边界（谁能建谁）

| 操作 | 平台 | provider_admin | 客户管理员 | 客户用户 |
|---|---|---|---|---|
| 建 provider 租户 | ✅ | ❌ | ❌ | ❌ |
| 建 customer 租户 | ✅ | ❌（当前设计） | ❌ | ❌ |
| 建 provider 首个管理员 | ✅ | —（尚不存在） | ❌ | ❌ |
| 建 provider 员工 | ✅ | ✅（本租户，tenant 通道） | ❌ | ❌ |
| 建 allocation | ✅ | ✅（本 provider） | ❌ | ❌ |
| 建客户首个管理员 | ✅ | ✅（msp 通道，需 allocation） | —（尚不存在） | ❌ |
| 建客户用户 | ✅ | ✅（msp 通道，需 allocation） | ✅（本租户，tenant 通道） | ❌ |
| 设 `msp_role` | ✅ | ✅（本租户员工） | ❌ | ❌ |
| 邀请 | ✅ | ✅（本租户/分配客户） | ✅（本租户） | ❌ |

**授权公式（三权分立，canon §7.3）**：跨客户操作 = **provider 租户 RBAC ∩ Allocation ∩ 客户租户内权限**；建号（msp 通道）额外要求目标租户 `active`。

---

## 6. 审计（必带字段）

| 事件 | 触发 | 关键字段 |
|---|---|---|
| `user.provision` | 任一通道建号 | `actor_account / channel / target_tenant / role / msp_role / result` |
| `user.invite_create` / `user.invite_accept` / `user.invite_revoke` | 邀请生命周期 | `invited_by / target_tenant / role / expires_at / result` |
| `user.first_login_password_change` | 首登改密 | `actor_account / target_tenant` |
| `tenant.switch` | 深度切换（关联） | `from_tenant / to_tenant / source / membership_id` |

> 审计口径见 canon I11；查询按 `target_tenant` 过滤。

---

## 7. 过渡期操作（P0 未上线前，现状可用路径）

按 `scripts/msp/setup-msp-tenants.sh`（8 阶段幂等）：

| 阶段 | 动作 | 对应账号环节 |
|---|---|---|
| 0 | admin 登录取 token | 前置（`tenant:write`） |
| 3 | 创建 `msp_provider` + `msp_customer`（绑定归属） | 租户创建 |
| 6 | SQL 建各租户首个用户（`admin-<code>` 目标态；现状写死风险见 `07:G2`） | 首个管理员（兜底） |
| 7 | 建立分配（mspadmin → 客户） | allocation |
| 8 | 隔离性探针 | 验收证据 |

> **风险提示**：过渡期 SQL 直写绕过审计且不可自助；P0 上线后降级为兜底（默认不使用）。

---

## 8. 现状 vs 目标（映射与决议）

| 环节 | 现状（as-is） | 目标 | 工作流 |
|---|---|---|---|
| 跨租户建号 | ❌ 403/Ent hook 拦截，仅 SQL（`07:G1`）→ **✅ 已交付（2026-09-30，IP-P0-5）** | platform/msp 通道 | **IP-P0-5** |
| 服务商建号 | ❌ 无 MSP 用户端点（K4）→ **✅ 已交付（2026-09-30，`/msp/customers/:id/users`）** | `/msp/customers/:id/users` | IP-P0-5 |
| 首个管理员 | ⚠️ 写死 `admin`，第二租户撞唯一（`07:G2`） | `admin-<tenantCode>` + 强制改密 | **IP-P1-5** |
| 邀请 | ❌ 无 | invitations + 一次性 token | **IP-P1-4** |
| 注册角色 | ⚠️ 无白名单，可提权（F3）→ **✅ 已修（2026-09-30，IP-P0-5：仅 `end_user`）** | 白名单默认 `end_user` | IP-P0-5 |
| 分配校验 | ⚠️ 不校验归属（R2）；通道绕过（R9/R10） | 归属一致性 + 统一授权入口 | IP-P0-2/4 |

**决议（2026-09-30）**：① 服务商自助建 customer 租户：**当前否**（平台通道为主，P2 评估）；② 邀请 token：TTL 72h（`INVITATION_TTL_HOURS` 可配）、sha256 哈希、一次性、撤销 API `POST /api/v1/users/invitations/:id/revoke`（`invitations` DDL 随 `IP-P1-4`）；③ 直客（`saas_customer`）：**允许，类型即显式标记**（`msp_provider_id` 为空；canon D2）。

---

## 9. 验收（本流程专项）

- [x] 四通道矩阵：platform/msp/tenant **已交付（2026-09-30，IP-P0-5，单测 8 用例）**；invite 归 `IP-P1-4`；
- [x] 服务商经 API 为**已分配客户**建号成功；未分配客户 403（`MSP_ALLOCATION_REQUIRED`）——**单测覆盖（2026-09-30）**；
- [x] 注册 `super_admin` 被拒（修 F3）；`msp_role` 仅 platform/msp/provider 本租户可设——**单测覆盖（2026-09-30）**；
- [ ] 首个管理员流程：连续 2 个租户 bootstrap 均成功（`07:G2` 关闭）；首登强制改密生效；
- [ ] 邀请：创建→落地→设密→首登→审计→撤销全链路；SMTP 未配置时 `inviteUrl` 可用；
- [ ] 审计：`user.provision`/`user.invite_*` 可按 `target_tenant` 查询；
- [ ] 与 [三角色演练剧本](./msp-three-persona-operation-simulation.md) M11/M12 及 `IP-P0-5` DoD 一致。

---

## 修订记录

| 版本 | 日期 | 变更 |
|---|---|---|
| v0.1 | 2026-09-29 | 首版：四通道矩阵、provider/customer 双场景流程、邀请与首登、授权边界、审计字段、过渡期操作、现状→目标映射与专项验收 |
| v0.2 | 2026-09-30 | 决策冻结同步：§8 未决项改为决议（自助开租户否 / 邀请契约冻结 / 直客类型即标记）；基准 HEAD 重钉 `337558e3` |
| v0.3 | 2026-09-30 | §8 标题术语对齐（"未决"→"决议"）；邀请 DDL 权威指针（[实施方案 §4.0-C](./msp-implementation-plan.md)） |
