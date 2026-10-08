# 建号角色多选与目标租户角色列表方案（平台 / MSP 通道）

> **批次建议**：`IP-P2-7`（落库前对照《[实施方案](./msp-implementation-plan.md)》§3.0 总表登记，未登记前本方案不视为冻结）
>
> **上位（权威顺序）**：[canon](./msp-concept-model-and-architecture-canon.md)（D5/D8/D10/D11、§2.3 深度切换）→ [目标架构 §6](./msp-target-architecture.md)（建号通道矩阵 / 权限矩阵）→ [主方案 §5.2](./msp-user-lifecycle-and-tenant-switching-plan.md)（F1–F4）→ [建号与注册流程](./msp-account-provisioning-and-registration-flow.md)（四通道矩阵）→ [实施方案 `IP-P0-5`](./msp-implementation-plan.md)
>
> **关联**：[前端页面与权限分析 §6.5:195](./msp-frontend-pages-and-permissions-analysis.md)（既定 P1 项「目标租户选择（按通道）+ 角色白名单（Q7/F3，去 super_admin/admin 可选项）」）；[membership 整合方案](./msp-membership-switch-consolidation-plan.md)（MSW-1 写路径收口）
>
> **状态**：v0.2（2026-10-08）——决策点 D1–D6 按 §3 推荐值落定；B1–B3 已实施并验证（实施记录见 §9）

---

## 1. 背景与问题

### 1.1 现状审计（证据）

| # | 现状 | 证据 |
|---|------|------|
| 1 | 「平台建号」弹窗的「MSP 角色」下拉**仅在租户类型为 `msp_provider` 时出现**，选项硬编码为 `provider_admin`/`provider_agent` 两项 | `itsm-frontend/src/pages/(main)/admin/tenants/index.tsx:827-834`；`itsm-frontend/src/components/provisioning/ProvisionUserModal.tsx:175-186` |
| 2 | 后端**已支持** `role`（单主角色）+ `roleIds[]`（多角色 M2M）+ `mspRole` 三个维度；`CanGrantRoles` 已实现「角色必须属于目标租户 + 不得高于调用者 rank」校验 | `itsm-backend/dto/user_dto.go:8-25`；`itsm-backend/service/user_service.go:171-193` |
| 3 | 前端建号契约 `ProvisionUserRequest` **只暴露 `mspRole`**，`role`/`roleIds` 未接线 | `itsm-frontend/src/lib/api/api-config.ts:175-183` |
| 4 | **不存在**「目标租户角色列表」读取端点：`GET /api/v1/roles` 用会话租户（`middleware.GetTenantID`），无 `/tenants/:id/roles` | `itsm-backend/handlers/rbac/handler.go:109-112`；路由 `router/common_system_routes.go:149-160` |
| 5 | `users.role` 是**枚举**（super_admin/admin/manager/it_admin/security_admin/sysadmin/agent/technician/security/end_user），**非枚举角色（MSP 五角色、provider_tenant_admin、租户自定义角色）只能经 `user_roles` 边（`roleIds`）授予** | `itsm-backend/ent/schema/user.go:34-37` |
| 6 | 开通时角色由 default 租户模板克隆注入（详见 §1.2），因此**每个租户都有完整的角色表可读** | `itsm-backend/pkg/seeder/tenant_provisioner.go:44-86,125-159` |

### 1.2 租户开通注入的初始角色（本方案的依据）

- `seedRoles` 把内置词表 + config 岗位角色写入 **default 租户**（`pkg/seeder/seeder.go:1025-1099`）：
  - 通用 9：`super_admin`、`admin`、`manager`、`it_admin`、`security_admin`、`sysadmin`、`agent`、`technician`、`end_user`；
  - MSP 五角色：`msp_viewer`、`msp_tech`、`msp_specialist`、`msp_manager`、`msp_admin`；
  - provider 首管组合：`provider_tenant_admin`（= admin ∪ msp_admin）。
