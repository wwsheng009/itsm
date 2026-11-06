# 07 · 已确认产品缺口（Known Gaps）

> **状态**：当前
> **更新日期**：2026-09-28
> **来源**：`saas_msp` 生产环境端到端初始化实测（1 服务商 + 2 客户，见 [06 文档 §7](./06-verification-and-troubleshooting.md)）
> **用途**：记录实测中确认的**产品缺口**、当前运维规避手段与建议修复方向；缺口关闭后在本页更新状态，不删除历史结论。
> **配套方案**：用户创建 / 登录选租户 / 租户切换相关缺口（G1/G2/G3/G9）的深入分析与分期方案见 [plan/msp-user-lifecycle-and-tenant-switching-plan.md](./plan/msp-user-lifecycle-and-tenant-switching-plan.md)（缺口编号 F1–F15）。

> **复核（2026-09-30）**：G1–G10 的承接工作流见[实施方案 §3.0-F](./plan/msp-implementation-plan.md)。G8 优先级上调为 **P0**（并入 `IP-P0-2` 缓存审查）；G1/G3/G8 在 P0 关闭；**G9 的 P0 关闭（IP-P0-6）经 2026-09-30 复核未生效（JWT 已锁定时 Header 被跳过），已由 IP-P1-8 真正关闭**（401 + `tenant.probe_denied` 落库审计，见 §10 实现复核）；G2/G4/G5/G6 在 P1 关闭，G7 接受现状（P2 提升为通用约定），G10 文档/脚本已规避（P0 收尾）。

> **新增（2026-10-06）**：**G11 · provider 首管只绑纯 MSP 角色（自身 ITSM 面不可见）** 已关闭（组合角色 `provider_tenant_admin` + 首管策略改绑 + `cmd/backfill_provider_admin` 存量回填，见 §12）。
>
> **新增（2026-11-06）**：**G12 · 错误码词表漂移**、**G13 · 最后管理员护栏按当页估算** 已关闭；
> **G16 · TUM 平台面端点零 Swagger 注解**（执行 `make swagger-gen` 时新发现）登记为待办（§17）；
> **G14 · membership 撤销面前端零接线** 与 **G15 · 跨租户角色调整职责边界未成文** 新登记。来源：
> [`docs/review/2026-11-06-tenant-user-role-adjustment-review.md`](../review/2026-11-06-tenant-user-role-adjustment-review.md) v0.2 复核（详见 §15、§16）。
>
> **新增（2026-11-06，客户主数据设计期间发现）**：**G17 · `msp_contracts` RLS 012 客户只读跨租户放宽**，违反「业务数据不跨租户」（BD-1），**已裁决收回**；须先建客户租户内快照通道再 DROP，否则 L1 SLA 匹配静默降级（§18）。
>
> **新增（2026-11-06，上游吸收规划期发现）**：**G18 · `users.username` 唯一性口径与 `AGENTS.md` 基线冲突（且登录契约依赖全局唯一）**、**G19 · `cab` 与上游 `change_review` 同能力两套命名** 登记为**待裁决**（§19、§20）；两者均为上游能力吸收的前置阻塞项。

> **编号（2026-09-29 一致性整改；2026-11-06 扩至 G19）**：本页 `G1–G19` 是**生产实测缺口**的唯一 G 空间；canon §7.3 的能力供给缺口已更名 `K1–K5`；工作台规则已更名 `WB1–WB6`；租户用户管理方案另占 `TUM-G1–TUM-G4`，评审 v0.2 的角色调整缺口续号为 `TUM-G5–TUM-G12`（与本空间不重叠）。跨文档引用本页写作 `07:G4`（注册表见 canon 附录 C）。

## 1. 缺口清单（按影响排序）

