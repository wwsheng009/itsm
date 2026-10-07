# 映射式多租户切换收口方案（Membership + Switch）

> 状态：**v0.1（待审查）**｜日期：2026-10-07｜基准：仓库 HEAD `ee9180e5`
> 上位：[canon v1.0](./msp-concept-model-and-architecture-canon.md)（I3/I5/B3/B5/D5/D10）→ [目标架构](./msp-target-architecture.md)（§3.2 Membership / §5.3 服务方在客户作用域内的有效权限 / §6.1 建号通道矩阵）→ [主方案](./msp-user-lifecycle-and-tenant-switching-plan.md)（F7/F14/F15）→ [实施方案](./msp-implementation-plan.md)
> 定位：**L4 收口方案**（局部编号 `MSW-#`；落地建议在实施方案登记为 `IP-P#`，本文件不占用全局编号）。范围：把"账号≠成员身份 + 映射式切换"从**已选定的目标架构**收口到落地——核对现状、登记差距、划定"切换面 vs 跨租户面"边界、给出批次与验收。**不新增目标模型、不新造映射表**。

---

## 0. 摘要（TL;DR）

- **结论**：提案（provider 用户在 customer 租户建立"映射"→ 授权后切换 → 切换后以该租户成员身份使用**全部单租户功能**；provider 侧保留 `/msp` 跨租户能力）= 本仓库**已选定的目标架构**（canon I3：Membership 是账号↔租户关系的唯一载体；I5：provider 员工访问客户 = provider membership + allocation + customer active + 目标租户 RBAC；目标架构 §3.2/§5.3；主方案 F7/F14/F15）。本方案**不做新架构设计，做收口**。
- **已就绪（代码证据，2026-10-07）**：`user_tenant_memberships` 表（IP-P1-1）；权限单源 `membership → role → role_permissions`（IP-P1-2，默认 fail-closed）；`POST /api/v1/auth/switch-tenant`（重签 JWT `tenant_source=switch` + 旧 refresh 轮换 + 审计 + **按目标租户重算权限**）；MSP 授权依据 `msp_allocations`（已补 `provider_tenant_id`）；前端深度切换（abort 在途请求 + `/auth/me` 重拉 + 查询缓存清空 + 深度态指示）。
- **未闭环（本方案要解决的，详见 §2.2）**：① 建号通道不写 membership → 切换后权限为空（**MSW-1/P0**）；② 切换授权仅认 allocation，邀请来源未纳入（MSW-2/待决）；③ 撤销映射与令牌/缓存联动未定义（MSW-3）；④"返回工作台"不切回 provider 作用域（MSW-4/待核）；⑤ 双源（legacy 回退 vs membership）无巡检对账（MSW-5）；⑥ 切换面/跨租户面边界未成文（MSW-6）。
- **批次**：**M1 写路径闭环（P0）** → M2 授权判定与撤销联动 → M3 切换体验收尾（可与 M2 并行）→ M4 边界文档与巡检。
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
| S3 | 切换作用域会话 | `itsm-backend/handlers/auth/service.go:100` `SwitchTenantWithRevoke`：native / super_admin / provider(`msp_role`)+active allocation；重签 JWT（`tenant_source=switch`，scope=目标租户）；旧 refresh 黑名单轮换；审计 `tenant.switch` / `tenant.switch_denied`；**权限按目标租户重算** | 测试：`TestService_SwitchTenant_MSPProviderScope`、`DeniedWithoutAllocation`、`permission_single_source_test.go`（A/B 租户权限独立） |
| S4 | MSP 授权依据（allocation） | `itsm-backend/ent/schema/msp_allocation.go`：`msp_user_id/customer_tenant_id/provider_tenant_id/role/assigned_at/deassigned_at`（IP-P2-1 已补 provider 维度） | 切换与工作台共用 |
| S5 | 前端深度切换 | `itsm-frontend/src/lib/store/auth-store.ts` `switchTenant`：`abortAllRequests()`（防旧作用域响应覆盖）+ `/auth/me` 重拉 + `queryClient.clear()`；`components/layout/header/CustomerFilter.tsx`：深度态指示（`deep-tenant-indicator`）+ 进入客户（`enter-customer-*`） | IP-P0-8 链路 |
| S6 | 跨租户面（与切换并存） | `/msp/*` 路由族 + 工作台（多客户分诊/报表/批量）+ 通知双投递（provider/customer 两侧） | 见单据流转方案 XF/XFD |

