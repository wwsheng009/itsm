# 多租户 × 权限系统 / 部门 / 团队 / 工作流：集成分析与冲突处置方案

> 状态：**Draft v0.4（2026-09-30 一致性回填 + P1 契约同步）**｜日期：2026-09-30｜基准：仓库 HEAD `337558e3`
> 范围：现有 RBAC、组织架构（部门/团队/组/项目）、工作流（审批链/BPMN/自动化/SLA/定时器）、通知订阅 与目标多租户机制（membership 多作用域、条目级跨租户操作、fail-closed）的**集成关系与冲突**
> 关联：[目标架构方案](./msp-target-architecture.md)｜[跨客户工作台与全局过滤方案](./msp-cross-customer-workbench-and-filter-plan.md)｜[登录与切换细化方案](./msp-login-and-switching-refinement-plan.md)｜[前端页面与权限分析](./msp-frontend-pages-and-permissions-analysis.md)｜[07 已知缺口](../07-known-gaps.md)
> 判定口径：✅ 兼容｜🟡 需改造｜❌ 冲突（安全/正确性风险）
> 编号：本文 §10 开放问题使用局部编号 `D1–D6`（跨文档引用必须带前缀 `INT-D#`，避免与 canon D# 混淆）。

---

## 0. 摘要（TL;DR）

| 域 | 判定 | 结论 |
|---|---|---|
| **权限系统（RBAC）** | 🟡 | 角色/权限/授权三表**已带 tenant_id**、运行时 DBOnly fail-closed ✅；但 `user_roles` 被**豁免为平台级**（无 tenant_id）、登录/切换权限来自**静态表**（双源）、`data_scope=department` **未实现** |
| **部门/团队/组/项目** | 🟡→❌ | 主表 tenant_id 齐全、应用层租户过滤完整 ✅；但**成员关系是 users 表上的单值 FK 列**（`department_id`/`group_members`/`team_users`）——**没有成员行**，无法承载"租户内多归属 + 角色 + 生效期"；`project.code`、`username/email` **全局唯一**与多租户目标冲突 |
| **工作流/审批/BPMN** | 🟡 | 实体 tenant_id **全覆盖且未豁免** ✅；审批人解析**全链路按租户收窄** ✅（用户/角色/组/部门/团队/项目/CAB）；存在 4 处 ❌（见 §7） |
| **自动化/定时器/后台任务** | ❌ | 定时器/worker 仅注入 `BPMNTenantIDContextKey`、**未注入 `tenantctx`**；RLS `enforce` 后这些路径必然失败（当前 `RLS=off` 掩盖）；`commandbus`（claim→条目级执行）是**正确样板** |
| **通知/订阅** | ✅→🟡 | 查询/偏好/投递/收件人全链路租户收窄 ✅；`messages` 模板表**全局豁免待评估**；后台通知"从工单反查租户"需改为显式作用域 |

**一句话**：**租户维度在数据模型上是"基本齐全"的**（tenant_guard 强制 + 组织/工作流/通知主表都带 `tenant_id`），真正的冲突集中在**三个结构性缺口**：① 组织成员关系没有成员行（无 membership 载体）；② 权限双源 + `user_roles` 平台级豁免；③ 执行器/定时器/后台任务的租户上下文通道不统一（RLS 兼容性）。另有 4 处**具体安全缺陷**需 P0 修复（§7.1）。

> **专项核实（§11）**：`msp_provider` 只是**租户类型标签**；"所有服务商用户同属一个平台级 tenant"**不是硬约束**——`saas_msp` 部署 seed 只创建一个 `code="default"` 的 provider 租户（隐式单例），且 `parent_tenant_id`/`msp_provider_id` 为"死元数据"、分配不校验 provider 归属（风险 R1–R6）。

---

## 1. 判定框架（五问）

对每个子系统问五个问题，任一"否"即存在集成缺口：

| # | 问题 | 说明 |
|---|---|---|
| Q1 | 数据是否带 `tenant_id`（或显式豁免登记）？ | `tenant_guard` 生产 fatal：缺列且未豁免 → 拒绝启动 |
| Q2 | 查询/写入是否**查询级**限定租户（而非取回后内存过滤）？ | 内存过滤 = 纵深防御缺口 |
| Q3 | 人员/角色解析是否限定在**目标租户内**？ | 审批人、处理人、通知收件人 |
| Q4 | 异步执行（定时器/队列/worker）是否携带**租户上下文**？ | RLS `enforce` 的硬前提 |
| Q5 | 跨租户访问是否**显式授权 + 可审计**（bounded bypass / 条目级授权）？ | MSP 工作台、平台治理 |