| # | 缺口 | 影响 | 当前规避 | 建议修复 | 优先级 |
|---|---|---|---|---|---|
| G1 | 跨租户创建用户被 tenant guard 拦截 | 无法为新租户创建首个用户 | SQL + pgcrypto 直写 | 提供受控的跨租户用户创建路径（见 §2）；**→ ✅ 已关闭（2026-09-30，IP-P0-5）：`UserProvisioningService` 三通道 + `tenantctx.WithProvisioningBypass(actor,channel,target)`；平台/MSP/租户内均可经 API 建号（`07:G2` 仍归 IP-P1-5）** | P0 |
| G2 | bootstrap 无法为多租户建首个管理员 | 第 2 个及以后租户无法走官方初始化 | 同上 | bootstrap 支持 `-tenant-id` + 用户名/邮箱策略（§3）；**→ ✅ 已关闭（2026-09-30，IP-P1-5）：账号策略 `admin-<tenantCode>`（username/email 不再全局冲突）；`cmd/initialize` 支持 `-tenant-id`/`-tenant-code`/`-admin-username`/`-admin-email`；`provision_tenant -create-admin` 无 token 通道（幂等）；`must_change_password` 首登强制改密 + `POST /api/v1/auth/change-password`；连续 2 租户 bootstrap 用例通过** | P0 |
| G3 | MSP 管理员有效角色解析为 `msp_manager` | 同租户 API 建号不可用（无 `user:write`，且过不了角色高攀校验） | 首个用户/技术员均走 SQL | JWT claims 取主角色或按 rank 取最大（§4）；**→ ✅ 已关闭（2026-09-30，IP-P0-9）：登录 role=max(主角色, MSP 映射角色)（`admin`+`provider_admin`→`admin`）；`middleware.RoleRank` 单源覆盖 `msp_*`；同租户 API 可建 `agent`** | P0 |
| G4 | 源租户 `default` 缺内置审批组 | `provision_tenant` readiness 失败（groups=0） | 脚本回填 6 个内置组 | migration 回填存量库 + readiness 降级策略（§5） | P1 |
| G5 | id 序列落后于 `max(id)` | 供给时 `roles_pkey` duplicate key | 脚本 `setval(...)` 校正 | 初始化/迁移统一校正（§6） | P1 |
| G6 | `role_permissions` 无唯一约束 | 无法 `ON CONFLICT`，供给脚本只能 `where not exists` | 脚本幂等插入 | 增加 `(role_id, permission_id, tenant_id)` 唯一索引（§7） | P2 |
| G7 | CLI 工具 stdout 混入状态行 | `psql -tAc` 的 `INSERT 0 1` 污染 `returning id` 输出，自动化解析失败 | `last_number()` 过滤 | 工具输出规范（§8） | P2 |
| G8 | 缓存 key 无租户维度 | 多客户场景存在串数据风险 | **✅ 已审查并关闭（2026-09-30）** | 逐 key 审查完成：租户分区缓存/序列均含 `tenant_id`；全局编号序列登记豁免（见 §9）；承接 `IP-P0-2` 步骤 6 | **P0** |
| G9 | `X-Tenant-Code` 与 JWT 冲突被静默忽略 | 调用方误以为切换了租户；无冲突告警，排障困难（不越权） | 依赖 JWT 租户；探针按实测标注 | Header 与 JWT 冲突时返回 401/400 并记录告警（§10）；**→ ✅ 真正关闭（2026-09-30 复核 + IP-P1-8 v1.29）**：IP-P0-6 声明的关闭未生效（JWT 已锁定时 Header 被跳过，冲突分支不可达）；现为 401 + `reasonCode=TENANT_MISMATCH_REJECTED` + `tenant.probe_denied` 落库审计（一致时正常放行） | P2 |
| G10 | snap 版 docker 下宿主 `/tmp` 对守护进程不可见 | `docker cp` 静默复制旧文件；`docker run -v /tmp/...` 产物"写丢"，构建/供给莫名失败 | 产物与暂存目录放 `$HOME` 非隐藏目录 + sha256 校验（§11） | 文档/脚本固化路径约定；容器化部署优先用 bind 到 `$HOME` 或非 snap docker | P1 |
| G11 | provider 首管只绑纯 MSP 角色，自身 ITSM 面不可见 | 服务商租户管理员登录后只见 4+1 个 MSP 菜单（工单/事件/变更/SLA/知识库/CMDB 全被权限过滤），与 canon §2.1「provider 同时是普通 ITSM 租户」冲突 | 无（权限驱动菜单按 membership 角色收敛，无提示） | 新增组合角色 `provider_tenant_admin`（= `admin` ∪ `msp_admin`）+ 首管策略改绑 + `cmd/backfill_provider_admin` 回填存量（§12）；**→ ✅ 已关闭（2026-10-06）** | P1 |
| G12 | 平台侧租户用户**错误码词表漂移**：前端 `TENANT_USER_ADMIN_ERROR_MESSAGES` 10 项 vs 后端常量表 12 项，缺 `TENANT_USER_MEMBERSHIP_NOT_FOUND`、`HOME_MEMBERSHIP_IMMUTABLE` | membership 撤销失败时用户看到英文原文/裸 message，无法自助判断原因；违反 API 客户端自身「两侧必须同步」的契约声明 | **已修复**：补两码中文文案 + 加**同源断言**（`tenant-user-admin-api.test.ts`），缺映射即测试红 | 后续新增稳定错误码必须同步前端映射（§15） | P2 |
| G13 | 租户用户抽屉「最后管理员」护栏按**当页 items** 估算活跃 admin | 管理员跨页分布（pageSize 默认 20，可选 10/20/50/100）时**假性禁用**停用开关，运维误判为「权限被锁」；真实拦截仍依赖后端 409 | **已修复**：`TenantUserListResponse.activeAdminCount` 返回租户级全量计数（刻意不受 keyword/status/role 筛选与分页影响），前端取该值并随乐观更新/回滚同步；字段缺失时回退当页估算（§15） | 服务端 `SetActive` 租户级校验始终为权威；待最低兼容版本确认后可移除前端回退 | P2 |
| G14 | membership 撤销面**后端已实现、前端零接线**，且「仅 API/CLI 通道」口径未成文 | 平台 UI 无法撤销跨租户成员映射（home 不可撤、软删 + 分配级联、权限缓存与在途 refresh 立即失效等能力只有直调 API 才可达）；`itsm-frontend/src` 对该端点零引用 | 直调 `POST /api/v1/tenants/:id/users/:userId/membership/revoke`；本批先在 API 客户端注释与本表成文口径，**不擅自接 UI** | **待产品决策**：是否在 `TenantUsersDrawer` 行操作增「撤销成员映射」（纯前端接线，后端/审计/通知/测试均已就绪）（§15） | P2 |
| G15 | **跨租户角色调整无通道**（平台 `super_admin` 与服务商 provider 均不能改他租户用户角色），且职责边界口径未成文 | 替他租户调权限的运维动线缺失 → 现实回落两种危险替代：「借目标租户管理员账号登录操作」（共享凭据、审计主体错位）或 **SQL 直改 `users.role` / `user_roles`**（无 rank 校验、无角色归属校验、无审计、不吊销会话） | **本租户内自助（设计主通道，非兜底）**：目标租户管理员用 `PUT /api/v1/users/:id`（主角色 + `roleIds`，`CanGrantRoles` 三重护栏 + 变更后吊销 token）与 `roles` CRUD；跨租户仅「建号时可指定角色」（`ProvisionUserToTenant` + `ListTenantGrantableRoles`） | 登记口径 + 运维 SOP（§16）；若 MSP 跨客户场景升级为需求，按 [`plan §10`](./plan/msp-tenant-user-management-enhancement-plan.md) 触发条件**另案评审**，前置：平台级角色词表收敛 + 变更审批/双人复核，并先解决 G11 四处同源 | P2（边界决策，维持不做） |
| G16 | **TUM 平台面 7 个租户用户端点零 Swagger 注解**（`handlers/tenant/user_admin.go` 无 `@Summary/@Success/@Router`），故 `activeAdminCount` 等新契约字段在 OpenAPI 中**不可见** | 外部集成方/MCP/客户端代码生成看不到租户用户治理通道；契约漂移只能靠读源码发现（G12 之所以能被发现纯属人工比对）；`make swagger-gen` 因此**无法**记录本批新字段 | 契约单一源在 `docs/multi-tenant/plan/...enhancement-plan.md` §3.1/§3.5 + `tenant-user-admin-api.ts` 注释；本批**不回滚式刷新** docs（刷新会带进他人未提交的 swagger 漂移） | 单独一批：补 7 端点注解 → `make swagger-gen` → 与 `docs-gate` 一并提交；不与缺陷修复混批（§17） | P2 |
| G17 | **`msp_contracts` RLS 012 存在客户只读跨租户放宽**：`msp_contract_customer_read`（`customer_tenant_id = ctx`）与 `msp_contract_binding_customer_read`（EXISTS 子查询继承）允许 customer 租户直读 provider 租户的合同业务行 | 客户租户可读对方 `name`/`penalties`（违约率与上限）/`settlement_cycle`/`currency`/`contract_no`，且**无列级粒度、无 `status`/有效期校验**（解约后历史行永久可读）、数据库侧不可审计；与「业务数据不跨租户」（BD-1）正面冲突 | 无（策略默认生效）；**不可用「直接 DROP」规避**——会造成 L1 静默降级（见下行） | **裁决收回**：013 删除两条策略；L1 匹配改读客户租户内 `contract_sla_snapshots`，结算/下发改读 `settlement_statements`。**必须先建快照通道（双写观察一个结算周期）再 DROP**。设计见 [`docs/customer-master-data/07-tenant-data-boundary.md` §3.2/§4.5](../customer-master-data/07-tenant-data-boundary.md)、DDL 与四项前置检查见 [`02-ddl-and-migration.md` §12.6](../customer-master-data/02-ddl-and-migration.md)（本节 §18） | P1 |
| G18 | **`users.username` 唯一性口径与仓库自身基线冲突，且登录契约依赖全局唯一**：schema 为全局 `Unique()`（`ent/schema/user.go:21-23`），而 `AGENTS.md:245` 规定「业务唯一键默认按 tenant 组合唯一」；上游 `heidsoft` 已按基线放宽为 `(tenant_id, username)`（`migrations/20261006_tenant_scope_user_username_unique.sql`） | ① 每租户都想要 `admin` 的产品直觉不可实现，只能靠 `admin-<tenantCode>` 命名绕行（`07:G2` 的关闭方式，可读性/AD-SSO 习惯受损）；② **反向约束**：登录在未指定租户时走 `UsernameEQ(username).Only(...)`（`handlers/common/service.go:195-199`），`.Only()` 在多命中时**直接报错**，注册也在跨租户作用域查重（`handlers/auth/service.go:288-294`）—— 因此**不能只改唯一键**，必须同时改登录契约，否则不带 tenantCode 的既有登录入口会硬失败 | 命名策略 `admin-<tenantCode>`（username 与 email 均带租户后缀，`pkg/bootstrap/token.go:79`） | **需裁决（两项绑定，不可只做其一）**：A 采纳组合唯一 → 必须同步把登录改为「tenantCode/domain 必填或引入账号消歧步骤」，并复核注册查重、`cmd/initialize`、`cmd/provision_tenant`、`cmd/backfill_provider_admin`、邀请与 SSO 计划；B 维持全局唯一 → 需在 `AGENTS.md` 明确「`users.username` 为登记例外并写明理由」，消除基线自相矛盾 | P1（阻塞上游吸收 B 类与部分 A 类） |
| G19 | **`cab` 与上游 `change_review` 是同一能力的两套命名/两张表**：四条 `/members` 端点方法与权限码完全一致（`change:read` / `change:write`，`router/cab_routes.go:15-18` vs 上游 `router/change_review_routes.go`），表名本地 `cab_member.go`、上游 `change_review_member.go`，上游另有 `20260921_drop_cab_members_table.sql`（注释：CAB 已重命名为 change_review） | 吸收上游变更评审能力会造出**第二套名册 + 第二组同名端点**；菜单、权限码、类型定义、acl manifest 分叉；存量数据需迁移 | 本地自成一套，功能可用（无上游介入时不影响） | **需裁决唯一命名**后再吸收。改名成本实测：后端 26 文件、前端 17 文件（`types/cab.ts`、`routes/area-permissions.ts` 等）、文档与 manifest 5 文件；建议连同权限码与菜单 key 一并定，并给出 `cab_members → change_review_members` 的迁移与回滚对（保留 ITIL 术语则反向要求上游移植时改名） | P1（阻塞 change 域吸收） |

