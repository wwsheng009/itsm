# MSP 多租户实施方案（P0 → P1 → P2）

> 状态：**Draft v0.2（待评审）**｜日期：2026-09-29｜基准：仓库 HEAD `4fcacf68`
> 上位：[ADR-004](../../architecture/adr-004-multi-customer-tenant-model-selection.md)（选型）｜[canon](./msp-concept-model-and-architecture-canon.md)（概念/边界/迁移路线，附录 C 注册表）
> 设计输入：[目标架构](./msp-target-architecture.md)｜[工作台方案](./msp-cross-customer-workbench-and-filter-plan.md)｜[登录与切换细化](./msp-login-and-switching-refinement-plan.md)｜[前端分析](./msp-frontend-pages-and-permissions-analysis.md)｜[主方案](./msp-user-lifecycle-and-tenant-switching-plan.md)｜[集成分析](./msp-integration-with-rbac-org-workflow-analysis.md)｜[一致性审计](./msp-docs-consistency-audit.md)
> 定位：把上述目标态落成**可执行、可验收、可回滚**的工程步骤；本文不新增概念。工作流编号为本文局部编号 `IP-P{0|1|2}-#`（已登记 canon 附录 C）。
> 现状基线文档：[01](../01-architecture.md)–[07](../07-known-gaps.md)（as-is）｜演练剧本：[三角色操作模拟](./msp-three-persona-operation-simulation.md)

---

## 0. 摘要（TL;DR）

| 项 | 结论 |
|---|---|
| 路线 | **P0 概念显式化 + 安全/可用闭环**（不改数据模型）→ **P1 Membership 化**（结构性）→ **P2 多 provider 与治理收尾** |
| P0 目标 | ① 未分配客户**不可见不可操作**（R9/R10）；② 服务商**工作台 + 过滤器 + 条目级写**可用（WB1–WB6）；③ 登录落 provider 家、切换/刷新 fail-closed（I8）；④ 三通道建号收口（K4）；⑤ 前端上下文/权限链路闭环（FE-A1–A5） |
| P1 目标 | `user_tenant_memberships` 落地（home + allocation 物化）、角色/权限单源、组织挂 membership、邀请与首登、RLS 纳入新表 |
| P2 目标 | 多 provider 维度收窄、RLS `enforce`、共享表治理、工作台批量/自定义视图、guard 扩展 |
| 贯穿不变量 | fail-closed（I7/I8/I9）；**客户端不做权限判定**（I2/A6）；**修订即回填**（审计 §6）；**编号登记**（canon 附录 C）；**每步可独立回滚** |
| 总验收 | canon **A1–A12** + 工作台 **WB-A1–WB-A6** + 前端 **FE-A1–A8** + 本文各阶段 DoD（§6） |

**三阶段出口条件（一句话）**：

- **P0 出口**：服务商在**不切换会话**的情况下安全处理多客户单据；未分配客户全通道 403；登录/切换/刷新/建号按目标契约工作；docs-gate 6/6 + 后端/前端测试全绿 + 三角色剧本 P0 项全过。
- **P1 出口**：membership 成为角色/组织/生效期唯一载体；权限 DB 单源（登录/切换/`/auth/me` 一致）；回填巡检 0 差异；A4/A5/A6/A9 通过。
- **P2 出口**：多 provider 下 N=1 与 N=2 e2e 均通过（A11）；RLS `enforce` 灰度无 500；A12 工单流转通过。

---

## 1. 现状基线（结合现有项目）

