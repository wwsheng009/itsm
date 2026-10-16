# 个人访问令牌（Personal Access Token / PAT）方案设计

> **Status: draft** ｜ 日期：2026-10-16 ｜ 定位：**方案文档（设计输入，不证明已实现）**
> 关联文档：[实施文档](./personal-access-token-implementation-2026-10-16.md) ｜ [验收标准](./personal-access-token-acceptance-2026-10-16.md) ｜ [文档治理规范](../documentation-governance.md)
>
> 代码锚点行号取自 **2026-10-16 工作树**（未提交状态）；行号会漂移，定位时以「文件 + 符号名」为准，行号仅作辅证。

---

## 0. 摘要（TL;DR）

**做什么**：在「个人中心 → 安全设置」中增加 **个人访问令牌（PAT）** 管理能力：用户自助创建 / 设置有效期 / 轮换 / 吊销 / 查看使用历史；用 `Authorization: Bearer itsm_pat_xxx` 直接调用 REST API（如创建工单），无需每次走用户名密码登录。

**怎么接**：不新建认证链，只在**唯一认证收口点** `middleware.AuthMiddleware` 增加一个 PAT 分支，合成与 JWT **完全相同的 gin 上下文契约**（`user_id / username / role / tenant_id / token`）。因此 RBAC、租户隔离、审计、限流、CSRF 豁免全部沿用既有机制，权限语义与用户名登录**逐条等价**。

**关键设计取舍**（详见 §4）：

| 决策 | 结论 |
|---|---|
| 权限快照 | **不冻结**在令牌里；每请求实时解析角色/权限（角色变更、停用、权限回收立即生效） |
| 哈希算法 | `sha256(token)` hex 唯一索引（256-bit 随机值无需 bcrypt，且需 O(1) 查找） |
| 明文返回 | **仅创建/轮换响应返回一次**；之后只返回 `tokenPrefix`，落库只有哈希 |
| 吊销语义 | 单令牌吊销 / 吊销全部 / 安全事件（改密、停用、离职）批量吊销；**角色变更不吊销**（实时解析已覆盖） |
| 使用历史 | 复用 `audit_logs`：新增 `source='pat'`（免迁移）+ `token_id` 列；按 60s 窗口去重防写放大 |
| 管理面 | 走身份自省面 `/api/v1/auth/personal-access-tokens`（AuthMiddleware + 显式 CSRF + 显式审计 + 本人作用域），**不新增权限码** |
| **防自我繁殖** | **PAT 不可调用管理面写操作**（create/rotate/revoke/revoke-all 仅限浏览器会话，D-PAT-13）——泄漏令牌无法续发新令牌 |
| **平台角色禁令** | super_admin/sysadmin **禁止创建 PAT**（D-PAT-14）——365 天全权限旁路凭据不予发放 |

**范围**：后端（schema + 认证 + 领域切片 + 路由 + 审计）+ 前端（个人中心 + 审计页）+ 文档 + 238 演示环境端到端验收。
**不在范围**：scopes 细粒度令牌（P2）、管理员代管他人令牌（P2）、OAuth2 / mTLS / 服务账号（NG）、令牌配额计费（NG）。

---

## 1. 背景与目标

### 1.1 用户诉求拆解

| 原始诉求 | 拆解为 |
|---|---|
| 「用户在个人配置里启用或是生成访问 token」 | 个人中心自助创建 / 启停 / 吊销；创建时命名与设置有效期 |
| 「用户可以直接使用这个 token 来访问相关的 api 功能，比如创建工单」 | `Authorization: Bearer <PAT>` 走既有 Bearer 通道；**验收硬指标：用 PAT 成功创建工单** |
| 「不需要每次需要在前端使用用户名登录」 | 令牌独立于 15 分钟会话；适合 curl / CI / CLI / 脚本 |
| 「token 需要能设置时间限制或是轮换」 | 必填 `expiresAt`（默认 90 天，上限 365 天）+ 轮换（返回新明文，旧令牌按宽限期失效） |
| 「查看相关的使用历史，类似于用户操作历史，可以合并到用户操作/登录日志」 | 复用 `audit_logs`：`source='pat'` + `token_id`；个人中心「登录与令牌使用历史」；管理端审计页新增「来源」列与过滤 |
| 「token 访问同样需要跟用户名一样需要受到权限等安全约束」 | 权限、租户、状态、审计、限流与 JWT 完全同链路；**验收方式：等价性对照**（同一请求 JWT 与 PAT 结果一致） |

### 1.2 目标（可验收）

| 编号 | 目标 |
|---|---|
| G1 | 用户可在个人中心创建 PAT，明文只显示一次 |
| G2 | PAT 可通过 `Authorization: Bearer` 调用任意已授权 API（含 `POST /api/v1/tickets` 创建工单） |
| G3 | PAT 支持有效期（默认 90 天 / 上限 365 天）、轮换（含宽限期）、单令牌吊销、吊销全部 |
| G4 | PAT 的权限与租户约束与用户名登录**逐条等价**，且权限变更实时生效 |
| G5 | 令牌使用历史可查（`last_used_at` / 使用次数 / 按 token 归因的审计行），并可合并进「登录与操作日志」视图 |
| G6 | 明文不落库、不落日志、不进审计正文；前端与接口只回掩码/前缀 |

### 1.3 非目标（本批明确不做）

| 编号 | 不做 | 理由 |
|---|---|---|
| NG1 | OAuth2 授权码 / 客户端凭证、mTLS、服务账号 | 面向第三方系统集成，与「用户自助令牌」是不同产品面，避免范围膨胀 |
| NG2 | 令牌级 scopes（权限收窄白名单） | 用户诉求未要求；先以「实时用户权限」保证正确性，P2 再评估 |
| NG3 | 管理员代管他人令牌（列表 / 吊销） | 涉及管理员越权面与审计要求，P2 单独立项 |
| NG4 | 令牌过期提醒 / 邮件通知 | 依赖通知域改造，P2 |
| NG5 | `X-API-Key` 新认证头 | 复用既有 Bearer 通道可让 `itsm-cli` 零改动（`itsm-cli/src/lib/api-client.ts:45` 已在发 `Bearer`） |

---

## 2. 现状核查（代码证据）

> 核查方式：全仓静态检索 + 关键文件逐行阅读（3 个只读子代理并行 + 主线复核）。**结论：全仓不存在任何用户级 API Token / 个人访问令牌实现**（`个人访问令牌`、`PersonalAccessToken`、`PAT` 命中 0）。

### 2.1 认证链路：单一收口点

`middleware/auth.go:121-294` `AuthMiddleware(jwtSecret string)` 是唯一入口：

| 步骤 | 位置 | 行为 |
|---|---|---|
| 读取 `Authorization` | `auth.go:124` | 无头则回退 cookie `access_token`（`auth.go:136-144`） |
| Bearer 前缀校验 | `auth.go:158-167` | 大小写敏感 `Bearer ` |
| JWT 解析 | `auth.go:189-206` | keyfunc 拒绝非 HMAC（算法混淆防护）；**解析失败即 401** |
| 类型闸门 | `auth.go:219-230` | `tokenType != "access"` → 401 |
| 吊销检查 | `auth.go:232-246` | Redis/内存 store；查询失败 **fail-closed** |
| 最小签发时间 | `auth.go:248-267` | 早于 `MinIssuedAt` → 401「账号权限已变更，请重新登录」 |
| **身份写入上下文** | `auth.go:269-273` | `user_id / username / role / tenant_id / token` |