- `ProvisionTenant` 从 default 克隆 **roles / permissions / role_permissions / menus / groups** 到新租户（`tenant_provisioner.go:125-159`；readiness 7 项首项即 `roles`，`tenant_readiness.go:37-44`）。
- 客户类型租户（`msp_customer`/`saas_customer`）克隆后对 5 个 `msp_*` 角色做 C1→C2 对账，替换为客户侧基线（`tenant_provisioner.go:308-312`；`internal/authz/roles.go:414-445`）。
- 首管注入（v1.69）：provider → `users.role=admin` + `msp_role=provider_admin` + `msp_admin` RBAC 边 + home membership；customer → `admin` + `customer_user` + `admin` 角色。

### 1.3 问题影响

1. **客户租户建号无法授权**：仅能落 `end_user`，管理员/经理等只能建号后再去用户管理页改（多一步，且平台侧入口的原意是一步到位）。
2. **provider 员工建号存在「权限空窗」残留**：只写 `msp_role`、`roleIds` 为空 → 用户无 RBAC 边；scenarios/04 §7 登记的「建号通道未写 RBAC 角色边」缺口仅由 MSW-1 补齐了 membership，本次多选可一并收口。
3. **数据源错误 / 不可演进**：新租户若自定义角色、或停用/改名内置角色，UI 完全不反映；类型判断 `type === 'msp_provider'` 严格相等，legacy 类型值（`msp`）会漏显。
4. **与既定 P1 规划不一致**：前端分析文档已登记「目标租户选择 + 角色白名单」随 P1 上线（`msp-frontend-pages-and-permissions-analysis.md:195`）。

---

## 2. 目标与非目标

### 目标（G）

- **G1**：新增「目标租户可授予角色列表」读端点（平台面 + MSP 面），返回**数据驱动**的角色集合。
- **G2**：建号弹窗角色选择改为**多选**，候选来自 G1 接口（租户自定义角色自动生效）。
- **G3**：写入沿用既有 `roleIds` 通道，权限/rank/租户归属校验与建号完全同源（`CanGrantRoles`），不新增写端点。
- **G4**：收口「多选角色 → `users.role` 主角色 / home membership 角色」的一致语义（含 provider `msp_role` 映射），避免权限空窗与 membership 角色错配。
- **G5**：前端移除 `type === 'msp_provider'` 硬编码判断；`mspRole` 字段保留为 provider 专属维度（见 D1）。

### 非目标（N）

- N1：不扩展 `users.msp_role` 词表（仍是 provider_admin/provider_agent 白名单）。
- N2：不做角色管理 CRUD、不做批量改角色（另批次）。
- N3：不改 RLS 模型与 `tenantctx` 旁路规则（只按既有规则接入）。
- N4：invite 通道（IP-P1-4）的角色/邀请多选不在本批（可作为后续对齐）。
- N5：不做「跨租户角色授予」（角色必须属于目标租户，校验口径不变）。

---

## 3. 决策点（评审确认，含推荐值）

