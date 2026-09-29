# MSP 多租户前端页面与权限分析及目标细化方案

> 状态：**Draft v0.1（待评审）**｜日期：2026-09-29｜基准：仓库 HEAD `99eb4074`
> 范围：`itsm-frontend` 页面/路由/权限/菜单/租户上下文现状 → 目标架构的前端细化（切换器、上下文指示、切换后权限与缓存刷新、按 `account_kind` 的差异化 UI）
> 关联：[目标架构方案](./msp-target-architecture.md)（§9 前端架构）｜[登录与切换细化方案](./msp-login-and-switching-refinement-plan.md)（隐私红线/切换器）｜[用户交互流程图](./msp-user-interaction-flows.md)（F-04/F-05）｜[07 已知缺口](../07-known-gaps.md)（G8 缓存）

---

## 0. 摘要（TL;DR）

| 维度 | 现状判定 | 结论 |
|---|---|---|
| 登录页 | ✅ **已无租户字段/选择器**（符合隐私红线） | 保持 + 测试防回归；但 `AuthService` 有硬编码默认租户回退 ❌ 必须移除 |
| 租户上下文 | 🟡 有单例 `tenant-context` + localStorage + 请求头注入 | 但 `session-bootstrap` 强制 `tenants[0]`、无切换器、无客户头注入 |
| 权限（前端） | 🟡 权限来自 `/auth/me`，`hasPermission` 通配符判定，守卫组件存在但未接线 | **后端权限双源**（登录/切换=静态表 vs 运行时=DB）→ 切换后权限刷新必须重建 |
| 菜单 | ✅ 后端按租户生成；无前端静态菜单 | ❌ React Query key 未按租户分键、登出/切换不失效 → 跨租户残留风险 |
| 页面 | 🟡 业务页齐全；MSP 两页用局部客户 state；管理页缺目标租户选择 | 细化清单见 §6.5 |
| 路由守卫 | 🟡 `RequireAuth`/`AdminRouteGuard` 可用；路由级权限元数据与 `RouteGuard` **未接入**（遗留并行模型） | 明确单一模型（页面级 `hasPermission` 为准）+ 补独立 403 |

**核心结论**：前端**不需要新增"多租户框架"**——已有租户上下文/权限/菜单三件套，缺的是**切换链路**（切换器 → 切换 API → 上下文/权限/菜单/缓存刷新）与**登出/切换的缓存一致性**。

---

## 1. 现状：路由与页面清单

### 1.1 路由结构

| 项 | 现状（证据） |
|---|---|
| 入口 | `src/main.tsx:3,7` `createBrowserRouter(routes)`；`src/App.tsx:22-42` Provider 树 + `<Outlet/>`；无 basename/子域路由 |
| 路由表 | `src/routes/index.tsx:185-389`（AUTO-GENERATED，来源 `vite-route-map.csv`）；静态路径集 `src/routes/route-paths.ts:3` |
| 分组 | auth（`:195-203`，`AuthLayout`）；main（`:205-208` `RequireAuth` → `MainLayout`）；tickets 子布局（`:210-225`）；admin（`:226-254`，30 条，混在 main children）；msp（`:311-312`） |
| 懒加载 | 全部 `lazy()`（`:28-183`）+ Suspense（`:20-26`） |
| 守卫 | `RequireAuth`（`src/routes/guards.tsx:18-47`）；`AdminRouteGuard` 挂 `MainLayout.tsx:125-131`（仅 `/admin` 前缀） |
| 403 | ❌ 无独立路由；内联 Result 403（`components/common/AdminRouteGuard.tsx:11-24`、`components/auth/AuthGuard.tsx:154-196`、`components/layout/RouteGuard.tsx:124-140` 未接入） |
| 遗留并行模型 | ❌ `src/lib/router/route-config.ts`（`RouteConfig`/roles/`RoutePermissionChecker`）与 `RouteGuard/withRouteGuard` **零业务引用** |

### 1.2 页面清单（按分组）

| 分组 | 页面 | 备注 |
|---|---|---|
| auth | login / register / forgot-password / reset-password / sso callback | `routes:195-203` |
| main（业务） | tickets、incidents、problems、changes、knowledge、cmdb、service-catalog、ai、reports、msp… | `routes:255-363` |
| admin | users、roles、permissions、menus、tenants、system-config、ticket-type、SLA… | 30 条（`:226-254`） |
| msp | `msp/index.tsx`（仪表盘）、`msp/management/index.tsx`（分配管理） | `routes:311-312`，无 MSP 专属守卫 |
| 系统 | `/system/users`（`routes:351`）与 `/admin/users` **两套入口并存** | 🟡 收敛项 |