---

## 2. 权限系统 × 多租户

### 2.1 现状（✅ 基础扎实）

| 项 | 现状（证据） | 判定 |
|---|---|---|
| 角色/权限/授权表 | `roles`/`permissions`/`role_permissions` 均带 `tenant_id`；`role_permissions` 唯一键 `(role_id, permission_id, tenant_id)`（`ent/schema/role.go:38-40`、`permission.go`、`role_permission.go:16-35`） | ✅ |
| 运行时执法 | **DBOnly fail-closed**（`middleware/rbac.go:487-504,1119-1135`；`middleware/smart_permission.go:144-150`） | ✅ |
| 权限缓存 | key = `roleName_tenantID`，TTL 5min；失效本地 + Redis 广播（`middleware/rbac.go:41-55,1225-1232`；`permission_cache_broadcast.go:24-68`） | ✅ |
| 菜单 | 后端按 `tenant_id + role + permission` 生成（`service/menu_service.go:334-411`）；无缓存 | ✅ |
| 数据权限范围 | `data_scope` 枚举 `all/department/owner` 已定义，未知值 fail-closed 收窄 owner（`handlers/common/datascope/datascope.go:19-41`） | 🟡 见 2.3 |

### 2.2 冲突点

| # | 冲突 | 证据 | 影响 |
|---|---|---|---|
| C1 | **`user_roles` 平台级豁免（无 tenant_id）** | `internal/schema/tenant_guard.go:83`（reason："平台级 RBAC 关系…ADR 0001"） | 用户↔角色关系不携带租户；加载时先 `user.ID(userID)` 再**内存过滤** `r.TenantID == tenantID`（`middleware/rbac.go:877-905`）→ 与 membership 模型（"角色挂 membership"）直接冲突 |
| C2 | **权限双源**：登录/切换响应 permissions 来自静态硬编码表（`handlers/common/service.go:67-91`、`handlers/auth/service.go:90-112`），运行时却是 DBOnly | 同上 | 前端"看得见点不动"；切换后权限快照不可信（前端分析 R1） |
| C3 | `roleRank`/`CanGrantRoles` 双实现（handler 层 + service 层各一份词表） | `handlers/user/handler.go:515-531`、`service/user_service.go:169-209` | 新增 `msp_*` 角色需两处同步，易漂移 |
| C4 | `msp_*` 角色双轨：硬编码表 + DB 模板（DBOnly 下以 DB 为准） | `middleware/rbac.go:437-469`、`middleware/msp_rbac.go:18-30` | 若租户模板缺 `msp_*` DB 授权，MSP 权限静默失效 |

### 2.3 `data_scope` 未落地（🟡）

- `DataScopeDepartment` 有定义、有 DTO 暴露（`dto/role_dto.go:49,100`），但仓库层只实现 `All` vs `OwnedOrAssigned`（`repository/ticket/repository_impl.go:499-503` 等）；`ApplyTicketFilter` 的部门分支 **0 调用点**（死代码，`handlers/common/datascope/datascope.go:117-143`）。
- 结论：**"按部门数据范围"目前是空承诺**；在 membership 化时应明确：要么实现（部门子树 × 租户内），要么从 DTO/文档移除该档位。

### 2.4 目标收敛（与目标架构对齐）

1. **角色挂 membership**：`user_tenant_memberships.role_id` 为唯一权威；`user_roles` 降级为"平台角色"（super_admin 等）专用，或直接收敛进 membership（P1 决策）；
2. **权限单源**：登录/切换/`/auth/me` 统一从 DB 按目标租户计算（B4 修复）；
3. **`data_scope` 二选一**：P1 实现"department"档（基于 membership 的部门归属 + 部门子树），或显式下线；
4. 角色变更 → membership 更新 → 权限缓存失效（已有 Redis 广播 ✅）。

---

## 3. 组织架构（部门 / 团队 / 组 / 项目）× 多租户

### 3.1 现状（✅ 表级隔离完整）