## 2. G1 · 跨租户创建用户被 tenant guard 拦截

**现象**：以 `super_admin`（default 租户）通过 `POST /api/v1/users` 为 `msp_provider` 租户（`tenantId=3`）创建用户，返回 `create User with tenant_id=3 but request tenant=1`。

**证据**：tenant guard 中间件按请求上下文（JWT 的 `tenant_id`）校验写入实体归属，`super_admin` 不豁免；当前无 HTTP 头/参数可切换请求租户上下文（`X-Customer-Tenant-ID` 仅对 MSP 分配用户生效，且面向客户业务数据）。

**影响**：新租户（尤其 `msp_provider` / `msp_customer`）的**首个用户**无法通过产品 API 创建，初始化流程必须回落数据库直写。

**当前规避**：`scripts/msp/setup-msp-tenants.sh` §6 用 `pgcrypto` 的 `crypt(..., gen_salt('bf',10))` 生成 bcrypt 口令直写 `users` + `user_roles`（与 Go `bcrypt` 校验兼容，已实测登录成功）。

**建议修复**：为平台管理员提供受审计的跨租户用户开通能力，二选一：

1. `POST /api/v1/tenants/{id}/users`（平台级路由，`tenant:write` + 审计日志，显式指定目标租户）；
2. 允许 `super_admin` 携带受控头（如 `X-Target-Tenant-ID`）覆盖请求租户上下文，并纳入 tenant guard 白名单与审计。

**验收标准**：`super_admin` 可经 HTTP 为任意租户创建首个管理员；tenant guard 仍拒绝普通跨租户写入；审计日志含操作者/目标租户/被创建用户。

## 3. G2 · bootstrap 无法为多租户建首个管理员

**现象**：

- `bootstrap_tokens` 表为空，且数据库未启用 `pgcrypto`；
- `cmd/initialize generate-bootstrap-token` 生成的流程固定创建 `username=admin` / `email=admin@example.com`；
- `users.username` / `users.email` 为**全局唯一**，第 2 个租户必然冲突。

**关闭（2026-09-30，IP-P1-5）**：账号策略改为 `admin-<tenantCode>`（邮箱 `admin-<tenantCode>@bootstrap.local`，可用 `-admin-username/-admin-email` 显式覆盖）；`cmd/initialize` 支持 `-tenant-id`/`-tenant-code` 定位目标租户（不再写死 `default`）；`provision_tenant -create-admin` 提供无 token 通道（`CreateFirstAdmin`，幂等保护：租户已有 bootstrap 管理员则拒绝）；bootstrap 管理员默认 `must_change_password=true`，登录响应携带 `mustChangePassword`，经 `POST /api/v1/auth/change-password`（持旧密码 + 密码策略）清除。回归：`pkg/bootstrap` 连续 2 租户用例 + 身份/幂等用例。

**影响**：`saas_msp` / `saas` 模式无法用官方 bootstrap 流程开通第二个租户的首个管理员。

**当前规避**：同 G1（SQL 直写）。

**建议修复**：

1. `users.username`/`email` 唯一性改为**租户内唯一**（`unique(tenant_id, username)`），或
2. bootstrap 令牌携带 `tenant_id`，并允许自定义 `username`/`email`（默认值带租户后缀，如 `admin@msp001.local`）；
3. 初始化 CLI 增加 `-tenant-id` 参数并在文档中明确多租户开通顺序。

**验收标准**：连续为 2 个以上租户执行 bootstrap 均成功，且互不冲突。

## 4. G3 · MSP 管理员有效角色解析为 `msp_manager`

**现象**：`mspadmin`（主角色 `admin`，`msp_role=provider_admin`，角色边含 `admin` + `msp_manager`）登录后 JWT claims 的 `role` 为 `msp_manager`：

- `roleRank("msp_manager") == 0`（`handlers/user/handler.go:516`），低于 `admin`；
- 其有效权限集仅 10 条 `msp_*`，不含 `user:write`；
- 即便补权限，创建用户时仍会触发"不得分配高于自身角色的用户角色"校验（`admin` 目标角色 > `msp_manager`）。

**影响**：服务商管理员无法在同租户内通过 API 创建用户/技术员（实测 `mspagent` 只能 SQL 创建）。

**当前规避**：`scripts/msp/setup-msp-tenants.sh` §6b 用 SQL 创建 `mspagent`。

**建议修复**：

1. JWT `role` claims 取**主角色**（`users.role`）或按 `roleRank` 取最大者，MSP 角色只作为附加维度（`msp_role`）；
2. `roleRank` 为 `msp_*` 角色定义合理秩（不应低于其绑定的主角色能力）；
3. 明确"角色高攀校验"在 MSP 场景的语义（应比较实际权限集而非角色名）。

**验收标准**：`mspadmin` 登录后 `role=admin`（或 rank ≥ admin），可经 `POST /api/v1/users` 创建 `agent` 用户。

## 5. G4 · 源租户 default 缺内置审批组

**现象**：`provision_tenant` 的 readiness 校验要求源模板租户 `groups > 0`；2026-09-15 新增的 `BuiltinGroups`（`pkg/seeder/seeder.go`：approvers-l1/l2/l3/managers/security/change）未在存量 `default` 租户生效 → 供给直接失败。

**影响**：存量库升级后新租户供给不可用（新建库不受影响）。

**当前规避**：脚本 §1 按 `BuiltinGroups` 回填 6 个组（`where not exists` 幂等）。

**建议修复**：随版本提供一次性回填（migration 或 `initialize` 子命令），并让 readiness 失败信息明确指出缺项与修复命令。

## 6. G5 · id 序列落后于 max(id)

**现象**：`roles` 表 `max(id)=112` 而序列 `last_value=100`，`provision_tenant` 插入时 `roles_pkey` duplicate key。

**影响**：任何依赖序列的自增写入（模板供给、种子数据）在存量库上随机失败。

