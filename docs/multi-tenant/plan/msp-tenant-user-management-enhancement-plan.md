# 平台侧租户用户管理增强方案（/admin/tenants · 跨租户账号治理）

> 状态：**v0.4（决策冻结 + TUM-1/TUM-2/TUM-3 后端落地）**｜日期：2026-10-05｜基准：仓库 HEAD `039371ef`
> 上位：[canon v1.0](./msp-concept-model-and-architecture-canon.md)（canon D5/D8/D11、附录 C）→ [目标架构](./msp-target-architecture.md)（§6 权限矩阵/建号）→ [实施方案](./msp-implementation-plan.md)（IP-P0-5 建号通道、IP-P2-6 硬配额）
> 定位：**L4 方案**（局部编号 `TUM-#`，跨文档引用必须带前缀）。范围：平台管理员在租户治理页（`/admin/tenants`）对**任意租户**用户的查看与账号治理（重置密码 / 启用停用 / 强制下线 / 审计）；不含 MSP 服务商面工作台（其可复用同一服务能力，另行接线）。

---

## 0. 摘要（TL;DR）

- **现状**：跨租户「建号」已通（`POST /api/v1/tenants/:id/users` + 平台建号弹窗），但**建号之后无管理**——重置密码、启停、批量等能力全部困在「调用方当前租户」作用域内（`/api/v1/users/*`），平台管理员在 `/admin/tenants` 既看不到某租户的用户清单，也无法代运维其账号。
- **本方案**：新增**租户维度用户管理通道**（读 + 账号治理），复用既有安全资产（按目标租户的密码策略、存量 token 吊销、一次性强口令 + 强制改密、`tenantctx` 系统旁路、审计掩码），前端在租户行新增「用户管理」抽屉。
- **P0 交付**：租户用户列表/详情、重置密码（一次性口令 / 指定新密码 两种模式）、启用/停用（含「最后管理员」护栏）、审计留痕。
- **P1**：批量启停、强制下线（吊销会话）、审计时间线、导出。
- **P2（另行评估）**：删除用户（不做物理删除，见 TUM-D8）、跨租户角色调整。
- **决策状态**：TUM-D1–TUM-D10 已按最佳实践冻结；原开放问题 Q1–Q5 全部收口（见 §9 决策确认记录），无 blocking 待决项。
- **体验与闭环（v0.2 新增）**：§4.4 操作便利性（核心动作 ≤3 步、深链可复现、失败可重试、结果可复制）与 §2.3 业务逻辑闭环（场景 S1–S3 + 闭环点 C1–C6：用户侧即时反馈 / 口令转交责任链 / 租户侧可见性 / 对账一致 / 通知触达 / 异常补偿）。
- **前置安全结论**（关键）：路由权限 `tenant:write` **不能**作为平台面判定依据——租户管理员同样持有该权限（`itsm-backend/internal/authz/roles.go:243`）；服务层必须叠加平台面判定（`role == super_admin`，沿用 2026-10-05 越权修复范式 `itsm-backend/handlers/tenant/handler.go:89-127`），否则构成跨租户越权。

---

## 1. 现状基线（代码为准，2026-10-05）

### 1.1 后端已有能力

| 能力 | 位置 | 作用域 | 备注 |
|---|---|---|---|
| 用户 CRUD/状态/重置/批量/统计 | `itsm-backend/router/common_system_routes.go:42-57` | **调用方当前租户**（`c.GetInt("tenant_id")`） | 平台管理员默认落在 `default` 租户，管不到其他租户 |
| 重置密码（指定新密码） | `handlers/user/handler.go:446-484`、`service/user_service.go:474-507` | 当前租户 | bcrypt 重设 + 目标租户密码策略 + **吊销全部存量 access token**；**不置** `must_change_password`，无一次性口令模式 |
| 启停用户 | `service/user_service.go:438-471` | 当前租户 | 停用即吊销 token；禁止停用当前登录用户；**无**「最后管理员」护栏 |
| 批量启停/部门 | `service/user_service.go:552+`、路由 `:56` | 当前租户 | 含「批量停用不含登录用户」护栏 |
| 跨租户**建号** | 路由 `common_system_routes.go:202`、`handlers/user/handler.go:120-188`、`service/user_provisioning.go:119-128` | 目标租户 | 唯一入口 `UserProvisioningService.ProvisionUser`：三通道 + 角色白名单 + rank 上限 + `tenantctx.WithProvisioningBypass` + 写守卫放行 + `maxUsers` 配额 + 稳定错误码 |
| 首管一次性口令 | `handlers/tenant/provisioning.go:84+`、`service/tenant_provisioning_service.go:131-203` | 目标租户 | 服务端生成 16 位强口令、**仅本次响应回显**、`must_change_password=true`；`tenantctx.SystemContext` 旁路 |
| 审计中间件 | `middleware/audit.go:243-281`、`middleware/mask.go:34-40` | 自动 | 写操作自动留痕；密码字段掩码；含 `source`/`target_tenant_id`/`actor_account`（IP-P0-10） |
| 平台面越权修复范式 | `handlers/tenant/handler.go:89-127` | — | 非 `super_admin` 的租户目录收敛为「本租户 + 直属客户」；新通道照此范式实现 |

