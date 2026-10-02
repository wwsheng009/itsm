# MSP 跨客户工作台与全局过滤方案（替代"全局切换"）

> 状态：**Draft v0.2（2026-09-30 P0 契约冻结）**｜日期：2026-09-30｜基准：仓库 HEAD `337558e3`
> 关联：[目标架构方案](./msp-target-architecture.md)（§5 权限、§9 前端）｜[登录与切换细化方案](./msp-login-and-switching-refinement-plan.md)（§4 切换器）｜[前端页面与权限分析](./msp-frontend-pages-and-permissions-analysis.md)（§6.2）｜[用户交互流程图](./msp-user-interaction-flows.md)（§0.5/F-05/F-06）｜[主方案](./msp-user-lifecycle-and-tenant-switching-plan.md)（F13/F14）
> 决策来源：2026-09-29 产品反馈——**服务商多客户并行处理时，"全局切换"会漏单**；顶栏选择器应作为**全局过滤器**（全部/部分客户），单据列表直接展示并处理跨客户条目。

---

## 0. 摘要（TL;DR）

**问题**：把顶栏控件做成"会话作用域切换器"，服务商员工必须"切到客户 A 处理 → 切到客户 B 处理"；多客户同时来单时容易**漏单**、丢失总览、切换成本高。

**方案**：把三个概念拆开——

| 概念 | 作用 | 变化频率 |
|---|---|---|
| **会话作用域**（Active Tenant） | 服务端授权/数据归属基线（JWT/RLS/审计） | 低频（仅"深度操作"） |
| **视图过滤器**（Global Filter） | 决定"看哪些客户"的数据（全部/子集） | 高频（随时调） |
| **条目级操作**（Item Action） | 对具体单据的读写；**授权按资源所属租户判定** | 高频（日常） |

**结论**：
1. **顶栏 = 全局过滤器**（多选客户 + "全部客户" + 计数徽标），只改视图，**不改会话**；
2. **跨客户工作台**：工单/需求列表带"客户"列（可分组），**行内直接回复/改状态/指派**，无需切换；
3. **会话切换降级**为"深度操作入口"（进入某客户做配置/用户管理等连续操作时才用）；
4. 写操作改为**条目级端点**（服务端按单据实际 `tenant_id` 授权），头通道维持"单请求只读"不变。

---

## 1. 概念分离与规则

```text
┌────────────────────────────────────────────────────────────┐
│ 顶栏：CustomerFilter（全局过滤器）                            │
│   [✓ 全部客户] 或 [Acme ✓] [Beta ✓] [Gamma …]  计数徽标       │
└───────────────────────┬────────────────────────────────────┘
                        │ 只影响"看什么"（视图）
                        ▼
┌────────────────────────────────────────────────────────────┐
│ 服务商工作台（provider 作用域）                               │
│  客户 | 工单 | 状态 | 优先级 | SLA | 负责人 | 更新时间         │
│  ├─ 行内操作：回复 / 改状态 / 指派（条目级授权，无需切换）       │
│  └─ 批量操作（护栏：上限 + 客户分布确认 + 逐条审计）            │
└───────────────────────┬────────────────────────────────────┘
                        │ 需要"进入客户"做深度操作时
                        ▼
┌────────────────────────────────────────────────────────────┐
│ 深度切换（Active Tenant = 客户 X）                            │
│  客户配置 / 用户管理 / 连续多步操作 / 客户内页面                 │
└────────────────────────────────────────────────────────────┘
```

**规则**：

| # | 规则 |
|---|---|
| WB1 | 过滤器改变**视图**，不改变会话作用域；会话作用域仍是单一"当前生效租户" |
| WB2 | 条目级操作按**资源所属租户**授权（分配集合 × 目标租户 RBAC × 租户状态），**不要求切换** |
| WB3 | 头通道（`X-Customer-Tenant-ID`）保持**单请求只读**；写操作一律走条目级端点（资源自带租户） |
| WB4 | 切换仅用于"深度操作"（客户内配置/用户/角色/连续多步）；切换后 UI 明确显示"当前客户：X" |
| WB5 | 过滤器可选集合 = 调用者的有效分配集合（服务端强制；未分配客户不可见、不可操作） |
| WB6 | 客户账号（`account_kind=customer`）无过滤器、无工作台、无切换器（API 侧同样拒绝） |