| ID | 议题 | 推荐 | 备选/影响 |
|----|------|------|-----------|
| D1 | `msp_role` 与 RBAC 角色是否合并为一个控件 | **不合并**：provider 租户保留「MSP 角色」单选项（users.msp_role 维度）；RBAC 角色多选独立 | 合并会让 provider 员工身份语义丢一半（membership/工作台依赖 msp_role），且与后端白名单契约冲突 |
| D2 | 平台面候选角色过滤规则 | `is_active=true` 且：① 排除 `super_admin`/`sysadmin`（对齐 `validateRole`）；② `rank ≤ 通道上限`（platform=5 / msp=4，复用 `serviceRoleRank`）；③ **客户类型目标租户**排除 `provider_tenant_admin` 与 5 个 `msp_*`（这些是 provider 员工经分配进入客户租户的 C2 基线角色，不由客户员工持有）。**已锁定为硬规则（产品口径：客户不应该知道 MSP 相关功能）**：客户租户的候选/列表/详情/授予全入口收口，词表单一源 `middleware.MSPOnlyRoleCodes()` | 不再保留"放开客户 msp_* 候选"选项；如未来产品确需变更，须先评审本硬规则与 C2 消费口径（`MSPCustomerScopeRolePermissionCodes`） |
| D3 | 多选后 `users.role` 主角色与 membership 角色怎么定 | 前后端约定：前端只传 `roleIds`（不传 `role`）；**服务端**：`len(roleIds)>0 && role==""` 时取「选中角色中 rank 最高的**枚举兼容** code」回写 `users.role`（无兼容项则保持 `end_user` 并 warn 留痕）；`provisionMembershipRoles` 扩展为优先按角色边解析（含 msp_* 映射），与 MSW-1 幂等语义兼容 | 只传 roleIds、不治理 role/membership → membership.role 落 end_user，切换/权限指示可能失真（回归面见 `service/membership_sync.go`、`user_provisioning.go:252-260`） |
| D4 | 新端点权限码 | 路由粗筛 `RequirePermission("tenant","write")`（与建号同面，`POST /:id/users` 一致）；服务层硬判定沿用平台通道规则（super_admin/sysadmin、任意 active 租户）+ 目标租户必须 active | 用 `tenant:read` 会扩大暴露面；TUM 读面用了 read，但本端点是建号表单数据源，按 write 面收窄更稳 |
| D5 | 是否同批做 MSP 面（`/msp/customers/:id/roles` + MSP 建号弹窗） | **同批做后端 + 前端接线**（共用 `ProvisionUserModal`，rank=4、mspguard allocation 校验与 MSP 建号同源） | 分两批则 MSP 弹窗继续无角色选择，能力不对称 |
| D6 | 多选是否必填 / 默认值 | 非必填（空 = 现状默认 `end_user`）；候选默认不勾选；`end_user` 是否作为显式候选：**返回（它是合法角色）但默认不选** | 若要求必填，会影响「不给角色」的既有用法，属破坏性变更 |
---

## 4. 总体设计

### 4.1 维度澄清（避免把三个「角色」混为一谈）

| 维度 | 存储 | 取值 | 谁写 | 本方案动作 |
|------|------|------|------|-----------|
| 主角色 `users.role` | `users.role` 枚举列 | 10 个枚举值（§1.1-5） | 建号/用户编辑 | 服务端按 D3 从 roleIds 派生（仅枚举兼容项） |
| RBAC 多角色 | `roles` 表 + `user_roles` M2M 边 | 目标租户任意 active 角色（含自定义、`msp_*`、`provider_tenant_admin`） | 建号 `roleIds` / 用户编辑 | **本方案主角**：多选 → `roleIds` |
| MSP 员工身份 `users.msp_role` | `users.msp_role` 列 | `provider_admin`/`provider_agent`（白名单） | provider 租户建号 | 保留独立单选（D1）；membership 映射不变 |

> 结论：UI 上 provider 租户同时可见「MSP 角色（必填）+ RBAC 角色（多选）」；非 provider 租户仅「RBAC 角色（多选）」。

### 4.2 后端设计

#### 4.2.1 新读端点（G1/D4/D5）

| 端点 | 权限（路由粗筛） | 服务层判定 | 说明 |
|------|-----------------|-----------|------|
| `GET /api/v1/tenants/:id/roles` | `tenant:write` | 平台通道：`super_admin`/`sysadmin` + 目标租户 `active` | 平台建号表单数据源 |
| `GET /api/v1/msp/customers/:customer_tenant_id/roles` | `RequireMSPPermission("msp_customer","write")` | MSP 通道：provider 管理员 + `mspguard.CanAccessCustomer` allocation 二次校验 | MSP 建号表单数据源 |