### 1.2 前端现状

| 页面 | 位置 | 已有 | 缺口 |
|---|---|---|---|
| 租户治理页 | `itsm-frontend/src/pages/(main)/admin/tenants/index.tsx` | 租户 CRUD/状态/用量/开通向导/「建号」按钮（`:411-418`、`:777+`） | **没有**用户维度入口：看不到任意租户的用户，无法重置/启停 |
| 用户管理页 | `itsm-frontend/src/pages/(main)/admin/users/index.tsx` | 搜索/创建/编辑/启停 `Switch`（`:301-308`）/重置密码 `Modal`（`:221`、`:742+`）/批量 | 全部作用于**当前租户**；交互件可复用、作用域不可复用 |
| 建号弹窗 | `itsm-frontend/src/components/provisioning/ProvisionUserModal.tsx` | 平台/MSP 双通道复用、密码策略提示 | 仅创建，无后续管理 |

### 1.3 可复用安全/一致性资产（实现清单）

1. **密码策略**：`UserService.PasswordPolicy(ctx, tenantID)` —— 按**目标租户**校验，不引入新策略源；
2. **会话吊销**：`middleware.InvalidateUserAccessTokens(ctx, userID, now)` —— 停用/重置后立即失效存量 token；
3. **系统旁路**：`tenantctx.WithProvisioningBypass(...)`（建号带审计）/ `tenantctx.SystemContext(ctx, component, reason)`（供给类）—— 旁路必须收敛到**单一 service 入口**，禁止 handler 自行拼装；
4. **一次性口令**：复用 `generateBootstrapAdminPassword` + 「仅回显一次」的前端展示模式；
5. **审计**：中间件自动审计 + 显式审计（`RecordTenantDeniedAudit` 样式）；密码/口令字段一律掩码；
6. **配额与租户状态**：`TenantQuotaService`（`maxUsers`）、`TENANT_SUSPENDED` 检查口径（建号拒绝；读/治理动作见 §3.3）。

### 1.4 差距清单（本方案要解决）

| # | 差距 | 影响 |
|---|---|---|
| TUM-G1 | 平台面**无法跨租户读**用户（列表/详情/检索） | 平台运维必须借用租户自身会话，排障与合规审计困难 |
| TUM-G2 | 重置密码仅「指定新密码」，且**不置强制改密**、无一次性口令模式 | 口令经平台人手传递/回显不可控；不符合首管一次性口令的既有安全基线 |
| TUM-G3 | 启停/会话吊销**无跨租户通道**；缺「最后管理员」防呆 | 可能把租户管理面整体锁死；停用后残留会话处理不可控 |
| TUM-G4 | 审计与可见性不足：操作未与**目标租户/目标用户**显式关联，前端无查看入口 | 平台治理动作不可追溯/不可复核 |

---

## 2. 需求（平台管理员视角）

### 2.1 用户故事

| # | 故事 | 优先级 |
|---|---|---|
| U1 | 作为平台管理员，我在租户行的「用户管理」抽屉中，可按 用户名/姓名/邮箱 搜索、按 状态/角色/MSP 角色 过滤并分页浏览该租户全部用户 | P0 |
| U2 | 我可以查看单个用户详情（基本信息 + 角色 + 账号安全标识：`must_change_password`、最近更新），只读 | P0 |
| U3 | 我可以**重置用户密码**：① 生成一次性强口令（仅回显一次、首登强制改密）；② 指定新密码（遵循目标租户密码策略，可选「下次登录需改密」） | P0 |
| U4 | 我可以**停用**用户：目标用户立即无法登录且存量会话失效 | P0 |
| U5 | 我可以**启用**用户，恢复其登录能力 | P0 |
| U6 | 我可以**强制下线**（仅吊销会话，不改密码/状态）——用于账号疑似泄露的应急处置 | P1 |
| U7 | 我可以对筛选结果**批量启用/停用**（≤50 条，带护栏与逐条结果） | P1 |
| U8 | 我可以导出该租户用户清单（审计/对账用途，含脱敏字段） | P2 |
| U9 | 我可以删除用户（仅当无业务引用；二次确认 + 输入租户编码） | P2（另行决策，默认不提供） |

### 2.2 非功能需求

- **权限最小化**：平台面仅 `super_admin` 可用；租户侧调用一律 403（fail-closed）；
- **默认关闭**：写操作由独立灰度开关控制（见 TUM-D3），可随时回滚；
- **幂等**：重复停用/重复生成口令语义明确（见 §3.3）；
- **审计完备**：actor / target_tenant / target_user / mode / 结果 全字段可查；明文口令绝不入库/入日志；
- **一致性**：错误码稳定（前端可映射文案）；分页 ≤100/页；列表查询命中既有索引（`tenant_id` + 状态/用户名）；
- **前端**：i18n（zh/en）、可访问性（抽屉焦点管理）、危险操作二次确认。

### 2.3 业务逻辑闭环（v0.2 新增）

**生命周期全景**：建号（platform / MSP / invite）→ 首登强制改密 → 日常使用（RBAC/租户隔离）→ **平台治理动作**（重置密码 / 启停 / 强制下线）→ 用户侧即时反馈 → 审计可追溯 → 对账与复盘。

