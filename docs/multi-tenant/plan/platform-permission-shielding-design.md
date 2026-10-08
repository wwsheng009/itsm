# 平台面权限屏蔽与角色架构设计（Platform Permission Shielding & Role Architecture）

> **状态**：v0.4（2026-10-08）——基于 IP-P2-7 收口补丁 1–6 的实施事实归纳与实弹验证回填（§7）；D4=B/R6 与 R2/R3 均已收口（补丁 5/6）；剩余待办与决策点见 §5/§6
>
> **上位（权威顺序）**：[canon](./msp-concept-model-and-architecture-canon.md)（D5/D8/D10/D11、§2.3）→ [目标架构 §6](./msp-target-architecture.md)（权限矩阵）→ [实施方案](./msp-implementation-plan.md)（IP-P2-7 批次）→ 本设计
>
> **关联**：[建号角色多选方案](./msp-provision-user-roles-multiselect-plan.md)（角色可见性/可授予三一致）；[前端页面与权限分析](./msp-frontend-pages-and-permissions-analysis.md)；`CHANGELOG.md`（补丁 1–4 条目）

---

## 1. 背景与根因

### 1.1 问题的本质：两个等式

| 等式 | 含义 | 后果 |
|------|------|------|
| **码 = 能力** | 路由按 `resource:action` 权限码判定（`AuthorizeResource`），用户持有某码即通过该路由 | 权限目录克隆把平台码（`system:*`/`tenant:write`/`mcp:admin`）带到非平台租户；角色授权使"持有码"发生；`POST /permissions` 还可自铸码 |
| **角色码 = 身份** | `AuthorizeResource` 对 `super_admin` 直通；RBACMiddleware 把 m2m 角色码物化进上下文 | 若租户可创建/改名角色为保留码，即获得平台级直通（已堵） |

### 1.2 攻击链（收口前实态）

1. 客户/服务商租户的权限目录由 default 租户克隆而来，**包含平台家族码**（实测：`system:read/write`、`tenant:read/write`、`mcp:admin` 等均在客户目录）。
2. 租户管理员持有 `role:write`，可把目录中任意权限挂到自有角色（无平台治理资源护栏时）。
3. 仅挂 `RequirePermission` 的平台路由即被穿过：`/ai/providers`（LLM 全局实例）、`/admin/operations/commands`、`/admin/rls/stats`、`/timers`、`/api/v1/tenants` 写面（补丁 3 前）。
4. 角色侧同理：改名 `super_admin` → 直通（补丁 3 前）；通配 `*:*` 可自造（补丁 3 前）。

**结论：平台治理面不能仅凭"码"判定；"码"必须由平台独占词表，且平台面必须叠加身份硬校验。**

---

## 2. 现状机制（已落地事实，截至补丁 5）

### 2.1 判定链

```
请求
 → RBACMiddleware：物化角色集（主角色 + m2m 附加角色，含深度切换 membership）＝ GetContextRoles
 → 路由 RequirePermission(resource, action)：AuthorizeResource
     （super_admin 直通 → *:* 通配 → 精确/前缀匹配；权限来自角色 m2m 权限行 + 角色默认矩阵）
 → service/handler 硬校验：平台身份（RequirePlatformAdmin）/ 租户 scope / membership / allocation
```

### 2.2 词表单一源

| 词表 | 位置 | 语义 |
|------|------|------|
| 平台角色码 `super_admin` / `sysadmin` | `middleware.PlatformRoleCodes()/IsPlatformRole()` | 平台治理身份 |
| MSP 专属角色码（`msp_*`、`provider_*` 等） | `middleware.MSPOnlyRoleCodes()/IsMSPOnlyRole()` | 服务商面角色 |
| 租户类型 | `service/tenant_scope.go`（`tenantTypeOf`） | platform root（internal/standard，或 `code=default` 根租户兜底）/ msp_provider / msp_customer |
| 平台治理权限资源 | `service.role_service.go:platformGovernedPermissionResources`（评估结论 `{system}`） | 非平台根不可分配 |