### 2.2 未闭环清单（MSW-1 … MSW-6）

**MSW-1（P0）建号与分配通道不写/不同步 membership → 切换后权限空窗**
- 现象：经平台/MSP 通道新建的 provider 员工，即使完成客户分配，切换进客户租户后权限集合为空（fail-closed），表现为菜单缺失/403。
- 根因（代码检索）：生产代码中 membership 写入点仅 `pkg/bootstrap/admin.go`（首管）、`service/invitation_service.go`（邀请）、`service/membership_org_service.go`（组织）；**`service/user_provisioning.go`、`service/user_service.go`、`handlers/user/*` 无 membership 写入**；**`service/msp_allocation_service.go`、`service/msp_bootstrap_allocation.go` 无 membership 同步**；`cmd/` 无 membership 回填命令（存量回填机制待核，见 §2.3 V1）。
- 影响：提案的核心体验（"切过去就是那个租户的用户"）在新建数据上不成立；亦与 canon I3（membership 为唯一载体）相悖。
- 建议：三通道统一 membership 同步（建号 → `source=platform`；MSP 分配/撤销 → `source=allocation` + `allocation_id` + `deassigned_at`；邀请已就绪），抽公共同步函数 + 对账巡检（MSW-5）。

**MSW-2（P1）切换授权仅认 allocation，"邀请来源"未纳入**
- 现状：`SwitchTenantWithRevoke` 放行条件为 native / super_admin / provider+msp_role+active allocation；`source=invite` 的 membership 存在但**不能**作为切换依据。
- 影响：客户邀请 provider 人员协作（无分配关系）时无法切换；授权语义与"映射存在即可切换"的提案口径存在偏差。
- 建议：授权判定抽为单一入口 `authz.Decide(actor, scope, "tenant.switch")`，把 invite 来源纳入（是否默认开放见 MSW-Q1）；同时消除三套权限解析路径的重复租户解析。

**MSW-3（P1）撤销映射与令牌/缓存联动未定义**
- 现状：membership 有 `status/expires_at/deleted_at`，权限缓存有失效函数；但**撤销路径是否调用缓存失效、是否吊销该 user×tenant 的在途 refresh（最长 7 天）未接线**（见 §2.3 V2）。
- 影响：撤销映射后存在"已撤销仍可续签/命中旧权限缓存"的窗口。
- 建议：撤销即失效缓存 + 吊销在途 refresh（或按 MSW-Q3 的宽限策略），并纳入验收 MSW-A4。

**MSW-4（P1，待核）"返回工作台"不切回 provider 作用域**
- 现状：`CustomerFilter.tsx:377` 的"返回工作台"仅 `navigate(MSP_WORKBENCH_PATH)`，**不调用 `switchTenant` 切回 provider**；会话仍停留在客户作用域。
- 待核：客户作用域下访问 `/msp/workbench` 的可用性（403？数据口径？）——见 §2.3 V3。
- 建议：语义二选一并落测：① 返回=切回 home provider（推荐，显式"退出客户作用域"）；② 保持作用域仅导航（需证明工作台在该作用域下语义正确）。

**MSW-5（P2）双源并存无巡检对账**
- 现状：读路径保留 legacy 回退（home + allocation 计算，`handlers/common/service.go` 等），与 membership 单源并存；代码库中未见 membership reconcile/巡检（检索无命中）。
- 影响：漂移（membership 缺失 / `role_id` 空 / `status` 与 `users` 不一致 / `account_kind` 快照过期）只能靠用户报障发现。
- 建议：巡检脚本 + 报告（纳入 M4），漂移分类与修复动作明示。