**上下文契约是下游全部机制的地基**：
- `middleware/rbac.go:957`（user_id）、`:990`（tenant_id）、`:1090`（role）、`:1047-1050`（实时 DB 查用户）、`:1078-1087`（停用即 403）、`:1119`（权限判定）、`:1142`（`user_entity`）；
- `middleware/tenant.go:45`（`c.GetInt("tenant_id")` 为**最强租户来源**）、`:224-231`；
- `handlers/common/handler.go:159`（登出读取 `c.GetString("token")`）。

> **设计含义**：PAT 只要合成同样的 5 个键，下游一切（含权限、租户、审计、限流）**零改动**即可等价工作。

### 2.2 路由三档与中间件顺序

| 路由组 | 创建位置 | 中间件链 |
|---|---|---|
| `public = r.Group("/api/v1")` | `router/router.go:314` | 无认证 |
| `auth = r.Group("/api/v1")` | `router/router.go:395-403` | `AuthMiddleware` → `RBACMiddleware` → 可选 `CSRFProtectionMiddleware` |
| `tenant = auth.Use(TenantMiddleware)` | `router/router.go:499-501` | 上述 + `TenantMiddleware` + `AuditMiddleware` |
| `authGrp = r.Group("/api/v1/auth")` | `router/common_system_routes.go:19-34` | **仅逐路由 `AuthMiddleware`**（无 RBAC、无 Tenant、无 Audit） |

业务域路由示例：`router/ticket_routes.go:20-21` → `GET/POST /api/v1/tickets`（`ticket:read` / `ticket:create`），挂在 `tenant` 组。

> **设计含义**：① PAT 认证必须发生在 `AuthMiddleware`（唯一能接受非 JWT 凭据处）；② PAT 调用业务 API 天然经过 `TenantMiddleware` + `AuditMiddleware`；③ 管理面若走 `authGrp` 则没有自动审计，**必须显式 `RecordAuthAudit`**。

### 2.3 权限与租户：实时 DB 解析 + fail-closed

- `RBACMiddleware`（`middleware/rbac.go:938-1146`）**每请求**按 `user_id` 从 DB 取用户（`:1047-1050`）、加载附加角色（`:1065-1075`）、校验 `Active`（`:1078-1087`）、做权限判定（`:1119`）。
- 权限三档语义：`PermissionConfig.Mode == DBOnly` 时 `SmartCheckPermission` 跳过硬编码表（`middleware/smart_permission.go:155-157`），以 DB 为权威（含空集即回收）。
- 响应侧权限单源：`middleware/membership_permission.go:166-260` `ResolvePermissions`（membership → role_permissions 并集；缺 membership 时默认 **fail-closed**）。
- 租户：`TenantMiddleware`（`middleware/tenant.go:32-235`）来源优先级 **JWT tenant > `X-Tenant-Code` > 子域 > 路径参数**；`X-Tenant-ID` 被**刻意忽略**（`:29-31`）；冲突 → 401 `TENANT_MISMATCH_REJECTED` + `tenant.probe_denied` 审计（`:101-117`、`:182-205`）；租户 `status != active` / 过期 → 403（`:208-217`）。

> **设计含义**：PAT 只要写入 `tenant_id`，租户隔离与越权探测防护**自动继承**；角色/权限变更**无需吊销令牌**即可实时生效。

### 2.4 令牌生命周期现状

| 机制 | 位置 | 说明 |
|---|---|---|
| access token | `middleware/auth.go:63-83` | HS256，**15 分钟**（`handlers/common/service.go:273`） |
| refresh token | `middleware/auth.go:90-113` | 7 天，带 `jti`，单次使用轮换 |
| 单令牌吊销 | `middleware/token_revocation.go:71-78` | Redis key = `sha256(token)`（`:156-159`，**不落原始值**） |
| 用户级批量失效 | `middleware/token_revocation.go:83-85` | `MinIssuedAt` 单调递增（Lua，`:195-204`），覆盖改密/停用/角色变更 |
| 另一套黑名单 | `service/token_blacklist_service.go:85` | **原始 token 进 Redis key**（历史不一致，PAT 不沿用） |
| 存储装配 | `internal/bootstrap/app.go:1264-1266` | `ConfigureAccessTokenRevocationRedis(redis)` 包级单例模式 |

> **设计含义**：PAT 的吊销采用「**表内状态 + 批量 UPDATE**」而非 `MinIssuedAt`，理由是 PAT 需要**可见的吊销记录**（列表里能看到"因安全事件吊销"），且不应把 JWT 的 15 分钟语义外溢到长期凭据（见 D-PAT-6）。

### 2.5 可复用先例

| 先例 | 位置 | 可复用点 |
|---|---|---|
| `Invitation` | `ent/schema/invitation.go:29-32`、`service/invitation_service.go:359-368`、`:779`、`:960-962` | **sha256(token) hex + MaxLen(64) + Unique + TTL + status + 可撤销**，且列表不回显哈希 —— PAT 直接照此建模 |
| `BootstrapToken` | `ent/schema/bootstrap_token.go:19-27` | bcrypt 一次性引导令牌（备选哈希方案） |
| `PasswordResetToken` | `ent/schema/password_reset_token.go:22-24` | **反例**：明文落库，禁止照抄 |
| 密钥掩码 | `mcp/admin/credential.go:203-210` `MaskSecret`；`middleware/mask.go:12-43`；`pkg/redact/redact.go:32` | 展示层掩码与日志脱敏 |
| 原始令牌生成 | `middleware/encryption.go:159-171` `GenerateSecureToken(n)` | `crypto/rand` + base64url，PAT 明文生成直接复用 |
| 密钥契约测试 | `handlers/ai/llm_provider_admin_test.go:261-270` | 「掩码≠明文 / 落库为密文 / 列表不泄漏」断言模板 |
| 契约守卫 | `router/route_permission_guard_test.go:27-50` | 自省面写路由的**带理由豁免**先例（logout / switch-tenant / change-password / me / me/preferences） |

### 2.6 审计与历史现状（决定「使用历史」怎么落地）

- **只有一张用户操作/登录审计表**：`audit_logs`（`ent/schema/auditlog.go:11-36`）。**不存在** `login_log` / `operation_log` / `user_activity`。
- 登录 = `audit_logs` 中 `resource='auth'`、`action='auth.login'`、`source='login'` 的行（写入点 `handlers/common/service.go:285-289`，失败分支 `:201-246`）。
- `source` 枚举（Go 侧）`login|switch|header|workbench|platform_selected|job|system`，列类型 `varchar(32)`（`auditlog.go:34`）→ **新增 `pat` 免迁移**。
- **`c.Set("audit_source", ...)` 覆盖钩子已存在但全仓无人使用**（读取点 `middleware/audit.go:339-356`）→ 这是 PAT 归因的天然挂点。
- 审计写入器：`AuditMiddleware`（`middleware/audit.go:191-336`，挂在 tenant 组）、`RecordAuthAudit`（`:57-91`）、`RecordTenantDeniedAudit`（`:99-145`）。
- 读取侧：`GET /api/v1/audit-logs`（`router/audit_routes.go:21`，`audit:read`），服务 `service/auditlog_service.go`，**已支持 `source` 过滤**（`:71-77`，`legacy` → NULL）。
- 缺口：`audit_logs` **无 `token_id` / `user_agent` / `auth_method` 列**；无 `(tenant_id, source)` 索引；无保留期/清理；无导出；前端审计页与 `src/lib/api/auditlog-api.ts` **未暴露 `source`**。
- 个人中心「登录历史」当前是 **disabled 占位**（`itsm-frontend/src/pages/(main)/profile/index.tsx:869-877`，文案「该功能即将推出」）；`lastLogin` 是死字段（无后端来源）。