**闭环场景（端到端验收剧本）**

| 场景 | 步骤链 | 闭环判据 |
|---|---|---|
| S1 租户排障（员工锁死） | 平台：重置·一次性 → 线下转交口令 → 用户首登强制改密 → 正常登录 | 旧口令/旧会话全失效；`must_change_password` 生效；审计含目标租户与目标用户 |
| S2 员工离职 | 平台：停用 → 用户立即掉线/无法登录 → （P1）通知目标租户管理员 → 审计留痕 | 即时 401；租户管理员收到通知；动作可复核 |
| S3 安全事件 | 平台：强制下线 → 重置密码 → 审计看板复核 | 会话双吊销（access+refresh）；动作按 `target_tenant_id` 可查 |

**闭环点清单（断点 → 本方案如何补）**

| # | 闭环点 | 现状断点 | 方案 |
|---|---|---|---|
| C1 | 用户侧即时反馈 | 停用后登录文案、强制下线后前端行为未纳入验收 | §4.5 定义稳定文案与 401 引导；TUM-A8 覆盖 |
| C2 | 口令转交责任链 | 一次性口令仅平台可见 | 前端口令卡片含「转交提示 + 仅显示一次」；本文明确线下/工单转交责任 |
| C3 | 租户侧可见性 | 平台动作租户侧不可见 | 审计按 `target_tenant_id` 过滤（既有）+ `target_user_id` 用户时间线（新增列，TUM-3） |
| C4 | 对账一致性 | 用户列表与配额用量可能口径不一 | 列表 `total` 与 `TenantQuotaService` 的 `users` 用量使用同一查询源；TUM-A8 断言 |
| C5 | 通知触达（P1） | 治理动作无通知 | 复用既有通知域（`notification` / `notification_delivery` / `notification_preference`）向**目标租户管理员**发站内通知（不含口令） |
| C6 | 异常补偿 | 批量部分失败无后续 | 逐条结果 + 失败项一键重试（TUM-D7） |

---

## 3. 目标契约

### 3.0 决策（v0.2 定稿：按最佳实践确认，含原开放问题收口）

| # | 决策 | 结论（已确认） | 理由 |
|---|---|---|---|
| TUM-D1 | 平台面判定 | 路由权限 `tenant:write` 仅作粗筛；**服务层必须叠加 `role == super_admin`**，不满足 → 403 `PLATFORM_SCOPE_REQUIRED` | `tenant:write` 租户管理员同样持有（`internal/authz/roles.go:243`），仅凭它无法区分平台面；沿用 2026-10-05 修复范式 |
| TUM-D2 | 服务归属 | 新建 `TenantUserAdminService`（读 + 账号治理），复用 `tenantctx` 旁路与 `mspguard` 校验；**不并入** `UserProvisioningService`（建号 vs 治理的审计语义不同） | 职责分离；旁路入口仍保持「每类动作单一 service 入口」 |
| TUM-D3 | 灰度开关 | 新增 `TENANT_USER_ADMIN_ENABLED`（默认 `false`），与 `USER_PROVISIONING_CHANNELS_ENABLED` 独立；读列表随路由上线（仍受 TUM-D1 判定） | 独立回滚；避免与建号灰度耦合 |
| TUM-D4 | 重置默认模式 | 默认「一次性口令 + 强制改密」（`mode=generated`）；「指定新密码」为次选（`mode=specified`）；两者都触发会话双吊销 | 安全优先；与首管一次性口令基线一致 |
| TUM-D5 | 停用护栏 | 硬约束：**目标租户最后一个可用管理员不可停用**（409 `LAST_ADMIN_PROTECTED`）；`default` 平台租户额外要求 `confirmCode=default`；禁止停用调用者自身 | 防呆：避免租户管理面锁死；平台根租户是全局兜底 |
| TUM-D6 | 会话吊销范围（原 Q1） | **access + refresh 双吊销**。access 复用按用户 `minIssuedAt`（`InvalidateUserAccessTokens`，`middleware/token_revocation.go:67-72`）；refresh 在同一 revocation store 扩展**按用户最低签发时间**（`refresh:user_min_iat:<uid>`）并在 RefreshToken 路径校验；现有按 token 黑名单（`service/token_blacklist_service.go:75-131`）保留为轮换/切换防护。Redis 不可用时沿用现有内存降级（单实例语义），写入部署前提 | refresh 仅按 token 拉黑，无法覆盖「用户所有历史 refresh」；停用/重置必须全量断链，否则可用旧 refresh 续签绕过 |
| TUM-D7 | 批量语义（原 Q2） | 上限 **50 条**；整体 200 + 逐条结果（成功/失败码）；**仅本页全选**（禁止跨页全选）；失败项支持一键重试 | 与既有批量上限一致；防「误伤页面外数据」；部分失败可恢复 |
| TUM-D8 | 删除用户（原 Q3） | **不提供物理删除**；停用为唯一「移出」手段；合规性删除（如被遗忘权）走独立离线流程与法务评估（P2 评估，不在本方案） | 工单/审计/引用完整性成本高；误删不可逆 |
| TUM-D9 | 审计只读角色（原 Q4） | 本批不新增权限词表，平台面统一 `super_admin`；未来如需只读审计角色，按 canon 附录 C 流程新增 `tenant.user.read_platform` 词表项 | 避免词表变更成本与双源风险；本批范围可控 |
| TUM-D10 | MSP 面复用（原 Q5） | 后端 `TenantUserAdminService` 设计为可复用；前端 MSP 入口另案接线（不在本批） | 控制交付面；服务能力一次实现两处受益 |

