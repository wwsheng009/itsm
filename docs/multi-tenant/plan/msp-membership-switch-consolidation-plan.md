# 映射式多租户切换收口方案（Membership + Switch）

> 状态：**v0.9（M1–M4 已落地；MSW-FU-1 闭环：联调库巡检 FAIL=0；MSW-A5 深度切换体验修复）**｜日期：2026-10-08｜基准：仓库 HEAD `78157476`（代码证据同 `ee9180e5`；M1–M4 于后续提交落地）
> 上位：[canon v1.0](./msp-concept-model-and-architecture-canon.md)（I3/I5/B3/B5/D5/D10）→ [目标架构](./msp-target-architecture.md)（§3.2 Membership / §5.3 服务方在客户作用域内的有效权限 / §6.1 建号通道矩阵）→ [主方案](./msp-user-lifecycle-and-tenant-switching-plan.md)（F7/F14/F15）→ [实施方案](./msp-implementation-plan.md)
> 定位：**L4 收口方案**（局部编号 `MSW-#`；落地建议在实施方案登记为 `IP-P#`，本文件不占用全局编号）。范围：把"账号≠成员身份 + 映射式切换"从**已选定的目标架构**收口到落地——核对现状、登记差距、划定"切换面 vs 跨租户面"边界、给出批次与验收。**不新增目标模型、不新造映射表**。

---

## 0. 摘要（TL;DR）

- **结论**：提案（provider 用户在 customer 租户建立"映射"→ 授权后切换 → 切换后以该租户成员身份使用**全部单租户功能**；provider 侧保留 `/msp` 跨租户能力）= 本仓库**已选定的目标架构**（canon I3：Membership 是账号↔租户关系的唯一载体；I5：provider 员工访问客户 = provider membership + allocation + customer active + 目标租户 RBAC；目标架构 §3.2/§5.3；主方案 F7/F14/F15）。本方案**不做新架构设计，做收口**。
- **已就绪（代码证据，2026-10-07）**：`user_tenant_memberships` 表（IP-P1-1）；权限单源 `membership → role → role_permissions`（IP-P1-2，默认 fail-closed）；`POST /api/v1/auth/switch-tenant`（重签 JWT `tenant_source=switch` + 旧 refresh 轮换 + 审计 + **按目标租户重算权限**）；MSP 授权依据 `msp_allocations`（已补 `provider_tenant_id`）；前端深度切换（abort 在途请求 + `/auth/me` 重拉 + 查询缓存清空 + 深度态指示）。
- **未闭环（本方案要解决的，详见 §2.2）**：① 建号与分配通道不写/不同步 membership → 切换后权限为空（**MSW-1/P0，✅ 2026-10-07 已闭环，见 §4 M1 实施记录**）；② 切换授权仅认 allocation，邀请来源未纳入（MSW-2/待决）；③ **生产无 membership 撤销面**，令牌/缓存未与映射绑定（MSW-3，撤销面随 M2）；④"返回工作台"不切回 provider 作用域——审查已查明客户作用域下工作台 403，"仅导航"不可行（MSW-4，Q4 取 A）；⑤ 真实双源＝请求期 RBAC（JWT 角色码回退链）与 membership 单源的口径差，无巡检对账（MSW-5）；⑥ 切换面/跨租户面边界未成文（MSW-6）。
- **批次**：**M1 写路径闭环（P0）** → M2 授权判定与撤销联动 → M3 切换体验收尾（可与 M2 并行）→ M4 边界文档与巡检。
- **独立审查（2026-10-07）**：判定 needs-revision；1 blocker + 7 major + 5 minor/nit 已全部处置（F-01–F-13，见 §8），修正后判定 **ready-to-implement**。
- **价值**：把"深度单租户操作"从跨租户路径里移出来——跨租户能力做减法（只保留 `/msp` 显式面），单租户能力靠切换复用（零改造），从根上停止"为每个功能造跨租户变体"的例外面增长。

---

## 1. 背景与提案映射

### 1.1 提案原始口径（用户，2026-10-07）

> 保持多个租户的数据隔离；增加**用户映射**概念——在 provider 租户中创建用户，并开通该用户在 customer 租户的映射关系；provider 用户像正常账户一样登录，经授权后**切换到不同租户**；切换后**相当于在 customer 租户中处理**，拥有独立用户权限、与 customer 租户用户相同的逻辑；provider 租户用户保留 `/msp` 相关功能（多租户单据报表分析、通知），同时拥有租户切换能力。目标：直接复用已有单租户全部功能，避免功能割裂与过于复杂的架构。

### 1.2 提案 ↔ 权威定义 ↔ 现有实现

| 提案概念 | 权威定义（已冻结） | 现有实现与证据 | 状态 |
|---|---|---|---|
| "用户映射"（provider 用户在 customer 租户的身份） | canon C7/C8 + I3：`user_tenant_memberships` 是账号↔租户关系唯一载体（角色/组织/生效期/状态） | `ent/schema/user_tenant_membership.go`（`account_kind/source/role_id/msp_role/allocation_id/status/is_default/expires_at/deleted_at`） | 表 + 读路径就绪；**写路径未闭环（MSW-1）** |
| "授权后可切换" | 目标架构 §3.3 + 主方案 §5.4 | `handlers/auth/service.go:100` `SwitchTenantWithRevoke`：native / super_admin / provider+msp_role+active allocation；拒绝亦有审计 | 就绪（allocation 为唯一"授权"） |
| "切换后相当于该租户用户" | canon B5：账号跨租户、**权限不跨租户** | JWT scope=目标租户（`tenant_source=switch`）；权限按目标租户 `ResolvePermissions`（membership 单源，fail-closed）；测试锁定 A/B 租户权限独立 | 就绪（`permission_single_source_test.go`） |
| "独立用户权限" | canon I3 + 目标架构 §5.2 | 无 membership → 空权限集合（`AUTHZ_STATIC_FALLBACK` 默认关） | 机制就绪；**建号不写 membership 导致空窗（MSW-1）** |
| 保留 `/msp`（跨租户报表/通知） | canon I5 + 工作台方案 | `/msp/*` 路由族 + 工作台 + 通知双投递（provider/customer 两侧） | 就绪，与切换会话并存 |
| 避免跨租户复杂逻辑 | canon B3：服务商≠超级管理员；跨租户只走显式条目级授权或平台治理 | 跨租户面已收窄为 `/msp` 显式路由 + allocation 校验 + 审计 | 方向一致；**边界未成文（MSW-6）** |

