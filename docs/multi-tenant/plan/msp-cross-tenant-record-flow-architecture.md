# 多租户单据流转交互架构（Cross-Tenant Record Flow Architecture）

> 状态：**Draft v0.6（P0 全部落地 + P1/P2 首批实跑；待评审）**｜日期：2026-10-07｜基准：仓库 HEAD `4aba4e4d`（工作树含未提交改动）
> 定位：**L3 架构级交互设计**——把"工单跨租户可见/可操作"泛化为**全部单据类型**（工单/变更/问题/服务请求/审批任务/CMDB 变更单…）的跨租户流转交互模型：参与方与关系、通道与授权链、通用操作矩阵、**端到端流转（建单→可见→处理→回写）**、**路由与交接（自动/手动/流程驱动）**、**信息互通矩阵**、流程/工作流集成、审计与不变量、分期落地。
> 上位：[canon](./msp-concept-model-and-architecture-canon.md)（L1，概念/边界/不变量，冲突时 canon 胜）＞ [工作台方案](./msp-cross-customer-workbench-and-filter-plan.md)（L2，跨客户交互修订源）＞ 本文件（L3，对 L2 的泛化与补充；冲突时 L2 胜并回填本文件）。关联：[集成分析](./msp-integration-with-rbac-org-workflow-analysis.md)、[目标架构](./msp-target-architecture.md)、[实施方案](./msp-implementation-plan.md)、[闭环审查](./msp-business-closure-review-and-refactor-plan.md)。
> 局部编号（引用必须带前缀，登记见 canon 附录 C.2）：`XF1–XF10`（交互规则）、`XFD1–XFD8`（设计决策）、`XFR1–XFR4`（风险）、`XFA1–XFA6`（验收）。
> 读者：架构评审、后端/前端研发、测试 QA、产品决策。

---

## 0. 摘要（TL;DR）

**一句话**：单据永远只属于**一个**租户（owner/resource tenant）；跨租户流转不是"复制单据"，而是**外部参与方在显式授权下、对同一单据的受控读写**——授权 = 租户间关系（Allocation/治理）∩ 目标租户 RBAC ∩ 条目级 `allowedActions`；日常"看+做"走工作台/头通道，**深度配置才切换会话**；流程与工作流始终在 owner 租户内闭环，跨租户只经"模板分发（copy-in）"与"任务投影"。

- **单一归属（XF1）**：所有单据携带 `tenant_id`（owner），跨租户字段只是快照/派生，不构成第二归属；
- **关系显式（XF2）**：provider↔customer 经 `msp_provider_id`（租户归属）+ Allocation（员工↔客户）；平台治理经显式治理通道；不存在隐式可见；
- **五通道（XFD2）**：Home（自有）/ 工作台（跨租户聚合看+做）/ 视图过滤器（只改看）/ 头通道（单请求只读钻取）/ 深度切换（改会话）；平台治理独立于五通道；
- **条目级授权（XF4/XF5）**：服务端计算 `allowedActions[]`，前端只按数组渲染、禁止推断权限；
- **流程闭环（XFD5）**：定义/实例/任务租户内闭环（canon B4/I6）；provider 通过**模板分发（部署为客户租户内的副本）**参与客户流程，运行时**零跨租户引用**；
- **审计四元组（XF8）**：每次跨租户读写审计 `actor_account + membership + target_tenant + source`；冲突探针（`tenant.probe_denied`）与拒绝（`tenant.scope_denied`）落库。
- **端到端（§8）**：customer 建单（owner=客户，快照派生）→ provider 经「关系 + 分配 + 目标租户 RBAC」派生可见（工作台/头通道/任务投影/通知）→ 条目级写回（7 步授权链 + 状态机在客户租户 + 版本锁）→ 客户侧可见 + 通知双投递 + 审计闭环；单据**不搬迁、不复制**。
- **路由与互通（§8.8–8.10）**：单据路由三模式——**自动路由（默认，按服务范围，复用分配规则引擎）** / 手动交接（兜底） / 流程驱动（复杂）；公开评论双向互通、内部备注租户内隔离（MSP 跨租户员工禁写客户内部备注）；附全流程模拟（S1 标准托管 / S2 流程驱动 / S3 反例）与 ITIL 业务适配评估。

---

## 1. 目的、范围与非目标

### 1.1 目的

1. 统一回答"一个租户的单据，另一个租户的人（provider 员工 / 平台治理者）如何看、如何做、如何审计"；
2. 把已落地的**工单**能力（工作台/头通道/切换/快照/双投递通知）抽象为**可复用的架构模式**，指导变更、问题、服务请求、审批任务等单据类型接入；
3. 给出 provider↔customer 在**流程设计器/工作流**上的集成边界与落地路径；
4. 作为跨租户新能力的**评审清单**（接入五件套）与**反例基线**（安全验收）。

### 1.2 范围（单据类型）

| 单据域 | 代表实体 | owner 租户 | 跨租户现状 |
|---|---|---|---|
| 工单 | `tickets` | 客户租户 | ✅ 工作台列表 + 回复/改状态/指派（部分） |
| 变更 | `changes` | 客户/服务商租户 | ❌ 无跨租户面 |
| 问题 | `problems` | 客户/服务商租户 | ❌ |
| 服务请求 | `service_requests` | 客户租户 | 🟡 与工单同源，未单独开放 |
| 审批/BPMN 任务 | `process_instances` / `process_tasks` | 单据 owner 租户 | ❌ 无跨租户面 |
| CMDB/配置项 | `configuration_items` | 租户 | ❌（仅目标租户 RBAC 内） |
| 通知/站内信 | `notifications` | 收件人租户 | ✅（工单双投递，唯一已落地） |

### 1.3 非目标

- 不改变 canon B1–B6 与不变量 I1–I13；不引入"单据多归属"或"多 provider 共享客户"（E6 仍不支持）；
- 不设计计费/结算、跨 provider 转派（E3）；
- 不设计单租户内部的流转（属现有工作流引擎职责）；
- 不替代 [工作台方案](./msp-cross-customer-workbench-and-filter-plan.md)（跨客户交互仍以其为 L2 权威）；本文件只做**泛化 + 单据域扩展 + 流程集成**。

---

## 2. 概念模型

### 2.1 参与方与关系（复用 canon 四层）

```
Platform（平台租户，治理）
   │ 治理通道（显式 + 审计）
Provider（msp_provider 租户，双重定位：自有 ITSM + 跨客户 MSP）
   │ 服务关系：customer.provider_tenant_id（1:N，唯一真相）
   │ 员工关系：Allocation（msp_user ↔ customer_tenant，primary/backup/specialist）
Customer（msp_customer 租户，单据 owner）
```

| 概念 | 定义 | 载体 |
|---|---|---|
| **单据（Record）** | 有状态机、有参与方、可审计的业务对象；**唯一归属一个 owner 租户** | 各业务表 `tenant_id` |
| **Owner/Resource tenant** | 单据归属租户；一切查询/状态机/配置（SLA/流程/通知模板）以它为准 | `tickets.tenant_id` 等 |
| **Home tenant** | 请求者会话 JWT 锁定的租户（登录落点）；Home 面只含自有数据 | JWT `tenant_id` |
| **外部参与方** | owner 租户之外的合法操作者：provider 员工（经 Allocation）、平台治理者（经治理通道） | membership + allocation |
| **条目级授权** | 对**单条**单据的可用动作集合，服务端计算、随响应下发 | `allowedActions[]` |
| **快照（Snapshot）** | 建单时从租户关系派生的冗余字段（`msp_provider_id`/`is_managed_by_msp`/`managed_by_user_id`），用于追溯/通知/统计，**不用于授权** | 业务表字段 |
| **投影（Projection）** | 把 owner 租户的待办/任务/单据按授权"映射"到外部参与方视图（不复制数据） | 工作台/任务面 |

### 2.2 三类跨租户关系（仅此三类，新增需 canon 修订）

| 关系 | 场景 | 授权依据 | 通道 |
|---|---|---|---|
| **服务关系（MSP）** | provider 员工服务已分配客户 | Allocation + 目标租户 RBAC | 五通道（§4） |
| **平台治理** | 平台管理员治理租户/账号/配置 | 平台角色 + 显式目标租户 + 审计 | 治理面（独立，D11） |
| **账号多归属（Membership）** | 同一账号在多个租户有成员身份（P1 目标态） | membership（按目标租户解析） | 深度切换（会话按 membership） |

> 关系不匹配的组合一律 fail-closed：客户账号访问 `/msp/*` → 403；provider 未分配客户 → 403 `MSP_ALLOCATION_REQUIRED`；跨 provider 访问 → 403；暂停/过期租户写 → 403。

### 2.3 现状基线（2026-10-07 实测，作为本文证据）