---

## 2. 目标交互（服务商视角）

### 2.1 顶栏全局过滤器（`CustomerFilter`）

- **形态**：多选下拉 + "全部客户"开关 + 搜索 + 每客户**计数徽标**（待处理/超 SLA/未指派）；
- **可见性**：**仅 `provider`**；客户账号永不渲染；**平台管理员不挂载工作台/过滤器/头通道**（治理通道独立：先选目标租户 + 审计 + 二次确认，见 canon D11/B2）；
- **记忆**：默认"全部客户"；用户选择持久化（P1 服务端偏好 `users.preferences.workbenchFilter`，P0 先 URL/localStorage）；
- **与页面联动**：工作台、报表、客户列表、计数徽标全部跟随过滤器；URL query 同步（可分享/刷新保持）。

### 2.2 跨客户工作台（核心页面）

- **位置**：`/msp/workbench`（provider 面新首页；现有 `/msp` 仪表盘并入；平台不挂载）；
- **列表**：`客户 | 工单号 | 标题 | 状态 | 优先级 | SLA 剩余 | 负责人 | 更新时间`；
- **排序**：默认 SLA 紧迫度（超期 → 临近 → 普通），次键更新时间；支持按客户分组视图；
- **行内操作**：回复 / 改状态 / 指派 / 查看详情（按目标客户权限集显示；服务端返回 `allowedActions` 避免前端重建权限）；
- **"进入客户"动作**：行/客户头部的"深度操作"入口 → 触发会话切换（进入客户 X），用于配置类页面；
- **批量**：勾选多条 → 批量回复/改状态/指派；护栏见 §3.3；
- **不遗漏保证**：过滤器默认"全部客户" + 每客户计数徽标 + "未指派/超 SLA"快捷筛选。

### 2.3 上下文指示（两种状态区分）

| 状态 | 顶栏显示 | 语义 |
|---|---|---|
| 工作台态（默认） | `服务商工作台 · 全部客户（12）` 或 `· 3 个客户` | 过滤器生效，会话仍是 provider |
| 深度态 | `当前客户：Acme（深度操作中）` + "返回工作台" | 会话作用域 = Acme |

> 明确区分可避免"在错误的客户下操作"的旧风险，同时不牺牲跨客户效率。

---

## 3. 授权与安全模型

### 3.1 资源级授权链（条目级操作）

```text
Allow(item, action) =
    item.tenant_id ∈ AllowedCustomers(actor)          // 有效 MSPAllocation
  ∧ RBAC(actor, action, tenant=item.tenant_id)        // 目标租户内角色（msp_tech/msp_manager…）
  ∧ TenantStatus(item.tenant_id) = active             // 暂停/过期 → 写拒绝
  ∧ ResourceState(item) 允许该动作                     // 状态机校验
```

- **不信任请求参数**：body/query 里的 `customerTenantId` 仅作**一致性校验**（与资源实际租户比对，不一致 → 400/403 + 审计）；
- **权限来源**：后端按**目标租户**的 DB 权限判定（与运行时 DBOnly 一致，不依赖前端快照）。

### 3.2 受控跨租户（bounded bypass）

跨客户列表/计数需要跨租户查询，必须走**带上限的显式通道**：

```go
// 仅 MSP 工作台使用；携带 actor + 允许集合 + reason，逐次审计
ctx = tenantctx.WithMSPWorkbenchBypass(ctx, actor, allowedTenantIDs, "workbench:list")
```