### 1.3 为什么这能停止"例外面增长"

现存的跨租户特例（角色并集 `provider_tenant_admin`、通知重绑、窄写通道、灰度开关）本质是"在不切换的会话里做跨租户事"。映射式切换把**深度操作**移回单租户路径后，跨租户面只剩三类显式场景：**平台治理**（`/admin/tenants`）、**服务商工作台**（`/msp/*`，多客户分诊/报表/批量）、**条目级授权**（单条工单的 reply/status/assign）。其余功能一律"切换后即单租户"，不再需要跨租户变体。

---
## 2. 现状核对（代码为准，2026-10-07，HEAD `ee9180e5`）

### 2.1 已就绪清单（含测试证据）

| # | 能力 | 证据 | 备注 |
|---|---|---|---|
| S1 | 成员身份表（账号≠成员身份） | `itsm-backend/ent/schema/user_tenant_membership.go`：`user_id × tenant_id`、`account_kind`、`source(home/allocation/platform/invite/migration)`、`role_id`、`msp_role`、`allocation_id`、`status(active/suspended)`、`is_default`、`expires_at`、`deleted_at` | IP-P1-1 建表；软删=回收即失效 |
| S2 | 权限单源（membership→role→role_permissions） | `itsm-backend/middleware/membership_permission.go`：按 `user_id+tenant_id` 解析与缓存；`AUTHZ_STATIC_FALLBACK` 默认 **false**（无 membership = fail-closed）；`invalidateMembershipPermissionCacheForTenant` | IP-P1-2 |
| S3 | 切换作用域会话 | `itsm-backend/handlers/auth/service.go:100` `SwitchTenantWithRevoke`：native / super_admin / provider(`msp_role`)+active allocation；provider 判定含 `origin.type=provider` 且 `target.type=customer` 门槛（`:112-121`）；重签 JWT（`tenant_source=switch`，scope=目标租户）；旧 refresh 黑名单轮换（仅 handler 传入旧 cookie 时，`:151-158`）；审计 `tenant.switch` + **仅授权拒绝分支** `tenant.switch_denied`；目标不存在/suspended/expired 在授权之后校验（`:136-141`，当前不落审计，M2 补齐）；**权限按目标租户重算** | 测试：`TestService_SwitchTenant_MSPProviderScope`、`DeniedWithoutAllocation`、`permission_single_source_test.go`（A/B 租户权限独立） |
| S4 | MSP 授权依据（allocation） | `itsm-backend/ent/schema/msp_allocation.go`：`msp_user_id/customer_tenant_id/provider_tenant_id/role/assigned_at/deassigned_at`（IP-P2-1 已补 provider 维度） | 切换与工作台共用 |
| S5 | 前端深度切换 | `itsm-frontend/src/lib/store/auth-store.ts` `switchTenant`：`abortAllRequests()`（防旧作用域响应覆盖）+ `/auth/me` 重拉 + `queryClient.clear()`；`components/layout/header/CustomerFilter.tsx`：深度态指示（`deep-tenant-indicator`）+ 进入客户（`enter-customer-*`） | IP-P0-8 链路 |
| S6 | 跨租户面（与切换并存） | `/msp/*` 路由族 + 工作台（多客户分诊/报表/批量）+ 通知双投递（provider/customer 两侧） | 见单据流转方案 XF/XFD |

### 2.2 未闭环清单（MSW-1 … MSW-6）

**MSW-1（P0）建号与分配通道不写/不同步 membership → 切换后权限空窗**
- 现象：经平台/MSP 通道新建的 provider 员工，即使完成客户分配，切换进客户租户后权限集合为空（fail-closed），表现为菜单缺失/403。
- 根因（代码检索，审查修订）：生产写入点＝`pkg/bootstrap/admin.go`（首管创建）、`service/invitation_service.go`（邀请接受创建）、`cmd/backfill_provider_admin/main.go:251-255`（存量首管角色重绑）；存量回填＝`migrations/20260504_create_user_tenant_memberships.sql:47-141`（home + active allocation + `audit_logs.membership_id`），**未覆盖 platform/invite/migration**。`service/membership_org_service.go` 仅写 `user_tenant_membership_orgs` 子表（无生产调用方，仅测试）。**`service/user_provisioning.go`、`service/user_service.go`、`handlers/user/*`、`service/msp_allocation_service.go`、`service/msp_bootstrap_allocation.go` 经检索无 membership 写入**。
- 影响：提案的核心体验（"切过去就是那个租户的用户"）在新建数据上不成立；亦与 canon I3（membership 为唯一载体）相悖。
- 建议：三通道统一 membership 同步（建号 → `source=platform`；MSP 分配 → `source=allocation` + `allocation_id`；**分配撤销 → `deassigned_at`/状态**；邀请已就绪），抽公共同步函数 + **同步审计**（`membership.sync` 或等价）+ 对账巡检（MSW-5）。