> **设计含义**：诉求中的「合并到用户操作/登录日志」**不需要新建日志表**，只需：`source='pat'` + `token_id` + 自作用域读取端点 + 前端补列。

### 2.7 前端现状（**注意：不是 Next.js App Router**）

- 实际技术栈：**Vite + React 18 + React Router 7 + antd 6 + Zustand + React Query**；页面在 `itsm-frontend/src/pages/**`，路由表 `src/routes/index.tsx`（`CLAUDE.md` 中 Next.js 描述已过时）。
- 会话鉴权走 **HttpOnly Cookie**：`src/lib/auth/token-storage.ts:109-121` 的 `getAccessToken()` 恒返回 `null`；`src/lib/api/http-client.ts:211-218` 的 `Authorization` 分支在浏览器中不可达（仅 SSR/非浏览器用）。→ **PAT 是「外部调用方凭据」+「浏览器内仅做管理」**，两者不混用。
- 插入点：`src/pages/(main)/profile/index.tsx`（930 行单文件）的 `Tabs` → `security`「安全设置」Tab（`:787-881`），已有三行 `<Card size="small">`（修改密码 / 两步验证 / 登录历史）。
- 可抄模板：`src/pages/(main)/admin/users/components/InvitationManagementModal.tsx`（列表 + 状态标签 + 过期列 + `Popconfirm` 撤销 + 创建弹窗 + 一次性展示 + 复制 + `api` 依赖注入便于测试）；一次性明文范式 `src/components/user/ResetPasswordModal.tsx:147-148`（「仅显示一次」+「我已保存」闸门）。
- 复制工具：`src/lib/component-utils.ts:322-340` `useClipboard()`。
- 菜单由后端 `/api/v1/auth/menus` 驱动（`src/components/layout/sidebar/index.ts:1-7`），个人中心入口在头部下拉（`src/components/layout/header/UserMenuDropdown.tsx:49-55`）→ **放在 `/profile` 页内无需动菜单与路由**。

### 2.8 演示环境与远程服务器现状

| 项 | 值 | 来源 |
|---|---|---|
| 演示入口 | `http://172.18.3.238:8089`（独立栈 `itsm-demo`） | [demo-environment.md](../multi-tenant/demo-environment.md) |
| 数据库 | `itsm_demo`（复用宿主 `itsm-postgres-prod`） | 同上 |
| 演示账号 | 见 demo-environment §2（平台 `admin`、客户 `demo.mfg.*` 等；口令为公开演示口令，**仅演示环境**） | 同上 |
| SSH | `n9e@172.18.3.238`，凭据在 `.agents/skills/itsm-deploy/references/environments.md` | 部署技能 |
| 部署方式 | git bundle + `.dev/deploy/remote.py` + prod compose 构建 + `up -d` 受影响服务 | [SKILL.md](../../.agents/skills/itsm-deploy/SKILL.md) |
| 版本约定 | `1.8.0-dev.<short-sha>` | 同上 |
| **当前可达性（2026-10-16 实测）** | 本工作站 `Test-NetConnection 172.18.3.238:8089 / :22` 均**超时**；经 SOCKS5 `127.0.0.1:10808`（该代理在线）访问返回 **502** | 本次核查 |

> **设计含义**：验收文档必须写明「执行机需具备到 238 的网络通道」这一前置条件；本次仅完成方案设计，未执行部署与端到端验证（见 §5 R-5）。

### 2.9 门禁与守卫（实现时必须满足）

| 守卫 | 位置 | 约束 |
|---|---|---|
| 写路由必须挂权限或带理由豁免 | `router/route_permission_guard_test.go:77`（豁免表 `:27-50`） | 自省面新增 POST 需在 `writeRouteExemptions` 写理由 |
| 预检表新鲜度 | `middleware/precheck_freshness_test.go:17-33` | 改动 `RequirePermission` 后必须 `go run ./cmd/authz-gen` |
| 预检/声明一致 | `middleware/route_precheck_alignment_test.go:42-101` | `getPermissionFromPath` 结果须与声明一致 |
| 权限码必须存在 | `router/permission_code_catalog_guard_test.go:31-38` | 新增权限码需改 `internal/authz/catalog.go` + `roles.go` + codegen |
| 文档门禁 | `scripts/docs-gate/*.sh`（`make docs-gate`） | C.3 内部链接有效、C.1 无硬编码口令、C.6 仅约束 `docs/multi-tenant/**` |

### 2.10 能力缺口矩阵（现状 → 目标）

| 能力 | 现状 | 目标 |
|---|---|---|
| 非交互式 API 凭据 | ✗（仅 15 分钟 JWT / 7 天 refresh） | ✓ PAT |
| 令牌自助管理 UI | ✗ | ✓ 个人中心「安全设置」 |
| 有效期 / 轮换 | 仅会话令牌 | ✓ 必填有效期 + 轮换宽限期 |
| 使用历史 | ✗（无归因字段） | ✓ `source=pat` + `token_id` + 使用历史面板 |
| 明文一次性展示 | 有先例（重置口令） | ✓ 同范式 |
| 权限/租户等价约束 | —（PAT 不存在） | ✓ 与 JWT 逐条等价（等价性验收） |
| 外部工具可用性 | `itsm-cli` 登录后 15 分钟即失效 | ✓ 零改动接入 PAT |

---

## 3. 方案总览

### 3.1 设计原则

1. **复用优先**：认证收口点、RBAC、租户、审计、掩码、限流、CSRF 全部沿用；不新建平行机制。
2. **等价性**：PAT 请求与 JWT 请求在权限、租户、审计、错误码上**逐条等价**（可作为验收断言）。
3. **实时授权**：权限不落令牌快照，每请求从 DB 解析 → 回收权限立即生效。
4. **最小暴露**：明文只出现一次；库中只有哈希；日志/审计/响应只有前缀与掩码。
5. **fail-closed**：任何校验失败（状态查询失败、租户缺失、用户停用）一律拒绝，不回退放行。
6. **可审计**：创建/轮换/吊销与每次使用都可归因到具体令牌。
7. **可回退**：配置开关 `PAT_ENABLED` 一键关闭认证分支与管理面。

### 3.2 令牌格式、熵与哈希

```
明文格式：itsm_pat_<base64url(32 字节 crypto/rand)>
示例：   itsm_pat_5Yk8mQ2vR7pLxN1sT4uW6zA9bC0dE3fG7hJ2kM5nP8q=
长度：   9（前缀 itsm_pat_）+ 44（base64url 含 padding）≈ 53 字符
熵：     256 bit（不可枚举、不可爆破）
```

- 生成：`middleware.GenerateSecureToken(32)`（`middleware/encryption.go:159-171`）。
- 落库：`token_hash = hex(sha256(明文))`（64 字符，唯一索引）。
- 展示：`token_prefix` = 明文**前 17 字符**（`itsm_pat_` + 8 位，如 `itsm_pat_5Yk8mQ2v`），用于列表识别；**不可用于认证**。`maskedToken` = `token_prefix + "****"`（**不含任何真实后缀字符**，防止拼接还原）。
- 识别：认证分支以 `strings.HasPrefix(token, "itsm_pat_")` 判定；前缀同时便于密钥扫描工具（gitleaks/trivy）加规则。