| 实体 | tenant_id | 关键点 |
|---|---|---|
| `departments` | ✅ `department.go:34` | 树（`parent_id`）、`manager_id`；边 users/tickets/workflows/categories/projects |
| `teams` | ✅ `team.go:34` | `manager_id`；边 users |
| `groups` | ✅ `group.go:25` | 边 members/on_call_schedules/assigned_incidents |
| `projects` | ✅ `project.go:44` | `department_id`；**`code` 全局 Unique ❌** |
| `cab_members` / `engineer_skills` / `on_call_schedules` / `on_call_shifts` | ✅ | `on_call_*` 有 `(tenant_id, ...)` 唯一索引 |

- 应用层隔离：部门/团队/组 CRUD 与树查询**全部** `TenantIDEQ`（`service/department_service.go`、`team_service.go`、`group_service.go` 全文）；成员添加校验"同租户且 active"（`team_service.go:52-70`、`group_service.go:134-167`）✅；
- 路由在 tenant group 下并带 `RequirePermission("department|team", ...)`（`router/common_system_routes.go:76-87,119-124`）✅；
- tenant_guard：上述表**均未豁免**（含 tenant_id）✅。

### 3.2 结构性冲突（❌ 核心）

| # | 冲突 | 证据 | 说明 |
|---|---|---|---|
| C5 | **成员关系没有成员行** | `ent/migrate/schema.go:5724-5725`（`users.group_members`、`users.team_users` 单值 FK 列）；`ent/schema/user.go:41`（`department_id`） | 一个用户最多 1 部门 / 1 组 / 1 团队；**无 tenant_id/scope/角色/生效期载体** → membership 多作用域模型"无结构可挂"；反向边 `groups.user_groups` 是另一条无关单向 FK（`:1975`） |
| C6 | 组织不参与授权判定 | `middleware/smart_permission.go`（grep 无组织命中）；`acl_expression_engine.go:22,202`（变量无 dept/team/group） | 组织仅作为**权限资源名**存在；"按部门/团队授权"目前只能靠审批人解析（BPMN 组解析 `bpmn_group_resolver.go:74-86`） |
| C7 | 全局唯一键 | `ent/schema/project.go:24`（`code`）；`ent/schema/user.go:22-27,48-51`（`username/email/feishu_open_id`） | 跨租户命名冲突 + 可探测他租户存在性；与"同一自然人跨租户 membership"目标冲突 |
| C8 | 裸 FK 无复合约束 | `user.go:41`、`department.go:31`、`project.go:32`、`cab_member.go:53`、`engineer_skill.go:67` | `user_id`/`department_id` 可指向他租户行，DB 无兜底，仅 service 自觉 |
| C9 | 客户端可覆盖租户 | `handlers/skill/handler.go:456-460` | Skill 入参 `tenantId` 可覆盖上下文租户 → 若以之查库即**跨租户读** |
| C10 | 组织表零 RLS | `database/rls/migrations/002_pilot_policies.sql:28-61`（仅 changes/vectors）；`config/config.go:460-464`（默认 off） | 隔离 100% 依赖应用层；RLS 适配器 skeleton 未接 Ent 主链路 |
| C11 | 无唯一约束兜底 | `group.go`/`team.go` 无 Indexes；部门 code 仅应用层查重 | 同租户重名/重复 code 可能产生 |
| C12 | guard 只查列存在性 | `internal/schema/tenant_guard.go:164-169` | 成员 FK、复合唯一、关联表 tenant 一致性不在门禁范围 |

### 3.3 目标模型（组织 = 租户内实体 + membership 载体）

```text
租户 T 内：
  departments（树）─┬─ projects ── teams
                    └─ users（home 归属）
membership（新增，统一作用域）：
  (user_id, tenant_id, org_type, org_id, role, is_default, expires_at, status)
  org_type ∈ department|team|group|project
  → 一个用户在一个租户内可多归属（部门 + 多个团队/项目组），带角色与生效期
```

- 组织关系**从 users 单值列迁移为 membership 行**（旧列保留只读兼容 + 回填）；
- 组织实体唯一约束统一为 `(tenant_id, code)` / `(tenant_id, name)`；
- 复合外键/应用层双保险：`(tenant_id, org_id)` 一致性校验（DB 侧用复合唯一索引兜底）；
- `data_scope=department` 基于 membership 的部门子树实现（或下线）。

---

## 4. 工作流 / 审批 / BPMN × 多租户

### 4.1 现状（✅ 实体与解析双达标）