**MSW-2（P1）切换授权仅认 allocation，"邀请来源"未纳入**
- 现状：`SwitchTenantWithRevoke` 放行条件为 native / super_admin / provider+msp_role+active allocation；`source=invite` 的 membership 存在但**不能**作为切换依据。
- 影响：客户邀请 provider 人员协作（无分配关系）时无法切换；授权语义与"映射存在即可切换"的提案口径存在偏差。
- 建议：授权判定抽为单一入口 `authz.Decide(actor, scope, "tenant.switch")`，把 invite 来源纳入（是否默认开放见 MSW-Q1）；同时消除三套权限解析路径的重复租户解析。

**MSW-3（P1）生产无 membership 撤销面，令牌/缓存未与映射绑定**
- 现状（审查修订）：**生产代码中不存在 membership 撤销/停用/删除入口**（检索仅测试与 backfill 角色重绑）；`MSPAllocationService.Deactivate` 只写 `msp_allocations.deassigned_at`（`service/msp_allocation_service.go:252-266`），**不同步 membership、无审计**；`invalidateMembershipPermissionCacheForTenant` 唯一调用者＝角色权限变更链路（`middleware/rbac.go:642`）；refresh 未与 membership 绑定（legacy `SwitchTenant` 不撤旧 refresh，`handlers/auth/service.go:93-96`；仅 handler 传入旧 cookie 时黑名单）。
- 影响：撤销不存在 + 即使撤销也不联动 → "已撤销仍可续签/命中旧权限缓存"的窗口。
- 建议：M2 增补**撤销面**（管理入口/API：membership 停用/软删 + 审计 + 缓存失效 + 按 user×tenant 吊销在途 refresh，策略按 MSW-Q3），并纳入验收 MSW-A4。

**MSW-4（P1，V3 已查明）"返回工作台"不切回 provider 作用域**
- 现状：`CustomerFilter.tsx:377` 的"返回工作台"仅 `navigate(MSP_WORKBENCH_PATH)`，**不调用 `switchTenant` 切回 provider**；会话仍停留在客户作用域。
- V3 结论（静态推演，高置信；建议浏览器 E2E 复核）：客户作用域下 `/api/v1/msp/workbench/*` 在 `RBACMiddleware` 预检即 **403**——要求 `msp_ticket:read`，而 switch JWT 主角色=`users.role`（`handlers/auth/service.go:142`），客户租户内无 msp 权限码；MSP 身份中间件按 home 租户判定本可通过，但先被 RBAC 拒。证据：`router/msp_routes.go:16-19`、`middleware/rbac_precheck_gen.go:369`、`middleware/rbac.go:961-971,1328-1344`、`middleware/msp_middleware.go:55-95`。
- 结论：选项②"保持作用域仅导航"不可行；MSW-Q4 取 A（返回=切回 provider 作用域）。"switch JWT 角色 vs 请求期 RBAC 角色"口径差登记为 MSW-5 巡检/收敛项（影响客户作用域下所有 `/msp/*` 访问）。

**MSW-5（P2）双源并存无巡检对账**
- 现状（审查修订）：读路径已是 membership 单源（`handlers/common/service.go:95` 委托 `middleware.ResolvePermissions`）。**真实双源**＝① 展示/菜单权限（membership 单源）与**请求期 RBACMiddleware**（JWT 角色码 + DB/hardcode 4 层回退链，`middleware/rbac.go`）并存；② `AUTHZ_STATIC_FALLBACK=true` 时回退编译期静态表（默认关）。代码中不存在 "home+allocation 计算" 权限路径；`SourceHome/SourceAllocation` 仅为枚举值（仅测试使用），schema/migration 相关注释为 IP-P1-1 遗留（应清理）。
- 影响：漂移＝`membership.role_id` 与请求期 RBAC（JWT 角色/`role_permissions`）不一致 → 菜单可见但接口 403（或反之）；切换会话的 JWT 主角色与目标租户权限码也可能不一致（见 MSW-4）。
- 建议：巡检目标改为"membership vs 请求期 RBAC 漂移"（缺失 / `role_id` 空 / `status` 不一致 / `account_kind` 快照过期 / JWT 角色 vs 权限码）；纳入 M4。

**MSW-6（P2）"切换面 vs 跨租户面"边界未成文**
- 现状：哪些操作走"切换进客户"、哪些走"/msp 工作台不切换"没有权威决策表；scenarios 亦无对应剧本。
- 影响：新功能默认再长一条跨租户路径（例外面增长复发）。
- 建议：§3.4 决策表入 canon/工作台方案；scenarios 增补"切换 vs 工作台"剧本（含反例）。

### 2.3 待核项结论（独立审查回填，2026-10-07）

| # | 结论 | 证据 |
|---|---|---|
| V1 | **已查明**：存量回填＝`migrations/20260504`（1 home membership + 2 active allocation→`source=allocation`，role_id 取客户租户同名角色、未 seed 则 NULL + 巡检清单 + 3 `audit_logs.membership_id`），**未覆盖 platform/invite/migration**；`membership_org_service.go` 只写 orgs 子表、无生产调用方；`cmd/backfill_provider_admin` 是首管角色重绑（非全量回填） | `migrations/20260504_create_user_tenant_memberships.sql:47-141`；`service/membership_org_service.go:104-203`；`cmd/backfill_provider_admin/main.go:251-255` |
| V2 | **已查明**：`invalidateMembershipPermissionCacheForTenant` 唯一调用者＝`middleware/rbac.go:642`（角色权限变更链路）；生产无 membership 撤销/停用/删除调用点；无按 user×tenant 吊销 refresh 的机制 | `middleware/rbac.go:642,1301-1305`；`service/role_service.go:287/391/479` |
| V3 | **已查明（静态推演，高置信）**：客户作用域访问 `/api/v1/msp/workbench/*` → **403**（RBAC 预检 `msp_ticket:read`；switch JWT 主角色=`users.role`；MSP 身份按 home 租户判定本可通过但先被 RBAC 拒）；建议浏览器 E2E 复核 | 见 MSW-4 |
| V4 | **已查明**：Login/Refresh/GetMe/菜单统一走 `middleware.ResolvePermissions`；legacy 回退仅 `AUTHZ_STATIC_FALLBACK=true` 时的编译期静态表（默认关）；`SourceHome/SourceAllocation` 无运行时分支 | `handlers/common/service.go:95`；`middleware/membership_permission.go:147-150`；`config/config.go:566` |
| V5 | **已查明**：登录页锁定"无租户列表/选择器、不请求 `/auth/tenants`"；作用域变更仅存在于切换面 | `pages/(auth)/login/__tests__/login-no-tenant-source.test.ts:9-24`；`lib/store/auth-store.ts:155-183` |

