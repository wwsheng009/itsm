# MSP 多租户用户生命周期与租户切换方案

> 状态：Draft（待评审）｜日期：2026-09-29｜范围：用户创建 / 登录选租户 / 租户切换 / 相关权限与前端
> 关联：[ADR-004](../../architecture/adr-004-multi-customer-tenant-model-selection.md)、[07 已知缺口](../07-known-gaps.md)（G1–G10）、[06 实测记录 §7](../06-verification-and-troubleshooting.md)、[05 使用指南](../05-usage-guide.md)
> 证据来源：三个只读子代理调研（认证链路 / 用户与约束 / 前端）+ 主代理复核，逐条 file:line 见 [附录 A](#附录-a证据索引)。
> 作用域模型（Q1 回答 + ai-gateway 参考）：见 [服务方/客户方作用域模型分析](./msp-scope-model-analysis-and-ai-gateway-reference.md)。

---

## 0. 摘要（TL;DR）

**设计意图（ADR-004）**：客户 = 独立 `msp_customer` 租户，客户用户建在各自租户内；服务方 = 一个 `msp_provider` 租户，员工经 `MSPAllocation` 跨客户，通过 `X-Customer-Tenant-ID` 或**租户切换**操作客户数据。

**现状结论**：链路"半成品"——切换租户的后端骨架（`POST /auth/switch-tenant`、`GET /auth/tenants`）已存在，但**建号能力被写路径守卫阻断、登录不按租户解析、切换语义有三处硬伤、前端完全没有入口**，导致实际运营只能靠 SQL 兜底（见 06 文档 §7）。

**四类功能缺口（F1–F15，详见 §3）**：

| 类别 | 编号 | 一句话 |
|---|---|---|
| 建号 | F1–F4 | 跨租户建号无合法通道；新租户首个用户无引导（固定 `admin@example.com` 撞全局唯一）；注册接口角色无白名单（可注入 `super_admin`）；无邀请/入职流程 |
| 登录 | F5–F8 | 登录不校验租户状态；`tenantCode` 不影响签发；无"多租户身份"模型；无租户选择/自动选择 |
| 切换 | F9–F12 | `GET /auth/tenants` 只返回主租户；**refresh 令牌按主租户重签导致切换被静默回退**；切换后响应/角色/权限不一致；无审计与旧令牌撤销 |
| 运营与前端 | F13–F14 | 前端无租户切换器与客户上下文指示；MSP 员工在客户租户内的角色权限未定义 |

**已定方案（2026-09-29 决策）**：

- **P0 = 路线 A 最小闭环**：保持 `users.tenant_id` 主租户 + `msp_allocations` 作为运营侧作用域来源，补齐**建号通道 / 登录解析 / 切换修复 / 前端入口**四件事（不触碰唯一约束与既有数据）；
- **P1 = 路线 B 转正（目标模型）**：引入 `user_tenant_memberships`——"**一个账号、多作用域**"；客户方单作用域由**数据库部分唯一索引**强约束，服务方作用域 = provider 租户 + 有效分配；
- **Q7 采用客户级角色差异化**：客户租户模板内置 4 个 msp 角色，客户 admin 可编辑其权限集；不引入 grants 表；
- 决策明细见 [§9.2 决议记录](#92-决议记录2026-09-29)，依据见[作用域模型分析](./msp-scope-model-analysis-and-ai-gateway-reference.md)。

---

## 1. 设计意图与既有约定

### 1.1 ADR-004 的口径

- 客户 = 独立租户；客户用户建在自己租户内，"无需跨租户机制"（ADR-004 A6）；
- 服务方员工集中在 `msp_provider` 租户，经 `MSPAllocation`（primary/backup/specialist）授权到多个客户租户；
- A10 验收项⑤明确要求"**切换租户后 JWT 的 `tenant_id` 正确**"；
- 明确**否决**"同租户内用 `customer_id` 做客户隔离"。

### 1.2 07 文档已登记的缺口（本次方案的输入）

| 缺口 | 与本文关系 |
|---|---|
| G1 跨租户创建用户被写路径守卫拦截（super_admin 亦被拦） | → F1，§5.2 给出"显式建号通道 + 收口 bypass" |
| G2 bootstrap 固定 `admin`/`admin@example.com` + 全局唯一 → 第二个租户必冲突 | → F2，§5.2.4 给出租户级引导身份 |
| G3 MSP 管理员有效角色为 `msp_manager`（rank 0）→ 无法用用户管理 API | → F1/F14，§5.2.3 + §5.5.2 |
| G8 缓存 key 无租户维度 | 本文不展开，P2 前置项（§7） |
| G9 `X-Tenant-Code` 与 JWT 冲突被静默忽略 | → F6 相关；§5.3.1 统一解析顺序 |
| G10 snap docker `/tmp` 不可见 | 运维侧已规避，与本文无关 |

### 1.3 本次调研对既有假设的两处纠正

1. `internal/schema/tenant_guard.go` **不是**请求级操作拦截器，而是**DB 表结构豁免清单 + 启动自检**（`tenant_guard.go:117-240`）。真正拦截跨租户建号的是：
   - 请求层：`handlers/user/handler.go:61-71`（跨租户仅允许 `super_admin`，其余 403 "无权限跨租户创建用户"）；
   - 写路径：`database/security.go:94-131`（Ent hook 校验 `tenant_id` 与请求上下文一致，不一致直接 `cross-tenant insert blocked`；豁免口径是 `tenantctx.IsSystemBypass(ctx)` **或没有租户上下文**）。
2. 因此 G1 的根因不是"角色不够高"，而是**缺少一条被显式授权、可审计的跨租户写入通道**：`super_admin` 在 API 层被允许，但写路径仍按调用者租户上下文拒绝。

---

## 2. 现状盘点

### 2.1 身份与归属模型

| 事实 | 证据 |
|---|---|
| `users.tenant_id` 必填正整数；`tenant` 边 `Required().Unique()` → **一个用户恰属一个租户** | `ent/schema/user.go:59-61, 89-93` |
| `users.username` / `users.email` 为**列级全局唯一**（无 `(tenant_id, email)` 复合索引） | `ent/schema/user.go:21-28`；`ent/migrate/schema.go:5708-5709`（无 Indexes 块） |
| 角色为 M2M（`user_roles` 连接表），该表**无 tenant_id 且被治理层豁免**（平台级 RBAC） | `ent/migrate/schema.go:6280-6301`；`internal/schema/tenant_guard.go:83` |
| `roles.code` 无唯一约束（仅业务查询 `CodeEQ + TenantIDEQ`） | `ent/schema/role.go:19-24`；`service/user_service.go:139-146` |
| `msp_allocations` **无 `(msp_user_id, customer_tenant_id)` 唯一约束**，可重复分配 | `ent/schema/msp_allocation.go:19-31`；`ent/migrate/schema.go:2717-2735` |
| 用户 `msp_role` 取值仅 `provider_admin / provider_agent / customer_user` | `ent/schema/user.go:69-72` |

**结论**：现状是"**一租户一账号**"+"运营侧靠分配表获得跨租户访问"，不存在成员关系（membership）表；`user_roles` 虽平台级，但角色本身带 `tenant_id`，事实上可表达"跨租户角色持有"，只是没有任何一处逻辑按此解析。

### 2.2 登录（`POST /api/v1/auth/login`）

| 行为 | 现状 | 证据 |
|---|---|---|
| 租户解析 | `tenantCode` 查不到租户时**静默忽略**（`tenantID` 保持 0，不报错） | `handlers/common/service.go:93-100` |
| 用户定位 | 无租户时**按 username 全局查**（`.Only` 要求唯一）；有租户时 username+tenant 双条件 | `handlers/common/service.go:101-122` |
| 租户状态 | **不校验**目标/所属租户的 `status` 与 `expiresAt` | `handlers/common/service.go:131-139`（仅校验密码与 `active`） |
| 签发租户 | **恒为用户 home 租户**（`u.TenantID`），请求里的 `tenantCode` 只影响"找到谁" | `handlers/common/service.go:141-159` |
| MSP 角色映射 | 有：`provider_admin→msp_manager`、`provider_agent→msp_tech`、`customer_user→end_user`、未知→`msp_viewer` | `middleware/msp_rbac.go:18-30`；`handlers/common/service.go:144-148` |
| 注册 | `POST /auth/register`：全局查重 username/email；`tenantCode` 缺省仅当"启用租户恰好 1 个"；**`role` 直接取请求值原样落库（枚举内任意值，含 `super_admin`）** | `handlers/auth/service.go:166-210` |

### 2.3 租户切换（`POST /api/v1/auth/switch-tenant`）

已实现：native（目标==home）/ `super_admin` / MSP 分配（需 `msp_role` 非空 + 源租户为 provider + 目标为 customer + 有效 allocation）三选一；重签 access+refresh 并把目标租户写入 JWT；目标租户需 `active` 且未过期（`handlers/auth/service.go:114-157`）。

但存在**四处硬伤**：

| # | 问题 | 证据 |
|---|---|---|
| a | **refresh 回退**：`RefreshToken` 忽略 `claims.TenantID`，按 `user.TenantID`（home）重签 → 切换后一旦刷新即静默回到主租户 | `handlers/common/service.go:172-198` |
| b | **响应不一致**：`LoginResponse.User.TenantID` 仍返回 home 租户（与 JWT/`Tenant` 字段矛盾） | `handlers/auth/service.go:158-163` |
| c | **角色不一致**：切换时用 `string(userEntity.Role)` 原样签发（未做 MSP 映射），而登录时已映射 → 同一 MSP 员工两条路径角色不同 | `handlers/auth/service.go:150-157` vs `handlers/common/service.go:144-148` |
| d | **无审计/无旧令牌撤销**：切换不写审计、不撤销切换前的 refresh；错误一律映射 403 | `handlers/auth/handler.go:92-110` |

### 2.4 建号能力

| 通道 | 现状 | 证据 |
|---|---|---|
| 同租户 API | `POST /api/v1/users`：`super_admin` 可用 `req.tenantId` 覆盖目标租户；其余角色跨租户 403；`roleIds` 走 `CanGrantRoles` | `handlers/user/handler.go:46-79` |
| 写路径 | Ent hook 拒绝"上下文租户 ≠ 目标 tenant_id"的插入；**唯一豁免是 `IsSystemBypass` 或无租户上下文** | `database/security.go:104-131` |
| 服务层 | 全局查重 username/email；`SetTenantID(tenantID)`；`msp_role` 可选 | `service/user_service.go:48-136` |
| 引导（bootstrap） | 消费 token 时**写死 `admin` / `admin@example.com` / `super_admin`**；seeder 预检查按租户查（看不到别家租户的 admin）→ 第二个租户必然撞全局唯一 | `pkg/bootstrap/token.go:141-154`；`pkg/seeder/seeder.go:849-863, 887-896`；`cmd/initialize/main.go:98-115`；`router/bootstrap_routes.go:53-73` |
| MSP 侧 | `/api/v1/msp/*` 仅 9 个端点（status/context/allocations×3/customers/tickets/report×2），**没有任何用户管理端点** | `router/msp_routes.go:13-35` |

### 2.5 前端现状（itsm-frontend，Vite + React Router SPA）

| 能力 | 现状 | 证据 |
|---|---|---|
| 登录表单 | 仅 username/password，**无租户输入**；提交时 `tenantCode=undefined` | `src/pages/(auth)/login/index.tsx:146,158-163,280-301` |
| API 层 | `AuthService.login` 已支持 `tenantCode` 参数 | `src/lib/services/auth-service.ts:242-260` |
| 租户状态 | 登录后保存 `currentTenant`，并注入 `X-Tenant-ID` / `X-Tenant-Code` 请求头 | `src/lib/store/auth-store.ts:60-74,111-119`；`src/lib/api/http-client.ts:179-192` |
| 会话启动 | `bootstrapSession` 固定取 `tenants[0]` 作为当前租户（**无选择**） | `src/lib/auth/session-bootstrap.ts:61-64,92-103` |
| 租户切换 | **无任何切换入口/调用**；`/auth/tenants` 只返回 1 个租户，即使有 UI 也无从选择 | 同上 + `handlers/common/service.go:308-334` |
| 403 处理 | 无通用处理（仅 CSRF 重试一次） | `src/lib/api/http-client.ts:267-271` |

### 2.6 能力矩阵（现状 → 目标）

| 能力 | 现状 | 目标（P0） |
|---|---|---|
| 平台管理员在任意租户建号 | ❌ 被写路径拦截 | ✅ 平台通道（显式 bypass + 审计） |
| MSP 管理员在被分配客户租户建号 | ❌ 403 | ✅ MSP 通道（allocation 校验） |
| 客户管理员在本租户建号 | ⚠️ 可建，但全局唯一与角色规则有坑 | ✅ 租户内唯一 + 角色白名单 |
| 新租户首个管理员 | ⚠️ 固定 admin 撞车，靠 SQL | ✅ 租户级引导身份 + 邀请 |
| 登录指定/自动选择租户 | ❌ 忽略语义 | ✅ 解析顺序 + 记忆租户 + 候选列表 |
| 登录后切换租户 | ⚠️ 后端可用，refresh 回退 | ✅ 修复语义 + 前端切换器 |
| MSP 员工在客户租户内工作 | ⚠️ 仅 msp_* 权限，客户业务权限未定义 | ✅ 模板化授权（P1） |
---

## 3. 功能缺口清单

> 优先级：P0 = 阻塞 MSP 多客户运营（本次方案第一批次）；P1 = 体验/合规补全；P2 = 演进项。
> 每条缺口给出：现象 → 证据 → 影响 → 目标。

### F1 跨租户建号没有合法通道（P0）

- **现象**：MSP 管理员调 `POST /api/v1/users`（带 `tenantId=客户租户`）→ 403 "无权限跨租户创建用户"；平台 `super_admin` 虽在 handler 层放行，但写路径仍被 Ent hook 以 `cross-tenant insert blocked` 拒绝（实测见 06 §7 / G1）。
- **证据**：`handlers/user/handler.go:61-71`；`database/security.go:104-131`；`handlers/user/handler.go:515-531`（`roleRank` 仅用于角色高攀校验，不涉及租户）。
- **影响**：客户租户的账号只能由 DBA 用 SQL 创建（本次生产即如此），MSP 无法自助开户，运营不可持续；且绕过审计。
- **目标**：新增**显式建号通道**（平台通道 / MSP 通道），bypass 收口在服务层（不散落 handler），全程审计（actor、target_tenant、channel、role）。

### F2 新租户首个用户没有引导机制（P0）

- **现象**：bootstrap 消费 token 时写死 `admin` / `admin@example.com` / `super_admin`；seeder 的"是否已存在"检查按租户过滤，看不到其他租户的 `admin` → 第二个租户插入时撞 `users.username` 全局唯一，报错或被跳过。
- **证据**：`pkg/bootstrap/token.go:141-154`；`pkg/seeder/seeder.go:849-863, 887-896`；`cmd/initialize/main.go:98-115`；`router/bootstrap_routes.go:53-73`；唯一约束 `ent/schema/user.go:21-28`。
- **影响**：新客户租户无法通过产品自身完成"开租户 → 有管理员 → 能登录"的闭环（G2）。
- **目标**：租户级引导身份（`admin-<tenantCode>` 或自定义）+ 一次性 token + **首登强制改密**；`provision_tenant` 支持 `-admin-username/-admin-email` 并默认生成租户唯一身份。

### F3 注册接口角色无白名单（P0，安全）

- **现象**：`POST /api/v1/auth/register` 把请求里的 `role` 原样落库，枚举内任意值（含 `super_admin`）都被接受；`msp_role` 无注入但角色可提权。
- **证据**：`handlers/auth/service.go:202-206`；路由 `router/router.go:305-324`（公开注册端点）。
- **影响**：任何能访问注册端点的人可自建 `super_admin`（若租户策略未关闭注册）→ 权限提升/接管。
- **目标**：注册角色白名单（默认仅 `end_user`，可被租户策略收紧）；`msp_role` 仅允许由受信通道（MSP/平台）设置；补回归测试。

### F4 没有邀请 / 入职流程（P1）

- **现象**：管理员建号需自行设定初始密码并线下传递；无邀请 token、无首登改密、无邮箱验证、无失效控制。
- **证据**：`handlers/user/handler.go:38-102`（创建即设密码）；`dto/user_dto.go:8-25`（无邀请字段）。
- **影响**：密码分发不可审计、离职/换岗无回收链路；与"客户自助开通"目标冲突。
- **目标**：邀请流（一次性 token + 租户 + 角色 + 有效期）→ 落地页设置密码 → 首登审计；邀请可撤销。

### F5 登录不校验租户状态（P0）

- **现象**：登录只校验密码与用户 `active`，**不校验租户 `status` / `expiresAt`**；被暂停/过期的租户仍能登录拿到 token（后续请求会被 `TenantMiddleware` 403，但登录语义错误，且 refresh 仍可续签）。
- **证据**：`handlers/common/service.go:131-139`（无租户校验）对比 `handlers/auth/service.go:144-149`（切换时有校验）与 `middleware/tenant.go:160-169`。
- **影响**：停租户后客户端仍显示"登录成功"，错误延迟到业务请求；审计与合规口径不一致。
- **目标**：登录时校验目标租户 `active` 且未过期，返回明确错误（`TENANT_SUSPENDED` / `TENANT_EXPIRED`）。

### F6 `tenantCode` 语义不闭环（P0）

- **现象**：`tenantCode` 查不到租户时**静默忽略**，登录退化为"按 username 全局查"，签发**恒为用户主租户**；MSP 员工带客户 `tenantCode` 登录仍拿 provider 上下文 token，调用方误以为已切客户。
- **证据**：`handlers/common/service.go:93-100, 141-159`。
- **影响**：与 G9（`X-Tenant-Code` 冲突被静默忽略）同类的"静默降级"，排障困难；MSP 场景下极易误操作。
- **目标**：① 租户无效 → 明确报错；② MSP 员工带可访问客户 `tenantCode` → 直接签发客户上下文；③ 冲突/不可访问 → 明确拒绝并记录。

### F7 缺少"多租户身份"模型（P1，路线 B 已采纳）

- **现象**：`users.tenant_id` 1:1；无成员关系表；`user_roles` 虽平台级但无解析逻辑。客户用户无法加入第二个租户；同一自然人不能既是 MSP 员工又是客户联系人；同一邮箱邀请到两个租户会被全局唯一挡住。
- **证据**：`ent/schema/user.go:59-61, 89-93`；`ent/migrate/schema.go:6280-6301`；`internal/schema/tenant_guard.go:83`。
- **影响**：集团顾问、跨法人审批、外包/驻场等真实场景无解；客户合并/迁移困难。
- **目标**：P1 先放开 `email` 租户内唯一 + 登录候选选择；P2 引入 `user_tenant_memberships`（路线 B，见 §6）。

### F8 登录无租户选择 / 自动选择（P0）

- **现象**：前端登录表单无租户字段（`tenantCode=undefined`）；后端登录响应不含"可访问租户集合"；`bootstrapSession` 固定取 `tenants[0]`。
- **证据**：`src/pages/(auth)/login/index.tsx:146,158-163,280-301`；`src/lib/auth/session-bootstrap.ts:61-64,92-103`；`handlers/common/service.go:308-334`。
- **影响**：多租户身份用户无法选择进入哪个租户；MSP 员工只能进 provider 租户再手工调接口切换。
- **目标**：登录解析顺序（§5.3.1）+ 自动选择（记忆租户）+ 候选列表返回 + 前端选择器。

### F9 `GET /api/v1/auth/tenants` 语义错误（P0）

- **现象**：只按 `user.TenantID` 返回一个租户，与"用户所属租户列表"语义不符；前端拿不到可切换目标。
- **证据**：`handlers/common/service.go:308-334`；`router/common_system_routes.go:22`。
- **影响**：切换 UI 无从构建；即便有 UI 也只能"切回自己"。
- **目标**：返回 `home ∪ allocations（∪ memberships）`，每项含 `id/code/name/type/status/source/role`（source：home|allocation|membership）。

### F10 refresh 令牌导致"切换被静默回退"（P0，功能+安全）

- **现象**：`RefreshToken` 忽略 `claims.TenantID`，按 `user.TenantID`（主租户）重签 access+refresh；切换租户后任意一次刷新都会把上下文退回主租户。
- **证据**：`handlers/common/service.go:172-198`。
- **影响**：MSP 员工在客户租户中操作时"莫名切回 provider"，跨租户写入可能落到错误租户；安全上是越权面的温床。
- **目标**：refresh 以 `claims.TenantID` 为准并复核用户对该租户的可访问性；切换时轮换 refresh 并撤销旧 jti；不可访问 → 401 而非静默重签。

### F11 切换后响应 / 角色 / 权限不一致（P0）

- **现象**：① `LoginResponse.User.TenantID` 返回主租户（与 JWT 矛盾）；② 切换时角色未做 MSP 映射（登录时已映射）；③ 权限列表来自静态 `middleware.RolePermissions`，而运行模式为 `PermissionConfigModeDBOnly`（DB 权威）。
- **证据**：`handlers/auth/service.go:150-163` vs `handlers/common/service.go:144-148`；`handlers/auth/service.go:96-112`；`middleware/rbac.go:498-506`。
- **影响**：前端显示租户/角色错误；权限判断与实际授权不一致（可能显示多余或缺失权限）。
- **目标**：切换响应以"当前租户"为准；角色统一走映射函数；权限按目标租户从 DB 解析（缺失回退静态表并告警）。

### F12 切换无审计、无旧令牌撤销（P0/P1）

- **现象**：`SwitchTenant` 不写审计事件、不撤销切换前的 refresh token；handler 把所有业务错误压成 403（无法区分"未认证/无权限/租户不存在"）。
- **证据**：`handlers/auth/handler.go:92-110`。
- **影响**：合规审计缺失（跨租户操作无留痕）；旧令牌在有效期内仍可续签回主租户（与 F10 叠加）。
- **目标**：审计 `tenant.switch`（actor、from/to、结果、IP、UA）；错误码细分；切换即轮换并撤销旧 refresh。

### F13 前端无租户入口与上下文指示（P0）

- **现象**：无登录租户输入、无租户切换器、无 MSP 客户上下文选择器；`X-Customer-Tenant-ID` 未在前端注入（MSP 页面若要用需手填）；切换后未清理页面级缓存。
- **证据**：登录页/`auth-store`/`session-bootstrap`/`http-client`（§2.5）。
- **影响**：MSP 运营在 UI 上不可用，只能靠 API/脚本；用户不知道自己当前处于哪个租户。
- **目标**：登录页可选租户 → 顶栏租户切换器（含"当前租户"徽标）→ MSP 客户选择器（写入 `X-Customer-Tenant-ID` 或调用切换）→ 切换后清缓存并重载数据。

### F14 MSP 员工在客户租户内的权限未定义（P1）

- **现象**：`msp_*` 角色的静态权限只有 `msp/msp_customer/msp_ticket/msp_allocation/msp_report`（无客户业务资源）；客户租户模板是否含 `msp_*` 角色及其 `role_permissions` 未确认；切换进客户租户后做客户业务（工单/知识库/CMDB）没有明确授权依据。
- **证据**：`middleware/rbac.go:437-481`；`middleware/rbac.go:498-506`（DB-only）；`router/msp_routes.go:13-35`（仅 msp_* 端点）。
- **影响**：切换租户"能进去但干不了活"；或被迫临时提权（admin）破坏最小权限。
- **目标**：在客户租户模板中内置 `msp_manager/msp_tech/msp_viewer` 角色与**受控的客户业务权限**（建议：工单 read/write、知识库 read、CMDB read；写操作按 ADR 评审），由 `provision_tenant` 幂等供给；MSP 分配角色（primary/backup/specialist）映射到这些角色。

### F15 跨租户两条通道并存且语义不统一（P1）

- **现象**：`X-Customer-Tenant-ID`（单请求头，MSP 中间件校验分配）与"切换租户后的 JWT"（`tenant_id` 已改）两条路径并行；前者不改上下文（审计/RLS 仍按 provider 租户？），后者改上下文但不撤销旧令牌；两者的权限解析、审计、缓存口径均未统一。
- **证据**：`middleware/msp_middleware.go:88-168`；`handlers/auth/service.go:114-157`；`middleware/tenant.go:26-31`（来源优先级）。
- **影响**：同一业务操作有两种"当前租户"来源，排障与合规口径分裂；G9 正是该分裂的表现之一。
- **目标**：明确推荐路径（**连续操作走切换、单请求走头**），并在中间件层统一"有效租户"的推导与审计字段（`tenant_source`）。
---

## 4. 方案总览与选型

### 4.1 已定路线（A → B 递进）

| | 路线 A（P0，最小闭环） | 路线 B（P1，目标模型） |
|---|---|---|
| 身份模型 | 保持 `users.tenant_id` 1:1 主租户 | 新增 `user_tenant_memberships`（一个账号、多作用域） |
| 跨租户访问来源 | `msp_allocations` + `super_admin` | memberships（= home ∪ allocations 的物化） |
| 客户用户跨租户 | ❌ 不支持 | ❌ 仍不支持（**DB 级单作用域约束**） |
| 服务方跨租户 | ⚠️ 隐式（仅分配，无作用域语义） | ✅ 显式作用域（可切换、可审计、可到期） |
| 改动面 | 认证/用户服务 + `users.last_active_tenant_id` | + 成员表 + 上下文扩展 + 4 角色模板 |
| 风险 | 低（不碰唯一约束与既有数据） | 中（新表 + 回填 + 会话复核；**不涉及唯一约束迁移**） |
| 交付 | 2–3 批次 | P1 首批 |

### 4.2 决策要点（2026-09-29）

1. **路线 B 由"可选演进"转正为 P1 首批**：它是"账号只有一个、作用域不同"的落地形态（分析文档 §0.2），P0 的 `AccessibleTenants` 抽象已为其预留接口；
2. **客户方单作用域**用 `uq_customer_single_scope` 部分唯一索引做**数据库级**强约束（不靠应用自觉）；
3. **不迁移唯一约束**：保持 `users.username/email` 全局唯一（客户方不跨租户 → 原 P1-2 取消）；
4. **Q7 采用客户级角色差异化**（4 个 msp 角色模板 + 客户 admin 可编辑权限），不引入 `membership_scope_grants` 表（B 方案留触发条件，见分析文档 B.8）；
5. **客户方登录 fail-closed**：多作用域命中不自动选择（对齐 ai-gateway），仅服务方允许显式选择/切换。

### 4.3 不可变约束

1. 不引入 `customer_id` 业务维度做隔离（ADR-004 否决项）；
2. 跨租户写入必须**显式授权 + 可审计**，禁止扩大 `IsSystemBypass` 的使用面（当前仅 seeder/provisioner 使用）；
3. 任何"当前租户"的判定必须收敛到**单一函数**（§5.1），不得在 handler 各自推导。
4. **客户账号永不跨租户**（DB 级约束）；服务方不得自我扩权（含 `msp_manager`）。

---

## 5. 方案 A 详细设计（P0）

### 5.1 统一"可访问租户集合"抽象

新增 `service/tenant_access.go`（新文件，单一真相源）：

```go
type TenantAccess struct {
    TenantID int
    Source   string // home | allocation | platform
    Role     string // 在该租户的有效角色（已映射）
}
// AccessibleTenants 返回用户可进入的租户集合（home ∪ 有效 allocations ∪ platform 全量）
func (s *Service) AccessibleTenants(ctx context.Context, userID int) ([]TenantAccess, error)
// CanAccessTenant 判定单一租户（供登录/切换/refresh 共用）
func (s *Service) CanAccessTenant(ctx context.Context, userID, tenantID int) (TenantAccess, error)
```

改造点（**替换四处各自实现**）：

| 位置 | 现状 | 改为 |
|---|---|---|
| `handlers/auth/service.go:114-139`（SwitchTenant 授权） | 内联 native/superAdmin/allocation 判断 | `CanAccessTenant` |
| `handlers/common/service.go:308-334`（GetUserTenants） | 只返回 home | `AccessibleTenants` |
| `handlers/common/service.go:172-198`（RefreshToken） | 按 home 重签 | `CanAccessTenant(claims.TenantID)` |
| `handlers/common/service.go:141-159`（Login 签发） | 恒 home | §5.3.1 解析结果 |

### 5.2 建号能力矩阵与通道

#### 5.2.1 通道定义

| 通道 | 调用方 | 目标租户 | 授权校验 | 写路径 bypass |
|---|---|---|---|---|
| `platform` | `super_admin` / `sysadmin` | 任意 `active` 租户 | `user:write` + 平台角色 | `tenantctx.WithProvisioningBypass(ctx, actor, "platform")` |
| `msp` | `provider_admin`（有效角色 `msp_manager`） | **仅**其有效 allocation 的客户租户 | `msp_customer:write` + allocation | 同上，`reason="msp:<providerTenantID>"` |
| `tenant` | 租户内 `admin`（含客户管理员） | 本租户 | 现有 `roleRank` + `CanGrantRoles` | 不需要 |
| `invite` | 持邀请 token 者 | 邀请指定租户 | token 校验（一次性、未过期） | 一次性、限定角色 |

#### 5.2.2 服务层收口（关键设计）

新增 `service/provisioning.go`：

```go
// ProvisionUser 在目标租户创建用户。
// bypass 只在本函数内构造：必须携带 actor 与 reason，便于审计与追责。
func (s *UserService) ProvisionUser(ctx context.Context, actor ActorRef, target int, in CreateUserInput) (*ent.User, error) {
    access, err := s.tenantAccess.CanAccessTenant(ctx, actor.UserID, target) // 或 platform 判定
    if err != nil { return nil, err }                                       // 403/404
    if err := s.authorizeChannel(actor, access, in); err != nil { ... }     // 角色白名单 + 高攀
    ctx = tenantctx.WithProvisioningBypass(ctx, actor, "channel="+access.Source)
    ctx = tenantctx.WithTenantID(ctx, target)                               // 注入目标租户（RLS 亦按此）
    return s.createUser(ctx, target, in)                                    // 复用现有唯一性/角色校验
}
```

- `database/security.go:104-106` 的 `IsSystemBypass` 语义不变，新增的 `WithProvisioningBypass` **只允许 service 层调用**（handler 直接调用视为违规，建议加静态检查/评审清单）；
- bypass 必须同时注入目标租户 ID（`WithTenantID`），保证 RLS（enforce 后）仍按目标租户过滤，而不是"跳过过滤"。

#### 5.2.3 API 契约

| 端点 | 方法 | 通道 | 权限 | 说明 |
|---|---|---|---|---|
| `/api/v1/users` | POST（扩展） | tenant / platform | `user:write` | 保持兼容；`tenantId` 语义按通道判定 |
| `/api/v1/tenants/:id/users` | POST（新增） | platform | `tenant.write` + `user:write` | 平台侧语义明确 |
| `/api/v1/msp/customers/:customer_tenant_id/users` | POST（新增） | msp | `msp_customer.write` | **MSP 前端主用**；校验 allocation |
| `/api/v1/users/invitations` | POST（P1） | tenant / msp / platform | 同上 | 返回一次性链接 |
| `/api/v1/auth/invitations/:token` | GET/POST（P1） | invite | 公开 | 校验 + 设置密码 |

请求/响应要点：

```jsonc
// 请求（新增字段以 * 标注）
{ "username": "custa_admin", "email": "admin@custa.example.com", "name": "客户A管理员",
  "password": null,            // 可为空 → 生成邀请（P1）；P0 必填
  "tenantId": 4,               // * 目标租户
  "role": "admin",             // 客户侧角色
  "roleIds": [12, 15],
  "mspRole": null }            // 仅 MSP 员工可为 provider_*/customer_user
// 响应
{ "code": 0, "data": { "id": 8, "tenantId": 4, "channel": "msp", "createdVia": "msp", "invited": false } }
```

错误码（新增，登记到 `common/response.go` 双 switch）：

| 码 | HTTP | 场景 |
|---|---|---|
| `CROSS_TENANT_FORBIDDEN` | 403 | 非平台/MSP 通道跨租户建号 |
| `MSP_ALLOCATION_REQUIRED` | 403 | MSP 通道但无有效分配 |
| `ROLE_NOT_GRANTABLE` | 422 | 目标角色不属于目标租户或 rank 过高 |
| `MSP_ROLE_NOT_ALLOWED` | 422 | msp_role 与角色组合非法 / 非 MSP 通道设置 msp_role |
| `USERNAME_EXISTS` / `EMAIL_EXISTS` | 409 | 唯一性冲突（附目标租户上下文） |
| `TENANT_NOT_FOUND` / `TENANT_SUSPENDED` | 400/403 | 登录/切换的租户校验 |

#### 5.2.4 唯一性与首个用户引导

**P0（不动唯一约束）**：

- 引导身份租户化：`admin-<tenantCode 小写>`；email `admin+<tenantCode>@<domain|example.com>`；`cmd/initialize` / HTTP bootstrap 支持 `-username/-email` 覆盖（默认值即上述规则）；
- `provision_tenant` 新增 `-admin-username/-admin-email/-admin-password-stdin`，并在创建后置 `must_change_password=true`；
- 新列 `users.must_change_password bool default false`；登录响应带 `mustChangePassword`，前端强制跳转改密页。

**P1（多作用域身份；Q3 已定：不放开同邮箱多租户）**：

- **保持** `users.username/email` 全局唯一，**不做唯一约束迁移**（客户方不跨租户；服务方多作用域由 membership 表达，与邮箱唯一性无关）；
- 登录支持 `identifier`（username 或 email）；命中多作用域时按 §5.3.1 分派：客户方 fail-closed，服务方 `409 SCOPE_SELECTION_REQUIRED` + 候选列表。

### 5.3 登录与租户选择（自动切换租户）

#### 5.3.1 解析顺序（替代现状）

1. **显式租户**：`tenantCode`（或域名 / `X-Tenant-Code`）→ 无效则 `400 TENANT_NOT_FOUND`（**不再静默忽略**，修 F6）；
2. **身份定位**：username（全局唯一）或 email；
3. **租户校验**：目标租户 `active` 且未过期，否则 `TENANT_SUSPENDED` / `TENANT_EXPIRED`（修 F5）；
4. **作用域选择**（无显式租户时，按 `account_kind` 分派，对齐 ai-gateway fail-closed 原则）：
   - **客户方**：唯一作用域直接进入；**多作用域命中 → `409 SESSION_AMBIGUOUS`（不自动选，属数据异常）**；
   - **服务方**：`last_active`（若仍是 active 作用域）→ 否则 provider home；命中多个且无偏好 → `409 SCOPE_SELECTION_REQUIRED` + 候选列表（登录页一次性选择，选定即锁定会话）；
   - **平台**：进入治理模式（先显式选择目标租户）；
5. **冲突**：显式租户不可访问 → `401 TENANT_MISMATCH` 并审计（对齐 G9 的 fail-closed 建议）；
6. **签发**：以**最终目标租户**签发 access+refresh（修 F6 的"恒 home"），并按 membership 复核（P1-a 后）。

#### 5.3.2 契约（向后兼容的增量字段）

```jsonc
{
  "user": { "...": "...", "tenantId": 4 },        // 当前生效租户（不再恒为 home）
  "tenant": { "id": 4, "code": "MSPCUSTA", "...": "..." },
  "tenantSelection": { "mode": "explicit|last_active|single", "autoSelected": true, "reason": "last_active" },
  "availableTenants": [
    { "id": 3, "code": "MSP001",  "type": "msp_provider", "source": "home",       "role": "msp_manager" },
    { "id": 4, "code": "MSPCUSTA","type": "msp_customer", "source": "allocation", "role": "msp_tech" }
  ]
}
```

多作用域歧义时**不返回 200**，而是 `409`：

```jsonc
{ "code": "SCOPE_SELECTION_REQUIRED", "scopeCandidates": [ { "id": 4, "code": "MSPCUSTA", "role": "msp_tech" } ] }
```

#### 5.3.3 记忆与偏好

- 新增列 `users.last_active_tenant_id int null`（登录/切换成功时更新；`CanAccessTenant` 复核后才采用）；
- 前端 `localStorage.current_tenant_id/code` 继续使用（已有），登录时以服务端返回为准覆盖。

### 5.4 租户切换（修复 + 完善）

| 修复项 | 设计 |
|---|---|
| 响应一致性（F11a） | `LoginResponse.User.TenantID = 当前租户`；`Tenant` 字段保持目标租户 |
| 角色一致性（F11b） | 切换与登录统一走 `GetMSPRBACRole` 映射后签发；映射表下沉到 `domain/role` 单一源 |
| 权限一致性（F11c） | 权限列表按目标租户解析：DB `role_permissions` 优先，缺失回退静态表并打 `warn` 日志 |
| refresh 语义（F10） | refresh 校验 `claims.TenantID` 且 `CanAccessTenant` 通过才重签；否则 401 `TENANT_ACCESS_REVOKED` |
| 令牌轮换（F12） | 切换时把旧 refresh `jti` 写入撤销表（复用 `middleware.RevokeAccessToken` 机制，扩展 token_type）；响应下发新 access+refresh |
| 审计（F12） | 事件 `tenant.switch`：`actor_user_id / from_tenant / to_tenant / source(home\|allocation) / result / ip / ua` |
| 错误码（F12） | 细分：401 未认证、403 无权限（`TENANT_FORBIDDEN`）、404 租户不存在、409 租户暂停/过期 |

**与 `X-Customer-Tenant-ID` 的分工（F15）**：单请求只读/列表 → 头通道（保留）；需要连续操作/写 → 切换租户。中间件为两种路径统一写入 `tenant_source`（`jwt|header`）并进入审计字段，文档同步更新 05 使用指南。

### 5.5 权限与安全

#### 5.5.1 角色白名单与高攀（修 F3）

- `POST /auth/register`：角色白名单（默认 `["end_user"]`，可由租户 `settings.self_registration` 收紧为关闭/指定角色）；**显式拒绝** `super_admin/sysadmin/admin`；
- `POST /users`（各通道）：角色必须属于目标租户（复用 `service/user_service.go:150-167`）；rank 比较以"调用者在**目标租户**的 rank"为准（MSP 员工在客户租户的 rank 由映射角色决定，见 5.5.2）；
- `msp_role` 仅允许：`platform`/`msp` 通道设置，且 `provider_*` 仅限 provider 租户、`customer_user` 仅限客户租户。

#### 5.5.2 MSP 员工在客户租户内的有效权限（F14，P1；Q7 已定）

客户租户模板内置 **4 个 msp 角色**（`pkg/seeder/tenant_provisioner.go` 扩展）：

| 角色 | 权限集（默认） | 适用合同形态 |
|---|---|---|
| `msp_observer` | `ticket:read` + 评论 | 只读协办 |
| `msp_tech` | `ticket:read/write`、`knowledge:read`、`cmdb:read`、`service_catalog:read` | 只代工单（默认） |
| `msp_manager` | `msp_tech` + `user:write`（限客户侧角色）+ `report:read` | 代工单 + 客户侧开号 |
| `msp_full` | `msp_manager` + `cmdb:write`、`change:write`（**默认不分配**） | 全托管（客户显式确认） |

- **差异化机制（Q7 决策：客户级角色差异化）**：客户 admin 调整本租户 `msp_*` 角色的权限集（覆盖绝大多数场景）；个别人员用 membership 的 `role`/`expires_at`（个人级）；**不引入 grants 表**（触发条件见分析文档 B.8）；
- **分配角色映射**：`primary → msp_manager`、`backup → msp_tech`、`specialist → msp_specialist`（沿用现有 specialist 角色）；
- **上限约束**：服务方在客户作用域的权限 ≤ 该租户内角色权限；**服务方不可自我扩权**；角色/授权变更需重新登录或重新切换作用域后生效；
- `provision_tenant` 幂等供给这些角色与 `role_permissions`（现 `setup-msp-tenants.sh` §5 的 SQL 改为脚本内 API 或迁移内置）。

#### 5.5.3 审计事件模型

| 事件 | 触发 | 必备字段 |
|---|---|---|
| `auth.login` | 登录成功/失败 | 增加 `target_tenant`、`selection_mode`、`failure_reason` |
| `user.provision` | 各通道建号 | `actor_user_id / actor_tenant / target_tenant / channel / role / msp_role` |
| `user.invite` / `user.invite_accept` | 邀请（P1） | token_id、目标租户、角色 |
| `tenant.switch` | 切换 | from/to、source、result、ip、ua |

#### 5.5.4 RLS 与 tenant guard 影响

- `WithProvisioningBypass` 仅影响 **Ent 写守卫**；同时必须 `WithTenantID(target)`，确保 RLS（`enforce` 后）按目标租户生效，而不是绕过；
- `tenant_guard` 的表级豁免清单**不新增**（本方案不新建免租户列的表；路线 B 的 memberships 表会带 `tenant_id`，若不带需登记豁免并说明理由）。
### 5.6 前端改造（itsm-frontend）

| 页面/组件 | 改造 | 对应缺口 |
|---|---|---|
| 登录页 `(auth)/login` | 增加"租户代码"输入（折叠在"更多选项"，支持 `?tenant=CODE` 预填）；提交带 `tenantCode`；处理 `409 SCOPE_SELECTION_REQUIRED` → 弹**作用域选择**（仅服务方；客户方不触发） | F6/F8 |
| 会话启动 `session-bootstrap` | 不再固定 `tenants[0]`：以登录响应的 `tenant` + `tenantSelection` 为准；服务方多作用域用 `availableTenants` 选择；**客户方多作用域视为异常并 fail-closed** | F8/F9 |
| 顶栏租户切换器（新增） | 展示当前租户（name/code/type 徽标）+ 下拉 `availableTenants`（标注 home/allocation）；切换 → `POST /auth/switch-tenant` → 成功后更新 store、清空数据缓存（react-query/SWR）、重拉 `/auth/me`、`/auth/menus` | F13 |
| MSP 控制台 `/msp` | 顶部客户选择器（`GET /msp/customers`）；两种模式显式切换：**连续操作**（调 switch-tenant）或**单请求**（注入 `X-Customer-Tenant-ID`，需在 http-client 增加该头支持）；页面显著位置显示"当前客户上下文" | F13/F15 |
| 用户管理页 | 新增"目标租户"选择（仅平台/MSP 通道可见）；角色下拉按目标租户加载；`mspRole` 仅在 provider/客户场景显示；错误码（`ROLE_NOT_GRANTABLE` 等）给出可读提示 | F1/F14 |
| 首登改密页（新增） | `mustChangePassword=true` 时强制跳转 | F2 |
| http-client | 403 错误码细分提示；`X-Customer-Tenant-ID` 注入与清理；切换租户后清空缓存 | F13 |

### 5.7 数据模型与迁移

| 变更 | DDL 要点 | 批次 |
|---|---|---|
| `users.last_active_tenant_id` | `int null`，FK `tenants(id)`；登录/切换成功时更新 | P0-1 |
| `users.must_change_password` | `bool not null default false` | P0-3 |
| `msp_allocations` 去重 | 部分唯一索引 `unique (msp_user_id, customer_tenant_id) where deassigned_at is null` | P0-2 |
| `user_tenant_memberships` | 见 §6.1（含 `account_kind`/`expires_at`/3 个部分唯一索引） | P1-a |
| `users.account_kind` | `varchar(16) not null default 'customer'`（customer/provider/platform） | P1-a |

迁移策略：加列/加索引均为在线 DDL（无锁或短锁）；**本方案不含唯一约束变更**（Q3 决策：保持 `username/email` 全局唯一）；每步可独立回滚（删列/删索引）；membership 回填后跑一致性巡检（customer 账号恰好 1 条 active 作用域）。

### 5.8 兼容与回滚

- **响应兼容**：登录/切换响应只**新增**字段（`tenantSelection`、`availableTenants`、`mustChangePassword`），旧前端不受影响；
- **行为兼容**：`tenantCode` 无效从"静默忽略"改为报错属**有意破坏**（修 F6），需在 02/05 文档与 CHANGELOG 标注；可通过开关 `AUTH_TENANT_STRICT_RESOLUTION` 灰度（默认开）；
- **通道开关**：`USER_PROVISIONING_CHANNELS_ENABLED`（默认关→灰度开），关闭时新端点 404、旧 `POST /users` 行为不变；
- **回滚**：新端点/新列可独立下线；bypass 只在新路径使用，回滚不影响既有租户内建号。

---

## 6. 方案 B（P1 首批，已采纳）：一个账号、多作用域

> 2026-09-29 决策：路线 B 由"可选演进"转正为 **P1 首批**；本节即目标模型的数据与语义基线（完整论证见[作用域模型分析](./msp-scope-model-analysis-and-ai-gateway-reference.md) §3/§4）。

### 6.1 数据模型

```sql
-- 账号类型：跨租户能力的唯一判据（由创建通道决定，禁止自助修改）
ALTER TABLE users ADD COLUMN account_kind varchar(16) NOT NULL DEFAULT 'customer';
-- customer（单作用域）| provider（多作用域）| platform（全域）

CREATE TABLE user_tenant_memberships (
  id            bigserial PRIMARY KEY,
  user_id       int  NOT NULL REFERENCES users(id),
  tenant_id     int  NOT NULL REFERENCES tenants(id),
  account_kind  varchar(16) NOT NULL DEFAULT 'customer',  -- 冗余列，供部分唯一索引判定
  subject_type  varchar(16) NOT NULL DEFAULT 'user',      -- user | service_account（P2）
  source        varchar(16) NOT NULL,                     -- home | allocation | platform | invite | migration
  role_id       int  NULL REFERENCES roles(id),           -- 该租户内的角色（租户模板角色）
  msp_role      varchar(32) NULL,                         -- provider 员工在客户作用域的有效 MSP 角色
  allocation_id int  NULL REFERENCES msp_allocations(id), -- 服务方作用域来源（可追溯）
  status        varchar(16) NOT NULL DEFAULT 'active',    -- active | suspended
  is_default    boolean NOT NULL DEFAULT false,           -- 服务方默认作用域（每人最多 1 条）
  expires_at    timestamptz NULL,                         -- 作用域到期（Q7 建议）
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
-- 客户方单作用域：数据库级强约束（不靠应用自觉）
CREATE UNIQUE INDEX uq_customer_single_scope
  ON user_tenant_memberships (user_id)
  WHERE deleted_at IS NULL AND status = 'active' AND account_kind = 'customer';
```

要点：成员关系**显式**（分配/邀请产生）、**可回收**（`deassigned_at`/`deleted_at`）、**可到期**（`expires_at`）、**带租户列**（不进 `tenant_guard` 豁免清单，RLS 可覆盖）、**客户单作用域为 DB 强约束**。

### 6.2 与既有模型的关系

| 概念 | 保留/新增 |
|---|---|
| `users.tenant_id` | 保留为"主租户"（home membership 的物化，兼容既有代码与快速查询） |
| `users.account_kind` | 新增（customer/provider/platform） |
| `msp_allocations` | 保留（运营侧分配）；有效分配 → provider 侧作用域（`allocation_id` 可追溯） |
| `user_roles` | 平台角色保留；租户内角色改挂 `membership.role_id`（P1 收敛，避免"角色双源"） |
| 会话 | `availableTenants` = home ∪ memberships（P0 阶段由 home ∪ allocations 计算，**接口不变**） |

### 6.3 风险与前置

- **无唯一约束迁移**（Q3 决策：客户方不跨租户，保持 `username/email` 全局唯一）——原 P1-2 取消；
- 回填：`users.tenant_id` → home membership；有效 `msp_allocations` → provider 作用域；回填后跑一致性巡检（每个 customer 账号恰好 1 条 active 作用域）；
- `user_roles` 与 `membership.role_id` 并存会造成"角色双源"，需在 P1-b 前收敛（membership 承载租户内角色，`user_roles` 仅保留平台角色）；
- RLS `enforce` 前需验证：切换后的 `tenant_id` 与 membership 一致、审计事件完整、`expires_at` 到期回收有效。

---

## 7. 分期实施计划

### P0（阻塞项，建议 3 个批次）

| # | 任务 | 主要改动 | 依赖 | 验收 |
|---|---|---|---|---|
| P0-1 | 认证与租户解析重构 | `service/tenant_access.go`（新）；`handlers/common/service.go`（login/refresh/GetUserTenants）；`handlers/auth/service.go`（switch）；`users.last_active_tenant_id` 迁移；错误码新增 | — | 单测：解析顺序矩阵（显式/自动/冲突/停租户）；refresh 保持切换后租户；`/auth/tenants` 返回多租户 |
| P0-2 | 建号通道 | `service/provisioning.go`（新）；`tenantctx.WithProvisioningBypass`；3 个端点；角色白名单；`msp_allocations` 部分唯一索引；审计 `user.provision` | P0-1 | 契约测试：平台/MSP/租户内三通道 + 越权矩阵（非分配客户 403、rank 高攀 422、注册注入 super_admin 拒绝） |
| P0-3 | 引导身份与首登改密 | `pkg/bootstrap/token.go`、`pkg/seeder/seeder.go`、`cmd/initialize`、`cmd/provision_tenant`、`users.must_change_password` | P0-2 | 新租户 `provision_tenant` 后可直接登录（不再 SQL）；第二个租户无唯一冲突 |
| P0-4 | 前端入口 | 登录租户输入、顶栏切换器、MSP 客户选择器、用户管理租户字段、首登改密页、http-client 扩展 | P0-1/2 | 手工回归 + 组件测试；切换后数据不串租户 |
| P0-5 | 文档与脚本收口 | 02/05/06/07 更新；`setup-msp-tenants.sh` 改为 **API 优先 + SQL 兜底**；CHANGELOG | P0-2/3 | 脚本在全新租户上零 SQL 完成建号与验证 |

### P1（目标模型 + 体验与合规）

| # | 任务 | 说明 |
|---|---|---|
| P1-a | membership 落地（F7） | `user_tenant_memberships` 表 + `users.account_kind` + 回填（home/allocations）+ 一致性巡检；`AccessibleTenants` 切换到读表 |
| P1-b | 上下文与审计扩展（F15） | 请求上下文注入 `tenant_source/membership_id/is_platform_admin`；`membership.grant/revoke/suspend`、`tenant.scope_switch/denied` 审计落库 |
| P1-1 | 邀请/入职流（F4） | 邀请 token、落地页、首登改密、撤销与审计 |
| P1-3 | 客户租户 MSP 角色模板（F14） | 模板内置 4 个角色（`msp_observer/msp_tech/msp_manager/msp_full`）+ 客户 admin 可编辑角色权限（含审计）+ `membership.expires_at`；分配角色映射（primary→manager、backup→tech、specialist→specialist）；Q7 采用"客户级角色差异化"，不引入 grants 表（见[分析文档 B.8](./msp-scope-model-analysis-and-ai-gateway-reference.md#b8-建议结论推荐方案)） |
| P1-4 | 审计与指标 | 建号/切换/登录事件看板；`selection_mode`、通道分布指标 |

### P2（演进）

| # | 任务 | 说明 |
|---|---|---|
| P2-1 | `service_account` 作用域（Q8） | API Key/集成账号纳入作用域（按 ai-gateway access-key binding 模式） |
| P2-2 | `membership_scope_grants`（Q7 备选 B） | 仅当出现"限时/单次/合同号绑定"提权需求时启用（触发条件见分析文档 B.8） |
| P2-3 | G8 缓存租户维度、RLS enforce 前置 | 与本文解耦，按 ADR-004 A8/A11 推进 |
---

## 8. 验收标准与测试计划

### 8.1 测试矩阵（后端）

| 主题 | 用例 | 期望 |
|---|---|---|
| 登录解析 | 显式 `tenantCode` 有效 / 无效 / 停用 / 过期 | 进入目标租户 / `TENANT_NOT_FOUND` / `TENANT_SUSPENDED` / `TENANT_EXPIRED` |
| 登录解析 | 无 `tenantCode`：单租户身份 / 多租户身份（有 last_active）/ 多租户身份（无偏好） | 单租户直接进入 / 自动进 last_active / 进 home + 返回 `availableTenants` |
| 登录解析 | MSP 员工 + 客户 `tenantCode`（已分配 / 未分配） | 签发客户上下文 / `401 TENANT_MISMATCH` + 审计 |
| 切换 | native / allocation / super_admin / 无权限 / 目标停用 | 200（JWT tenant=目标）/ 403 `TENANT_FORBIDDEN` / 403 / 403 |
| 切换 | 切换后 refresh（同租户 / 已回收租户） | 仍为目标租户 / `401 TENANT_ACCESS_REVOKED` |
| 切换 | 切换后旧 refresh 复用 | 401（已撤销） |
| 建号 | platform 通道任意租户 / msp 通道已分配客户 / msp 通道未分配客户 / tenant 通道本租户 / 跨租户 | 201 / 201 / 403 `MSP_ALLOCATION_REQUIRED` / 201 / 403 `CROSS_TENANT_FORBIDDEN` |
| 建号 | 角色不属于目标租户 / rank 高于调用者 / `msp_role` 非法组合 | 422 `ROLE_NOT_GRANTABLE` / 422 / 422 `MSP_ROLE_NOT_ALLOWED` |
| 注册 | `role=super_admin` / `role=admin` / 缺省 | 422 / 按白名单策略 / `end_user` |
| 唯一性 | username 全局重复 / email 全局重复（Q3：不允许同邮箱多租户） | 409 / 409 |
| 作用域 | customer 账号被加入第二租户 / 服务方访问 `expires_at` 已过期作用域 | DB 约束拒绝（`CUSTOMER_SCOPE_CONFLICT`）/ 403（作用域失效）+ 审计 |
| 作用域 | 客户方命中多作用域（数据异常）/ 服务方多作用域未选择 | `409 SESSION_AMBIGUOUS` / `409 SCOPE_SELECTION_REQUIRED` + 候选列表 |
| 权限 | 服务方在客户作用域执行 `cmdb:write`（默认基线）/ 客户 admin 调整 msp 角色权限后重签会话 | 403 `SCOPE_GRANT_REQUIRED` / 新权限生效（未重签则维持旧权限） |
| 审计 | 上述每条路径 | 生成对应审计事件且字段齐全 |

### 8.2 生产验收脚本（改造 `scripts/msp/setup-msp-tenants.sh`）

目标：**零 SQL** 完成"建租户 → 建号 → 分配 → 验证"。新增/替换探针：

| 探针 | 期望 |
|---|---|
| `POST /api/v1/msp/customers/4/users`（mspadmin） | 201，客户 A 得到 `custa_admin2` |
| `POST /api/v1/msp/customers/5/users`（mspagent，`msp_tech`） | 403（无 `msp_customer:write`） |
| `POST /api/v1/tenants/4/users`（custa_admin） | 403（非平台通道） |
| `POST /api/v1/auth/register`（`role=super_admin`） | 422 |
| 登录 `mspadmin` + `tenantCode=MSPCUSTA` | 200，JWT `tenantId=4` |
| `POST /api/v1/auth/switch-tenant {tenantId:4}` → `POST /api/v1/auth/refresh` | 刷新后仍 `tenantId=4` |
| `POST /api/v1/auth/switch-tenant {tenantId:5}`（mspagent 未分配） | 403 |
| 新租户 `provision_tenant` 后首登 | 成功且 `mustChangePassword=true` |

### 8.3 安全回归（越权矩阵）

- 客户 A token → 客户 B 数据：401/403（既有，保持）；
- MSP 未分配 → 客户数据（头通道与切换通道）：403；
- 租户内 admin → 创建高于自己 rank 的角色：422；
- 任何人 → 注册 `super_admin`：422；
- 切换后的旧 refresh / 旧 access：access 仍短期有效（≤15m，接受），refresh 必须失效。

---

## 9. 风险与开放问题

### 9.1 风险

| 风险 | 等级 | 缓解 |
|---|---|---|
| `tenantCode` 由"静默忽略"改为"报错"造成存量调用失败 | 中 | 开关 `AUTH_TENANT_STRICT_RESOLUTION` 灰度；CHANGELOG 显著标注；先在生产观察日志 |
| `WithProvisioningBypass` 被滥用扩大跨租户写入面 | 高 | 仅 service 层可调用（评审清单 + 静态检查）；必须携带 actor/reason；审计强制；CODEOWNERS 覆盖 `tenantctx` 包 |
| refresh 撤销表写入量/性能 | 低 | 复用现有撤销机制（TTL 与 access 有效期一致），仅切换时写入 |
| 前端切换后残留旧租户数据 | 中 | 统一切换函数：清 store/缓存 → 重拉 me/menus → 硬刷新兜底；组件测试覆盖 |
| membership 回填不一致（P1-a） | 中 | 回填脚本幂等 + 一致性巡检（customer 恰好 1 条 active 作用域）+ 差异清单人工处置 |

### 9.2 决议记录（2026-09-29）

| # | 问题 | 决议 | 依据/落地 |
|---|---|---|---|
| Q1 | 服务方/客户方权限边界 | ✅ 客户方锁单租户、拥有完整业务功能；服务方一个账号多作用域，客户租户内基线 = 工单读写 + 知识/CMDB/服务目录只读 + `msp_manager` 开通客户侧账号 | [分析文档 §3](./msp-scope-model-analysis-and-ai-gateway-reference.md#3-业务规则确认q1-正式回答)；§5.5.2 |
| Q2 | 平台 `super_admin` 跨租户建号 | ✅ 仅 `super_admin`，强制审计 + 高危日志告警 | §5.2 |
| Q3 | 同一邮箱多租户 | ✅ **不允许**（客户方不跨租户）→ 保持全局唯一，取消邮箱租户内唯一迁移 | §4.2、§6.3 |
| Q4 | 邀请邮件通道（SMTP） | ⏳ 待确认；未就绪时先出"邀请链接"由管理员线下传递 | P1-1 |
| Q5 | 路线 B 排期 | ✅ 提前至 **P1 首批**（目标模型） | §4.1、§6 |
| Q6 | 客户账号"转移"到另一租户 | ✅ 仅平台通道；软删旧作用域 + 新建，保留审计与历史归属 | §6.1 |
| Q7 | 服务方写权限按合同差异化 | ✅ **客户级角色差异化**（4 角色模板 + 客户 admin 可编辑权限）；不建 grants 表；授予方 = 客户 admin 为主、平台应急兜底；`expires_at` + 季度复核替代到期回收 | [分析文档 B.8](./msp-scope-model-analysis-and-ai-gateway-reference.md#b8-建议结论推荐方案)；§5.5.2 |
| Q8 | `service_account` 是否纳入作用域 | ✅ 第一期不纳入（仅 `user`）；P2 按 access-key binding 模式扩展 | §7 P2-1 |

---

## 10. 附录 A：证据索引

### 后端

| 主题 | 位置 |
|---|---|
| 用户 schema（唯一约束/tenant_id/msp_role/边） | `itsm-backend/ent/schema/user.go:21-28, 48-51, 59-61, 69-78, 89-93, 106-107` |
| 迁移生成（无复合唯一/无部分唯一） | `itsm-backend/ent/migrate/schema.go:5708-5709, 5729-5765, 2717-2735, 6280-6301` |
| 角色 code 无唯一 | `itsm-backend/ent/schema/role.go:19-24` |
| 表级豁免清单（tenant_guard 真实语义） | `itsm-backend/internal/schema/tenant_guard.go:60-64, 83, 92, 117-240` |
| Ent 写路径租户守卫 | `itsm-backend/database/security.go:94-131` |
| 用户创建 handler（通道/角色高攀） | `itsm-backend/handlers/user/handler.go:38-102, 515-531` |
| 用户创建服务（全局唯一/角色归属） | `itsm-backend/service/user_service.go:48-136, 139-167, 171-191` |
| 登录（解析/签发/审计） | `itsm-backend/handlers/common/service.go:93-159` |
| refresh（按 home 重签） | `itsm-backend/handlers/common/service.go:172-198` |
| GetUserTenants（只返回 home） | `itsm-backend/handlers/common/service.go:308-334` |
| 切换租户（授权/重签/响应） | `itsm-backend/handlers/auth/service.go:114-163`；`handlers/auth/handler.go:92-110` |
| 注册（角色原样落库） | `itsm-backend/handlers/auth/service.go:166-210` |
| JWT claims / 签发 / 上下文注入 | `itsm-backend/middleware/auth.go:14-20, 60-92, 253-267` |
| 租户中间件（来源优先级/状态校验） | `itsm-backend/middleware/tenant.go:26-31, 60-83, 133-169` |
| MSP 角色映射 / 静态权限表 | `itsm-backend/middleware/msp_rbac.go:18-30`；`middleware/rbac.go:437-481, 498-506` |
| MSP 中间件（分配校验/头通道） | `itsm-backend/middleware/msp_middleware.go:88-168` |
| 路由（auth/switch-tenant/tenants、msp） | `itsm-backend/router/common_system_routes.go:19-27`；`router/msp_routes.go:13-35` |
| bootstrap/引导（固定 admin） | `itsm-backend/pkg/bootstrap/token.go:141-154`；`pkg/seeder/seeder.go:849-863, 887-896`；`cmd/initialize/main.go:98-115`；`router/bootstrap_routes.go:53-73` |

### 前端（itsm-frontend）

| 主题 | 位置 |
|---|---|
| 登录表单/提交（无租户） | `src/pages/(auth)/login/index.tsx:146, 158-163, 280-301` |
| 登录 API（支持 tenantCode） | `src/lib/services/auth-service.ts:242-260` |
| 认证 store / 租户持久化 | `src/lib/store/auth-store.ts:60-74, 111-119, 160-167` |
| 会话启动（固定 tenants[0]） | `src/lib/auth/session-bootstrap.ts:61-64, 92-103` |
| 请求头注入（X-Tenant-ID/Code） | `src/lib/api/http-client.ts:179-192`；`src/lib/auth/tenant-context.ts:8-17, 42-45` |
| 403 处理（仅 CSRF） | `src/lib/api/http-client.ts:267-271` |

---

## 11. 附录 B：与既有文档/缺口的映射

| 本文缺口 | 07 文档缺口 | ADR-004 行动项 | 交付批次 |
|---|---|---|---|
| F1 | G1 | A6（客户用户创建路径） | P0-2 |
| F2 | G2 | A3/A6 | P0-3 |
| F3 | —（本次新发现，安全） | — | P0-2 |
| F4 | — | — | P1-1 |
| F5/F6 | G9（同类静默降级） | A10⑤ | P0-1 |
| F7 | — | — | P1-2 / P2-1 |
| F8/F9 | — | A10⑤ | P0-1 / P0-4 |
| F10/F11/F12 | — | A10⑤ | P0-1 |
| F13 | — | — | P0-4 |
| F14 | G3 | A5（MSP 权限授予） | P1-3 |
| F15 | G9 | A10② | P0-1（审计统一） |
| — | G4/G5/G6/G7 | A2（供给可复现） | 已由脚本规避，P0-5 收敛 |
| — | G8/G10 | A8 | P2-2 / 运维已规避 |

**文档同步清单（P0-5）**：`02-deployment-and-configuration.md`（建号通道与引导）、`05-usage-guide.md`（登录选租户/切换/客户上下文）、`06-verification-and-troubleshooting.md`（新增探针与错误码）、`07-known-gaps.md`（G1/G2/G3/G9 状态更新为"方案已出"）、`CHANGELOG.md`。

---

## 修订记录

| 版本 | 日期 | 变更 |
|---|---|---|
| v0.1 | 2026-09-29 | 首版：现状盘点（三方调研）、缺口 F1–F15、路线 A/B 选型、P0 详细设计、分期与验收 |
| v0.2 | 2026-09-29 | 依据[作用域模型分析](./msp-scope-model-analysis-and-ai-gateway-reference.md)：Q1 已答（服务方/客户方边界）；路线 B（membership）提前至 P1；取消 P1-2 邮箱租户内唯一；客户方登录解析收紧为 fail-closed |
| v0.3 | 2026-09-29 | **按建议固化决策**：§0/§4 改为"已定路线（A→B 递进）"；§6 由"可选演进"改写为"P1 首批目标模型"（membership DDL 含 `account_kind`/`expires_at`/客户单作用域 DB 约束）；§5.2.4 取消唯一约束迁移；§5.3 登录解析按 account_kind 分派（客户 fail-closed、服务方 409 选择）；§5.5.2 落 4 个 msp 角色与客户级差异化；§7 分期重排（P1-a/b、P2-1/2/3）；§9.2 改为决议记录（Q1–Q8） |