| 模块 | 现状（代码/脚本/文档） | 关键缺口 |
|---|---|---|
| 部署门控 | `middleware/msp_gate.go:21-33`：仅 `private` 关闭；`saas`/空/未知值**开启** | **R12**（目标 I12：仅 `saas_msp` 开启） |
| 租户解析 | `middleware/tenant.go`：JWT > Header > 子域名 > 路径；Header 冲突**静默忽略**（JWT 已解析时） | **07:G9**（fail-closed 化） |
| MSP 授权 | 头通道 `X-Customer-Tenant-ID` 仅**头部**校验分配；`MSPAccessValidator` 未接线；路径/请求体通道绕过 | **R9/R10**（P0 安全项） |
| 工单 MSP 字段 | `ent/schema/ticket.go:130-141` 四字段**零写入**，仅 repository 读 | **R11** |
| 归属字段 | `parent_tenant_id` / `msp_provider_id` 只写不读；customer 归属未校验 | **R3**（canon A2/A3） |
| 建号 | HTTP 建号被 tenant guard/角色高攀拦截；`scripts/msp/setup-msp-tenants.sh` **SQL 直写**首个用户与角色权限 | **K4**、**07:G1/G2** |
| 角色供给 | `msp_viewer/tech/specialist/manager/admin` **不在内置词表**（`internal/authz/roles.go`），靠脚本 SQL 直写 + 硬编码兜底 | **K1/K2/K3**（D10 已定稿） |
| 登录/切换 | 服务方登录可带客户 `tenantCode` 直签客户上下文；`switch-tenant` 响应/撤销/审计不一致；refresh 可回退 home | F5/F6/F9/F10/F11/F12 |
| 前端 | `session-bootstrap.ts` 强制 `tenants[0]`；`tenant-api.ts` 端点错误；无过滤器/工作台；菜单缓存未分键；无 403 路由 | **FE-A1–A8** |
| 执行器/RLS | BPMN/定时器/worker 的 tenantctx 不统一；RLS `off/shadow/enforce` 灰度 | 集成分析 §5.2；A7 |
| 运营/验证资产 | `scripts/msp/{build-provision-tenant,setup-msp-tenants}.sh`（8 阶段幂等 + 隔离探针）；`scripts/smoke-test.sh`、`test_permissions_and_menus.sh`、`itsm_api_runner.py`、`regression_p1p2.py`；三角色剧本 §8 checklist；06 §2/§7 探针 | 直接复用为验收载体 |
| 文档门禁 | docs-gate **C.1–C.6**（C.6 = 多租户一致性，`scripts/docs-gate/check-multi-tenant-consistency.sh`） | 每批交付必须 6/6 |

---

## 2. 阶段总览

| 批次 | 主题 | 关键交付 | 依赖 | 出口（DoD 摘要） |
|---|---|---|---|---|
| **P0** | 概念显式化 + 安全/可用闭环（不改数据模型） | R9/R10/R11 修复；建号收口；登录/切换契约；工作台+过滤器；前端链路；角色供给；审计统一 | 无（可独立发布） | §6.2 P0-DoD 全过；A1–A3/A6/A8–A10 中可测项通过 |
| **P1** | Membership 化（结构性） | `user_tenant_memberships` + 回填 + 巡检；权限单源；组织挂 membership；邀请/首登；RLS 纳入；批量 | P0 | §6.3 P1-DoD；A4/A5/A6/A7/A9 |
| **P2** | 多 provider 与治理收尾 | provider 维度与收窄；RLS enforce；共享表治理；自定义视图/配额；guard 扩展 | P1 | §6.4 P2-DoD；A11/A12 |

> **发布策略**：每批内按工作流独立发布（feature flag / 灰度开关），不做大爆炸（canon §8 迁移原则）。
---

## 3. P0 详细实施（10 个工作流）

> 通用规则：每个工作流 = 一个 PR（或一组小 PR）；**代码与文档同 PR**（修订即回填）；新增编号先登记 canon 附录 C；发布前 `make docs-gate` 6/6。

### IP-P0-1 部署与门控显式化（R4/R12；ADR-004:A1/A10）

**目标**：`DEPLOYMENT_MODE` 单一来源；仅 `saas_msp` 开启 MSP；未知值不静默。

**步骤**：
1. `config`：`DEPLOYMENT_MODE` 仅接受 `private|saas|saas_msp`；启动时校验，未知值 **fatal**（带可操作错误信息）；
2. `middleware/msp_gate.go`：目标态 = 仅 `saas_msp` 开启；过渡期（一个版本）对 `saas`/空值打 **warning 日志**，随后拒绝；
3. 启动自检：`gate 状态 ↔ seed 租户形态 ↔ 模式` 一致性（§2.2 矩阵）；`/msp/status` 暴露当前模式与 gate 状态；
4. 文档：02 §9 表格更新为"现状→目标"两列（canon §7 已改）。

**验收**：`private` → `/msp/*` 404；`saas` → 404；未知值 → 启动失败；`saas_msp` → 200；自检日志含模式/gate/租户形态三项。

**回滚**：env 回退 `saas_msp`；自检降级为 warning。

### IP-P0-2 隔离与授权修复：allocation 二次校验（R9/R10/R2/R3；canon A3）

**目标**：**所有通道**（头 / 路径参数 / 请求体）统一走同一授权函数；未分配客户一律 403。