- **实体 tenant_id 全覆盖且未豁免**：`approval_workflow:44`、`approval_record:62`、`ticket_approval:39`、`process_definition:81`（索引 `(tenant_id,key,version)`）、`process_instance:57`、`process_task:84`、`workflow:36`、`workflowinstance:37`、`workflowtask:58`、`ticket_workflow_record:58`、`ticket_assignment_rule:42`、`ticket_automation_rule:46`、`incident_escalation_rule:64`、`sla_policy:59`、`workflow_template:26`（唯一键 `(tenant_id,key,version)`）、`workflowversion:43`、`bpmn_permission:49`、`processbinding` ✅；
- **审批人/处理人解析全链路租户收窄** ✅：`service/approver/resolver.go:22-31`（`ApproverContext` 强制携带 `TenantID`）；用户/角色/组/部门经理/团队/项目经理/CAB 六类 resolver 均带租户过滤（`approval_chain_evaluation.go:311-331,383-428`、`bpmn_group_resolver.go:80-84,155-170`）；**未发现跨租户角色名匹配**；
- **定义/实例/任务链路 fail-closed** ✅：定义读取要求 BPMN 租户上下文、请求不一致直接拒绝（`bpmn_process_definition_service.go:365-371`）；引擎从 ctx 取租户（`bpmn_process_engine.go:223`）；任务待办三重收窄（`bpmn_task_service.go:165-202,579-618`）；
- **无流程引擎缓存** ✅（grep 无 cache/sync.Map）——无跨租户缓存风险。

### 4.2 冲突点

| # | 冲突 | 证据 | 影响 |
|---|---|---|---|
| C13 | **指派/重派不校验处理人租户** | `service/ticket_assignment_service.go:571`（`client.User.Get(ctx, assigneeID)`）、`:485-503` | 可把工单指派给**他租户用户** → 后续通知/权限链错乱 |
| C14 | **BPMN 权限授予去重缺租户** | `service/bpmn_permission_service.go:59-65`（GrantPermission 去重未带 tenant） | 可**跨租户覆写**授权记录（Revoke 已强制 ctx 租户 `:98-111` ✅） |
| C15 | **列表 fail-open** | `service/workflow_service.go:74-75`（`req.TenantID<=0` 时不加租户过滤）；`bpmn_deployment_service.go:200-201` 同型 | 未带租户的调用会返回**全租户列表** |
| C16 | 组名回退角色后二次查询缺租户 | `service/bpmn/bpmn_group_resolver.go:140-148` | 纵深防御缺口（ids 源自租户内解析，当前风险有限） |
| C17 | `approvalchain.tenant_id` Optional 与读取恒 `TenantIDEQ` 矛盾 | `ent/schema/approvalchain.go:25`、`service/approval_chain_service.go:77,92,128` | NULL 租户的"全局链"实际不可达 → 语义死代码 |
| C18 | `incident_escalation_rule.target_assignee_id/target_group` 为裸字段 | `ent/schema/incident_escalation_rule.go:43,46`；未找到解析为租户内用户/组的代码 | 规则目标可能指向他租户实体（或被静默忽略） |

---

## 5. 自动化 / 定时器 / 后台任务 × 多租户（❌ 最集中的区域）

### 5.1 现状与冲突

| # | 项 | 证据 | 判定 |
|---|---|---|---|
| C19 | 定时器/超时扫描**只注入 `BPMNTenantIDContextKey`，未注入 `tenantctx`** | `service/timer_event_handler.go:213`、`service/bpmn_timeout_scanner.go:76` | ❌ 与 RLS 只认 `tenantctx` 的通道不一致；`enforce` 后失败 |
| C20 | 调度器跨租户枚举无显式 bypass 声明；`CASFire` 无租户参数 | `service/timer_scheduler.go:422-423`、`timer_store.go:197` | 🟡 缺纵深（timerID 全局唯一，风险有限） |
| C21 | 后台 worker 逐租户循环但**未 per-tenant 注入 ctx**，也未整体 `SystemContext` | `internal/bootstrap/app.go:1919-1923,1941-1954` | 🟡 当前 `RLS=off` 掩盖；enforce 后报"requires tenant_id" |
| C22 | **自动升级任务未接线**（`StartAutoEscalationTimer` 无调用方） | `service/workflow_automation_service.go:143,458` | ❌ 功能实际不运行（静默失效） |
| C23 | 正确样板：commandbus claim 用 `SystemContext`，执行前 `WithTenantID(claimed.TenantID)` 收窄 | `internal/commandbus/commandbus.go:195,241` | ✅ **所有执行器应对齐此模式** |