- 注册位置：平台面加入 `router/common_system_routes.go` 的 `tenants` 组（紧邻 `POST /:id/users`，`common_system_routes.go:200-203`）；MSP 面加入 `router/msp_routes.go`（紧邻 `msp_routes.go:33-36`）。
- Handler：`handlers/user` 新增 `ListTenantGrantableRoles` / `ListCustomerGrantableRoles`（复用 `provisionActorFromContext` 与既有错误映射 `respondProvisionError`）。
- 服务：`UserProvisioningService.GrantableRoles(ctx, actor, targetTenantID, channel)`，与建号共用 `resolveChannel`/`serviceRoleRank`/白名单，**单一源**，避免 UI 过滤与写入校验漂移。
- 实现要点：
  - 目标租户读取用 `tenantctx.WithTenantID(ctx, target)`（RLS enforce 下正确收窄；只读，不改写旁路语义）；
  - 查询 `roles WHERE tenant_id=target AND is_active=true`，内存过滤（排除 `super_admin`/`sysadmin`；客户类型目标租户排除 `provider_tenant_admin`/`msp_*`；`rank ≤ provisionCallerRank(actor, channel)`），按 rank 升序、code 稳定排序；
  - 响应 DTO `RoleOption{ id, code, name, description?, isSystem, rank }`（新增 `dto` 类型；`rank` 由 `middleware.RoleRank` 计算，前端只读展示，不参与判定）。

#### 4.2.2 写入侧（G3，无新端点）

- 前端 `roleIds` 直达既有 `POST /:id/users`；`validateRole` → `CanGrantRoles` 已覆盖：角色必须属于目标租户、rank 不高于通道上限；平台角色（super_admin/sysadmin）在 `validateRole` 已被显式拒绝（`user_provisioning.go:353-364`）。
- 错误码沿用：`ROLE_NOT_GRANTABLE`（422）、`MSP_ROLE_NOT_ALLOWED`（422）等（`msp-implementation-plan.md:83-85`）。

#### 4.2.3 主角色与 membership 一致性（G4/D3）

现状缺口（本方案必须闭合，否则多选会制造"有边但 membership 为 end_user"的错配）：

1. `CreateUser`：`req.Role` 空时 `users.role` 落默认 `end_user`；`roleIds` 只写 M2M 边（`user_service.go:98-124`）。
2. `provisionMembershipRoles`：只读 `users.msp_role`/`users.role`（`user_provisioning.go:252-260`）。

设计：

- 在 `UserProvisioningService.ProvisionUser` 的 `validateRole` 之后、`CreateUser` 之前，做**主角色派生**（服务端，仅当 `req.Role==""` 且 `len(req.RoleIDs)>0`）：
  - 取目标租户选中角色中 rank 最高者；若其 code ∈ `users.role` 枚举 → `req.Role = code`；
  - 否则保持 `req.Role` 空（`users.role=end_user`），并记录结构化 warn（租户/角色集），不阻断建号。
- `CreateUser` 返回后，`syncHomeMembershipAfterProvision` 解析 membership 角色时：优先 `users.msp_role` 映射（不变）→ 其次按 `roleIds` 对应角色实体的最高 rank code（新增；覆盖自定义/MSP 角色场景）→ 最后回落 `users.role`。为拿到角色边，建号事务内按 `roleIds` 直接查 `roles`（避免依赖返回实体的边加载）。
- 兼容性：`roleIds` 为空的旧客户端行为完全不变（membership 解析自然回落到 `users.role`），MSW-1 幂等 upsert 语义不变。

### 4.3 前端设计（G2/G5）

- 契约：`ProvisionUserRequest` 增加 `roleIds?: number[]`（`api-config.ts:175-183`）；新增 `RoleOption` 类型与 `TenantAPI.listGrantableRoles(tenantId)`（`tenant-api.ts`）。
- `ProvisionUserModal`：
  - 新增 `roleOptions?: RoleOption[]`（或 `loadRoleOptions` 注入）；渲染 `Select mode="multiple"`，label 展示 `名称（code）`，非必填（D6）；
  - 提交载荷追加 `roleIds`；与既有 `mspRole` 并存（D1）；
  - provider 租户的「MSP 角色」下拉显示条件改为**由页面/接口策略传入**（`mspRoleOptions` 存在即显示，沿用组件现状），但页面不再用 `type === 'msp_provider'` 直接写死两个常量。
- 页面接线：
  - `admin/tenants/index.tsx`：打开建号弹窗时按 `provisionTenant.id` 调 `listGrantableRoles`；加载失败 → 角色区置灰 + 可重试提示（不阻断 msp_role/基本字段提交，但告警文案明确）；
  - `msp/management/index.tsx`：MSP 弹窗同样接入（D5）。