**步骤**：
1. 在 `service/` 层实现唯一入口 `CanAccessCustomer(actor, customerTenantID, action)`：`allocation 有效 ∧ 客户归属 provider ∧ 客户 active ∧ 目标租户 RBAC`；
2. 接线 `MSPAccessValidator`（或删除死代码，二选一，禁止并存）；`GetTicketsForCustomer`/`MSPFilterByCustomer` 一并处理；
3. `handlers/msp/handler.go`：`/msp/customers/:id/tickets`、`assign`（请求体 `customerTenantId`）、工单详情/回复等**全部改走**统一入口；
4. `msp_allocations` 增加部分唯一索引 `(msp_user_id, customer_tenant_id) WHERE deassigned_at IS NULL`（ADR-004:A4 数据面）；
5. 反例用例：未分配客户 3 条通道均 403（见 §6.5）。

**验收**：三角色剧本 M10 三条全部 403（现状为 200，见 07/剧本）；`R9`/`R10` 关闭；新增单测覆盖 3 通道 × 2 角色。

**回滚**：入口函数保留开关 `MSP_STRICT_CUSTOMER_ACCESS`（默认 on），off 时退回旧行为仅用于排障（发布后一个版本移除）。

### IP-P0-3 工单 MSP 快照写入（R11；canon A12）

**目标**：建单落 `is_managed_by_msp / msp_provider_id / managed_by_user_id / msp_ticket_id`，支持按 provider 过滤/统计/外部映射。

**步骤**：
1. 建单链路（客户租户内 + MSP 代建）统一 setter：provider 由**客户派生**（`customer.provider_tenant_id`）+ 落快照；
2. 列表/详情/报表查询按 `provider_tenant_id` 收窄（与 IP-P0-2 入口组合）；
3. `msp_ticket_id` 外部映射字段：仅在有外部工单源时写入，唯一性按 provider 维度；
4. 回填：存量工单按客户归属补快照（幂等脚本，dry-run 输出差异清单）。

**验收**：新建工单四字段正确；按 provider 过滤返回=分配集合内工单；快照与客户归属不一致被拒绝；A12 前置条件满足。

**回滚**：字段可空，停止写入即可；回填脚本带 `--rollback`（清空本批写入标记）。

### IP-P0-4 租户类型与归属收敛（R3；canon A1/A2）

**目标**：`tenants.type` 只出现 3 类新值；customer 归属唯一化。

**步骤**：
1. 校验函数：写入拒绝 legacy 值（`msp`/`customer`/`standard`…），读取兼容映射（文档 + 单测）；
2. `provider_tenant_id`：customer 必填（或显式 `direct_customer` 标记）；`parent_tenant_id`/`msp_provider_id` **二选一**收敛（保留兼容读，停止双写）；
3. 建 customer 租户 API/脚本补归属校验（指向必须为 `msp_provider`，禁自指）；
4. 文档：01/03 的枚举与双字段表述回填为"现状 + 目标"两列。

**验收**：A1/A2 通过；错误归属建租户被拒；存量数据巡检 0 异常。

**回滚**：校验可降级 warning（一个版本）；读兼容保留。

### IP-P0-5 建号通道收口（K4；ADR-004:A2/A3；F1/F3/F14）

**目标**：三通道建号（platform/msp/tenant）统一走 `ProvisionUser`，**handler 不得自行拼 bypass**。

**步骤**：
1. 新建 `service/provisioning.go`：`ProvisionUser(actor, targetTenant, input)` 内部完成 ① `CanAccessTenant` ② 通道授权 ③ `WithProvisioningBypass(actor, reason)` ④ `WithTenantID(target)`；
2. 通道矩阵：`platform`（`user:write` + 平台角色，任意 active 租户）/ `msp`（`provider_admin` + 有效 allocation + `msp_customer:write`，仅分配客户）/ `tenant`（本租户 admin + `roleRank`/`CanGrantRoles`）；
3. 角色白名单：拒绝 `super_admin/sysadmin/admin`；`msp_role` 仅 platform/msp 通道可设；错误码 `CROSS_TENANT_FORBIDDEN/MSP_ALLOCATION_REQUIRED/ROLE_NOT_GRANTABLE/USERNAME_EXISTS/EMAIL_EXISTS`；
4. 端点：`POST /api/v1/tenants/:id/users`、`POST /api/v1/msp/customers/:id/users`（平台/MSP 面），与既有 `POST /api/v1/users` 共用 service；
5. 灰度：`USER_PROVISIONING_CHANNELS_ENABLED`（按租户开启）；运营脚本 §6 的 SQL 直写降级为**兜底**（默认不再使用）。