---
## 3. 目标模型（承接既有权威，不新增）

### 3.1 三原语

1. **Membership（唯一映射）**：账号在某租户内的角色/组织/生效期/状态载体（canon I3）。`users` 只保留账号属性；provider 用户在 customer 租户的"映射"就是一行 `user_tenant_memberships`，**不新造 mapping 表**。
2. **Scope Session（切换会话）**：一次切换 = 重签 JWT（`tenant_source=switch`，scope=目标租户）+ 旧 refresh 轮换；会话内一切按**目标租户**计算（权限、数据、审计），等同该租户用户（canon B5）。
3. **Cross-tenant Surface（跨租户显式面）**：`/msp/*`（工作台/报表/通知/批量）与平台治理 `/admin/tenants`；跨租户访问必须显式（allocation/平台判定）且审计。**除此之外不新增跨租户路径。**

### 3.2 切换授权矩阵（目标口径）

| 场景 | 判定 | 现状 |
|---|---|---|
| 用户切回本租户（home） | native | ✅ 已实现 |
| super_admin 切任意 active 租户 | 平台判定 | ✅ 已实现 |
| provider 员工 → 客户租户 | provider 租户类型 + `msp_role` + **active allocation** + 目标租户 active/未过期 | ✅ 已实现 |
| provider 员工 → 客户租户（邀请来源，无 allocation） | 待决（MSW-Q1）：`source=invite` 的 active membership 是否放行 | ❌ 未实现 |
| 普通客户用户 → 其它租户 | 一律拒绝 | ✅ 已实现（fail-closed） |

### 3.3 权限与失败语义

- **权限只在目标租户内算**：`membership.role_id → role_permissions`（fail-closed；`AUTHZ_STATIC_FALLBACK` 仅过渡，需在 M4 登记关闭计划）。
- **切换失败审计**：授权拒绝分支 403 + `tenant.switch_denied`（含 home/target/source）；**目标不存在/suspended/expired 分支当前未落审计**（`handlers/auth/service.go:132-141`），M2 补齐（MSW-A3 断言全分支）；对未授权者不做目标存在性枚举。
- **无 membership = 空权限**：这是设计（fail-closed），但必须靠 MSW-1 保证"合法映射一定有 membership"，否则表现为"切得过去、没有权限"的体验事故。

### 3.4 边界决策表（MSW-6 的核心产出，建议入 canon/工作台方案）

| 操作类型 | 走哪条路径 | 理由 |
|---|---|---|
| 多客户分诊、跨客户报表、通知中心、批量回复/指派 | **`/msp` 工作台（不切换）** | 跨租户只读 + 批量；唯一跨租户写面，已收窄 + 审计 |
| 单客户深度操作（工单类型/流程/组织/权限配置、复杂工单处理） | **切换进客户租户** | 复用单租户全部功能，零跨租户代码 |
| 平台跨租户账号治理（重置密码/启停/强制下线） | **`/admin/tenants`（TUM 通道）** | 平台面显式治理，独立审计 |
| 建号 / 邀请 / 分配 | 建号通道矩阵（[建号文档](./msp-account-provisioning-and-registration-flow.md)） | 三通道统一写 membership（M1） |

**反例（必须拒绝）**：为单个功能再造"跨租户版本"（如跨租户的工单类型编辑）；以 `users.role`/`msp_role` 直读替代 membership 判定；以 header/参数临时改写作用域。

---

## 4. 落地批次（M1–M4）

### M1（P0）写路径闭环：三通道 → membership 单源

- **范围**：
  1. 建号通道（`UserProvisioningService.ProvisionUser`）在目标租户内写入 membership：`source=home`（建号即账号 home 作用域，与 20260504 回填 1 / bootstrap 同口径；通道可追溯性由 `user.provision` 审计承载）；`role_id` 由 `req.RoleIDs/Role` 映射；`account_kind` 按**账号类型**快照——provider 员工在客户租户=`provider`、客户账号=`customer`、平台账号=`platform`（**禁止按目标租户类型快照**：会与 `uq_customer_single_scope` 冲突并破坏快照语义，证据 `ent/schema/user_tenant_membership.go:29-32,102-106`、`migrations/20260504:96-107`）；`allocation.role → role_id` 映射按 20260504 回填预设（specialist→`msp_specialist`，primary/backup→`msp_tech`；与目标架构 §5.3 的 primary→`msp_manager` 口径差登记为 MSW-Q5），目标角色未就绪按"NULL + 巡检"显式登记，不得静默；
  2. allocation 分配/撤销（`msp_allocation_service.go`）同步 membership：`source=allocation`、`allocation_id`；分配行 `is_default=false`；撤销写 `deassigned_at`/状态；幂等 upsert 明确"复活/更新既有行"策略（约束 `uq_membership_live`/`uq_membership_default`，`ent/schema/user_tenant_membership.go:93-101`）；
  3. 抽公共 `membership_sync`（幂等 upsert + 冲突策略）+ **同步审计**（`membership.sync` 或等价）。