| 事实 | 证据 |
|---|---|
| 客户租户 11 的工单 #223 在 provider(10) Home 面 `/tickets` 不可见（设计如此，双工作面） | `middleware/msp_tenant_resolver.go:29-57`；`repository/ticket/repository_impl.go:494-496` |
| provider 无 Allocation 时：工作台空、`/msp/customers/11/tickets` 403 `MSP_ALLOCATION_REQUIRED` | 实测（`.dev/msp-integration-probe*.ps1`）；`pkg/mspguard/mspguard.go:111-147` |
| 创建 Allocation 后：工作台出现 #223，`allowedActions`=reply/status/assign 全开；summary `open=1` | 实测 |
| #223 快照为空（`isManagedByMSP=false`、`mspProviderID=null`，建单早于归属关系）；指派会幂等回填 | `service/msp_snapshot.go:12-28`；`service/ticket_service.go:2634-2643` |
| 工作台**无详情端点**：无描述/评论/时间线/SLA 明细/CI/附件；前端工作台不能打开详情 | `service/msp_workbench.go:225-244`；`router/msp_routes.go:16-60` |
| 流程定义/绑定/实例**全部租户内**；provider 无任何 bpmn/process 跨租户端点；`workflow_templates.is_public` 为死字段、无 import 端点 | `ent/schema/process_definition.go:81-83,114`；`handlers/bpmn/workflow.go:264-297`；`service/bpmn_workflow_template_catalog.go:36-37` |
| 工单流程端点对 `admin`/`provider_tenant_admin` 403：角色有 `bpmn:*` 但无 `workflow:*`，路由仍要求 `workflow:read/write` | `router/ticket_routes.go:229-233`；`internal/authz/roles.go:232+` vs `:455`；实测 `/auth/me` |

---

## 3. 交互规则（XF1–XF10）

> 规则是 canon B/I 系列在"单据流转"域的展开；与 canon 冲突时 canon 胜。

| # | 规则 | 说明与反例 |
|---|---|---|
| **XF1** | **单据单一归属**：每条单据只属于一个 owner 租户；跨租户字段（快照）不构成第二归属 | 反例：在 provider 租户建"客户工单副本"——禁止（会产生双写/双状态机） |
| **XF2** | **跨租户可见性来自显式关系**：服务关系（Allocation）/治理/多归属；无关系 → 不可见（403/404，且不泄露存在性） | 反例：`GET /tickets` 在 provider Home 面带 `X-Customer-Tenant-ID` 期望看到客户工单——该 header 只在 `/msp/*` 生效 |
| **XF3** | **读通道与写通道分离**：聚合读走工作台/头通道；**写操作一律条目级端点**（携带目标租户声明），不依赖会话切换 | 反例：让用户"先切到客户再操作"作为日常路径——违反 I9/WB2，易漏单 |
| **XF4** | **条目级授权链（三权分立）**：`可用 = 关系（Allocation/治理） ∩ 目标租户 RBAC ∩ 资源状态（active/终态/版本）`；授权实时计算，快照不参与 | 反例：用 `is_managed_by_msp` 决定可见性——禁止（历史快照 ≠ 当前授权） |
| **XF5** | **`allowedActions[]` 是唯一 UI 契约**：每条单据随响应下发 `{action, allowed, reasonCode, reasonText}`；前端只渲染不推断；写操作服务端二次校验 | 反例：前端按"角色名"推断能否回复 |
| **XF6** | **状态机在 owner 租户内**：跨租户参与者只能触发被授权的状态迁移；迁移规则、SLA、审批链以 owner 租户配置为准；跨租户不得旁路状态机直接改字段 | 反例：provider 直接 `UPDATE status` 绕过客户流程 |
| **XF7** | **通知双投递 + 可定位**：单据事件同时投递 owner 侧参与方与外部参与方（provider 被指派人/provider 管理员）；通知深链必须能定位到该单据（或明确降级到工作台筛选） | 现状：工单已双投递但深链固定 `/msp/workbench`；详情端点缺失时无法直达 |
| **XF8** | **审计四元组**：`actor_account + membership + target_tenant + source(home/workbench/header/switch/platform)`；拒绝与冲突同样落库（`tenant.scope_denied` / `tenant.probe_denied`） | 反例：跨租户只读不审计 |
| **XF9** | **快照与归属变更**：建单时派生快照；客户改挂 provider 时**历史单据保留原快照**（E2）；新增归属关系不追溯改写历史，允许幂等回填（显式操作/迁移） | 现状：#223 快照为空，需回填或按需派生 |
| **XF10** | **接入五件套（新单据类型准入清单）**：① schema 带 `tenant_id` + 快照字段；② 授权走统一入口（`mspguard`）；③ 跨租户端点（列表/详情/条目级写）按 §5 矩阵；④ 审计事件登记；⑤ 反例测试（未分配/暂停/跨 provider/客户账号） | 缺任一 → 不得对外宣称"支持跨租户" |

---

## 4. 通道模型（五通道 + 治理面）

### 4.1 通道定义与选择准则

| # | 通道 | 会话变化 | 数据范围 | 典型用途 | 规则 |
|---|---|---|---|---|---|
| ① | **Home 面** `/api/v1/*` | 不变 | 本租户（JWT 锁定） | 自有 ITSM 全功能 | 客户工单在此**不可见**（设计如此） |
| ② | **跨租户工作台** `/api/v1/msp/workbench/*` | 不变 | `AllowedCustomers` 聚合（服务端集合，不接受客户端传入） | 日常看+做：列表/摘要/回复/改状态/指派/批量 | WB1/WB5；逐租户查询 + 内存合并；`tenant_source=workbench` |
| ③ | **视图过滤器**（顶栏 CustomerFilter） | 不变 | ②的子集 | 收窄视图/计数 | WB1：只改"看什么"；可选集合=有效 Allocation |
| ④ | **头通道** `X-Customer-Tenant-ID` | 不变 | 单请求、只读 | 单请求钻取（如客户工单列表/详情） | WB3：仅读；必须命中 `AllowedCustomers`；仅 `/msp/*` 解析 |
| ⑤ | **深度切换** `POST /auth/switch-tenant` | **变**（重签 JWT、撤销旧 refresh） | 目标客户租户 | 深度配置：用户/组织/流程设计/多步连续操作 | WB4：仅深度操作；审计 `source=switch` |
| ⑥ | **平台治理面** | 独立 | 显式目标租户 | 平台级治理（租户/账号/配置） | D11：平台不可见 MSP 工作台；治理操作必须"先选目标租户 + 审计" |

**选择决策树**（对 provider 员工）：

```
要做什么？
├─ 看总览/多条/日常处理（回复、状态、指派） → ② 工作台（+③ 过滤器）
├─ 看单条详情（只读、不切会话）             → ④ 头通道（或工作台详情端点，P1 补齐）
├─ 配置类/多步连续（流程设计、用户、组织）    → ⑤ 深度切换
└─ 平台治理（跨租户账号、租户配置）           → ⑥ 平台治理面（独立入口）
```

### 4.2 通道不变量

1. **同一单据经任意通道读到的状态必须一致**（同一 owner 租户数据源；快照字段展示需标注"派生"）；
2. **写操作不依赖通道**：②/④/⑤ 最终都调用同一套条目级授权与状态机（④ 只读，写走条目级端点）；
3. **聚合通道只来自服务端集合**：`customerTenantIds` 参数只作**筛选**，越界即 403，禁止客户端扩大范围；
4. **会话切换是重操作**：切换后 Home 面变为目标租户（可逆）；切换必须审计并撤销旧 refresh token；
5. **fail-closed**：任一关系校验失败 → 403/404 + 审计；404 用于"防枚举"（未授权资源不暴露存在性）。

---

## 5. 通用操作矩阵（单据 × 通道）

> 行=操作，列=通道；"✅ 现状 / 🟡 部分 / ❌ 缺口"标注当前实现（2026-10-07）。所有写操作授权=XF4；UI 契约=XF5。

| 操作 | ② 工作台 | ③ 过滤器 | ④ 头通道 | ⑤ 切换 | ⑥ 治理面 | 现状与缺口 |
|---|---|---|---|---|---|---|
| 聚合列表 | ✅（工单） | ✅ | — | — | 🟡（租户列表） | ❌ 变更/问题/服务请求未接入；游标分页已备 |
| 单条详情（描述/字段） | ❌ | — | 🟡（客户工单列表有全字段） | ✅ | 🟡 | **缺 `GET /msp/tickets/:id`**；前端无法打开详情 |
| 评论/时间线（只读） | ❌ | — | ❌ | ✅ | ❌ | 缺口 |
| 回复/评论（写） | ✅（工单） | — | ❌（只读） | ✅ | ❌ | 语义=写入客户可见评论 |
| 状态迁移 | ✅（工单） | — | — | ✅ | ❌ | 受 owner 租户状态机与终态检查约束 |
| 指派/转派 | 🟡（仅指派给自己） | — | — | ✅ | ❌ | 缺口：不能指派其他技术员；批量同 |
| SLA 明细 | 🟡（仅 deadline + 汇总） | — | ❌ | ✅ | ❌ | 缺口 |
| 附件读写 | ❌ | — | ❌ | ✅ | ❌ | 缺口 |
| 配置项/CMDB 关联 | ❌ | — | ❌ | ✅ | ❌ | 缺口 |
| 字段更新/批量 | 🟡（批量仅 reply/status/assign，≤100） | — | — | ✅ | ❌ | 批量护栏：低危白名单 + 逐条授权/审计/限流 |
| 导出 | ❌ | — | ❌ | 🟡 | 🟡 | 缺口 |
| 审计可见 | ✅（审计看板） | — | — | ✅ | ✅ | `actor+membership+target+source` 已具备 |