**为何不用 bcrypt**：PAT 明文是 256-bit 随机值，不存在字典/暴力风险；bcrypt 每次校验需全表遍历（无法索引），会引入 O(n) 认证开销。**为何不用 AES 可解密存储**：令牌是「校验」而非「取回」语义，可解密存储会扩大泄漏面（与 `encrypted_api_key` 的机器凭据场景不同）。**为何不加 pepper**：高熵随机值下 pepper 无额外收益，且会引入密钥轮换复杂度。

### 3.3 生命周期与状态机

```
              create                 revoke / rotate(grace=0) / security_event
   (none) ───────────▶ active ──────────────────────────────▶ revoked
                         │                                        ▲
                         │ expires_at <= now                      │
                         └──────────────▶ 过期（派生态） ──────────┘
```

- **状态只存两态** `active | revoked`；「过期」由 `expires_at <= now` **派生**，避免定时任务写放大。
- 校验顺序（认证分支）：`token_hash 命中` → `revoked_at IS NULL` → `expires_at > now` → `user.active` → `tenant.active` → 写入上下文。
- 轮换：新建令牌（继承 `name` 与剩余有效期），旧令牌 `gracePeriodHours=0` 时立即 `revoked`；`=24` 时把旧令牌 `expires_at` 收紧为 `now+24h` 并记 `rotated_to_token_id`，实现**零中断轮换**。
- 上限：单用户 `active` 令牌数默认 **20**（超出返回 HTTP 422 / `code=4220`，提示先吊销）；**平台角色（super_admin/sysadmin）禁止创建**（D-PAT-14，403）。
- 竞态语义（接受并文档化）：MaxActive 为 check-then-insert（并发创建可短暂超限，下次创建被拒）；RevokeAll 与并发创建之间创建的令牌会存活；到期判定用应用服务器时钟（偏差可忽略）。

### 3.4 认证链路改造

```
AuthMiddleware
  ├─ 读 Authorization / cookie 回退（不变）
  ├─ Bearer 前缀校验（不变）
  ├─ 【新增】if strings.HasPrefix(token, "itsm_pat_") {
  │        claims, err := patAuthenticator.Resolve(ctx, token)   // 查表 + 校验
  │        失败 → 401（统一「token无效」，不区分不存在/已吊销，防枚举）
  │        成功 → c.Set("audit_source", "pat")
  │                 c.Set("auth_token_id", row.ID)
  │                 c.Set("auth_token_prefix", row.TokenPrefix)
  │                 c.Set("token_type", "pat")
  │                 → 跳到「写入身份上下文」（与 JWT 分支汇合）
  │     }
  ├─ JWT 解析 / 类型闸门 / 吊销 / MinIssuedAt（不变）
  └─ 写入身份上下文 user_id / username / role / tenant_id / token（共用）
```

- 装配方式：**包级单例 + 显式配置**，与 `ConfigureAccessTokenRevocationRedis` 同范式：
  ```go
  // middleware/pat_auth.go
  func ConfigurePersonalAccessTokens(client *ent.Client, enabled bool)
  ```
  在 `internal/bootstrap/app.go`（`ConfigureAccessTokenRevocationRedis` 邻近位置）调用。**`AuthMiddleware(jwtSecret)` 签名保持不变**，`router.go` 与既有测试零改动。
- 关键：PAT 分支必须在 `jwt.ParseWithClaims` **之前**（否则非 JWT 串会先被判 401，`auth.go:196-206`）。
- `username` 从 `users` 表实时取（审计 `actor_account` 需要），`role` 取 `middleware.EffectiveTokenRole(user.Role, user.MSPRole)`（MSP 端点等价性关键，`middleware/msp_rbac.go:35-43`）。

### 3.5 权限约束（与用户名登录等价）

| 约束 | 实现 | 效果 |
|---|---|---|
| 资源动作权限 | 由 `RBACMiddleware` 每请求判定（不变） | 无 `ticket:create` → 403「权限不足」 |
| 角色变更 | 不吊销令牌，实时解析 | 降级立即生效 |
| 账户停用 | 双重防线：停用即触发 R3 批量吊销（401）；未及吊销时认证期 `Active` 校验兜底 401 | 401「token无效」（与 JWT 停用即 401 等价；RBAC 层的 403 仅适用于极窄的请求中途竞态） |
| 账户删除 | 用户查询失败（`rbac.go:1051-1061`） | 401「用户不存在」 |
| 权限集解析 | `ResolvePermissions`（membership 并集，fail-closed） | 与登录会话同一单源 |
| MSP 角色 | `EffectiveTokenRole` | provider 用户 MSP 端点不被误拒 |

> 注：生产 `DBOnly` 模式下，用户若无有效 `user_tenant_memberships` 行，权限集为空 → 403。这是**既有语义**，PAT 保持一致（验收时用真实演示账号规避）。

### 3.6 租户约束

- 令牌**绑定创建时的作用域租户**（`tenant_id` = 创建请求的租户上下文）。
- 不支持用 PAT 切换租户（无 `X-Tenant-Code` 覆盖）：`TenantMiddleware` 会以 `tenant_id` 为最强来源，header 不一致 → **401 `TENANT_MISMATCH_REJECTED` + `tenant.probe_denied` 审计**（`middleware/tenant.go:101-117`、`:182-205`）。跨租户需求 = 在目标租户下再建一个令牌。
- 租户停用/过期 → 403（`tenant.go:208-217`），与 JWT 一致。
- RLS：新表启用行级安全（`tenant_id = get_current_tenant_id()`）；**认证期查询在租户上下文建立之前**，而 RLS enforce 驱动对无租户上下文语句 fail-closed（`database/rls/driver.go`「enforce mode requires tenant_id in context」，`tenantctx.IsSystemBypass` 是唯一豁免）→ 使用**带审计的** `tenantctx.SystemContext(ctx, "pat_auth:resolve", ...)`（每次调用留 zap 日志），并按 `token_hash` 唯一索引点查，旁路范围仅限这一条认证查询。同类先例：`bootstrap_tokens` 同样在租户上下文前校验一次性令牌（RLS 010 已对其启用策略）。管理面查询使用正常租户上下文 + `user_id` 过滤。

### 3.7 使用历史与审计

**归因字段**：`audit_logs` 新增 `token_id int NULL`（+ 索引 `(tenant_id, token_id, created_at)`），写入来自 `c.GetInt("auth_token_id")`。

**写入策略**（`PAT_USAGE_AUDIT_MODE`，默认 `dedupe`）：

| 模式 | 行为 |
|---|---|
| `dedupe`（默认） | PAT 请求一律产生审计行，但以 Redis `SETNX pat:audit:<tokenId>:<method>:<path>`（TTL 60s）去重，防止轮询型集成刷表 |
| `all` | 每请求一行（排障用） |
| `off` | 不写使用行（仅敏感操作由 `AuditMiddleware` 原有规则覆盖） |

**令牌自省计数**：`last_used_at` / `last_used_ip` / `use_count` 由同一 Redis 闸门节流（每令牌 60s 最多写一次），**更新失败不影响请求**。

**读取面**：