- **验收**：MSW-A1/A2/A6；新增测试覆盖"建号 → 分配 → 切换 → 目标租户权限非空"与同步审计断言。
- **回滚**：同步逻辑置于独立服务；**当前不存在 per-channel membership-sync 开关**（仅全局 `USER_PROVISIONING_CHANNELS_ENABLED`，`service/user_provisioning.go:98-111`）——如需按通道回退须随 M1 实现，否则以 revert 回滚（回退后仍 fail-closed，不产生越权）。
- **实施记录（2026-10-07，v0.3 回填）**：✅ 已落地。新增 `service/membership_sync.go`（幂等 upsert / 冲突不覆盖异源行 / 审计 `membership.sync`、`membership.revoke`）；接入 `ProvisionUser`（`source=home`、account_kind 账号类型、msp_role→词表映射、NULL 告警）、`MSPAllocationService.Create/Deactivate`（`source=allocation`、`allocation_id`、`is_default=false`、撤销软删 `deassigned_at+deleted_at`）、`EnsureBootstrapAllocationsFor*`（自动分配同样同步）。测试：`service/membership_sync_test.go` 5 例（home 幂等 / 分配生命周期 / 冲突不覆盖 / 写守卫兼容 / 建号通道）+ `handlers/auth/membership_switch_closure_test.go` 端到端（建号→登录→分配→切换权限非空且与 home 独立→撤销后切换被拒）。回归：`./service ./middleware ./handlers/auth ./handlers/user ./router` 全绿（另修复 P2 提交漏跑 `authz-gen` 的生成物过期，独立提交 `bb7dc1f7`）。偏差：① source 由计划文本的 platform 修正为 `home`（对齐既有语义）；② 分配角色映射按回填口径（见 MSW-Q5）；③ 撤销的缓存失效 / 在途 refresh 联动仍归 M2（本批测试内显式失效缓存验证 fail-closed 底层语义）。

### M2 授权判定归一 + 撤销联动

- **范围**：`authz.Decide(actor, scope, action)` 单一入口承载 `tenant.switch`（并逐步承接其它判定）；纳入 `source=invite`（按 MSW-Q1）；**增补撤销面**（管理入口/API：membership 停用/软删 + 审计 + 缓存失效 + 按 user×tenant 吊销在途 refresh，策略按 MSW-Q3）；**切换前置校验补 `userEntity.Active`**（当前不检查，被停用账号仍可换发令牌，`handlers/auth/service.go:100-188`）；**切换失败全分支审计**（授权拒绝 + 目标不存在/suspended/expired）。
- **验收**：MSW-A3/A4；`tenant.switch_denied` 全分支审计断言；账号停用场景断言。
- **实施记录（2026-10-07，v0.4 回填）**：✅ 已落地。①授权判定抽为 `internal/authz.DecideTenantSwitch` 纯函数单一入口（决策表 14 例；泛化 `Decide(actor, scope, action)` 签名留待后续批次）；②**Q1=C 落地**：`source=invite ∧ active ∧ invited_by 非空` 的客户侧确认邀请纳入放行（basis=invite；普通客户用户跨租户仍拒绝，不泛化）；③**全分支审计**：allow→`tenant.switch`（basis）/ deny→`tenant.switch_denied`（reasonCode：user_inactive/no_grant/target_not_found/target_suspended/target_expired），未授权者不进入目标存在性分支（防枚举）；④**账号停用校验**：停用账号拒绝换发令牌；⑤**撤销面**：新增 `POST /api/v1/tenants/:id/users/:userId/membership/revoke`（TUM 通道：`tenant:write` 粗筛 + super_admin 硬判定 + 写通道开关；home 不可撤销 409；allocation 来源级联解除分配；软删 `deleted_at+deassigned_at+status=suspended` + 双审计）；⑥**Q3=A 落地**：撤销即 `InvalidateMembershipPermissionCache(user,tenant)` + `InvalidateUserRefreshTokensForTenant(user,tenant)`（新增租户级 refresh cutoff，refresh 校验 user 级/租户级并联 fail-closed、作用域隔离；M1 allocation 撤销路径同步挂接）。测试：authz 决策表、middleware 租户级吊销、service 撤销/级联/缓存/refresh、handlers/auth 全分支审计、TUM service+handler+路由契约全绿（回归清单见 CHANGELOG）。

### M3 切换体验收尾（可与 M2 并行）

- **范围**：MSW-4 语义落地（**MSW-Q4 已决 A：返回=切回 provider 作用域**，依据 V3 403）；深度态指示补全（目标租户/来源/退出入口）；切换失败回滚与提示；客户侧成员列表对 `account_kind=provider` 的只读展示（按 MSW-Q2）。
- **验收**：MSW-A5；前端用例 + 浏览器实测。
- **实施记录（2026-10-07，v0.5 回填）**：✅ 已落地。①**MSW-4/Q4=A**："返回工作台"改为**切回 provider 作用域**（`switchTenant(homeTenantId)` 成功后再导航；失败不导航、保留会话并提示）；后端补齐归属租户契约——切换响应 `user.homeTenantId` 与 `/auth/me.homeTenantId`（前端 `mapServerUser` 映射；旧会话回退 `user.tenantId`）；②深度态指示保留"当前客户：X（深度操作中）"+"返回工作台"（语义升级为切回作用域，按钮 title/loading 完善）；③**切换失败回滚**：`switchTenant` 失败仅复位 loading，user/currentTenant 不变（新增用例断言）；④**Q2=B**：客户侧成员列表（`/admin/users`）新增"服务方成员（只读）"区块——后端 `GET /api/v1/users` 对客户类型租户返回 `providerMembers`（`account_kind=provider ∧ status=active ∧ 未软删`；复用 status/search/department 过滤；独立字段不计入分页 total；读通道失败降级不阻断主列表），行含 `accountKind=provider`/`readOnly=true`；前端只读卡片（服务方标签、无任何管理动作）。测试：后端 service 用例 + 前端 3 套件（CustomerFilter 深度态两例、auth-store 回滚一例、ProviderMembersCard 两例）；实跑（8090）：切换响应/`/auth/me` home=10 ✓、客户管理员列表 `providerMembers=[admin-demo01 readOnly=true role=msp_tech]` ✓。浏览器 MCP 不可用（Chrome 403），以 API 实跑 + 组件用例替代，登记为残余项。