**API 命名规范（新增能力必须遵守）**：

- 聚合读：`GET /api/v1/msp/workbench/{resources}`（筛选参数 `customerTenantIds`，服务端集合约束）；
- 单条只读钻取：优先头通道（`X-Customer-Tenant-ID` + 租户域既有端点）；聚合面新增 `GET /api/v1/msp/{resources}/:id`（P1）；
- 条目级写：`POST /api/v1/msp/{resources}/:id/{action}`，请求体携带 `customerTenantId`（资源租户声明），服务端校验 `声明租户 == 资源租户`，否则 400 `RESOURCE_TENANT_MISMATCH`；
- 响应统一携带 `allowedActions[]` 与 `customerTenantId`（或 `targetTenantId`）；
- 错误码沿用 `MSP_ALLOCATION_REQUIRED` / `CUSTOMER_INACTIVE` / `CUSTOMER_TENANT_NOT_FOUND` / `RESOURCE_TENANT_MISMATCH` / `TENANT_MISMATCH_REJECTED`。

---

## 6. 单据类型映射与接入差距

| 单据类型 | owner 字段 | 关系派生 | 现有跨租户能力 | 需补齐（按 XF10 五件套） | 建议优先级 |
|---|---|---|---|---|---|
| 工单 `tickets` | `tenant_id` ✅ | 快照 ✅（建单/指派） | 列表+回复+状态+指派（部分） | 详情/评论/时间线/SLA/CI/附件；指派他人；批量扩展 | **P1** |
| 变更 `changes` | `tenant_id` ✅ | ❌ 无快照 | ❌ | 快照派生 + 工作台接入 + 条目级写 + 审计 | P2 |
| 问题 `problems` | `tenant_id` ✅ | ❌ | ❌ | 同上 | P2 |
| 服务请求 `service_requests` | `tenant_id` ✅ | 与工单同源 | 🟡 | 明确聚合口径（是否并入工单面） | P2 |
| 审批/BPMN 任务 | `tenant_id` ✅ | ❌ | ❌ | 任务投影（§7.3）+ 跨租户待办面 | P2 |
| CMDB/CI | `tenant_id` ✅ | ❌ | ❌ | 只读投影（详情页嵌入） | P3 |
| 通知 | 收件人租户 ✅ | 工单双投递 ✅ | ✅（工单） | 泛化到全部单据类型；深链直达详情 | P1 |

**接入五件套模板**（新类型复制此清单，缺一不可）：

1. **Schema**：`tenant_id`（Positive，进 tenant guard 白名单检查）+ 快照字段（如适用）+ 索引 `(tenant_id, status)`；
2. **授权**：全部跨租户访问经 `pkg/mspguard` 统一入口（禁止旁路）；服务层二次校验 `声明租户 == 资源租户`；
3. **端点**：按 §5 矩阵提供聚合读/单条读/条目级写（含 `allowedActions` 计算）；
4. **审计**：跨租户读写 + 拒绝事件登记（事件名规范：`{domain}.{action}` + `target_tenant` + `source`）；
5. **测试**：正向（已分配客户）+ 反例（未分配/暂停/过期/跨 provider/客户账号调 MSP）+ 通道一致性（同单据经 ②④⑤ 读一致）。

---

## 7. 流程与工作流集成（流程设计器 × 多租户）

### 7.1 边界（现状事实）

- 流程**全部租户内闭环**：`process_definition (tenant_id,key,version)`、`process_deployment`、`process_instance`、`process_binding`、`workflow_template`、`ticket_type.workflow_definition_key` 均强制 `tenant_id`，且 tenant guard 无豁免；触发时定义必须属本租户且 active（`service/bpmn_process_trigger_service.go:62-76`）；
- 工单类型→流程是**租户内绑定**；解析链：显式 key > TicketType 绑定 > ProcessBinding > `ticket_general_flow`（高/紧急升级 `ticket_urgent_flow`）；
- provider **无任何** bpmn/process 跨租户端点；请求体/查询伪造 tenant 会被 ctx 覆盖或拒绝；`X-Tenant-ID` 被忽略、冲突 401；
- `workflow_templates.is_public` 是**死字段**（无跨租户查询使用）；导出有、**导入缺失**；克隆仅同租户。

### 7.2 集成模式 A：模板分发（copy-in，推荐）

> 原则：**provider 设计、客户持有副本、运行时零跨租户引用**（保持 B4/I6）。

```
provider 设计器 ──发布"分发模板"──▶ 分发服务（新端点）
                                      │ 校验：Allocation + 客户租户 bpmn:write + 客户 active
                                      ▼
                            客户租户内 ProcessDefinition（副本，记录来源）
                                      │
                            客户绑定 TicketType/ProcessBinding（租户内校验不变）
                                      ▼
                            客户工单触发（租户内执行，tenantctx=客户）
```

- **新增端点（建议）**：`POST /api/v1/msp/customers/:id/workflow-templates/{key}/deploy`（分发部署）、`GET /api/v1/msp/customers/:id/workflow-templates`（分发状态/版本）；
- **版本与来源追踪**：客户侧副本记录 `source_provider_tenant_id + source_template_key + source_version + deployed_at`（复用 `workflow_templates` 元数据或新增关联表）；
- **漂移治理**：provider 更新模板 → 生成"可升级"提示，客户管理员确认后升级（不强制覆盖客户本地修改）；
- **权限**：provider 侧要求 `msp:write` + allocation；客户侧要求目标租户 `bpmn:write`（条目级授权链）；
- **验收**：分发→客户部署→绑定→工单触发→实例运行全程租户内；反例：未分配 provider 分发 403；客户无 `bpmn:write` 403。

### 7.3 集成模式 B：跨租户任务投影（客户流程中的 provider 任务）

> 场景：客户流程出现"需服务商处理/审批"的节点（升级、会签、外部处理）。

- **任务归属**：任务行仍属客户租户（owner）；provider 侧通过**投影**看到"待我处理"（不复制任务行）；
- **授权**：任务投影 = Allocation ∩ 客户租户 RBAC（`task:read/update`）∩ 任务分配规则（指派给 provider 角色/员工，经分配校验）；
- **写回**：处理结果写回客户租户任务/实例（tenantctx=客户），审计 `source=workbench`；
- **现状缺口**：无任何跨租户任务面；BPMN 任务端点全部租户内过滤；需新增 MSP 任务聚合端点（P2）。

### 7.4 集成模式 C：深度切换代管（兜底，不推荐日常）

- 用于"必须进入客户租户上下文"的配置类操作（流程设计器调整客户流程、组织/用户配置）；
- 走 `switch-tenant`（重签 JWT + 撤销旧 refresh + 审计 `source=switch`），操作完成后切回；
- **禁止**把切换作为日常处理路径（违反 I9/WB2；漏单与误操作风险，见 [工作台方案](./msp-cross-customer-workbench-and-filter-plan.md) §7 REV-1/2）。

### 7.5 流程侧不变量

1. 运行时（触发/执行/任务/定时器）**永不跨租户引用**定义或配置；
2. 跨租户只发生在"设计期分发"（copy-in）与"运行期任务投影"（读/写回 owner 租户）；
3. 客户改挂 provider 后：历史实例留在原租户；新分发需新关系授权；模板来源字段保留（追溯）；
4. 一切跨租户流程操作审计四元组（XF8）。

---

## 8. 端到端流转设计（customer 建单 → provider 处理 → 状态回写）

> 本章把 §3 规则落到一条可实现的链路上，回答四个问题：单据如何"流转到" provider（8.2 读路径）、provider 处理后状态如何回写（8.3 写路径）、用户/角色/单据/流程四维如何对齐（8.1/8.4）、流程节点如何跨租户（8.5）。每节附**现状 vs 目标**，缺口映射到 §13 分期。

### 8.1 四维对齐总表（用户 / 角色 / 单据 / 流程）

| 维度 | 归属与载体 | 跨租户参与方式 | 现状 |
|---|---|---|---|
| **用户（账号）** | `users`（username/email 全局唯一）；home 租户=创建租户 | provider 员工账号属 provider 租户；客户用户账号属客户租户；P1 membership（账号↔租户 N:N）后按目标租户解析身份 | ✅ 现状 `users.tenant_id` + `user_roles`；P1 目标态 membership 单源 |
| **租户内角色（RBAC）** | `roles/permissions/role_permissions`（均带 `tenant_id`） | 权限**只在目标租户内计算**（B5）：provider 员工对客户单据的动作按**客户租户** RBAC 校验（MSP 角色映射 `msp_manager/msp_tech/msp_viewer` → 客户租户 `msp_ticket:*` 授权） | ✅ `HasResourcePermission(ctx, …, 目标租户)` 已实现 |
| **MSP 角色** | `users.msp_role`（5 值词表，D10） | 决定 `/msp/*` 能力与工作台可写性（`provider_admin`/`provider_agent`…） | ✅ |
| **分配角色（Allocation.role）** | `msp_allocations.role`：`primary`/`backup`/`specialist` | 服务关系语义：主责/备份/专项；目标态用于**通知首选与指派候选解析**（provider 任务/升级） | 🟡 仅存储与展示，未参与通知/指派 |
| **单据（Record）** | 业务表 `tenant_id`（owner=客户租户）；快照字段 | **不搬迁、不复制**；provider 经工作台/头通道/任务投影派生访问 | 🟡 工单部分落地 |
| **流程（BPMN/审批）** | `process_definition/deployment/instance/task/binding`（owner=客户租户，tenant guard 无豁免） | 模板分发（copy-in，§7.2）+ 任务投影（§7.3）；运行时零跨租户引用 | ❌ 无跨租户面 |