| 端点 | 作用域 | 用途 |
|---|---|---|
| `GET /api/v1/auth/personal-access-tokens/:id/usages` | 本人令牌 | 令牌详情「使用历史」（分页 + 汇总） |
| `GET /api/v1/auth/security-events` | 本人 | 个人中心「登录与令牌使用历史」（合并 `resource='auth'` 的登录/切换/令牌事件） |
| `GET /api/v1/audit-logs?source=pat&tokenId=` | `audit:read` | 管理端审计检索（新增 `tokenId` 过滤 + 来源列） |

**「合并进登录日志」的落地方式**：不新建表、不迁移历史数据；登录事件（`source=login`）与令牌事件（`source=pat`）同表，个人中心按 `user_id` 合并展示，管理端按 `source` 过滤区分。**注意既有语义**：今天所有普通 JWT 请求也会落到 `source='login'`（`middleware/audit.go:352-354`），历史数据不做追溯修正，文档中说明口径。

### 3.8 数据模型

**新表 `personal_access_tokens`**（ent schema：`ent/schema/personal_access_token.go`）

| 字段 | 类型 | 说明 |
|---|---|---|
| `id` | int PK | |
| `user_id` | int, Positive | 归属用户（自作用域过滤键） |
| `tenant_id` | int, Positive | 归属租户（RLS 键） |
| `name` | string, MaxLen 64, NotEmpty | 用户可读名称（如「CI 发布脚本」） |
| `description` | string, MaxLen 255, Optional | 备注 |
| `token_hash` | string, MaxLen 64, **Unique**, NotEmpty | `hex(sha256(明文))` |
| `token_prefix` | string, MaxLen 24, NotEmpty | 明文前 16 字符，仅用于展示 |
| `expires_at` | time, NotEmpty | 必填，默认 now+90d，上限 now+365d |
| `last_used_at` | time, Optional Nillable | 节流更新 |
| `last_used_ip` | string, MaxLen 64, Optional | |
| `use_count` | int, Default 0 | |
| `revoked_at` | time, Optional Nillable | 非空即失效 |
| `revoked_reason` | string, MaxLen 32, Optional | `self` / `rotated` / `revoke_all` / `security_event` |
| `rotated_to_token_id` | int, Optional Nillable | 轮换血缘 |
| `created_at` / `updated_at` | time | 常规 |

索引：`token_hash`（唯一）、`(user_id, revoked_at)`、`(tenant_id, revoked_at)`、`(expires_at)`（清理扫描）。

**既有表变更**

| 表 | 变更 | 迁移 |
|---|---|---|
| `audit_logs` | `+ token_id int NULL` | `migrations/20261016_personal_access_tokens.sql`（幂等 `DO $$` 块）+ 索引 `idx_audit_token (tenant_id, token_id, created_at)`、`idx_audit_source_created (tenant_id, source, created_at)` |
| `personal_access_tokens` | 建表 + RLS | ent `Schema.Create` 建表；RLS 走 `database/rls/migrations/014_personal_access_tokens_policies.sql`（+ `_rollback.sql`） |

### 3.9 API 契约（全部 camelCase，统一 `{code,message,data}` 信封）

**管理面（身份自省，`AuthMiddleware` + 浏览器会话走 CSRF）**，挂 `/api/v1/auth/personal-access-tokens`：

| # | 方法 | 路径 | 请求 | 响应 `data` |
|---|---|---|---|---|
| 1 | GET | `/api/v1/auth/personal-access-tokens` | — | `{ tokens: PatItem[] }` |
| 2 | POST | `/api/v1/auth/personal-access-tokens` | `{ name, description?, expiresInDays? , expiresAt? }` | `{ token: PatItem, plaintextToken }`（**仅此一次**） |
| 3 | POST | `/api/v1/auth/personal-access-tokens/:id/rotate` | `{ gracePeriodHours?: 0\|24 }` | `{ token: PatItem, plaintextToken }` |
| 4 | POST | `/api/v1/auth/personal-access-tokens/:id/revoke` | `{}` | `{ id, revokedAt, revokedReason }` |
| 5 | POST | `/api/v1/auth/personal-access-tokens/revoke-all` | `{}` | `{ revokedCount }` |
| 6 | GET | `/api/v1/auth/personal-access-tokens/:id/usages` | `?page&pageSize` | `{ items: UsageItem[], total, page, pageSize, summary }` |
| 7 | GET | `/api/v1/auth/security-events` | `?page&pageSize&kind=all\|login\|token` | `{ items: SecurityEvent[], total, page, pageSize }` |

```ts
interface PatItem {
  id: number;
  name: string;
  description?: string;
  tokenPrefix: string;       // itsm_pat_5Yk8mQ2v（明文前 17 字符）
  maskedToken: string;       // itsm_pat_5Yk8mQ2v****（前缀 + 固定掩码，无真实后缀）
  status: 'active' | 'revoked' | 'expired';
  expiresAt: string;
  lastUsedAt?: string;
  lastUsedIp?: string;
  useCount: number;
  revokedAt?: string;
  revokedReason?: string;
  createdAt: string;
}
```

**错误语义**（沿用既有码）：未认证 401 / `2001`；无权限 403 / `2003`；租户不匹配 401 + `reasonCode=TENANT_MISMATCH_REJECTED`；参数错误 400 / `1001+`；超出上限 HTTP 422 / `code=4220`（`common.UnprocessableEntityCode`）；平台角色创建 / PAT 调管理面写 → 403 / `2003`；他人令牌 id → **404**（防枚举）。**防枚举设计**：认证失败统一「token无效」，不区分不存在/已吊销/已过期（排障见 api-reference）。

**使用侧（外部调用方）**：

```bash
curl -sS -X POST 'http://172.18.3.238:8089/api/v1/tickets' \
  -H "Authorization: Bearer itsm_pat_xxx" \
  -H 'Content-Type: application/json' \
  -d '{"title":"PAT 验收工单","description":"由个人访问令牌创建","priority":"medium","type":"incident"}'
# → {"code":0,"message":"success","data":{"ticketNumber":"INC-...", ...}}
```
（无需 CSRF token：`middleware/csrf.go:154-159` 对 Bearer 请求直接放行。）

**PAT 可调用面矩阵**（等价性原则的边界，逐面明确）：

| 面 | PAT 可用性 | 说明 |
|---|---|---|
| 业务 API（tenant 组，如 `POST /tickets`） | ✅ 完整等价 | 核心场景 |
| 只读管理面（list / usages / security-events） | ✅ 允许 | 与 JWT 等价 |
| **PAT 管理面写操作**（create / rotate / revoke / revoke-all） | ❌ **403（D-PAT-13）** | 防自我繁殖；仅浏览器会话可写 |
| `POST /ws/ticket`（通知 WS 票据） | ✅ 允许（文档化） | 票据短 TTL（`DefaultWSTicketTTL`），暴露面可控；自动化接收通知属合理需求 |
| `POST /auth/logout` | ⚠️ 返回 401（无害） | `Logout` 校验 JWT 失败即拒（`handlers/common/handler.go:159-163`），不产生任何吊销副作用；PAT 无需登出，文档注明「吊销请走个人中心」 |
| `/auth/refresh`、`/auth/switch-tenant` | ❌ 天然失效 | 凭 refresh_token cookie，PAT 不参与（已核 `handlers/common/service.go:325-331`、`handlers/auth/handler.go:104`） |
| `GET /auth/me` | ✅ 允许 | 返回内容与 JWT 等价，无额外披露 |