**当前规避**：脚本 §2 对 `roles/permissions/menus/groups/role_permissions/sla_definitions/ci_types/approval_workflows/system_configs` 执行 `setval(seq, greatest(max(id),1))`。

**建议修复**：初始化/迁移脚本统一执行序列校正（覆盖全部自增表），并在启动自检中告警。

## 7. G6 · role_permissions 无唯一约束

**现象**：`role_permissions` 无 `(role_id, permission_id, tenant_id)` 唯一约束，`ON CONFLICT` 不可用，重复插入会产生重复行（影响权限并集计算与审计）。

**当前规避**：脚本用 `insert ... where not exists (...)`。

**建议修复**：增加唯一索引（先清理历史重复行），并让授权接口使用 upsert。

## 8. G7 · CLI 工具 stdout 混入状态行

**现象**：`psql -tAc "insert ... returning id"` 的输出包含 `INSERT 0 1` 状态行，导致脚本取到的 ID 为多行（实测：`-tenant-id` 解析失败）。

**当前规避**：脚本统一用 `last_number()`（`sed -n 's/^\([0-9][0-9]*\)$/\1/p' | tail -n 1`）过滤；函数内部状态信息一律走 stderr。

**建议修复**：工具/脚本规范：结构化输出仅走 stdout，状态与日志走 stderr；文档中给出示例。

## 9. G8 · 缓存 key 无租户维度

**审查结论（2026-09-30，`IP-P0-2` 步骤 6）**：**关闭**。逐 key 清单：

| 键/缓存（代码位置） | 格式 | 判定 |
|---|---|---|
| RBAC 权限缓存（`middleware/rbac.go`） | `{role}_{tenantID}` | ✅ 含租户维度 |
| HTTP 响应缓存（`middleware/cache_middleware.go`） | `api:tenant:{id}:user:{id}:path:{path}:query:{hash}` | ✅ 含租户+用户维度 |
| 影响面解释缓存（`service/impact_explanation_service.go`） | `cacheKey(tenantID, ciID, hops)` | ✅ 含租户维度 |
| 工单号序列（`repository/ticket`、`service/ticket_core_service.go`） | `sequence:ticket:{tenantID}:{YYYYMM}` | ✅ 含租户维度 |
| Token 黑名单（`service/token_blacklist_service.go`） | `jwt:blacklist:{token}` / `refresh:blacklist:{token}` | ➖ 凭据键，无租户业务数据 |
| 事件号/CI 号序列（`incident_service`、`configuration_item_service`） | `sequence:incident:{YYYYMM}` / `sequence:ci:{YYYYMM}` | ⚪ **豁免**：编号为全局唯一（唯一约束不含 `tenant_id`），序列用于跨租户协调 + 存在性跳号，不含租户数据；若后续要求编号按租户隔离，需先改唯一约束（登记 P2） |
| `cache/redis.go` 包装器 / `query_optimizer.GenerateCacheKey` | — | ➖ 当前无业务调用方（无生效键） |

**评审检查项**：新增缓存/序列键必须含 `tenant_id`；全局协调键须在代码注释中声明豁免理由，并在本表登记。

## 10. G9 · `X-Tenant-Code` 与 JWT 冲突被静默忽略

**现象**：客户 A 的管理员 token 携带 `X-Tenant-Code: MSPCUSTB` 请求 `GET /api/v1/users`，实测返回 **200** 且仅返回客户 A 用户（数据不越界），但也没有任何"租户不匹配"拒绝或告警。

**根因**（`middleware/tenant.go:31-83`）：租户来源优先级为 `JWT claims.tenant_id > X-Tenant-Code > Subdomain > Path`；步骤 2 的 Header 解析条件是 `tenantEntity == nil`，即 **JWT 已解析出实体时 Header 根本不会被读取**，因此步骤 5 的"JWT 与最终结果不一致 → 拒绝"（`tenant.go:144-157`）也不会触发。文档旧表述（"冲突返回 401"）与实际不符。

**影响**：安全上 fail-safe（不会跨租户取数），但：

- 客户端/网关若依赖该 Header 切租户，会得到"看似成功"的响应，实际仍在原租户 → 误操作与排障成本；
- 冲突请求无审计/告警信号，不符合 fail-closed 的可观测性要求。

**当前规避**：文档与探针按实测行为描述（06 文档 §2 用例 2、§7.3）；客户端一律以 JWT 为准（登录时用 `tenantCode` 换取正确 token）。

**建议修复**：在步骤 2 之前增加"冲突检测"：若请求同时携带 JWT `tenant_id` 与 `X-Tenant-Code`，且二者解析结果不一致 → 返回 401（或 400 参数错误）并记录 `tenant mismatch rejected` 告警。

**验收标准**：`X-Tenant-Code` 与 JWT 冲突时返回 401/400；一致时正常放行；日志含冲突双方租户 ID。

**实现复核（2026-09-30，IP-P1-8）**：IP-P0-6 曾声明本缺口已在 P0 关闭，但复核确认该路径仍被跳过（根因同上）——冲突分支仅在"JWT 解析不到实体"时才可能触发。IP-P1-8 将 Header 解析改为**无论 JWT 是否锁定都执行**，锁定时做一致性校验：冲突 → 401 + `TENANT_MISMATCH_REJECTED` + `tenant.probe_denied` 审计（source=header）；一致 → 正常放行。回归：`middleware/tenant_test.go` 冲突/一致两态 + 头通道拒绝审计。

## 11. G10 · snap 版 docker 下宿主 `/tmp` 对守护进程不可见

**现象（2026-09-28 实测）**：

- 宿主 `/tmp/provision_tenant_linux_amd64`（123,248,392 字节，sha256 `e7f518…`）经 `docker cp` 注入容器后，容器内 `/tmp/provision_tenant` 仍是**旧文件**（119,537,895 字节，sha256 `7c14a7…`），且 `docker cp` 返回码为 0、无任何报错；
- `docker run -v /tmp:/out ...` 的构建产物在宿主 `/tmp` 不可见（模块缓存目录同样为空），表现为"构建成功但找不到二进制"；
- 将源文件换到 `/home/<user>/...` 后 `docker cp` 立即恢复正常（sha256 一致）。

**根因**：docker 由 snap 安装（`/snap/bin/docker` → `/usr/bin/snap`，`snap list docker` = 29.8.0）。snap 沙箱为守护进程提供**独立的 `/tmp`**，宿主 `/tmp` 不在其可见范围内；snap 的 `home` 接口同时**不放行 `$HOME` 下的隐藏目录**（实测 `$HOME/.itsm-provision` 报 `permission denied`，`$HOME/itsm-artifacts` 正常）。

**影响**：所有依赖 `docker cp` / `docker run -v` 且路径在 `/tmp` 的运维动作（二进制注入、构建产物导出、缓存挂载）都可能静默失败或使用陈旧文件；`docker cp` 不报错使问题极难定位。

**当前规避**：

- `scripts/msp/setup-msp-tenants.sh` 注入前先把二进制复制到 `$HOME/itsm-artifacts`（snap 可见）并**校验容器内 sha256**，不一致立即失败退出；
- `scripts/msp/build-provision-tenant.sh` 的产物与模块缓存默认落在 `$HOME/itsm-artifacts`；
- 本次实际采用的构建路径为本机交叉编译 + `scp` 到 `$HOME/itsm-artifacts`。

**建议修复（环境/工具侧）**：