---

## 2. 现状：权限模型（前端）

| 项 | 现状（证据） | 判定 |
|---|---|---|
| 权限来源 | 后端 `/api/v1/auth/me` 的 permissions 列表（`lib/auth/session-bootstrap.ts:41,74`；`components/auth/AuthGuard.tsx:46-55,92`） | ✅ |
| 存储 | `lib/store/auth-store.ts:20-27`（`user.permissions`）；`:161-167` **不持久化**（防跨用户残留） | ✅ |
| 判定 | `hasPermission`（`auth-store.ts:138-145`，支持 `*`/`resource:*`）；`hasRole :148-151`；`isAdmin :154-157`（`admin|super_admin`） | ✅ |
| Hook | `usePermissions`（`lib/hooks/use-permissions.ts:35-37`；`isAdmin :87-89`；`canAccessRoute :63-82`） | ✅ |
| 守卫组件 | `PermissionGuard/RoleGuard/AdminGuard/AnyPermissionGuard`（`components/auth/PermissionGuard.tsx:35-145`）——**除自身与测试外无业务引用** | 🟡 未接线 |
| 实际调用点 | Sidebar（`Sidebar.tsx:219-224`）、Header（`Header.tsx:49,267`）、AIChat（`AIChat.tsx:481`）、AI 审计/审批（`ai/audit/index.tsx:557`、`ai/approval/index.tsx:129`）、服务目录（`service-catalog/index.tsx:45` 等）、系统配置（`admin/system-config/index.tsx:105`） | 🟡 直接 `hasPermission()` |
| 硬编码 | `auth-store.ts:156`、`use-permissions.ts:88`（admin/super_admin）；`constants/common.ts:5-8`；`user-api.ts:9-12,32-35`（含 super_admin/admin）；`types/msp.ts:9`（`msp_*` 仅类型） | 🟡 |
| 租户维度 | ❌ 前端不做 `tenant × permission` 重算——**权限列表由后端按 JWT 租户下发** | ❌ 依赖后端 B4 修复 |

> **关键风险（与后端联动）**：登录/切换响应里的 permissions 来自后端**静态硬编码表**，而运行时执法是 **DBOnly fail-closed**——前端菜单/按钮可能"看得见但点不动"或反之。修复归属后端（见[登录与切换细化方案](./msp-login-and-switching-refinement-plan.md) §8.1 B4）；前端配合：**切换/登录后一律以最新响应或 `/auth/me` 重建 permissions**。

---

## 3. 现状：菜单

| 项 | 现状（证据） | 判定 |
|---|---|---|
| 数据源 | 后端 `GET /api/v1/auth/menus`（`lib/api/menu-api.ts:105-107`）；管理 CRUD `/api/v1/menus`（`:67-102`，租户由 JWT 注入） | ✅ |
| 前端静态菜单 | 无（`components/layout/sidebar/menu-config.ts:1-11` 注释声明；仅 `capabilityPathRules :31-51`） | ✅ |
| 取数/缓存 | `useUserMenusQuery`（key **常量** `['auth','menus']`，staleTime 5min，React Query 内存） | ❌ 未按租户/用户分键 |
| 渲染/过滤 | `Sidebar.tsx:179-180` 树转换；`:199-217` capability fail-closed；`:219-224` isAdmin；`:258-265` 管理菜单 | ✅ |
| 刷新机制 | `MENUS_UPDATED_EVENT`（`menu-api.ts:59-65`）+ `useInvalidateUserMenus`（`useUserMenusQuery.ts:46-60`） | ✅（仅管理端更新场景） |
| 登出/切换失效 | ❌ 无（grep 无 auth 流程 invalidate） | ❌ |
| 双份拷贝 | `capabilityPathRules` 在 `menu-config.ts:31-51` 与 `Sidebar.tsx:49-67` 各一份 | 🟡 漂移风险 |

---

## 4. 现状：租户上下文与会话