### 5.2 目标模式（执行器租户上下文规范）

```text
后台任务/定时器/队列：
  1) 取任务：SystemContext（显式、可审计的跨租户读取，仅限"领取"动作）
  2) 执行前：WithTenantID(task.TenantID) → tenantctx（RLS/服务层一致）
  3) 若需跨租户批量：WithMSPWorkbenchBypass / 显式 system bypass（携带 actor+reason+集合）
  4) 禁止：无租户 ctx 直接执行业务写；禁止仅注入 BPMN 专用 key
```

---

## 6. 通知 / 订阅 × 多租户

| 项 | 现状（证据） | 判定 |
|---|---|---|
| 通知/偏好/投递实体 | `notification:39`、`notification_preference:22`、`notification_delivery:16` 均带 tenant_id + 索引 | ✅ |
| 查询/落库 | `notification_service.go:29-46,72,212-226` 全链路租户校验 | ✅ |
| 收件人解析 | `notification_outbox.go:26-46`（`user.TenantIDEQ + Active`） | ✅ |
| 投递命令 | `notification_delivery_command_handler.go:36,89,158,265`（按 `cmd.TenantID` 收窄；外部通道显式传租户） | ✅ |
| 工单通知 | `ticket_notification_service.go:635-636,718-724`（tenant mismatch 拒绝） | ✅ |
| **串租户投递路径** | 未找到（已检索） | ✅ |
| 后台通知"反查租户" | `ticket_notification_service.go:1066-1074`（`resolveTenantID(ticketID)` 而非 ctx） | 🟡 membership 多作用域下语义需重审 |
| 通知模板 | 内容内联生成；`messages` 表**全局豁免**且标注"待评估"（`tenant_guard.go:112`） | 🟡 模板租户化或固化产品决策 |
| 共享表 | `marketplace_items`、`prompt_templates` 全局豁免（`:111,113`） | 🟡 若用于流程/模板分发即为跨租户共享通道，需显式授权语义 |

---

## 7. 冲突总清单与处置矩阵

### 7.1 🔴 P0：安全/正确性缺陷（建议立即修复，独立于 membership 化）

| # | 缺陷 | 位置 | 修复 |
|---|---|---|---|
| 1 | 指派/重派不校验处理人租户 | `ticket_assignment_service.go:571,485-503` | 校验 `assignee.TenantID == ticket.TenantID`；失败即拒绝 + 审计 |
| 2 | BPMN 授权授予可跨租户覆写 | `bpmn_permission_service.go:59-65` | 去重键加 tenant；授予强制 ctx 租户 |
| 3 | 列表 fail-open（全租户） | `workflow_service.go:74-75`、`bpmn_deployment_service.go:200-201` | `TenantID<=0` → 拒绝（fail-closed） |
| 4 | Skill 入参可覆盖租户 | `handlers/skill/handler.go:456-460` | 强制覆写上下文租户；不一致拒绝 |
| 5 | 自动升级任务静默失效 | `workflow_automation_service.go:458` | 接线 + 补租户 ctx（对齐 commandbus 样板） |
| 6 | 组织/项目全局唯一键（跨租户冲突+可枚举） | `project.go:24`、`user.go:22-27,48-51` | `project.code` → `(tenant_id, code)`；账号唯一性按既定 Q3 决策保持（见 §10 INT-D2） |

### 7.2 🟡 P1：结构性改造（与 membership 化同批）