### 2.3 已收口的面（补丁 1–6）

| 层 | 机制 | 落点 |
|----|------|------|
| 平台根判定 | 根租户标识叠加：`code=default` 兜底判为平台根（provider 语义仍按类型，不放大） | `service/tenant_scope.go`（`isPlatformRootTenant`，`platform_root_tenant_test.go`） |
| 角色·可见性 | 按租户类型过滤列表/详情（客户隐平台+MSP、服务商隐平台、平台根不过滤；命中按"不存在"处理、`total` 收敛） | `RoleService`（`msp_role_visibility_test.go`） |
| 角色·可授予 | 同口径拒绝（用户建号/编辑 + 建号通道 `roleIds`，422 `ROLE_NOT_GRANTABLE`） | `CanGrantRoles` / `GrantableRoles` |
| 角色·可写 | 保留码全局拒绝（创建/改名；`ErrReservedRoleCode`→400） | `CreateRole/UpdateRole` |
| 权限·通配 | 非平台根禁创建/分配 `*:*`（`ErrWildcardPermissionForbidden`→400，覆盖 Create/Assign/Update 三路径） | `role_service.go` |
| 权限·平台码 | `platformGovernedPermissionResources`（评估结论 `{system}`）不可分配（`ErrPlatformPermissionForbidden`→400） | `ensureAssignablePermissionsForNonPlatformRoot` |
| 权限·词表所有权 | 权限创建/init 路由仅平台（`RequirePlatformAdmin` + `role:write`） | `common_system_routes.go` |
| 平台面硬校验 | `middleware.RequirePlatformAdmin()`：身份层判定（主角色+m2m 并集），与码无关 | 租户治理写面、LLM 实例管理、运维命令、RLS 指标、定时器 |
| 防回退守卫 | 写路由必须挂权限中间件（AST 扫描）；权限码必须在目录；平台守卫单测；平台面路由 L1 注册表（补丁 6） | `route_permission_guard_test.go`、`permission_code_catalog_guard_test.go`、`require_platform_admin_test.go`、`platform_face_guard_test.go` |


---

## 3. 目标架构

### 3.1 三层防线（判定分层）

| 层 | 问题 | 机制 | 适用范围 |
|----|------|------|----------|
| **L1 身份** | "你是谁"（平台治理者 vs 租户用户） | `RequirePlatformAdmin()`（平台角色码判定，主角色 + m2m 角色并集，与权限码无关） | 平台治理面（必挂） |
| **L2 能力** | "你能做什么"（码 = 能力） | `RequirePermission(resource, action)` + 服务层分配护栏（通配/平台资源/保留码） | 全部受保护路由（必挂） |
| **L3 数据** | "你作用于谁的数据" | 租户作用域（ctx tenantID）、membership、分配记录（allocation）、RLS | 多租户数据面 |

**组合规则**：

1. **平台治理面 = L1 + L2**：只挂 `RequirePermission` 不够——`system:write` 等平台码可能经目录克隆+角色授权到达租户管理员（本轮实测确认），必须叠加 L1。
2. **租户功能面 = L2 + L3**：不要求平台身份，但数据层必须收紧（ctx 租户 + membership/allocation 双验）。
3. **L1 与 L2 不可互替**：L1 不看码（防码扩散）；L2 不看身份（防绕过授权体系）。两层都过才放行平台面。

### 3.2 词表所有权（单一真源，平台独占写）

| 词表 | 拥有者 | 租户侧权限 |
|------|--------|------------|
| 权限定义（`permissions` 表）：码集合与语义 | 平台 | **只读**（`GET /permissions` 列目录）；创建/初始化仅平台（`POST /permissions`、`/permissions/init` 已收口） |
| 平台治理码（`resource=system` 等） | 平台 | 可见（目录现状）但**不可分配**（`ErrPlatformPermissionForbidden`） |
| 通配码 `*:*` | 平台 | 非平台根**不可创建/分配**（`ErrWildcardPermissionForbidden`） |
| 保留角色码（`super_admin`/`sysadmin`） | 平台 | 全局**不可创建/改名**（`ErrReservedRoleCode`） |
| 角色定义（`roles` 表） | 各租户 | 租户自管（可见性/可授予按租户类型收口） |