**MSW-6（P2）"切换面 vs 跨租户面"边界未成文**
- 现状：哪些操作走"切换进客户"、哪些走"/msp 工作台不切换"没有权威决策表；scenarios 亦无对应剧本。
- 影响：新功能默认再长一条跨租户路径（例外面增长复发）。
- 建议：§3.4 决策表入 canon/工作台方案；scenarios 增补"切换 vs 工作台"剧本（含反例）。

### 2.3 待核项（交独立审查，v0.2 回填结论）

| # | 待核问题 | 涉及位置 |
|---|---|---|
| V1 | 存量 membership 回填机制（IP-P1-1 声称"建表 + 回填"）在哪实现、覆盖哪些来源？`membership_org_service.go` 的写入触发与字段完整性（是否含 `role_id`） | `service/membership_org_service.go`、migration/脚本 |
| V2 | 撤销/停用/删除 membership 的调用点是否失效权限缓存、是否吊销 refresh；`invalidateMembershipPermissionCacheForTenant` 的实际调用者 | 全仓检索 |
| V3 | 客户作用域（未切回 provider）下 `/msp/workbench` 的实际行为：403、空列表还是正确数据？（决定 MSW-4 的语义） | `middleware/msp_*`、`service/msp_workbench.go`、路由守卫 |
| V4 | login 的权限解析在 legacy 回退与 membership 单源之间的切换条件；`SourceHome/SourceAllocation` 的遗留口径 | `handlers/common/service.go`、`middleware/membership_permission.go` |
| V5 | 登录期是否允许出现租户选择/切换器（`login-no-tenant-source.test.ts` 约束），与切换面 UX 的边界 | `pages/(auth)/login/__tests__/login-no-tenant-source.test.ts` |

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
- **切换失败**：403 + `tenant.switch_denied` 审计（含 home/target/source），不做目标存在性枚举；目标租户 suspended/expired 拒绝。
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
  1. 建号通道（`UserProvisioningService.ProvisionUser`）在目标租户内写入 membership：`source=platform`（平台通道）/ 由 actor 来源决定；`role_id` 由 `req.RoleIDs/Role` 映射；`account_kind` 按目标租户类型快照；
  2. allocation 分配/撤销（`msp_allocation_service.go`）同步 membership：`source=allocation`、`allocation_id`、`deassigned_at`；
  3. 抽公共 `membership_sync`（幂等 upsert + 冲突策略），供三通道与后续场景复用。
- **验收**：MSW-A1/A2/A6；新增测试覆盖"建号 → 分配 → 切换 → 目标租户权限非空"。
- **回滚**：同步逻辑置于独立服务，可按通道开关回退（回退后仍 fail-closed，不产生越权）。

### M2 授权判定归一 + 撤销联动

- **范围**：`authz.Decide(actor, scope, action)` 单一入口承载 `tenant.switch`（并逐步承接其它判定）；纳入 `source=invite`（按 MSW-Q1）；撤销/停用/删除 membership → 失效权限缓存 + 吊销在途 refresh（或宽限，按 MSW-Q3）。
- **验收**：MSW-A3/A4；`tenant.switch_denied` 审计断言。

### M3 切换体验收尾（可与 M2 并行）

- **范围**：MSW-4 语义落地（返回=切回 provider，或按决策）；深度态指示补全（目标租户/来源/退出入口）；切换失败回滚与提示；客户侧成员列表对 `account_kind=provider` 的只读展示（按 MSW-Q2）。
- **验收**：MSW-A5；前端用例 + 浏览器实测。

### M4 边界文档 + 巡检 + 开关登记

- **范围**：§3.4 决策表入 canon/工作台方案；scenarios 增补"切换 vs 工作台"剧本；membership 巡检脚本（缺失/漂移/快照不一致）；`AUTHZ_STATIC_FALLBACK` 等过渡开关的 owner/到期登记。
- **验收**：MSW-A6；docs-gate 通过；巡检报告样本入档。