- 允许集合来自服务端 `AllowedCustomers`（**不接受客户端传入的集合作为授权依据**）；
- 禁止将其扩展为"全域 bypass"；使用点静态检查 + 审计（`tenant_source=workbench`）；
- RLS `enforce` 兼容策略：**P0 采用"逐租户查询 + 内存合并"**（对 RLS 天然友好、天然限流），P1 评估单查询 `tenant_id IN (...)` + bypass 优化。

### 3.3 批量操作护栏

| 护栏 | 要求 |
|---|---|
| 上限 | 单次 ≤ 100 条（可配置）；跨客户批量仅允许"低危动作"（回复/改状态/指派），高危动作（删除/关闭并归档）禁止跨客户批量 |
| 展示 | 确认对话框显示**客户分布**（如 "Acme×12、Beta×3"）与动作摘要 |
| 授权 | **逐条授权**（任一条越权即整单拒绝或按条返回失败，建议"逐条结果"） |
| 审计 | 逐条写审计（`source=workbench, batch_id=...`），可回溯整批 |
| 限流 | 每租户速率上限（防单客户风暴影响其他客户的工作台体验） |

### 3.4 必须拒绝的请求（新增反例）

1. 过滤器/参数中出现**未分配客户** → 该客户从结果中剔除 + 审计（`tenant.scope_denied`），不报"客户不存在"（防枚举）；
2. 条目操作的目标单据租户 ≠ 请求声明的 `customerTenantId` → 拒绝 + 审计；
3. 客户账号调用 `/msp/workbench/*` → 403；
4. 暂停/过期租户的条目写操作 → 403（读按策略）；
5. 批量操作超出上限或含高危动作 → 400。

---

## 4. API 设计

| 端点 | 用途 | 备注 |
|---|---|---|
| `GET /api/v1/msp/workbench/tickets` | **跨客户工单列表**：`customerTenantIds=all\|1,2`、`status/priority/assigneeId/q/updatedAfter`、`groupBy=customer`、`sort=sla\|updated`、cursor 分页 | 每项含 `customerTenantId/customerName` + `allowedActions[]`；**响应/游标/元素结构见[实施方案 §3.0-D](./msp-implementation-plan.md)** |
| `GET /api/v1/msp/workbench/summary` | 每客户计数（待处理/超 SLA/未指派），供过滤器徽标 | 轻量聚合 |
| `POST /api/v1/msp/tickets/:id/reply` | 条目级回复 | 服务端按单据租户授权 + 审计 |
| `POST /api/v1/msp/tickets/:id/status` | 条目级改状态 | 同上；状态机校验 |
| `POST /api/v1/msp/tickets/:id/assign` | 指派（**已有**，扩展审计与 allowedActions 一致性） | 现有 `msp_routes.go:35` |
| `POST /api/v1/msp/workbench/batch`（P1） | 批量操作（护栏见 §3.3） | 返回逐条结果 |
| `GET/PUT /api/v1/users/me/preferences`（P1） | 过滤器/排序偏好 | 仅本人 |
| 保留 | `GET /msp/customers/:id/tickets`、`/msp/reports/*` | 单客户钻取与报表不变 |

**错误码**（权威注册表见[实施方案 §3.0-A](./msp-implementation-plan.md)）：`MSP_ALLOCATION_REQUIRED`、`RESOURCE_TENANT_MISMATCH`、`CUSTOMER_INACTIVE`、`INVALID_CURSOR`、`BATCH_LIMIT_EXCEEDED`、`ACTION_NOT_ALLOWED`（均带审计）。

---

## 5. 数据与性能