**验收**：服务商经 API 为分配客户建号 201；未分配客户 403；客户 admin 无法建平台角色（422）；`07:G1/G2` 关闭；K4 关闭。

**回滚**：开关关闭即回退旧路径；SQL 兜底脚本保留。

### IP-P0-6 登录、切换与刷新契约（I8；F5/F6/F9/F10/F11/F12；07:G9）

**目标**：登录落 provider 家；**无候选列表/无 409**；切换/刷新/头通道全部 fail-closed 且可审计。

**步骤**：
1. 登录：`customer` → 唯一租户；`provider` → **provider 家**（不因 `last_active`/`tenantCode` 直签客户）；`platform` → 控制台；登录响应仅 `tenantSelection{mode}`；
2. `/auth/tenants`：语义修正为 **home ∪ 有效分配 ∪ 平台全量**（认证后使用）；
3. `switch-tenant`：目标租户校验（membership/allocation）→ 重签 JWT（`tenant_id + tenant_source + membership_id`）→ **撤销旧 refresh** → 审计 `tenant.switch` → 响应 `user.tenantId == tenant.id`；
4. `refresh`：按 `claims.TenantID` 重签 + 复核；失效 → `TENANT_ACCESS_REVOKED`（不回退 home）；
5. 头/JWT 冲突：**拒绝**（401/400 + `tenant mismatch rejected` 告警）——修 07:G9；
6. 前端契约同步（见 IP-P0-8）。

**验收**：三角色剧本 M1–M5、F5–F12 相关项；`mspadmin` 登录 JWT 落 provider 家；`switch→refresh` 保持目标租户；撤销后旧 refresh 401；冲突请求 401 + 日志。

**回滚**：无状态变更，代码回退即可；审计事件保留。

### IP-P0-7 跨客户工作台 + CustomerFilter + 条目级操作（WB1–WB6；WB-A1–A6）

**目标**：服务商**不切换会话**即可看+做多客户单据；写操作按资源租户授权。

**步骤**：
1. 后端：`GET /msp/workbench/tickets`（逐租户查询 + 内存合并，P0）、`GET /msp/workbench/summary`（计数徽标）、条目级 `reply/status/assign`（`POST /msp/workbench/tickets/:id/*`）；
2. 授权链（每条目）：`资源租户 ∈ 分配 ∧ 目标租户 RBAC ∧ 租户 active ∧ 资源状态`；响应逐行返回 `allowedActions`；
3. bounded bypass：`actor + 集合 + reason`，逐次审计（`source=workbench`）；
4. 前端：顶栏 `CustomerFilter`（全部/子集 + 搜索 + 计数徽标，URL 同步）；列表带"客户"列 + 行内操作；"进入客户"深度入口；
5. 客户账号调用 `/msp/workbench/*` → 403（前端不渲染 + API 拒绝，A9）；
6. 批量操作留 P1（D8）。

**验收**：WB-A1–A6 全过；未分配客户不出现在过滤器/列表（服务端强制）；暂停租户条目只读；审计含 `target_tenant`。

**回滚**：工作台 feature flag（前端）+ 路由开关（后端）；旧 `/msp` 仪表盘保留一个版本。

### IP-P0-8 前端上下文与权限链路（FE-A1–A8；frontend §6）

**目标**：上下文由服务端派生；权限/菜单/缓存随作用域一致刷新。

**步骤**：
1. `tenant-context` v2（id/code/type/role/source/accountKind；**服务端写入**）；移除 `session-bootstrap.ts` 的 `tenants[0]` 强制与 `AuthGuard` 同源逻辑；
2. `tenant-api.ts` 端点修正 `/api/v1/auth/switch-tenant`；`http-client` 注入 `X-Tenant-ID/X-Tenant-Code`，头通道请求注入 `X-Customer-Tenant-ID`（不写上下文）；
3. 切换链路：`setCurrentTenant → GET /auth/me → invalidate ['auth','menus',tenantId] / capabilities → 取消在途请求 → 目标首页`；
4. 登出：`queryClient.clear() + resetSessionBootstrap() + 断 WS`；
5. 守卫：页面级 `hasPermission()` 单一模型 + 独立 `/403` 路由 + `/admin/*`/`/msp/*` 分组守卫；切换中全局 loading 屏障；
6. 登录页：DOM 无任何租户列表（防回归测试）。

**验收**：FE-A1–A8；A9（客户账号无切换器/过滤器/工作台节点）；刷新保持作用域；登出无残留。

