# 场景 09：平台侧租户用户管理（重置 / 启停 / 强制下线 / 审计）

> **状态**：2026-10-05 落地（TUM-1–TUM-5）｜**定位**：平台管理员在 `/admin/tenants` 对**任意租户**用户执行账号治理（S1–S3 闭环）的浏览器实操剧本
> **前置**：[00 环境与账号](./00-environment-and-accounts.md)；实例开启 `TENANT_USER_ADMIN_ENABLED=true`（**首管口令恢复默认可用，不受该开关约束**，见 §3.1）；平台 `super_admin` 账号（本地开发缺省 `admin`）
> **上位口径**：[增强方案 §4/§6](../plan/msp-tenant-user-management-enhancement-plan.md)（TUM-A1–A8 + 反例）｜**API 通道自动化**：[run-tenant-user-admin-acceptance.ps1](../../../scripts/msp/acceptance/run-tenant-user-admin-acceptance.ps1)
> **证据**：路由器级 API e2e（TUM-A1–A8 + 反例，全中间件链）已实跑全绿；Go 定向用例 9 条（含 S1–S3 闭环、C5 通知、审计掩码）+ 前端 jest（抽屉 5 / API 契约 9 / 守卫 4）全绿；**联调实例 run-summary 与浏览器逐步截图**为可选补强（脚本已就绪，见 §9）

---

## 1. 角色、入口与权限

| 项 | 内容 |
|---|---|
| 角色 | 平台管理员（`super_admin`，且持有 `tenant:write`） |
| 入口 | `/admin/tenants` → 目标租户行「**用户管理**」（`Users` 图标，与「建号」并列） |
| 权限 | 路由 `tenant:write` 粗筛 + 服务层 `super_admin` 硬校验（TUM-D1，fail-closed）；租户管理员调用一律 `403 PLATFORM_SCOPE_REQUIRED` |
| 深链 | `?tenantUsers=<tenantId>` 直接打开抽屉；筛选/分页同步 `tuKeyword/tuStatus/tuRole/tuPage/tuPageSize`（刷新/分享可复现，TUM-A7） |
| 灰度 | 写操作受 `TENANT_USER_ADMIN_ENABLED` 控制；关闭时 `403 TENANT_USER_ADMIN_DISABLED`（读通道不受限）；**例外：首管（`is_bootstrap_admin`）重置密码为兜底恢复通道，默认可用**（TUM-D3 修订，见 §3.1） |

> **安全边界**：抽屉只做 UX 收敛，真正的平台面判定在服务端；不要以「按钮可见」推断权限。

## 2. 抽屉界面速览

1. **头部**：租户名称 / 编码 / 类型标签 / 用户总数（`total` 与配额用量同源，见 §8）。
2. **工具条**：搜索（300ms 防抖，用户名/姓名/邮箱）、状态筛选、角色筛选、刷新。
3. **表格列**（身份优先）：用户（用户名固定左列 + 「首管」标记；姓名/邮箱次级行，超长省略 + Tooltip）、角色（主角色 + MSP 角色）、部门、状态（开关 + `待改密` 徽标）、创建时间（紧凑到分钟）、操作（重置密码 / 强制下线，固定右列）；宽屏无横向滚动，窄屏用户名/操作列固定可见、中间列横向滚动。
4. **批量条**（写权限）：本页勾选（上限 50）→ 批量启用/停用；失败项展示逐条原因并可「重试失败项」。
5. **空态三分**：该租户暂无用户（附「去建号」）／搜索无结果（附「清空筛选」）／403 平台面说明。
6. **无障碍**：Esc 关闭、焦点陷阱；禁用开关有 Tooltip 说明原因（如「该租户最后一个管理员不可停用」）。

## 3. S1 用户锁死排障（重置·一次性口令）

> 场景：员工忘记密码/账号被锁，平台侧发放一次性口令，员工首登强制改密后恢复。

| # | 操作（浏览器） | 预期界面反馈 |
|---|---|---|
| 1 | 打开 `/admin/tenants`，目标租户行点「用户管理」 | 抽屉打开，头部显示租户信息与总数 |
| 2 | 搜索框输入用户名 | 约 300ms 后列表收敛为匹配项；URL 出现 `tuKeyword=` |
| 3 | 目标用户行点「重置密码」 | Modal：默认「一次性口令（推荐）」，提示「16 位、仅显示一次、首登强制改密」 |
| 4 | 点「确认重置」 | 结果卡片展示口令（等宽字体）+「复制」按钮 +「我已保存口令」勾选框 |
| 5 | 点「复制」，勾选「我已保存」 | 提示「口令已复制」；未勾选时「关闭」置灰（防误关） |
| 6 | 关闭卡片，**线下安全渠道**转交口令 | 抽屉列表该用户「待改密」徽标出现 |
| 7 | 员工用一次性口令登录 | 进入 `/change-password`（`RequireAuth` 收敛），改密成功后回 `/dashboard` |
| 8 | 用旧口令登录 | 被拒（401）；旧 refresh 亦失效（TUM-D6 双吊销） |