| 项 | 现状（证据） | 判定 |
|---|---|---|
| 上下文单例 | `lib/auth/tenant-context.ts:14-17`（模块级内存，tenantId/tenantCode；get/set/clear `:26-67`；subscribe `:73-78`） | ✅ |
| 持久化 | `lib/auth/token-storage.ts:15-29`（`current_tenant_id/code`）；legacy 迁移 `:80-92`；`clearAuthStorage :141-149` | ✅ |
| store 写入 | `auth-store.ts:60-75`（login 同步 httpClient setter）；`setCurrentTenant :111-120`；`clearTenant :123-132`；hydration `:321-345` | ✅ |
| 请求注入 | `lib/api/http-client.ts:180-192` 注入 `X-Tenant-ID` / `X-Tenant-Code`；**无 `X-Customer-Tenant-ID`** | 🟡 |
| 会话引导 | ❌ `session-bootstrap.ts:61-62` 每次探活强制 `tenants[0]` 并覆写 store（`:64-103`）；`AuthGuard.tsx:81` 同 | ❌ |
| 切换能力 | ❌ `TenantAPI.switchTenant`（`lib/api/tenant-api.ts:41-44`）无调用方、端点为 `/api/v1/tenants/switch`（错误）；无切换器组件 | ❌ |
| 刷新语义 | ❌ 整页刷新后 `tenants[0]` 覆盖，手动选择无法保持 | ❌ |
| 登出清理 | ✅ 清 user/token/currentTenant（`auth-store.ts:79-85`）、localStorage（`token-storage.ts:141-149`）、httpClient token、tenant-context 置 null（`:90-92`）；❌ 未清 React Query 缓存、未调 `resetSessionBootstrap`（`session-bootstrap.ts:32-34`）、WS 仅 Header 卸载断开（`Header.tsx:115-118`） | 🟡 |
| localStorage 键 | 🟡 `auth-store.ts:117-118` 字面量 vs `token-storage.ts:21-22` 常量（未复用） | 🟡 |

---

## 5. 现状：关键页面

### 5.1 登录 / 注册 / 找回密码

| 页面 | 现状（证据） | 判定 |
|---|---|---|
| 登录 | 字段仅 username/password/rememberMe（`pages/(auth)/login/index.tsx:280-327`）；提交 `AuthService.login(..., undefined, ...)`（`:158-163`）→ tenantCode 恒 undefined；错误处理含限流倒计时（`:103-128,183-189`） | ✅ 无选择器（隐私合规） |
| AuthService | 回退硬编码默认租户（`auth-service.ts:306-315`）；会话确认 `:192-199`；cookie 标记 `:277-282` | ❌ 兜底必须移除 |
| 注册 | 角色选项硬编码（`register/index.tsx:267-270`：developer/manager/admin/user）；无租户字段；错误被吞（`auth-service.ts:363-366`） | ❌ 与 F3 白名单冲突 |
| 找回密码 | 页面无租户输入；`AuthService.forgotPassword(email, tenantCode?)` 支持但未用（`auth-service.ts:370-378`） | 🟡 |

### 5.2 管理页面

| 页面 | 现状（证据） | 判定 |
|---|---|---|
| 用户管理 | 目标租户 = `currentTenant?.id`，缺失即报错（`admin/users/index.tsx:130-134`）；角色下拉含 `sysadmin/admin/super_admin`、**无 `msp_*`**（`user-api.ts:32-35`） | ❌ F11 |
| 租户管理 | `admin/tenants/index.tsx:93-121` CRUD；含 `domain` 字段（`:76,86`）；类型 mspProvider/mspCustomer（`:62-70`）；无"切换为当前租户"动作 | 🟡 |
| 角色/权限/菜单 | `admin/roles`、`admin/permissions`、`admin/menus`（`routes:239-242`；permissionCode 编辑 `admin/menus/index.tsx:587-596`） | ✅ |
| 系统配置 | SMTP（`admin/system-config/index.tsx:433-441`、`EnhancedSystemConfig.tsx:576-618`）、租户默认 LLM（`llm-provider-settings.tsx:274-278,448-449`）；均按请求上下文租户生效 | 🟡 无租户选择器（治理侧需先切换） |

### 5.3 MSP 页面

| 页面 | 现状（证据） | 判定 |
|---|---|---|
| 仪表盘 | `MSPService.isMSPUser()` 定权（`msp/index.tsx:60-73`）；并发拉 allocations/customers/context/reports（`:75-100`）；按页内 `selectedCustomerId` 拉客户工单（`:103-115`）；展示服务端上下文（`:470`） | 🟡 局部 state |
| 分配管理 | `isMSP||isAdmin` 放行（`msp/management/index.tsx:37-49`）；建分配选 `customerTenantId`（`:222-230`） | ✅ 基础 |
| 全局上下文 | ❌ 两页 `currentTenant` 零引用；无顶栏切换器/全局指示；`X-Customer-Tenant-ID` 前端从未注入 | ❌ F5/F6/F10 |

---

## 6. 目标细化方案

### 6.1 上下文模型（`TenantContext v2`）

```ts
type TenantContext = {
  tenantId: number;            // 当前生效作用域
  tenantCode: string;
  tenantType: 'msp_provider' | 'msp_customer' | 'internal';
  role?: string;               // 该作用域内角色（msp_manager/msp_tech/...）
  source: 'login' | 'switch' | 'restore';
  accountKind: 'customer' | 'provider' | 'platform';
  customerTenantId?: number;   // 头通道单请求只读（可选）
};
```