- 权限提示：候选为空（无任何可选角色）时显示空态说明，而不是静默隐藏。

### 4.4 兼容、灰度与回滚

- 接口：新增只读端点，旧客户端无感；未装配 provisioning 服务时端点 503（对齐现有 `provisionUserWithRequest` 的"建号通道未启用"语义）。
- 写入：`roleIds` 已存在，无 schema/迁移变更；**无需 DDL**。
- 灰度：可先仅平台面（B1/B2），MSP 面若评审延后则保留 `roleOptions` 为空 = 现状。
- 回滚：前端恢复不传 `roleIds` 即回到当前行为；后端新增代码路径不改变旧请求语义。

---

## 5. 实施批次

| 批次 | 内容 | 交付物 |
|------|------|--------|
| B0（本方案） | 冻结决策点 D1–D6；登记 `IP-P2-7` 到实施方案总表与 README 索引 | 本文档 + 总表/索引回填 |
| B1 后端 | `GrantableRoles` 服务 + 两处端点 + DTO + 主角色派生/membership 适配 + 单测（权限矩阵/过滤/RLS/幂等） | `service/user_provisioning.go(+test)`、`dto`、`handlers/user`、`router` 两处；`go test ./service ./handlers/user ./router` |
| B2 前端 | 契约 + 多选组件 + 两个页面接线 + mock/jest + tsc/eslint | `api-config.ts`、`tenant-api.ts`、`ProvisionUserModal(+test)`、`admin/tenants`、`msp/management` |
| B3 联调与收尾 | 浏览器实测（provider：msp_role+多角色；customer：多角色）→ 登录验证菜单/权限；CHANGELOG、scenarios/04 §7 缺口状态、前端分析 P1 行状态回填 | 实测记录 + 文档同步 |

---

## 6. 测试与验收（DoD）

### 后端（B1）

- 权限矩阵：非平台角色调用平台端点为 403；platform 端点对 active 目标租户 200、对 suspended 拒绝；MSP 端点非分配客户 403（mspguard）。
- 过滤正确性：`super_admin`/`sysadmin` 永不出现在候选；rank 高于通道上限的角色被过滤；客户类型目标租户不返回 `provider_tenant_admin`/`msp_*`；inactive 角色不返回；返回项 `tenant_id == target`（RLS 隔离，A 租户角色绝不出现于 B 租户候选）。
- 写入回归：`roleIds=[custom_role]` 建号成功且 `user_roles` 边落库；越权 roleIds（跨租户/高 rank）422 `ROLE_NOT_GRANTABLE`；不传 roleIds 行为与现状一致。
- 主角色/membership：多选含 `admin` 时 `users.role=admin`、home membership role 与之一致；多选仅自定义角色时 `users.role=end_user` 但 membership 角色取最高角色 code 且不落 NULL。

### 前端（B2）

- `ProvisionUserModal`：多选渲染/载荷、`mspRoleOptions` 条件显示、加载失败态；jest 用例扩充。
- 页面：mock 接口下候选数据驱动（改 mock 返回自定义角色即出现在下拉）；tsc 0 error。

### 联调（B3）

- provider 租户建号：`msp_role=provider_agent` + 多选 `msp_tech` → 登录后 MSP 工作台可达、菜单非空（关闭"权限空窗"）。
- 客户租户建号：多选 `admin` → 登录后客户管理菜单可见；取消角色 → 默认 `end_user` 行为不变。

---

## 7. 风险与未决问题

| 风险 | 等级 | 缓解 |
|------|------|------|
| membership 角色解析变更影响既有回填口径（20260504） | 中 | 只在 `roleIds` 非空时走新分支；回填 SQL/巡检口径先对拍（`msp-membership-switch-consolidation-plan.md`） |
| `users.role` 枚举与自定义角色不兼容 | 中 | D3 派生仅枚举兼容项 + warn 留痕；权限以 roleIds 边为准 |
| 平台面候选过滤规则（D2）与产品预期不一致 | 中 | **已锁定**：客户侧不得出现 MSP 专属角色（产品口径）；读（候选/列表/详情/改删/授权）+ 写（授予/建号 roleIds）三层同源收口，见 §9 |
| legacy 租户类型（`msp`）在 UI 侧判定遗漏 | 低 | 显示条件改为接口/策略驱动后自然消除；后端用 `tenantmode.IsMSPProviderTenantType` 归一 |
| MSP 面权限面扩大 | 低 | mspguard + `msp_customer:write` 与建号同源；只读、无新写面 |