**依赖**：M1 → M2/M3 → M4（M3 可与 M2 并行）。

---
## 5. 验收标准（MSW-A1–A6）

| # | 验收 | 判据 |
|---|---|---|
| MSW-A1 | 建号→分配→切换闭环 | 经平台/MSP 通道新建的 provider 员工，完成客户分配后切换：目标租户菜单/权限**非空**且与目标租户角色一致；无 403 误伤 |
| MSW-A2 | 双作用域权限独立 | 同一账号在 provider 与 customer 两作用域的权限集合各自独立（不沿用 home 权限）；扩展 `permission_single_source_test.go` 至建号通道场景 |
| MSW-A3 | 拒绝路径与审计 | 无 allocation/无邀请的切换仍拒绝（403）+ `tenant.switch_denied` 审计；目标 suspended/expired 拒绝 |
| MSW-A4 | 撤销即时生效 | 撤销映射后：权限缓存失效、在途 refresh 失效（或按宽限策略）、再次切换被拒 |
| MSW-A5 | 作用域体验正确 | "返回工作台"后会话回到 provider 作用域（或按 MSW-Q4 决策的语义）；深度态指示与退出入口可用；切换失败不破坏当前会话 |
| MSW-A6 | 巡检与边界 | membership 巡检对种子数据 0 漂移、对人工构造漂移能报出；§3.4 边界表与 scenarios 剧本落档 |

## 6. 风险与缓解

| 风险 | 说明 | 缓解 |
|---|---|---|
| 双源漂移 | legacy 回退（home+allocation）与 membership 并存期数据不一致 | M1 同步 + M4 巡检；回退开关登记到期 |
| 权限空窗被放大 | MSW-1 不补时，"切过去没权限"随切换面扩大成为体验事故 | M1 列为 P0 前置；切换失败/空权限给出可诊断提示 |
| 体验混淆 | 同一人两种身份（provider 员工/客户成员），用户分不清当前作用域 | 深度态指示 + 返回入口 + 客户侧成员标识（M3） |
| 撤销时延 | refresh 最长 7 天 | MSW-3 联动吊销/宽限策略 |
| 开关债务 | `AUTHZ_STATIC_FALLBACK`、灰度开关永久化 | M4 开关登记（owner/到期/默认开计划） |
| 与既有方案的重复 | 本文与目标架构/主方案存在承接关系，若各自演化会产生冲突 | 本文只做收口与差距登记，冲突以 canon/目标架构为准；编号 `MSW-#` 不占用全局空间 |

## 7. 待决策问题（MSW-Q1–Q4）

| # | 问题 | 备选 | 建议 |
|---|---|---|---|
| MSW-Q1 | 邀请来源（`source=invite`）是否可作为切换依据？ | A. 默认放行；B. 需 allocation；C. 需客户侧显式确认 | C（客户同意后落 invite membership 并可切换） |
| MSW-Q2 | 客户侧成员列表是否展示服务方成员？ | A. 不展示；B. 只读展示（标注"服务方"）；C. 可管理 | B |
| MSW-Q3 | 撤销映射的令牌处理？ | A. 立即吊销 refresh；B. 宽限至 access 过期（≤15min） | A（安全优先；如需宽限须登记） |
| MSW-Q4 | "返回工作台"语义？ | A. 切回 provider 作用域；B. 仅导航 | A（避免"带着客户作用域逛工作台"的歧义） |

## 8. 审查记录

> 待独立审查（v0.2 回填）：审查范围、结论、逐条处置（接受/驳回/转待决）。

## 9. 修订记录

| 版本 | 日期 | 变更 |
|---|---|---|
| v0.1 | 2026-10-07 | 首版：提案映射（§1）、现状核对 S1–S6 / MSW-1–6 / 待核 V1–V5（§2）、目标模型与边界决策表（§3）、批次 M1–M4（§4）、验收/风险/待决（§5–§7） |