### 8.2 读路径："流转到 provider" = 可见性派生（不是数据搬迁）

**步骤表**（客户建单 → provider 侧可见）：

| # | 步骤 | 执行方 | 规则/依据 | 现状 |
|---|---|---|---|---|
| 1 | 客户员工建单 | 客户租户 API | owner=`tickets.tenant_id`=客户；解析 `customer.msp_provider_id` → 写快照 `is_managed_by_msp/msp_provider_id`；启动客户流程（`tenantctx=客户`） | ✅（`service/msp_snapshot.go`、`ticket_core_service.go:125-154`） |
| 2 | 关系校验 | provider 侧请求时 | 客户 `msp_provider_id` == 该 provider（R2） | ✅（`pkg/mspguard`） |
| 3 | 分配校验 | 同上 | 员工持有该客户**有效 allocation**（`deassigned_at IS NULL`） | ✅；❌ 首管默认无分配（P0） |
| 4 | 目标租户 RBAC | 同上 | 按**客户租户**角色解析 `msp_ticket:read/write` 等 | ✅ |
| 5 | 呈现（四入口） | provider 员工 | ① 工作台聚合列表（主）→ ② 单条详情（P1）→ ③ 任务投影（P2）→ ④ 通知深链 | 🟡 ①✅ ②③❌ |

**时序（目标态）**：

```
客户员工        客户租户 API                关系/快照               provider 员工        provider API(工作台)        通知/审计
  │ 建单 ───────▶ │
  │              │ 派生快照(msp_provider_id) + 启动客户流程
  │              │ 通知：客户侧(requester/assignee) + provider侧(被指派人/管理员*)
  │◀─ 建单成功 ──│
  │                                       工作台列表(AllowedCustomers ∩ active)
  │                                                   │◀──────── 列表 + allowedActions
  │                                       指派(assignee∈provider ∧ 持有allocation) → 写 assignee_id/managed_by_user_id + 快照回填
  │              │◀── 状态/评论写回(tenantctx=客户, 状态机+版本) ──│
  │◀─ 客户侧可见/通知 ─│
```

> *provider 侧通知收件人 = 托管处理人/工单 assignee（须属 provider 租户且 active）+ provider 管理员；未指派且无快照时按"客户内闭环"处理（不投 provider 侧）。

**关键澄清**：`GET /api/v1/tickets`（provider Home 面）永远看不到客户工单（JWT 锁定 provider 租户 + repo 硬过滤 `tenant_id`）；`X-Customer-Tenant-ID` 仅在 `/msp/*` 被解析。这不是缺陷，而是双工作面（canon §7.3）的设计结果。

### 8.3 写路径：provider 处理 → 状态回写（条目级授权链，代码级）

**端点**：`POST /api/v1/msp/tickets/:id/{reply|status|assign}`，请求体携带 `customerTenantId`（资源租户声明）+ 载荷（`content` / `status`）；批量 `POST /api/v1/msp/workbench/batch`（仅 reply/status/assign，≤100 条）。

**授权链（7 步，`service/msp_workbench.go:590-622`）**：

| # | 校验 | 失败结果 |
|---|---|---|
| 1 | 以**声明租户**为 RLS GUC 读取工单（enforce 下跨租户探测 fail-closed） | 404 `CUSTOMER_TENANT_NOT_FOUND`（防枚举） |
| 2 | 资源租户一致性：`ticket.tenant_id == 声明租户` | 400 `RESOURCE_TENANT_MISMATCH` |
| 3 | Allocation：声明租户 ∈ `AllowedCustomers` | 403 `MSP_ALLOCATION_REQUIRED` |
| 4 | 客户租户 active 且未过期 | 403 `CUSTOMER_INACTIVE`（仅可查看） |
| 5 | 资源状态：`resolved/closed/cancelled` 为终态，拒绝写 | 403 `ACTION_NOT_ALLOWED` |
| 6 | 目标租户 RBAC：MSP 角色映射后校验客户租户 `msp_ticket:write` | 403 `ACTION_NOT_ALLOWED` |
| 7 | 状态机：`common.IsValidTicketStatusTransition(current, new)`（单一事实来源）+ 版本/并发 | 400 非法迁移 / 409 版本冲突 |

**状态机（工单，`common/constants.go:136-158`）**：

```
new         → open / assigned / in_progress / cancelled
assigned    → in_progress / pending / resolved / cancelled
open        → in_progress / pending / resolved / cancelled
in_progress → resolved / pending / cancelled
pending     → in_progress / resolved / open / cancelled
resolved    → closed / in_progress / open
closed      → 终态（禁止转换）
cancelled   → 终态（禁止转换）
```

**回写后事件**：`UpdateTicketStatus(tenantctx=客户, operator=provider 员工)` → `NotifyTicketStatusChanged`（客户侧 + provider 侧双投递）→ 审计 `workbench.action`（`source=workbench` + `target_tenant_id`）。

**动作语义**：

- `reply`：写**客户可见评论**（客户租户内 `CreateTicketComment`）；
- `status`：状态迁移（上表）+ 通知双投递；
- `assign`：指派给**当前 MSP 用户自己**（`assignee_id=提交人` + `managed_by_user_id` + 快照幂等回填）；不支持指派其他技术员（缺口）。

**已发现契约不一致**：`allowedActions` 对终态工单仍标记 `reply=true`（`msp_workbench.go:438`），但写路径第 5 步对终态**一律拒绝**（含 reply）——前端会渲染可点但必失败的回复按钮。修复：二者取其一（建议终态禁回复，与写路径一致）。

### 8.4 角色-动作矩阵（谁、在什么关系下、能做什么）

| 动作 | 执行者 | 关系要求 | 目标租户 RBAC（示例） | 现状 |
|---|---|---|---|---|
| 建单 | 客户员工 | — | `ticket:create`（客户租户） | ✅ |
| 工作台查看 | provider 员工 | allocation + 客户 active | `msp_ticket:read` | ✅ |
| 回复 | provider 员工 | 同上 | `msp_ticket:write` | ✅ |
| 改状态 | provider 员工 | 同上 | `msp_ticket:write` + 状态机 | ✅ |
| 指派（自己） | provider 员工 | 同上 | `msp_ticket:write` | ✅ |
| 指派（他人）/转派 | provider 员工 | 同上 + 目标 assignee 属 provider 且有分配 | `msp_ticket:write` + `ticket:assign` | ❌ |
| 流程分发 | provider 管理员 | allocation + 客户 active | provider `bpmn:write` + 客户 `bpmn:write` | ❌（P2） |
| 客户流程任务处理 | provider 员工 | allocation + 任务分配规则 | 客户租户 `task:update` | ❌（P2） |
| 治理（重置密码/启停） | 平台管理员 | 治理通道（显式目标租户） | 平台角色 | ✅（TUM 方案） |

### 8.5 流程节点级设计（provider 任务如何进入客户流程）

1. **任务模型**：任务行属**客户租户**；节点标记"服务商处理"（目标态新增任务类型/分配策略）；`assignee` 解析 = 客户租户流程上下文内、经 allocation 登记数据解析 `primary/backup/specialist` → 具体 provider 员工（**只读**跨租户登记表，运行时零跨租户引用）；
2. **投影面**：`GET /api/v1/msp/workbench/tasks`（建议新增，P2）——按 allocation ∩ 客户租户 `task:read` 聚合"待我处理"；条目同样返回 `allowedActions[]`；
3. **写回**：认领/完成/加签 → 在客户租户 `tenantctx` 内执行，RBAC=客户租户 `task:update`，审计 `source=workbench`；
4. **超时/升级**：按**客户租户** SLA/升级规则（E1）；provider 侧仅 overlay（提醒/看板）；
5. **模板分发**：客户流程中"服务商处理"节点的流程定义由 provider 经 §7.2 分发（copy-in）或客户自建；二者都保持租户内闭环。

### 8.6 通知矩阵（目标态）