| # | 项 | 位置 | 改造 |
|---|---|---|---|
| 1 | 成员关系无成员行 | `migrate/schema.go:5724-5725`、`user.go:41` | 新增 `user_tenant_memberships`（权威 DDL 见[目标架构 §3.2](./msp-target-architecture.md)）+ 组织关联子表 `user_tenant_membership_orgs`（[实施方案 §4.0-A](./msp-implementation-plan.md)），旧列回填兼容 |
| 2 | `user_roles` 平台级豁免 | `tenant_guard.go:83` | 角色挂 membership；`user_roles` 收敛为平台角色专用 |
| 3 | 权限双源 | `common/service.go:67-91`、`auth/service.go:90-112` | 统一 DB 计算（B4） |
| 4 | `data_scope=department` 空承诺 | `datascope.go:117-143` | **下线**（canon D6 已确认：P1 移除档位并显式报"未启用"） |
| 5 | 执行器租户 ctx 不统一 | `timer_event_handler.go:213` 等 | 统一 `tenantctx`（§5.2 规范） |
| 6 | 后台 worker 缺租户 ctx | `app.go:1919-1954` | `SystemContext` + per-tenant `WithTenantID` |
| 7 | 组织零 RLS | `002_pilot_policies.sql`、`config.go:460-464` | R2 把组织表纳入 policy（与 membership 表同批） |
| 8 | 裸 FK 无复合约束 | `user.go:41` 等 | `(tenant_id, id)` 复合唯一 + 应用层校验双保险 |
| 9 | 组织唯一约束缺失 | `group.go`/`team.go` | `(tenant_id, name)` 唯一 |
| 10 | 通知模板全局豁免 | `tenant_guard.go:112` | **租户化**（canon D3 已确认；平台默认模板登记豁免、租户可覆盖） |
| 11 | 后台通知反查租户 | `ticket_notification_service.go:1066-1074` | 显式传作用域 |
| 12 | `approvalchain` NULL 语义 | `approvalchain.go:25` | 收敛为 NOT NULL 或删除"全局链"概念 |
| 13 | 升级规则目标裸字段 | `incident_escalation_rule.go:43,46` | 目标解析限定租户内 |
| 14 | guard 只查列存在性 | `tenant_guard.go:164-169` | 扩展校验：成员/关联表 tenant 一致性 |

### 7.3 汇总判定

| 域 | ✅ | 🟡 | ❌ |
|---|---|---|---|
| 权限系统 | 表结构/DBOnly/缓存/菜单 | 数据范围、双源、双实现 | `user_roles` 平台级（结构） |
| 组织 | 主表 tenant_id、应用层过滤、guard | 唯一约束、裸 FK、tags 豁免 | 成员行缺失、客户端可覆盖租户、零 RLS |
| 工作流/审批 | 实体/解析/定义实例任务/无缓存 | 二次查询、NULL 语义、规则目标 | 指派未验租户、授权覆写、列表 fail-open、自动升级未接线 |
| 执行器 | commandbus 样板 | 定时器枚举/CAS/worker ctx | 租户 ctx 通道不统一（RLS enforce 必失败） |
| 通知 | 全链路收窄、无串租户路径 | 模板豁免、后台反查租户 | — |

---

## 8. 目标集成模型（不变量）

| # | 不变量 |
|---|---|
| I1 | **一租户内闭环**：组织、角色、工作流定义、模板、自动化规则、通知偏好全部租户内自洽；跨租户仅通过显式通道 |
| I2 | **membership 是唯一作用域载体**：账号↔租户↔组织归属↔角色↔生效期，一行承载；旧单值 FK 只读兼容并回填 |
| I3 | **权限单源**：DB 按目标租户计算（登录/切换/`/auth/me`/菜单一致）；静态表仅作迁移期回退且默认关闭 |
| I4 | **执行器必须携带租户上下文**：claim（SystemContext）→ 执行（`WithTenantID`）；禁止裸执行；BPMN 专用 key 与 `tenantctx` 统一 |
| I5 | **跨租户写入显式化**：MSP 条目级操作（资源租户授权 + 审计）或 bounded bypass（服务端集合 + actor + reason）；`tenant_guard` 豁免清单季度复核 |
| I6 | **唯一约束租户内化**：业务键一律 `(tenant_id, key)`；全局唯一仅保留平台实体（`tenants`、`schema_migrations` 等已登记项） |
| I7 | **数据范围显式**：`data_scope` 要么实现（部门子树）、要么移除；不允许"声明了但不生效"的权限语义 |

---

## 9. 分期与验收

| 批次 | 内容 | 验收 |
|---|---|---|
| **P0（安全修复，独立可发）** | §7.1 六项 + 定时器/worker ctx 统一（C19/C21） | 指派/授权/列表/技能四类越权用例全部拒绝并审计；RLS `shadow` 下执行器无"requires tenant_id"报错 |
| **P1（与 membership 同批）** | `user_tenant_memberships` 表 + 回填；角色挂 membership；权限单源；组织唯一约束/复合 FK；RLS 纳入组织表；`data_scope=department` 下线 | 一个用户在一租户内可多组织归属并携带角色；跨租户成员关系被 DB 与应用双重拒绝 |
| **P2（收尾）** | `messages`/`is_public` 模板共享决策；guard 扩展；升级规则目标解析；`approvalchain` 语义收敛 | 共享表清单与豁免理由经评审；guard 覆盖成员/关联表一致性 |