### M4 边界文档 + 巡检 + 开关登记

- **范围**：§3.4 决策表入 canon/工作台方案；scenarios 增补"切换 vs 工作台"剧本；**membership vs 请求期 RBAC 巡检脚本**（缺失 / `role_id` 空 / `status` 不一致 / `account_kind` 快照过期 / JWT 角色 vs 权限码），定义夹具与漂移分类阈值；`AUTHZ_STATIC_FALLBACK` 等过渡开关的 owner/到期登记；IP-P1-1 遗留注释（"读路径仍回退 home+allocation"）清理。
- **验收**：MSW-A6；docs-gate 通过；巡检报告样本入档。
- **实施记录（2026-10-07，v0.6 回填）**：✅ 已落地。①**边界表入档**：canon 新增 §2.3"切换面 vs 跨租户面（边界决策表，MSW-6）"（含 Q4=A 返回语义），工作台方案 §2.2 增补指针；新增剧本 [scenarios/10](../scenarios/10-membership-switch-and-workbench-boundary.md)（深度切换→返回=切回 provider→客户侧只读成员→撤销反例）+ scenarios README 登记；②**巡检**：新增 `internal/membershipcheck`（纯函数：missing_membership / role_id_null / status_mismatch / customer_scope_conflict 为 FAIL；account_kind_snapshot / jwt_role_mismatch 为 WARN；与写入路径 `accountKindForHome` 同口径 + D10 角色映射）与 `cmd/membership-drift-check`（只读；`-json`/`-strict`；FAIL>0 退出码 1）；夹具＝scenarios 种子投影（0 漂移）+ 人工构造 6 类漂移分类断言（单测）；**实跑样本（demo DB）**：`users=68 memberships=38 active_allocations=9 roles=351 tenants=11` → **FAIL=36（全部 missing_membership：M1 前创建的验收用户/历史分配未回填）WARN=3（tenant 1 类型=msp_provider 异常引发的 account_kind 快照）**；报告 artifact：`.dev/membership-drift-report.txt`（本机）；③**开关登记**：[02 §3.1](../02-deployment-and-configuration.md) 登记 6 组过渡开关（owner/默认/退出计划），并声明"巡检 FAIL=0 是关闭 `AUTHZ_STATIC_FALLBACK` 前置"；④**IP-P1-1 遗留注释清理**：Ent schema / config / middleware 三处"读路径仍回退 home+allocation"改为 membership 单源口径 + 开关登记指引。
  **巡检发现（登记为后续项）**：a) **MSW-FU-1**：M1 前存量用户/分配的 membership 回填修复器（重放 20260504 回填 1/2 或提供 backfill CLI；修完复跑巡检至 FAIL=0）；b) **MSW-FU-2**：demo DB `default` 租户类型=`msp_provider` 与平台语义冲突（数据治理，canon §2.2 R7 同源；巡检已按 super_admin 覆盖规则降噪）。
  **MSW-FU-1 实施（2026-10-08 联调库实跑收口，v0.8）**：✅ 完成。①代码：`service/membership_backfill.go` + `cmd/backfill_membership`（dry-run/`-apply`/`-reconcile`/`-json`；幂等 + `membership.sync` 审计；异源存活行不覆盖记 conflicts）；②口径对齐：`accountKindForHome` 增加 super_admin→platform 覆盖（canon D5，与巡检 `expectedKind` 同口径），回归用例 `TestMembershipBackfill_SuperAdminKindPlatform`；③**联调库实跑（经 .dev SSH 隧道 → `127.0.0.1:15433/itsm_prod`）**：dry-run 计划 31 home + 5 allocation → `-apply` 执行（memberships 38→74，+36 条 `membership.sync` 审计）→ FAIL 36→1（剩余 user 7@tenant 2 `role_id_null`：tenant 2 缺角色字典）→ `cmd/provision_tenant -tenant-id 2` 幂等供给基线（36 角色）→ `-apply -reconcile` 校正 → **`cmd/membership-drift-check`：FAIL=0**；报告与日志：`.dev/membership-drift-report.{txt,json}`、`.dev/membership-backfill-{apply,reconcile}.log`；④回归：`go test ./service ./internal/membershipcheck` 全绿。
  **MSW-FU-2 处置（2026-10-08）**：**接受为观察项（维持 saas_msp 部署语义）**。剩余 WARN=2：tenant 1（`default`，type=`msp_provider`）内 E2E 遗留邀请账号 `e2e.invite.1790999578851` / `e2e.invite.1790999723888`（source=invite、account_kind=customer、is_default=true），与 tenant 1 的 provider 类型口径冲突（R7 同源）。处置：不按 provider 改写 invite 语义（邀请协作者=customer 正确），保留 WARN 观察；可选后续动作=清理 E2E 遗留账号（数据卫生，未执行）。`AUTHZ_STATIC_FALLBACK` 关闭前置（巡检 FAIL=0）已满足。

**依赖**：M1 → M2/M3 → M4（M3 可与 M2 并行）。

---
## 5. 验收标准（MSW-A1–A6）