| 事件 | 客户侧收件人 | provider 侧收件人 | 现状 |
|---|---|---|---|
| 建单 | requester + 客户 assignee | 被指派人/管理员（仅当已指派或有快照） | 🟡 |
| 指派 | requester + 客户 assignee | 被指派 MSP 员工 + provider 管理员 | 🟡（指派限自己） |
| 回复 | requester + 客户 assignee | 被指派人（若为 provider 员工） | ✅（工单） |
| 状态变更 | requester + 客户 assignee | 被指派人 + provider 管理员 | ✅（工单） |
| 任务待办 | —（客户流程内） | 分配解析出的 provider 员工 | ❌（P2） |
| 超时/升级 | 客户侧规则收件人 | provider 管理员（overlay） | 🟡 |

### 8.7 端到端差距与承接（汇总）

| 环节 | 现状 | 缺口 | 承接 |
|---|---|---|---|
| 建单快照 | ✅ 建单派生 | 历史工单无快照（#223） | ✅ P0 已交付：`scripts/msp/backfill-ticket-msp-snapshot.sql` + `cmd/backfill_ticket_snapshot`（联调 dry-run 0 候选） |
| 可见性 | ✅ 工作台列表 | 首管无默认分配 | ✅ P0 已实施：provision/bootstrap 钩子 + `cmd/backfill_msp_allocations`（bootstrap ∪ admin/super_admin 角色） |
| 单条详情 | ✅ 工单只读 | 评论/时间线/CI/附件仍缺 | ✅ P1 已交付 `GET /msp/tickets/:id` + 前端抽屉（评论/时间线留 P2） |
| 回复/状态 | ✅ | ~~终态 reply 契约不一致~~ | ✅ P0 已修复（allowedActions 与写路径同口径 + 回归用例） |
| 类型级路由 | ❌ | 无类型级自动路由 | ✅ P0 已实施：`msp_route_enabled` + `msp_routed_at` + `routedOnly`（联调 #224 实跑通过） |
| 指派 | 🟡 仅自己 | 指派他人/转派 | P1 |
| 流程分发 | ❌ | 无分发端点/来源追踪 | P2（§7.2） |
| 任务投影 | ❌ | 无跨租户任务面 | P2（§7.3） |
| 通知深链 | ✅ | provider 侧 `Notification.action_url` = `/msp/workbench?ticket=<id>&customerTenantId=<tid>`，铃铛点击直达 + 工作台自动打开详情；客户侧既有 `/tickets/:id` | ✅ P2 2026-10-07 |

### 8.8 单据路由与交接设计（自动 / 手动 / 流程驱动）

**问题**：客户建单后，按什么机制"到"provider？三种模式并存，按场景选择（决策 XFD7）：

| 模式 | 触发点 | 适用场景 | 现状 |
|---|---|---|---|
| **A 自动路由（默认）** | 建单时按**服务范围**判定 → 派生 provider → 生成 provider 侧"待处理"投影 + 通知 | 标准托管工单（客户报障即服务商受理） | ❌ 未实现（现有快照派生 ≠ 路由） |
| **B 手动交接（兜底）** | ① 客户侧"转服务商"动作；② provider 侧"认领/指派他人" | 范围外例外、客户主动求助、值班调度 | 🟡 provider 仅能指派自己；客户侧无动作 |
| **C 流程驱动（复杂）** | 客户 BPMN 节点"服务商处理"（任务投影） | 需审批/多步/多角色的服务流程 | ❌（§8.5 / P2） |

**服务范围（决定"哪些单据送到 provider"）——三级模型**：

1. **租户级**：客户租户托管开关（合同覆盖全租户）——现状无字段；
2. **类型级（推荐）**：工单类型 `msp_route_enabled`（或复用分配规则引擎新增 provider 动作）——粒度最实用；
3. **工单级**：客户侧"转服务商"手动触发（模式 B）。

> **未命中服务范围的单据 = 客户内闭环**（不投 provider、不出现在工作台），避免 provider 被非托管噪音淹没。

**自动路由实现建议（复用现有引擎，避免新造轮子）**：

- 现有建单钩子已支持自动分配：`TicketType.auto_assign_enabled` + `assignment_rule_id` + `ticket_assignment_rules`，建单时 `AssigneeID == nil` 触发 `AutoAssign/AutoAssignWithRule`（`service/ticket_service.go:473-484`）；
- 扩展**规则动作类型**：`provider_queue`（送 provider 待处理池）/ `provider_user`（按 allocation 解析具体员工）——解析顺序 `primary → backup → specialist`（分配角色语义首次被消费）；P2 可加负载均衡；
- 路由产出：**只产生投影 + 通知，不预写客户侧 `assignee`**（保持"客户工单的 assignee 由实际接单人落定"）；
- 幂等：同一工单单一 provider 归属；重复触发（自动+手动）幂等；
- **工作台可见性口径**：现状工作台列出已分配客户租户的**全部**工单；目标态增加"托管范围"过滤（默认只看路由过的单据，提供"全部"开关），与路由范围同源。

**手动交接（模式 B）**：客户侧"转服务商"= 写托管标记 + 快照 + 通知 provider（幂等，审计 `source=home`）；provider 侧"认领"= 现有 assign（自己）扩展为"认领/指派他人"（目标 assignee 须属该 provider 且持有该客户 allocation）。

> **落地说明（v0.4 MVP）**：类型级已直接以 `ticket_types.msp_route_enabled` 实现（建单标记 `msp_routed_at` + 工作台 `routedOnly` 过滤）；`provider_queue`/`provider_user` 规则动作、自动指派解析与"托管范围默认过滤"留待 P1/P2（见 §13）。

**优先级**：流程节点（C）> 手动（B）> 自动（A）；三者最终都收敛到同一状态机与授权链（§8.3）。

### 8.9 跨租户信息互通矩阵（评论 / 字段 / 附件 / 身份）

**评论模型（现状字段已具备）**：`ticket_comments.is_internal`（默认 false）+ `tenant_id`；公开评论 = 双方可见，内部备注 = 租户内可见。

**已核实的隐私设计（保持）**：`canManageInternalComments` 仅租户内 `super_admin/admin/manager/agent/technician/security` 可写内部备注；**跨租户 MSP 员工明确视为非特权**（"仅可发公开评论，禁止 internal note"）——即 provider **不能**在客户工单写内部备注，客户内部备注 provider 也读不到（设计如此，非缺陷）。

| 协作对象 | 客户侧可见 | provider 侧可见 | 规则/现状 |
|---|---|---|---|
| 公开评论（`is_internal=false`） | ✅（含 provider 写的） | ✅（目标态；**读通道 P1**） | MSP 回复已写客户可见公开评论（`Reply`）；客户公开评论 provider 侧读取待 P1 |
| 客户内部备注 | ✅（客户 support staff） | ❌ | 设计隔离（现状即如此） |
| provider 内部备注 | ❌ | ✅ | **目标态新增**：存 provider 侧（不写客户租户）；或评论加 `visibility=provider` 扩展（待评审） |
| 评论附件 | 随评论可见性 | 随评论可见性 | 现状：附件校验限同租户宿主（跨租户读通道 P1 同批） |
| @提及 | 仅本租户成员 | 仅本租户成员 | 不跨租户 @（避免身份枚举） |
| 时间线/状态变更 | ✅ | ✅ | provider 侧详情 P1 |
| 满意度/评价 | ✅（客户填写） | 🟡 汇总（overlay） | 目标态 |

| 字段级互通 | 共享（双向/按需） | 不共享（硬边界） |
|---|---|---|
| 单据核心 | 编号、标题、描述、状态、优先级、类型、创建/更新时间、公开评论、SLA 期限（按客户租户规则计算的结果） | 客户租户内部字段（成本、内部标签、标记为内部的 `custom_fields`）；provider 内部字段（工作量、成本、内部备注） |
| 参与人 | 处理人姓名 + 租户类型标签（"服务商"）；requester/assignee 姓名 | 邮箱/手机等敏感身份默认不共享（按需脱敏展示，评审后定） |
| 配置 | —（仅"生效结果"共享，如 SLA 期限） | 双方租户配置：SLA 模板、流程定义、通知模板、角色/成员、分配表、审计原始日志 |
| 其他数据 | — | provider 的其他客户数据；客户租户其他单据；平台治理数据 |

**决策（XFD8）**：公开评论双向互通（同一评论流）；内部备注租户内隔离（MSP 禁写客户内部备注，保持现状）；provider 内部备注存 provider 侧。

### 8.10 端到端模拟与业务适配评估

**S1 · 标准托管工单（自动路由，主链路）**

| # | 角色 | 动作 | 系统行为 | 预期/证据 |
|---|---|---|---|---|
| 1 | 客户员工 | 建单（类型命中服务范围） | 快照派生 + 自动路由（投影 + 通知 provider） | 工单 `new`；provider 工作台出现"待处理"；**现状**：无自动路由（需手工分配） |
| 2 | provider 员工 | 打开工作台 | Allocation ∩ 客户 active ∩ 目标租户 RBAC | 列表可见 + `allowedActions`（现状 ✅） |
| 3 | provider 员工 | 认领（assign 自己） | `assignee=自己` + `managed_by_user_id` + 快照回填 | 客户侧处理人显示服务商员工（现状 ✅） |
| 4 | provider 员工 | 回复 | 公开评论写客户租户 | 客户详情可见（作者带"服务商"标签）（现状 ✅ 写侧） |
| 5 | provider 员工 | 改状态 `in_progress` | 状态机 + 通知双投递 | 客户侧状态同步 + 通知（现状 ✅） |
| 6 | provider 员工 | 改状态 `resolved` | 同上 | 客户侧 `resolved`（现状 ✅） |
| 7 | 客户员工 | 关闭 `closed` | 状态机 `resolved→closed` + 审计 | 关单（现状 ✅）；满意度评价（目标态 ❌） |

