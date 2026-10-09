# 08 · 服务商账号与客户授权操作手册（用户级）

> **状态**：当前（2026-10-08 联调环境实测通过）
> **更新日期**：2026-10-08
> **适用**：平台运维、服务商管理员/员工、客户管理员
> **前置阅读**：[04 服务商维度](./04-provider-dimension.md)、[05 使用指南](./05-usage-guide.md)、[scenarios/00 环境与账号](./scenarios/00-environment-and-accounts.md)

> **本文定位**：面向**界面操作**的逐步手册——从"平台建服务商员工账号"到"分配到客户"到"员工进入客户（深度切换）操作"到"解除分配"。每步给出：操作路径、填写内容、预期界面反馈、常见错误。权限口径（C1/C2 矩阵、membership）见 §9 附录与[实施方案 §3.0](./plan/msp-implementation-plan.md)。

## 1. 一页看懂（先读这个）

**核心模型**：账号（`users`）≠ 成员身份（`user_tenant_memberships`）。同一个服务商账号在服务商租户与客户租户里各有一条成员身份（角色不同），两条身份的权限**互相独立**。

```
① 平台建号（§3）                    ② 服务商分配（§4）                    ③ 员工进入客户（§5）
provider 租户内建员工账号      →    把员工分配到客户租户              →    员工登录 →「进入客户」深度切换
 · 必选 MSP 角色：                   · 分配角色 primary/backup/             · 客户侧菜单与权限（C2）
   provider_admin / provider_agent      specialist                         · 数据限定在该客户租户
 · 自动补 home membership（provider 域） · 自动生成客户 membership（source=allocation）
```

**管理动作只有两处**：① 建号时定岗位（MSP 角色）→ 决定服务商工作台侧能力；② 分配（员工→客户）→ 决定客户侧能力。**客户租户里不需要手工建服务商用户、也不需要手工配权限**（客户侧角色权限自动就位，客户管理员可再调整）。

**界面位置速查**：

| 要做的事 | 界面 | 谁有权限 |
|---|---|---|
| 建服务商员工账号 | `/admin/tenants` → 租户行「建号」 | 平台 `super_admin`（`tenant:write`） |
| 分配到客户 / 解除分配 | `/msp/management`（分配管理） | 服务商管理员（`msp_allocation:write`） |
| 员工进入客户（深度切换） | 顶栏「客户过滤器」→「进入客户」 | 被分配的服务商员工 |
| 客户侧查看服务方成员 / 调角色 | 客户租户 `/admin/users`、`/admin/roles` | 客户管理员 |

## 2. 联调环境与账号（测试专用）

- 前端：<http://localhost:3000>｜后端：<http://127.0.0.1:8090>
- 目标租户：`demo01`（服务商「测试」，type=`msp_provider`）、`demo_customer`（客户「测试-客户」，type=`msp_customer`）。

| 账号 | 口令 | 角色 / 作用域 | 用途 |
|---|---|---|---|
| `admin-mspacpt` | `Acpt@2026Admin2!` | `super_admin`（平台） | 平台建号（§3） |
| `admin-demo01` | `Passw0rd@1234` | provider 管理员（tenant 10「测试」，`msp_manager`） | 分配管理（§4/§7） |
| `prov.agent1`（2026-10-08 演练账号） | `Agent@2026Pw1!` | `provider_agent`（tenant 10） | 员工侧演练（§5）；当前无分配，回放前先按 §4 分配 |
| `admin_demo_customer` | `Passw0rd@1234` | 客户管理员（tenant 11「测试-客户」） | 客户侧查看（§6） |

> 口令为联调环境实测值（同 `.dev/connection-guide.md`）；生产环境一律走首管一次性口令与强制改密（见 [scenarios/00](./scenarios/00-environment-and-accounts.md)）。

## 3. 场景 A：创建服务商员工账号（平台建号）

**前置**：平台 `super_admin` 账号（接口要求 `tenant:write`）；建号通道 `USER_PROVISIONING_CHANNELS_ENABLED` **默认开启**（2026-10-08 起，无需设置；仅显式 `false/0/no/off` 时关闭，关闭后建号报"建号通道未开启（USER_PROVISIONING_CHANNELS_ENABLED）"）。