### 3.1 API 契约（新增，挂 `/api/v1/tenants` 组）

> 统一前缀 `/api/v1`；响应包均为既有 `common.Response`（`code/message/data`）。

| 方法/路径 | 粗筛权限 | 说明 | 阶段 |
|---|---|---|---|
| `GET /tenants/:id/users` | `tenant:read` | 列表。Query：`page`(default 1)、`pageSize`(default 20, max 100)、`search`（用户名/姓名/邮箱模糊）、`status`(`active/inactive`)、`role`、`mspRole`；响应 `PagedUsersResponse`（`dto/user_dto.go:72`） | P0 |
| `GET /tenants/:id/users/:uid` | `tenant:read` | 详情。响应 `UserDetailResponse`（含 `active/role/roleIds/roleNames/mspRole/createdAt/updatedAt`；不含敏感字段） | P0 |
| `POST /tenants/:id/users/:uid/reset-password` | `tenant:write` | 重置密码。请求：`{mode:"generated"\|"specified", newPassword?, requireChange?}`；响应：`{mode, password?, mustChangePassword}` | P0 |
| `PUT /tenants/:id/users/:uid/status` | `tenant:write` | 启停。请求 `{active:boolean}`；停用=吊销存量 token；触发护栏 → 409 | P0 |
| `POST /tenants/:id/users/:uid/revoke-sessions` | `tenant:write` | 强制下线（仅吊销会话） | P1 |
| `PUT /tenants/:id/users/batch-status` | `tenant:write` | 批量启停。请求 `{userIds:[], active:boolean}`；响应逐条结果 `{succeeded:[], failed:[{userId,code,message}]}`，上限 50 | P1 |
| `GET /tenants/:id/users/export` | `tenant:read` | 导出 CSV（脱敏：无手机号/无密码列；含审计水印） | P2 |

**重置密码响应示例（`mode=generated`）**

```json
{
  "code": 0,
  "message": "success",
  "data": {
    "mode": "generated",
    "username": "b2ccust_admin",
    "password": "Xk7#mQ2pL9@vR4tZ",
    "mustChangePassword": true,
    "sessionsRevoked": true
  }
}
```

**约束**：
- `mode=generated`：服务端生成 16 位强口令（复用 `generateBootstrapAdminPassword`），**明文仅本次响应回传一次**；`mustChangePassword` 强制 `true`；
- `mode=specified`：`newPassword` 必填，按**目标租户**密码策略校验（失败 400 并透出规则）；`requireChange` 默认 `true`；
- 两种模式均调用 `InvalidateUserAccessTokens`，并在响应中回执 `sessionsRevoked`；
- 幂等：重复 `status` 相同值返回 200（不重复吊销）；重复 `generated` 每次生成新口令（旧口令即失效）——前端需提示「重新生成会作废上一次口令」。

### 3.2 权限与作用域矩阵（fail-closed）

| 调用方 | 通道 | 目标租户范围 | 结果 |
|---|---|---|---|
| 平台管理员（`super_admin`） | `/tenants/:id/users*` | 任意租户 | ✅ 允许（写操作需 `TENANT_USER_ADMIN_ENABLED=true`） |
| 租户管理员（`admin`，持有 `tenant:write`） | `/tenants/:id/users*` | — | ⛔ 403 `PLATFORM_SCOPE_REQUIRED`（即使 `:id` 是自己的租户，也引导走 `/users/*` 租户面） |
| MSP 服务商员工 | `/tenants/:id/users*` | — | ⛔ 403（服务商面通道另行接线，不在本方案） |
| 任何调用方 | 目标租户不存在 | — | 404 `TENANT_NOT_FOUND` |
| 任何调用方 | 目标用户不在目标租户 | — | 404 `USER_NOT_IN_TENANT`（防 IDOR：先按 `(tenant_id, user_id)` 双键查询） |

**RLS/写守卫**：service 内部以 `tenantctx` 将 ctx 的租户绑定为**目标租户**（读写均然）；旁路调用格式统一 `WithTenantID(target)` + `With*Bypass(actor, component, target)`，保证 `app.current_tenant_id` 与审计 reason 同源。

### 3.3 语义定义