**S2 · 流程驱动（服务商处理节点）**

| # | 角色 | 动作 | 系统行为 | 现状 |
|---|---|---|---|---|
| 1 | 客户员工 | 建单 | 客户流程触发（含"服务商处理"节点） | ✅（客户流程）；❌ 无 provider 节点类型 |
| 2 | 系统 | 流程到达 provider 节点 | 任务投影（assignee 按 allocation 解析 `primary`） | ❌（P2） |
| 3 | provider 员工 | 处理并提交 | 写回客户实例（`tenantctx=客户` + `task:update`） | ❌（P2） |
| 4 | 客户 | 审批/验收 | 流程推进 → 单据 resolved/closed | 🟡 |

**S3 · 反例与边界**

| 场景 | 预期 | 现状 |
|---|---|---|
| 未托管类型建单 | 不外送；provider 工作台不可见 | 🟡（现无范围概念，工作台默认可见全部客户工单——需 8.8 过滤） |
| 未分配员工访问 | 403 `MSP_ALLOCATION_REQUIRED` + 审计 | ✅ |
| MSP 写客户内部备注 | 拒绝（非特权） | ✅（已实现） |
| MSP 读客户内部备注 | 不可见 | ✅（过滤逻辑已具备；provider 读通道 P1 沿用同一过滤） |
| 客户租户暂停 | 只读（写 403 `CUSTOMER_INACTIVE`） | ✅ |
| 终态写操作 | 拒绝；reply 契约与写路径一致 | 🟡（终态 reply 不一致，P0 修复） |
| 重复路由触发 | 幂等（单一 provider 归属） | ❌（目标态规则） |

**业务适配评估（对照 ITIL/服务台实践）**：

| 业务环节 | 标准做法 | 本设计适配 | 差距/承接 |
|---|---|---|---|
| 受理登记 | 建单即受理、通知 | ✅ | — |
| 分类定级 | 类型/优先级/SLA | ✅ | — |
| L1/L2 分流 | 技能组/队列路由 | 🟡 | 缺队列/技能路由（P2 可基于 assignment rule 扩展） |
| 派单/接单 | 派单 + 接单确认 | 🟡 | 缺"接单确认"语义（现状 assign=直接认领自己）；转派缺失（P1） |
| 处理协作 | 评论/附件/内部备注 | 🟡 | 公开评论 ✅；详情/附件/时间线缺（P1）；provider 内部备注缺（P2） |
| 升级 | SLA 触发升级 | 🟡 | 客户租户规则 ✅；provider 侧提醒/overlay 缺（P2） |
| 关单确认 | 客户验证后关闭 + 满意度 | ❌ | 缺客户验收动作与满意度跨租户呈现（P1/P2） |
| SLA 计时 | 按合同/SLA 时钟 | 🟡 | 客户租户计算 ✅；provider 侧时钟展示部分（workbench `slaDeadline`） |
| 报表考核 | 服务商考核看板 | 🟡 | workbench summary 部分（open/slaDueSoon 等）；考核 overlay 待补 |

**结论**：主链路（建单 → 可见 → 处理 → 状态回写 → 通知/审计）已适配实际业务流程；差距集中在**接单确认、转派、详情协作、关单验收、provider 内部协作**五处，分别对应 P0/P1/P2 承接（§8.7 / §13）。

---

## 9. 数据与接口契约

### 9.1 单据通用字段（跨租户相关）

| 字段 | 语义 | 规则 |
|---|---|---|
| `tenant_id` | owner 租户（唯一归属） | 必填；一切查询的硬过滤；RLS/tenantctx 一致 |
| `msp_provider_id` | 归属 provider 快照 | 建单派生；不参与授权；归属变更保留历史（XF9） |
| `is_managed_by_msp` | 托管标记快照 | 同上；仅追溯/统计/通知 |
| `managed_by_user_id` | 外部处理人快照（provider 员工） | 指派时写入；幂等回填 |
| `status/version` | 状态机 + 乐观锁 | 跨租户写必须携带版本（并发冲突 → 409） |
| `assignee_id` | 处理人（可能为 provider 员工，ID 跨租户） | 指派校验：assignee ∈ provider 租户 ∧ 持有该客户 allocation |

### 9.2 响应契约（条目级）

```jsonc
{
  "id": 223, "ticketNumber": "TKT-202610-000176", "title": "…",
  "customerTenantId": 11, "customerName": "测试-客户",
  "status": "new", "priority": "medium", "assigneeId": 71, "assigneeName": "…",
  "slaDeadline": "2026-10-07T07:42:44Z", "updatedAt": "…",
  "allowedActions": [
    {"action": "reply",  "allowed": true},
    {"action": "status", "allowed": true},
    {"action": "assign", "allowed": false, "reasonCode": "MSP_ALLOCATION_REQUIRED", "reasonText": "未分配该客户"}
  ]
}
```

- `allowedActions` 为**完整动作集**（含 `allowed=false` + 原因），前端不推断（XF5）；
- 列表接口分页/游标规范沿用工作台：每租户独立游标 + 内存合并（避免跨租户分页跳行）；
- 排序维度：`updated` / `sla`（服务端白名单，游标与排序绑定，非法游标 400）。

### 9.3 错误与拒绝策略

| 场景 | HTTP | 错误码 | 审计事件 |
|---|---|---|---|
| 未分配客户 | 403 | `MSP_ALLOCATION_REQUIRED` | `tenant.scope_denied` |
| 客户不存在（防枚举） | 404 | `CUSTOMER_TENANT_NOT_FOUND` | `tenant.scope_denied` |
| 客户暂停/过期 | 403 | `CUSTOMER_INACTIVE` | `tenant.scope_denied` |
| 声明租户 ≠ 资源租户 | 400 | `RESOURCE_TENANT_MISMATCH` | `tenant.scope_denied` |
| Home 面租户头冲突 | 401 | `TENANT_MISMATCH_REJECTED` | `tenant.probe_denied` |
| 客户账号访问 `/msp/*` | 403 | `FORBIDDEN` | `tenant.scope_denied` |
| 并发版本冲突 | 409 | `VERSION_CONFLICT` | 常规审计 |

---

## 10. 安全与审计

1. **统一授权入口**：所有跨租户判定经 `pkg/mspguard`（`CanAccessCustomer` / `ListAccessibleCustomerIDs`）；服务层**二次校验**（防路径/请求体绕过，R9 教训）；
2. **聚合面 bounded bypass**：工作台跨租户查询是**显式登记**的受控旁路（服务端集合 + `tenant_source=workbench`），不接受客户端租户列表扩大范围（WB5）；
3. **RLS/tenantctx**：逐租户查询时 `tenantctx.WithTenantID(ctx, 目标租户)` 重绑定 GUC；异步执行必须携带 ctx（I6）；
4. **审计清单**（跨租户必录）：读（列表/详情/导出）、写（回复/状态/指派/批量）、拒绝（§9.3）、分发（模板分发/升级）、切换（`source=switch`）；
5. **限流**：批量按租户限流；聚合列表 limit 上限（现状 ≤50 租户/次、批量 ≤100 条）；
6. **隐私**：登录页/未认证面不含租户信息（I8）；通知内容不含会话材料/口令；
7. **不新增隐式共享**：平台共享表（global 豁免）需显式登记 + 季度复核（I10）。

---

## 11. 不变量与验收（XFA1–XFA6）

### 11.1 不变量映射（本文件 → canon）

| 本文规则 | canon 依据 |
|---|---|
| XF1 单一归属 | B1、I7 |
| XF2 关系显式 | B3、I4、I5 |
| XF3 读写通道分离 | B6、I9、WB2/WB3 |
| XF4 条目级授权 | I5、I7、§7.3 三权分立 |
| XF6 状态机归属 | B4、I6 |
| XF8 审计四元组 | I11 |
| XF10 接入五件套 | I10（禁隐式共享）、tenant guard |

### 11.2 验收清单

| # | 验收项 | 判据（可执行） |
|---|---|---|
| **XFA1** | 新单据类型接入完整 | 五件套齐备；tenant guard 通过；跨租户端点按 §5 矩阵提供 |
| **XFA2** | 授权反例矩阵 | 未分配/暂停/过期/跨 provider/客户账号调 MSP 全部 403/404 + 审计落库；声明租户≠资源租户 400 |
| **XFA3** | 通道一致性 | 同一单据经 ②④⑤ 读取状态/字段一致；写操作版本控制生效（并发 409） |
| **XFA4** | 通知双投递与深链 | owner 侧与 provider 侧均收到；深链可直达单据详情（或明确降级规则） |
| **XFA5** | 流程集成闭环 | 模板分发→客户部署→绑定→触发→实例运行全租户内；任务投影授权与写回审计通过 |
| **XFA6** | 规模与性能 | N 客户（≥10）聚合列表游标稳定、无跳行；批量护栏生效；P95 达标（联调环境基线） |