- 单一来源：由服务端响应/`/auth/me` 写入；**前端不得自行推导**（`tenants[0]` 兜底移除）；
- 持久化仅 `tenantId/tenantCode`（现有 localStorage 键保留）；内存态以 store 为准。

### 6.2 切换器与上下文指示（`TenantSwitcher`）

| 项 | 设计 |
|---|---|
| 可见性 | 仅 `accountKind=provider/platform` **且作用域数 > 1**；customer 永不渲染（`accountKind` 判定，不靠角色字符串） |
| 数据源 | `GET /api/v1/auth/tenants`（认证后；home ∪ 分配 ∪ 平台） |
| 位置 | `Header` 右侧（`Header.tsx:221-250` 区域）；当前作用域常驻文本（`tenantCode/name`） |
| 交互 | 选中 → `POST /api/v1/auth/switch-tenant` → 成功后：写入上下文 → 重拉 `/auth/me` → invalidate 菜单/能力缓存 → 取消在途请求 → 跳转目标作用域首页 |
| 失败 | 保持原作用域 + 明确提示；**不静默回退**；403 与网络错误区分文案 |
| 头通道 | 仅 MSP 聚合视图的**单请求只读**使用（`customerTenantId` 临时置入请求头，不写上下文） | 

> **2026-09-29 修订（R11）**：顶栏主控件改为 `CustomerFilter`（全局过滤器：多选/全部客户 + 计数徽标，只改视图不改会话）；`TenantSwitcher` 降级为"进入客户"的**深度操作入口**。跨客户工作台（列表带客户列 + 行内条目级操作，无需切换）见[跨客户工作台与全局过滤方案](./msp-cross-customer-workbench-and-filter-plan.md)。

### 6.3 切换后刷新链路（关键）

```text
switch-tenant 成功
  → auth-store.setCurrentTenant(目标)         // 上下文 + http-client 头
  → GET /api/v1/auth/me                       // 重建 user.permissions（后端按 DB×目标租户）
  → invalidate ['auth','menus']（按租户分键）  // 菜单重拉
  → invalidate capabilities/业务查询（按租户分键或全清）
  → 取消在途请求 + 路由回到目标作用域首页
```

- **queryKey 按租户分键**（`useUserMenusQuery.ts:20` → `['auth','menus',tenantId]`；`useCapabilities.ts:10-13` 同）；
- **登出**：`queryClient.clear()` + `resetSessionBootstrap()` + 断开 WS（补齐 §4 三个缺口）。

### 6.4 路由与守卫细化

1. 明确**单一权限模型**：页面/按钮级以 `hasPermission()` 为准；`lib/router/route-config.ts` 与 `RouteGuard` 二选一（建议：接入路由元数据，或删除遗留文件，避免并行模型）；
2. 补**独立 403 路由**（`/403`）供守卫跳转（替换内联 Result）；
3. `/admin/*` 与 `/msp/*` 增加**分组守卫**（`AdminRouteGuard` 语义扩展为"能力位判定"，MSP 页要求 `msp` 能力且已加载上下文）；
4. 切换进行中：全局阻塞业务请求（loading 屏障），避免"旧作用域请求打到新会话"。

### 6.5 页面级改造清单

| 页面/模块 | 改动 | 批次 |
|---|---|---|
| `pages/(auth)/login/index.tsx` | 保持无租户字段；补测试断言（DOM 无租户列表/选择器）；可选折叠"企业代码"（D1 决策后） | P0 |
| `lib/services/auth-service.ts` | 移除默认租户兜底；统一错误文案（与后端防枚举对齐） | P0 |
| 新增 `components/layout/header/TenantSwitcher.tsx` | 切换器 + 指示器（§6.2） | P0 |
| `Header.tsx` / `UserMenuDropdown.tsx` | 挂载切换器；上下文指示并入 | P0 |
| `lib/auth/session-bootstrap.ts` / `AuthGuard.tsx` | 移除 `tenants[0]`；尊重已选作用域；`resetSessionBootstrap` 接入登出 | P0 |
| `lib/api/tenant-api.ts` / `http-client.ts` | 端点修正；`X-Customer-Tenant-ID` 注入（仅头通道请求） | P0 |
| `lib/store/auth-store.ts` | 切换后重拉 `/auth/me`；登出清 queryClient + reset bootstrap | P0 |
| `useUserMenusQuery.ts` / `useCapabilities.ts` | queryKey 按租户分键 + invalidate | P0 |
| `pages/(main)/msp/**` | 客户维度收敛到全局上下文/头通道；去除局部 state 双源 | P1 |
| `pages/(main)/admin/users/index.tsx` + `user-api.ts` | 目标租户选择（按通道）+ 角色白名单（Q7/F3，去 super_admin/admin 可选项） | P1 |
| `pages/(auth)/register/index.tsx` | 角色选项走白名单接口（默认 end_user）；错误透传 | P1 |
| `routes/*` | 独立 403 路由；路由元数据接入或清理遗留；admin/msp 分组守卫 | P1 |
| `menu-config.ts` / `Sidebar.tsx` | `capabilityPathRules` 去重（单一来源） | P1 |