| 主题 | 口径 |
|---|---|
| 重置·一次性 | 生成 16 位；`must_change_password=true`；吊销存量 token；明文仅响应一次；**后端不落库明文、不写日志**；前端口令卡片关闭后不可再取（需重新生成） |
| 重置·指定 | 目标租户密码策略校验；`requireChange` 默认 true；吊销存量 token；后端立即以 bcrypt 存储 |
| 首登强制改密 | 沿用 `must_change_password` + `/api/v1/auth/change-password` + 前端 `RequireAuth` 收敛（既有链路，不新增机制） |
| 停用 | `active=false` + 吊销存量 token；对目标租户后续请求即时 401；重复停用幂等返回 200 |
| 启用 | `active=true`；不自动恢复任何旧会话（用户需重新登录） |
| 最后管理员护栏 | 判定：同租户内 `role='admin' ∧ active=true ∧ deleted_at IS NULL ∧ id<>目标` 计数为 0 → 409 `LAST_ADMIN_PROTECTED`（防止租户失去唯一管理入口） |
| `default` 平台租户 | 额外要求请求携带 `confirmCode=default`；且禁止停用调用者自身/最后一个 `super_admin` |
| 目标租户状态 | `suspended/expired`：**读允许**；重置/启停允许（运维场景）；**建号拒绝**（现状 `TENANT_SUSPENDED`）——本方案不改变建号口径 |
| 会话吊销范围 | **access + refresh 双吊销**（TUM-D6）：access 走按用户 `minIssuedAt`；refresh 走同 store 的按用户最低签发时间校验；现有按 token 黑名单保留；Redis 不可用时沿内存降级（单实例） |

### 3.4 审计事件（写操作全覆盖）

| 动作 | 审计 action（建议） | 关键字段 | 备注 |
|---|---|---|---|
| 重置密码 | `user.admin_password_reset` | actor_user_id、target_tenant_id、target_user_id、mode、sessions_revoked | service 显式审计（`TenantUserAdminService.recordAudit`）；`newPassword`/`password` 由 `mask.go` 掩码，**一次性口令不写审计**；中间件自动行同时携带 target 标记（handler `c.Set`） |
| 启停 | `user.admin_status` | 同上 + active/to | 同上 |
| 强制下线 | `user.admin_force_logout` | 同上 | 同上 |
| 批量启停 | `user.admin_batch_status` | 批量计数 + 逐条失败码 | P1（未实现），逐条结果写审计摘要 |

> 行归属保持 actor home 租户（`tenant_id`），目标租户落 `target_tenant_id`（IP-P0-10 既有字段），确保「平台动作」在审计看板可按目标租户过滤。
> ✅ v0.4（TUM-3）：`audit_logs.target_user_id` 可空列 + 索引 `(target_tenant_id, target_user_id, created_at)` 已落地（迁移 `20261005_audit_target_user_id.sql`）；审计查询支持 `targetUserId` 过滤；历史行 NULL 按 legacy 处理。

### 3.5 错误码（稳定，前端映射文案）

| 码 | HTTP | 触发 |
|---|---|---|
| `PLATFORM_SCOPE_REQUIRED` | 403 | 非 `super_admin` 调用平台面通道（TUM-D1） |
| `TENANT_USER_ADMIN_DISABLED` | 403 | 写通道灰度开关 `TENANT_USER_ADMIN_ENABLED` 关闭（默认关；读通道不受限） |
| `TENANT_NOT_FOUND` | 404 | `:id` 目标租户不存在 |
| `TENANT_USER_NOT_FOUND` | 404 | `(tenant_id, user_id)` 双键未命中（含 IDOR 尝试） |
| `INVALID_PARAM` | 400 | 参数非法（ID/分页/状态过滤值/缺 `active`） |
| `PASSWORD_POLICY_VIOLATION` | 400 | `specified` 新密码不满足目标租户策略（透出规则） |
| `INVALID_RESET_MODE` | 400 | `mode` 非 `generated/specified`；或 `specified` 缺 `newPassword` |
| `LAST_ADMIN_PROTECTED` | 409 | 停用目标租户最后一个可用管理员 |
| `SELF_OPERATION_FORBIDDEN` | 409 | 停用调用者自身 |
| `TENANT_CONFIRM_REQUIRED` | 409 | `default` 平台租户启停需 `confirmCode=default`（TUM-D5） |
| 复用 | 400/422 | 通用参数错误（400）；`TENANT_QUOTA_EXCEEDED`（422，仅建号路径复用） |

---

## 4. 前端设计（`/admin/tenants`）

### 4.1 入口与结构

- 租户行操作新增 **「用户管理」**（`Users` 图标，与既有「建号」并列；`index.tsx:411-418` 同区域）；
- 打开 `TenantUsersDrawer`（`itsm-frontend/src/pages/(main)/admin/tenants/components/TenantUsersDrawer.tsx`，宽 920）：
  - 头部：租户名称/编码/类型标签 + 用户总数；
  - 工具条：搜索框（300ms 防抖）、状态筛选、角色筛选、刷新；
  - 表格列：用户名、姓名、邮箱、角色（Tag）、MSP 角色（Tag，可空）、部门、状态（Switch）、首登待改密（Badge）、创建时间、操作；
  - 操作：重置密码、启用/停用（Popconfirm）、强制下线（P1）、批量选择条（P1）。

### 4.2 关键交互

