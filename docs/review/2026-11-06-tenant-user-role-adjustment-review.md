# 租户用户管理「角色调整」能力缺口评审

> **状态**：v0.2 复核修订（2026-11-06）——保留当次快照结论，并按当前源码逐条校正证据；
> 衍生的三项真实缺陷已登记为 `07-known-gaps.md` **G12–G14**（其中 G13 本批已修复）
> **检查对象**：`系统管理 → 租户管理 → 租户用户管理`（抽屉 `?tenantUsers=<id>`）
> **检查方式**：运行实例页面走查 + 前端/后端源码契约核对 + 既有规划文档交叉验证
> **结论级别**：**确认「跨租户角色调整」能力不存在（非渲染缺陷）**；
> 但 v0.1 把它判为「业务缺口」**依据不足**——本租户内角色调整是完整可用的产品主干道，
> 真正的空洞只在「平台/服务商跨租户改权限」这一条被显式冻结的边界上
> **建议决策**：**维持 TUM-7「不做」**，改为登记职责边界口径 + 补齐三项契约/UX 缺陷；
> 解除「不做」需先满足 `plan §10` 的两项前置（平台级角色词表 + 变更审批/双人复核）

---

## 1. 结论摘要

| 判断 | 结论 |
|---|---|
| 租户用户管理抽屉是否有「角色调整」入口 | **没有**。行操作仅 `重置密码 / 强制下线 / 离职`；批量仅 `批量启用 / 批量停用` |
| 前端是否具备角色写接口 | **没有**。`tenant-user-admin-api.ts` 无角色变更方法 |
| 后端是否具备租户用户角色写接口 | **没有**。`tenant_user_admin_dto.go` 只有列表/详情/重置密码/启停/强制下线契约，无角色写 DTO |
| **本租户内**谁能改角色 | **租户管理员自助，且通道完整**：`PUT /api/v1/users/:id`（主角色 + `roleIds` RBAC）+ `POST/PUT /roles`、`POST /roles/:id/permissions`（自建角色与勾权限）。这是设计上的**主通道**，不是「替代路径」 |
| 平台侧跨租户是否可改角色 | **不可，且为刻意冻结**。平台跨租户只有「建号（可同时指定角色）」与「账号治理」两类通道；建号后再改角色无跨租户端点 |
| 角色字段是否可见 | **可见但只读**。角色列正常展示（含 MSP 角色合并标签），且有「角色」筛选下拉，说明数据面完整、仅缺写通道 |
| 是否存在替代路径 | **有，且覆盖面大于 v0.1 的判断**。`/admin/users` 编辑框的作用域=调用者自己登录的租户；对「目标租户自己的管理员」而言这就是正常操作路径，v0.1 把它记为「不覆盖」是把**平台运维视角**误当成了**唯一视角** |
| 是否为已知范围决策 | **是**。`msp-tenant-user-management-enhancement-plan.md` §10（TUM-7）明确将「跨租户角色调整」列为 P2 不做实现，并登记触发条件 |

**一句话结论（v0.2 修订）**：既不是界面 bug，也不是「角色变更只能靠离职重建」——
**角色变更在本租户内由租户管理员完整自助**；缺的是**平台/服务商替他租户改角色**的能力，
而这一项是 TUM-7 基于越权面显式拒绝的**边界决策**。本批的真实待办是三项契约/UX 缺陷
（错误码词表漂移、membership 撤销面无口径、护栏计数按当页估算）与一条运维口径登记。

---

## 2. 检查范围与方法

1. **页面走查**：打开 `租户管理`，通过「用户管理」进入租户 `仁和医疗集团 / demo-med`（6 个用户）的租户用户抽屉，逐项核对工具条、表格列、行操作、批量操作、空态。
2. **前端契约核对**：读取抽屉组件与 API 客户端源码，确认是否存在被隐藏/未激活的角色写能力。
3. **后端契约核对**：读取平台侧租户用户管理 DTO，确认服务端是否提供角色写请求体。
4. **交叉验证**：对照 `docs/multi-tenant/plan/msp-tenant-user-management-enhancement-plan.md` 的范围、决策与 P2 评估纪要，判断是「遗漏」还是「既定范围外」。
5. **横向对比**：对照平台侧 `admin/users` 用户管理页的编辑能力，识别能力不对称的位置。

---

## 3. 现状证据

### 3.1 页面证据（运行实例）

抽屉标题：`仁和医疗集团 / demo-med / MSP客户 / 共 6 个用户`。

- **工具条**：刷新、搜索（用户名/姓名/邮箱）、`状态` 筛选、`角色` 筛选、`批量启用`、`批量停用`（未选中时禁用）、`已选 0 / 50`。
- **表格列**：选择框、用户、角色、部门、状态、创建时间、操作。
- **行操作（每行仅三项）**：`重置密码`、`强制下线`、`离职`。
- **角色列**：只读 Tag 展示（安全管理员 / IT管理员 / 最终用户 / 部门经理 / 服务台坐席 / 系统管理员）。
- **状态列**：Switch 启停；其中 `系统管理员`（demo.med.admin）的 Switch 为 **disabled**，属最后管理员保护。
- **批量能力**：仅启用/停用，无批量角色调整。