1. 部署文档与运维脚本统一约定"docker 可见路径"（`$HOME` 下非隐藏目录，或 `/var/lib/...` 等），禁止把 docker 输入/输出放 `/tmp`；
2. 或改用非 snap 安装的 docker（apt/deb），消除沙箱路径差异；
3. 运维脚本对 `docker cp` / `-v` 结果做内容校验（本次已加 sha256 校验，可作为通用范式）。

**验收标准**：在 snap docker 环境下按文档路径执行构建与注入，容器内文件与宿主 sha256 一致；文档不再出现"把二进制/构建产物放 `/tmp` 再 docker cp/-v"的指引。

## 12. G11 · provider 首管只绑纯 MSP 角色（自身 ITSM 面不可见）

**现象（2026-10-06 反馈 + 实测）**：`msp_provider` 租户首管登录后左侧只有 5 个菜单（服务台 + 客户管理 + 跨客户工作台 + 分配管理 + 审计看板）；租户内工单/事件/变更/SLA/知识库/CMDB 等菜单全部不可见，使用者提出"作为 msp_provider 租户怎么只有 4 个功能，如何承接业务功能"。

**根因**：菜单按权限过滤（`service/menu_service.go`），而 `pkg/bootstrap/admin.go` 的 provider 首管策略把租户内 RBAC 角色固定为纯 MSP 角色 `msp_admin`（权限仅 10 个 `msp_*` 码 + task 基线）；`admin` 角色虽含全量 ITSM 权限但只有 `msp:read`——**两角色权限矩阵不重叠**，单角色无法同时覆盖「自身 ITSM + 跨客户 MSP」，与 canon §2.1 provider 双重定位（自身普通 ITSM 租户 + 跨客户 MSP 运营方）冲突。

**影响**：服务商租户无法在自有租户内开展内部 ITSM（自有工单/CMDB/知识/变更/服务目录）；且无任何提示说明如何获得这些菜单，造成"服务商只有 4 个功能"的产品误解。

**修复（2026-10-06）**：

1. 新增租户内组合角色 `provider_tenant_admin`（名称「服务商租户管理员」）：权限 = `admin` ∪ `msp_admin` 并集，由 `internal/authz.BuiltinRolePermissionCodes()` 显式派生（继承 task 基线/同义补齐/admin 显式补齐，避免双份清单漂移；不含平台通配符）；
2. `pkg/bootstrap` provider 首管策略改绑该角色（`RBACRoleCode`）；账号仍 `users.role=admin` + `users.msp_role=provider_admin`，membership 仍记 `msp_role=msp_admin`；
3. 三处单一源同步：`pkg/seeder.BuiltinRoles()` 角色种子、`middleware.RolePermissions` 硬编码兜底（`init` 派生 = admin ∪ msp_admin）、`RoleRank`（rank 4）与菜单提权名单（`isElevatedMenuRole`）；
4. 存量租户回填：新增 `cmd/backfill_provider_admin`（幂等；dry-run 默认开启）——补角色行 + 权限并集 + 首管 membership 重绑 + `user_roles` 边 + 审计 `rbac.provider_tenant_admin_backfill`，并内置「首管可见菜单」探针（与 `/auth/menus` 同一服务）。

**验收标准**：provider 租户首管可见菜单包含自身 ITSM 全量（工单/事件/问题/变更/知识库/CMDB/SLA/AI）与 MSP 四菜单；客户租户首管不受影响（仍 `admin`）；无平台通配符；回填幂等（复跑零变更）。

**关闭记录（2026-10-06）**：代码 + 测试 + 回填 + 实测完成——

- 回归：`go test ./internal/authz ./pkg/bootstrap ./tests/parity ./middleware ./pkg/seeder ./service ./handlers/user ./handlers/tenant` 全绿（新增组合角色并集 / 三源 parity / 首管策略 / 菜单提权 / MCP 兜底面用例）；
- 存量回填实测：provider 租户 1/3/7/10 角色补齐，租户 7、10 首管 membership 重绑；幂等复跑 `perms_added=0, admins_rebound=0`；
- 菜单实测（与 `/auth/menus` 同一服务）：demo01 首管 **42 主菜单 + 35 管理菜单**，`main_names` 含「工单管理/事件管理/问题管理/变更管理/知识库/CMDB/SLA 管理/AI 助手」与「客户管理/跨客户工作台/分配管理/审计看板」双面。

## 13. 与 ADR-004 行动项的关系

| 本页缺口 | ADR-004 行动项 | 状态 |
|---|---|---|
| G1/G2 | A2–A3（租户开通与首个管理员） | 实测确认；G1→`IP-P0-5`（P0）→ **✅ 关闭（2026-09-30）**；G2→`IP-P1-5`（P1）→ **✅ 关闭（2026-09-30）** |
| G3 | A4–A5（MSP 授权与权限矩阵） | 实测确认；承接 `IP-P0-9`（P0）→ **✅ 关闭（2026-09-30）** |
| G4/G5/G6 | A2（供给可复现性） | 实测确认；脚本已规避，承接 `IP-P1-5`（P1） |
| G7 | 工具规范 | 实测确认；脚本已规避，P2 提升为通用约定（接受现状） |
| G8 | A8（缓存租户维度） | **P0**；承接 `IP-P0-2` 步骤 6（2026-09-30 上调） |
| G9 | A10（隔离回归） | ✅ 已关闭（IP-P1-8 v1.29；IP-P0-6 漏覆盖 header 冲突路径，复核后修复并落审计） |
| G10 | 部署/工具链（非 ADR 行动项） | 已文档化 + sha256 校验；P0 收尾（随 `IP-P0-1` 自检复核） |
| G11 | 非 ADR-004 行动项（canon §2.1 provider 双重定位的落地缺口） | **✅ 已关闭（2026-10-06）**：组合角色 + 首管策略 + 存量回填 |
| G12 | 非 ADR-004 行动项（契约同步治理） | **✅ 已关闭（2026-11-06）**：补两码 + 同源断言（§15） |
| G13 | 非 ADR-004 行动项（护栏口径） | **✅ 已关闭（2026-11-06）**：服务端租户级计数 + 前后端用例（§15） |
| G14 | 非 ADR-004 行动项（MSW-3 交付面） | ⏸ **待产品决策**：口径已成文，UI 接线另议（§15） |
| G15 | 非 ADR-004 行动项（`plan §10` TUM-7 冻结项的运维口径） | ⏸ **维持不做**：登记边界口径 + SOP（§16）；解除需先满足 `plan §10` 前置 |
| G16 | 非 ADR-004 行动项（OpenAPI 契约可见性） | ⏸ **待办**：本批执行 `make swagger-gen` 时发现刷新会带入他人 swagger 漂移，已回滚；注解补齐另批处理（§17） |
| G17 | 非 ADR-004 行动项（四层隔离 / 客户主数据设计的 BD-1 边界） | **P1，已裁决收回**：013 删除 `msp_contract_customer_read` + `msp_contract_binding_customer_read`；**前置** = 先建 `contract_sla_snapshots` 双写并观察一个结算周期（§18） |
| G18 | 非 ADR-004 行动项（多租户账号模型基线 vs 登录契约） | ⏸ **待裁决（两项绑定）**：要么组合唯一 + 登录必带租户标识，要么全局唯一 + 在 `AGENTS.md` 登记例外（§19） |
| G19 | 非 ADR-004 行动项（域命名与上游双实现） | ⏸ **待裁决唯一命名**后再吸收 change 评审域（§20） |

## 14. 证据索引