| # | 操作 | 预期反馈 |
|---|---|---|
| 1 | 用 `admin-mspacpt` 登录（`/login`） | 进入平台主页（侧栏含 `租户管理` 等平台菜单） |
| 2 | 打开 **租户管理** `/admin/tenants` | 租户列表；行内按钮：开通｜**建号**｜用户管理（满足条件时） |
| 3 | 找到服务商租户 **「测试（demo01）」**（type=`msp_provider`），点行内 **「建号」** | 弹出「平台建号」弹窗，并显示目标租户 |
| 4 | 填写：用户名 `prov.agent1`、姓名、邮箱、初始口令；**「MSP 角色」下拉选「服务商技术员（provider_agent）」** | 仅服务商类型租户会出现该下拉（普通租户没有） |
| 5 | 点 **「建号」** | 提示成功、弹窗关闭；该账号即可登录 |

**系统自动行为**（无需手工处理）：

- 账号写入 provider 租户，`users.msp_role=provider_agent`（或 `provider_admin`）；
- 自动补 **home membership**（provider 域，`source=home`）→ 员工可进入服务商工作台。弹窗注释原文："服务商租户员工必须带 MSP 角色，否则无法进入服务商工作台"；
- 岗位→角色映射：`provider_admin → msp_manager`、`provider_agent → msp_tech`（IP-P0-9 收敛）。

**常见错误**：

| 现象 | 原因 | 处理 |
|---|---|---|
| 弹窗没有「MSP 角色」下拉 | 目标租户不是 `msp_provider` | 确认租户类型后重试 |
| 新账号登录后看不到 MSP 菜单 | 建号时未带 MSP 角色 | 平台「用户管理」补 `msp_role`，或重建账号 |
| 建号报"该租户已存在同名用户" | 用户名在目标租户内重复 | 换用户名 |

## 4. 场景 B：把员工分配到客户（客户域授权）

**前置**：服务商管理员（本文用 `admin-demo01`；权限码 `msp_allocation:write`）。

| # | 操作 | 预期反馈 |
|---|---|---|
| 1 | 用 `admin-demo01` 登录（`/login`） | 进入服务商作用域；侧栏：服务台｜客户管理｜跨客户工作台｜**分配管理**｜审计看板 |
| 2 | 打开 **分配管理** `/msp/management` | 卡片「MSP 分配管理」；右上按钮：**为客户建号**、**新建分配**；表格列出本 provider 的分配（含「解除」列） |
| 3 | 点 **「新建分配」** | 弹窗「创建 MSP 分配」 |
| 4 | 选择：**MSP 员工**=`prov.agent1`；**客户租户**=`demo_customer - 测试-客户`；**分配角色**=`主支持 (Primary)`（可选 备份 Backup / 专家 Specialist） | 员工下拉只列本 provider 的员工；客户下拉只列本 provider 的客户 |
| 5 | 点 **「创建」** | 弹窗关闭；列表出现 `prov.agent1 → 测试-客户（primary）` 行 |

**系统自动行为**：

- 写入 `msp_allocations`（固化 provider 归属）；同时在**客户租户**自动生成 membership（`source=allocation`）：
  - `specialist → msp_specialist`；
  - `primary / backup → msp_tech`（最小权限口径；`primary→msp_manager` 的差异登记为 MSW-Q5 待决）；
- 该 membership 的权限内容 = **客户租户内该角色的 `role_permissions`**（C2 客户侧基线，见 §9）；
- 该分配同时是**深度切换的授权依据**（active allocation 才可切换；见 §5）。

**常见错误**：

| 现象 | 原因 | 处理 |
|---|---|---|
| 409 `MSP_ALLOCATION_EXISTS` | 同员工同客户已有有效分配（幂等语义） | 如需改角色：先解除再重建 |
| 员工下拉为空 | 本 provider 下没有带 MSP 角色的员工 | 先按 §3 建号 |
| 客户下拉缺少目标客户 | 目标客户不属于本 provider | 在平台租户管理确认 `mspProviderId` |

## 5. 场景 C：员工进入客户（深度切换）与验证