**独立验证（API）**

```powershell
# 列表/详情（平台面）
curl.exe -H "Authorization: Bearer <platform-token>" "http://127.0.0.1:8090/api/v1/tenants/<tid>/users?keyword=<user>"
# 一次性重置（响应仅此一次回传 generatedPassword）
curl.exe -X POST -H "Authorization: Bearer <platform-token>" -H "Content-Type: application/json" `
  -d '{"mode":"generated"}' "http://127.0.0.1:8090/api/v1/tenants/<tid>/users/<uid>/reset-password"
```

**闭环判据（C1/C2/C3）**：旧口令/旧会话全失效；`must_change_password=true` 生效；审计含 `target_tenant_id`/`target_user_id`（§5）；口令转交责任链见方案 §4.5（线下/工单渠道，通知不含口令）。

## 3.1 首管口令恢复（默认可用，不受写通道开关约束）

> 场景：租户**唯一**管理员（bootstrap 首管 `admin-<tenantCode>`）口令丢失，且该租户内无第二个管理员可代为重置——
> 这是「防租户锁死」的平台兜底恢复通道（TUM-D3 修订），**不依赖 `TENANT_USER_ADMIN_ENABLED`**。

| # | 操作（浏览器） | 预期界面反馈 |
|---|---|---|
| 1 | `/admin/tenants` → 目标租户行「用户管理」→ 首管行（「首管」标记）点「重置密码」 | 开关未开启时：普通用户重置返回 `403 TENANT_USER_ADMIN_DISABLED`；**首管照常可用** |
| 2 | 选「一次性口令」确认 | 返回口令（仅显示一次）；`must_change_password=true`；该用户存量 access+refresh 双吊销 |
| 3 | 用一次性口令登录 | 进入 `/change-password` 强制改密后恢复访问 |

**服务器侧兜底（无控制台 / 无 `super_admin` 会话）**

```powershell
cd itsm-backend
go run ./cmd/reset_password -tenant-code B2CCUST -password '<NewStrongPass>'          # 默认目标 = 该租户首管
go run ./cmd/reset_password -tenant-code B2CCUST -username b2ccust_admin -password '<NewStrongPass>' -must-change=false
```

- 口令须通过**目标租户密码策略**；`-must-change` 默认 `true`；
- 会话双吊销为尽力而为：多实例部署需共享 Redis（CLI 会尝试连接 `REDIS_*` 配置；不可用时告警，旧会话可能存活至过期）；
- 审计：`user.admin_password_reset`（HTTP 窄通道在开关关闭时 `channel=bootstrap_recovery`；CLI 通道 `path=cmd:reset_password`、`source=system`）。

## 4. S2 员工离职（停用 + 租户管理员通知）

| # | 操作（浏览器） | 预期界面反馈 |
|---|---|---|
| 1 | 目标用户行切换「状态」开关 | Popconfirm：「确认停用该用户？该用户将立即掉线，且无法登录；存量会话全部吊销。」 |
| 2 | 确认停用 | 开关乐观置灰；成功提示「已停用，该用户已立即下线」；失败自动回滚并提示原因 |
| 3 | 员工侧任意请求/重新登录 | 401 → 前端拦截器引导登录，提示「会话已失效，请重新登录」（§4.5 稳定口径） |
| 4 | 目标租户管理员登录 → 通知中心 | 收到站内通知「用户已被平台停用」（类型 `user_admin_action`，**不含口令**；C5） |
| 5 | 需要恢复时再切换开关并确认 | 提示「已启用」；员工可重新登录 |

**护栏核对**

- 目标为租户**最后一个可用管理员** → 开关禁用（Tooltip），强行调用返回 `409 LAST_ADMIN_PROTECTED`；
- `default` 平台租户停用 → 弹出二次确认（需输入租户编码 `default`），否则 `409 TENANT_CONFIRM_REQUIRED`；
- 平台管理员**停用自己** → `409 SELF_OPERATION_FORBIDDEN`。

## 5. S3 安全事件（强制下线 + 重置 + 审计复核）

| # | 操作 | 预期 |
|---|---|---|
| 1 | 行内点「强制下线」 | 提示「已强制下线」（仅吊销会话，不改密码/状态） |
| 2 | 该用户旧 access 请求 | 401（`minIssuedAt` 判定）；旧 refresh replay → 401 |
| 3 | 按需执行「重置密码」（generated/specified） | 同 §3/§6；指定模式受**目标租户**密码策略约束 |
| 4 | 审计复核：`/admin/audit`（或 API）按目标租户 + 用户过滤 | 可见 `user.admin_password_reset` / `user.admin_status` / `user.admin_force_logout`，均带 `target_tenant_id`、`target_user_id` |

**审计查询（API）**

```powershell
curl.exe -H "Authorization: Bearer <platform-token>" `
  "http://127.0.0.1:8090/api/v1/audit-logs?targetTenantId=<tid>&targetUserId=<uid>&pageSize=100"
```