> 原则：**租户可以"用码"，不能"造码"；可以用"角色"，不能自称"平台"。**

### 3.3 平台治理面清单（L1 强制点，随补丁 4 落地）

| 面 | 路由 | 守卫组合 | 理由 |
|----|------|----------|------|
| LLM 实例管理 | `/ai/providers*` 管理组（除 available/user-preference 读） | L1 + `system:write` | `llm_provider` 无租户字段（全局资源） |
| 运维命令 | `/admin/operations/commands*` | L1 + `system:write` | 平台运维面 |
| RLS 指标 | `/admin/rls/stats` | L1 + `system:read` | 平台运维面 |
| 定时器 | `/timers*` | L1 + `system:read` | 平台调度面 |
| 租户治理写面 | 租户创建/更新/删除、状态、配额等（补丁 3：handler 内 `requirePlatformAdmin`） | L1 + `tenant:write` | 平台独占管理面 |
| 权限词表写面 | `POST /permissions`、`POST /permissions/init` | L1 + `role:write` | 词表所有权 |

> **新增平台面路由的评审规则**：凡作用于全局/跨租户资源、或写平台词表/平台身份的路由，必须同时挂 L1 与 L2，并在 `route_permission_guard_test.go` 可审计（写路由缺权限中间件即失败）。
>
> **扩展纪律（配套清单）**：新增角色/权限/路由的归类、登记与验证步骤见《[新增角色/权限/路由评审清单](./role-permission-extension-checklist.md)》（§2.2 平台家族码评估、§5 守卫矩阵）；平台面路由的 L1 挂载由 `router/platform_face_guard_test.go` 注册表守卫锁定（补丁 6）。

### 3.4 角色三一致

角色的「可见性 / 可授予性 / 可写性」必须同口径（`visibility == grantability`，写面更严）：

| 角色族 | 平台根 | 服务商租户 | 客户租户 |
|--------|--------|------------|----------|
| 平台保留码（`super_admin`/`sysadmin`） | 可见 · 可授 | 隐藏 · 拒绝 | 隐藏 · 拒绝 |
| MSP 专属码（`msp_*`/`provider_*` 等） | 可见 · 可授 | 可见 · 可授 | 隐藏 · 拒绝 |
| 租户本地角色 | 可见 · 可授 | 可见 · 可授 | 可见 · 可授 |
| 保留码写面（创建/改名） | ⛔ 全局拒绝 | ⛔ | ⛔ |

判定口径由 `middleware.PlatformRoleCodes()` / `IsPlatformRole()` / `IsMSPOnlyRole()` 与 `service.tenantTypeOf` 单一真源提供。

---

## 4. 判定矩阵

### 4.1 路由面 × 租户类型

| 面（示例路由） | 平台根 | 服务商 | 客户 |
|----------------|--------|--------|------|
| 平台身份面（`/ai` 管理组、`/admin/operations/commands`、`/admin/rls/stats`、`/timers`） | ✅ | ⛔ 403（L1） | ⛔ 403（L1） |
| 租户治理写面（`POST /api/v1/tenants` 等） | ✅ | ⛔ 403（L1） | ⛔ 403（L1） |
| 权限词表写面（`POST /permissions`、`/permissions/init`） | ✅ | ⛔ 403（L1） | ⛔ 403（L1） |
| 租户功能面（roles/users/projects/…） | ✅ | ✅（限本租户 + membership） | ✅（限本租户） |

### 4.2 权限分配护栏（service 层，按**作用域租户类型**判定，与调用者身份无关）