> 关键点：存在「角色」**筛选**控件（说明角色是列表一等字段、查询通道已通），但不存在任何角色**写**控件——典型的「读通写不通」。

### 3.2 前端契约证据

`itsm-frontend/src/lib/api/tenant-user-admin-api.ts` 暴露的**前端**方法全集：

| 方法 | 端点 |
|---|---|
| `listUsers` | `GET /api/v1/tenants/:id/users` |
| `getUser` | `GET /api/v1/tenants/:id/users/:userId` |
| `resetPassword` | `POST /api/v1/tenants/:id/users/:userId/reset-password` |
| `setStatus` | `PUT /api/v1/tenants/:id/users/:userId/status` |
| `forceLogout` | `POST /api/v1/tenants/:id/users/:userId/force-logout` |
| `offboardUser` | `POST /api/v1/tenants/:id/users/:userId/offboard` |

- 无 `updateRole` / `assignRoles` / `changePrimaryRole` 一类方法。
- **v0.2 校正（v0.1 遗漏）**：后端实际还有一条**已实现但前端零接线**的写通道
  `POST /api/v1/tenants/:id/users/:userId/membership/revoke`（路由 `router/common_system_routes.go:372`、
  handler `handlers/tenant/user_admin.go:156`、RBAC 预检 `middleware/rbac_precheck_gen.go:943`、
  服务 `service/tenant_user_admin.go` `RevokeMembership`）。检索 `itsm-frontend/src` 对该端点**零引用**。
  因此准确口径是「**后端写通道已建、前端未接线**」，不是「读通写不通」；该通道来自 MSW-3
  （`msp-membership-switch-consolidation-plan.md` §M2 实施记录第 ⑤ 项，交付形态即「撤销面 API」）。
- 错误码词表：v0.1 记为「仅 9 项」**有误**。实际前端 `TENANT_USER_ADMIN_ERROR_MESSAGES` 为 **10 项**，
  后端常量表为 **12 项**，差的正是 membership 面的 `TENANT_USER_MEMBERSHIP_NOT_FOUND` / `HOME_MEMBERSHIP_IMMUTABLE`
  —— 二者会以英文/原始 message 透出，属**契约漂移**（该文件头部自证「URL/载荷变更必须同步两侧」）。登记 G12。
- 同样**不存在角色相关稳定错误码**（如「降级后无管理员」「角色不可授予」）——该结论 v0.2 复核仍成立。

`TenantUsersDrawer.tsx` 相关实现要点：

- 行操作仅渲染 `重置密码`、`强制下线`、`离职` 三个按钮。
- 角色列通过 `getUserRoleLabels(record)` 只读渲染（主角色 + MSP 角色合并 Tag）。
- 最后管理员判定：`record.role === 'admin' && record.active && activeAdminCount <= 1`，仅用于**禁止停用**。
- 批量：本页选择、上限 50、逐条串行聚合（后端批量端点为 P1，尚未实现）。

### 3.3 后端契约证据

`itsm-backend/dto/tenant_user_admin_dto.go` 请求体全集：

