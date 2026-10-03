# MSP 多租户业务验收场景设计（本机实际业务测试）

> 状态：**v1.0（2026-10-03）**｜基准：仓库 HEAD `c8325dca`｜运行形态：**本机 saas_msp 实例 + 联调库真实数据**
> 定位：**从操作者出发**的端到端业务验收设计——平台管理员如何建租户、服务商如何分配/接单、客户与用户如何建单/回看、隔离反例与审计如何取证。
> 实现载体：`scripts/msp/acceptance/run-msp-business-acceptance.ps1`（单入口、幂等、产出 run-summary）。
> 关联：[三角色操作剧本](./msp-three-persona-operation-simulation.md)（现状剧本）｜[实施方案 §6 DoD](./msp-implementation-plan.md)｜[用户交互流程图](./msp-user-interaction-flows.md)（F-01…F-10）｜[跨客户工作台方案](./msp-cross-customer-workbench-and-filter-plan.md)（A11/A12）

---

## 1. 目标与非目标

**目标**：在本机对 `saas_msp` 实例做**真实 HTTP（真实 DB）业务闭环验收**，证据可复跑、可留档：

1. 平台管理员：创建租户 → 模板供给 → 首个管理员激活（强制改密）→ 治理读取（用量）；
2. 服务商：登录 → 建号（技术员）→ 分配客户 → 跨客户工作台接单（看/回/改状态/指派/批量）；
3. 客户与用户：管理员建号/邀请 → 用户首登 → 建单 → 回看结果；
4. 隔离反例：跨客户读取、客户访问 MSP 面、未分配技术员写操作、Header/JWT 冲突、越权建租户；
5. 审计：工作台动作、拒绝/冲突事件可查。

**非目标**：性能/容量；UI 像素级验收（前端 Playwright 见既有 `itsm-frontend/tests/e2e/`，本设计为 API 业务面，后续可平移为浏览器剧本）；生产/多云环境。

---

## 2. 环境与拓扑（本机）

### 2.1 启动（实例必须为 `saas_msp`）

```powershell
cd itsm-backend
$env:DEPLOYMENT_MODE='saas_msp'; $env:USER_PROVISIONING_CHANNELS_ENABLED='true'; $env:LOG_LEVEL='info'
Start-Process -FilePath .\main.exe -WorkingDirectory (Get-Location) -WindowStyle Hidden
```

> `USER_PROVISIONING_CHANNELS_ENABLED=true` 是建号三通道（platform/msp/tenant）的灰度开关；未开启时 S/P/C 建号场景将显式 SKIP 并给出原因。

### 2.2 拓扑与账号（联调库实测，2026-10-03）

| 角色 | 账号 | Home 租户 | 说明 |
|---|---|---|---|
| 平台管理员 | `admin` / `passw0rd` | default(1, internal) | 平台治理；非 MSP 员工（`msp/status.isAdmin=false`） |
| 服务商管理员 | `mspadmin` / `Msp@2026Staff!` | MSP001(3) | 已分配客户 A/B；建号/分配/工作台全动作 |
| 服务商技术员 | `mspagent` / `Msp@2026Staff!` | MSP001(3) | 现状无分配（可用于「未分配」反例；本脚本自建受控技术员） |
| 客户 A 管理员 | `custa_admin` / `Cust@2026User!` | MSPCUSTA(4) | 客户侧建号/邀请/建单 |
| 客户 B 用户 | `custb_user` / `Cust@2026User!` | MSPCUSTB(5) | 跨客户隔离反例的数据来源 |

客户租户归属：`MSPCUSTA/MSPCUSTB.parentTenantId = mspProviderId = MSP001(3)`。

### 2.3 运行时自定义实体（脚本幂等创建/复用）

| 实体 | 默认值 | 用途 |
|---|---|---|
| `acpt_agent` / `Acpt@2026Staff!` | 服务商技术员（`roleIds=[msp_tech]`, `mspRole=provider_agent`） | 「未分配 → 分配后接单」主线 |
| `acpt_user` / `Acpt@2026User!` | 客户 A 用户（`role=end_user`） | 客户用户建单/回看主线 |
| 邀请对象 | `acpt-invite@example.com` | 邀请 → 落地 → 接受 流程（可选） |
| 平台新租户 | `MSPACPT`（msp_customer，parent=MSP001） | 平台建租户 → 供给 → 首管理员（可选，`-SkipTenantLifecycle` 关闭） |