| 分配物 \ 作用域 | 平台根 | 服务商 | 客户 |
|-----------------|--------|--------|------|
| 普通租户码（如 `project:read`） | ✅ | ✅ | ✅ |
| 通配 `*:*` | ✅ | ⛔ 400 | ⛔ 400 |
| 平台资源码（`resource=system`） | ✅ | ⛔ 400 | ⛔ 400 |
| 保留角色码（创建/改名） | ⛔ 400 | ⛔ 400 | ⛔ 400 |

### 4.3 数据层（L3）要点

- 平台根跨租户操作：以 `tenantID`（含 switch-tenant 会话作用域）为锚，服务层一律按 ctx 租户过滤；
- 服务商→客户：allocation 记录限定可服务范围（MSP 可见性），membership 校验实际关系；
- 客户：只能看/改本租户成员与实体（RLS 兜底）；
- **L1/L2 决定"能否做动作"，L3 决定"动作落在哪个租户"——三者正交。**


---

## 5. 落地状态与待办

### 5.1 已落地（补丁 1–6 + 守卫）

| 项 | 内容 | 测试/守卫 |
|----|------|-----------|
| 补丁 1 | 角色可见性/可授予分级过滤 + 保留码写面拒绝 | `service/msp_role_visibility_test.go`、`role_config_bypass_guard_test.go` |
| 补丁 2 | 用户建号/编辑通道拒绝不可授予角色（422 `ROLE_NOT_GRANTABLE`） | `service/user_provisioning_*_test.go`（角色校验子用例） |
| 补丁 3 | 租户治理写面平台身份硬校验（handler 层）+ 通配权限三路径拒绝 | `handlers/tenant/*_test.go`、`role_config_bypass_guard_test.go` |
| 补丁 4 | `middleware.RequirePlatformAdmin()`；`/ai` 管理组、运维命令、RLS 指标、定时器、权限词表写面挂 L1；`resource=system` 不可分配（`ErrPlatformPermissionForbidden`→400） | `middleware/require_platform_admin_test.go`（6 子用例）、`service/role_config_bypass_guard_test.go`（步骤 ⑤） |
| 补丁 5 | 平台根判定叠加根租户标识（D4=B）：`code=default` 兜底判为平台根；拆分平台语义与 provider 语义调用点（`role_service`/`user_service`） | `service/platform_root_tenant_test.go`（谓词矩阵 + 调用点差分）、`tenant_scope.go` |
| 补丁 6 | 扩展护栏（R2/R3 收口）：平台面路由 L1 注册表守卫（AST：注册表 + 反向前缀 + handler 级登记）；平台家族码逐家族评估（仅 `system` 纳入；机制改显式集合 + 决策锁） | `router/platform_face_guard_test.go`、`service/role_config_bypass_guard_test.go`（决策锁）、[扩展评审清单](./role-permission-extension-checklist.md) §2.2 |
| 防回退 | 写路由必须挂权限中间件（AST）；权限码必须在目录 | `router/route_permission_guard_test.go`、`router/permission_code_catalog_guard_test.go` |

### 5.2 待办

| 优先级 | 事项 | 说明 |
|--------|------|------|
| P0 | 存量租户权限目录处置 | 方向待决策（§6.1 D1）；本补丁后短期风险已由 L1+L2 覆盖 |
| P1 | 前端展示层联动 | 角色权限选择器/用户角色下拉隐藏"不可分配项"，与后端 400 语义对齐（减少无谓失败） |
| P2 | 角色码归类完整性守卫（建议） | 新增角色码"漏登记词表 → 按租户本地角色放行"暂无自动守卫；建议扫描角色码定义源（如 `msp_` 前缀 ⊆ MSP 词表），见扩展评审清单 §5 |
| P2 | L3 数据面复核 | membership/allocation/RLS 的剩余缺口见对应专文（不在本设计范围） |

---

## 6. 决策点与残余风险

### 6.1 决策点