| # | 验收 | 判据 |
|---|---|---|
| MSW-A1 | 建号→分配→切换闭环 | 经平台/MSP 通道新建的 provider 员工，完成客户分配后切换：目标租户菜单/权限**非空**且与目标租户角色一致；无 403 误伤。前置：目标租户角色已就绪（`msp_tech/msp_specialist` 等已 seed）；未就绪按 NULL→巡检清单显式登记（不得静默） |
| MSW-A2 | 双作用域权限独立 | 同一账号在 provider 与 customer 两作用域的权限集合各自独立（不沿用 home 权限）；扩展 `permission_single_source_test.go` 至建号通道场景 |
| MSW-A3 | 拒绝路径与审计 | **全拒绝分支**均 403 + `tenant.switch_denied` 审计：无授权、目标不存在、目标 suspended/expired；被停用账号（`users.active=false`）拒绝换发令牌 |
| MSW-A4 | 撤销即时生效 | 按 MSW-Q3=A：撤销映射（membership 停用/软删）后权限缓存失效、该 user×tenant 在途 refresh 吊销、再次切换被拒；账号停用同理 |
| MSW-A5 | 作用域体验正确 | "返回工作台"后会话回到 provider 作用域（MSW-Q4 已决 A）；深度态指示与退出入口可用；切换失败不破坏当前会话 |
| MSW-A6 | 巡检与边界 | 巡检夹具＝scenarios 种子集：对种子数据 0 漂移；对人工构造漂移（缺失/`role_id` 空/`status` 不一致/`account_kind` 快照/JWT 角色 vs 权限码）能分类报出；§3.4 边界表与 scenarios 剧本落档。**✅ 2026-10-07 落地**：夹具 0 漂移 + 构造 6 类分类断言；边界表入 canon §2.3；scenarios/10 落档；demo 实跑样本入 M4 实施记录 |

## 6. 风险与缓解

| 风险 | 说明 | 缓解 |
|---|---|---|
| 双源漂移 | 展示权限（membership 单源）与请求期 RBAC（JWT 角色码回退链）口径不一致 → 菜单可见但接口 403（或反之） | M1 同步 + M4 巡检（membership vs 请求期 RBAC）；`AUTHZ_STATIC_FALLBACK` 回退开关登记到期 |
| 权限空窗被放大 | MSW-1 不补时，"切过去没权限"随切换面扩大成为体验事故 | M1 列为 P0 前置；切换失败/空权限给出可诊断提示 |
| 体验混淆 | 同一人两种身份（provider 员工/客户成员），用户分不清当前作用域 | 深度态指示 + 返回入口 + 客户侧成员标识（M3） |
| 撤销面缺失/时延 | 生产无 membership 撤销入口；即使撤销，refresh 最长 7 天未与映射绑定 | M2 增补撤销面 + 按 user×tenant 吊销（Q3=A）；MSW-A4 验收 |
| 开关债务 | `AUTHZ_STATIC_FALLBACK`、灰度开关永久化 | M4 开关登记（owner/到期/默认开计划） |
| 与既有方案的重复 | 本文与目标架构/主方案存在承接关系，若各自演化会产生冲突 | 本文只做收口与差距登记，冲突以 canon/目标架构为准；编号 `MSW-#` 不占用全局空间 |

## 7. 待决策问题（MSW-Q1–Q4）

| # | 问题 | 备选 | 建议 |
|---|---|---|---|
| MSW-Q1 | 邀请来源（`source=invite`）是否可作为切换依据？ | A. 默认放行；B. 需 allocation；C. 需客户侧显式确认 | **已决 C（2026-10-07 落地）**：客户侧显式确认的 invite membership（`invited_by` 非空）可切换 |
| MSW-Q2 | 客户侧成员列表是否展示服务方成员？ | A. 不展示；B. 只读展示（标注"服务方"）；C. 可管理 | B |
| MSW-Q3 | 撤销映射的令牌处理？ | A. 立即吊销 refresh；B. 宽限至 access 过期（≤15min） | **已决 A（2026-10-07 落地）**：撤销即吊销 (user×tenant) 在途 refresh；如需宽限须登记 |
| MSW-Q4 | "返回工作台"语义？ | A. 切回 provider 作用域；B. 仅导航 | **已决 A**（V3 已查明：仅导航时 `/msp/workbench/*` 必 403） |
| MSW-Q5 | 分配角色映射口径：目标架构 §5.3（primary→`msp_manager`）vs 20260504 回填/实现（primary/backup→`msp_tech`） | A. 维持回填口径（最小权限）；B. 改按 §5.3；C. 分阶段 | **已决 A（2026-10-07 确认）**：维持回填口径；如需 B 需评审客户租户权限面与存量数据迁移 |

## 8. 审查记录

**独立审查（2026-10-07，只读子代理，基准 HEAD `78157476`）**：结论 **needs-revision**（方向正确、差距清单有价值）；1 blocker + 7 major + 5 minor/nit，全部处置如下（F 编号为审查编号，不入全局编号空间）。