---

## 3. 场景目录（操作者视角）

> 判据中的 `≠2xx` 表示允许的业务性失败码（如 404 不泄露）；所有断言以**响应码 + reasonCode/错误码 + 关键字段**为准。SKIP 必须携带原因。

### G0 环境与拓扑（E）

| ID | 操作者 | 步骤 | 判据 |
|---|---|---|---|
| E1 | — | `GET /api/v1/health` | 200 |
| E2 | admin | `GET /api/v1/msp/status` | 200；`deploymentMode=saas_msp`、`mspRoutesEnabled=true` |
| E3 | admin | `GET /api/v1/tenants` | 含 MSP001(provider) / MSPCUSTA / MSPCUSTB；客户 `parentTenantId=mspProviderId=MSP001.id` |
| E4 | 四角色 | 登录 mspadmin/mspagent/custa_admin/custb_user | `code=0`；`user.tenantId` 与拓扑一致 |

### G1 平台治理闭环（S；`-SkipTenantLifecycle` 可关）

| ID | 操作者 | 步骤 | 判据 |
|---|---|---|---|
| S1 | admin | `POST /api/v1/tenants`（`type=msp_customer`，绑定 provider） | 2xx 且落库可查；已存在则按 code 复用（幂等） |
| S2 | admin（部署通道） | `go run ./cmd/provision_tenant -tenant-code MSPACPT -create-admin -admin-username … -admin-password …` | exit 0；模板 + 首管理员幂等 |
| S3 | 新租户管理员 | 登录（`mustChangePassword=true`）→ `POST /auth/change-password` → 重登录 → `GET /auth/me` | 改密成功；`tenantId=新租户`；`permissions` 非空 |
| S4 | admin | `GET /api/v1/tenants/:id/usage` | 200；含 `limits/used` |

### G2 服务商准备（P1–P3）

| ID | 操作者 | 步骤 | 判据 |
|---|---|---|---|
| P1 | mspadmin | 登录 → `GET /msp/context` | 200；`isMsp=true` |
| P2 | mspadmin | `POST /users`（provider 租户内建 `acpt_agent`，绑定 `msp_tech`） | 2xx；重复执行 → `USERNAME_EXISTS` 视为复用 |
| P3 | mspadmin | 确保 `acpt_agent → MSPCUSTA`（存在性以 **`/msp/customers`** 该员工可见客户为准；`/msp/allocations` 只返回调用者自身记录） | 客户A 已可见；客户B 必须保持**未分配**（供 I3/I6 反例） |

### G3 客户与用户（C）

| ID | 操作者 | 步骤 | 判据 |
|---|---|---|---|
| C1 | custa_admin | 登录 | `code=0`；`tenantId=4` |
| C2 | custa_admin | `POST /users` 建 `acpt_user`（`role=end_user`） | 2xx；重复 → `USERNAME_EXISTS` 复用 |
| C2b | custa_admin → 匿名 | `POST /users/invitations`（`roleId=end_user`）→ `GET /auth/invitations/:token` → `POST .../accept` | 邀请创建 2xx（`inviteUrl` 形如 `…/invite/<token>`，token 在**路径段**）；落地页 200；接受后新用户可登录（用户名=邮箱前缀） |
| C3 | acpt_user | 登录（如需强制改密则先改密）→ `POST /api/v1/tickets`（`type=incident`） | 2xx；响应 `id`/`ticketNumber`；`tenantId=4` |
| C4 | acpt_user | `GET /api/v1/tickets/:id` → `GET /api/v1/tickets` | 详情 200 且 `status=open`；列表含该单 |

### G4 服务商接单（P4–P9；mspadmin 主操，acpt_agent 分配后复查）