- 脚本与复现步骤：[02 文档 §10](./02-deployment-and-configuration.md)、[06 文档 §7](./06-verification-and-troubleshooting.md)；
- 代码位置：`middleware/tenant.go`、`middleware/msp_middleware.go`、`handlers/user/handler.go:516`、`pkg/seeder/tenant_provisioner.go:27-43`、`cmd/initialize`、`cmd/provision_tenant/main.go`；
- G11 代码位置：`internal/authz/roles.go`（组合角色派生）、`pkg/bootstrap/admin.go`（首管策略）、`pkg/seeder/seeder.go`（角色种子）、`middleware/rbac.go`（兜底 + rank）、`service/menu_service.go`（提权名单）、`cmd/backfill_provider_admin/main.go`（存量回填）；
- 生产实测数据：租户 3/4/5 供给计数、`msp_allocations` 三条分配、隔离性探针输出（06 文档 §7）。

## 15. G12–G14 · 平台侧租户用户通道的契约与护栏复核（2026-11-06）

**来源**：[`docs/review/2026-11-06-tenant-user-role-adjustment-review.md`](../review/2026-11-06-tenant-user-role-adjustment-review.md) v0.2 复核。
**线上生效（2026-11-06）**：G12/G13 已部署至 238 演示栈（`1.8.0-dev.057b3813`），
实测证据与回滚步骤见 [`evidence/238-deploy-20261106/tenant-user-admin-g12-g13.md`](./evidence/238-deploy-20261106/tenant-user-admin-g12-g13.md)。
三项都发生在 TUM/MSW-3 通道上，与「是否做跨租户角色调整」**无关**，属可独立修复的契约/UX 缺陷。

### G12 错误码词表漂移（已修复）

**现象**：后端 `service/tenant_user_admin.go` 常量表 12 项，前端 `tenant-user-admin-api.ts` 映射 10 项，
差 `TENANT_USER_MEMBERSHIP_NOT_FOUND`、`HOME_MEMBERSHIP_IMMUTABLE`；命中缺项时 `tenantUserAdminErrorMessage`
回落原始 message，中文界面直接透出英文/后端原文。

**修复**：补两项中文文案；在 `__tests__/tenant-user-admin-api.test.ts` 增加**同源断言**
（映射存在 + 文案关键次校验），后续后端加码而前端不同步即测试即红。

### G13 最后管理员护栏按当页估算（已修复）

**现象**：`TenantUsersDrawer` 原以 `items.filter(i => i.active && i.role === 'admin').length` 判定
「最后一个管理员」，而 `items` 只是**当前页**（`PAGE_SIZE_DEFAULT=20`，可选 10/20/50/100；后端上限 100）。
管理员分布到多页时，每页都可能算出 `<= 1` → **开关被假性禁用**，运维以为权限被锁。

**修复**：

1. `dto.TenantUserListResponse` 新增 `activeAdminCount`，由 `ListUsers` 按
   `tenant_id ∧ role=admin ∧ active=true` **全量计数**，刻意不接 keyword/status/role 筛选与分页偏移；
2. 前端取 `activeAdminCount` 作为护栏口径，并在启停乐观更新/失败回滚时同步该计数；字段缺失时回退当页估算（兼容旧服务端）；
3. 用例：后端断言「筛选/分页不收敛计数 + 跨租户不串」（`service/tenant_user_admin_test.go`），
   前端断言「`activeAdminCount=2` 时当页唯一管理员仍可停用」与「`=1` 时禁用」（`TenantUsersDrawer.test.tsx`）。

**不变量**：服务端 `SetActive` 的租户级校验（同租户其他活跃 admin 为 0 → `409 LAST_ADMIN_PROTECTED`）**始终是权威判定**，
前端禁用只是 UX 收敛，不得作为安全依据。

### G14 membership 撤销面「后端已实现、前端零接线」（口径已成文，UI 接线待决策）

**现状**：`POST /api/v1/tenants/:id/users/:userId/membership/revoke` 全链路就绪 ——
路由 `router/common_system_routes.go:372`、handler `handlers/tenant/user_admin.go:156`、
RBAC 预检 `middleware/rbac_precheck_gen.go:943`、服务 `service/tenant_user_admin.go` `RevokeMembership`
（平台面 `super_admin` 硬校验 + 写通道开关 + home 映射不可撤 409 + 分配级联 + 软删 + 双审计 + 权限缓存/在途 refresh 立即失效 + 租户通知），
来源为 MSW-3（`msp-membership-switch-consolidation-plan.md` §M2 实施记录第 ⑤ 项，交付形态即「撤销面 API」）。
`itsm-frontend/src` 对该端点**零引用**。

**本批处置**：只在 API 客户端注释与本表成文「当前仅 API/CLI 可达」，**不在本批擅自加 UI 动作**
（撤销成员映射是产品可见动作，且与「离职/停用」语义相邻，需评审确认动线）。

**待决策**：是否在 `TenantUsersDrawer` 行操作增「撤销成员映射」；若加，需同时明确与 `离职`/`停用` 的语义差别与二次确认文案。

**触发条件（本批判定「先不接线」的依据）**：跨租户撤销成员映射的运维请求**频次稳定出现（≥2 次/月）**，
或出现「成员映射已撤但用户在该租户仍可见数据」的实际案例。届时工作量是**纯前端接线**
（API 客户端 1 方法 + 行操作 + 二次确认 + 用例），后端、审计、通知、测试均无需改动。

## 16. G15 · 跨租户角色调整的职责边界口径（2026-11-06 登记）

**边界（现行设计，非缺陷）**：

| 方向 | 能否调权限 | 通道 |
|---|---|---|
| 本租户内 | ✅ **可以，且是主通道** | 目标租户管理员 `PUT /api/v1/users/:id`（主角色 + `roleIds`）、`roles` CRUD |
| 平台 → 他租户（建号时） | ✅ 可以 | `ProvisionUserToTenant` + `ListTenantGrantableRoles`（角色白名单 + rank 上限 + 配额） |
| 平台 → 他租户（建号后） | ❌ 不可 | 无端点；`plan §10` TUM-7 显式冻结 |
| 服务商 → 客户租户 | ❌ 不可 | MSP 面仅建号与工作台；同上冻结 |

**为什么不做**（`plan §10` 三条依据的 v0.2 现状）：

1. ~~「租户管理员也持 `tenant:write`，路由权限不足以区分平台面」~~ —— **该前提已被 2026-10-10 治理域收口 D7 推翻**
   （`internal/authz/roles.go:247-249,521-523`：`tenant` 家族为平台专属码，租户 `admin` 不再持有）；
2. **平台级角色词表未收敛**（仍成立）：`roles` 是租户内语义，`CanGrantRoles` 明确拒绝跨租户角色；
3. **无变更审批 / 双人复核**（仍成立）：单点即可提权。

**运维口径（SOP，替代人工工单外溢）**：

> 「用户角色/权限变更请由**该租户自己的管理员**在其租户内 `系统管理 → 用户管理` 操作（可改主角色与 RBAC 角色；
> 只能授予不高于自身 rank 的角色）。平台侧**只负责建号与账号治理**（重置密码 / 启停 / 强制下线 / 离职 / 撤销成员映射），
> **不代租户调整权限**。若目标租户已无可用管理员，走**恢复**通道：首管窄通道重置密码
> （`is_bootstrap_admin`，不受 `TENANT_USER_ADMIN_ENABLED` 约束）或服务器侧 `cmd/reset_password`，
> 恢复后再由该租户管理员自行调角色。**禁止**用 SQL 直改 `users.role` / `user_roles` 代替：该路径无 rank 校验、
> 无角色归属校验、无审计、且**不吊销存量会话**（旧 token 继续携带旧权限）。」