| 编号 | 决策 | 选项 | 建议 | 影响 |
|------|------|------|------|------|
| D1 | 存量权限目录中的平台码（`system:*` 等）如何处置 | A. 裁剪（逐租户删除平台家族码行）；B. 保留但不可分配（现状） | **B 先行、观察期后评估 A**：裁剪会改变历史角色→权限映射与审计可读性，且回滚成本高；保留+护栏已阻断攻击链 | 裁剪影响 `permissions` 表存量行、角色权限 m2m、前端目录展示 |
| D2 | 平台治理判定是否下沉到 service（而非仅路由） | A. 维持路由层 L1；B. service 层再加身份校验 | **A**：service 层无调用者身份语义（且 `CreatePermission` 被 `InitDefaultPermissions` 内部调用），路由层是正确收口点；service 层只做分配护栏（按作用域租户类型） | 若未来出现非 HTTP 调用路径需单独评审 |
| D3 | `RequirePlatformAdmin` 的判定口径 | A. 仅主角色；B. 主角色 + m2m 并集 | **B（已实现）**：与 `AuthorizeResource` 的 `GetContextRoles` 口径一致，避免切换/附加角色场景绕过 | — |
| D4 | 平台根判定口径：仅类型判定（`internal`/`standard`）是否足够 | A. 维持类型判定；B. 叠加根租户标识（`tenants.code=default`）兜底，provider 语义仍按类型；C. 改 seeder 使 `saas_msp` 根租户保持 `internal` | **B（已实施，补丁 5，2026-10-08）**：`saas_msp` 模式根租户由 seeder 固定为 `msp_provider`（部署形态自检要求存在 provider 租户，且根租户即 MSP 服务商主体），类型判定使平台根丧失平台面（§7.3 实测）；B 不动 provider 语义，改动收敛在平台根谓词及其调用点 | 补丁 5（已落地）：`tenant_scope.go` 谓词拆分（`isPlatformRootTenant`/`tenantScopeOf`/`hiddenRoleCodesForTenant`）+ `role_service`/`user_service` 调用点 + `platform_root_tenant_test.go`；实弹复测见 §7.4；选 A 则 `saas_msp` 平台根功能降级为"仅 `super_admin` 直通" |

### 6.2 残余风险

| 编号 | 风险 | 现状 | 缓解 |
|------|------|------|------|
| R1 | 克隆目录仍含平台码，用户界面可见 | 存在 | L1 使持码不可穿过平台面；P1 前端过滤；D1 决策后再清 |
| R2 | 平台面新增路由漏挂 L1 | 已收口（补丁 6：注册表 + 反向前缀守卫，`router/platform_face_guard_test.go`） | 新增平台面路由必须同步注册表（§3.3 评审规则 + 扩展评审清单 §3） |
| R3 | `resource != system` 的平台家族码（`menu`/`system_config`/`mcp`/…）仍可分配 | 已收口（补丁 6 逐家族评估：`system_config`/`mcp`/`connector` 为租户面不纳入；`menu`/`federation` 码空间不存在；结论见扩展评审清单 §2.2） | 护栏集合维持 `{system}`；新增/调整家族须先完成评估并更新清单 |
| R4 | 内部调用（`InitDefaultPermissions` 等）不经路由 | 已确认 | service 层不做身份拦截（D2）；仅分配护栏生效，内部调用默认可信（平台初始化路径） |
| R5 | 前端角色页可勾选不可分配权限，提交后 400 | 存在 | P1 展示层联动 |
| R6 | `saas_msp` 根租户被类型判定当作服务商租户 | 已收口（补丁 5 落地 + §7.4 复测：平台角色可见、`system:write` 分配 200；补丁 4 时点观测见 §7.3） | D4=B 已实施：`isPlatformRootTenant` 根租户标识叠加判定；provider 语义仍按类型（不放大） |

---

## 7. 实弹验证证据（2026-10-08，8090 联调环境）