| # | 操作 | 预期反馈 |
|---|---|---|
| 1 | 用 `prov.agent1` 登录 | 服务商作用域；侧栏：服务台｜客户管理｜跨客户工作台｜分配管理｜审计看板（后两者可见；分配的新建/解除需 `msp_allocation:write`，属管理员） |
| 2 | 顶栏点 **「客户过滤器」** → 在 `测试-客户` 行点 **「进入客户」** → 确认框点 **「进 入」** | 跳到 `/dashboard`；顶栏出现 **「当前客户：测试-客户（深度操作中）」** + **「返回工作台」** |
| 3 | 观察左侧菜单 | **切换为客户侧菜单**：服务台｜服务请求｜工单统计｜工单管理｜我的请求｜知识库｜服务目录｜待我审批-目录｜CMDB｜配置项列表｜关系管理｜拓扑图 |
| 4 | 打开「工单管理」 | 列表为**该客户租户**的工单（数据限定在目标租户） |
| 5 | （可选）深度态下访问 `/api/v1/msp/workbench/summary` | **403**——跨客户工作台只在服务商作用域使用（canon §2.3 边界：工作台不切换） |
| 6 | 顶栏点 **「返回工作台」** | 回到服务商作用域；菜单恢复 MSP 组 |

**菜单对照（2026-10-08 实测）**：

| 作用域 | 侧栏菜单 |
|---|---|
| 服务商（`msp_manager`，如 admin-demo01） | 服务台｜客户管理｜跨客户工作台｜分配管理｜审计看板（5 项） |
| 客户（`msp_tech`，C2 基线） | 服务台｜服务请求｜工单统计｜工单管理｜我的请求｜知识库｜服务目录｜待我审批-目录｜CMDB｜配置项列表｜关系管理｜拓扑图（侧栏 12 项） |

**常见问题**：

| 现象 | 原因 | 处理 |
|---|---|---|
| 「进入客户」后菜单没有变化 | 客户租户内 membership/角色权限缺失或未按 C2 供给 | 对目标租户重放供给：`go run ./cmd/provision_tenant -tenant-id <客户租户ID>`；并确认分配存在 |
| 刷新后深度态丢失 | 旧版 `/auth/me` 归属租户契约缺陷（已修复） | 更新版本后 `/auth/me` 返回 `tenantId=目标租户、homeTenantId=归属租户` |
| 提示"仅 MSP 员工可访问" | 当前账号无 `msp_role`（非服务商员工） | 按 §3 建号 |

## 6. 场景 D（可选）：客户侧查看与调整

| # | 操作 | 预期反馈 |
|---|---|---|
| 1 | 用客户管理员（`admin_demo_customer`）登录 | 客户租户作用域 |
| 2 | `/admin/users` 用户列表 | **服务方成员**区域只读展示 provider 成员（`accountKind=provider, readOnly=true`，MSW-2 / Q2=B） |
| 3 | `/admin/roles` 角色管理 → 打开 `msp_tech` / `msp_specialist` 等角色 | 可编辑该角色权限（客户可收回/扩展），变更记审计 |

> 注：客户**不需要**也不应该手工"建服务商用户"；调整权限请在**角色**上做（不是逐账号）。

## 7. 场景 E：解除分配（撤销）

| # | 操作 | 预期反馈 |
|---|---|---|
| 1 | 服务商管理员 → `/msp/management` | 分配列表 |
| 2 | 目标行点 **「解除」** → 确认「确定要解除 … 对客户 … 的分配吗？」 | 提示"分配已解除"，列表移除该行 |
| 3 | 该员工再尝试进入该客户 | **权限空集（fail-closed）**：切换被拒绝（`TENANT_ACCESS_REVOKED` / no_grant）；工作台客户列表不再含该客户 |

> **实测（2026-10-08）**：解除后该员工 `POST /auth/switch-tenant` 返回 **403**、`GET /msp/customers` 为 **0 条**；客户侧 membership 行软删（`deassigned_at`/`deleted_at` 置位，`source=allocation`，立即生效）。

## 8. 审计与自查