| 交互 | 设计要点 |
|---|---|
| 重置密码 Modal | 两步：① 选择模式（一次性口令 / 指定新密码）② 确认；指定模式内嵌密码策略提示（来自租户策略文案，与服务端同一错误码映射） |
| 一次性口令结果 | 独立 `Alert` 卡片：口令 + 复制按钮 + 「仅显示一次，关闭后需重新生成」警示 + 「已保存」勾选后方可关闭（防误关） |
| 停用确认 | Popconfirm 文案含「该用户将立即掉线」；若为最后管理员 → 按钮禁用 + Tooltip「该租户最后一个管理员不可停用」；`default` 租户 → 二次输入租户编码确认 |
| 错误映射 | 403/404/409/422 → antd message 文案（`PLATFORM_SCOPE_REQUIRED`/`LAST_ADMIN_PROTECTED`/密码策略等） |
| 权限收敛 | 无 `tenant:write` 或非 `super_admin`：不渲染「用户管理」入口（仅 UX 收敛，安全在后端 TUM-D1） |

### 4.3 复用与拆分（避免复制 `admin/users`）

- 抽取共享组件：`UserStatusToggle`、`ResetPasswordModal`（从 `admin/users/index.tsx:221/742+` 抽取，两处复用）；
- `admin/users` 页面改为引用共享组件（行为不变，测试回归）；
- 新增 `TenantUsersDrawer` 及其 API 客户端 `itsm-frontend/src/lib/api/tenant-user-admin-api.ts`（锁定契约用例）；
- 单测：抽屉（列表/搜索/空态/两种重置模式/停用护栏/错误码映射）；`tsc`/`eslint`/目标 jest 全绿。

### 4.4 操作便利性设计（平台操作者，v0.2 新增）

**原则**：核心动作 ≤3 步；不跳页、不记路径、不重复输入；危险动作「后果先行」；失败可恢复、结果可复制。

| 维度 | 设计 |
|---|---|
| 入口 | 租户行「用户管理」；抽屉深链 `?tenantUsers=<tenantId>`（刷新/分享可复现）；打开即展示用户总数（来自列表 `total`，避免租户列表 N+1） |
| 搜索与筛选 | 300ms 防抖；支持 用户名/姓名/邮箱；筛选条件与 URL 同步（浏览器前进/后退可回退） |
| 常用动作 | 重置密码 2 步（选模式 → 提交）；启停一键（Popconfirm 后果文案）；口令结果一键复制 |
| 状态反馈 | 开关**乐观更新 + 失败回滚**；行级 loading 防重复点击；成功/失败 message 定位到具体用户 |
| 批量 | 仅本页全选（禁止跨页全选）；上限 50；逐条结果面板 + 「重试失败项」 |
| 空态三分 | 无用户（CTA「建号」）/ 搜索无结果（清空筛选）/ 无权限（说明需要平台管理员角色） |
| 错误恢复 | 表单校验错误就地提示、不丢输入；409 护栏给出导引（如「请先指派新管理员」） |
| 可访问性 | Esc 关闭、焦点陷阱；危险按钮禁用态 + Tooltip 说明禁用原因 |

### 4.5 被操作用户体验（闭环 C1，v0.2 新增）

| 动作 | 用户侧预期 | 落地 |
|---|---|---|
| 停用 | 登录被拒且文案稳定（不泄露账号是否存在） | 复用 auth 登录错误口径；TUM-A8 断言 |
| 强制下线 / 会话吊销 | 下次请求 401 → 前端既有拦截器引导登录，提示「会话已失效，请重新登录」 | 全局 401 拦截（既有）+ 文案核对 |
| 重置·一次性 | 首登被要求改密后才能进入业务 | `must_change_password` + `RequireAuth`（既有链路，回归覆盖） |
| 增量便利（P1） | 列表「待改密」徽标；详情「最近登录」（审计派生、单用户查询，避免列表 N+1；口径见 §9 残余观察项） | TUM-4/TUM-5 |

---

## 5. 分期与工作流（`TUM-1`–`TUM-7`）

> 每个工作流 = 一个 PR（或一组小 PR）；**代码与文档同 PR**（修订即回填）。