---

## 10. 风险与开放问题

| # | 项 | 说明 |
|---|---|---|
| INT-D1 | membership 表与现有单值 FK 的迁移顺序 | ✅ 已确认（canon D7）：先建 `user_tenant_memberships` + 回填（只读），再切读路径，最后废弃列；期间双写同事务 |
| INT-D2 | `username/email` 全局唯一是否保留 | ✅ 已确认（canon D4）：保持全局唯一 + `identity_key` 仅作识别、不做账号合并 |
| INT-D3 | `messages` 模板是否租户化 | ✅ 已确认（canon D3）：租户化（租户可覆盖）；平台默认模板登记豁免 |
| INT-D4 | `is_public` 工作流模板的跨租户语义 | 需显式"条目级跨租户授权"（谁可见/可实例化 + 审计），不能靠 `is_public` 隐式放行（P2，随 IP-P2-3 登记复核） |
| INT-D5 | RLS `enforce` 的推进顺序 | ✅ 既定：先执行器 ctx 统一（P0=IP-P0-11）→ 再 shadow 比对 → 最后 enforce（IP-P2-2）；组织表与 membership 表同批纳入 |
| INT-D6 | `data_scope=department` 的实现成本 | ✅ 已确认（canon D6）：**下线**该档位（P1），未来按 membership 部门子树立项 |

---

## 11. 专项核实：`msp_provider` 概念与"单一 provider 租户"假设（2026-09-29）

**问题**：服务商（MSP）用户是否都属于同一个平台级 tenant（类型 `msp_provider`）？

**结论：不是硬约束，但存在"隐式单例"。**

- `msp_provider` 是 **`tenants.type` 枚举上的一个类型标签**（legacy `msp`），不是独立实体；provider 租户与客户租户之间**没有 edge/外键**，`parent_tenant_id`（"MSP客户指向MSP提供商"）与 `msp_provider_id`（"MSP服务提供商ID"）都是**可空裸整数**（`ent/schema/tenant.go:29-41`；Tenant 仅 users/allocations/bootstrap_tokens 三条 edge，`:80-88`）。
- **事实上的唯一 provider**：`saas_msp` 部署下 seed 把 `code="default"` 租户设为 `msp_provider`（"MSP Provider Tenant"，`pkg/seeder/seeder.go:784-833`）；测试断言该模式**只建 provider、不建客户**（`seeder_test.go:159-178`）。全仓**无任何配置项**固定 provider 租户 ID/Code。
- **身份判定与 provider 归属无关**：`IsMSP = 用户 home tenant 类型 ∈ {msp_provider, msp} ∧ msp_role ≠ ''`（`middleware/msp_middleware.go:91-96`）；`AllowedCustomers` 只来自 `MSPAllocation(msp_user_id, customer_tenant_id, deassigned_at IS NULL)`（`:104-131`）；`SwitchTenant` 同样只查 allocation（`handlers/auth/service.go:126-135`）。
- **可创建任意多个 provider 租户**：`CreateTenant` 原样落库 type/parent/provider 字段、无类型-父子一致性校验（`service/tenant_service.go:48-52`、`dto/tenant_dto.go:10`）；type 无唯一约束（仅 `code` 唯一）。

**风险清单（R1–R6）**：

| # | 风险 | 证据 | 影响 |
|---|---|---|---|
| R1 | 无唯一性约束：有 tenant 写权限即可建第二个 `msp_provider` 租户，其用户立即被认定 MSP，但无客户关系/归属记录 | `middleware/msp_middleware.go:93` | 多 provider 语义混乱 |
| R2 | **跨 provider 分配**：分配创建不校验"客户所属 provider == 用户所属 provider"；admin 还跳过两侧类型校验 | `service/msp_allocation_service.go:43,46,66-68` | provider A 用户可访问 provider B 客户（访问链全链路只查 allocation） |
| R3 | 父字段是**死元数据**：`parent_tenant_id`/`msp_provider_id` 只写不读（仅 DTO 序列化输出） | `service/tenant_service.go:51-52,195-199` → `dto/mappers.go:414-418` | 不能作为归属校验依据；建客户时无人自动填 provider |
| R4 | 门控与 seed 模式**源不一致**：`main.go:33` 读 env，seeder 读 cfg | `main.go:33`、`pkg/seeder/seeder.go:835-840` | 可能"路由开但按 private 初始化"或反之 |
| R5 | 身份命名双轨：`dto/msp_dto.go:25-30` 的 `msp_*` 常量未被引用；实际值为 `provider_admin/provider_agent/customer_user` | `middleware/msp_rbac.go:18-22` | `msp_role` 取值域无集中校验 |
| R6 | 弱重建：`GetMSPContextFromContext` 只要 Go ctx 有 customer tenant id 即返回 `IsMSP=true`（Role/AllowedCustomers 丢失） | `middleware/msp_context.go:67-76` | 服务层误用做鉴权会绕过校验 |