### 11.3 已发现缺陷（本文件视角，待承接）

| 缺陷 | 影响 | 建议 |
|---|---|---|
| `admin`/`provider_tenant_admin` 缺 `workflow:*`（有 `bpmn:*`）导致工单流程端点 403 | 工单“流程”页不可用 | 补权限码或迁移路由到 `bpmn:*/task:*`（**顺延 P1**） |
| 工作台无详情端点/前端不能打开详情 | 跨租户“看得全”缺失；通知无法深链 | ✅ P1 已交付：`GET /api/v1/msp/tickets/:id`（声明租户 + allocation 双重校验）+ 前端详情抽屉 |
| 建单早于归属关系的工单快照为空 | provider 侧通知/统计漏投 | ✅ P0：幂等回填已交付（SQL + CLI；联调 0 候选） |
| provider 首管默认无 Allocation | 工作台空白（首体验断点） | ✅ P0：默认分配已实施（bootstrap ∪ admin/super_admin → primary allocation，幂等） |

---

## 12. 设计决策（XFD1–XFD8）

| # | 决策 | 理由 | 状态 |
|---|---|---|---|
| **XFD1** | 不引入单据多归属/副本模式；跨租户一律"对同一单据的受控访问" | 避免双写/双状态机；与 B1/I7 一致 | 建议定稿 |
| **XFD2** | 跨租户交互采用五通道模型（Home/工作台/过滤器/头通道/切换）+ 独立治理面；日常看+做走工作台，深度配置走切换 | 与 L2 工作台方案一致；泛化到全部单据 | 建议定稿 |
| **XFD3** | `allowedActions[]` 为唯一 UI 契约；快照字段不参与授权 | 防前端推断与历史快照误用 | 建议定稿 |
| **XFD4** | 快照策略：建单派生 + 归属变更保留历史 + 幂等回填（显式操作）；快照仅用于追溯/通知/统计 | E2 口径；修复 #223 类缺口 | 建议定稿 |
| **XFD5** | 流程集成 = 模板分发（copy-in）+ 任务投影；禁止运行时跨租户引用 | 保 B4/I6；复用既有模板/部署机制 | 待评审（需产品确认分发语义） |
| **XFD6** | 审计与反枚举策略：跨租户全审计；未授权资源用 404 防枚举；冲突探针落库 | I11；安全基线 | 建议定稿 |
| **XFD7** | 路由默认策略：按**服务范围**自动路由（三级：租户/类型/工单级；复用 `auto_assign` 规则引擎扩展 `provider_queue`/`provider_user` 动作）+ 手动兜底 + 流程驱动；未托管单据不外送、工作台默认按范围过滤 | 业务适配；复用既有引擎；幂等单归属 | **已实施 MVP**（类型级 `msp_route_enabled` + `msp_routed_at` + `routedOnly`）；手动/流程驱动待 P1/P2 |
| **XFD8** | 评论可见性：公开评论双向互通（同一评论流）；内部备注租户内隔离（MSP 跨租户员工禁写客户内部备注，保持现状）；provider 内部备注存 provider 侧 | 隐私与协作平衡（现状 `is_internal` 字段已具备） | 待评审 |

**开放问题**（待评审确认）：

1. provider 首管默认分配策略：自动为"归属客户"建 primary allocation，还是仅提供"一键分配"按钮？（体验 vs 最小权限）
2. 模板分发的版本治理：强制升级 / 提示升级 / 仅一次性分发？（XFD5 落地前确认）
3. 服务请求是否并入工单面聚合（现状同源）？变更/问题的优先级排序？
4. 跨租户只读详情的最小字段集（描述/评论/时间线/SLA/CI/附件哪些必须）？

---

## 13. 分期落地建议

> 编号承接：建议在[实施方案](./msp-implementation-plan.md)登记为 `IP-P1-#` / `IP-P2-#`（避免与本文件局部编号混用）。

| 阶段 | 交付物 | 依赖 | 验收 |
|---|---|---|---|
| **P0（修正与体验）✅ 2026-10-07 全部落地** | ① ✅ 快照存量回填（SQL + `cmd/backfill_ticket_snapshot`，联调 0 候选）；② ✅ provider 首管默认分配（provision/bootstrap 钩子 + `cmd/backfill_msp_allocations`）；③ ✅ 终态 reply 契约修复；④ ✅ 类型级自动路由（`msp_route_enabled`/`msp_routed_at`/`routedOnly`）；⑤ ✅ `workflow:*` 权限缺口修复（admin 补 `workflow:read/write`，provider_tenant_admin 派生继承；工单流程端点实测 200） | 无 | 单测全绿；S1 实跑：类型 14 开启路由 → #224 → provider 工作台 all=2 / routedOnly=1 |
| **P1（跨租户详情）🟡 部分落地 2026-10-07** | ① ✅ `GET /api/v1/msp/tickets/:id`（工单只读详情：描述/SLA/发起人/处理人/`allowedActions`；声明租户 + allocation 双重校验）；② ✅ 前端工作台详情抽屉（行点击 + 只读动作展示）；③ ✅ 通知深链直达详情（provider 侧 `action_url` 深链 + 铃铛点击直达 + 工作台参数自动打开）；④ ✅ 评论只读面（`GET /api/v1/msp/tickets/:id/comments`，XFD8：公开评论双向互通/客户内部备注不下发；抽屉只读区 + 首个共享渲染件 `TicketCommentTimeline`）；⑤ ⏳ `ticket-api` 头通道；状态变更时间线（无历史表）/CI/附件留后续 | P0 | XFA3/XFA4 |
| **P2（流程集成）** | ① 模板分发端点与来源追踪；② 客户流程中的 provider 任务投影（待办面 + 写回）；③ 变更/问题/服务请求接入工作台 | XFD5 定稿 | XFA5；接入五件套 XFA1 |
| **P3（泛化收尾）** | CMDB/CI 只读投影、导出、跨租户报表扩展 + 前端复用收敛（§13.1：列表壳/标签单源/筛选 DSL/详情投影） | P1/P2 | XFA6 |

### 13.1 前端复用收敛清单（P3 前置；2026-10-07）

**决策**：跨租户**授权边界保持独立**（`/api/v1/msp/*` 与客户端点双通道，不合并——权限面/RLS 作用域/写路径审计不同）；**呈现层收敛复用**——工作台与 `/tickets` 共享展示组件，工作台只注入「客户列 + provider 动作集」。

| # | 复用件 | 现状 | 抽取目标 | 状态 |
|---|---|---|---|---|
| 1 | 评论条目渲染单源 `components/business/CommentItemBody.tsx`（只读时间线 `TicketCommentTimeline` 底层复用） | 工作台详情抽屉 + 客户侧 `CommentPanel`（工单/事件详情 Tab）均已接入 | 客户侧头像/@提及/附件/编辑删除经槽位注入（交互与 i18n 不变） | ✅ 已完成（2026-10-07） |
| 2 | 工单列表壳（表格 + 游标分页 + 空态/错误态 + 列配置） | 工作台与 `/tickets` 各自维护 | 抽 `TicketListShell`：列定义/行操作/数据源由调用方注入 | 待做 |
| 3 | 状态/优先级标签映射 | 两处各自维护 `STATUS_LABEL`/`PRIORITY_LABEL` | 抽 `ticket-labels.ts` 单源（含颜色） | 待做 |
| 4 | 筛选条（状态/优先级/处理人/关键词 + URL 同步） | 两处形态不同 | 抽筛选 DSL + URL 同步 hook（跨租户附加客户维度） | 待做 |
| 5 | 详情只读投影（Descriptions 区块） | 工作台抽屉自有 | 与客户侧详情共享"只读字段渲染"（字段清单由调用方给定） | 待做 |

**刻意不收敛**：授权中间件（`RequireMSPPermission` vs `RequirePermission`）、数据获取层（单租户查询 vs 逐租户查询+合并）、写路径（条目级 `allowedActions` + 跨租户逐条审计）——合并会把跨租户分支扩散到 40+ 个客户端点，破坏 fail-closed 边界。

**迁移与回滚**：

- 快照回填 = 幂等批处理（按客户租户分批，可重入；失败不影响在线）；
- 权限修复 = 角色模板补码 + 迁移（存量角色行回填）+ 缓存刷新（参考 `023_grant_ticket_type_install_preset_to_admin` 模式）；
- 分发能力 = 新端点 + 灰度开关（默认关，按租户/按 provider 放量）；
- 全部改动不改变现有数据归属与状态机，可独立回滚。

---

## 14. 风险（XFR1–XFR4）