| 项 | 设计 |
|---|---|
| 索引 | `tickets(tenant_id, status, updated_at)`、`tickets(tenant_id, assignee_id, status)`、`tickets(tenant_id, sla_resolution_deadline)`（先查 `pg_indexes`，缺则建；DDL 见[实施方案 §3.0-B5](./msp-implementation-plan.md)） |
| 分页 | 跨租户统一排序用**复合游标** `(sort_key, tenant_id, id)`；禁用深 OFFSET |
| 查询规模 | 单请求租户集合 ≤ 50（超出拒绝或分批）；P0 逐租户查询并发上限 8 |
| 限流 | 工作台查询按用户 QPS；写操作按 `(actor, tenant)` 双维度 |
| 缓存 | 列表不缓存（实时）；`summary` 允许 30s 内存缓存（key 含租户集合哈希 + 用户） |
| RLS | P0 逐租户查询（`WithTenantID` 逐次切换）；P1 评估集合查询 + bounded bypass |

---

## 6. 前端细化（与前端分析文档联动）

| 项 | 设计 | 落点 |
|---|---|---|
| 组件 | 新增 `components/layout/header/CustomerFilter.tsx`（多选+全部+搜索+徽标） | 顶栏 |
| 深度切换 | `TenantSwitcher` 降级为"进入客户"动作（客户详情/工作台行/客户列表），不再作为顶栏主控件 | `components/**` |
| 工作台页 | 新增 `pages/(main)/msp/workbench/index.tsx`（列表+分组+行内操作+批量护栏） | 路由 `/msp/workbench` |
| 过滤器状态 | URL query（P0）+ 服务端偏好（P1）；**与 `tenant-context` 解耦** | store/URL |
| 上下文指示 | 工作台态 vs 深度态两套文案（§2.3） | `Header` |
| 权限显示 | 行内按钮按 `allowedActions[]` 渲染（后端计算，含目标租户权限） | 工作台 |
| 现有 MSP 页 | `/msp` 仪表盘并入工作台；`/msp/management` 保留（分配管理） | 路由 |

---

## 7. 对既有方案的修订（REV-1–REV-5）

> 编号说明（2026-09-29 一致性整改）：本节原用 `R7–R11`，与 canon 风险编号 `R7–R10` 重号，现改为 `REV-*`；上方规则表原用 `G1–G6`，与 07-known-gaps 的 `G1–G10` 重号，现改为 `WB1–WB6`（注册表见 canon 附录 C）。

| # | 既有设计 | 修订后 |
|---|---|---|
| REV-1 | "**写操作必须切换作用域**"（目标架构 §4.2、流程图 §0.5/F-06） | **修订**：条目级写操作按资源所属租户授权，**无需切换**；仅"深度操作"需要切换 |
| REV-2 | 顶栏 = 作用域切换器（登录与切换细化 §4） | **修订**：顶栏主控件 = **全局过滤器**；切换器降级为"进入客户"入口 |
| REV-3 | 头通道仅单请求只读 | **保持**（不扩展头通道写；写走条目级端点） |
| REV-4 | 流程图 §0.5"三种方式"（总览/头通道/切换） | **更新为四种**：工作台（看+做，主路径）/ 过滤器（看）/ 头通道（单请求只读）/ 深度切换（进入客户） |
| REV-5 | 前端分析 §6.2"TenantSwitcher"为顶栏主控件 | **修订**：`CustomerFilter` 为顶栏主控件；`TenantSwitcher` 降级 |

---

## 8. 分期与验收

| 批次 | 内容 |
|---|---|
| **P0** | 工作台列表 + summary + 条目级 reply/status/assign + `CustomerFilter`（全部/子集）+ 行内操作 + 审计 + 未分配客户剔除（**后端 ✅ 2026-09-30，IP-P0-7**；前端归 IP-P0-8） |
| **P1（全项完成）** | 批量操作（护栏）**✅ 2026-09-30（IP-P1-6a 后端 `POST /msp/workbench/batch`：≤100/低危白名单/逐条授权审计 batch_id/租户限流；IP-P1-6b 前端：行勾选 + 客户分布确认 + 逐条结果）** + 过滤器服务端偏好**✅（IP-P1-6c：后端 `users.preferences` + `GET/PUT /users/me/preferences`；前端水合 + 400ms 节流保存）** + `allowedActions` 全量接入**✅（reply/status/assign 三动作行内渲染；assign 语义=指派给当前技术员，批量同口径）** + RLS 集合查询评估**✅（[评估文档](./msp-rls-collection-query-assessment.md)：保留逐租户查询；enforce 前置清单归 IP-P1-7）** + 分组视图**✅（平铺/按客户分组切换，`view=group` URL 持久化；组头客户名+条数，组内复用行内操作与批量勾选）** |
| **P2** | 工作台自定义视图（保存过滤器组合）、SLA 风险看板、每客户配额可视化 |