### 3.10 前端设计

| 位置 | 变更 |
|---|---|
| `src/lib/api/personal-access-token-api.ts`（新增） | 7 个端点客户端；camelCase DTO；仿 `invitation-api.ts` |
| `src/pages/(main)/profile/components/AccessTokenPanel.tsx`（新增） | 列表（名称/前缀/状态/创建/最后使用/有效期/操作）+ 创建弹窗 + 一次性明文（「我已保存」闸门 + 复制）+ `Popconfirm` 吊销 + 轮换 + 「吊销全部」 |
| `src/pages/(main)/profile/components/CreateAccessTokenModal.tsx`（新增） | 名称 + 有效期（7/30/90/180/365 天）+ 说明「明文仅显示一次」 |
| `src/pages/(main)/profile/components/SecurityEventsPanel.tsx`（新增） | 「登录与令牌使用历史」：合并展示登录/切换/令牌使用事件，含来源、IP、时间、结果 |
| `src/pages/(main)/profile/index.tsx`（修改） | `security` Tab 内新增「访问令牌」卡片；把 disabled 的「登录历史」替换为可用的 `SecurityEventsPanel` |
| `src/pages/(main)/audit-logs/index.tsx` + `src/lib/api/auditlog-api.ts`（修改） | 新增「来源」列与 `source` / `tokenId` 过滤（后端 DTO 已返回 `source`，前端类型当前缺失） |
| `src/lib/i18n/translations.ts`（修改） | `profile.accessTokens.*` / `securityEvents.*`（zh-CN + en-US） |

**不做**：不新增路由、不改菜单（`/profile` 已在头部下拉可达）、不动 `http-client.ts` 的鉴权/CSRF/刷新链路、不动 `token-storage.ts` 键名。

### 3.11 CLI 与外部集成

- `itsm-cli/src/lib/api-client.ts:44-45` 已在发送 `Authorization: Bearer <cred.token>` → **零改动**即可使用 PAT（今日 15 分钟过期问题一并解决）。
- `docs/api-reference.md` 增补「个人访问令牌」章节（创建流程、请求示例、错误码、限流与配额）。
- `CHANGELOG.md` `[Unreleased]` 增补用户视角条目；`ROADMAP.md` 视迭代归属决定是否登记（见实施文档 §8）。

### 3.12 配置项与开关

| 配置 | 默认 | 说明 |
|---|---|---|
| `PAT_ENABLED` | `true`（代码默认） | 关闭后：认证分支直接拒绝 PAT 且管理面返回 404（一键回退）。**交付注意**：`.env.prod` / `.env.demo` 首轮显式置 `false`，验收通过后再开启（见实施文档 §5 灰度） |
| `PAT_DEFAULT_TTL_DAYS` | `90` | 创建默认有效期 |
| `PAT_MAX_TTL_DAYS` | `365` | 有效期上限（请求超过则 400） |
| `PAT_MAX_ACTIVE_PER_USER` | `20` | 单用户 active 令牌上限 |
| `PAT_USAGE_AUDIT_MODE` | `dedupe` | `dedupe` / `all` / `off` |
| `PAT_USAGE_AUDIT_WINDOW_SECONDS` | `60` | 使用行去重窗口（排障可调小） |
| `PAT_RATE_LIMIT_PER_MINUTE` | `120` | 单令牌每分钟请求上限（超限 429；复用使用审计的 Redis 闸门实现，无 Redis 时退化为内存计数） |

> 另需交付：① `.env.example` / `.env.demo.example` 增补上述键（运维可发现性）；② CI 密钥扫描规则 `itsm_pat_[A-Za-z0-9_=-]{43,}`（gitleaks/trivy 自定义规则，防令牌误入库）——列入实施文档 T11/T12。

---

## 4. 关键设计决策

| # | 决策 | 备选与理由 |
|---|---|---|
| **D-PAT-1** | PAT 认证**内嵌 `AuthMiddleware`**，共用身份上下文写入 | 备选：新建 `PATAuthMiddleware`。否决——会形成第二套上下文契约，后续任何新增上下文键都要改两处，且顺序错误将静默绕过 RBAC |
| **D-PAT-2** | 明文 `itsm_pat_` + 32B base64url | 备选：无前缀裸随机。否决——前缀让「识别/扫描/误用提示」成为可能，且不牺牲熵 |
| **D-PAT-3** | `sha256` hex 唯一索引 | 备选：bcrypt（O(n) 校验，不适用高熵值）、AES 可解密（扩大泄漏面）。见 §3.2 |
| **D-PAT-4** | 权限**不冻结**，每请求实时解析 | 备选：令牌内嵌 role/权限快照。否决——快照会漂移，形成长期越权通道；实时解析使「回收权限立即生效」 |
| **D-PAT-5** | 令牌绑定单租户，不支持 header 切换 | 备选：允许 `X-Tenant-Code` 切换。否决——与 `TenantMiddleware` 的 fail-closed 设计冲突，且跨租户应显式建多令牌（可审计） |
| **D-PAT-6** | 吊销五规则：R1 单令牌 / R2 吊销全部 / R3 安全事件批量 / R4 角色变更不吊销 / R5 到期即失效 | 备选：接入 `MinIssuedAt`。否决——会让 15 分钟会话语义外溢到长期凭据，且吊销不可见；改用表内 `revoked_at` + `revoked_reason`，可展示可审计。R3 覆盖改密/重置/停用/删除/离职 |
| **D-PAT-7** | 轮换支持宽限期（0 / 24h） | 备选：仅立即吊销。否决——CI/脚本无法原子替换，宽限期是「零中断轮换」的最低成本实现 |
| **D-PAT-8** | 有效期必填（默认 90 天，上限 365 天）；**v2 修订（2026-10-16，产品决策 Q2 翻转）：增加「永久不过期」选项**——实现为 9999-12-31 哨兵时间（不改表结构；派生状态/清理/轮换宽限天然兼容；DTO 输出 `neverExpires:true` + `expiresAt=nil`，哨兵值不外泄）；**安全事件批量吊销（改密/停用/删除/离职）对永久令牌仍全覆盖**，UI 附提示 | 初版否决永久令牌（合规优先）；用户决策要求长期自动化场景免轮换负担，安全边界由 R1–R3 承担 |
| **D-PAT-9** | 使用历史复用 `audit_logs` + `token_id` + 60s 去重 | 备选：新建 `token_usage_logs` 表。否决——诉求明确「合并到用户操作/登录日志」，同表才能一次查询合并展示；去重控制写放大 |
| **D-PAT-10** | 管理面挂 `authGrp` 自省面，显式 CSRF + 显式 `RecordAuthAudit` + `user_id` 自作用域，**不新增权限码** | 备选：挂 tenant 组 + 新权限码 `api_token:*`。否决——自省面若要求资源权限码，就必须把该码授予所有角色（含 end_user），比带理由豁免更糟；先例见 `route_permission_guard_test.go:44-49` |
| **D-PAT-11** | 新表启用 RLS；**认证期点查用带审计的 `tenantctx.SystemContext`**（RLS enforce 对无租户上下文语句 fail-closed，认证先于租户解析，唯一豁免通道即 system bypass），管理面用租户上下文 | 备选：新表不启用 RLS。否决——违反仓库多租户基线；旁路范围严格限定为「按 token_hash 唯一索引的一条点查」并留 zap 审计日志 |
| **D-PAT-12** | scopes 字段本批不落库 | 备选：预留空列。否决——死列即技术债；P2 需要时以 `scopes jsonb` + RBAC 交集实现（迁移路径见实施文档） |
| **D-PAT-13** | **管理面写操作拒绝 PAT**（create/rotate/revoke/revoke-all 仅限浏览器会话，读操作 list/usages/security-events 允许） | 背景：管理面挂 `authGrp` 且 Bearer 免 CSRF（`csrf.go:154-159`），若不限制，泄漏的 PAT 可自行创建新令牌实现**自我繁殖**，吊销单枚泄漏令牌无法止血。实现：写路由追加 `middleware.RejectPATAuth()`（检查 `token_type=pat` → 403），路由守卫测试 + 验收 AC-39 锁定 |
| **D-PAT-14** | **平台角色（super_admin/sysadmin）禁止创建 PAT**（v1） | 背景：`rbac.go:1270-1273` 对 super_admin 硬放行全部权限，365 天无 scope 的平台令牌等价于长期 RBAC 旁路凭据，风险量级远超 15 分钟 JWT。service.Create 校验 `IsPlatformRole(user.Role)` → 403；验收 AC-41 锁定。P2 再评估「短 TTL 平台令牌」（≤7 天） |