**SLA 影响**：跨租户权限变更请求一律转「目标租户管理员自助」，平台侧不承诺时效；仅当目标租户无可用管理员时按恢复类工单受理。

**解除冻结的前置**（三者齐备才另案评审，缺一即退回「不做」）：平台级角色词表收敛 + 变更审批/双人复核 +
先解决 G11 四处同源（`mspRole` 若纳入首批，必须联动 `internal/authz/roles.go` 组合角色派生、`pkg/bootstrap/admin.go`
首管策略、`middleware.RoleRank` + 菜单提权名单、`pkg/seeder` 角色种子，否则复现 G11 回归）。

## 17. G16 · TUM 平台面端点零 Swagger 注解（2026-11-06 执行期发现）

**发现过程**：本批为让 `activeAdminCount` 进入 OpenAPI 而执行 `make swagger-gen`，结果：

1. 生成的 `docs/{docs.go,swagger.json,swagger.yaml}` 中 `TenantUserListResponse` **0 次出现** ——
   因为 `itsm-backend/handlers/tenant/user_admin.go` 全部处理函数**没有任何 swagger 注解**
   （无 `@Summary` / `@Success` / `@Router`），swag 根本不会为这 7 个端点产出条目；
2. 同时刷新带入了**与本批无关的既有 swagger 漂移**（`/api/v1/projects/owner-team-candidates`、
   `project.HealthReason`、工时校验字段等，来自先前 commits 未随附文档再生成），单文件 +182 行；
3. 因此**已回滚** `itsm-backend/docs/*`：把它塞进缺陷修复提交会让评审无法分辨哪些改动属于本批，
   而它对 `activeAdminCount` 也毫无记录价值（字段压根不在文档里）。

**影响**：租户用户治理通道（列表/详情/重置密码/启停/强制下线/撤销成员映射/离职）对外**只存在于源码与方案文档**，
OpenAPI 不可见 → 外部集成方、客户端代码生成、以及 `docs-gate` 的 swagger 路径检查都覆盖不到它。
G12（错误码词表漂移）之所以能被人工发现，正是因为没有任何机器可校验的契约面。

**建议修复（单独一批，勿与缺陷修复混做）**：

1. 给 `handlers/tenant/user_admin.go` 7 个端点补注解，响应体显式引用
   `dto.TenantUserListResponse` / `dto.ResetTenantUserPasswordResponse` / `dto.ForceLogoutTenantUserResponse` 等；
   并把 `activeAdminCount` 的**语义与「不构成安全依据」**写进 `@description`（防后来者再按当页估算实现护栏）；
2. 顺带补 `docs/docs.go` 里 membership 撤销面（G14）与离职编排（v0.11）的缺失条目；
3. `make swagger-gen` 后**单独提交**（提交信息标注「含先前 commits 的 swagger 漂移补齐」），
   再跑 `bash scripts/docs-gate/run-all.sh`；
4. 建议在 CI/docs-gate 增一条**契约可见性检查**：`tenant:read|write` 路由集 ⊆ swagger 路径集，
   否则通道继续会靠人工比对才被发现。

**为什么不阻塞本批**：本批改的是响应体字段与前端取数，运行契约由 `service`/`jest` 用例锁定；
OpenAPI 缺项是**既有**缺口（先前 7 端点上线时就未补注解），不是本批引入。

## 18. G17 · `msp_contracts` RLS 012 客户只读跨租户放宽（2026-11-06 登记并裁决）

### 18.1 现象与证据

`itsm-backend/database/rls/migrations/012_msp_contract_policies.sql` 共四条策略，其中两条为跨租户读放宽：

| 策略 | 位置 | 谓词 | 效果 |
|---|---|---|---|
| `msp_contract_customer_read` | `012:54-58` | `FOR SELECT USING (customer_tenant_id = current_setting('app.tenant_id')::uuid)` | customer 租户可直读 provider 租户的 `msp_contracts` 行 |
| `msp_contract_binding_customer_read` | `012:81-88` | `EXISTS (SELECT 1 FROM msp_contracts c WHERE c.id = contract_id AND c.customer_tenant_id = ctx)` | `msp_contract_sla_bindings` 经子查询继承同一放宽 |

**暴露的字段**（整行可见，PostgreSQL 无列级 RLS）：`name`、`contract_no`、`status`、`effective_from/until`、`penalties`（违约率与上限）、`settlement_cycle`、`currency`。

### 18.2 方案声明的三个用途 vs 实测

| # | 声明用途 | 出处 | 实测 |
|---|---|---|---|
| 1 | L1 SLA 匹配（客户建单解析自己生效合同） | `service/ticket_sla_service.go:682-724`（`matchContractDefinition` 注释明写「customer ctx 下经 RLS 012 授信策略读取」） | ✅ **唯一真实依赖** |
| 2 | 下发视图 / provided 读面 | `012` 头注释 L4-5；方案 IM-15 | ⚠️ **未实现**。`router/sla_routes.go:83-92` 的 `/sla/contracts` 四端点全在 `MSPMiddleware` 的 provider 面；`itsm-frontend/src` 检索 `msp-contracts`/`mspContract` 零命中 |
| 3 | P2 结算报表读面 | `012` 头注释 L4-5 | ❌ **不成立**。`GET /sla/settlement-report` 亦在 `MSPMiddleware` + `RequireMSPPermission("sla","settlement:read")` 下，上下文是 provider 租户，`msp_contract_provider_all`（`tenant_id = ctx`）已完全覆盖 |

### 18.3 三条不可接受的性质

1. **无列级粒度**：客户需要的是「我的工单适用哪套时限」，却连带读到服务商的罚则口径与币种。
2. **无生命周期**：策略不含 `status` / 有效期谓词。合同 `terminated`、双方解约、甚至争议中的历史行，**永久可读**。
3. **不可审计**：客户读 provider 行在数据库侧无痕迹，无法回答「谁在何时读过对方的罚则条款」。

对比方案自身对 SLA 定义的处理（`sla_definition_grants` 独立授权行、`is_active`、`valid_from/until`、可单独撤销），同一份方案内两处通道模式不一致：**合同把行上的 `customer_tenant_id` 业务字段当作了授权凭据**。

### 18.4 为什么不能「直接 DROP」

```
matchContractDefinition（customer ctx）
  → client.MSPContract.Query()
  → RLS driver（database/rls/driver.go:151）把非 system-bypass 的租户作用域语句路由到低权池 itsm_app
  → 删除 customer_read 后返回 0 行
  → L1 未命中 → L2 → L3b → L4
  → L4 fail-loud，但返回默认 60 / 480 分钟
  → 结果：合同 SLA 不再生效，且只有 warn 日志
```

即**合规改造变成业务事故**：计时不报错，覆盖率虚高、漏报违约、结算基准失真。

### 18.5 处置：收回（分两步，禁止先 DROP）

| 步骤 | 内容 | 门禁 |
|---|---|---|
| **Step 1（新增）** | 建 `contract_sla_snapshots`（`tenant_id = 客户租户`，RLS `tenant_id = ctx`）+ provider 侧合同 active/变更/终止时下发；`matchContractDefinition` 改读快照（双写期快照优先、保留回退） | 观察**一个结算周期**，L1 命中率与改前一致 |
| **Step 2（收回）** | 移除快照回退分支 → 执行 `013` DROP 两条策略 → 验收 | C1 客户侧无直读、C2 存量结算单已迁 `settlement_statements`、C3 命中率无下降、C4 回滚脚本已演练 |