- `TenantUserListQuery`（page/pageSize/keyword/status/**role** 过滤）
- `TenantUserItem` / `TenantUserListResponse`（**含 `Role`、`MSPRole` 只读字段**）
- `ResetTenantUserPasswordRequest`（mode: generated/specified）
- `SetTenantUserStatusRequest`（active、confirmCode）
- `ForceLogoutTenantUserResponse`
- 离职编排结果（offboard）

- **无任何角色写请求体**（无 `UpdateTenantUserRoleRequest`）。
- 角色只作为**查询过滤条件与响应字段**存在，无写入契约。
- **v0.2 补充**：membership 撤销面无请求体（纯路径参数），所以「DTO 全集」并不等于「写通道全集」；
  计数契约本批新增 `TenantUserListResponse.activeAdminCount`（租户级活跃 admin 全量计数，G13 修复）。

### 3.4 规划文档交叉验证（关键）

`docs/multi-tenant/plan/msp-tenant-user-management-enhancement-plan.md`：

- §1 范围：`**P2（另行评估）**：删除用户（不做物理删除）、跨租户角色调整。`
- §10 / TUM-7 P2 评估纪要（2026-10-05）：`跨租户角色调整 | 不做 | 平台面角色调整放大越权面；需先收敛到平台级角色词表与变更审批流 | RBAC 作用域模型支持平台级角色指派 + 变更审批/双人复核`
- TUM-7 落地记录：`删除用户 / 跨租户角色调整 / 导出用户清单三项均不做实现`。

**含义**：缺口是**已知且被显式接受的**范围外能力，不是实现回归。本次评审价值在于：确认该决策在运行时仍然成立（无隐藏入口、无半成品代码），并把「不做」的业务代价量化后交回决策。

**v0.2 关键校正（影响「不做」论据的语气，不改变结论）**：
`plan:18` 与 TUM-D1（`plan:121`）论证「路由权限 `tenant:write` 不能作平台面依据」时写道
「租户管理员同样持有该权限」并引用 `internal/authz/roles.go:243` —— **该表述已过时**。
2026-10-10 治理域收口（设计 §8 D7）后，`tenant` 家族是**平台治理专属码**：

- 租户 `admin` 角色的权限表内**已无** `tenant:*`（`authz/roles.go:247-249` 注释明示「租户管理员不再持有」，
  该行体现为 `user:read/write/delete`、`role:read/write/delete`、`permission:read`、`group:read/write`）；
- `it_director` / `ops_director` 经 `allExcept` 显式排除 `tenant:read|write`（`roles.go:28,30`）；
- 只有平台角色 `sysadmin` 持码以通过 L2（`roles.go:521-523`）。

也就是说 §10 依据①「平台面角色调整放大越权面」中的**权限码泄露面已被 D7 收敛掉一半**，
`super_admin` 服务层硬校验（`service/tenant_user_admin.go` `requirePlatformScope`）现在更多是 fail-closed 双保险。
**仍然成立的部分**是依据②③：平台级角色词表未收敛、变更审批/双人复核不存在 —— 这才是「不做」的现行拦路项。

### 3.5 横向对比：能力不对称

| 能力 | 平台侧 `admin/users`（本租户） | 租户用户抽屉（目标租户） |
|---|---|---|
| 查看用户 | ✅ | ✅ |
| 搜索/状态/角色筛选 | ✅ | ✅ |
| 建号 | ✅（`POST /api/v1/users`） | ✅（`建号` 按钮 → 建号流程） |
| 重置密码 | ✅ | ✅ |
| 启停 | ✅ | ✅（含批量） |
| 强制下线 | ✅ | ✅ |
| 离职 | ✅ | ✅ |
| **主角色调整** | ✅ 编辑模态框「主角色」 | ❌ |
| **RBAC 角色调整（多选）** | ✅ 编辑模态框「RBAC 角色（可多选）」 | ❌ |
| 批量更新 | ✅（`batchUpdateUsers`） | ❌（仅启停） |

平台侧编辑模态框含 `主角色` + `RBAC 角色（可多选）`（含 `grantable` 过滤，防越权授予），走 `UserApi.updateUser`（`PUT /api/v1/users/:id`）。该端点作用域为**当前登录租户上下文**，平台管理员无法用它修改 `demo-med` 等目标租户用户的角色。

**v0.2 修正「不对称」的解读**：这张表读起来像「同一操作者在两个页面能力不一致」，
但两列的**操作者根本不是同一个人**——左列调用者=本租户用户（含租户管理员），右列调用者=平台 `super_admin`。
把「平台管理员不能替他租户改角色」当成缺陷，等价于要求平台持有「给任意租户授予任意权限」的根能力，
正是 TUM-7 拒绝的东西。**真正的不对称只有一处值得追问**：服务商（provider）对其**客户租户**的成员
能否调整角色 —— 见 §5 与 `07-known-gaps.md` **G15**。

### 3.6 职责边界：谁在什么范围内可以改角色（v0.2 新增，源码验证）

| 操作者 | 范围 | 通道 | 护栏（源码） |
|---|---|---|---|
| 租户管理员（`users.role=admin`，rank 4） | **仅本租户** | `PUT /api/v1/users/:id` 传 `role`（主角色）与 `roleIds`（RBAC 多选） | ① 主角色 `roleRank(target) > roleRank(caller)` → 403（`handlers/user/handler.go:363-368`）② `roleIds` 走 `UserService.CanGrantRoles`（`service/user_service.go:193-241`）三重校验：角色必须属本租户、rank ≤ 调用者、平台/MSP 专属角色不得外授 ③ 主角色变更后吊销存量 token |
| 租户管理员 | **仅本租户** | `POST/PUT /api/v1/roles`、`POST /roles/:id/permissions`（`common_system_routes.go:308-312`） | 角色行天然带 `tenant_id`，作用域即租户；`role:write` 为租户内码 |
| 平台 `super_admin` | 跨租户**建号**时可指定角色 | `POST /api/v1/tenants/:id/users` + `GET /api/v1/tenants/:id/roles`（可授角色清单） | 三通道 + 角色白名单 + rank 上限 + `tenantctx.WithProvisioningBypass` + 配额 |
| 平台 `super_admin` | 跨租户**改角色** | **无端点** | 即 TUM-7 冻结项；本评审确认无隐藏入口、无半成品代码 |
| 服务商 provider 员工 | 客户租户成员的角色 | **无平台面/跨客户通道**（MSP 面同样只有建号与工作台） | **G15** 的真实空洞所在（解除条件见 `plan §10`） |

**结论**：「租户如何管理用户权限」有明确答案 —— **由该租户自己的管理员在本租户内自助完成**，
这不是缺失的兜底方案，而是现行架构的主干道；跨租户方向上平台刻意只提供「供给 + 账号治理」，不提供「权限授予」。

---

## 4. 缺口清单

> **编号变更（v0.2）**：v0.1 的 `G1–G6` 与 `07-known-gaps.md` 的**唯一 G 空间**（G1–G11，见该页 §编号说明）
> 以及 `plan` 的 `TUM-G1–G4` 三重冲突。本表改挂 TUM 空间，续号为 **TUM-G5–TUM-G12**；
> 治理侧另给 `07` 的 **G12–G15**（G12/G13 缺陷已修、G14 待接线决策、G15 边界口径），两套编号在最后一列互相指向。

| 编号 | 缺口 | 证据 | 影响等级 | 治理登记 |
|---|---|---|---|---|
| **TUM-G5** | 平台面抽屉无单用户角色调整入口（无菜单项、无弹窗、无内联编辑） | 行操作仅三项；`TenantUsersDrawer.tsx` 操作列 | ~~P1~~ → **P2（边界决策，非缺陷）** | G15（口径） |
| **TUM-G6** | 前端无跨租户角色写 API 方法与类型 | `tenant-user-admin-api.ts` 方法全集 | P2（同上） | G15 |
| **TUM-G7** | 后端无跨租户角色写接口与 DTO | `tenant_user_admin_dto.go`；路由表 `common_system_routes.go:366-374` | P2（同上） | G15 |
| **TUM-G8** | 无「角色变更」审计事件与稳定错误码体系 | 后端 12 码 / 前端 10 码，均无角色项；仅复用 `target_user_id` 审计列 | P2 | G15 |
| **TUM-G9** | 最后管理员护栏未覆盖「角色降级」 | 护栏判定仅 `active` 维度；`LAST_ADMIN_PROTECTED` 语义仅停用 | 仅当 G15 解除后才是 P1 | G15（附条件） |
| **TUM-G10** | **前端错误码词表与后端常量表漂移**（membership 两码缺映射，真实用户可见英文原文） | `tenant-user-admin-api.ts` 10 项 vs `service/tenant_user_admin.go` 12 项 | **P2 缺陷** | **G12 ✅ 本批已修复** |
| **TUM-G11** | **「最后管理员」护栏按当页 items 估算**，管理员跨页分布时假性禁用开关 | 原 `TenantUsersDrawer.tsx` `activeAdminCount = items.filter(...)`（`PAGE_SIZE_DEFAULT=20`，可选 10/20/50/100） | **P2 缺陷** | **G13 ✅ 本批已修复** |
| **TUM-G12** | membership 撤销面**后端已实现、前端零接线**，且「该通道只走 API/CLI」的口径未成文 | `RevokeMembership` + 路由 `:372` 存在；`itsm-frontend/src` 零引用 | **P2 缺陷** | **G14** |

### TUM-G9 特别说明（若将来解除「不做」，这是最容易被漏掉的安全项）

现有护栏逻辑是「同租户内活跃 admin 计数 ≤ 1 时禁止停用」。若仅新增角色调整而不扩展护栏，就会出现等价绕过：

> 把某租户**唯一管理员降级为最终用户** → 该租户彻底失去管理入口，效果等同于停用最后管理员。

因此角色调整的护栏必须是**双维度**：停用（已有）+ 降级出 `admin`（新增），且需同时给出 `LAST_ADMIN_PROTECTED` 语义扩展或独立错误码（如 `LAST_ADMIN_ROLE_PROTECTED`；`HOME_MEMBERSHIP_IMMUTABLE` 的 409 形态可直接参照）。前端需同步禁用按钮并给出解释文案。**本批不实现**，仅保留为解除冻结时的必做前置。

---

## 5. 业务影响场景（不做该能力时的真实代价）

v0.1 把五条场景全部记为「只能离职重建/无法降权」，**其中三条经复核不成立**——
它们发生在**单一租户内**，该租户管理员用 `PUT /api/v1/users/:id` 即可完成，历史工单归属、
审计连续性、通知订阅**都不受影响**（主角色变更仅吊销 token，不动账号身份）。

| 场景 | v0.1 判断 | v0.2 复核结论 |
|---|---|---|
| 1. 转岗/升职（医院信息科「最终用户」→ 服务台坐席） | 「唯一路径是停用旧账号 + 重新建号」 | ❌ **不成立**。本租户管理员在 `系统管理 → 用户管理` 改主角色/RBAC 角色即可；目标角色 rank ≤ 操作者（admin=4）时全部可授 |
| 2. 临时授权后降回（科室主任暂代 IT 管理员） | 「无法降权，只能整号离职」 | ❌ **不成立**。降权同为 `PUT /users/:id`；`roleRank` 只限制**授予高于自身**的角色，不限制下调 |
| 3. 安全事件降权（外包转岗、权限过高） | 「无法只降角色」 | ❌ **不成立**，同上；且变更会吊销存量 token，旧权限立即失效 |
| 4. **MSP 交付一致性**：服务商替换**客户租户**的管理员 | 缺口外溢为人工运维工单 | ✅ **成立，且是唯一结构性空洞**。provider 对客户租户成员既无平台面角色通道，MSP 面也只有建号/工作台（G15） |
| 5. 合规审计「权限变更不可追溯」 | 因无该动作而空白 | ⚠️ **部分成立**。跨租户方向确实无该动作；**本租户方向有留痕**（`UserService` 更新链路 + RBAC 面审计），空白期不等于全系统空白 |
| （v0.1 未列）**目标租户已无可用管理员** | — | ✅ **成立**，但归属另一通道：首管窄通道重置（`is_bootstrap_admin`，不受写开关约束）+ 服务器侧 `cmd/reset_password`，属**恢复**而非**授权** |

**净代价**：v0.1 的「产品能力缺口」应收敛为一条 —— **平台/服务商替他租户调整权限的运维动线缺失**，
其现实替身是「让人去目标租户登录管理员账号操作」或「SQL 直改 `users.role` / `user_roles`」。
后者无 rank 校验、无角色归属校验、无审计、不吊销会话，**这才是真正的风险来源**，
也正是应通过 G15 口径 + 运维 SOP 收口的对象，而不是通过新建跨租户端点收口。

---

## 6. 建议方案

v0.1 的「至少补 P1 单用户角色调整」建议**在 v0.2 撤回**：那等于用工程手段推翻一条已冻结的产品边界，
而它声称要解决的三条场景（§5 表 1/2/3）本来就有本租户内通道。

### 6.1 本批应做（已随本评审执行）

| 项 | 动作 | 落点 |
|---|---|---|
| G12 | 前端错误码词表补齐 membership 两码，并加**同源断言**防回归 | `tenant-user-admin-api.ts` `TENANT_USER_ADMIN_ERROR_MESSAGES` + `__tests__/tenant-user-admin-api.test.ts` |
| G13 | 「最后管理员」计数改**服务端租户级权威** `activeAdminCount`；前端不再按当页估算（仅字段缺失时回退） | `dto.TenantUserListResponse` + `service.ListUsers`（计数刻意不带筛选/分页）+ `TenantUsersDrawer` 乐观更新与回滚同步计数；后端断言「筛选/分页不收敛计数」，前端断言 `activeAdminCount=2` 时当页唯一管理员**仍可停用** |
| G14 | membership 撤销面**不擅自接 UI**（属产品决策）；先在 API 客户端注释与本评审 §3.2 成文「当前仅 API/CLI 通道」，是否接抽屉交评审 | `tenant-user-admin-api.ts` 注释 + `07-known-gaps.md` G14 |
| G15 | 登记**职责边界口径**：角色变更由目标租户管理员自助；平台/服务商跨租户改权限需另案（附 `plan §10` 两项前置） | `07-known-gaps.md` G15 + 本评审 §3.6；**已同步运维侧入口** `07` 同级文档 `06-verification-and-troubleshooting.md` §3 排查表（含「禁止 SQL 直改」红线） |
| G16 | 执行期新发现：TUM 平台面 7 端点**零 Swagger 注解** → 新契约字段在 OpenAPI 不可见；`itsm-backend/docs/*` 已回滚，注解补齐另批处理 | `07-known-gaps.md` §17（含建议的契约可见性门禁） |
| 治理 | 本评审在 `docs/review/README.md` 登记表挂账（该页 §登记表 要求新报告一律登记） | `docs/review/README.md` |

### 6.2 若评审决定解除 TUM-7：原 v0.1 设计（保留为参考，**非本批**）

**解除前必须先解决的三件事**（缺一即退回「不做」）：

1. **平台级角色词表**收敛（`plan §10` 依据②）：当前 `roles` 是租户内语义，`CanGrantRoles` 明确拒绝跨租户角色；
   跨租户授予需要先定义「平台可代授的角色集合」及其与租户 rank 词的映射。
2. **变更审批 / 双人复核**（依据③）：单点即可提权的动作必须有复核闭环。
3. **`mspRole` 从首批范围剔除**：它牵动 G11 的四处同源（`internal/authz/roles.go` 组合角色派生、
   `pkg/bootstrap/admin.go` 首管策略、`middleware.RoleRank` + 菜单提权名单、`pkg/seeder` 角色种子）。
   只改 `mspRole` 而不联动并集，会直接复现「服务商管理员改完看不见 ITSM 菜单」的 G11 回归。
   首批安全子集限于：主角色 `role` + 服务端租户级双维度护栏 + 变更后吊销会话 + 审计。

**可复用的实现范式**：MSW-3 的 `RevokeMembership` 已是同形态模板 —— 平台面硬判定 + 写通道开关 +
租户级护栏 409（`HOME_MEMBERSHIP_IMMUTABLE`）+ `target_user_id` 审计 + 租户通知，照此实现即可，无需新范式。

**接口**

```
PUT /api/v1/tenants/:id/users/:userId/role
Body: { "role": "agent", "roleIds": [3,7], "mspRole": "provider_admin" }
权限: tenant:write（路由粗筛）+ 服务层 super_admin 硬校验（沿用 TUM-D1）
```

- 字段语义与平台侧 `updateUser` 对齐，避免第二套角色模型。
- 复用 `CanGrantRoles` 的 `rank` 上限与 `grantable` 过滤，禁止越权授予。
- 会话策略：主角色或 RBAC 角色变更后**吊销该用户存量会话**（与停用一致），避免旧 token 携带旧权限；这是权限变更的必要闭环。
- 幂等：重复提交相同角色返回 200 并记录 no-op。

**护栏（必须与接口同批）**

- 降级判定：目标当前 `role === 'admin' && active`，且同租户活跃 admin 计数 ≤ 1 → 409 `LAST_ADMIN_ROLE_PROTECTED`。
- 目标为 `is_bootstrap_admin` 时禁止降级（保持首管可恢复性），复用 TUM-D3 首管窄通道思路。
- 禁止对自身降级（`SELF_OPERATION_FORBIDDEN` 语义扩展）。

**审计**

- 事件 `user.platform_role_change`，写 `target_tenant_id` + `target_user_id` + `before`/`after`（角色与 roleIds 差异）。复用已存在的 `target_user_id` 审计列（TUM-3），无需新迁移。

**前端**

- 行操作新增 `调整角色`（`调整角色` 与其他三项同级，`强制下线`/`离职` 之间不需插入）。
- 弹窗：主角色 Select（`PRIMARY_ROLE_OPTIONS`）+ RBAC 角色多选（过滤 `grantable`）+ 差异预览 + 二次确认。
- 护栏：`isSelfOrLastAdmin` 条件下按钮禁用并 Tooltip 说明；后端 409 兜底。
- 成功后刷新行数据，不整表重载。

### 6.3 P2（本批不做，仅登记）

- **批量角色调整**：依赖后端批量端点（当前批量本身为逐条串行聚合），建议与批量端点一批做。
- **跨租户角色模板/审批流**：规划文档已给触发条件（平台级角色词表 + 变更审批/双人复核），维持不做。

### 6.4 不建议的做法

- ❌ 直接复用 `POST /users/:id`（建号/用户更新）打通跨租户：绕过平台面判定与 `tenantctx` 重绑定，构成跨租户越权。
- ❌ 仅加前端下拉、不加服务端硬校验：`tenant:write` 租户管理员同样持有，必然越权。
  （**v0.2 注**：2026-10-10 治理域收口 D7 后该前提已变——租户 `admin` 不再持 `tenant:*`（§3.4）；
  但「路由权限不可单独作依据」仍成立，服务层 `super_admin` 硬校验照旧必须。）
- ❌ 只做角色变更、不做会话吊销：旧 token 仍带旧权限，等于变更无效。
- ❌ **用 SQL 直改 `users.role` / `user_roles` 作为跨租户调权限的常规手段**：无 rank 校验、无角色归属校验、
  无审计、不吊销会话 —— 这是当前最可能发生、也最该由 G15 口径 + 运维 SOP 收口的实际风险面。
- ❌ 用「新增跨租户角色端点」来回应本批的三项契约/UX 缺陷（G12–G14）：两者无关，混做会把边界决策拉进缺陷修复批次。

---

## 7. 验收建议（TUM-A 系列追加）

| 编号 | 用例 | 期望 |
|---|---|---|
| TUM-A9 | 平台管理员将 `demo.med.user`（最终用户）改为服务台坐席 | 200；列表角色即时更新；审计含 before/after；目标用户旧会话失效 |
| TUM-A10 | 将租户唯一活跃 admin 降级 | 409 `LAST_ADMIN_ROLE_PROTECTED`；角色不变 |
| TUM-A11 | 降级首管账号 | 409/403；角色不变 |
| TUM-A12 | 对自身降级 | **409** `SELF_OPERATION_FORBIDDEN`（v0.2 校正：既有自操作拒绝即为 409，见 `service/tenant_user_admin.go` `SetActive` 的 `TenantUserAdminCodeSelfOperation`；v0.1 写 403 与先例冲突） |
| TUM-A13 | 租户管理员（非 super_admin）调用 | 403 `PLATFORM_SCOPE_REQUIRED` |
| TUM-A14 | 授予 rank 高于调用者的 RBAC 角色 | 403/400；不落库 |
| TUM-A15 | 提交与现值相同的角色 | 200 no-op；不产生虚假审计变更记录 |

> **v0.2 状态**：本节 A9–A15 全部随 §6.2 一起**挂起**（TUM-7 维持不做），不进本批。
> 本批实际落地的用例是护栏计数两项（后端「筛选/分页不收敛计数」+ 前端「`activeAdminCount=2` 仍可停用」）
> 与 membership 错误码同源断言（见 §6.1）。
> 另补：`TenantUsersDrawer` 前端用例（按钮可见性/护栏禁用/409 文案）、`tenant_user_admin_dto` 契约用例、
> 路由级 e2e（沿用 `router/tenant_user_admin_e2e_test.go` 范式）——待解除冻结时一并做。

---

## 8. 风险与回滚

| 风险 | 缓解 |
|---|---|
| 角色降级导致租户失去管理入口 | 双维度护栏 + 前端禁用 + 409 兜底（三重） |
| 越权：租户管理员借该接口跨租户提权 | 服务层 `super_admin` 硬校验（TUM-D1 范式），路由权限不可单独作依据 |
| 授予超 rank 角色 | 复用 `CanGrantRoles` 与 `grantable` 过滤 |
| 旧会话携带旧权限 | 变更后强制吊销存量会话 |
| 与平台侧 `updateUser` 双写不一致 | 抽公共角色校验服务，两处共用同一 `rank`/`grantable` 规则 |
| 审计噪声 | 幂等 no-op 不记变更；仅记真实差异 |

**开关**：建议挂 `TENANT_USER_ADMIN_ENABLED` 同一开关（当前生产默认关，见 TUM-7 v0.12），随该通道灰度。

**v0.2 本批改动的回滚面**（与上表无关，单独列）：

| 改动 | 回滚方式 |
|---|---|
| `activeAdminCount` 响应字段 | **纯增量**：前端在字段缺失时自动回退当页估算，回滚只需撤字段（老前端不受影响）；无 DDL、无迁移 |
| membership 两码前端文案 | 撤 2 个 map 条目即可；有断言保护，撤后测试即红 |

---

## 9. 结论与待决策点

v0.2 复核后的结论替换 v0.1 的四条（v0.1 判定方向部分错位，保留在 §5 对照表可追溯）：

1. **确认（不变）**：**跨租户**角色调整在前端入口、前端 API、后端接口三层同时缺失，**不是渲染层缺陷**，
   运行时也无隐藏入口或半成品残留。
2. **确认（不变）**：该能力此前已被 `plan §10` TUM-7 显式登记为 P2「不做实现」，是范围决策不是回归。
3. **修正（v0.1 判定的关键偏移）**：「角色调整」在**本租户内**是完整可用的产品主干道
   （`PUT /users/:id` 主角色 + `roleIds`，`CanGrantRoles` 三重护栏，`roles` CRUD），
   因此 v0.1 的「只能离职重建、历史归属断链」不成立（§5 场景 1/2/3 全部有正规通道）。
   真正的空洞只有两条：**MSP 服务商跨客户调权限**（场景 4）与**目标租户已无可用管理员**（走恢复通道，非授权通道）。
4. **修正（论据时效）**：§10「不做」依据①所依赖的「租户管理员也持 `tenant:write`」已被 2026-10-10 D7 收口推翻；
   「不做」的现行支撑是依据②③（平台级角色词表未收敛、无变更审批/双人复核）。**结论不变，理由要换**。
5. **决策落点（本批按此执行）**：
   - **维持 TUM-7 不做**，不新建 `PUT /tenants/:id/users/:userId/role`；
   - 登记 **G15 职责边界口径**（含运维话术），替代「人工工单外溢」；
   - 修掉与「做不做角色调整」无关却真实伤人的三项：**G12 错误码漂移（已修）**、
     **G13 护栏按当页估算（已修）**、**G14 membership 撤销面无 UI 且口径未成文（待接线决策）**。
6. **仍待评审**：G14 是否在租户用户抽屉加「撤销成员映射」行操作（纯前端接线，后端已就绪）；
   G15 若因 MSP 场景升级为需求，必须先满足 §6.2 的三项前置，并**先解决 G11 四处同源**。

---

## 附：本次检查未覆盖的部分

- 未验证 `TENANT_USER_ADMIN_ENABLED` 关闭态下抽屉的写操作表现（本次为开启态实例）。
- **v0.2 已补**：MSP 服务商自服务面（`provider` 维度）**确认无角色调整通道** —— MSP 面只有建号（`ProvisionUserToTenant`
  三通道之一）与工作台/跨客户视图，无角色写端点；与平台面口径一致（都是「可供给、不可授权」）。
  残留未验证项：provider 是否**应该**具备跨客户授权能力（属 G15 的产品决策，非本评审范围）。
- **v0.2 已执行代码变更与测试**：G12/G13 已落地；
  验证范围 `go build` / `go vet`（dto+service）通过、`go test ./service/ -run TestTenantUserAdmin_ListUsers|SetActiveGuards` 通过、
  前端 `tsc --noEmit` exit 0、`jest TenantUsersDrawer tenant-user-admin-api` **18/18 通过**、
  `eslint` 4 个改动文件 0 problem、`bash scripts/docs-gate/run-all.sh` **6 gates / 0 failed**。

**⚠️ 残项 1 —— 两个后端 suite 仍未执行（本机 + 远端皆不可得）**

`./handlers/tenant/`、`./router/` 在本机**构建期**即失败，未进入任何用例：
`compile.exe: fatal error: runtime: cannot allocate memory`（GC worker 无法分配）。已尝试并全部失败：
`-p 1`、`GOMAXPROCS=1`、以及本仓自用的低内存写法 `-gcflags=all=-c=1`（见 `plan` v0.10 证据行）；
本机空闲物理内存约 3.7 GB。原计划在 238 上取 HEAD 基线，但远端 shell 命令白名单**不含 `go`**
（`COMMAND_DENIED: command "go" is not in the allowlist`），无法执行。
**判定**：失败发生在依赖包 `ent/schema` 编译/link 阶段，与本批两行改动（结构体加字段 + 一次 Count 查询）无关，
但 `service` 绿**不能替它们背书** —— 需在内存充足的工作站补跑 `make test-backend`
或 `go test ./handlers/tenant/ ./router/` 后方可合入。

**⚠️ 残项 2 —— 浏览器实测未做**：护栏跨页场景（同一租户把 pageSize 调至 10，管理员分布多页时开关不应再灰）
需在 238 live 走查确认；本机无实例凭据。

**⚠️ 新发现（执行期）—— G16**：曾按建议执行 `make swagger-gen` 想把 `activeAdminCount` 写进 OpenAPI，结果：
① `handlers/tenant/user_admin.go` **7 个端点全部零 swagger 注解**，`TenantUserListResponse` 在 swagger 中 0 次出现，
所以刷新对本批字段**毫无记录作用**；② 刷新同时带入他人先前 commits 的 swagger 漂移（单文件 +182 行）。
据此**已回滚 `itsm-backend/docs/*`**，并把「TUM 平台面端点 OpenAPI 不可见」登记为 `07` **G16**（§17），
建议单独一批补注解后再生成。这条也反向解释了 G12 为什么能长期存在：**该通道没有任何机器可校验的契约面**。

---

**文件信息**

| 项 | 值 |
|---|---|
| 创建日期 | 2026-11-06 |
| 检查对象版本 | 运行实例 `172.18.3.238:8089`（2026-11-06 时点） |
| 关联文档 | `docs/multi-tenant/plan/msp-tenant-user-management-enhancement-plan.md`（TUM-1–TUM-7）、`docs/multi-tenant/07-known-gaps.md` |
| 治理登记 | G12 / G13 / G14 / G15 / G16 → `docs/multi-tenant/07-known-gaps.md`（§15–§17）；本文件已登记 `docs/review/README.md` |
| 后续动作 | G14 接线决策 + G15 运维 SOP 文案评审；TUM-7 维持不做，解除需先满足 §6.2 三项前置 |

## 修订记录

| 版本 | 日期 | 变更 |
|---|---|---|
| v0.1 | 2026-11-06 | 首版：页面走查 + 三层契约核对，判定「角色调整能力缺口」，建议补 P1 单用户角色调整 + 双维度护栏 |
| v0.2 | 2026-11-06 | **复核修订**（按当前源码逐条重验 + 决策校正）：① §3.2/3.3 补第 7 个写通道 membership/revoke 与「后端已建、前端零接线」口径；② 错误码数量由「9 项」改为「前端 10 / 后端 12」并升级为契约漂移缺陷（G12）；③ §3.4 标注 `plan:18`/TUM-D1 的 `tenant:write` 论据已被 D7 收口推翻；④ 新增 §3.6 职责边界表（租户管理员自助为主通道）；⑤ §4 编号 G1–G6 → TUM-G5–G12（消 G 空间冲突）并新增 TUM-G10–G12；⑥ §5 场景 1/2/3 判为不成立、空洞收敛为 MSP 跨客户 + 无可用管理员；⑦ §6.1 撤回「本批做角色调整」，改为 G12–G15 处置；原设计降级为 §6.2 参考方案并加三项前置（含 `mspRole` 剔除，避 G11 回归）；⑧ §7 TUM-A12 403 → 409；⑨ §8 补本批回滚面；⑩ 落地 G12/G13 修复与用例，登记 known-gaps 与 review README |