**回滚**：前端按发布批次回退（无数据依赖）。

### IP-P0-9 角色供给显式化（K1/K2/K3；D10）

**目标**：`msp_*` 角色纳入内置词表；常规 seed 生成权限行；脚本降级兜底。

**步骤**：
1. `internal/authz/roles.go`：新增 5 角色 `msp_viewer/msp_tech/msp_specialist/msp_manager/msp_admin`（D10 唯一词表）及权限映射；
2. `pkg/seeder`：provider 租户 seed 生成上述角色 + `role_permissions`（幂等）；
3. `msp_role` 收敛：`provider_admin → msp_manager`、`provider_agent → msp_tech`（provider 家）；`customer_user` 仅 legacy 读映射；
4. Q7 预设映射：`observer→msp_viewer`、`full→msp_admin`（合同/分配层预设，不新增角色名）；
5. 脚本 `setup-msp-tenants.sh` §5 改为"校验 + 兜底"（默认不写 SQL）。

**验收**：新 provider 租户 seed 后 `/roles` 可见 5 角色且权限行齐备；不跑脚本的租户不再落入硬编码兜底（K1/K2 关闭）；D10 映射表单测。

**回滚**：角色为新增数据，删除/停用即可；脚本兜底保留。

### IP-P0-10 审计统一与文档回填（I11；C.6）

**目标**：跨租户/切换/建号/工作台操作统一审计字段；文档与代码同步。

**步骤**：
1. 审计字段：`actor_account + membership_id + target_tenant + source(login/header/workbench/platform_selected/job)`；事件目录（`tenant.switch`/`tenant.scope_denied`/`user.invite_accept`…）；
2. 05 使用指南回填工作台/过滤器口径；01–07 与目标差异随各文档下次修订回填；
3. docs-gate C.6 保持 6/6；新增编号（如有）登记 canon 附录 C。

**验收**：审计查询可按 `target_tenant` 过滤；三角色剧本审计断言通过；C.6 通过。

**回滚**：字段为新增列，停止写入即可。

### IP-P0-11 执行器/定时器租户上下文统一（I7；集成分析 §5.2；canon §8 P0 ⑤）

**目标**：所有后台执行路径（BPMN/工作流、定时器/延迟任务、自动化规则、升级矩阵、队列消费者）在**显式租户 ctx** 下运行；无 ctx 即拒绝执行，禁止"无租户上下文旁路"。

**步骤**：
1. 入口收口：`handlers/{bpmn,approval,approval_chain,timer,automation_rule,escalation_matrix}/` 与对应 service 统一经 `tenantctx` 注入（入队即携带 `tenant_id + actor + reason`）；
2. 执行器：worker 取出任务先 `WithTenantID` 再执行；审计 `source=job`；确需跨租户时走显式 bounded bypass；
3. 指派/授权：工作流指派校验 `assignee ∈ provider ∧ allocation`（与 IP-P0-2 同口径）；列表查询 fail-closed；
4. 测试：错误 ctx 下的跨租户 job 必须失败；客户 A 的定时器/自动化不泄漏到客户 B。

**验收**：跨租户 ctx 用例全过（错误 ctx 执行被拒）；`source=job` 审计可查；集成分析 §5.2 项关闭；A7 前置条件满足。

**回滚**：ctx 注入为增量逻辑；异常时按租户关闭自动化入口（feature flag）。
---

## 4. P1 详细实施（Membership 化）

> 原则：先加表/列并回填（只读兼容）→ 双写同事务 → 切读路径 → 废弃旧列；每步独立回滚（canon §8）。