| ID | 操作者 | 步骤 | 判据 |
|---|---|---|---|
| P4 | mspadmin | `GET /msp/workbench/tickets?customerTenantIds=4` | 200；含 C3 工单；`allowedActions` 非空 |
| P5 | mspadmin | `POST /msp/tickets/:id/reply` | 200；客户侧评论可见（C5 复核） |
| P6 | mspadmin | `POST /msp/tickets/:id/status`（`open→in_progress`） | 200 |
| P7 | mspadmin | `POST /msp/tickets/:id/assign` | 200（指派给当前技术员） |
| P8 | mspadmin | `POST /msp/workbench/batch`（reply ×1 成功；101 条 → 拒绝） | 成功批：`succeeded=1`；超限批：4xx 且不产生半执行 |
| P9 | mspadmin | `POST /msp/tickets/:id/status`（`in_progress→pending`） | 200；`resolved` 属受保护终局，必须走 `ResolveTicket`（见 C6） |
| P10 | acpt_agent | 分配后 `GET /msp/workbench/tickets?customerTenantIds=4` | 200；可见该单（分配使能） |

### G5 客户回看（C5）

| ID | 操作者 | 步骤 | 判据 |
|---|---|---|---|
| C5 | acpt_user | `GET /tickets/:id` → `GET /tickets/:id/comments` | `status=pending`；评论含 P5 回复内容 |
| C6 | custa_admin | `POST /tickets/:id/resolve`（提交解决方案）→ acpt_user 复查 | resolve 2xx；`status=resolved`（服务商处理→客户确认解决，闭环完成） |

### G6 隔离反例（I；必须全部被拒）

| ID | 操作者 | 步骤 | 判据 |
|---|---|---|---|
| I1 | custb_user 建/取 B 租户工单，acpt_user 访问 | 跨客户读取 | `≠2xx`（404/403），不泄露标题 |
| I2 | custa_admin | `GET /msp/customers` | 403 |
| I3 | acpt_agent（对**未分配客户B**） | `POST /msp/tickets/:B_id/reply`（写路径） | 403 + `MSP_ALLOCATION_REQUIRED` |
| I4 | acpt_user | `GET /tickets` 携带 `X-Tenant-Code: MSPCUSTB` | 401 + `TENANT_MISMATCH_REJECTED`（`07:G9`） |
| I5 | acpt_user | `POST /api/v1/tenants` | 403（平台权限不落客户身份） |
| I6 | acpt_agent（对**未分配客户B**） | `GET /msp/customers/:B_id/tickets`（路径通道） | 403 + `MSP_ALLOCATION_REQUIRED` |

### G7 审计（A）

| ID | 操作者 | 步骤 | 判据 |
|---|---|---|---|
| A1 | admin | `GET /api/v1/audit-logs?targetTenantId=4` | 含 P5/P6/P7 对应 `workbench.action`（≥3 条） |
| A2 | mspadmin | `GET /msp/audit/summary` | 200；含拒绝/冲突统计结构 |
| A3 | admin | I4 后审计 | 存在 `probe_denied`（目标 tenant=4）记录 |

---

## 4. 验收判据与通过标准

- **整体通过**：`FAIL = 0`；SKIP 必须逐条给出原因（前置缺失/开关未开），且不影响主线闭环（S/P/C/I/A 主线中 S、C2b、P10 允许有理由 SKIP）。
- **失败即证据**：任何 FAIL 记录 `场景 ID + 请求 + 实际码/字段 + 期望`，脚本以非零 exit code 结束。
- **反例必须拒绝**：G6 任一项出现 2xx 即为 FAIL（安全回归）。

## 5. 证据与产物

| 产物 | 位置 | 内容 |
|---|---|---|
| run-summary | `docs/multi-tenant/evidence/msp-business-acceptance/run-summary-<ts>.md` | 每场景 PASS/FAIL/SKIP、耗时、请求摘要、关键响应字段、失败明细 |
| 控制台输出 | 同源 | 逐行 `[PASS]/[FAIL]/[SKIP] <ID> <标题> — <证据>` |
| 原始响应（可选） | 同上目录 `raw-<ts>.json`（`-KeepRaw`） | 每个请求 method/path/status/body 截断 |

## 6. 脚本使用