| # | 风险 | 影响 | 缓解 |
|---|---|---|---|
| **XFR1** | 分配缺省导致"工作台空白"首体验断点 | 用户误判"产品不打通"（本次实测即此） | P0-③ 默认分配策略 + 空态引导（"去分配"） |
| **XFR2** | 历史快照缺失导致 provider 侧通知/统计漏投 | 通知不一致、考核失真 | P0-② 回填 + XF9 规则化 |
| **XFR3** | 详情缺位迫使切换代偿 → 会话污染/漏单 | 违反 I9/WB2 的根因回归 | P1 详情端点；工作台空态引导切换仅用于配置 |
| **XFR4** | 模板分发引入配置漂移（provider 版本 vs 客户副本） | 客户流程与预期不一致 | 来源/版本追踪 + 升级提示（XFD5 开放问题 2） |

---

## 附录 A：证据索引

**代码（后端）**

- 租户中间件/头通道：`middleware/tenant.go:32-47,61-119`、`middleware/msp_middleware.go:136-173`、`middleware/msp_tenant_resolver.go:29-57`
- 授权唯一入口：`pkg/mspguard/mspguard.go:65-106,111-147`；门面 `service/msp_access_validator.go:86-121`
- 工作台：`service/msp_workbench.go:121-195`（列表）、`:225-244`（条目字段）、`:412-427`（目标租户 RBAC）、`:589-622`（条目级授权）、`:716-804`（批量）
- 工单快照/指派：`service/msp_snapshot.go:12-28`、`service/ticket_core_service.go:125-154`、`service/ticket_service.go:2575-2603,2606-2647`
- 通知：`service/msp_provider_side_notification.go:3-17,29-30,35-73`
- 路由：`router/msp_routes.go:16-60`、`router/ticket_routes.go:164-180,229-233`
- 工作流：`ent/schema/process_definition.go:81-83,114`、`ent/schema/processbinding.go:103-106`、`service/bpmn_process_binding_service.go:32-39`、`service/bpmn_process_trigger_service.go:62-76`、`service/process_resolver.go:26-68`、`handlers/bpmn/workflow.go:35-52,264-297,307-319`、`service/bpmn_workflow_template_catalog.go:36-37,83-84`、`internal/schema/tenant_guard.go:65-130`
- 权限：`internal/authz/roles.go:232+`（admin，无 `workflow:*`）、`:455`（含 `workflow:*` 的角色）、`internal/authz/catalog.go:145-146,209-211`

**代码（前端）**

- 工作台：`src/lib/api/msp-workbench-api.ts:168-198,200-237,247-294`；页面 `src/pages/(main)/msp/workbench/index.tsx:106-131,284-286,367-370,472,517,535`
- 工单：`src/lib/api/ticket-api.ts:17-29,42-46`；`src/lib/api/http-client.ts:404-411`（单请求显式头能力）
- 流程设计器：`src/pages/(main)/workflow/designer/index.tsx`、`src/components/business/TicketTypeFormModal.tsx:154`

**实测（2026-10-07，本机联调实例）**

- 脚本：`.dev/msp-integration-probe.ps1` ~ `.dev/msp-integration-probe6.ps1`
- 关键结果：Allocation 前工作台空/客户工单 403；创建 Allocation（id=31）后工作台出现 #223、`allowedActions` 全开；#223 快照为空；工单流程端点对管理员 403；provider/customer 各自独立 BPMN 定义与绑定。

**文档**

- canon：`plan/msp-concept-model-and-architecture-canon.md` §2.1/§6/§7.2/§7.3/附录 C；工作台方案 `WB1–WB6`/`REV-1–5`；场景 05/07/08。

## 修订记录

| 版本 | 日期 | 变更 |
|---|---|---|
| v0.1 | 2026-10-07 | 首版：单据流转泛化（XF1–XF10）、五通道模型与决策树、操作矩阵与 API 规范、单据类型接入五件套、流程集成三模式（模板分发/任务投影/深度切换）、审计与验收（XFA1–XFA6）、分期落地与风险（XFR1–XFR4）；附实测基线与证据索引 |
| v0.2 | 2026-10-07 | 新增 **§8 端到端流转设计**（customer 建单 → provider 处理 → 状态回写）：四维对齐（用户/角色/单据/流程）、读路径（可见性派生 + 四入口 + 时序）、写路径（代码级 7 步授权链 + 工单状态机迁移表 + 回写事件）、角色-动作矩阵、provider 任务节点设计（任务投影 + allocation 角色解析 + 写回）、通知双投递矩阵、端到端差距承接；章节号顺延（原 §8–§13 → §9–§14）；记录终态 reply 契约不一致缺陷 |
| v0.3 | 2026-10-07 | 新增 **§8.8 路由与交接**（自动/手动/流程驱动 + 服务范围三级 + 复用分配规则引擎 + 工作台范围过滤）、**§8.9 信息互通矩阵**（公开评论双向互通/内部备注租户内隔离/provider 内部备注；字段与身份边界）、**§8.10 端到端模拟与业务适配评估**（S1 标准托管 / S2 流程驱动 / S3 反例 + ITIL 对照）；新增决策 XFD7/XFD8 |
| v0.4 | 2026-10-07 | **P0 落地**：终态 reply 契约修复（`workbenchAllowedActions` 与 `authorizeTicketAction` 同口径 + 回归用例）；快照回填 Go 版（`service.BackfillTicketMSPSnapshots` + `cmd/backfill_ticket_snapshot`）；provider 首管默认分配（`EnsureBootstrapAllocationsFor*` + provision/bootstrap 钩子 + `cmd/backfill_msp_allocations`，bootstrap ∪ admin/super_admin）；类型级自动路由（`ticket_types.msp_route_enabled` + `tickets.msp_routed_at` + 建单标记 + `routedOnly` 过滤 + 迁移 024 + 前端类型表单开关）；联调实跑 S1（类型 14 → #224 → 工作台 all=2/routedOnly=1）与回填 dry-run；`workflow:*` 权限缺口顺延 P1 |
| v0.5 | 2026-10-07 | **P1 首批落地**：① `workflow:*` 权限缺口修复（admin 补 `workflow:read/write`，provider_tenant_admin 由 admin ∪ msp_admin 派生继承；新增 `cmd/sync_role_permissions` 跨租户幂等同步——联调 11 租户补 112 条、复跑 0/0；工单流程端点 403→200 实测）；② 跨租户工单详情 `GET /api/v1/msp/tickets/:id?customerTenantId=NN`（声明租户绑定 RLS + allocation 二次校验 + 与列表同口径 `allowedActions`，暂停客户可读只读；未分配/不存在同码防枚举）+ 服务层/路由级回归用例；③ 前端工作台详情抽屉（行点击打开、只读投影、动作可用性展示）+ API 契约测试。剩余：通知深链、评论/时间线只读面（P2） |
| v0.6 | 2026-10-07 | **P2 首批落地（通知深链）**：provider 侧双投递 `Notification.action_url` 由 `/msp/workbench` 升级为条目深链 `/msp/workbench?ticket=<id>&customerTenantId=<tid>`（非事务/Tx 两路径同源）；**修复存量缺陷**：双投递未重绑 provider 上下文，被写路径租户守卫拦截（客户 ctx 写 provider 行 → `cross-tenant insert blocked`，provider 侧通知实际未落库）——现按 `tenantctx.WithTenantID(ctx, provider)` 重绑后再查询/写入（联调实测修复）；前端铃铛 `NotificationDrawer` 点击按 `actionUrl` 直达（仅站内路径），工作台按深链参数自动打开详情抽屉（参数不参与列表过滤键）；测试：双投递守卫在线校验 + 深链断言 + A12 路由 e2e 断言 + 工作台深链用例；**浏览器实测**：打开深链自动弹出 #224 详情抽屉（字段 + 动作可用性完整）。剩余：评论/时间线只读面、`ticket-api` 头通道、流程驱动路由 |
| v0.7 | 2026-10-07 | **P2 评论只读面 + 前端复用收敛决策**：新增 `GET /api/v1/msp/tickets/:id/comments`（XFD8：公开评论双向互通、客户内部备注不下发；声明租户 + allocation 双重校验；作者名/归属按客户/provider 双作用域解析；时间正序 + total + limit）；工作台详情抽屉评论只读区（与详情解耦、失败仅降级区块）；首个共享渲染件 `TicketCommentTimeline`（纯展示、无写入口、缺省作者侧标签 → 客户侧可复用）；新增 §13.1 前端复用收敛清单（呈现层共享；授权/数据获取/写路径刻意不收敛）。测试：服务层评论用例 + A11/A12 路由 e2e 评论断言 + 前端 3 套件 16 用例 |
| v0.8 | 2026-10-07 | **前端复用收敛 #1 收尾（评论条目渲染单源）**：抽取 `CommentItemBody`（身份行/侧标签/内部标签/时间/已编辑标记/正文 + 头像/附加标签/底部/操作四槽位），`TicketCommentTimeline`（工作台只读）与客户侧 `CommentPanel`（工单/事件详情 Tab）共用同一渲染源；客户侧头像、@提及、附件、编辑/删除与编辑态经槽位注入，交互与 i18n 不变。测试：新增 `CommentItemBody` 用例 + 7 套件 35 用例全绿；`tsc --noEmit`/eslint 0 |