- **审计看板** `/msp/audit`（服务商管理员）：跨租户审计聚合、越权尝试/冲突告警（`tenant.scope_denied`、`tenant.probe_denied`）；
- **关键审计动作**：`tenant.switch`（切换，含 basis）、`tenant.switch_denied`、`msp.allocation.assign`/解除、`membership.sync`、`user.admin_password_reset`；
- **接口抽查**（PowerShell，先登录持有 `$s` 会话）：

```powershell
$base = 'http://127.0.0.1:8090'
Invoke-RestMethod "$base/api/v1/auth/me" -WebSession $s                 # 看 tenantId / homeTenantId / permissions
(Invoke-RestMethod "$base/api/v1/auth/menus" -WebSession $s).data.main.path   # 目标租户菜单
(Invoke-RestMethod "$base/api/v1/msp/allocations" -WebSession $s).data        # 本 provider 分配
```

- **巡检工具**：`go run ./cmd/membership-drift-check`（membership 漂移）、`go run ./cmd/backfill_membership --dry-run`（回填预演）。

## 9. 附录：角色映射与权限矩阵（2026-10-08）

**角色映射**

| 环节 | 输入 | 落到角色 |
|---|---|---|
| provider 建号 | `msp_role=provider_admin` / `provider_agent` | provider 域 home membership：`msp_manager` / `msp_tech` |
| 分配到客户 | allocation `role=primary/backup/specialist` | 客户域 membership：`msp_tech`（primary/backup）/ `msp_specialist`（specialist） |

**两套权限矩阵（同名角色、不同租户、不同内容）**

| 角色 | provider 租户（C1，跨租户码） | 客户租户（C2，客户侧码） |
|---|---|---|
| `msp_viewer` | `msp:read`、`msp_customer:read`、`msp_ticket:read`、`msp_allocation:read`、`msp_report:read` + 任务基线 | `ticket:read`、`ticket:create` |
| `msp_tech` | viewer + `msp_ticket:write` | `ticket:read/create/update/write/assign`、`problem:write`、`change:write`、`service_request:write`、`knowledge:read`、`cmdb:read`、`service_catalog:read` + 任务基线 |
| `msp_specialist` | `msp_tech` + `msp_customer:write` | 同 `msp_tech` |
| `msp_manager` | `msp_tech` + `msp:write`、`msp_customer:write`、`msp_allocation:write`、`msp_report:write` | `msp_tech` + `user:write`、`report:read` |
| `msp_admin` | 同 `msp_manager`（语义等价全动作） | `msp_manager` + `cmdb:write`、`change:write` |

> 权威定义：`itsm-backend/internal/authz/roles.go`（`BuiltinRolePermissionCodes`＝C1；`MSPCustomerScopeRolePermissionCodes`＝C2）。客户租户 C2 由租户供给自动落库（`pkg/seeder/tenant_provisioner.go`，客户类型租户 C1→C2 对账），变更记录见项目 `CHANGELOG.md`（2026-10-08）。

## 10. FAQ（速查）

| 问题 | 答案 |
|---|---|
| 需要在两个租户里各配一次权限吗？ | 不需要。管理动作＝①provider 建号定岗位 ②分配到客户；客户侧权限自动就位（客户可再调角色） |
| 客户里要手工建服务商用户吗？ | 不要。分配即生成客户侧 membership；手工在客户租户建的是**客户账号**（走客户建号/邀请） |
| 同一账号为什么两处角色不同？ | 账号≠成员身份；两处是各自租户内的角色（provider: C1；客户: C2） |
| 如何限制某员工只能看某个客户？ | 分配（allocation）即限权；工作台与切换都按分配列表校验 |
| 收回权限怎么做？ | 解除分配（§7）；必要时客户侧收紧角色权限（§6） |
| 分配后客户侧角色是哪个？ | 按分配角色映射：primary/backup→`msp_tech`；specialist→`msp_specialist`（MSW-Q5：primary→msp_manager 待决） |

---

> **实测记录（2026-10-08，联调库）**：按本手册 §3→§7 全流程浏览器操作通过（新建 `prov.agent1` → 分配 `测试-客户` → 深度切换菜单 5→12 项、`tenantId=11/home=10` → 返回工作台 → 解除分配后切换 403、客户列表 0 条）；接口侧同口径验证见 §8。