```powershell
# 默认：复用联调库拓扑，跑 主线 + 反例 + 审计 + 邀请 + 平台建租户（S 组）
pwsh scripts/msp/acceptance/run-msp-business-acceptance.ps1

# 跳过平台建租户（快速复跑）：仅业务闭环
pwsh scripts/msp/acceptance/run-msp-business-acceptance.ps1 -SkipTenantLifecycle

# 跳过邀请流
pwsh scripts/msp/acceptance/run-msp-business-acceptance.ps1 -SkipInvitation

# 覆盖环境（账号/URL/新租户 code）
pwsh scripts/msp/acceptance/run-msp-business-acceptance.ps1 -Base http://127.0.0.1:8090 -MspAdminPass '***'
```

**幂等与数据影响**：脚本只做「创建缺省、复用已有」；每次运行会新增 1 张客户工单（标题带时间戳）与审计记录；`-SkipTenantLifecycle` 时不触达 CLI 供给。审计目标准备数据（客户B工单）同理由 custb_user 复用或新建。

## 7. 场景 ↔ 契约 ↔ DoD 对照

| 场景 | 契约/接口 | DoD/用例 |
|---|---|---|
| S1–S4 | `POST /tenants`、`cmd/provision_tenant`、`/auth/change-password`、`/tenants/:id/usage` | IP-P1-5（`07:G2`）、IP-P2-6 |
| P2–P3 | `POST /users`、`POST /msp/allocations` | IP-P0-5（三通道）、IP-P0-2 |
| C2/C2b | `POST /users/invitations`、`/auth/invitations/:token(/accept)` | IP-P1-4（邀请→首登） |
| C3–C5 / P4–P9 | `/tickets`、`/msp/workbench/*`、`/msp/tickets/:id/*` | IP-P0-7、IP-P1-6、A11/A12 |
| I1–I6 | 四通道 403、Header 冲突 401 | IP-P0-2/6、IP-P1-8、`07:G9` |
| A1–A3 | `/audit-logs`、`/msp/audit/summary` | IP-P0-10、IP-P1-8 |

## 8. 变更记录

| 版本 | 日期 | 说明 |
|---|---|---|
| v1.0 | 2026-10-03 | 首版：G0–G7 场景目录、判据、脚本入口与证据约定（本机 saas_msp 实测基线） |
| v1.1 | 2026-10-03 | 首轮实测校准：P3 分配真值改用 `/msp/customers`；I3/I6 反例改用「未分配客户B」；P9 改为状态机合法终局 `pending` + 新增 C6 客户确认解决；C2b 明确 token 在 `/invite/<token>` 路径段 |

## 9. 首轮实测发现（2026-10-03，本机 saas_msp）

| # | 发现 | 处置 |
|---|---|---|
| D-1 | **工单号碰撞后事务内重试导致 500**：`tickets.ticket_number` 唯一键冲突（序列落后于 max，如 `TKT-202610-000004` 重复）后，仓库层在**同一已中止事务**内重试语句 → `25P02 current transaction is aborted`，建单失败（业务验收 C3 实测捕获）。 | **已修复**：`repository/ticket` 新增 `ErrTicketNumberCollision` 哨兵并停止事务内重试；`service.CreateTicket` 以**新事务**重试 ≤3 次。回归用例 `TestRepository_CreateWithTx_NumberCollisionReturnsSentinel`。 |
| D-2 | 重复分配（同一员工同一客户已有有效记录）时 `POST /msp/allocations` 返回 **500 `操作失败`**，非 409/业务错误码，调用方无法稳定判定。 | **登记遗留**（建议：映射 409 + 稳定 `reasonCode`，需登记 canon 错误码表后实施）；当前脚本以 `/msp/customers` 为真值、并把 500 重复文案按幂等复用处理。 |
| D-3 | 契约差异：`/msp/allocations` 仅返回**调用者自身**分配（不可用于核对他人）；邀请链接 token 在路径段（`/invite/<token>`）。 | **脚本已适配**；设计文档已明确口径。 |
| D-4 | 邀请语义：对**已有账号**的邮箱再次发起邀请，`accept` 返回 409 `INVITATION_EMAIL_EXISTS`（需管理员绑定后重发，不支持自助注册）。 | **脚本改用每轮唯一邀请邮箱**（`acpt-invite-<ts>@example.com`），完整走通「邀请→落地→接受→首登」；语义本身符合设计（防账号劫持）。 |