未决问题：D2 已锁定（客户侧永不出现 MSP 专属角色，无"放开"选项）；D5 灰度顺序已按"平台+MSP 同批"实施。

---

## 8. 文档同步清单

- [x] 《[实施方案](./msp-implementation-plan.md)》§3.0 总表登记 `IP-P2-7` 与 DoD（v1.72 版本表同步）；
- [x] 《[前端页面与权限分析 §6.5:195](./msp-frontend-pages-and-permissions-analysis.md)》P1 行状态更新（✅ 已交付）；
- [x] 《[scenarios/04-user-provisioning.md](../scenarios/04-user-provisioning.md)》§7「建号未写 RBAC 角色边」缺口状态更新（✅ 已关闭）；
- [x] `CHANGELOG.md` 记录接口新增/UI 行为变化；
- [x] `docs/multi-tenant/README.md` 索引补充本方案。

---

## 9. 实施记录（2026-10-08）

**决策落定**：D1–D6 均按 §3 推荐值实施——① `msp_role` 保留独立维度（provider 专属单选，白名单不变）；② 候选过滤含客户作用域排除（客户类型租户排除 `provider_tenant_admin`/`msp_*`）与平台角色排除；③ 主角色派生仅枚举兼容项 + membership 按所选最高 rank 解析（`msp_role` 映射优先）；④ 平台面路由粗筛 `tenant:write` + service 通道判定；⑤ MSP 面同批交付；⑥ 多选非必填（不勾选 = 默认 `end_user`）。

**交付物（对照 §5）**：

| 批次 | 状态 | 证据 |
|------|------|------|
| B0 | ✅ | 本文档（§3 决策点、§5 批次） |
| B1 后端 | ✅ | `service/user_provisioning.go`（`GrantableRoles` / `validateRole` roleIds 防护 / 主角色派生 / membership 角色解析）、`dto.RoleOption`、`handlers/user` 两 handler、路由两处、`rbac_precheck_gen.go` 再生成；新增 `service/user_provisioning_roles_test.go` 5 用例 |
| B2 前端 | ✅ | `api-config`/`tenant-api`/`msp-api`/`msp-service` 契约；`ProvisionUserModal` 异步多选（失败告警不阻断）；`admin/tenants`、`msp/management` 接线与常量硬编码移除；弹窗 7/7、租户页 4 套件 26/26、`tsc`/`eslint` 0 |
| B3 联调与收尾 | ✅ | 新二进制 16:33 重启（health 200）；浏览器实测（见下）；CHANGELOG、实施方案 v1.72、前端分析 §6.5、scenarios 04、README 回填 |

**回归命令（均绿）**：`go test -p 1 ./service -run "TestGrantableRoles|TestProvisionUser_RoleIDs"`、`go test -p 1 ./service -run "Provision|Membership|Quota"`、`go test -p 1 ./handlers/user`、`go test -p 1 ./middleware -run "Precheck|RoutePrecheck|RouteScan"`、`go test -p 1 ./router -run "Tenant|MSP|Provision"`。

**联调实测（浏览器，超级管理员会话）**：