| 工作流 | 目标 | 关键步骤 | 验收（DoD） | 回滚 |
|---|---|---|---|---|
| **IP-P1-1** membership 表 | 角色/组织/生效期唯一载体 | 建 `user_tenant_memberships`（§3.2 字段 + 3 个部分唯一索引，含 **customer 单作用域**）；回填 home（`source=home`）+ 有效 allocation（`source=allocation`）；巡检脚本（0 差异） | A4/A5；巡检报告：customer 恰 1 条 active、provider = home + 有效分配；D10 映射落库 | 新表独立，读路径回退 home+allocation 计算 |
| **IP-P1-2** 权限单源 | 登录/切换/`/auth/me` 一致（I2） | 权限由 DB（membership.role → role_permissions）计算；静态表仅迁移期回退且默认关闭；菜单按目标租户生成 | A6；同一账号在租户 A/B 权限互不影响 | 回退静态表（开关） |
| **IP-P1-3** 组织挂 membership | 多组织归属 + 生效期 | 组织成员关系迁 membership；唯一约束 `(tenant_id, code/name)`；复合 FK（组织与 membership 同租户） | A5；跨租户组织关联被 DB/应用双重拒绝 | 保留 users 单值 FK 兼容读 |
| **IP-P1-4** 邀请生命周期 | 安全邀请 + 首登 | `invitations`（token 哈希、目标租户、角色白名单、TTL、`invited_by`、`revoked_at`）；落地页/设置密码；平台 SMTP 未配置时返回 `inviteUrl` + `emailSent=false` | 邀请→落地→首登→审计 `user.invite_accept`；撤销生效；`super_admin` 不可被邀请 | 表可下线，API 关闭 |
| **IP-P1-5** 首登与 bootstrap 租户化 | 多租户首个管理员 | `must_change_password` 流程；bootstrap token 带 `tenant_id` + 用户名/邮箱策略（`admin-<tenantCode>`）；`cmd/initialize`/`provision_tenant` 支持覆盖 | `07:G2` 关闭：连续 2 租户 bootstrap 成功互不冲突；首登强制改密 | token 机制保留，新增参数可回退 |
| **IP-P1-6** 工作台批量与偏好 | 效率与护栏（D8） | 批量 ≤100、仅低危动作、客户分布确认、逐条授权/审计、限流；过滤器服务端偏好 `users.preferences.workbenchFilter` | WB-A4；批量越限/高危被拒；偏好跨设备生效 | 批量入口 feature flag |
| **IP-P1-7** RLS 纳入新表 | 数据库层兜底 | membership/组织/invitations 纳入 RLS policy；`shadow` 观察 → 无 `requires tenant_id` 报错 | A7；shadow 期 0 新增错误；enforce 前置清单完成 | 模式回退 `shadow/off` |
| **IP-P1-8** 审计看板 | 治理可观测 | 按 `target_tenant/source/membership` 聚合；越权尝试/冲突告警面板 | 审计看板可查"未分配客户访问尝试"；告警接通 | 面板独立 |

---

## 5. P2 详细实施（多 provider 与治理收尾）

| 工作流 | 目标 | 关键步骤 | 验收 | 回滚 |
|---|---|---|---|---|
| **IP-P2-1** provider 维度 | 多 provider 收窄 | `msp_allocations.provider_tenant_id` + 归属一致性校验；工作台/报表/审计按 provider 收窄；N=1 无额外 UI/步骤 | **A11**：同一 e2e 在 N=1/N=2 均过；A12 工单流转通过 | 字段可空 + 收窄开关 |
| **IP-P2-2** RLS `enforce` | 数据库层强制 | 低权角色 + 全路径 ctx 补齐后 `shadow → enforce` 灰度 | enforce 后核心路径 0 500；隔离回归全过 | 回退 `shadow` |
| **IP-P2-3** 共享表治理 | 显式共享（D3） | `TenantExemptTables` 复核（标签云/市场模板/`messages`/`prompt_templates`）；`messages` 租户化决策落地 | 每张共享表有 owner/理由/复核期；季度复核记录 | 逐表回退 |
| **IP-P2-4** 工作台进阶 | 自定义视图/配额 | 保存过滤器组合、SLA 风险看板、每客户配额可视化 | 视图可分享/复现；配额数据与后端一致 | feature flag |
| **IP-P2-5** guard 扩展 | 成员/关联表一致性 | tenant_guard 增加"关联表一致性"检查（跨租户 FK/悬挂成员） | 启动扫描 0 高危；CI 用例覆盖 | 检查项分级（fatal→warn） |

---

## 6. 验收体系（步骤 ↔ 标准 ↔ 证据）

### 6.1 自动化门禁（每个 PR / 发布前）

| 命令 | 作用 | 门槛 |
|---|---|---|
| `make docs-gate`（含 C.1–**C.6**） | 文档质量 + 多租户一致性 | **6/6 通过** |
| `make test` / `make test-backend` / `make test-frontend` | 单测/组件测试 | 全绿；新增用例覆盖本批改动 |
| `make test-e2e` | 端到端 | 关键路径全过（P0：工作台/登录/切换；P1：邀请/首登；P2：N=1/N=2） |
| `make check-contracts` / `make check-handlers-hygiene` / `make verify-scripts` | 工程契约/切片卫生/脚本语法 | 全过 |
| `scripts/smoke-test.sh` | 部署冒烟 | 200/401/403 符合预期 |
| `scripts/test_permissions_and_menus.sh` | 权限/菜单一致性 | 通过 |
| `scripts/msp/setup-msp-tenants.sh` | 一键初始化（8 阶段幂等 + 隔离探针） | 重复执行全 `exists/跳过`；探针通过 |
| 三角色剧本 §8 checklist | 手工/半自动演练 | P0/P1/P2 对应项全过 |