**掩码要求**：`mode=specified` 的 `newPassword` 在审计 `request_body` 中必须为掩码；一次性口令**不落审计**（Go 用例 `TestTenantUserAdmin_AuditRows` 断言）。

## 6. 指定模式与密码策略

1. 重置 Modal 切到「指定新密码」；
2. 输入弱口令（如 `short`）提交 → `400 PASSWORD_POLICY_VIOLATION`，错误文案来自目标租户策略；
3. 输入符合策略的强口令 → 成功（`mustChangePassword=false`），旧口令/旧会话立即失效。

## 7. 便利性核对（TUM-A7）

| 项 | 操作 | 预期 |
|---|---|---|
| 深链 | 复制含 `?tenantUsers=<id>&tuKeyword=...` 的 URL 新开页 | 抽屉自动打开并复现筛选（前进/后退可回退） |
| 核心动作 ≤3 步 | 搜索 → 重置 → 确认 | 完成 |
| 口令复制 | 结果卡片「复制」 | 剪贴板写入并提示 |
| 批量与重试 | 勾选 ≤50 → 批量停用（含 1 个最后管理员） | 其余成功、该项 409 原因可见；「重试失败项」可仅重跑失败项 |
| 乐观回滚 | 断网/服务端 409 时切换开关 | 开关自动回滚 + 错误提示（不产生脏状态） |

## 8. 对账（C4）

列表 `total` 与租户硬配额 `users` 用量应一致：

```powershell
curl.exe -H "Authorization: Bearer <platform-token>" "http://127.0.0.1:8090/api/v1/tenants/<tid>/users?page=1&pageSize=1"
curl.exe -H "Authorization: Bearer <platform-token>" "http://127.0.0.1:8090/api/v1/tenants/<tid>/usage"
```

两者口径同源（同一查询）；不一致时优先排查是否有绕过配额校验的写入通道。

## 9. 自动化与证据

```powershell
# API 通道验收（默认新建隔离租户，不触碰既有数据；退出码 0=无 FAIL）
pwsh scripts/msp/acceptance/run-tenant-user-admin-acceptance.ps1 -Base http://127.0.0.1:8090
# 复用既有租户（需提供租户管理员凭据以覆盖通知/越权用例）
pwsh scripts/msp/acceptance/run-tenant-user-admin-acceptance.ps1 -TenantId 8 -TenantAdminUser xxx -TenantAdminPass yyy
```

- 覆盖：`TUM-A1–A8` 正向 + 反例（403/404/409×3/400）；产物 `docs/multi-tenant/evidence/tenant-user-admin-acceptance/run-summary-<ts>.md`；
- 离线回归（无需实例）：
  - 后端**路由器级 API e2e**（全中间件链 Auth→RBAC→Tenant→handler→service；ent/sqlite 内存库）：`go test -p 1 ./router -run TestTUM -count=1` —— `router/tenant_user_admin_e2e_test.go` 覆盖 TUM-A1–A8 + 反例（403/404/409×3/400）与副作用（哈希/双吊销/审计掩码/通知/对账），已实跑全绿；
  - 后端服务用例 `go test -p 1 ./service -run TestTenantUserAdmin -count=1`（9 条，含 `ClosureS1S2S3`、`NotifiesTenantAdmins`）；
  - 前端 `npx jest TenantUsersDrawer tenant-user-admin-api guards-must-change-password --coverage=false`（18 条）；
- **环境说明**：本工作机未启动实例（Docker 守护进程不可用、PostgreSQL 未运行）；路由器级 e2e 已覆盖接口契约与副作用，**联调实例的 run-summary（真实登录/CSRF/多实例）与浏览器逐步截图**为可选补强，待联调环境执行。

## 10. 已知限制

1. **批量端点为 P1 未实现**：前端按本页选择逐条串行调用单条状态接口，结果逐条展示 + 失败重试；上限 50 为前端约束（后端批量契约见方案 §3.1，落地后替换）；
2. **通知为 best-effort**：发送失败仅告警不回滚治理动作（`TestTenantUserAdmin_NotifierFailureDoesNotBlock`）；收件人为目标租户**启用中的管理员**，排除被操作用户自身；
3. **导出（P2）未提供**：用户清单导出暂缺；
4. **列表「最近登录」**：待审计派生字段落地（方案 §9 残余观察项）。