> 脚本：`.dev/probe-platform-shield.ps1`（三身份 × 平台面矩阵 + L2 分配护栏）、`.dev/probe-root-tenant1-diff.ps1`（根租户类型/角色面差分）。均为真实账号登录、临时角色用后即删，探测零残留。

### 7.1 平台面屏蔽矩阵（客户 11 / 服务商 10 / 平台根 1）

| 探测项 | 客户 11 | 服务商 10 | 平台根 1 |
|--------|---------|-----------|----------|
| `POST /permissions` | ⛔ 403（L1） | ⛔ 403（L1） | · |
| `POST /permissions/init` | ⛔ 403（L1） | · | ✅ 200 |
| `/admin/operations/commands` | ⛔ 403（L2） | · | ✅ 200 |
| `/admin/rls/stats` | ⛔ 403（L2） | ⛔ 403（L2） | ✅ 200 |
| `/timers` | ⛔ 403（L2） | ⛔ 403（L2） | ✅ 200 |
| `/ai/providers` | 404（功能开关未开，非权限路径） | 404（同） | 404（同） |

> ·＝未单独探测（不构成结论）；L1 文案「仅平台管理员可执行该操作」，L2 文案「权限不足」。

### 7.2 L2 分配护栏差分（建临时角色 → 分配 → 删除）

| 分配物 | 客户 11 | 平台根 1 |
|--------|---------|----------|
| 普通权限码 | ✅ 200 | ✅ 200 |
| `system:write`（id=2194） | ⛔ 400（`ErrPlatformPermissionForbidden`） | ⛔ 400 ⚠️（补丁 4 时点；补丁 5 复测转正 200，见 §7.4） |
| 临时角色建/删 | ✅ 200 / ✅ 200 | ✅ 200 / ✅ 200 |
| 残留 | 0 | 0 |

客户权限目录 206 条（含 `system:write`），印证 §1.1「码＝能力」前提：目录含平台码，靠 L2 分配护栏阻断挂载。

### 7.3 发现（补丁 4 批次登记 D4/R6；已由补丁 5 修复，复测见 §7.4）

- **F1**：平台根租户分配 `system:write` 被 400 拒绝（预期平台根放行）。
- **F2**：平台根租户角色列表（total=34）不显示 `super_admin`/`sysadmin`（平台角色被隐藏）。
- **根因**：`saas_msp` 模式 seeder 把根租户（`code=default`）类型固定为 `msp_provider`（`pkg/seeder/seeder.go:795`）；平台根谓词 `isPlatformRootTenantKind` 仅认 `internal`/`standard`（`service/tenant_scope.go:60`），根租户被判为服务商租户。
- **影响面**：平台根的角色管理面（平台角色可见性、平台码授予）降级；`super_admin` 直通（L2 `*:*`）与 L1 身份判定不受影响，无越权扩大。
- **处置**：D4 建议 B（根租户标识叠加判定）→ 补丁 5 已落地（§5.1），实弹复测转正见 §7.4。

### 7.4 复测（补丁 5 后，2026-10-08，8090 重启二进制）

> 脚本：`.dev/probe-root-tenant1-diff.ps1`（根租户差分：真实账号、临时角色用后即删、探测零残留）。

| 探测项 | 结果（平台根 1：`code=default`/type=`msp_provider`） | 对应发现 |
|--------|----------------------------------------------------------|----------|
| 平台角色可见性（角色列表） | ✅ 显示 `100:super_admin`、`101:sysadmin` | F2 转正 |
| `system:write`（id=2194）分配 | ✅ 200 | F1 转正 |
| 普通权限码分配 | ✅ 200（未回归） | — |
| 临时角色建 / 删 | ✅ 200 / ✅ 200 | — |
| 残留 | 0 | — |

判定：D4=B 生效——根租户（`code=default`）按平台根作用域恢复平台治理面（F1/F2 转正）；provider 语义仍按类型判定（不放大）；R6 关闭。