### 6.2 P0 出口 DoD（发布门）

- [ ] **安全**：未分配客户在头/路径/请求体 3 通道均 403（R9/R10 关闭）；头/JWT 冲突 401 + 告警（07:G9 关闭）；
- [ ] **执行器/定时器**：后台任务/自动化在显式租户 ctx 下运行、错误 ctx 被拒、`source=job` 可审计（IP-P0-11）；
- [ ] **功能**：工作台跨客户看+做（WB-A1–A6）；写操作无需切换且逐条审计；
- [ ] **登录/会话**：provider 登录落 provider 家；切换/刷新/撤销契约通过（F5/F6/F9/F10/F11/F12 对应项）；
- [ ] **建号**：三通道 `ProvisionUser` 生效；`07:G1/G2` 关闭；角色白名单生效；
- [ ] **角色供给**：新 provider 租户 seed 后 5 个 `msp_*` 角色权限齐备（K1/K2 关闭）；
- [ ] **前端**：FE-A1–A8；登录页 DOM 无租户列表；客户账号无过滤器/切换器/工作台节点；
- [ ] **门禁**：docs-gate 6/6；`make test` 全绿；三角色剧本 P0 项全过。

### 6.3 P1 出口 DoD

- [ ] membership 回填巡检 0 差异；customer 恰 1 条 active（部分唯一索引生效，A4）；
- [ ] 权限 DB 单源：登录/切换/`/auth/me` 权限一致（A6）；跨租户权限互不影响；
- [ ] 组织多归属 + 生效期（A5）；邀请/首登链路通过；bootstrap 多租户连续成功（07:G2 关闭）；
- [ ] RLS `shadow` 无新增错误（A7）；批量护栏通过（WB-A4）；
- [ ] docs-gate 6/6；`make test` 全绿。

### 6.4 P2 出口 DoD

- [ ] A11（N=1/N=2 同一 e2e）；A12（多 provider 工单流转：快照/收窄/指派校验/通知双投递/拒绝路径）；
- [ ] RLS `enforce` 灰度无 500；共享表治理清单完成（owner/复核期）；
- [ ] guard 扩展检查 0 高危；docs-gate 6/6。

### 6.5 安全反例（必须全部拒绝，target-arch §5.4）

1. 客户 A 账号访问客户 B 数据 → 401/403；
2. 客户账号被加入第二个租户 → 拒绝（`CUSTOMER_SCOPE_CONFLICT`，P1 起 DB 兜底）；
3. 服务方访问未分配客户（头/切换/路径/请求体）→ 403；
4. 同一人双重身份（provider + customer）→ 拒绝（不同账号）；
5. 非平台身份用 query/body/header 指定目标租户 → 拒绝；
6. 服务方在客户作用域执行未授予的写操作（CMDB 写/变更审批/系统配置/角色管理/审计导出）→ 拒绝。

### 6.6 canon A1–A12 映射（验收 → 工作流 → 证据）

| 验收 | 落地工作流 | 证据 |
|---|---|---|
| A1 type 3 类 + legacy 只读 | IP-P0-4 | 单测 + 巡检 |
| A2 customer 归属必填 | IP-P0-4 | 建租户反例 |
| A3 provider 不可跨 provider 分配 | IP-P0-2/4 | 反例用例 + 剧本 M8 |
| A4 多租户 membership（customer 单作用域） | IP-P1-1 | 回填巡检 + DB 约束 |
| A5 单租户多组织归属 | IP-P1-3 | 集成测试 |
| A6 权限 DB 单源一致 | IP-P1-2 | 三端点对比测试 |
| A7 RLS shadow 无 ctx 报错（含执行器/定时器） | IP-P1-7 + IP-P0-11 | shadow 观察报告 |
| A8 跨租户写仅两类且审计 | IP-P0-7/10 | 审计查询 |
| A9 客户账号三无（前端+API） | IP-P0-7/8 | FE 测试 + API 403 |
| A10 模式单一来源自检 | IP-P0-1 | 启动自检日志 |
| A11 N=1/N=2 e2e | IP-P2-1 | e2e 报告 |
| A12 多 provider 工单流转 | IP-P0-3 + IP-P2-1 | 剧本 P 系列 + 快照断言 |