### 6.6 状态管理边界

- **持久化**：仅 `current_tenant_id/code`（+ rememberMe 语义）；`user/permissions` 不持久化（现状保持）；
- **hydration**：`auth-store.ts:321-345` 保留，但 hydration 后**必须探活复核**（`/auth/me`），失败即登出；
- **竞态**：切换时对在途请求打标（AbortController 或请求序号），避免旧响应覆盖新上下文。

---

## 7. 分期与验收

| 批次 | 内容 | 验收 |
|---|---|---|
| **P0** | 切换器 + 切换链路 + 上下文指示；`tenants[0]` 移除；登出清理补齐；queryKey 分键；端点/头修正；登录页防回归测试 | A1 服务商可在顶栏切换并立即生效（菜单/权限/数据全换）；A2 刷新页面保持所选作用域；A3 登出后重登不残留上一租户菜单/数据；A4 登录页 DOM 无任何租户列表；A5 客户账号无切换器节点 |
| **P1** | MSP 页面上下文收敛；管理页目标租户选择 + 角色白名单；路由/守卫收敛；403 路由；capabilityPathRules 去重 | A6 建号可选目标租户（按通道）且 `msp_*` 角色可选；A7 越权路由跳 403；A8 路由元数据单一模型 |
| **P2** | 切换器搜索/最近使用（依赖 `last_active` 排序）；页面级骨架屏与切换体验优化 | — |

---

## 8. 风险

| # | 风险 | 处置 |
|---|---|---|
| R1 | 后端权限双源（静态表 vs DBOnly）导致前端"看得见点不动" | 后端 B4 修复（登录/切换/`/auth/me` 统一 DB 计算）；前端切换后一律重建 permissions |
| R2 | 菜单缓存未按租户分键 → 跨租户残留 | P0 queryKey 分键 + 切换/登出 invalidate（已列入） |
| R3 | 切换竞态（在途请求旧作用域返回覆盖新状态） | 请求序号/Abort + 切换屏障 |
| R4 | 路由守卫并行模型（route-config/RouteGuard 遗留） | P1 明确单一模型并清理 |
| R5 | `AuthService` 兜底移除后，后端未返回租户时前端白屏 | 失败即登出 + 明确提示（不做本地兜底） |

---

## 附录：证据索引

- 路由/页面：`itsm-frontend/src/routes/index.tsx:185-389`、`src/routes/guards.tsx:18-47`、`src/layouts/MainLayout.tsx:125-131`
- 权限：`src/lib/store/auth-store.ts:20-27,138-157`、`src/lib/hooks/use-permissions.ts:35-97`、`src/components/auth/PermissionGuard.tsx:35-145`
- 菜单：`src/lib/api/menu-api.ts:59-107`、`src/lib/hooks/useUserMenusQuery.ts:20-60`、`src/components/layout/sidebar/Sidebar.tsx:49-67,179-265`
- 上下文/会话：`src/lib/auth/tenant-context.ts:14-78`、`src/lib/auth/token-storage.ts:15-149`、`src/lib/auth/session-bootstrap.ts:32-103`、`src/lib/api/http-client.ts:138-195`
- 页面：`src/pages/(auth)/login/index.tsx:103-327`、`src/pages/(auth)/register/index.tsx:128-272`、`src/pages/(main)/admin/users/index.tsx:50,130-145,597-605`、`src/pages/(main)/msp/index.tsx:38,60-115,470`
- 后端联动：`itsm-backend/handlers/common/service.go:67-91`、`middleware/rbac.go:497-506`、`service/menu_service.go:334-411`

## 修订记录

| 版本 | 日期 | 变更 |
|---|---|---|
| v0.1 | 2026-09-29 | 首版：前端路由/页面/权限/菜单/租户上下文现状盘点（HEAD `99eb4074`）+ 目标细化（上下文 v2、切换器、切换后刷新链路、路由守卫、页面改造清单、分期与验收、风险） |