**处置建议**：

- **P0**：R2（分配时校验客户 provider 归属，admin 不跳过归属校验）+ R4（统一 mode 源）+ R6（标注"仅数据传递、不可鉴权"或移除）；
- **P1**：R1/R3（明确多 provider 策略：`parent_tenant_id` 回填+消费+校验，或显式废弃；provider 唯一性策略）+ R5（`msp_role` 集中校验、清理双轨常量）；
- **与目标架构衔接**：provider 归属应由 `MSPAllocation`/membership **显式承载**（建议 allocation 增加 `provider_tenant_id`，或由 membership 派生），不再依赖"单一 `default` provider 租户"的隐式约定。

> **架构归位**：本节结论已并入[概念模型与架构总纲](./msp-concept-model-and-architecture-canon.md)——`provider_tenant_id` 字段归一、单/多 provider 决策（canon D1）、部署模式单一来源（I12）。

---

## 附录：证据索引

- 权限/RBAC：`itsm-backend/middleware/rbac.go:41-55,99-469,487-504,877-905,1225-1239`、`middleware/smart_permission.go:144-150`、`service/menu_service.go:334-411`、`handlers/common/datascope/datascope.go:19-41,117-143`
- 组织：`ent/schema/{department,team,group,project,cab_member,engineer_skill,on_call_schedule,on_call_shift}.go`、`ent/migrate/schema.go:1975,5724-5725`、`service/{department,team,group}_service.go`
- 工作流/审批：`service/approver/*`、`service/approval_chain_evaluation.go:276-428`、`service/bpmn/*`、`service/ticket_assignment_service.go:485-571`、`service/bpmn_permission_service.go:59-111`、`service/workflow_service.go:74-75`
- 执行器：`service/timer_event_handler.go:213`、`service/timer_scheduler.go:32,201-224,422-423`、`service/timer_store.go:197`、`internal/commandbus/commandbus.go:195,241`、`internal/bootstrap/app.go:1919-1954`
- 通知：`service/notification_service.go`、`notification_preference_service.go`、`notification_outbox.go:26-46`、`notification_delivery_command_handler.go:36-265`、`ticket_notification_service.go:635-724,1066-1074`
- 治理：`internal/schema/tenant_guard.go:57,65-115,164-169`、`config/config.go:199-206,460-464`、`database/rls/migrations/002_pilot_policies.sql:28-61`

## 修订记录

| 版本 | 日期 | 变更 |
|---|---|---|
| v0.1 | 2026-09-29 | 首版：五问判定框架；权限/组织/工作流/执行器/通知五域现状与冲突（❌ 6 项 P0 + 🟡 14 项 P1）；目标集成模型 7 条不变量；分期验收与开放问题（D1–D6） |
| v0.2 | 2026-09-29 | 新增 §11 专项核实：`msp_provider` 为租户类型标签；"单一平台级 provider 租户"为 seed 隐式单例而非约束；`parent_tenant_id/msp_provider_id` 死元数据、分配不校验 provider 归属；风险 R1–R6 与处置建议 |
| v0.3 | 2026-09-30 | **一致性回填**：membership 表名统一 `user_tenant_memberships`（3 处）；§10 开放问题改局部前缀 `INT-D#` 并标记决议（↔canon D3/D4/D6/D7）；§7.2/§9 同步 `data_scope=department` 下线与消息模板租户化 |
| v0.4 | 2026-09-30 | 头部状态与修订记录对齐（v0.1→v0.4）；§7.2 成员关系行指向组织关联子表（实施方案 §4.0-A）；基准 HEAD 重钉 `337558e3` |