---

## 5. 风险与缓解

| # | 风险 | 影响 | 缓解 |
|---|---|---|---|
| R-1 | RLS enforce 模式下认证期查询被策略拦截，PAT 全部 401 | 高 | 认证期使用带审计的 `tenantctx.SystemContext` + 按 `token_hash` 点查（先例：`bootstrap_tokens`，RLS 010）；**必须有「RLS enforce + PAT 认证成功」的集成测试** |
| R-2 | 高频集成导致审计表写放大 | 中 | `dedupe` 默认 + Redis 闸门 + `last_used_at` 节流；预留 `off` 开关 |
| R-3 | 明文泄漏（日志/审计/前端/异常栈） | 高 | 三层防护：① 日志路径禁止打印令牌值（`AuthMiddleware` 新增分支不得复用会打印 token 的日志；**含 gin context 中的 `token` 键**——任何未来中间件不得打印该键）；② 审计只写 `token_id`/`token_prefix`；③ 响应只在创建/轮换返回一次。测试断言「落库无明文 / 列表无明文 / 日志无明文」。**另**：`gin.Logger()` 记录含 query string 的完整 URL——令牌误放入 query 的请求即使 401，明文也会进访问日志，API 文档需显著警示「仅接受 Authorization 头」，并加负向测试（query/body 传令牌一律无效） |
| R-3a | 泄漏令牌自我繁殖 | 高 | D-PAT-13：管理面写操作拒绝 `token_type=pat`；吊销后攻击者无法续发新令牌 |
| R-3b | 平台角色长期旁路凭据 | 高 | D-PAT-14：平台角色禁止创建 PAT（v1） |
| R-3c | 限流按 IP 而非凭据 | 中 | 全局限流键为 `ClientIP`（`router.go:274-306`）：轮换 IP 的泄漏令牌不受限、共享出口 IP 的合法 CI 会互撞 429。缓解：新增单令牌限流（`PAT_RATE_LIMIT_PER_MINUTE`，复用 Redis 闸门）+ 同一 tokenPrefix 高频 401 异常信号；文档明确「等价性为逐请求而非逐身份」 |
| R-4 | 长期令牌权限漂移 | 中 | 实时 RBAC + 安全事件批量吊销 + 有效期上限 |
| R-5 | **238 演示环境当前不可达**（本工作站 TCP 8089/22 超时，SOCKS 10808 → 502） | 中 | 验收执行需具备网络通道（在 238 侧或具备内网/代理的执行机运行）；本批仅交付设计文档，不声称已验证 |
| R-6 | ent codegen 资源门槛（峰值 ~4.1GB / ~4m40s） | 中 | 按 [ent 代码生成手册](../entc-codegen-runbook.md) 后台执行、输出重定向到文件、不加管道 |
| R-7 | `source` 语义历史污染（普通 JWT 请求也记为 `login`） | 低 | 文档说明口径，不追溯修正历史行；新数据语义准确 |
| R-8 | 前端审计页缺 `source`/`tokenId`，PAT 事件无法区分 | 低 | 本批一并补齐列与过滤 |
| R-9 | 守卫测试失败（写路由豁免/预检新鲜度/权限码目录） | 中 | 实施文档 §7 给出必跑命令与预期；豁免条目必须写理由 |
| R-10 | 令牌被误提交进仓库（脚本/CI 文件） | 中 | `itsm_pat_` 前缀便于加入密钥扫描规则；文档与 UI 明确「不要提交到代码库」 |

---

## 6. 开放问题（待产品/安全确认）

| # | 问题 | 建议默认 | 影响面 |
|---|---|---|---|
| Q1 | 密码重置/修改是否吊销全部 PAT？ | **是**（R3） | 若否，被盗令牌在改密后仍可用，安全预期不符 |
| Q2 | 是否允许永不过期令牌？ | ~~否~~ **已翻转（2026-10-16 产品决策）**：允许，见 D-PAT-8 v2 | 安全边界由吊销规则 R1–R3 承担；UI 附保管提示 |
| Q3 | 管理员是否可查看/吊销他人令牌？ | **本批不做**（P2） | 需新增管理权限码与越权审计 |
| Q4 | 单用户 active 令牌上限 20 是否合适？ | **20** | 影响自动化场景（多环境多脚本） |
| Q5 | 是否引入 scopes 收窄？ | **P2** | 影响数据模型（预留迁移路径） |
| Q6 | 使用审计默认 `dedupe` 窗口 60s 是否够细？ | **60s，且已可配**（`PAT_USAGE_AUDIT_WINDOW_SECONDS`） | 影响排障粒度与写放大 |
| Q7 | 令牌/审计数据的 PII 保留策略（`last_used_ip`、使用行含 IP/UA） | **本批登记为遗留项**：审计表现无保留期（§2.6 已核），PAT 会增加 PII 存量；P2 增加 `audit_logs` 保留任务或 `last_used_ip` 截断/掩码选项 | PIPL/GDPR 合规 |
| Q8 | 长期 revoked/过期令牌行是否需要清理任务？ | **建议 T13**：过期超 180 天的行由后台任务清理（`expires_at` 索引已预留）；审计行的血缘保留在 `audit_logs` | 表膨胀 |
| Q9 | 是否需要「临时停用（可恢复）」档位（区别于不可逆吊销）？ | **P2**：当前按「或」解读原诉求，create/revoke 已覆盖；如需暂停集成可先轮换 | 派生态 `disabled` + `enabled` 列 |

---

## 7. 与既有机制/文档的关系

| 既有资产 | 关系 |
|---|---|
| `middleware.AuthMiddleware` | **扩展**（新增分支），不替换 |
| `middleware.token_revocation.go` | **不修改**；PAT 用表内状态吊销，避免与 `MinIssuedAt` 语义纠缠 |
| `ent/schema/invitation.go` + `invitation_service.go` | **建模与生成范式来源**（sha256 + TTL + status） |
| `audit_logs` / `AuditMiddleware` / `RecordAuthAudit` | **扩展**（新增 `source='pat'`、`token_id`、使用行策略） |
| `itsm-cli` | **零改动受益** |
| [demo-environment.md](../multi-tenant/demo-environment.md)、[itsm-deploy SKILL](../../.agents/skills/itsm-deploy/SKILL.md) | 验收环境与部署通道 |
| [documentation-governance.md](../documentation-governance.md) | 本批文档同步义务（CHANGELOG / api-reference） |
| `docs/multi-tenant/**` | 不涉及；本方案不动多租户文档编号空间（C.6 不适用） |