| # | 严重度 | 审查发现 | 处置 |
|---|---|---|---|
| F-01 | blocker | M1 `account_kind` 按"目标租户类型"快照会与 `uq_customer_single_scope` 冲突（provider 员工多客户场景） | **接受**：改为按账号类型快照（provider/customer/platform），M1 已修订 |
| F-02 | major | MSW-1 写入点清单不准确（`membership_org_service` 不写 membership；`cmd/backfill_provider_admin` 写；存量回填在 `migrations/20260504`） | **接受**：MSW-1 根因段重写；核心结论（建号/分配无写入）保留 |
| F-03 | major | MSW-5 "legacy home+allocation 回退"与代码不符；真实双源＝请求期 RBAC 口径差 | **接受**：MSW-5 重写；M4 巡检目标改为 membership vs 请求期 RBAC |
| F-04 | major | 切换失败仅授权拒绝分支落审计（目标不存在/suspended/expired 未落） | **接受**：§3.3 口径修正 + M2 补代码 + A3 全分支断言 |
| F-05 | major | M2/M3 预设的 membership 撤销路径不存在；allocation 撤销不同步 membership、无审计 | **接受**：M2 增补"撤销面"；M1 增补分配撤销同步 + 审计 |
| F-06 | major | V3 查明客户作用域 `/msp/workbench` 403；MSW-4"仅导航"不可行 | **接受**：MSW-4 回填结论；Q4 取 A；口径差登记为 MSW-5 项 |
| F-07 | major | 遗漏：账号停用未校验、目标角色未就绪、`is_default`/upsert 策略、M1 回滚开关不存在 | **接受**：M1/M2 范围与 A1/A3/A4 判据补齐 |
| F-08 | minor | S3 条件描述缺类型门槛与校验顺序 | **接受**：S3 已补 |
| F-09 | minor | MSW-3 低估（无撤销面 + refresh 未绑定） | **接受**：MSW-3 重写并与 F-05 合并推进 |
| F-10 | minor | M1 缺同步审计要求 | **接受**：M1 增补 `membership.sync` 审计 |
| F-11 | minor | A4/A6 判据模糊 | **接受**：A4 定 Q3=A；A6 定义夹具与漂移分类 |
| F-12 | nit | 基准 HEAD 注记过期 | **接受**：头部基准更新为 `78157476` |
| F-13 | nit | 治理合规（R1/R4/R7/编号登记）通过 | 保持 |

**V1–V5**：全部查明，结论见 §2.3。**判定**：修订后 ready-to-implement（待办：M1 前置确认 MSW-Q1/Q3 决议）。

## 9. 修订记录

| 版本 | 日期 | 变更 |
|---|---|---|
| v0.1 | 2026-10-07 | 首版：提案映射（§1）、现状核对 S1–S6 / MSW-1–6 / 待核 V1–V5（§2）、目标模型与边界决策表（§3）、批次 M1–M4（§4）、验收/风险/待决（§5–§7） |
| v0.2 | 2026-10-07 | **独立审查修订（F-01–F-13 全处置，判定 ready-to-implement）**：M1 `account_kind` 口径改按账号类型（blocker）；MSW-1/3/5 按代码重写（写入点清单、无撤销面、真实双源）；切换失败审计口径修正 + M2 补全分支；M2 增补撤销面与账号停用校验；MSW-4 回填 V3 结论（403）并定 Q4=A；A1/A3/A4/A6 判据具体化；§2.3 回填 V1–V5 结论；新增 §8 审查记录 |
| v0.3 | 2026-10-07 | **M1 实施回填**：membership 写路径闭环落地（建号 `source=home` / 分配与撤销 / bootstrap 自动分配 + 审计 + 测试全绿）；source 由 platform 修正为 home；新增 MSW-Q5（分配角色映射口径差）；M1 实施记录入 §4 |
| v0.4 | 2026-10-07 | **M2 实施回填**：切换授权单一入口 `authz.DecideTenantSwitch`（Q1=C invite 依据、账号停用校验、全分支审计）；撤销面 API（TUM）+ allocation 级联 + 缓存/租户级 refresh 吊销（Q3=A）；Q1/Q3/Q5 决议回填 |
| v0.5 | 2026-10-07 | **M3 实施回填**：MSW-4 返回=切回 provider 作用域（homeTenantId 契约 + 失败回滚）；Q2=B 客户侧"服务方成员（只读）"区块；测试与 API 实跑证据入 §4 |
| v0.6 | 2026-10-07 | **M4 实施回填**：边界决策表入 canon §2.3 + 工作台方案指针 + scenarios/10 剧本；membership vs 请求期事实巡检（`internal/membershipcheck` + `cmd/membership-drift-check`，5 类漂移 FAIL/WARN 分级 + 夹具/实跑样本）；过渡开关登记（02 §3.1，owner/到期）；IP-P1-1 遗留注释清理；巡检发现登记 MSW-FU-1/2 |
| v0.7 | 2026-10-07 | **MSW-FU-1 实施回填**：新增存量 membership 回填修复器（`service/membership_backfill.go` + `cmd/backfill_membership`：dry-run/apply/reconcile、幂等、`membership.sync` 审计、巡检 0 漂移闭环单测）；demo 复跑因本机 DB 引擎未启动待执行；MSW-FU-2 维持登记 |
| v0.8 | 2026-10-08 | **MSW-FU-1 联调库收口**：经 .dev SSH 隧道实跑回填（+31 home/+5 allocation）→ tenant 2 基线供给（36 角色）→ reconcile 校正 → 巡检 **FAIL=0**；`accountKindForHome` super_admin→platform 口径对齐（canon D5）；WARN=2 登记为接受观察项（FU-2 处置：E2E 遗留 invite 账号） |
| v0.9 | 2026-10-08 | **MSW-A5 深度切换体验回归修复**：`/auth/me` 契约修复（`tenantId`=当前 JWT 作用域、`homeTenantId`=归属租户；此前两者均为 home → 刷新后作用域回退、深度态指示/目标域菜单丢失）；「进入客户」落点由公开落地页 `/` 修复为 `/dashboard`；浏览器实测通过（进入客户→深度态指示+目标域菜单；`/auth/me tenantId=11/homeTenantId=10`） |
| v0.10 | 2026-10-08 | **深度切换菜单/权限口径落地（C2）**：客户租户内 `msp_*` 角色此前随 default 克隆落入 C1 跨租户码，导致深度切换菜单与 provider 作用域相同（"切换没有变化"）——按实施计划 §3.0-C2 落库客户侧基线（`MSPCustomerScopeRolePermissionCodes` + provision 客户类型对账，provider 租户保持 C1）。实跑 tenant 11：菜单 5→15 项（工单/知识库/CMDB/服务目录等）、`/msp/workbench` 客户作用域 403（canon §2.3 边界）；测试 authz/parity/seeder 全绿 |