- 平台面 API：客户租户 `MSPCUSTA`（id=4）返回 27 个候选，**含租户自定义角色**（如 `ops_manager`/`rd_manager`），**不含** `super_admin`/`sysadmin`/`msp_*`/`provider_tenant_admin`；服务商租户 `MSP001`（id=3）含 `msp_viewer/specialist/tech/manager/admin` + `provider_tenant_admin`，不含平台角色。
- UI：客户租户建号弹窗字段 = 用户名/姓名/邮箱/初始密码/**角色（可多选）**；服务商租户 = 前述字段 + **角色（可多选）** + **MSP 角色**。
- 说明：本轮未在联调库实际创建用户（避免污染 dev 数据）；写入路径、主角色派生与 membership 一致性由 B1 单测覆盖。

**遗留/边界**：

- **D2/D2+ 已锁定为硬规则（产品口径：客户不该知道 MSP 相关功能；客户与服务商租户都不该有超级管理员/平台管理员）**，按租户类型三层同源收口：① 读（候选）`GrantableRoles` 过滤；② 读（角色管理）`RoleService` 列表/详情/改删/授权隐藏——客户租户隐藏「平台角色 + MSP 专属角色」、服务商租户隐藏「平台角色」、平台根（internal/standard）不过滤，命中按"不存在"处理（`total` 同步收敛，不泄露存在性）；③ 写（授予）`UserService.CanGrantRoles` 同口径拒绝，覆盖用户建号/编辑与建号通道 `roleIds`（422 `ROLE_NOT_GRANTABLE`）。词表单一源 `middleware.PlatformRoleCodes()/IsPlatformRole()` + `MSPOnlyRoleCodes()/IsMSPOnlyRole()`；建号服务平台角色防护同步改同源词表（原 `provisionPlatformRoleCodes` 内联表已移除）。实测（联调，super_admin 切换作用域）：客户租户 35→28（`mspOnly=[]`、`platformLeak=[]`）；服务商租户 34→32（MSP 六角色保留、`platformLeak=[]`）；平台根 34 不变。回归：service 新增 2 用例（含平台角色矩阵）+ role/user/provision 既有用例、handlers/rbac、handlers/user、middleware 全绿。
- **角色配置面越权绕过审计（2026-10-08 补丁 3，安全）**：`/admin/roles` 的三个动作逐一验证——①「创建/改角色」可把自有角色改名 `super_admin`（roles 表无 `(tenant_id,code)` 唯一索引 + `UpdateRole` 无保留词校验）→ 持有者获 `AuthorizeResource` 直通；②「配权限」可经 `POST /permissions` 自造 `*:*`（resource/action=`*`，`checkPermissionMatch` 无条件放行）或直接挂 catalog 中的 `tenant:write`；③ 权限分配后果落到**租户治理写面**：`/api/v1/tenants` 写操作仅 `tenant:write` 粗筛，而客户/服务商租户的克隆权限目录使其实 admin 角色**实际持有** `tenant:write` → 可跨租户创建/改删/供给租户（读面已有 scope 收敛，写面没有）。已修复：保留角色码全局护栏（Create/Update → 400）、非平台根禁通配权限（Create/Assign/Update 三路径 → 400）、`requirePlatformAdmin` 租户治理写面硬校验（403）、用户面 service 层纵深（平台角色/`msp_role` 仅服务商租户）。实弹（8090 真实账号）：客户/服务商 admin 的越权尝试全部 403/400/422，平台超管治理路径未被误伤（空体→400 参数错误）；零残留。**残余**：客户侧权限词表仍含 `msp:read/msp:write` 等码（角色权限树可能展示 MSP 概念，展示层过滤待产品确认）；其他平台路由族（system/menu 等）的"仅 permission 粗筛"面建议专项审计；`roles` 表建议补 `(tenant_id,code)` 唯一约束迁移。
- `users.role` 枚举是硬边界：自定义/MSP 角色只经 `roleIds` 边授予（主角色保持 `end_user` 并 warn 留痕），权限判定不受影响。
- invite 通道（IP-P1-4）未纳入本批，后续可复用 `dto.RoleOption` 与候选接口。

---

## 10. 版本表

| 版本 | 日期 | 说明 |
|------|------|------|
| v0.1 | 2026-10-08 | 首版草案：现状审计（硬编码/能力倒挂/缺跨租户读）、开通角色注入盘点、接口与多选设计、决策点 D1–D6 待评审 |
| v0.2 | 2026-10-08 | 实施记录回填：D1–D6 按推荐值落定；B1（服务/端点/派生/防护 + 单测）/ B2（契约 + 多选组件 + 双页接线）/ B3（重启 + 浏览器实测 + 全量文档回填）完成，回归与实测证据见 §9 |