---

## 附录 A：关键代码锚点索引

**认证 / 权限 / 租户**
- `itsm-backend/middleware/auth.go:14-24`（Claims）、`:27-42`、`:121-294`（AuthMiddleware）、`:269-273`（上下文契约）、`:232-267`（吊销 + MinIssuedAt）
- `itsm-backend/middleware/rbac.go:938-1146`（RBACMiddleware）、`:1047-1050`、`:1078-1087`、`:1119`、`:1142`
- `itsm-backend/middleware/membership_permission.go:166-260`（ResolvePermissions）
- `itsm-backend/middleware/msp_rbac.go:18-43`（MSP 角色映射 / EffectiveTokenRole）
- `itsm-backend/middleware/tenant.go:27-31`（来源优先级）、`:45-59`、`:101-117`、`:182-205`、`:208-231`
- `itsm-backend/middleware/csrf.go:154-159`（Bearer 免 CSRF）
- `itsm-backend/middleware/token_revocation.go:71-85`、`:156-159`
- `itsm-backend/middleware/encryption.go:159-171`（GenerateSecureToken）

**审计 / 历史**
- `itsm-backend/ent/schema/auditlog.go:11-36`
- `itsm-backend/middleware/audit.go:27-35`（source 枚举）、`:57-91`、`:99-145`、`:191-336`、`:339-356`（audit_source 覆盖钩子）
- `itsm-backend/service/auditlog_service.go`（列表 + `source` 过滤）
- `itsm-backend/router/audit_routes.go:21`、`itsm-backend/handlers/auditlog/handler.go:44`
- `itsm-backend/dto/auditlog_dto.go`（camelCase DTO）

**路由 / 装配**
- `itsm-backend/router/router.go:314`、`:395-403`、`:499-501`
- `itsm-backend/router/common_system_routes.go:19-34`（authGrp 自省面）、`:41-62`（users 组）
- `itsm-backend/router/ticket_routes.go:20-21`（`POST /api/v1/tickets`）
- `itsm-backend/internal/bootstrap/app.go:1250-1271`（服务/中间件装配点）
- `itsm-backend/config/config.go:20`、`:27`、`:341`、`:402`

**先例 / 门禁**
- `itsm-backend/ent/schema/invitation.go:29-32`、`itsm-backend/service/invitation_service.go:359-368`
- `itsm-backend/ent/schema/bootstrap_token.go:19-27`、`ent/schema/password_reset_token.go:22-24`（反例）
- `itsm-backend/mcp/admin/credential.go:203-210`、`middleware/mask.go:12-43`、`pkg/redact/redact.go:32`
- `itsm-backend/router/route_permission_guard_test.go:27-50`、`middleware/precheck_freshness_test.go:17-33`、`router/permission_code_catalog_guard_test.go:31-38`
- `itsm-backend/database/rls/migrations/013_ticket_type_distribution_policies.sql`（RLS 范式）
- `itsm-backend/migrations/20261014f_ticket_type_distribution.sql`（幂等迁移范式）

**前端**
- `itsm-frontend/src/pages/(main)/profile/index.tsx:537-882`（Tabs / security Tab）、`:869-877`（登录历史占位）
- `itsm-frontend/src/lib/api/http-client.ts:203-228`、`:279-298`、`:387-636`
- `itsm-frontend/src/lib/api/invitation-api.ts`、`src/pages/(main)/admin/users/components/InvitationManagementModal.tsx`
- `itsm-frontend/src/components/user/ResetPasswordModal.tsx:147-148`、`src/lib/component-utils.ts:322-340`
- `itsm-frontend/src/pages/(main)/audit-logs/index.tsx:53-59`、`:115-182`、`src/lib/api/auditlog-api.ts:4-34`
- `itsm-frontend/src/routes/index.tsx:377`（/profile 路由）、`src/lib/store/auth-store.ts:201-208`

---

## 附录 B：命名与术语

| 术语 | 取值 | 说明 |
|---|---|---|
| 产品名 | 个人访问令牌 / Personal Access Token（PAT） | UI 文案用中文「访问令牌」 |
| 明文前缀 | `itsm_pat_` | 认证判定与泄漏扫描 |
| Go 领域包 | `itsm-backend/handlers/pat/` | 包注释写明 PAT = Personal Access Token |
| ent 实体 / 表 | `PersonalAccessToken` / `personal_access_tokens` | |
| 审计 source | `pat` | 与 `login/switch/header/...` 并列 |
| 审计 action | `auth.token_created` / `auth.token_rotated` / `auth.token_revoked` / `auth.token_revoked_all` / `auth.token_used` | 自由字符串列，无需迁移 |
| 前端模块 | `personal-access-token-api.ts` / `AccessTokenPanel.tsx` | 避免与「会话 token / CSRF token」混淆 |

---

## 附录 C：方案审查记录

| 轮次 | 时间 | 方式 | 结论 |
|---|---|---|---|
| 1 | 2026-10-16 | 3 个只读子代理并行核查（认证链路 / 审计与日志 / 前端与设置面）+ 主线复核 | 见下「已吸收的纠正」 |
| 2 | 2026-10-16 | 独立子代理交叉审查（对照源码逐条验证本方案断言） | 6 项发现（1 BLOCKER / 2 MAJOR / 3 MINOR）已全部落实，见实施文档 §10 第 1–12 条 |
| 3 | 2026-10-16 | **完整性三维审查**：安全（12 项）/ 功能与用户操作走查（12 项）/ 前后端集成（6 项） | 30 项发现已全部落实；新增 D-PAT-13（防自我繁殖）、D-PAT-14（平台角色禁令）、T13（清理任务）、AC-39..44；全量修正记录见实施文档 §10 第 13–30 条 |

**已吸收的核查纠正**（避免后续按错误前提实施）：

1. **前端不是 Next.js App Router**，而是 Vite + React Router 7；页面在 `src/pages/**`。`CLAUDE.md` 相关章节已过时，实施文档以实际结构为准。
2. **会话令牌不进 JS**（HttpOnly Cookie，`getAccessToken()` 恒 null）→ PAT 定位为「外部调用方凭据」，浏览器内只做管理。
3. **`scopeTenantID` 不是 JWT claim**，租户作用域就是 `Claims.TenantID`；实施时不要去找一个不存在的字段。
4. **`c.Set("audit_source", ...)` 覆盖钩子存在但从未被使用**——PAT 是第一个使用者，顺序契约（必须在 `AuditMiddleware` 之前）必须写进实现与测试。
5. **`ResolvePermissions` 在缺少 membership 行时 fail-closed**（生产 DBOnly）→ 验收须用具备真实 membership 的演示账号，否则会误判为 PAT 缺陷。
6. **`ValidateRefreshToken` 缺 HMAC 算法校验**（`auth.go:45-60`）——既有缺陷，本方案不顺手改，避免混入无关变更（已在实施文档登记为观察项）。
7. **`audit_logs` 无 `(tenant_id, source)` 索引** → 本批一并补索引，否则「来源过滤」在大表上退化为扫描。