---

## 7. 发布、灰度与回滚

| 项 | 策略 |
|---|---|
| 灰度开关 | `USER_PROVISIONING_CHANNELS_ENABLED`（建号，按租户）；工作台/过滤器前端 flag；`MSP_STRICT_CUSTOMER_ACCESS`（临时排障）；RLS `off→shadow→enforce` |
| 数据迁移 | 只增不删（加列/加表/加索引，在线 DDL）；回填脚本**幂等 + dry-run 差异清单**；双写同事务；每步独立回滚点（canon §8） |
| 发布顺序 | P0：安全修复（IP-P0-1/2/3）→ 登录/切换（6）→ 建号（5）→ 工作台（7）→ 前端（8）→ 角色/审计（9/10）；每项可独立发布 |
| 回滚 | 代码回退 + 开关关闭；数据变更保留（只增不删）；审计不删除 |
| 运营脚本 | `scripts/msp/*` 保持幂等；SQL 直写仅兜底；`$HOME/itsm-artifacts` 路径约定（07:G10） |

---

## 8. 风险、依赖与决策前置

| 项 | 影响 | 处置 |
|---|---|---|
| **R9/R10**（未分配客户可读/可指派） | 安全（高） | P0 首日修复（IP-P0-2），修复前禁止生产开通多客户 |
| **R12**（未知模式开启 MSP） | 安全/治理 | IP-P0-1；过渡期告警 |
| **K1/K4**（角色供给/建号） | 交付效率 | IP-P0-5/9；SQL 兜底保留 |
| **D2**（直客无 provider） | 归属模型 | IP-P0-4 前确认；默认"显式直客标记" |
| **D3**（`messages` 租户化） | 共享表语义 | IP-P2-3 前确认 |
| **D5**（平台 membership） | 治理审计 | 与 D11 一并确认（平台治理通道独立） |
| **D8**（批量边界） | 效率/风险 | IP-P1-6 前确认（低危 + 护栏为建议值） |
| **E1–E6**（工单流转） | A12 | IP-P0-3/IP-P2-1 设计冻结前确认 |
| **07:G4/G5/G6**（审批组/序列/唯一约束） | 供给可复现 | 迁移脚本一次性校正（并入 IP-P1-5） |
| **07:G10**（snap docker 路径） | 运维 | 路径约定 + sha256 校验（已文档化，脚本沿用） |

---

## 9. 里程碑与工作量（粗估，待排期校准）

| 批次 | 工作流数 | 粗估（人日，后端+前端+测试） | 关键里程碑 |
|---|---|---|---|
| P0 | 10 | 25–40 | M1 安全闭环（IP-P0-1/2/3）；M2 登录/建号（5/6）；M3 工作台+前端（7/8）；M4 角色/审计+文档（9/10） |
| P1 | 8 | 25–35 | M5 membership+回填；M6 权限单源/组织；M7 邀请/首登；M8 RLS shadow |
| P2 | 5 | 15–25 | M9 provider 维度+N=2 e2e；M10 RLS enforce；M11 治理收尾 |

> 说明：估算不含产品决策等待时间（D/E 项）；以"工作流 = 1 个可独立发布的 PR 组"为粒度校准。

---

## 10. 维护规则（本方案与设计文档同步）

1. **修订即回填**：本方案引用的设计口径变化时，同 PR 更新本方案 + 被修订文档（审计 §6）；
2. **编号**：本文使用 `IP-P*-*` 局部编号（canon 附录 C 已登记）；引用他文档编号必须带前缀（`ADR-004:A#`、`07:G#`、`WB-A#`、`FE-A#`）；
3. **状态行**：本方案每次批次交付后更新头部日期与"当前批次进度"；docs-gate C.6 强制头部四件套；
4. **验收证据**：每批出口 DoD 的勾选必须有可复现证据（命令输出/报告/审计查询），存入 `docs/release/` 或发布记录。

---

## 修订记录

| 版本 | 日期 | 变更 |
|---|---|---|
| v0.1 | 2026-09-29 | 首版：P0（10 工作流）/P1（8）/P2（5）步骤、验收 DoD、A1–A12 映射、发布回滚、风险与里程碑 |
| v0.2 | 2026-09-29 | 补 **IP-P0-11（执行器/定时器租户 ctx 统一，原 canon P0 ⑤ / 集成分析 §5.2 缺口）**；P0 DoD 增执行器勾选项；A7 证据并入该项 |