DDL、四项前置检查与回滚脚本见 [`docs/customer-master-data/02-ddl-and-migration.md` §12.6](../customer-master-data/02-ddl-and-migration.md)；完整因果与 L1 改道三方案对比见 [`07-tenant-data-boundary.md` §3.2 / §4.5](../customer-master-data/07-tenant-data-boundary.md)。

### 18.6 已明确否决的两条替代路径

| 路径 | 否决理由 |
|---|---|
| **L1-A 用 `tenantctx.SystemContext` 走 BYPASSRLS** | 技术上可行（仓库既有 `SystemContext` 惯例），但让「请求路径常态持有跨租户读权限」，与「受控访问 ≠ 跨租户读表」自相矛盾；一次谓词写错即全量泄露 |
| **保留现状（不动策略）** | 与「所有业务数据都不跨租户」硬约束正面冲突；且策略无法收窄、无生命周期，任何新增 customer 侧查询（导出、搜索建议、AI 检索）都会自动继承这份可见性，应用层失去兜底 |

### 18.7 一句话

> G17 不是「权限设计缺失」，而是**把外键字段误当授权凭据**导致的边界溢出；修法不是补授权，而是**把跨租户读换成客户租户内副本**，且必须与 L1 改道同批。

## 19. G18 · `users.username` 唯一性口径（2026-11-06 登记，待裁决）

**发现路径**：为规划吸收上游 `heidsoft` 能力而做不变量测绘时命中。不是新功能缺陷，而是**两份内部规范互相矛盾**，且既有登录实现把矛盾固化了。

### 19.1 冲突三方

| 方 | 立场 | 证据 |
|---|---|---|
| 仓库基线 | 「业务唯一键默认按 tenant 组合唯一」 | `AGENTS.md:245` |
| 本地实现 | `users.username` **全局唯一** | `itsm-backend/ent/schema/user.go:21-23`（`field.String("username").Unique()`） |
| 本地绕行 | 首管命名 `admin-<tenantCode>`（username 与 email 都带后缀） | `pkg/bootstrap/token.go:79`、`pkg/bootstrap/admin.go:100`；登记为 `07:G2` 的关闭方式 |
| 上游实现 | 已按基线放宽为 `(tenant_id, username)`，并**刻意保留 email 全局唯一** | `heidsoft/main:itsm-backend/migrations/20261006_tenant_scope_user_username_unique.sql` 头注释（症状：第二个租户建 `admin` 必撞全局唯一键） |

### 19.2 为什么"直接对齐基线"不是免费的（本轮关键实测）

```
handlers/common/service.go:195-199
  if tenantID == 0 {   // 登录未指定租户
      entUser, err = s.client.User.Query().
          Where(entuser.UsernameEQ(username)).
          Only(tenantctx.SystemContext(ctx, "auth:login", "pre-auth username lookup (cross-tenant)"))
```

- `.Only()` 语义是"恰好一条"，**多命中即报错** → 一旦 username 变为租户内唯一且出现同名 `admin`，
  **不带 `tenantCode` 的登录会硬失败**（不是"随便选一个租户"，更不是静默歧义）；
- 注册路径同样依赖全局唯一：`handlers/auth/service.go:288-294` 在跨租户作用域查重并直接报"用户名已被注册"；
- 因此 G18 是**绑定决策**：唯一键口径与登录契约必须一起定，任何一半单独实施都会造成登录倒退。

### 19.3 选项与代价

| 选项 | 内容 | 必须同时做的事 | 残余风险 |
|---|---|---|---|
| **A 对齐基线**（组合唯一） | 删全局唯一约束，建 `(tenant_id, username)` 唯一 | ① 登录契约变更：`tenantCode`/domain 必填或引入账号消歧步骤；② 复核 `handlers/auth`（注册查重、找回密码）、`pkg/bootstrap`、`cmd/initialize`、`cmd/provision_tenant`、`cmd/backfill_provider_admin`、邀请流程；③ 更新 `07:G2` 的关闭表述；④ 前端登录页加租户标识输入/域名解析；⑤ 若同时推进 SSO（吸收候选），IdP 主体名与租户映射需一并设计 | 存量"只输用户名即可登录"的所有入口（含 API 客户端、脚本、集成）行为变化，属**产品可见变更** |
| **B 维持全局唯一** | 保持现状 | 在 `AGENTS.md` 明确登记 `users.username` 为**例外**并写明理由（登录预认证无法确定租户），消除自相矛盾 | 每租户 `admin` 直觉永久不可得；上游多租户相关能力吸收时需逐处改名适配 |

### 19.4 验收标准（任一选项都要满足）

1. `AGENTS.md` 与 schema/迁移三者表述一致（无"文档说组合唯一、代码全局唯一"的分裂）；
2. 不带 `tenantCode` 的登录在**两个选项下都有确定行为**（成功或明确错误码，不得出现 `Only()` 非唯一错误裸奔 500）；
3. 连续开通 2 个以上租户的首管流程有回归用例（`pkg/bootstrap` 既有对应用例需扩展）；
4. 若选 A：提供迁移 + 回滚对，且 `cmd/mig-verify -ro` 只读预检零阻塞（`docs/plan/multi-llm-provider-plan.md` §4.4 账本漂移教训）。

**优先级**：P1。**阻塞**：上游 B 类差集吸收、部分 A 类（依赖多租户首管语义者）。

## 20. G19 · `cab` 与 `change_review` 同能力双命名（2026-11-06 登记，待裁决）

**实测对照**（四条端点方法与权限码完全一致）：

| 端点 | 本地 `router/cab_routes.go:15-18` | 上游 `router/change_review_routes.go` |
|---|---|---|
| `GET /members` | `RequirePermission("change","read")` | 同 |
| `POST /members` | `RequirePermission("change","write")` | 同 |
| `PUT /members/:id` | `RequirePermission("change","write")` | 同 |
| `DELETE /members/:id` | `RequirePermission("change","write")` | 同 |

- 表：本地 `ent/schema/cab_member.go` ↔ 上游 `ent/schema/change_review_member.go`；
- 上游另有 `20260921_drop_cab_members_table.sql`，注释写明"CAB 已重命名为 change_review（评审组），数据迁移到 `change_review_members`，`cab_members` 从未被新代码写入"；
- 两侧 handler 语义注释几乎一致（都是"仅名册管理，评审流转由审批链引擎驱动"）。

**改名成本实测**：后端 26 文件、前端 17 文件（`itsm-frontend/src/types/cab.ts`、`routes/area-permissions.ts` 等）、文档与 `docs/acl-manifest.yaml` 5 文件。

**裁决要求**：
1. 定唯一对外命名（ITIL 术语 `cab` vs 泛化 `change_review`），含权限码、菜单 key、路由前缀、类型名；
2. 若统一为 `change_review`：给 `cab_members → change_review_members` 迁移 + 回滚对（上游声称旧表"从未被新代码写入"，**须以本地库实测核实**后再决定是否需要数据搬迁）；
3. 在裁决前，**禁止**吸收上游 change 评审域任何文件（否则立刻出现第二套名册与同名端点）。

**优先级**：P1。**阻塞**：change 评审域相关吸收。