**验收（WB-A1–WB-A6）**：

- WB-A1 不切换会话即可处理不同客户工单；审计含 `actor/target_tenant/source=workbench`；
- WB-A2 过滤器"全部/子集"生效，刷新/分享 URL 保持；徽标计数与列表一致；
- WB-A3 未分配客户不出现在过滤器与列表（且不可操作）；
- WB-A4 批量操作逐条授权、逐条审计、超限拒绝、客户分布确认；
- WB-A5 暂停租户条目只读；`msp_admin` 未授予域仍拒绝（角色词表统一见 canon D10）；
- WB-A6 工作台态与深度态指示互不混淆。

---

## 9. 风险（WB-R1–WB-R6；局部编号，避免与 canon R1–R12 重号）

| # | 风险 | 处置 |
|---|---|---|
| WB-R1 | 跨客户批量误操作 | 上限 + 客户分布确认 + 逐条审计 + 高危动作禁批量 |
| WB-R2 | bounded bypass 被滥用为全域通道 | 集合来自服务端 + 静态检查 + 审计 + 评审 |
| WB-R3 | 热点客户拖慢工作台 | 每租户限流 + 逐租户查询并发上限 + summary 缓存 |
| WB-R4 | RLS `enforce` 后集合查询不可行 | P0 逐租户查询路径已兼容；P1 再评估优化 |
| WB-R5 | 审计量激增 | 写操作逐条审计（保留）；读不审计明细，仅记录 bypass 使用与越权尝试 |
| WB-R6 | 过滤器语义与"当前客户"混淆 | UI 双态指示（§2.3）+ 文档/培训口径统一 |

---

## 附录：证据索引

- 现有 MSP 路由：`itsm-backend/router/msp_routes.go:16-40`（`/customers/:id/tickets`、`/tickets/:id/assign`）
- MSP 中间件与分配校验：`itsm-backend/middleware/msp_middleware.go:27-173`、`msp_tenant_resolver.go:29-57`
- 前端 MSP API/页面：`itsm-frontend/src/lib/api/msp-api.ts:19-139`、`src/pages/(main)/msp/index.tsx`、`msp/management/index.tsx`
- 关联文档：[目标架构方案](./msp-target-architecture.md)、[登录与切换细化方案](./msp-login-and-switching-refinement-plan.md)、[前端页面与权限分析](./msp-frontend-pages-and-permissions-analysis.md)、[用户交互流程图](./msp-user-interaction-flows.md)

## 修订记录

| 版本 | 日期 | 变更 |
|---|---|---|
| v0.1 | 2026-09-29 | 首版：会话作用域/视图过滤器/条目级操作三概念分离；顶栏全局过滤器；跨客户工作台（列表带客户列 + 行内操作 + 批量护栏）；资源级授权与 bounded bypass；API 设计（workbench list/summary + 条目级写）；数据/性能（逐租户查询、复合游标、限流）；R7–R11 修订 |
| v0.2 | 2026-09-30 | **P0 契约冻结与回填**：API 响应/游标/`allowedActions` 元素结构指向实施方案 §3.0-D；错误码统一（`RESOURCE_TENANT_MISMATCH`/`CUSTOMER_INACTIVE`/`INVALID_CURSOR`）；索引补 `sla_resolution_deadline`；WB-A5 角色改 `msp_admin`；基准 HEAD 重钉 `337558e3` |