| 编号 | 工作流 | 内容 | 验收（DoD） | 回滚 |
|---|---|---|---|---|
| TUM-1 | 后端读通道 | `GET /tenants/:id/users[/:uid]`；`TenantUserAdminService`（读）；平台面判定 + `(tenant_id,user_id)` 双键；分页/过滤 | 单测（service/handler/router）+ 越权反例（租户侧 403） | 摘路由 |
| TUM-2 | 后端账号治理 | 重置（两模式）/启停/强制下线；**access+refresh 双吊销（TUM-D6）**；最后管理员护栏；`default` 保护；幂等 | 单测（含护栏/幂等/策略失败/**refresh replay 拒绝**）+ 集成 | 关 `TENANT_USER_ADMIN_ENABLED` |
| TUM-3 | 审计与错误码 | 显式审计补齐 target 字段；`audit_logs.target_user_id` 迁移 + 索引（TUM-D6 行同批迁移批次登记）；错误码表落地；mask 复核（`newPassword`/`password`） | 审计断言用例（含掩码 + target_user_id） | 随 TUM-2 |
| TUM-4 | 前端抽屉与共享组件 | `TenantUsersDrawer` + 抽取 `UserStatusToggle`/`ResetPasswordModal` + API 客户端；**深链/批量重试/乐观回滚/空态三分/无障碍（§4.4）** | `tsc`/`eslint`/jest（抽屉+共享组件）；契约用例锁 URL/载荷 | 隐藏入口 |
| TUM-5 | 测试与场景 | 后端定向回归 + 前端 jest + api 通道 e2e（`TUM-A1`–`TUM-A8`）+ 浏览器场景（`scenarios/09-*`：S1–S3 闭环剧本） | 全绿并留证据（run-summary） | — |
| TUM-6 | 灰度与文档回填 | `TENANT_USER_ADMIN_ENABLED` 装配与按需开启；README/INDEX/canon 附录 C 登记；实施方案增补映射 | docs-gate 6/6 | 关开关 |
| TUM-7 | P2 评估（不做实现） | 删除用户 / 角色调整 / 导出 的产品与合规评估 | 评审纪要 | — |

**进度（2026-10-05）**

- ✅ **TUM-1 后端读通道**：`GET /api/v1/tenants/:id/users[/:userId]`；`TenantUserAdminService`；双层平台面判定（路由 `tenant:read` 粗筛 + 服务层 `super_admin` 硬校验，TUM-D1）；分页/关键字/状态/角色过滤；目标租户 RLS 重绑定 + `(tenant_id,user_id)` 双键。
- ✅ **TUM-2 后端账号治理**：重置（generated/specified，按目标租户密码策略）、启停（最后管理员 409 / `default` 二次确认 / 禁止自停用）、强制下线；**TUM-D6 会话双吊销**落地（access `minIssuedAt` + refresh 按用户最低签发时间、续签路径校验，`middleware/token_revocation.go`）。
- ✅ **审计**：`user.admin_password_reset` / `user.admin_status` / `user.admin_force_logout`；行归属 actor 家租户、`target_tenant_id`=目标租户、`source=platform_selected`；一次性口令不落审计（掩码沿用既有）。
- ✅ **TUM-3 审计与用户时间线**：三类治理动作显式审计补齐 `target_user_id`（中间件 `audit_target_user_id` + service 显式行）；迁移 `20261005_audit_target_user_id.sql`（列 + 索引 `(target_tenant_id, target_user_id, created_at)`）；审计查询新增 `targetUserId` 过滤；断言用例（掩码 + target_user_id）落地。
- ⏳ **TUM-4** 前端抽屉（下一批）；**TUM-5** 集成/e2e 剧本随前端批次补齐。

**验证命令（建议）**

```powershell
# 后端
cd itsm-backend; go test ./service ./handlers/user ./handlers/tenant ./router -run 'TenantUser|UserAdmin' -count=1
# 前端
cd itsm-frontend; npm run tsc; npm run lint; npx jest src/pages/\(main\)/admin/tenants --silent
# 文档门禁
bash scripts/docs-gate/check-multi-tenant-consistency.sh
```

---

## 6. 验收与反例（`TUM-A1`–`TUM-A6`）

### 6.1 正向验收

| # | 验收 | 证据 |
|---|---|---|
| TUM-A1 | 平台管理员可列出/检索任意租户用户；列表字段完整（含 mustChangePassword/角色/mspRole） | 接口用例 + 抽屉 jest + 浏览器场景 |
| TUM-A2 | 重置·一次性：响应含 16 位口令且仅一次；旧 token 立即 401；首登强制改密生效 | 集成用例（login 新旧口令对比） |
| TUM-A3 | 重置·指定：策略违规 400（透出规则）；成功后新口令可登录、旧口令 401 | 集成用例 |
| TUM-A4 | 停用：目标用户即时 401；启用后恢复；最后管理员停用 409；`default` 未带确认码 409 | 护栏用例 |
| TUM-A5 | 审计：三类动作均落审计且字段含 target_tenant_id/target_user_id；`newPassword` 已掩码、一次性口令不落审计 | 审计断言 |
| TUM-A6 | 回归：既有 `/users/*` 行为与权限不变；前端既有用例全绿；docs-gate 6/6 | 全量回归 |
| TUM-A7 | 便利性：深链刷新可复现筛选；核心动作 ≤3 步；口令一键复制；批量失败项可重试 | jest + 浏览器场景 |
| TUM-A8 | 闭环：S1–S3 剧本通过；停用/下线用户侧反馈符合 §4.5；列表 `total` 与配额 `users` 用量一致；P1 通知送达目标租户管理员 | 剧本 run-summary + 对账断言 |

### 6.2 反例（必须被拒绝/记录）

1. 租户管理员持 `tenant:write` 调用 `/tenants/:id/users/*` → 403 `PLATFORM_SCOPE_REQUIRED`；
2. 用 A 租户用 `uid` 拼 B 租户路径（IDOR）→ 404 `USER_NOT_IN_TENANT`；
3. 停用目标租户唯一管理员 → 409 `LAST_ADMIN_PROTECTED`；
4. 未开开关时调用写操作 → 404 `TENANT_USER_ADMIN_DISABLED`；
5. 指定模式弱口令（违反目标租户策略）→ 400；
6. 平台管理员停用自己（当其为目标用户时）→ 409/400；
7. 并发两次 `generated` 重置：后一次口令生效、前一次失效（响应不泄漏前值）。
8. 会话吊销后用旧 refresh 换新 access → 401（replay 被拒，TUM-D6）；
9. 批量含「最后一个管理员」：该项 409、其余成功；结果面板逐条展示且可忽略失败项重试其余（TUM-D7）。

---

## 7. 风险、依赖与回滚

| 风险 | 等级 | 缓解 |
|---|---|---|
| 平台面权限放大（最大风险） | 高 | TUM-D1 双因子判定（路由权限 + `super_admin`）；service 单一入口；越权反例纳入 CI |
| `tenantctx` 旁路滥用 | 高 | 旁路仅允许出现在 `TenantUserAdminService`；格式 `With*Bypass(actor, component, target)`；全量审计 reason |
| 明文口令暴露面 | 中 | 一次性、仅响应一次；审计/日志掩码；前端「仅显示一次+复制」交互；不在浏览器存储 |
| 最后管理员误停 → 租户锁死 | 中 | 硬护栏 + 前端禁用态；`default` 额外二次确认 |
| 性能（大租户列表） | 低 | `(tenant_id, active)` / `(tenant_id, username)` 检索路径；分页 ≤100；预留索引评估（TUM-1 DoD） |
| 与 `USER_PROVISIONING_CHANNELS_ENABLED` 灰度耦合 | 低 | 独立开关 TUM-D3 |

**回滚**：关闭 `TENANT_USER_ADMIN_ENABLED`（写操作即刻 404）→ 前端隐藏入口 → 需要时摘除路由。无数据迁移，回滚零风险。

**依赖**：TUM-1..3 无外部依赖；浏览器场景依赖联调环境（`saas_msp` + 建号开关，见 `scenarios/00`）。

---

## 8. 文档回填与治理

1. **本方案登记**：canon 附录 C.2 新增 `TUM-#` 行（局部编号，引用必须带前缀）；本目录 `README.md` 方案表与 `INDEX.md` §2 文档清单同步登记；
2. **实施后回填**：实施方案（新增批次映射，编号届时按 canon 附录 C 规则登记）、`07-known-gaps.md`（如产生新的运维缺口）、审计事件表、场景目录新增 `scenarios/09-tenant-user-management.md`（浏览器实操）；
3. **修订规则**：本方案引用其它文档编号必须带前缀（`IP-P0-5`、`07:G#`、`ADR-004:A#`、`WB-A#`、`LOGIN-*`、`UF-*`）；修订既有结论时必须回填被修订文档并留指针；
4. **门禁**：docs-gate C.6 头部四件套/编号注册表 + C.2 链接检查 + 本方"验收命令"全绿。

---

## 9. 决策确认记录（v0.2：原开放问题收口）

原开放问题 Q1–Q5 已按最佳实践全部定稿，**无 blocking 开放项**；后续需求变化时按 §8 修订规则更新本文与 canon 登记。

| 原问题 | 定稿 | 说明 |
|---|---|---|
| Q1 refresh token 联动吊销 | → TUM-D6 | access+refresh 双吊销；扩展按用户 refresh 最低签发时间校验（存量按 token 黑名单保留） |
| Q2 批量上限 / 部分失败 | → TUM-D7 | 上限 50、逐条结果、失败重试、仅本页全选 |
| Q3 删除用户 | → TUM-D8 | 不提供物理删除；停用为唯一移出手段；合规删除走离线评估 |
| Q4 审计只读角色 | → TUM-D9 | 本批 `super_admin` 统一；未来按词表流程引入 `tenant.user.read_platform` |
| Q5 MSP 面复用 | → TUM-D10 | 后端 service 复用；前端入口另案 |

**残余观察项（不阻塞实施，TUM-1/TUM-5 期间验证）**：

1. 「最近登录」数据口径：优先从 `audit_logs`（login 动作）派生、仅单用户查询；若数据不足，退回评估新增 `users.last_login_at` 列；
2. 多实例部署且 Redis 不可用时会退化为单实例内存吊销语义（既有实现属性，非本方案引入）——部署前提写入 `02-deployment-and-configuration.md` 对应批次。

---

## 修订记录

| 版本 | 日期 | 变更 |
|---|---|---|
| v0.1 | 2026-10-05 | 首版：现状基线（跨租户建号已有/账号治理缺失）、差距 TUM-G1–G4、需求 U1–U9、契约 TUM-D1–D5 + API/权限/审计/错误码、前端抽屉设计、分期 TUM-1–TUM-7、验收 TUM-A1–A6 与反例、风险回滚、治理登记 |
| v0.2 | 2026-10-05 | **决策冻结（按最佳实践）**：TUM-D1–TUM-D10 定稿并收口原 Q1–Q5（会话双吊销/批量语义/不删除/权限统一/MSP 另案）；新增 §2.3 业务逻辑闭环（场景 S1–S3 + 闭环点 C1–C6）与 §4.4/§4.5 操作便利性/被操作用户体验；审计新增 `target_user_id` 迁移计划；验收新增 TUM-A7/A8 与两类反例 |
| v0.3 | 2026-10-05 | **TUM-1/TUM-2 后端落地**：读通道（列表/详情，双层平台面判定）+ 账号治理（重置两模式/启停护栏/强制下线）+ TUM-D6 refresh 按用户双吊销（含续签路径校验）+ 路由与装配 + 定向测试；`target_user_id` 列（TUM-3）与前端抽屉（TUM-4）顺延下一批 |
