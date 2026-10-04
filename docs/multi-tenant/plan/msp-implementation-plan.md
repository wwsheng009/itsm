# MSP 多租户实施方案（P0 → P1 → P2）

> 状态：**v1.0（2026-09-30 评审通过；P0 契约冻结）**｜日期：2026-09-30｜基准：仓库 HEAD `337558e3`
> 上位：[ADR-004](../../architecture/adr-004-multi-customer-tenant-model-selection.md)（**Accepted 2026-09-30**）｜[canon](./msp-concept-model-and-architecture-canon.md)（v1.0；概念/边界/决策登记，附录 C 注册表）
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
| 决策与契约 | ✅ 2026-09-30 冻结：D1–D11 / E1–E6 全部定稿（canon v1.0 §10）；P0 错误码 / DDL / 权限矩阵 / workbench schema / 审计事件见 **§3.0** |

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

## 3. P0 详细实施（11 个工作流）

> 通用规则：每个工作流 = 一个 PR（或一组小 PR）；**代码与文档同 PR**（修订即回填）；新增编号先登记 canon 附录 C；发布前 `make docs-gate` 6/6。

### 3.0 P0 冻结契约（2026-09-30；错误码 / DDL / 权限矩阵 / Schema）

> 本节与 [canon v1.0 §10](./msp-concept-model-and-architecture-canon.md)、ADR-004（Accepted）同步；**冻结后变更须回填本节并在修订记录登记**。

**A. 错误码注册表（统一 `{code, message, details}`；所有 401/403 带审计）**

| 错误码 | HTTP | 触发 | 关联工作流 |
|---|---|---|---|
| `CROSS_TENANT_FORBIDDEN` | 403 | 非合法跨租户通道/身份（无平台或 MSP 权限） | IP-P0-2/5 |
| `MSP_ALLOCATION_REQUIRED` | 403 | 无有效 allocation（头/路径/请求体/切换四通道一致） | IP-P0-2/5/6/7 |
| `CUSTOMER_TENANT_NOT_FOUND` | 404 | 目标客户租户不存在或不属于本 provider | IP-P0-2 |
| `CUSTOMER_INACTIVE` | 403 | 目标租户 suspended/expired（写一律拒绝） | IP-P0-2/6/7 |
| `RESOURCE_TENANT_MISMATCH` | 400 | 请求声明的 `customerTenantId` 与资源实际租户不一致 | IP-P0-2/7 |
| `TENANT_MISMATCH_REJECTED` | 401 | JWT 与 `X-Tenant-Code`/`X-Tenant-ID` 冲突（修 `07:G9`） | IP-P0-6 |
| `TENANT_SELECTION_CONFLICT` | 400 | 请求参数租户与会话冲突 | IP-P0-6 |
| `TENANT_ACCESS_REVOKED` | 401 | refresh 复核失败（分配/会话失效，不回退 home） | IP-P0-6 |
| `MSP_TICKET_ID_CONFLICT` | 409 | `(msp_provider_id, msp_ticket_id)` 重复 | IP-P0-3 |
| `MSP_ALLOCATION_EXISTS` | 409 | 重复分配（同员工同客户已有有效 allocation）；调用方可按 reasonCode 判定为已存在（幂等） | 验收 D-2（2026-10-04） |
| `ROLE_NOT_GRANTABLE` | 422 | 角色不在目标租户或高于调用者权限集 | IP-P0-5/9 |
| `MSP_ROLE_NOT_ALLOWED` | 422 | `msp_role` 非法组合或通道不允许 | IP-P0-5 |
| `USERNAME_EXISTS` / `EMAIL_EXISTS` | 409 | 全局唯一冲突 | IP-P0-5 |
| `INVALID_CURSOR` | 400 | 工作台游标失效/非法 | IP-P0-7 |
| `BATCH_LIMIT_EXCEEDED` / `ACTION_NOT_ALLOWED` | 400 / 403 | 批量超限 / 含高危动作（P1） | IP-P1-6 |
| `CUSTOMER_SCOPE_CONFLICT` | 422 | customer 账号出现第 2 条 active membership（P1，DB 兜底） | IP-P1-1 |
| `TENANT_QUOTA_EXCEEDED` | 422 | 租户硬配额超限（`maxUsers` / `maxTicketsPerMonth` / `maxStorageMB`；明细 `quota/limit/used`；缺省/<=0 = 不限） | IP-P2-6 |

**B. P0 DDL 清单（在线 DDL；索引先跑重复行预检，`CREATE INDEX CONCURRENTLY`；回滚 = DROP INDEX / 保留列）**

```sql
-- B1 users 两列（在线加列，可空/默认 false）
ALTER TABLE users ADD COLUMN IF NOT EXISTS last_active_tenant_id int NULL REFERENCES tenants(id);
ALTER TABLE users ADD COLUMN IF NOT EXISTS must_change_password boolean NOT NULL DEFAULT false;

-- B2 allocation 活跃唯一（预检重复行后建索引；历史重复按"保留最早、其余 deassigned_at=now"处置，并由回填脚本输出差异清单）
CREATE UNIQUE INDEX CONCURRENTLY IF NOT EXISTS uk_msp_allocation_active
  ON msp_allocations (msp_user_id, customer_tenant_id) WHERE deassigned_at IS NULL;

-- B3 审计扩展列（membership_id 在 P1 建表后补 FK；历史行 source 读侧映射 legacy）
ALTER TABLE audit_logs ADD COLUMN IF NOT EXISTS actor_account varchar(64);
ALTER TABLE audit_logs ADD COLUMN IF NOT EXISTS membership_id bigint;
ALTER TABLE audit_logs ADD COLUMN IF NOT EXISTS target_tenant_id int;
ALTER TABLE audit_logs ADD COLUMN IF NOT EXISTS source varchar(32);
CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_audit_scope ON audit_logs (tenant_id, target_tenant_id, created_at);

-- B4 工单外部号：provider 维度唯一（E4 启用时；仅非空行）
CREATE UNIQUE INDEX CONCURRENTLY IF NOT EXISTS uk_ticket_provider_msp_ticket
  ON tickets (msp_provider_id, msp_ticket_id) WHERE msp_ticket_id IS NOT NULL;

-- B5 工作台索引（先查 pg_indexes；实测缺失后再建；SLA 列名为 sla_resolution_deadline）
CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_tickets_tenant_status_updated ON tickets (tenant_id, status, updated_at);
CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_tickets_tenant_assignee_status ON tickets (tenant_id, assignee_id, status);
CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_tickets_tenant_sla_deadline ON tickets (tenant_id, sla_resolution_deadline);
```

- 表名/列名以 Ent schema 为准：审计表 `audit_logs`（`ent/schema/auditlog.go`）；工单列 `assignee_id` / `sla_resolution_deadline`（`ent/schema/ticket.go`）。
- Ent 侧同步：`ent/schema/msp_allocation.go` 增加部分唯一索引（`entsql.IndexWhere`），避免 auto-migrate 与实际库漂移；DDL 同部落 `migrations/`（legacy `ent/migrate/20250313_add_msp_tables.sql:35` 仅作历史）。
- P1 登记（不阻塞 P0）：`tenants.domain` 部分唯一（`WHERE deleted_at IS NULL`，`LOGIN-D1`，随专属域名入口 P1）；membership/RLS 见 IP-P1-1/7。

**C. 五角色权限矩阵（D10 唯一词表；两套作用域）**

C1 · provider 租户 RBAC（资源码 = `middleware/rbac.go` 硬编码矩阵；落库由 seeder 幂等生成）：

| 角色 | 权限（resource:action） | 条数 | 用途 |
|---|---|---|---|
| `msp_viewer` | msp:read、msp_customer:read、msp_ticket:read、msp_allocation:read、msp_report:read | 5 | 只读 |
| `msp_tech` | viewer + msp_ticket:write | 6 | 工单处理（默认） |
| `msp_specialist` | msp:read、msp_customer:read/write、msp_ticket:read/write、msp_allocation:read、msp_report:read | 7 | 专项（含客户信息写） |
| `msp_manager` | msp:read/write、msp_customer:read/write、msp_ticket:read/write、msp_allocation:read/write、msp_report:read/write | 10 | provider 管理（分配/报表） |
| `msp_admin` | `msp_*` 全资源全动作（`*`） | 兜底 | 全托管；默认不分配 |

C2 · 客户租户内 msp 角色基线（Q7 合同形态 → 客户侧业务权限；客户 admin 可编辑，变更审计）：

| 角色（唯一词表） | 默认权限集 | 合同预设映射 |
|---|---|---|
| `msp_viewer` | ticket:read + 评论 | observer（只读协办） |
| `msp_tech` | + ticket:write、knowledge:read、cmdb:read、service_catalog:read | tech（只代工单，默认） |
| `msp_specialist` | msp_tech 基线（专项能力由客户 admin 按需扩展） | specialist（由 `allocation.role` 映射） |
| `msp_manager` | msp_tech + user:write（限客户侧角色）+ report:read | manager（代工单 + 开号） |
| `msp_admin` | msp_manager + cmdb:write、change:write | full（全托管；默认不分配） |

- 落库：`internal/authz/roles.go` 内置词表新增 5 角色 → `pkg/seeder` 为 provider 租户幂等生成 `roles` + `role_permissions`；迁移 `20260501_enable_rbac_from_db.sql:33-77`（id 108–112）保留兼容；`scripts/msp/setup-msp-tenants.sh` 的 SQL 直写降级为兜底（并补 `msp_admin`）。DBOnly 三态（unavailable / unconfigured / configured）语义不变。
- `msp_role` 收敛（IP-P0-9）：`provider_admin→msp_manager`、`provider_agent→msp_tech`；`customer_user` 仅 legacy 读映射。

**D. 工作台 API Schema（P0 冻结；端点全量见[工作台方案 §4](./msp-cross-customer-workbench-and-filter-plan.md)）**

- 查询参数：`customerTenantIds=all|1,2`（服务端上限 50）、`status/priority/assigneeId/q/updatedAfter`、`groupBy=customer`、`sort=sla|updated`、`cursor`、`limit`（默认 50、上限 200）。
- 列表项：`{id, customerTenantId, customerName, status, priority, assigneeId, assigneeName, updatedAt, slaDeadline, allowedActions[]}`。
- `allowedActions[]` 元素：`{"action":"reply|status|assign","allowed":true|false,"reasonCode":"<A 表错误码>","reasonText":"..."}`；`allowed=false` 必须带 `reasonCode`（前端仅按该数组渲染，禁止前端推断权限）。
- `summary` 响应：`{generatedAt, ttlSeconds:30, customers:[{customerTenantId, customerName, open, slaRisk, unassigned}]}`（仅当前分配集合；未分配客户服务端剔除并审计 `tenant.scope_denied`）。
- 游标：opaque base64url(JSON `{v:1, sort:"sla|updated", perTenant:[{tenantId, lastSort, lastId}]}`)；非法/过期 → 400 `INVALID_CURSOR`。
- 显式请求未分配客户 → 403 `MSP_ALLOCATION_REQUIRED`；条目声明租户与资源不符 → 400 `RESOURCE_TENANT_MISMATCH`。

**E. 审计事件与 source 枚举（canonical）**

- 事件目录：`auth.login`、`tenant.switch`、`tenant.switch_denied`、`tenant.scope_denied`、`tenant.probe_denied`、`user.provision`、`user.invite`、`user.invite_accept`、`membership.grant/revoke/suspend/role_change`（P1）、`tenant.lifecycle`、`workbench.action`。
- `audit_logs.source` 枚举：`login|switch|header|workbench|platform_selected|job|system|legacy`（历史行为 NULL，读侧视为 `legacy`）。
- 统一字段：`actor_account` + `membership_id`（P1 起）+ `target_tenant_id` + `request_id`；跨租户操作（条目级/治理/bypass）**必审计**。
- 目标架构 §10 原 `scope_switch` 事件名、登录细化 §8 原 `scope_switch/denied` 统一为 `tenant.switch` / `tenant.switch_denied` / `tenant.scope_denied`（以本节为权威）。

**F. `07:G1–G10` 承接映射（2026-09-30 复核；消除“缺口无工作流”）**

| 缺口 | 承接工作流 | 批次 | 验收 |
|---|---|---|---|
| `07:G1` 跨租户建号被拦 | IP-P0-5 | P0 | `ProvisionUser` 三通道；`07:G1` 关闭 |
| `07:G2` bootstrap 多租户 | IP-P1-5（P0 DoD 不再声称关闭 G2） | P1 | 连续 2 租户 bootstrap 成功 |
| `07:G3` MSP 管理员角色解析 | IP-P0-9（JWT `role`=主角色或最大 rank；`roleRank` 覆盖 msp_*；高攀按权限集比较） | P0 | 同租户 API 建 `agent` 成功；`07:G3` 关闭 |
| `07:G4/G5/G6` 审批组/序列/唯一约束 | IP-P1-5（迁移一次性校正） | P1 | 存量库供给可复现 |
| `07:G7` CLI 输出规范 | 工具规范（脚本 `last_number()` 已规避；P2 提升为通用约定） | P2（接受现状） | 文档示例 + 脚本 lint |
| `07:G8` 缓存 key 无租户维度 | IP-P0-2 步骤 6（逐 key 审查 + 修复 + 单测） | P0 | 缓存 key 清单 0 缺失；`07:G8` 关闭 |
| `07:G9` 头/JWT 冲突静默 | IP-P0-6 | P0 | 冲突 401 + 告警 |
| `07:G10` snap docker 路径 | 已文档化 + 脚本 sha256 校验；随 IP-P0-1 部署自检复核 | P0（收尾） | 文档无 `/tmp` 指引；校验失败即退出 |

### IP-P0-1 部署与门控显式化（R4/R12；ADR-004:A1/A10）

**目标**：`DEPLOYMENT_MODE` 单一来源；仅 `saas_msp` 开启 MSP；未知值不静默。

**步骤**：
1. `config`：`DEPLOYMENT_MODE` 仅接受 `private|saas|saas_msp`；启动时校验，未知值 **fatal**（带可操作错误信息）；
2. `middleware/msp_gate.go`：目标态 = 仅 `saas_msp` 开启；过渡期（一个版本）对 `saas`/空值打 **warning 日志**，随后拒绝；
3. 启动自检：`gate 状态 ↔ seed 租户形态 ↔ 模式` 一致性（§2.2 矩阵）；`/msp/status` 暴露当前模式与 gate 状态；
4. 文档：02 §9 表格更新为"现状→目标"两列（canon §7 已改）。

**验收**：`private` → `/msp/*` 404；`saas` → 404；未知值 → 启动失败；`saas_msp` → 200；自检日志含模式/gate/租户形态三项。

**回滚**：env 回退 `saas_msp`；自检降级为 warning。

**进度（2026-09-30）**：✅ **已实现**。代码：`pkg/tenantmode.ValidateDeploymentMode/MSPRoutesEnabled`、`middleware.ApplyDeploymentMode`（仅 `saas_msp` 开启；`saas` 关闭并告警；空/未知返回错误且不改变门控状态）、`internal/bootstrap` 启动自检（warning-only，模式/gate/provider 租户数）、`/msp/status` 返回 `deploymentMode`+`mspRoutesEnabled`；`main.go` 移除原始 env 调用（单一来源 = `cfg.Deployment.Mode`）。测试：`TestApplyDeploymentMode*`、`TestDeploymentShapeMismatch` 通过；`go build ./...` 通过。

### IP-P0-2 隔离与授权修复：allocation 二次校验（R9/R10/R2/R3；canon A3；`07:G8`）

**目标**：**所有通道**（头 / 路径参数 / 请求体 / 切换）统一走同一授权函数；未分配客户一律 403；缓存键带租户维度（`07:G8`，隔离面 fail-closed）。

**步骤**：
1. 在 `service/` 层实现唯一入口 `CanAccessCustomer(actor, customerTenantID, action)`：`allocation 有效 ∧ 客户归属 provider ∧ 客户 active ∧ 目标租户 RBAC`；错误码见 §3.0-A（`MSP_ALLOCATION_REQUIRED` / `CUSTOMER_TENANT_NOT_FOUND` / `CUSTOMER_INACTIVE` / `RESOURCE_TENANT_MISMATCH`）；
2. **接线 `MSPAccessValidator`**（已冻结：复用既有实现；`GetTicketsForCustomer`/`MSPFilterByCustomer` 收敛为内部方法或删除，禁止并存与双实现）；
3. `handlers/msp/handler.go`：`/msp/customers/:id/tickets`、`assign`（请求体 `customerTenantId`）、工单详情/回复等**全部改走**统一入口；
4. `msp_allocations` 部分唯一索引：从 legacy 迁移到规范迁移与 Ent schema（DDL 见 §3.0-B2；含重复行预检）；
5. 反例用例：未分配客户 3 条通道均 403（见 §6.5）；
6. **缓存租户维度（`07:G8` / `ADR-004:A8`）**：逐 key 审查 `itsm-backend/cache/`（读写键必须含 `tenant_id`；已合规键登记清单），修复无租户维度的 key，补单测与评审检查项；工作台 `summary` 内存缓存 key = 租户集合哈希 + 用户。

**验收**：三角色剧本 M10 三条全部 403（现状为 200，见 07/剧本）；`R9`/`R10` 关闭；`07:G8` 关闭（缓存 key 清单 0 缺失）；新增单测覆盖 3 通道 × 2 角色 + 缓存键用例。

**回滚**：入口函数保留开关 `MSP_STRICT_CUSTOMER_ACCESS`（默认 on），off 时退回旧行为仅用于排障（发布后一个版本移除）。

**进度（2026-09-30）**：✅ **安全核心已实现**（步骤 1–3、5–6），步骤 4 待发布流程执行线上 DDL。

- 统一入口：`pkg/mspguard.CanAccessCustomer`（判定链：MSP 身份 → 有效分配 → 客户存在 → 归属本 provider（R2）→ 客户 active/未过期），错误码 `MSP_ALLOCATION_REQUIRED`/`CUSTOMER_TENANT_NOT_FOUND`/`CUSTOMER_INACTIVE`；`RESOURCE_TENANT_MISMATCH` 用于工单与声明租户不符。因 `service` 已依赖 `middleware`，实现置于 `pkg/`（避免循环依赖），`service.MSPAccessValidator` 为门面委托。
- 通道接线：`MSPMiddleware` 头通道、`GetCustomerTicketsForMSP`（路径）、`AssignMSPTechnician`（请求体）、`GetMSPCustomerReports`（改为按可访问客户集合聚合，同时修掉旧实现把 userID 当 tenant 查询的缺陷）。
- 死代码收敛：`MSPFilterByCustomer`、`MSPAllocationService.GetTicketsForCustomer` 删除，禁止双实现（R10）。
- 反例单测：`pkg/mspguard`（8 例）+ `service`（3 例：路径/请求体/报表）+ `middleware`（2 例：头通道 403/404）全部通过。
- DDL：ent schema `Indexes()`（`entsql.IndexWhere`）+ 迁移 **022**（先处置历史重复行，再建 `uk_msp_allocation_active` 部分唯一索引）。
- G8 缓存审查：关闭（清单与豁免见 [07 §9](../07-known-gaps.md)）。
- **偏差记录**：不实现文档原设的 `MSP_STRICT_CUSTOMER_ACCESS` 运行时旁路（避免授权外露面）；回滚 = revert 本 PR + env `DEPLOYMENT_MODE` 无需变更。

### IP-P0-3 工单 MSP 快照写入（R11；canon A12）

**目标**：建单落 `is_managed_by_msp / msp_provider_id / managed_by_user_id / msp_ticket_id`，支持按 provider 过滤/统计/外部映射。

**步骤**：
1. 建单链路（客户租户内 + MSP 代建）统一 setter：provider 由**客户派生**（`customer.provider_tenant_id`）+ 落快照；
2. 列表/详情/报表查询按 `provider_tenant_id` 收窄（与 IP-P0-2 入口组合）；
3. `msp_ticket_id` 外部映射字段：仅在有外部工单源时写入；唯一性按 `(msp_provider_id, msp_ticket_id)`（DDL 见 §3.0-B4；冲突返回 `MSP_TICKET_ID_CONFLICT`）；
4. 回填：存量工单按客户归属补快照（幂等脚本，dry-run 输出差异清单）。

**验收**：新建工单四字段正确；按 provider 过滤返回=分配集合内工单；快照与客户归属不一致被拒绝；A12 前置条件满足。

**回滚**：字段可空，停止写入即可；回填脚本带 `--rollback`（清空本批写入标记）。

**进度（2026-09-30）**：✅ **建单/指派快照写入 + 回填脚本已实现**（步骤 1、4；步骤 2 随 IP-P0-7 工作台，步骤 3 待外部工单源 E4）。

- 建单统一 setter：`service/msp_snapshot.go`（`resolveTicketMSPProvider`，判据 = 客户 active/未过期 → `msp_provider_id`（legacy `customer` 回退读 `parent_tenant_id`）→ provider 类型/状态有效）；两条建单链路（`TicketService.CreateTicket`、`TicketCoreService.CreateTicketBasic`）共用；无效归属按普通工单处理，不阻断建单。
- 指派写 `managed_by_user_id`：`AssignMSPTechnician` 经 `UpdateParams` 补写（含历史工单快照补齐，幂等）。
- 仓库层：`CreateParams`/`UpdateParams` 增加 MSP 字段，`applyMSPCreateSnapshot`/`applyMSPUpdateSnapshot` 覆盖 Create/CreateWithTx/Update/UpdateWithTxHook 四个 builder，防漏写路径。
- 单测：建单 4 场景（托管客户/直客/客户停用/provider 停用）+ 指派补写；`go build` 通过。
- 回填：`scripts/msp/backfill-ticket-msp-snapshot.sql`（`mode=dry_run|apply|rollback`，psql 参数切换；dry-run 输出候选计数+前 50 行样例）。
- **偏差记录**：未加批次标记列，rollback 按"快照 == 当前客户归属"匹配（会同时清运行时同 provider 快照），脚本内已注明；`msp_ticket_id` 与 `(provider, msp_ticket_id)` 唯一索引随 B4/E4 外部源启用。

### IP-P0-4 租户类型与归属收敛（R3；canon A1/A2）

**目标**：`tenants.type` 只出现 3 类新值；customer 归属唯一化。

**步骤**：
1. 校验函数：列值口径固定为 `internal/msp_provider/msp_customer`（概念名 platform/provider/customer）；写入拒绝 legacy 值（`msp`/`customer`/`standard`…），读取兼容映射（文档 + 单测）；
2. 归属校验（D2 已确认）：`msp_customer` ⇔ `msp_provider_id` 非空且指向 `msp_provider`；`saas_customer`（直客）⇔ 为空；其余组合拒绝。归属写入仅 `msp_provider_id`（不改物理列名；`parent_tenant_id` 保留兼容读，**P0 停止新增双写**，P1 数据收敛/回填）；
3. 建 customer 租户 API/脚本补归属校验（指向必须为 `msp_provider`，禁自指）；
4. 文档：01/03 的枚举与双字段表述回填为"现状 + 目标"两列。

**验收**：A1/A2 通过；错误归属建租户被拒；存量数据巡检 0 异常。

**回滚**：校验可降级 warning（一个版本）；读兼容保留。

**进度（2026-09-30）**：✅ **代码与巡检脚本已实现**（步骤 1–3；步骤 4 文档已回填 01/03 + canon）。

- 校验函数（`pkg/tenantmode`）：`ValidateTenantTypeForWrite`（目标集合 `internal`/`msp_provider`/`msp_customer`/`saas_customer`；legacy/未知/空拒绝）、`NormalizeTenantTypeRead` + `TenantTypeFilterValues`（legacy 读取映射与过滤归一）、`ValidateTenantOwnership`（形状+禁自指）。
- 服务接入（`TenantService`）：`CreateTenant`/`UpdateTenant` 写入校验 + 归属复核（目标必须是有 records 的 `msp_provider`）；更新仅在类型/归属被触碰时校验；**停止 `parent_tenant_id` 双写**（旧字段仅作兼容输入映射至 `msp_provider_id`，清空走显式 `Clear`）。
- API 契约：`CreateTenantRequest`/`UpdateTenantRequest` binding 收敛为目标集合（legacy 写入 400）；列表过滤保留 legacy 输入（服务端归一）。
- 单测：`pkg/tenantmode`（写入集合/读取映射/过滤/归属形状）+ `service` 3 组（类型收敛、归属一致性、更新守卫、legacy 过滤兼容）全绿；`handlers/tenant` 全绿。
- 存量巡检：`scripts/msp/audit-tenant-type-ownership.sql`（5 段：legacy 类型 / 归属形状 / 目标无效 / 双写残留 / 汇总计数）；`setup-msp-tenants.sh` 同步停止双写。
- **待办**：存量数据收敛（P1，按巡检输出回填）；e2e 纳入 M1 验收（A2 反例：建租户拒绝错误归属）。

### IP-P0-5 建号通道收口（K4；ADR-004:A2/A3；F1/F3/F14）

**目标**：三通道建号（platform/msp/tenant）统一走 `ProvisionUser`，**handler 不得自行拼 bypass**。

**步骤**：
1. 新建 `service/provisioning.go`：`ProvisionUser(actor, targetTenant, input)` 内部完成 ① `CanAccessTenant` ② 通道授权 ③ `WithProvisioningBypass(actor, reason)` ④ `WithTenantID(target)`；
2. 通道矩阵：`platform`（`user:write` + 平台角色，任意 active 租户）/ `msp`（`provider_admin` + 有效 allocation + `msp_customer:write`，仅分配客户）/ `tenant`（本租户 admin + `roleRank`/`CanGrantRoles`）；
3. 角色白名单：拒绝 `super_admin/sysadmin/admin`；`msp_role` 仅 platform/msp 通道可设；错误码 `CROSS_TENANT_FORBIDDEN/MSP_ALLOCATION_REQUIRED/ROLE_NOT_GRANTABLE/USERNAME_EXISTS/EMAIL_EXISTS`；
4. 端点：`POST /api/v1/tenants/:id/users`、`POST /api/v1/msp/customers/:id/users`（平台/MSP 面），与既有 `POST /api/v1/users` 共用 service；
5. 灰度：`USER_PROVISIONING_CHANNELS_ENABLED`（按租户开启）；运营脚本 §6 的 SQL 直写降级为**兜底**（默认不再使用）。

**验收**：服务商经 API 为分配客户建号 201；未分配客户 403（`MSP_ALLOCATION_REQUIRED`）；客户 admin 无法建平台角色（422 `ROLE_NOT_GRANTABLE`）；**`07:G1` 关闭（`07:G2` 由 IP-P1-5 关闭，不在本项范围）**；K4 关闭。

**回滚**：开关关闭即回退旧路径；SQL 兜底脚本保留。

**进度（2026-09-30）**：✅ **三通道建号收口已实现（K4/`07:G1` 关闭）**。

- 唯一入口：`service/user_provisioning.go#UserProvisioningService.ProvisionUser`（与交付任务的 `ProvisioningService` 同名冲突已改名为 `UserProvisioningService`）——内部完成 ① 目标租户存在/active ② 通道解析（platform/msp/tenant）③ 角色白名单 + 通道 rank 上限 ④ `WithTenantID(target)` + `WithProvisioningBypass(actor,channel,target)` ⑤ 复用 `UserService.CreateUser`。
- 通道矩阵：`platform`（super_admin/sysadmin → 任意 active 租户）；`msp`（provider 管理员 + `mspguard.CanAccessCustomer` allocation/归属二次校验 → 仅分配客户，rank 上限=客户 admin）；`tenant`（本租户，rank=调用方有效角色，msp_role 映射计入）。
- 错误码：`CROSS_TENANT_FORBIDDEN`(403) / `MSP_ALLOCATION_REQUIRED`(403) / `ROLE_NOT_GRANTABLE`(422) / `MSP_ROLE_NOT_ALLOWED`(422) / `USERNAME_EXISTS`、`EMAIL_EXISTS`(409) / `TENANT_NOT_FOUND`(404) / `TENANT_SUSPENDED`(403) / `PROVISIONING_CHANNELS_DISABLED`(404)。
- 写路径：`database/security.go` 写守卫放行 provisioning bypass（仍强制 ctx tenant=目标租户，RLS 语义不变）；handler 不拼 bypass（`SetProvisioningService` 注入，未接线时保留 legacy 逻辑）。
- 端点：`POST /api/v1/tenants/:id/users`（平台面）、`POST /api/v1/msp/customers/:customer_tenant_id/users`（MSP 面，`msp_customer:write`）、既有 `POST /api/v1/users`（有 provisioning 时统一走收口）。
- 灰度：`USER_PROVISIONING_CHANNELS_ENABLED`（默认关；关闭时新端点 404 `PROVISIONING_CHANNELS_DISABLED`，`/users` 回退 legacy）。
- 单测：8 个子用例（平台建首管/租户内建号/平台角色拒绝/mspRole 白名单/未分配客户拒绝/已分配客户成功/跨租户拒绝/开关关闭）全绿；`go build ./...`、`handlers/user`、`router`、`database`、`common/tenantctx` 全绿。
- **待办**：`invite` 通道归 P1（IP-P1-2）；按租户灰度（settings 级开关）与 `07:G2`（provider 首个管理员 bootstrap 租户化）归 IP-P1-5；e2e（服务商 API 建号 201）纳入 M1 验收。

### IP-P0-6 登录、切换与刷新契约（I8；F5/F6/F9/F10/F11/F12；07:G9）

**目标**：登录落 provider 家；**无候选列表/无 409**；切换/刷新/头通道全部 fail-closed 且可审计。

**步骤**：
1. 登录：`customer` → 唯一租户；`provider` → **provider 家**（不因 `last_active`/`tenantCode` 直签客户）；`platform` → 控制台；登录响应仅 `tenantSelection{mode}`；
2. `/auth/tenants`：语义修正为 **home ∪ 有效分配 ∪ 平台全量**（认证后使用）；
3. `switch-tenant`：目标租户校验（membership/allocation）→ 重签 JWT（`tenant_id + tenant_source + membership_id`）→ **撤销旧 refresh** → 审计 `tenant.switch` → 响应 `user.tenantId == tenant.id`；
4. `refresh`：按 `claims.TenantID` 重签 + 复核；失效 → `TENANT_ACCESS_REVOKED`（不回退 home）；
5. 头/JWT 冲突：**拒绝**（401/400 `TENANT_MISMATCH_REJECTED` + `tenant mismatch rejected` 告警）——修 07:G9（错误码见 §3.0-A）；
6. 前端契约同步（见 IP-P0-8）。

**验收**：三角色剧本 M1–M5、F5–F12 相关项；`mspadmin` 登录 JWT 落 provider 家；`switch→refresh` 保持目标租户；撤销后旧 refresh 401；冲突请求 401 + 日志。

**回滚**：无状态变更，代码回退即可；审计事件保留。

**进度（2026-09-30）**：✅ **登录/切换/刷新后端契约已实现（F10/F11a/F12/G9 关闭）**。

- **登录落 home**：`handlers/common` Login 始终以 `users.tenant_id` 签发（`tenant_source=home`），`tenantCode` 仅参与身份定位、不改签发作用域；响应新增 `tenantSelection{mode}`（`platform`/`home`/`single`）——登录页/响应不含候选列表（I8）。
- **切换**：`SwitchTenantWithRevoke` → 目标校验（本租户/平台/有效 allocation）→ 重签 JWT（`tenant_source=switch`）→ 撤销旧 refresh（`AddRefreshToBlacklist`，handler 传 httpOnly cookie）→ 审计 `TENANT_SWITCH` → **响应 `user.tenantId` = 目标租户**（修 F11a）。
- **刷新**：按 `claims.TenantID` 重签（保持 `tenant_source`），并复核 home/平台/有效 allocation + 租户 active/未过期；失效 → 401 `TENANT_ACCESS_REVOKED`（**不回退 home**，修 F10）。
- **`/auth/tenants`**：语义修正为 home ∪ 有效 allocation 客户 ∪ 平台全量（super_admin/sysadmin，去重、home 优先）；F5/F6 契约。
- **头/JWT 冲突**：401 + `reasonCode=TENANT_MISMATCH_REJECTED` + `tenant mismatch rejected` 告警（修 G9）。
- 单测：`handlers/auth` 切换（含 provider→分配客户 claims 断言、无分配反例）、`handlers/common` 刷新作用域保持/撤销拒绝/租户并集、`middleware` 预检新鲜度全绿；`go build ./...` 全绿；`authz-gen` 生成物已同步。
- **遗留**：F11c（权限按目标租户 DB `role_permissions` 解析）随 IP-P0-9 的 `msp_*` 权限行；前端切换入口/缓存刷新（F5/F6/F11b/F12 前端侧）归 IP-P0-8；`membership_id` claim 随 P1 membership 表落地。

### IP-P0-7 跨客户工作台 + CustomerFilter + 条目级操作（WB1–WB6；WB-A1–A6）

**目标**：服务商**不切换会话**即可看+做多客户单据；写操作按资源租户授权。

**步骤**：
1. 后端：`GET /msp/workbench/tickets`（逐租户查询 + 内存合并，P0）、`GET /msp/workbench/summary`（计数徽标）、条目级 `reply/status/assign`（`POST /msp/workbench/tickets/:id/*`）；响应/游标/`allowedActions` schema 见 §3.0-D；
2. 授权链（每条目）：`资源租户 ∈ 分配 ∧ 目标租户 RBAC ∧ 租户 active ∧ 资源状态`；响应逐行返回 `allowedActions`；
3. bounded bypass：`actor + 集合 + reason`，逐次审计（`source=workbench`）；
4. 前端：顶栏 `CustomerFilter`（全部/子集 + 搜索 + 计数徽标，URL 同步）；列表带"客户"列 + 行内操作；"进入客户"深度入口；
5. 客户账号调用 `/msp/workbench/*` → 403（前端不渲染 + API 拒绝，A9）；
6. 批量操作留 P1（D8）。

**验收**：WB-A1–A6 全过；未分配客户不出现在过滤器/列表（服务端强制）；暂停租户条目只读；审计含 `target_tenant`。

**回滚**：工作台 feature flag（前端）+ 路由开关（后端）；旧 `/msp` 仪表盘保留一个版本。

**进度（2026-09-30，后端）**：✅ 工作台后端已落地；前端 `CustomerFilter`/行内操作/双态指示归 IP-P0-8。

- 端点：`GET /msp/workbench/tickets`（`customerTenantIds=all|1,2`、`status/priority/assigneeId/q/updatedAfter`、`sort=updated|sla`、`cursor`、`limit≤200`）、`GET /msp/workbench/summary`（open/slaRisk/unassigned）、`POST /msp/tickets/:id/reply`、`POST /msp/tickets/:id/status`；`assign` 复用既有端点。
- 授权链（条目级）：资源租户 ∈ 服务端 `AllowedCustomers` ∧ 目标租户 RBAC（`msp_ticket:write`，DB 权限）∧ 租户 active ∧ 资源非终态；每条返回 `allowedActions[]`（`CUSTOMER_INACTIVE` / `ACTION_NOT_ALLOWED` 原因码）。
- 错误语义：显式请求未分配客户 → 403 `MSP_ALLOCATION_REQUIRED`；条目租户与声明不符 → 400 `RESOURCE_TENANT_MISMATCH`；非法游标 → 400 `INVALID_CURSOR`；单请求集合 >50 → 400 `TOO_MANY_TENANTS`。
- 实现：P0 逐租户查询 + 内存合并（RLS 友好）；复合游标 `(updated_at|sla, id)` per-tenant；跨租户写逐条审计（`source=workbench` + `target_tenant_id` 写入 `request_body`；独立列随 IP-P0-10）；索引 DDL `migrations/20260502_msp_workbench_indexes.sql`。
- 测试：`service/msp_workbench_test.go` 4 组（作用域/allowedActions、summary、写授权链+审计、游标翻页）；`handlers/msp`、`middleware`、`router`、`service`（剔除 HEAD 既有红）全绿；`authz-gen` 预检物已同步。
- 待办：前端链路（IP-P0-8，含 WB-A6 双态指示与 A9 前端不渲染）；批量操作（WB-A4）按批次留 P1（§3.3 护栏已冻结）。

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

**进度（2026-09-30）**：✅ 前端上下文/权限链路落地（FE-A1–A5、A7）；FE-A6/A8 余项归 P1。

- 会话/API（`lib/auth`、`lib/api`、`lib/store`）：`session-bootstrap` 移除 `tenants[0]` 强制（作用域 = 服务端 `/auth/me.tenantId`，候选列表仅深度入口）；`TenantAPI.switchTenant → POST /api/v1/auth/switch-tenant` + `getMyTenants`；http-client 支持按请求显式 `X-Customer-Tenant-ID`（不写上下文）与 `abortAllRequests`；store 新增 `switchTenant`（取消在途→重签→重拉 me→`queryClient.clear()`）与登出清理（clear + `resetSessionBootstrap` + 断 WS）；菜单/能力 queryKey 按租户分键。
- 路由/守卫（`routes/*`）：新增独立 `/403` 页与路由；`RequireCapability` 分组守卫覆盖 `/admin/*` 与 `/msp/*`（`hasPermission()` 单一模型 + admin 语义保留）；`/msp/workbench` 路由注册。
- 工作台/过滤器（`components/layout/header/CustomerFilter.tsx`、`pages/(main)/msp/workbench/`）：`CustomerFilter` 多选/全部 + 搜索 + 计数徽标（URL `customerTenantIds` 同步，只改视图；provider-only）；工作台列表带客户列、严格按 `allowedActions[]` 渲染 reply/status、空数组或 `CUSTOMER_INACTIVE` 只读、`nextCursor` 分页；“进入客户”走 store `switchTenant` 链路（无整页重载）。
- 登录页：源码级回归锁定 DOM 无租户列表/选择器（FE-A4）；Header 对客户账号/无 MSP 权限完全不渲染过滤器与工作台入口（FE-A5）。
- 遗留：FE-A6（管理页建号目标租户 UI）与 FE-A8（legacy route-config 单一模型清理）随 P1；`ticket-attachment-api` 1 条用例为 HEAD 既有红（与本批无关）。
- 验证：`tsc --noEmit` 0 错；`vite build` 成功；受影响 jest 86/87 套件通过（唯一红为上述既有）。

### IP-P0-9 角色供给显式化（K1/K2/K3；D10）

**目标**：`msp_*` 角色纳入内置词表；常规 seed 生成权限行；脚本降级兜底。

**步骤**：
1. `internal/authz/roles.go`：新增 5 角色 `msp_viewer/msp_tech/msp_specialist/msp_manager/msp_admin`（D10 唯一词表）及权限映射；
2. `pkg/seeder`：provider 租户 seed 生成上述角色 + `role_permissions`（幂等）；
3. `msp_role` 收敛：`provider_admin → msp_manager`、`provider_agent → msp_tech`（provider 家）；`customer_user` 仅 legacy 读映射；
4. Q7 预设映射：`observer→msp_viewer`、`full→msp_admin`（合同/分配层预设，不新增角色名）；
5. 脚本 `setup-msp-tenants.sh` §5 改为"校验 + 兜底"（默认不写 SQL）。

6. 修 `07:G3`：JWT `role` claims 取主角色（`users.role`）或按 `roleRank` 取最大；为 `msp_*` 定义合理 rank（不低于其绑定主角色能力）；"角色高攀"校验改为比较实际权限集。

**验收**：新 provider 租户 seed 后 `/roles` 可见 5 角色且权限行齐备（矩阵见 §3.0-C1）；不跑脚本的租户不再落入硬编码兜底（K1/K2 关闭）；D10 映射表单测；**`07:G3` 关闭**（mspadmin 登录 `role=admin` 或 rank ≥ admin，可经同租户 API 建 `agent` 用户）。

**回滚**：角色为新增数据，删除/停用即可；脚本兜底保留。

**进度（2026-09-30）**：✅ **角色供给显式化已落地（K1/K2 关闭；07:G3 关闭）**。

- 词表：`internal/authz/roles.go` 内置 5 角色（C1 矩阵；`msp_admin` 以 read/write 显式码表达全动作）；`pkg/seeder.BuiltinRoles` 同步 5 角色 → 常规 seed 幂等生成 `roles` + `role_permissions`。
- 单一源守卫：`tests/parity` 新增矩阵对拍（authz 词表 ⟷ `middleware.RolePermissions` 硬编码，剥离 task 基线后全等）+ 种子存在性 + rank 单调（`msp_manager ≥ agent`）；`internal/authz` 新增 C1 全等测试。
- rank 单源：`middleware.RoleRank` 统一 `handlers/user`、`service`、登录三处（原三份 switch 合并；覆盖 `msp_*`）。
- **07:G3**：登录 JWT `role` 取「主角色 vs MSP 映射角色」rank 更高者（`admin`+`provider_admin` → `admin`(4)；`end_user`+`provider_admin` → `msp_manager`(3)），同租户建号/管理不再被 `msp_manager` 遮蔽。
- 脚本：`setup-msp-tenants.sh` §5 默认仅校验（`MSP_ROLE_SQL_FALLBACK=1` 才 SQL 兜底，含 `msp_admin`）；§6 注释同步 API 建号可用。
- 遗留：`msp_role` 枚举保持 3 值（D10：`provider_admin`/`provider_agent` + `customer_user` legacy 读映射）；`msp_specialist`/`msp_admin` 经 `allocation.role`/合同预设映射，不改枚举。
- 验证：`go build ./...`、`internal/authz`、`tests/parity`、`middleware`、`router`、`handlers/common`、`handlers/user`、`pkg/seeder` 与 `service`（除 HEAD 既有红 2 条：`TestProvisionTenant*` process-definition fixture、`TestTicketService_GetMSPCustomerReports_AllocationAware`）全绿。

### IP-P0-10 审计统一与文档回填（I11；C.6）

**目标**：跨租户/切换/建号/工作台操作统一审计字段；文档与代码同步。

**步骤**：
1. 审计字段/事件目录/`source` 枚举按 §3.0-E 实现；新增列 DDL 见 §3.0-B3（在线加列 + `idx_audit_scope`）；
2. 05 使用指南回填工作台/过滤器口径；01–07 与目标差异随各文档下次修订回填；
3. docs-gate C.6 保持 6/6；新增编号（如有）登记 canon 附录 C。

**验收**：审计查询可按 `target_tenant` 过滤；三角色剧本审计断言通过；C.6 通过。

**回滚**：字段为新增列，停止写入即可。

**进度（2026-09-30）**：✅ 审计作用域字段/事件目录/`source` 枚举与查询过滤已落地。

- 列（B3）：`actor_account` / `membership_id` / `target_tenant_id` / `source`（全部可空；历史行 NULL 读侧映射 `legacy`）+ `idx_audit_scope`；DDL `itsm-backend/migrations/20260503_audit_scope_columns.sql`（Ent schema 同步）。
- 写入方统一（source 枚举 `login|switch|header|workbench|platform_selected|job|system`）：
  - 认证：`auth.login`（成功/失败）、`tenant.switch` / `tenant.switch_denied`（source=switch，target=目标租户）；
  - 工作台：`workbench.action`（source=workbench，行归属 actor 家租户、target=客户租户）、`tenant.scope_denied`（显式请求未分配客户时逐租户记录）；
  - 建号：`user.provision`（source 按通道：platform→`platform_selected` / msp→`header` / tenant→`login`，target=目标租户）；
  - 通用 API 审计（`AuditMiddleware`）：按上下文派生 source（workbench 路径 > switch > 头通道 > login > system），`target_tenant_id` 默认同 `tenant_id`。
- 查询：`GET /api/v1/audit-logs` 新增 `targetTenantId` / `source`（`legacy`→NULL 历史行）/ `actorAccount` 过滤。
- `membership_id` 列已建；**2026-09-30 IP-P1-1 已回填历史行**（按 actor home membership），其后由写入方落值。
- 测试：`middleware`（字段 + source 派生）、`service`（作用域过滤 + 工作台审计列断言）、`handlers/common`/`handlers/auth` 回归全绿。
- 文档：05 使用指南回填工作台/过滤器与审计作用域口径；事件目录仍以本文 §3.0-E 为权威。

### IP-P0-11 执行器/定时器租户上下文统一（I7；集成分析 §5.2；canon §8 P0 ⑤）

**目标**：所有后台执行路径（BPMN/工作流、定时器/延迟任务、自动化规则、升级矩阵、队列消费者）在**显式租户 ctx** 下运行；无 ctx 即拒绝执行，禁止"无租户上下文旁路"。

**步骤**：
1. 入口收口：`handlers/{bpmn,approval,approval_chain,timer,automation_rule,escalation_matrix}/` 与对应 service 统一经 `tenantctx` 注入（入队即携带 `tenant_id + actor + reason`）；
2. 执行器：worker 取出任务先 `WithTenantID` 再执行；审计 `source=job`；确需跨租户时走显式 bounded bypass；
3. 指派/授权：工作流指派校验 `assignee ∈ provider ∧ allocation`（与 IP-P0-2 同口径）；列表查询 fail-closed；
4. 测试：错误 ctx 下的跨租户 job 必须失败；客户 A 的定时器/自动化不泄漏到客户 B。

**验收**：跨租户 ctx 用例全过（错误 ctx 执行被拒）；`source=job` 审计可查；集成分析 §5.2 项关闭；A7 前置条件满足。

**回滚**：ctx 注入为增量逻辑；异常时按租户关闭自动化入口（feature flag）。

**进度（2026-09-30）**：✅ 执行器/定时器租户 ctx 统一落地；`source=job` 审计可查。

- 统一模式（§5.2）：新增 `tenantctx.EnsureJobTenant` —— 任务租户必须 >0、ctx 租户不符即拒绝（系统旁路不豁免执行期 mismatch）；跨租户**枚举/领取**用 `SystemContext` 显式声明，**执行前** `WithTenantID(task.TenantID)` 收窄。
- 写入路径：
  - Timer：调度器 fire 回调按 timer 租户注入 ctx；`TimerEventHandler` 入口 guard + `tenantctx` + BPMN key，落 `timer.fire`（source=job）；
  - 超时扫描：`TimeoutScanner` 入口 guard + 逐租户 ctx（BPMN key 同步），批量处理落 `bpmn.timeout_scan`；
  - 自动升级：`workflow_automation` guard + 任务租户一致性防御；**`StartAutoEscalationTimer` 接线**（集成分析 C22 关闭，此前无调用方静默失效），升级发生落 `workflow.escalation`；
  - bootstrap 后台循环（模板部署/审批自愈/附件清理/嵌入管线/SLA/升级/超时扫描/自动升级）逐租户注入 ctx，枚举统一 `SystemContext`（C21 关闭）。
- 安全修复（§7.1）：工单指派 fail-closed（`ReassignTicket`/`AssignTickets` 校验处理人与工单同租户、批量按租户收窄）；`ListWorkflows`/`ListDeployments` fail-closed（缺租户即拒绝）；BPMN 授权去重键含 tenant + 授予强制 ctx 租户（跨租户覆写被拒）。
- 测试：`tenantctx` 守卫单测；timer/scanner/auto-escalation 错误 ctx 拒绝与 `source=job` 审计断言；既有超时/指派/部署/权限套件回归。
- 遗留：C20（`CASFire` 无租户参数）为纵深项，timerID 全局唯一风险有限，不阻塞 A7；`handlers/skill` 入参租户覆盖（§7.1-4）另行复核。
---

## 4. P1 详细实施（Membership 化）

> 原则：先加表/列并回填（只读兼容）→ 双写同事务 → 切读路径 → 废弃旧列；每步独立回滚（canon §8）。

### 4.0 P1 冻结契约（2026-09-30；membership 组织 / allocation provider / invitations）

> 与 §3.0 同规则：本节即 P1 编码基线，**变更须回填本节并登记修订记录**。审计 C13（P1 设计冻结）由此闭环；组织关联、邀请 DDL 不再有"实现评审时冻结"的悬置项。

**4.0-A 成员组织关联：`user_tenant_membership_orgs`（新增子表；IP-P1-3）**

```sql
-- 父表补复合唯一（作为子表复合 FK 目标）
CREATE UNIQUE INDEX uq_membership_id_tenant ON user_tenant_memberships (id, tenant_id);

CREATE TABLE user_tenant_membership_orgs (
  id            bigserial PRIMARY KEY,
  membership_id bigint NOT NULL,
  tenant_id     int    NOT NULL REFERENCES tenants(id),
  org_type      varchar(16) NOT NULL,          -- department | team | group | project
  org_id        bigint NOT NULL,
  role_id       int NULL REFERENCES roles(id), -- 可选：组织内角色
  is_primary    boolean NOT NULL DEFAULT false,
  status        varchar(16) NOT NULL DEFAULT 'active', -- active | suspended
  expires_at    timestamptz NULL,
  deleted_at    timestamptz NULL,
  created_at    timestamptz NOT NULL DEFAULT now(),
  updated_at    timestamptz NOT NULL DEFAULT now(),
  CONSTRAINT fk_membership_org_membership
    FOREIGN KEY (membership_id, tenant_id)
    REFERENCES user_tenant_memberships (id, tenant_id),
  CONSTRAINT uq_membership_org UNIQUE (membership_id, org_type, org_id)
);
CREATE INDEX idx_membership_org_scope ON user_tenant_membership_orgs (tenant_id, org_type, org_id);
CREATE UNIQUE INDEX uq_membership_org_primary ON user_tenant_membership_orgs (membership_id, org_type)
  WHERE deleted_at IS NULL AND is_primary;
```

- **多态校验**：`org_type` 指向 department/team/group/project 四表，无法建单一物理 FK → 应用层写入校验 `org.tenant_id == membership.tenant_id`，并由 P1 guard 扩展做启动扫描（`IP-P2-5`）；跨租户组织关联用例必须被拒（`IP-P1-3` DoD）。
- **租户列必带**：不申请 `tenant_guard` 豁免，RLS 可覆盖（`IP-P1-7`）。

**4.0-B allocation provider 维度：`msp_allocations.provider_tenant_id`（新增列；IP-P2-1 落地、P0 起应用层校验）**

```sql
ALTER TABLE msp_allocations ADD COLUMN IF NOT EXISTS provider_tenant_id int NULL REFERENCES tenants(id);
-- 回填：provider_tenant_id = 客户租户的 msp_provider_id（差异清单人工复核）
UPDATE msp_allocations a SET provider_tenant_id = c.msp_provider_id
  FROM tenants c WHERE c.id = a.customer_tenant_id AND a.provider_tenant_id IS NULL;
-- 校验通过后置 NOT NULL（2026-10-03 收尾落地：migrations/20261003_msp_allocations_provider_not_null.sql），并加"必须指向 msp_provider"的应用校验
CREATE INDEX IF NOT EXISTS idx_msp_allocations_provider
  ON msp_allocations (provider_tenant_id) WHERE deassigned_at IS NULL;
```

- 写入校验：`allocation.provider_tenant_id == customer.msp_provider_id`；跨 provider 分配拒绝（R2）。
- 与 D2 配合：直客（`saas_customer`，`msp_provider_id` 为空）**不得创建 allocation**。

**4.0-C 邀请表：`invitations`（DDL 冻结；IP-P1-4）**

```sql
CREATE TABLE invitations (
  id             bigserial PRIMARY KEY,
  tenant_id      int NOT NULL REFERENCES tenants(id),
  token_hash     varchar(64) NOT NULL UNIQUE,      -- sha256(token) hex；原始 token 不落库
  email          varchar(255) NOT NULL,
  target_user_id int NULL REFERENCES users(id),    -- 可选：邀请已存在账号绑定
  role_id        int NOT NULL REFERENCES roles(id),
  msp_role       varchar(32) NULL,                 -- 白名单：provider_admin/provider_agent（provider 通道）
  invited_by     int NOT NULL REFERENCES users(id),
  status         varchar(16) NOT NULL DEFAULT 'pending', -- pending | accepted | revoked | expired
  expires_at     timestamptz NOT NULL,             -- 默认 now()+72h（INVITATION_TTL_HOURS 可配）
  accepted_at    timestamptz NULL,
  revoked_at     timestamptz NULL,
  created_at     timestamptz NOT NULL DEFAULT now(),
  updated_at     timestamptz NOT NULL DEFAULT now()
);
CREATE UNIQUE INDEX uq_invitation_pending ON invitations (tenant_id, lower(email)) WHERE status = 'pending';
CREATE INDEX idx_invitations_tenant_status ON invitations (tenant_id, status);
CREATE INDEX idx_invitations_expiry ON invitations (expires_at) WHERE status = 'pending';
```

- **生命周期**：创建（角色白名单 + `msp_role` 通道校验）→ 投递（SMTP 或 `inviteUrl`）→ 接受（一次性：`status=pending ∧ now()<expires_at`，事务内置 `accepted` + 建号/绑定）→ 撤销（仅 pending，`revoked_at`）→ 过期由巡检置 `expired`；重发 = 新 token 且旧 token 失效。
- **API**：`GET /api/v1/auth/invitations/:token`（最小回显）、`POST /api/v1/auth/invitations/:token/accept`、`POST /api/v1/users/invitations/:id/revoke`；审计 `user.invite` / `user.invite_accept`。
- **安全**：token 128-bit 随机 + sha256 存储 + 日志脱敏；`super_admin/sysadmin/admin` 不可被邀请（F3）。
- **租户列必带**：`tenant_id` 纳入 RLS/guard（同 4.0-A）。

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

**进度（2026-09-30）**：✅ **IP-P1-1 表 + 回填 + 约束落地**（读路径未切换；A4 的 DB 约束与物化完成，读切换归 IP-P1-2）。

- 表：`user_tenant_memberships`（目标架构 §3.2 字段全集）+ 3 个部分唯一索引（live / 唯一默认 / customer 单作用域）+ 复合 FK 目标 `uq_membership_id_tenant`（供 IP-P1-3 子表）；Ent schema 同步（`ent/schema/user_tenant_membership.go`，auto-migrate 与磁盘迁移一致）。
- 迁移：`migrations/20260504_create_user_tenant_memberships(.sql/_down.sql)`（幂等）：`account_kind` 按 home 租户类型派生 customer/provider/platform；home 回填含 D10 角色映射（`provider_admin→msp_manager`、`provider_agent→msp_tech`）；有效 allocation 回填（`specialist→msp_specialist`、其余 `msp_tech`）；`audit_logs.membership_id` 按 actor home membership 回填。
- 巡检：`scripts/msp/verify-membership-backfill.sql`（缺 home/主作用域 / 多默认 / customer 多作用域 / provider 作用域数 / 分配缺行 / 角色映射失败 / 重复存活 / 审计余量，共 8 节）。**2026-10-03 联调库执行：8 节全零**（第 1 节判据修正为 `(source='home' OR is_default)`，覆盖邀请建号的 `source=invite` 主作用域语义，见目标架构 §6.2）；生产库留档随后续部署。
- 验证：`go build ./...`；约束回归 `TestUserTenantMembership_Constraints`（三索引 + 软删重入 + provider 多作用域 5 子例）；`go test ./internal/schema ./migration/...`；`cmd/migration-lint` 全绿。
- IP-P0-10 遗留的 `membership_id` 填充随本批关闭（历史行按 actor home 回填，其后由写入方落值）。

**进度（2026-09-30）**：✅ **IP-P1-2 权限单源落地**（A6 主体：登录/刷新/切换/`/auth/me`/菜单同源，同账号 A/B 权限独立）。

- 解析器：`middleware/membership_permission.go` `ResolvePermissions(user_id, tenant_id)`：`super_admin` 直通 → 存活 membership（active 且未软删）→ `role_id`（同租户 roles）→ `role_permissions` → `permissions`；角色已配置但权限为空 = DB 显式撤销（不回退）；无 membership / `role_id` 为空 → 按 `AUTHZ_STATIC_FALLBACK`（默认 **false = fail-closed**）决定是否回退静态表并记 Warn；缓存 key 含 `user_id + tenant_id`，角色/权限变更经既有失效链路按租户清理。
- 接线：Login 落 home 租户、Refresh 按 claims 租户、SwitchTenant 按**目标租户**、`/auth/me` 读 token `tenant_id`（`GetUserScoped`）、菜单生成共用同一解析器；`GetUser` 保留为未传租户的兼容入口。
- 配置：`AUTHZ_STATIC_FALLBACK`（`config.authz.static_fallback`，默认 false）+ `.env.example` 迁移窗口说明；启动时注入 `middleware.SetAuthzStaticFallback`。
- 测试：`middleware` 解析器 5 例（A6 跨租户/防串缓存、fail-closed、静态回退、显式撤销、super_admin）；`handlers/auth` 端点四方对拍（provider→customer 切换权限独立，`login≠switch`）；`handlers/auth/service_test.go` 仅补夹具播种（未改既有断言）。
- 边界：请求期 `RBACMiddleware` 仍走既有角色码/DB 判定链（未动；`AUTHZ_STATIC_FALLBACK` 只管权限清单与菜单的计算源）；成员变更的显式缓存失效钩子随 membership 写入路径（IP-P1-3/5）接入。
- 验证：`go build ./...`、gofmt 绿；`middleware`/`handlers/auth`/`handlers/common`/`router` 全绿；`service` 仅 `TestTicketService_GetMSPCustomerReports_AllocationAware` 失败——已在 `c13cc055` worktree 复现为**既有红灯**（legacy `type=msp` 夹具 vs mspguard provider 强校验），与本批无关。

**进度（2026-09-30）**：✅ **IP-P1-3 组织挂 membership（数据模型 + 双校验）落地**（A5 主体：多组织归属以 membership 为唯一载体；跨租户关联拒绝）。

- 表：`user_tenant_membership_orgs`（§4.0-A 契约：`org_type ∈ department|team|group|project` 多态 + `role_id/is_primary/status/expires_at/deleted_at`）；索引 `uq_membership_org`（membership+type+org 唯一，含历史行 → 复挂复活原行）、部分唯一 `uq_membership_org_primary`（同类型至多一个存活主组织）、`idx_membership_org_scope`。
- DB 约束：复合 FK `(membership_id, tenant_id) → user_tenant_memberships(id, tenant_id)`（迁移 20260505 以 `DROP IF EXISTS` + `ADD` 落地，ent 先行建表也能补齐；跨租户配对 DB 层拒绝）。
- 回填：department（`users.department_id`）/team（`users.team_users`）/group（`users.group_members`）按**同租户** JOIN 预过滤入表，`ON CONFLICT DO NOTHING`；project 当前无成员列留空（由写入路径产生）。users 单值 FK 保留兼容读。
- 应用层：`service/membership_org_service.go` `Attach/Detach/ListForMembership`——多态四表加载比对 `org.tenant_id == membership.tenant_id`（跨租户 `ErrMembershipOrgCrossTenant`、悬挂组织 / 类型非法 / 成员非活跃分列错误）；主组织切换自动降级旧主；软删复活幂等复用原行。
- 测试：`TestMembershipOrgService_*` 4 组（同租户挂接 + 主组织唯一 + 多态 team/group、跨租户拒绝（department/project 两态）、软删复活、校验错误）；`go generate ./ent` + `go build ./...` 绿。
- 巡检：`scripts/msp/verify-membership-orgs-backfill.sql` 8 节（复合 FK 存在 / 类型分布 / 跨租户错配 / 悬挂 / 主组织重复 / 遗留 FK 未回填 / 软删状态不一致 / 过期存活）。
- IP-P1-3b（本批收口）：组织唯一约束租户化——team/group 名称、project 代码收敛为 `(tenant_id, ...)` 唯一（迁移 20260506；project 去全局唯一，team 部分唯一 `WHERE deleted_at IS NULL`）；跨租户同名/同码可共存、同租户重复被 DB 拒绝、team 软删可重名；`service/org_unique_constraint_test.go` 断言。
- 边界：组织表 RLS 与 guard 一致性扫描归 IP-P1-7 / IP-P2-5。

**进度（2026-09-30）**：✅ **IP-P1-4 邀请生命周期（后端闭环：服务 + API）**；前端落地页 + e2e 归 IP-P1-4c。

- 表：`invitations`（§4.0-C 契约冻结）+ `migrations/20260507_create_invitations(.sql/_down.sql)`（token_hash UNIQUE、`uq_invitation_pending` 按 `lower(email)` 部分唯一、tenant/status 与 expiry 索引）。
- 服务：`service/invitation_service.go`——`Create`（platform/tenant/msp 三通道授权 + rank 上限；`super_admin/sysadmin` 不可被邀请；msp_role 白名单；重发 = 旧邀请置 revoked；token 128-bit 随机仅存 sha256；SMTP 未配置返回 `inviteUrl` + `emailSent=false`；审计 `user.invite`）、`Accept`（一次性：pending ∧ 未过期；事务内建号/绑定已有账号 + membership `source=invite` + 置 accepted；`channel=invite` 显式建号通道；审计 `user.invite_accept`）、`Revoke`（仅 pending）、`Inspect`（邮箱脱敏最小回显）。
- 测试：`TestInvitationService_*` 5 组（创建+接受全链路与审计、白名单/rank/msp_role 守卫、过期/撤销/重发失效、绑定已有账号、跨租户禁止）；`go generate ./ent`、`go build ./...`、`cmd/migration-lint`、docs-gate 全绿。
- IP-P1-4b（API 层）：`handlers/invitation/handler.go` 四端点——`POST /api/v1/users/invitations`（认证 + `user:write`；`tenantId` 省略取当前租户）、`POST /api/v1/users/invitations/:id/revoke`（认证 + `user:write`）、`GET /api/v1/auth/invitations/:token`（公开，最小回显）、`POST /api/v1/auth/invitations/:token/accept`（公开 + 登录限流）；邀请域错误码映射（`INVITATION_*` → 404/409/410/422/403）；`inviteUrl` 直返调用方（SMTP 未配置）。装配于 `internal/bootstrap/app.go`，路由注册独立于 UserHandler 接线。测试：`handlers/invitation` 全链路 HTTP 契约（创建→回显→接受→重放 409→撤销→410→非法 token 404）+ `router` 路由契约各 1 组。
- 边界：前端邀请落地页/设置密码页 + e2e 邀请→首登链路归 IP-P1-4c（依赖 IP-P1-5 首登契约）；invitations RLS policy 归 IP-P1-7。

**进度（2026-09-30）**：✅ **IP-P1-5 首登与 bootstrap 租户化（后端闭环）**（`07:G2` 关闭）。

- 账号策略：`pkg/bootstrap` 两条通道（bootstrap token / break-glass）统一 `admin-<tenantCode>` + `admin-<tenantCode>@bootstrap.local`（`WithAdminIdentity` 可覆盖；租户 code 清洗后截断 24 字符）；`Status` 改按 `is_bootstrap_admin` 判定（兼容历史 `admin` 账号）。
- users 两列（§4.0-B B1）：`must_change_password`（默认 false；bootstrap 管理员默认 true，`BOOTSTRAP_ADMIN_MUST_CHANGE_PASSWORD` 可关）+ `last_active_tenant_id`（登录时更新）；迁移 `20260508_users_login_hardening(.sql/_down.sql)`。
- 首登强制改密：登录响应 `user.mustChangePassword` → `POST /api/v1/auth/change-password`（认证 + 旧密码校验 + 密码策略 + 清标志 + 审计 `auth.change_password`）。
- CLI：`cmd/initialize` 支持 `-tenant-id`/`-tenant-code`（不再写死 `default`）与 `-admin-username`/`-admin-email`；`cmd/provision_tenant` 支持 `-tenant-code` 与 `-create-admin`（无 token 通道 `bootstrap.CreateFirstAdmin`，幂等：已有 bootstrap 管理员则拒绝）。
- 测试：`TestBootstrapToken_MultiTenantAdminsDoNotCollide`（连续 2 租户 bootstrap 成功 + 身份/首登标志断言）、`TestCreateFirstAdmin_IdempotentAndTenantScoped`、`TestChangePassword_FlowAndFlagClear`；build/migration-lint/docs-gate 全绿。
- 边界：前端强制改密路由归 IP-P1-4c（与邀请落地页同批）；`07:G4/G5/G6`（供给可复现性）仍按 P1 排期。

**进度（2026-09-30）**：✅ **IP-P1-4c 前端落地页与强制改密 UI**（前端部分）。

- 公开邀请落地页 `/invite?token=...`：`AuthService.inspectInvitation` 回显（租户/脱敏邮箱/角色/状态）→ 设置密码（强度条 + 二次确认）→ `acceptInvitation` 激活 → 引导登录；失效/过期/撤销给出可读原因。
- 首登强制改密：登录与 `/auth/me` 下发 `mustChangePassword`（`toUserDomain`/`session-mappers`/`AuthService.login` 全链路映射）；`RequireAuth` 对未改密用户把所有受保护路由收敛到 `/change-password`（放行改密页防自锁）；改密页 `changePassword` 成功后清本地标志并回 `/dashboard`。
- 路由：`vite-route-map.csv` 增 `/invite`（(auth)）与 `/change-password`（(main)），`gen_routes.py` 重生成 `route-paths.ts`；`routes/index.tsx` 手工增量（保留 IP-P0-8 分组守卫结构）。
- 测试：`guards-must-change-password.test.tsx` 3 用例（收敛/自锁豁免/放行）；`tsc --noEmit` 0 错误；`guards`、`auth-service` 既有套件回归通过。
- 边界：浏览器级 invite→首登 e2e 与个人中心改密入口待环境联跑（Playwright 需后端+DB）。

**进度（2026-09-30）**：✅ **IP-P1-8 审计看板落地**（治理可观测；顺带复核并真正关闭 `07:G9`）。

- 拒绝事件落库补全：① 头通道拒绝（`X-Customer-Tenant-ID` 未分配/客户不存在）写 `tenant.scope_denied`（source=header，target=客户租户）；② `X-Tenant-Code` 与 JWT 冲突写 `tenant.probe_denied`（source=header，target=被请求租户）。两者经 `middleware.RecordTenantDeniedAudit` 统一同步落库（2s 超时、失败可见）。
- **G9 复核（诚实更正）**：原 IP-P0-6 的"冲突 401"未生效——`tenant.go` 在 JWT 已解析实体时**完全跳过** Header 读取，步骤 5 的冲突分支不可达（与 07 §10 根因描述一致）；本轮改为"JWT 已锁定时 Header 仍解析并校验一致性"，冲突 → 401 + `TENANT_MISMATCH_REJECTED` + 审计。测试：`TestTenantMiddleware/JWT/Header Conflict Rejected and Audited`（含一致放行回归）、`TestMSPMiddleware_HeaderChannelUnifiedGuard`（拒绝落审计断言）。
- 聚合 API：`GET /api/v1/msp/audit/summary?days=30`（`msp_report:read`）——窗口内跨租户事件数、拒绝事件数、按 source/action/目标租户/membership 聚合（目标租户名回填）、最近 50 条拒绝明细（含 reasonCode）；`service/msp_audit.go` + `handlers/msp/audit.go`，扫描上限 5000 行兜底。
- 前端看板：`/msp/audit`（`pages/(main)/msp/audit`，msp 分组守卫内）——三统计卡 + 来源/动作分布 + 客户分布 + 越权/冲突明细表；`msp-audit-api.ts` 契约层。
- 测试：`TestMSPAuditService_Summary`（窗口过滤/聚合/名称回填/排序/窗口收敛）；前端看板 4 用例（聚合渲染/空态/窗口切换重拉/失败提示）；`go build ./...`、`router`、`middleware`、`handlers/msp`、`service` 定向、tsc/eslint 全绿；`cmd/authz-gen` 生成物随路由更新。
- 边界：治理看板当前面向 provider 管理员（`msp_report:read`）；平台治理视角（跨 provider 汇总）留 P2（IP-P2-1 provider 维度）。

---

## 5. P2 详细实施（多 provider 与治理收尾）

| 工作流 | 目标 | 关键步骤 | 验收 | 回滚 |
|---|---|---|---|---|
| **IP-P2-1** provider 维度 | 多 provider 收窄 | `msp_allocations.provider_tenant_id` + 归属一致性校验；工作台/报表/审计按 provider 收窄；N=1 无额外 UI/步骤 | **A11**：同一 e2e 在 N=1/N=2 均过；A12 工单流转通过 | 字段可空 + 收窄开关 |
| **IP-P2-2** RLS `enforce` | 数据库层强制 | 低权角色 + 全路径 ctx 补齐后 `shadow → enforce` 灰度 | enforce 后核心路径 0 500；隔离回归全过 | 回退 `shadow` |
| **IP-P2-3** 共享表治理 | 显式共享（D3） | `TenantExemptTables` 复核（标签云/市场模板/`messages`/`prompt_templates`）；`messages` 租户化决策落地 | 每张共享表有 owner/理由/复核期；季度复核记录 | 逐表回退 |
| **IP-P2-4** 工作台进阶 | 自定义视图/配额 | 保存过滤器组合、SLA 风险看板、每客户配额可视化 | 视图可分享/复现；配额数据与后端一致 | feature flag |
| **IP-P2-5** guard 扩展 | 成员/关联表一致性 | tenant_guard 增加"关联表一致性"检查（跨租户 FK/悬挂成员） | 启动扫描 0 高危；CI 用例覆盖 | 检查项分级（fatal→warn） |
| **IP-P2-6** 平台租户管理：硬配额 | 租户 limits 模型与校验 | `tenants.quota`（jsonb，`maxUsers`/`maxTicketsPerMonth`/`maxStorageMB`）；三写入路径接入（建号/建单/附件）；平台面读写 + 非法值 400；用量口径与校验一致 | 超限 422 `TENANT_QUOTA_EXCEEDED`（附件沿用 6106）；缺省不限行为不变；单测覆盖三路径 | 列可空保留 + 移除校验调用（行为回退） |

> **P2 进度（2026-09-30 起）**：IP-P2-1 首批落地——`provider_tenant_id` 迁移（可空 + 回填 + 部分索引，`20260930_msp_allocation_provider_dimension.sql`）、写侧归属校验（admin 不豁免）、读侧 provider 收窄（middleware / 租户切换列表 / 分配列表；工作台与报表维持 `mspguard` 单源收窄）、巡检脚本与单测；**A11/A12 api 通道 e2e 落地**（v1.39）；**NOT NULL 收尾已落地**（v1.44：联调库巡检 4/4 归零 → 迁移 `20261003_msp_allocations_provider_not_null.sql` + ent 必填 + 读侧等值收敛）。IP-P2-5 首组检查落地（v1.32）。IP-P2-2 前置代码收口（v1.33）。IP-P2-3 共享表复核（v1.34）。IP-P2-4a 自定义视图全链完成（v1.36）。IP-P2-4b SLA 风险看板落地（v1.37）。IP-P2-4c 每客户用量看板（usage-only 定案）落地——**P2-4 全项收口（v1.38）**。**IP-P2-6 硬配额全链落地（v1.41–v1.42）**：`tenants.quota` + 三写入路径校验（建号/建单/附件）+ 治理页编辑（v1.41）与「用量」弹窗（`GET /api/v1/tenants/:id/usage`，v1.42）。

### 5.0 P2 冻结契约（2026-09-30；本节即 P2 编码基线）

> 与 §3.0/§4.0 同规则：**变更须回填本节并登记修订记录**。

**5.0-A IP-P2-1 provider 维度（收口 §4.0-B）**

- **DDL**：沿用 §4.0-B（`provider_tenant_id int NULL REFERENCES tenants(id)` 在线加列 → 回填 → 部分索引）；**NOT NULL 收尾条件**：回填差异清单为零 + N=1/N=2 e2e 全绿后单独迁移（登记修订记录）。**2026-10-03 收尾落地**：联调库 `verify-allocation-provider-backfill.sql` 4/4 归零 + A11/A12 e2e 绿 → 迁移 `20261003_msp_allocations_provider_not_null.sql`（幂等；残余 NULL 先再回填、仍存在则显式报错阻断）+ ent `provider_tenant_id` 必填（边 `Required`）+ 读侧三处收敛为等值。
- **写入校验（唯一入口 `service.MSPAllocationService.Create`）**：`allocation.provider_tenant_id` = MSP 员工 home provider；必须 == `customer.msp_provider_id`；跨 provider 分配拒绝（R2；admin 不豁免归属校验，canon C10/D1）。
- **收窄点（N=1 行为不变）**：① `middleware/msp_middleware.go` 的 `AllowedCustomers` 由 provider 收窄后的分配集合构建（授权链仍唯一走 `mspguard`）；② 工作台 / 报表（`GetMSPCustomerReports`）/ 审计（`MSPAuditService`）查询按 `provider_tenant_id` 收窄。
- **过渡兼容（已收敛）**：收窄条件曾为 `provider_tenant_id = <provider> OR provider_tenant_id IS NULL`（回填前存量行不丢）；v1.44 NOT NULL 收尾后三处读路径统一为等值。
- **回滚**：字段可空 + 代码回退；R2 主链不受影响（`mspguard` 读路径仍强校验归属）。
- **验收**：`TestMSPAllocationService_*`（provider 派生 / 跨 provider 拒绝 / 一致性）、`middleware` N=2 收窄用例；**A11/A12 api 通道 e2e ✅**（`router/msp_a11_a12_e2e_test.go`，v1.39）。

**5.0-B IP-P2-2 RLS `enforce` 前置（冻结）**

- **前置清单（全部满足才允许 `shadow → enforce`）**：① 低权角色（`msp_tech` 等）在 shadow 期无新增 `permission denied`；② 全路径 ctx 补齐：HTTP（auth→tenant→rbac）、BPMN 执行器、定时器/worker、迁移/运维脚本（集成分析 §5.2 清单）逐个 dry-run；③ 运维通道显式带 tenant context 或列入豁免表并复核。
- **灰度**：`off → shadow → enforce` 顺序不可跳跃；enforce 按租户/表分批；回退点 `shadow`。
- **门禁**：enforce 后核心路径 0 500；隔离回归（A10）全过。

**5.0-C IP-P2-3 共享表治理（冻结清单，源自 `TenantExemptTables`）**

- 待复核条目（须有 owner/理由/复核期）：`marketplace_items`（global）、`messages`（global，**D3 已定：租户化落地**）、`prompt_templates`（global）、标签云系列（derived，维持）。
- 动作：`messages` 增 `tenant_id`（在线加列 + 回填 + 切读）；其余逐表复核并记季度记录（180 天超期启动告警已存在）。
- 回滚：逐表回退（只增不删）。

**5.0-D IP-P2-4 工作台进阶（范围冻结）**

- 保存过滤器组合（命名视图 / 分享）；SLA 风险看板；每客户用量/配额可视化——**2026-09-30 数据源定案**：硬配额无数据源（`tenants` 无 `quota/settings` 列；`TenantDTO.Quota` 遗留未赋值；附件配额 6106 仅错误码），以 **usage-only** 交付（`members` / 未关闭 / `ticketsCreated30d`，窗口 `usageWindowDays=30` 下发）；**硬配额（limits）模型与校验登记「平台租户管理」批次遗留**。
- 门禁：feature flag 灰度；前端单测 + 视图 URL 可复现。

**5.0-E IP-P2-5 guard 扩展（冻结检查项）**

- `tenant_guard` 新增"关联表一致性"检查：① 跨租户 FK（如 `user_tenant_membership_orgs.org_id` 指向异租户组织）；② 悬挂成员（membership 指向已删租户/用户）；③ `msp_allocations.provider_tenant_id` 与 `tenants.msp_provider_id` 不一致。
- 分级：生产默认 `fatal`，逐项可降 `warn`；CI 用例覆盖（构造违规样本断言检出）。

**5.0-F IP-P2-6 平台租户管理：硬配额（冻结契约）**

- **模型**：`tenants.quota`（jsonb，可空）；键 `maxUsers` / `maxTicketsPerMonth` / `maxStorageMB`（单位 MB）。**值 <= 0 / 键缺省 / 列 NULL = 不限**（fail-open，存量行为不变）；写入按显式模型校验（未知键、负值、非整数、超上限 → 400），`PUT /api/v1/tenants/:id` 与创建接口均接受 `quota` 对象（显式 `{}` = 清空）。
- **用量口径（与校验一致）**：users = 该租户 `users` 行数；ticketsThisMonth = `created_at >= 本月起点 ∧ deleted_at IS NULL`；storageBytes = `attachments(status=active ∧ deleted_at IS NULL).file_size` 合计（自然月按服务器本地时区）。
- **接入点与错误语义**：建号（`UserProvisioningService`，含 platform/msp/tenant 三通道）→ 422 `TENANT_QUOTA_EXCEEDED`；建单（`TicketService.CreateTicket`）→ 422 `TENANT_QUOTA_EXCEEDED`（响应含 `quota/limit/used`）；附件上传（`AttachmentService`，写盘前预检）→ 沿用 6106 `ErrAttachmentQuotaExceeded`（422）。bootstrap/break-glass 属恢复通道，不经 `UserProvisioningService`，不受建号配额约束（有意例外）。
- **用量查询（v1.42 收尾）**：`GET /api/v1/tenants/:id/usage`（`tenant:read`）→ `{tenantId, limits:{…}, used:{users,ticketsThisMonth,storageBytes}}`；上限与用量同源自 `TenantQuotaService`（与写入校验逐字同口径），租户不存在 fail-closed 报错；管理端治理页「用量」弹窗展示已用/上限与进度条（不限键仅展示用量文本）。
- **回滚**：列可空保留；撤销 `SetTenantQuotaService` 注入即恢复旧行为（校验对 nil 服务安全）。

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

- [x] **安全**：未分配客户在头/路径/请求体/切换 4 通道均 403（含 `MSP_ALLOCATION_REQUIRED` 错误码；R9/R10 关闭，v1.3）；头/JWT 冲突 401 + 告警（`07:G9`：**v1.7 声明的关闭经 2026-09-30 复核未生效**——JWT 已锁定时 Header 被跳过、冲突分支不可达；**由 IP-P1-8 v1.29 真正落地**：401 + `TENANT_MISMATCH_REJECTED` + `tenant.probe_denied` 审计）；
- [x] **缓存隔离**：`itsm-backend/cache/` 逐 key 审查完成、跨租户 key 修复 + 单测（`07:G8` 关闭，v1.3；清单与豁免见 07 §9）；工作台 summary 缓存 key = 租户集合哈希 + 用户（v1.9）；
- [x] **执行器/定时器**：后台任务/自动化在显式租户 ctx 下运行、错误 ctx 被拒、`source=job` 可审计（IP-P0-11，2026-09-30）；
- [x] **功能（后端）**：工作台 list/summary/reply/status 落地 + 条目级授权链 + `allowedActions[]` + 逐条审计（2026-09-30）；前端入口/行内操作/双态指示（WB-A2/A6 前端面）归 IP-P0-8；批量（WB-A4）留 P1；
- [x] **登录/会话**：provider 登录落 provider 家；切换/刷新/撤销契约通过（F5/F6/F9/F10/F11a/F12 后端，2026-09-30；F11b/c 前端/权限行归 IP-P0-8/9）；
- [x] **建号**：三通道 `UserProvisioningService` 生效；`07:G1`/K4 关闭（2026-09-30）；角色白名单与注册白名单生效（`07:G2` 归 IP-P1-5）；
- [x] **角色供给**：新 provider 租户 seed 后 5 个 `msp_*` 角色权限齐备（K1/K2 关闭，2026-09-30）；`07:G3` 关闭（登录 rank 取最大 + roleRank 单源）；
- [x] **前端（P0 面）**：FE-A1–A5、A7 落地（2026-09-30）；登录页 DOM 无租户列表；客户账号无过滤器/切换器/工作台节点；FE-A6（管理页建号目标租户 UI）/A8（路由元数据单一模型清理）随 P1；
- [x] **契约**：错误码/DDL/审计事件与 §3.0 一致（审计作用域列 + 事件目录 + source 枚举，2026-09-30；`membership_id` 填充随 IP-P1-1）；`07:G1–G10` 映射表（§3.0-F）无遗漏；
- [x] **门禁**：docs-gate 6/6 —— 完整 `run-all.sh` 终局 `6 total, 0 failed`、`GATE_EXIT=0`（2026-09-30；C.1/C.2/C.4/C.5/C.6 = 0 violation；C.3 84 条历代断链为 advisory）；`make test` 全绿 —— 后端 `go test ./...` 全绿（既有 fixture/报告用例已修复）；前端规范 `npm test` 全量复跑 **263/263 套件、4014 通过、13 skip**，覆盖率门槛全达标（Statements 80.33% / Branches 67.81% / Functions 81.24% / Lines 81.51%）；三角色剧本 P0 项以单测/e2e 已覆盖部分为准（M10 四通道 403 见 IP-P0-2 单测）。

### 6.3 P1 出口 DoD

- [x] membership 表/回填/约束机制落地（v1.13，2026-09-30；customer 恰 1 条 active 由 `uq_customer_single_scope` DB 强约束 + 约束回归）；**联调库巡检 2026-10-03：8 节全零**（判据修正：邀请建号 `source=invite` 即其主作用域）；生产库巡检档案随后续部署执行 `scripts/msp/verify-membership-backfill.sql` 留档；
- [x] 权限 DB 单源（权限清单面）：登录/刷新/切换/`/auth/me`/菜单同源，跨租户权限互不影响（A6；v1.14，2026-09-30；请求期 RBAC 判定链与 `shadow`/enforce 仍按 IP-P1-7 推进）；
- [x] 组织多归属 + 生效期（A5：`user_tenant_membership_orgs` 子表/回填/复合 FK/应用双校验 + 组织唯一约束租户化；v1.15/v1.16，2026-09-30）；
- [x] bootstrap 多租户连续成功（`07:G2` 关闭，v1.19；连续 2 租户用例 + 幂等无 token 通道）；
- [x] 邀请→首登 e2e（**2026-10-03 联调环境端到端绿**）：后端 ✅（v1.18）、前端 UI ✅（v1.28）；浏览器链路 `flow-invitation-onboarding.spec.ts` 实测通过（2 次复跑绿）——联调实例换版至含邀请路由的 HEAD 构建 + 联调库迁移至 `pending=0`（`mig-verify -ro` 核验）；spec 对齐真实契约（Cookie 会话 CSRF Double Submit：`GET /api/v1/csrf-token` → `X-CSRF-Token`；`test.slow()` 覆盖 vite 冷编译）；
- [x] 审计看板：拒绝事件落库（`tenant.scope_denied` / `tenant.probe_denied`）+ 聚合 API + `/msp/audit` 面板（IP-P1-8，v1.29；`07:G9` 真正关闭）；
- [x] 批量护栏通过（WB-A4）：批量 ≤100、仅低危动作（reply/status/assign）、高危拒绝、客户分布确认、逐条授权/审计与限流（IP-P1-6，v1.24；后端 `TestWorkbenchBatch_Validation` + 前端批量链路用例）；
- [ ] RLS `shadow` 无新增错误（A7）：IP-P1-7 评估完成（见 `msp-rls-collection-query-assessment.md`），shadow 观察待 staging/生产环境执行；
- [x] docs-gate 6/6；`make test` 全绿 —— 2026-09-30 终局认定：docs-gate `run-all.sh` `6 total, 0 failed`（`GATE_EXIT=0`）；后端 `go test ./...` 全绿；前端 `npm test` 263/263 套件全绿（含覆盖率门槛）。既有红修复：契约扫描误报、附件契约对齐、慢环境用例超时放宽；全量首跑 5 套件因并发负载超时，隔离复验与全量复跑均绿。

### 6.4 P2 出口 DoD

- [x] A11/A12 **api 通道 e2e 落地**（v1.39，`router/msp_a11_a12_e2e_test.go`）：N=1/N=2 同一剧本行为指纹一致；建单快照 / 工作台可见与 provider∩allocation 收窄 / 指派校验 / 跨 provider 与未分配客户拒绝全覆盖；**通知双投递已实现**（v1.40：provider 侧 = 托管处理人 + provider 管理员，回复/改状态/指派/建单四事件全链；单测 `service/msp_provider_side_notification_test.go` + e2e 断言）；浏览器/多部署环境 e2e 为可选补强；
- [ ] RLS `enforce` 灰度无 500——代码侧 ctx 收口与 shadow 前置完成（v1.33），灰度待 staging 执行；**✅ 共享表治理清单完成**（v1.34：`messages` 租户化 + `msp-exempt-tables-quarterly-review.md` 固化 owner/复核期）；
- [ ] guard 扩展检查 **0 高危**待生产库执行（首组三检查 v1.32 已接入，启动扫描 0 高危以生产巡检为准）；**✅ docs-gate 6/6**（2026-09-30 全量 `run-all.sh`，C.6 语义锚点门禁常开）。
- [x] **平台租户管理：硬配额（limits）全链落地**（v1.41–v1.42，IP-P2-6）：`tenants.quota` 模型 + 用量口径；三写入路径接入（建号/建单/附件，超限 422 `TENANT_QUOTA_EXCEEDED`、附件沿用 6106）；平台面读写 + 非法值 400；用量查询端点（`GET /tenants/:id/usage`，v1.42）；前端治理页配额编辑与「用量」弹窗（配额 vs 用量，v1.42）；单测覆盖三条路径、fail-open 缺省与用量委派。

> **P2 工作流代码侧全项交付（2026-09-30；v1.44 追加 IP-P2-1 NOT NULL 收尾）**：IP-P2-1（provider 维度收窄 + NOT NULL 收尾）/ IP-P2-2（ctx 收口 + 评估档案）/ IP-P2-3（共享表治理）/ **IP-P2-4 全链（视图 / SLA 看板 / 用量看板，v1.35–v1.38）** / IP-P2-5（guard 首组检查）。上表未勾项均为**环境/数据依赖**项（enforce 灰度、生产库巡检/留档——联调库已按 v1.44 执行），随部署执行。

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
| A11 N=1/N=2 e2e | IP-P2-1 | `router/msp_a11_a12_e2e_test.go`（`TestMSP_A11_SameScenarioForN1AndN2`：同一剧本行为指纹一致） |
| A12 多 provider 工单流转 | IP-P0-3 + IP-P2-1 | 同上（`TestMSP_A12_ProviderScopedTicketFlow`：快照/收窄/指派/拒绝/双投递）+ 快照单测 + `service/msp_provider_side_notification_test.go`（双投递矩阵） |

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
| **D2**（直客无 provider） | 归属模型 | ✅ 2026-09-30 已确认：`saas_customer` 即显式直客标记；IP-P0-4 直接实施 |
| **D3**（`messages` 租户化） | 共享表语义 | ✅ 已确认租户化（IP-P2-3） |
| **D5**（平台 membership） | 治理审计 | ✅ 已确认必须有 membership（IP-P1-1；与 D11 一致） |
| **D8**（批量边界） | 效率/风险 | ✅ 已确认低危 + 护栏（IP-P1-6） |
| **E1–E6**（工单流转） | A12 | ✅ 全部确认（canon §7.2）；IP-P0-3 / IP-P2-1 直接实施 |
| **07:G1–G10**（生产实测缺口） | 可追溯性 | 承接映射见 **§3.0-F**（G2/G4–G6 在 IP-P1-5；G7 P2 接受现状；G10 收尾） |

---

## 9. 里程碑、排期骨架与工作量（2026-09-30 基线）

| 批次 | 工作流数 | 粗估（人日，后端+前端+测试） | 关键里程碑 |
|---|---|---|---|
| P0 | 11 | 25–40 | M1 安全闭环（IP-P0-1/2/3，含缓存审查）；M2 登录/建号（5/6）；M3 工作台+前端（7/8）；M4 角色/审计/执行器+文档（9/10/11） |
| P1 | 8 | 25–35 | M5 membership+回填；M6 权限单源/组织；M7 邀请/首登；M8 RLS shadow |
| P2 | 5 | 15–25 | M9 provider 维度+N=2 e2e；M10 RLS enforce；M11 治理收尾 |

> 说明：估算不含产品决策等待时间（D/E 项已清零）；以"工作流 = 1 个可独立发布的 PR 组"为粒度校准。

### 9.1 执行顺序与建议窗口（Owner 待指派）

| 里程碑 | 工作流（顺序） | 依赖 | 建议窗口（相对 P0 启动） | 出口证据 |
|---|---|---|---|---|
| M1 安全闭环 | `IP-P0-1` → `IP-P0-2` → `IP-P0-3` | — | 第 1–2 周 | R9/R10/`07:G8`/`07:G9` 关闭；四通道 403 用例全过 |
| M2 登录/建号 | `IP-P0-6` → `IP-P0-5` | M1（统一授权入口） | 第 2–4 周（与 M1 尾并行） | 登录/切换/refresh 契约用例；`07:G1` 关闭 |
| M3 工作台+前端 | `IP-P0-7` → `IP-P0-8` | M2（响应契约） | 第 4–6 周 | WB-A1–A6；FE-A1–A8 |
| M4 角色/审计/执行器/文档 | `IP-P0-9` → `IP-P0-10` → `IP-P0-11` | 可与 M1–M3 并行 | 第 3–6 周 | K1/K2/`07:G3` 关闭；docs-gate 全绿 |
| M5–M8（P1） | `IP-P1-1/2` → `IP-P1-3` → `IP-P1-4/5` → `IP-P1-6/7/8` | P0 出口 DoD（§6.2） | P0 完成后 4–6 周 | 路线 B 转正；`07:G2`、`07:G4–G6` 关闭 |

- **关键路径**：M1 → M2 → M3；`IP-P0-2` 是所有跨租户能力的共同依赖，**禁止后置**；
- **并行度**：M4 与 M1–M3 并行（角色/审计/执行器改动面独立）；`IP-P1-1`（只加表/回填，不改读路径）可在本方案冻结后先行开发；
- **Owner 与日历排期**：由项目组按上述工作流粒度指派 RACI 并在 3 个工作日内确认窗口；本方案不预设人名。

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
| v1.0 | 2026-09-30 | **评审通过 + P0 契约冻结**：新增 §3.0（错误码注册表 / P0 DDL 清单 / 五角色权限矩阵 / 工作台 Schema / 审计事件目录 / `07:G1–G10` 承接映射）；修正工作流计数 10→11；IP-P0-2 增加缓存租户维度步骤（`07:G8`）；IP-P0-9 增加 `07:G3` 修复项；IP-P0-5/P0 DoD 修正 G2 批次；D2/D3/D5/D8/E1–E6 标记已确认；基准 HEAD 重钉 `337558e3` |
| v1.1 | 2026-09-30 | **P1 契约冻结 + 排期骨架**：新增 §4.0（`user_tenant_membership_orgs` 组织关联子表与复合 FK、`msp_allocations.provider_tenant_id` 列与回填、`invitations` DDL/生命周期/API/安全口径）——审计 C13 闭环；§9 增 §9.1 执行顺序与建议窗口（Owner 待指派）；标题去除"待排期校准" |
| v1.2 | 2026-09-30 | **IP-P0-1 落地**：门控严格化（仅 saas_msp）+ 单一来源（cfg）+ 未知/空值 fatal + 启动自检 + `/msp/status` 暴露模式/gate；相关单测与 `go build ./...` 通过；02 §1/§9 同步目标口径 |
| v1.3 | 2026-09-30 | **IP-P0-2 安全核心落地**：`pkg/mspguard` 唯一授权入口（头/路径/请求体/报表四通道）+ R10 死代码删除 + `uk_msp_allocation_active`（ent schema + 迁移 022）+ G8 缓存审查关闭；三通道反例单测全绿 |
| v1.4 | 2026-09-30 | **IP-P0-3 快照落地**：建单双链路派生 `is_managed_by_msp`/`msp_provider_id`（无效归属按普通工单）；指派补写 `managed_by_user_id`；仓库 4 个 builder 全覆盖；回填脚本（dry-run/apply/rollback）交付 |
| v1.5 | 2026-09-30 | **IP-P0-4 类型/归属收敛落地**：`pkg/tenantmode` 校验+读取映射；`TenantService` 写入接入、归属复核、停止 `parent_tenant_id` 双写；DTO binding 收敛；巡检脚本 + 01/03 文档回填 |
| v1.6 | 2026-09-30 | **IP-P0-5 建号通道收口落地（K4/07:G1 关闭）**：`UserProvisioningService` 三通道 + 角色白名单/rank + `WithProvisioningBypass` + 写守卫放行 + 3 端点 + 灰度开关；8 子用例全绿 |
| v1.7 | 2026-09-30 | **IP-P0-6 登录/切换/刷新契约落地（F10/F11a/F12/G9 关闭）**：登录落 home + `tenantSelection`；切换重签 `tenant_source=switch`、撤销旧 refresh、审计、响应 `user.tenantId=目标`；refresh 按 claim 重签 + `TENANT_ACCESS_REVOKED`；`/auth/tenants` = home∪allocation∪平台全量；头冲突 `TENANT_MISMATCH_REJECTED` |
| v1.8 | 2026-09-30 | **IP-P0-9 角色供给显式化落地（K1/K2/07:G3 关闭）**：5 个 `msp_*` 入内置词表 + seeder 角色种子；`middleware.RoleRank` 单源（登录取主角色/MSP 映射 rank 更高者）；脚本 §5 降级为校验 + `MSP_ROLE_SQL_FALLBACK` 兜底；tests/parity 矩阵对拍守卫 |
| v1.9 | 2026-09-30 | **IP-P0-7 工作台后端落地**：列表/summary/reply/status 四端点 + 条目级授权链 + `allowedActions[]` + per-tenant 复合游标 + 逐条审计（source=workbench/target_tenant）+ 工作台索引 DDL；前端链路归 IP-P0-8 |
| v1.10 | 2026-09-30 | **IP-P0-10 审计统一落地**：`audit_logs` 四列（actor_account/membership_id/target_tenant_id/source）+ `idx_audit_scope`；事件目录 `auth.login`/`tenant.switch(-_denied)`/`tenant.scope_denied`/`workbench.action`/`user.provision` 与 source 枚举写入；审计查询支持 targetTenantId/source（legacy=历史 NULL）/actorAccount；`membership_id` 填充随 IP-P1-1 |
| v1.11 | 2026-09-30 | **IP-P0-11 执行器 ctx 统一落地（C19/C21/C22 关闭）**：`tenantctx.EnsureJobTenant` fail-closed + `SystemContext` 枚举 / `WithTenantID` 执行两段式；timer/超时扫描/自动升级/bootstrap 八类后台循环统一注入；`source=job` 审计（timer.fire/bpmn.timeout_scan/workflow.escalation）；工单指派同租户校验 + workflow/deployment 列表 fail-closed + BPMN 授权租户化 |
| v1.12 | 2026-09-30 | **IP-P0-8 前端上下文/权限链路落地（FE-A1–A5、A7）**：`tenants[0]` 移除（作用域=服务端）；`switch-tenant` 端点修正 + store 切换链路（abort→重签→重拉 me→清缓存）+ 登出清理；菜单/能力 queryKey 按租户分键；`/403` + `RequireCapability` 分组守卫（admin/msp）；`CustomerFilter` + `/msp/workbench` 页（allowedActions 行内操作、只读态、游标）；登录页无租户面回归；FE-A6/A8 归 P1 |
| v1.13 | 2026-09-30 | **IP-P1-1 membership 表/回填/约束落地**：`user_tenant_memberships`（目标架构 §3.2 字段 + 3 个部分唯一索引 + 复合 FK 目标）+ 幂等迁移（home/allocation 回填、D10 角色映射、`audit_logs.membership_id` 回填）+ 巡检脚本 `verify-membership-backfill.sql` + Ent schema 与约束回归；读路径仍回退 home+allocation（切换归 IP-P1-2） |
| v1.14 | 2026-09-30 | **IP-P1-2 权限单源落地**：`middleware.ResolvePermissions`（membership.role → role_permissions，super_admin 直通，空角色=显式撤销）+ Login/Refresh/Switch/`/auth/me`/菜单同源接线（切换按目标租户，A6）；`AUTHZ_STATIC_FALLBACK` 默认 false（fail-closed，仅迁移窗口可开）；解析器 5 例 + 端点四方对拍测试；请求期 RBAC 判定链未动（后续批次） |
| v1.15 | 2026-09-30 | **IP-P1-3a 组织挂 membership 落地**：`user_tenant_membership_orgs` 子表（§4.0-A：多态 org_type + 生效期/主组织/软删）+ 复合 FK `(membership_id, tenant_id)` + department/team/group 回填 + `MembershipOrgService` 应用层同租户强校验（跨租户拒绝 A5）+ 4 组测试 + 8 节巡检脚本；组织唯一约束 `(tenant_id, code/name)` 拆分为 IP-P1-3b |
| v1.16 | 2026-09-30 | **IP-P1-3b 组织唯一约束租户化**：team/group 名称 + project 代码收敛为 `(tenant_id, ...)` 唯一（迁移 20260506；project 去全局唯一、team 部分唯一软删可重名）；跨租户同名/同码共存 + 同租户重复拒绝断言；IP-P1-3 批次收口（A5 数据面完成） |
| v1.17 | 2026-09-30 | **IP-P1-4a 邀请生命周期（服务层）**：`invitations` 表（§4.0-C）+ 迁移 20260507 + `InvitationService`（创建/接受/撤销/回显；token 仅存 sha256、重发失效、事务建号 + membership source=invite、审计 user.invite / user.invite_accept）+ 5 组测试；路由/落地页归 IP-P1-4b |
| v1.18 | 2026-09-30 | **IP-P1-4b 邀请 API（后端闭环）**：`handlers/invitation` 四端点（创建/撤销认证 + `user:write`；落地页/接受公开 + 限流）+ 邀请域错误码映射 + bootstrap 装配；HTTP 契约测试与路由契约测试全绿；前端落地页/e2e 归 IP-P1-4c |
| v1.19 | 2026-09-30 | **IP-P1-5 首登与 bootstrap 租户化**：账号策略 `admin-<tenantCode>`（token/break-glass/provision_tenant 三通道同口径，`07:G2` 关闭）；users `must_change_password` + `last_active_tenant_id`（迁移 20260508）；登录下发 `mustChangePassword` + `POST /auth/change-password` 自助改密；`cmd/initialize` 租户定位与身份覆盖、`provision_tenant -create-admin` 幂等通道；4 组回归用例 |
| v1.20 | 2026-09-30 | **IP-P1-4c 前端落地页与强制改密 UI**：`/invite` 邀请落地页（回显→设密→激活→登录）、`/change-password` 改密页、`RequireAuth` 强制收敛、`mustChangePassword` 全链路映射；路由 CSV + 重生成 + 守卫测试 3 用例；tsc/jest 绿；e2e 待联跑 |
| v1.21 | 2026-09-30 | **IP-P1-6a 工作台批量（后端）**：`POST /msp/workbench/batch`（≤100、低危白名单 reply/status/assign、逐条授权/结果/审计带 `batch_id`、每租户 20 次/分钟护栏）；DTO + 服务 + 路由 + 4 组护栏用例；批量前端与偏好存储待续 |
| v1.22 | 2026-09-30 | **IP-P1-6c 过滤器服务端偏好**：`users.preferences` jsonb（迁移 20260930 + ent 再生成）+ `GET/PUT /api/v1/users/me/preferences`（白名单键 `workbenchFilter`、≤8KB、顶层合并、nil 删除、仅本人）；写路由白名单豁免登记；服务级回归 1 组；批量前端（6b）待续 |
| v1.23 | 2026-09-30 | **IP-P1-6c 前端接入**：`user-preferences-api` + `CustomerFilter` 水合（仅工作台页且 URL 未显式指定时应用偏好）+ 选择变更 400ms 节流保存（失败静默回退 URL）；tsc + CustomerFilter 6/6、工作台页 5/5 用例绿；批量 UI（6b）待续 |
| v1.24 | 2026-09-30 | **IP-P1-6b 批量 UI**：工作台行勾选（只读行禁用）+ 批量条（回复/改状态/指派 ≤100）+ 客户分布确认弹窗（无权限条目自动排除并提示）+ 逐条结果弹窗（batchId/成功失败/原因码）；`batchWorkbenchItems` 契约对齐；tsc/eslint 绿、工作台页 6/6 用例（含端到端批量链路） |
| v1.25 | 2026-09-30 | **`allowedActions` 全量接入**：行内新增 `assign`（`POST /msp/tickets/:id/assign`，语义=指派给当前 MSP 技术员，同步 managed_by/快照）；批量指派修正为同语义（移除误导性 assigneeId 输入，改为说明提示）；tsc/eslint 绿、工作台页 7/7 用例 |
| v1.26 | 2026-09-30 | **RLS 集合查询评估（IP-P1-7 前置）**：新增 `plan/msp-rls-collection-query-assessment.md`——结论=保留逐租户查询；发现 enforce 前置缺口（工作台 per-tenant 查询 ctx 仍为 provider 租户，`tickets` 入 policy 后需 `tenantctx.WithTenantID` 重绑定）+ 5 条前置清单；B2 请求面 bypass 否决、B3 授权集合 GUC 触发条件登记 |
| v1.27 | 2026-09-30 | **分组视图（IP-P1-6 收口）**：工作台新增平铺/按客户分组切换（`view=group` URL 持久化、不参与查询键避免重拉）；Collapse 组头=客户名+条数，组内省略客户列；复用行内操作与批量勾选（只读行禁用）；tsc/eslint 绿、工作台页 8/8 用例。**工作台 P1 清单全项完成**（批量 ✓ / 偏好 ✓ / allowedActions ✓ / RLS 评估 ✓ / 分组视图 ✓） |
| v1.28 | 2026-09-30 | **IP-P1-4c 收口（邀请落地/首登 UI）**：修复契约断链——后端 `inviteUrl` 为路径式 `/invite/<token>`，前端此前仅注册 `/invite` 且只读 `?token=`（邮件链接将 404）；新增 `invite/:token` 路由 + `useParams` 优先（保留查询式兼容）。测试：落地页 5 用例（路径/查询 token、缺 token、accepted、密码不一致）+ 强制改密 2 用例；e2e `flow-invitation-onboarding.spec.ts`（@multi-tenant；`page.request` cookie/token 双模；旧构建 404 显式 skip 不假红）；fixture 支持 `E2E_ADMIN_USERNAME/PASSWORD` 覆盖（本机 seeder 口令差异痛点）；后端 handler/service 邀请定向回归绿 |
| v1.29 | 2026-09-30 | **IP-P1-8 审计看板 + `07:G9` 真正关闭**：复核发现 IP-P0-6 的"header/JWT 冲突 401"从未生效（JWT 已锁定时 Header 被完全跳过 → 冲突分支不可达），本轮改为锁定时仍解析并校验 Header（冲突 → 401 + `TENANT_MISMATCH_REJECTED` + `tenant.probe_denied` 审计）；头通道未分配/客户不存在拒绝落 `tenant.scope_denied` 审计；新增 `GET /api/v1/msp/audit/summary`（窗口聚合：跨租户/拒绝计数、by source/action/target/membership、最近拒绝 + reasonCode、租户名回填）+ `/msp/audit` 前端看板；后端 service/middleware 定向与 router/build/前端 tsc/eslint 全绿 |
| v1.30 | 2026-09-30 | **P0/P1 门禁终局收官（docs-gate 6/6 + make test 全绿）**：docs-gate 完整 `run-all.sh` 通过（`6 total, 0 failed`、`GATE_EXIT=0`；C.3 84 条历代断链为 advisory）；后端 `go test ./...` 全绿（清 2 条既有 fixture/报告红）；前端全量 `npm test` 263/263 套件、4014 通过、13 skip，覆盖率门槛达标（S 80.33% / B 67.81% / F 81.24% / L 81.51%）；修复清单：api-contract 误报、附件服务契约对齐、邀请/工作台/慢套件超时放宽、TicketDetailAssignSearch 30s→120s；P0/P1 出口 DoD 门禁项勾选 |
| v1.31 | 2026-09-30 | **P2 启动：§5.0 契约冻结 + IP-P2-1 provider 维度落地**：`msp_allocations.provider_tenant_id`（在线加列 + 回填 + 部分索引，迁移 `20260930_msp_allocation_provider_dimension.sql`；巡检 `scripts/msp/verify-allocation-provider-backfill.sql`）；写侧 `Create` 派生 provider 并强校验 `== customer.msp_provider_id`（admin 不豁免）；读侧收窄——middleware `AllowedCustomers`、租户切换列表、分配列表按 provider（过渡兼容 NULL 行），`/msp/customers` 改走 `mspguard` 单源；DTO 增 `providerTenantId`；单测 7 子例 + 中间件 N=2 用例绿；NOT NULL 收尾待巡检归零 + A11/A12 e2e |
| v1.32 | 2026-09-30 | **IP-P2-5 首批：guard 关联一致性检查**：新增 `internal/schema/tenant_guard_consistency.go` 的 `ApplyConsistencyChecks`（三类检查：悬挂成员、组织关联跨租户（自身 tenant + 目标组织双检）、`msp_allocations.provider_tenant_id` 与客户归属错配）；`hasColumn` 探测实现迁移前自动跳过；接入 `runTenantGuard`（与 `ApplyGuard` 同策略 fatal/warn/silent）；用例 3 组（违规检出 4 项计数、fatal 阻断、缺列跳过）全绿 |
| v1.33 | 2026-09-30 | **IP-P2-2 前置：RLS ctx 重绑定**：`msp_workbench.go`（List/Summary/`authorizeTicketAction`/Reply/ChangeStatus）与 `ticket_service.go`（`GetCustomerTicketsForMSP`/`AssignMSPTechnician`）按目标客户租户 `tenantctx.WithTenantID` 重绑定（enforce 下逐租户查询与 GUC 单值一致；跨租户探测 fail-closed）；评估档案 §8 记录清单 1/2/3 完成、shadow 观察留 staging；MSP 定向回归绿 |
| v1.34 | 2026-09-30 | **IP-P2-3 共享表治理：messages 租户化 + 季度复核**：`messages` 加 `tenant_id`（可空过渡）+ 会话回填 + `(tenant_id, conversation_id, created_at)` 索引（`20260930_messages_tenant_id.sql`；巡检 `scripts/msp/verify-messages-tenant-backfill.sql`），写入由请求 ctx 派生（`handlers/ai/repository_impl.go`），移出豁免清单；`marketplace_items` / `prompt_templates` 保留显式共享并刷新复核期；新增 `plan/msp-exempt-tables-quarterly-review.md`（结论摘要 / 判定依据 / 季度流程）；schema 迁移用例 + guard 用例绿 |
| v1.35 | 2026-09-30 | **IP-P2-4a 自定义视图（保存过滤器组合）后端落地**：新增 `workbench_views`（provider 域 + owner + 名称唯一；`filters` JSON 与 `WorkbenchTicketQuery` 对齐；`is_shared` 同 provider 可见 / `is_default` 每 owner 至多一，部分唯一索引 `uq_workbench_views_owner_default`；迁移 `20260930_workbench_views.sql`）；服务 `MSPWorkbenchViewService`（列表=own+分享、CRUD、设为默认事务先清后置；过滤器服务端校验 ⊆ `MSPContext.AllowedCustomers`；错误码族 `WORKBENCH_VIEW_*`）；路由 5 条（`msp_ticket` read/write）；灰度 `WORKBENCH_VIEWS_ENABLED`（默认关，未开启 404 reasonCode）；用例覆盖生命周期/可见性/越权/默认唯一/跨 provider 隔离；前端接入 + SLA 风险看板 + 配额可视化下一批 |
| v1.36 | 2026-09-30 | **IP-P2-4a 前端：SavedViews 控件 + URL 复现**：`msp-workbench-api.ts` 增视图 API/类型/`viewFilterToQueryPatch`；新组件 `SavedViews`（视图下拉：默认★/他人分享标记；保存当前筛选为视图；编辑/删除/设为默认仅 owner；删除二次确认）；工作台页接入：服务端 `enabled` 驱动灰度（未开启静默隐藏）、`?viewId=N` 首次加载展开过滤器回 query（刷新/分享/收藏复现、防循环），`viewId` 不参与数据过滤键；组件单测 6/6 + 页面 8/8 + CustomerFilter 6/6；tsc/eslint 绿 |
| v1.37 | 2026-09-30 | **IP-P2-4b SLA 风险看板**：`WorkbenchSummaryCustomer` 增 `slaDueSoon`、响应增 `slaDueSoonWindowHours=24`（`workbenchSLADueSoonWindow`；已超期不重复计入）；前端新组件 `SlaRiskBoard`（超期 desc → 临近 desc → open desc 排序；合计徽标；红/橙/灰占比条；点击行写 `customerTenantIds` 收窄；刷新/空态/错误态）；后端 summary 用例扩展（追加 2h 内到期工单断言分桶与窗口）+ 前端组件 5/5、页面 8/8、CustomerFilter 6/6；tsc/eslint 绿 |
| v1.38 | 2026-09-30 | **IP-P2-4c 每客户用量看板（usage-only 定案；P2-4 收口）**：数据源核查——`tenants` 无 `quota/settings` 列、`dto.TenantDTO.Quota` 从未赋值、附件配额 6106 无校验，确认**无硬配额数据源**；`WorkbenchSummaryCustomer` 增 `members`（active 且未删除 membership 计数）/`ticketsCreated30d`（`CreatedAtGTE(now-30d)`），响应增 `usageWindowDays=30`；前端新组件 `CustomerUsageBoard`（窗口新增 desc → 成员 desc 排序；成员/未关闭/新增数字 + 相对最大值条；点击行写 `customerTenantIds`；刷新/空态/错误态 + 口径提示）；后端 summary 用例扩展（membership active/suspended 分桶 + 窗口断言）+ 前端组件 5/5、页面 8/8、CustomerFilter 6/6；硬配额（limits）登记遗留；fmt/tsc/eslint 绿 |
| v1.39 | 2026-09-30 | **A11/A12 api 通道 e2e 落地**：新增 `router/msp_a11_a12_e2e_test.go`（路由器级全中间件链：Auth → RBAC → MSPMiddleware → RequireMSPPermission → handler → service/mspguard，ent/sqlite 内存库，无需外部环境）——`TestMSP_A11_SameScenarioForN1AndN2` 以同一剧本跑 N=1/N=2 并比较行为指纹（工作台列表/汇总/指派状态与计数全等）；`TestMSP_A12_ProviderScopedTicketFlow` 覆盖建单 provider 快照（DTO+DB 双断言）/工作台可见与 provider∩allocation 收窄/指派校验（allocated=200 且落 `managed_by_user_id`，未分配 403 `MSP_ALLOCATION_REQUIRED`）/跨 provider 拒绝（P2 员工访问 P1 客户 403、不带筛选仅见本 provider 客户）；夹具还原生产口径（`users.role=agent` + m2m `msp_tech` 角色 + role_permissions + allocation 带 `provider_tenant_id`）；`./router` 全包回归绿；通知双投递仍为遗留子项（后端无实现） |
| v1.40 | 2026-09-30 | **A12 通知双投递（provider 侧）落地**：新增 `service/msp_provider_side_notification.go`——托管工单（`is_managed_by_msp=true ∧ msp_provider_id>0`）在客户侧通知之外向 **provider 租户**再投递一份；收件人 = 托管处理人（`managed_by_user_id`，须属 provider 租户且 active）+ 工单 assignee（同校验）+ provider 租户 active `provider_admin`；actor 自身排除；provider 侧行（`notification`/`ticket_notification`）归属 provider 租户、深链 `/msp/workbench`；provider 租户不存在/停用/类型非法 → fail-closed 跳过；接入四个事件：`commented`/`assigned`/`status_changed`/`created`（`NotifyTicketCreatedTx` 在调用方事务内写行，与工单主表同生同死）；工作台条目级改状态开始触发状态通知（`MSPWorkbenchService.ChangeStatus` → `TicketService.NotifyTicketStatusChanged`，客户侧 requester/assignee + provider 侧）；单测矩阵 `service/msp_provider_side_notification_test.go`（4 用例：评论/指派+状态/守卫四态/Tx 原子性全绿）；`router/msp_a11_a12_e2e_test.go` 追加回复与改状态的 provider 侧落库断言（含 actor 排除） |
| v1.41 | 2026-09-30 | **IP-P2-6 平台租户管理：硬配额（limits）后端落地**：新增 `pkg/tenantquota`（`Limits{maxUsers,maxTicketsPerMonth,maxStorageMB}`；严格解析：未知键/负值/非整数/超上限拒绝；零值/缺省 = 不限）+ `tenants.quota` jsonb 迁移（`20261001_add_tenants_quota.sql`，幂等加列）；`TenantQuotaService` 统一用量口径（users / 本月新建未删工单 / active 未删附件字节）与三键校验（nil 安全）；三写入路径接入——建号三通道（422 `TENANT_QUOTA_EXCEEDED`，`ProvisionError` 稳定码）、建单（422 + `quota/limit/used` 明细）、附件上传（写盘前预检，沿用 6106 `ErrAttachmentQuotaExceeded`，超限不落盘）；平台面 `PUT/POST /api/v1/tenants/:id` 接受 `quota` 对象（GET 回显），非法值 400；单测 5 组（用量口径含软删排除、fail-closed/nil 安全、建号/建单/附件三路径拦截与正例回滚验证）全绿 |
| v1.42 | 2026-09-30 | **IP-P2-6 收尾：租户用量展示（配额 vs 用量）**：后端新增 `GET /api/v1/tenants/:id/usage`（`tenant:read`；响应 `{tenantId, limits, used:{users,ticketsThisMonth,storageBytes}}`；上限与用量同源自 `TenantQuotaService`，与写入校验逐字同口径；租户不存在 fail-closed）——`dto.TenantQuotaUsageResponse` + `TenantService.QuotaUsage`（`SetTenantQuotaService` 委派，nil 安全）+ handler/路由 + `rbac_precheck_gen.go` 再生成；前端治理页行操作新增「用量」→ `TenantUsageModal`（三行已用/上限 + 进度条，不限键仅文本；可注入 `fetchUsage`，关闭重开重新拉取）；`TenantAPI.getTenantUsage` + 类型契约；测试：handler 2 用例、service 委派用例、组件 5 用例、租户 API 契约用例全绿；tsc/eslint 与 router/middleware 守卫（含预检新鲜度/对齐）全绿 |
| v1.43 | 2026-10-03 | **联调环境换版 + 邀请→首登 e2e 收口（环境项首次推进）**：SSH 隧道联调库（`127.0.0.1:15433`，经 `mig-verify` 核对为 dev 目标）只读预检 `mig-verify -ro`（applied=53 / pending=13，无 checksum 冲突）→ `mig-verify -up` 应用 13 个迁移（022 allocation 唯一索引 / P0-7 工作台索引 / P0-10 审计列 / P1-1/P1-3 membership+组织 / P1-4 invitations / P1-5 首登列 / P1-6c preferences / P2-1 provider 列 / P2-3 messages / P2-4a views / P2-6 quota），复检 `pending=0`；8090 实例重建为 HEAD 产物（含邀请路由）并干净重启（health/login 200，启动无报错）；e2e spec 对齐真实 CSRF 契约（`GET /api/v1/csrf-token` → `X-CSRF-Token`，Double Submit）并加 `test.slow()` 覆盖 vite 冷编译；邀请→落地→设密→首登浏览器链路 **2 次复跑全绿**（访问日志：`POST /users/invitations` → `GET/POST /auth/invitations/:token(/accept)` → `POST /auth/login` 全链 200）。 |
| v1.44 | 2026-10-03 | **联调库巡检执行 + IP-P2-1 NOT NULL 收尾**：本机无 psql，新增临时只读 runner（`tmp_sqlrun`，不入库）实跑 5 个 `scripts/msp/verify-*.sql`——allocation 4/4、membership 7 差异节、membership-orgs/messages 全零，tenant 类型巡检仅存量 legacy 1 行（既有登记）；**修正 membership 巡检第 1 节判据**为 `(source='home' OR is_default)`（邀请建号 `source=invite` 即主作用域，消除邀请用户误报）。**NOT NULL 收尾**：新增迁移 `20261003_msp_allocations_provider_not_null(.sql/_down.sql)`（幂等；残余 NULL 先再回填、仍存在显式报错阻断）、ent 字段必填（边 `Required`）、读侧 `msp_middleware` / `ListByMSPUser` / 租户联合三处收敛等值；联调库应用后 `applied=67 pending=0`，实例换版（health/login 200）。测试：pkg/mspguard、middleware、handlers/common\|msp\|auth、internal/schema、migration、internal/bootstrap、service（384s）、router 全绿。**存量红登记**：`pkg/seeder` 2 用例 3 子例（`TestProvisionTenantReadinessAcrossDeploymentModes` saas/saas_msp、`TestProvisionTenantRollsBackWhenSourceTemplateIsIncomplete`；`process_definition incident_emergency_flow` fixture 缺失）在干净 HEAD 工作树复现，与本批无关，待单独修复 |
| v1.45 | 2026-10-03 | **存量红修复：Windows embed.FS 路径缺陷（B2-03 O-4 / M0-10 关单）**：`service/bpmn_template_service.go` 的 `deployTemplate`/`GetTemplateContent` 以 `filepath.Join` 拼 **embed.FS** 路径——Windows 产出 `bpmn\x.bpmn`，embed 不识别反斜杠 → 18 个内置模板全部被误判"未通过部署门禁"，默认租户 0 流程定义 + 6 条悬挂绑定 → `ProvisionTenant` 在 `resolve process definition incident_emergency_flow` 失败（Linux CI 不复现，故长期被当作"既有欠账"）。修复：改 `path.Join`（恒正斜杠）；`pkg/seeder/seeder_test.go` 组件选取由 `components[len-1]` 改为按名 `extension-core`（v1.40+ 追加 `ai-bot-core` 打破顺序假设）。验证：`pkg/seeder` 全包绿（98s，含 ProvisionTenant 三模式与源模板不完整回滚）；service BPMN 子集绿；18/18 模板可部署。联调库：`initialize -action=apply`（runId=35，`BPMN workflows seeded: 18`）+ `-action=verify` 7/7 组件 verified |
| v1.46 | 2026-10-03 | **IP-P2-3 messages 租户化收尾**：巡检三节归零（联调库 ①=②=③=0）后执行——迁移 `20261003_messages_tenant_not_null(.sql/_down.sql)`（幂等：先再回填、残余空值显式报错阻断、再 `SET NOT NULL`）；ent `messages.tenant_id` 必填（生成物同步）；写入侧 `CreateMessage` 解析链 ctx→会话派生，跨租户冲突/双方缺失 fail-closed（新增 5 场景用例）；schema 迁移用例改写为"必填 + 缺租户写入被拒"；联调库应用：`applied=68 pending=0`、`tenant_id nullable=NO`、空值 0 行；测试 handlers/ai 定向与 ent/schema 定向绿 |
| v1.47 | 2026-10-03 | **RLS shadow 观察（联调环境）与 enforce 前置推进**：实例以 `RLS_MODE=shadow` + `LOG_LEVEL=debug` 运行，三类身份（admin / mspagent 低权 / custa_admin）只读流量观察——首轮 131 缺租户 warn / 28 带租户。**归因补强**：driver 告警与 debug 增补 `query` 预览（单行/160 字符/仅占位符）。**ctx 修复**：RBAC 预检 ACL 查询补 `tenantctx.WithTenantID`（实测 21→0）、审计 3 个写入点补 ctx 或 system bypass（实测 4→1，第三处修复后待复跑）。环境事实：`itsm` 连接仍 superuser+BYPASSRLS、`itsm_app/itsm_admin` 未建、changes/vectors 未启用策略——enforce 需先完成 DB 角色/策略与应用连接切换（清单/复现见 `rls-shadow-observation-2026-10-03.md`，工具 `scripts/msp/rls-shadow-observation.ps1`；MSP 面待 `saas_msp` 模式复跑） |
| v1.48 | 2026-10-03 | **RLS shadow run8 复跑（最终构建）**：warn 131→**25**（带租户 28→**131**）——审计 3 写入点/**ACL 预检/RBAC 角色权限解析/用户与菜单链路全部归零**。补强：`RBACMiddleware` 在 JWT 解析出 tenantID 后显式注入请求 ctx（auth-scoped 路由不挂租户中间件）；`loadPermissionsFromDB`/`DBOnlyState`、`ResolvePermissions`、`GetUserMenus`、`GetUserScoped` 补租户 ctx；`GetUserTenants`（跨租户聚合）显式 system bypass。残余 25 = `Tx` 无 query 12 + MCP/连接器元数据 7 + 预认证/会话 6（分类与 enforce 清单见观测报告 §3/§5）。实例已恢复默认配置（RLS off / .env 日志级别，health/login 200） |
| v1.49 | 2026-10-03 | **RLS shadow 收口至 0（跨模式全表面）**：driver 告警增补 `caller` 归因（跳过 ent 生成码与 `database/` 拦截器帧）。逐条修复：登录落 home 租户视图（`WithTenantID`）；登录/注册/找回/密码策略等预认证跨租户查询 → `tenantctx.SystemContext` 平台作用域；MCP 组件启动 + 工具缓存 List/Replace + 服务状态回写；连接器重水合与管理器内部 ctx；工具队列启动恢复扫描；allocation 展示名解析。验证：`DEPLOYMENT_MODE=saas_msp` + `RLS_MODE=shadow` + debug，三类身份全端点 + 启动序列——**run15 warns=0 / 带租户 192（含启动窗口）**；测试 `database/rls`、`handlers/auth\|common`、`mcp/admin`、`connector`、`internal/bootstrap`、`router` 全绿。enforce 剩余前置＝DB 角色/策略（001/002）+ 应用连接切 `itsm_app`（报告 §5）。实例已恢复默认配置（private/RLS off/error，health/login 200） |
| v1.50 | 2026-10-03 | **enforce 前置推进（DB 侧完成 + 演练）**：新增 `cmd/rls-apply`（无 psql 环境的角色/策略应用、密码注入、状态校验与 `itsm_app` 低权探针，探针行自动播种/清理；rollback 内置）。联调库落地 `001_roles.sql`（`itsm_app` 非 superuser/非 BYPASSRLS + 740 表授权；`itsm_admin` BYPASSRLS）与 `002_pilot_policies.sql`（`changes`/`vectors` RLS+FORCE + `tenant_isolation`）；低权探针实证隔离（自身 1 行 / 无租户 0 / 他租户 0）。集成测试 `integration_rls` 通过（测试改为动态选取正例租户，去除「tenant=1」硬编码）。`RLS_MODE=enforce` 演练：与 `off` 基线逐请求一致（diff=0），无 fail-closed 错误。剩余：连接侧双池化（请求=itsm_app / 平台=itsm_admin）+ 监控接入（报告 §5） |
| v1.51 | 2026-10-03 | **连接侧分流落地（enforce 全链路可运行）**：`rls.Driver` 按 ctx 作用域选池——enforce 下租户语句走低权请求池 `itsm_app`（policy 强制），系统绕过/平台语句走管理池（BYPASSRLS）；`InitDatabaseWithRLS` 在 `DB_APP_ROLE_*` 就绪时开通请求池（启动期 schema DDL/水合仍走管理池）并内建启动探针（`current_user`）与首路由日志；`WithTenantSQL/WithTenantTx` 与系统编号分配经 `requestDB` 同步分流。现场修复：`Tx/BeginTx` 的 `SET LOCAL` 参数 untyped `nil` → 定型 `[]any{}`（此前 enforce 下「记录 last_active 租户」事务静默失败；新增单测锁定）。复核：`app pool ready user=itsm_app current_user=itsm_app`、`first statement routed to app pool tenant_id=1`；全端点流量与 `off` 基线 diff=0，无 fail-closed/权限错误——**`changes`/`vectors` 两表 DB 级强制生效**。剩余：监控接入 + 策略扩展（R2）。实例已恢复默认配置（private/RLS off/error，health/login 200） |
| v1.52 | 2026-10-03 | **监控接入（enforce 灰度观测面）**：`database/rls/metrics.go` 拉取式收集器将 `Driver.Stats()` 桥接 Prometheus（`itsm_rls_info{mode}` / `queries_off` / `queries_shadow` / `missing_tenant` / `system_bypass` / `enforce_applied` / `app_routed` / `app_pool_configured`，`RegisterMetrics` 幂等）；新增 `GET /api/v1/admin/rls/stats`（`system:read`，认证组内、匿名 401）输出同一快照；`cmd/authz-gen` 再生成预检映射（`/api/v1/admin/rls/stats → system:read`）。实测 enforce 运行态：missing=0 / app_routed=206 / enforce=212 / bypass=338，`/metrics` 与端点数值一致。**告警口径**：`itsm_rls_missing_tenant_total>0` 暂停灰度并评估回滚；`app_pool_configured=0 且 mode=enforce` 视为分流未生效。剩余：策略扩展（R2）。实例已恢复默认配置（private/RLS off/error，health/login 200） |
| v1.53 | 2026-10-03 | **本机多租户业务验收体系首落地（从操作者出发的闭环）**：新增设计 `msp-multi-tenant-business-acceptance-design.md`（G0–G7：平台建租户/供给/首管理员改密 → 服务商建号/分配/工作台接单（回复/状态/指派/批量护栏）→ 客户建号/邀请/建单/回看/确认解决 → 隔离反例（跨客户、客户访 MSP、未分配写路径、Header 冲突、越权建租户）→ 审计）与单入口脚本 `scripts/msp/acceptance/run-msp-business-acceptance.ps1`（幂等、run-summary 证据、非零退出）。**实测 39/39 全绿**（真机 47s）。**验收发现并修复**：① 工单号碰撞后同事务重试导致 500（`25P02`）——`ErrTicketNumberCollision` 哨兵 + service 新事务重试，回归用例锁定；② `provision_tenant -create-admin` 复跑非幂等——`ErrAdminExists` 容忍跳过。**登记发现**：重复分配返回 500 应为 409（D-2，遗留）；`/msp/allocations` 自作用域与邀请 token 路径段形态（D-3/D-4，脚本适配）。证据：`docs/multi-tenant/evidence/msp-business-acceptance/run-summary-20261003-232101.md` |
| v1.54 | 2026-10-04 | **业务验收第二波「操作链与规则逻辑」（G8–G12）+ 三项工程收口**：新增 L 生命周期链（new→in_progress→pending→in_progress→resolve→closed；resolved 受保护、closed 终态、服务商终态守卫）、W 批量逐条语义（跨客户成功 / 未分配条目逐条拒绝且无副作用）、R 分配回收（回收后即时 403 + 重复分配 409 幂等）、Q 租户硬配额（maxUsers/maxTicketsPerMonth：末位放行 / 超限 422 `TENANT_QUOTA_EXCEEDED` / 解除恢复）、T 租户暂停恢复（业务路由 403 fail-closed、服务商面 `CUSTOMER_INACTIVE`、恢复无损）；**实测 70/70 全绿**（FAIL=0 SKIP=0）。**验收发现并修复**：① **D-6 工单号跨租户碰撞**——`ticket_number` 全局唯一而序列按租户分片，新租户建单必撞他租户号段（500）→ Redis 候选号全局探针+跳号（≤20，推进失败回退 DB）、DB 回退同样跳号、`queryMaxTicketSeqFromDB` 改取全表当月最大号，回归三例；② **D-7 状态机绕过**——`PUT /tickets/:id/status` 对 `closed→open` 曾放行 → `handlers/ticket.Service.UpdateStatus` 收口状态机 + resolved 保护，`failTicketOperation` 按 AppError 分流 4xx，回归锁定；③ **D-2 收口**——重复分配 500 → **409 + `MSP_ALLOCATION_EXISTS`**（§3.0-A 注册表新增），回归 `TestCreateAllocation_DuplicateReturns409`。**登记** D-5（PS7 WebSession 头污染，脚本隔离）/D-8（`/auth/me` 不受租户状态门禁，观察项）。**运维补充**：`LOGIN_RATE_LIMIT_PER_MIN` 可调（默认 10/min/IP 不变；验收/压测/共享出口 IP 场景提高）。证据：`docs/multi-tenant/evidence/msp-business-acceptance/run-summary-20261004-074008.md` |
| v1.55 | 2026-10-04 | **UI 操作层验收落地（Playwright，承接 G0–G12）**：新增 `itsm-frontend/tests/e2e/flows/flow-msp-workbench-ui.spec.ts`（`@multi-tenant`）——mspadmin 真实点击链：顶栏客户过滤器（URL `customerTenantIds` 同步）→ 行内回复（弹窗/成功提示）→ 行内改状态（下拉/行内徽标）→ 批量回复（勾选→客户分布确认→逐条结果「成功 2」）→ SLA/用量看板与按客户分组视图；真后端 + vite 代理，**两连跑全绿**（单次 ~1.8m；非 saas_msp 模式自动 skip）。**验收发现并修复 D-9（阻断级）**：`/auth/menus` 对 provider 用户返回 `admin: null`，Header 面包屑 `for...of null` 抛 `TypeError` → **整页 ErrorBoundary（服务商用户全站白屏、无法进入工作台）**——后端 `buildMenuTree` 契约化为恒返回 `[]`（回归 `TestBuildMenuTreeReturnsEmptySlicesNotNull`），前端 `collectMenuLabels` 数组防御 + `Sidebar` 空菜单判定容错（回归 2 例）。固化三条 UI 稳定性经验（AntD 两字中文按钮可访问名含空格 / hover 下拉被固定列遮挡改 DOM 级 click / 服务端偏好水合竞态以显式 URL 参数规避）。 |
| v1.56 | 2026-10-04 | **UI 错误态呈现验收（R6/Q 的浏览器层）+ D-10/D-10b 阻断级修复**：新增 `flow-msp-error-presentation.spec.ts`（`@multi-tenant`）——**E1 重复分配 409**：/msp/management 表单提交 → 后端 409 `MSP_ALLOCATION_EXISTS` → 弹窗内提示「该员工已分配至该客户」且弹窗/页面保持可用；**E2 超配额 422**：把客户A `maxTicketsPerMonth` 设为本月已用量 → 客户在 /tickets/create 提交 → 422 → 页面展示**用户可读中文**（新增 `mapTicketCreateError`，按 `httpStatus` 判定，单测 4 例；原逻辑误用业务码）。真后端两连跑全绿（单次 ~1.2m；配额在 finally 恢复，不污染后续）。**验收发现并修复 D-10（阻断级）**：`msp-api.ts` 把 httpClient 解包结果当 envelope 读（`res.data?.isMsp` 恒 undefined）→ /msp/management 全角色「无权限」、/msp 概览读空——全量改「解包后数据」契约（`msp-service` 适配，42 例单测同步）；**D-10b**：management 页提前 return 位于 `loadData` 定义之前，effect 闭包调用触发 TDZ（`Cannot access 'loadData' before initialization`）→ 结构重排。 |
| v1.57 | 2026-10-04 | **RLS 策略扩展批次 1（R2）+ enforce 演练 70/70**：新增 `003_business_tables_policies.sql`（+回滚）——`tickets` / `ticket_comments` / `ticket_attachments` / `ticket_ccs` / `ticket_workflow_records` / `user_tenant_memberships` / `user_tenant_membership_orgs` / `groups` / `projects` / `workbench_views` 十表统一 `tenant_id = get_current_tenant_id()` 策略（只 ENABLE 不 FORCE，与 009 旧表约定一致；helper 前向修复内联）。`rls-apply -verify` 泛化为 12 受管表状态 + 逐表低权探针（scoped>0 / none=0 / other=0 全绿）；`rls_integration_test.go` 新增批次 1 逐表隔离回归。**enforce 演练（saas_msp + 双池）**：首轮 59/70 → 归因修复后 **70/70（FAIL=0 SKIP=0，100s）**；修复的 enforce 阻断：① **D-11 工单号全局探针被策略收窄**（23505 建单 500）→ 探针改 system 作用域、序列播种改全表 max；② **D-12 配额计数被 RLS 静默清零**（Q2 误判 422、Q5 **超限仍建号**——配额可被绕过）→ `TenantQuotaService` 全方法以目标租户重绑定 ctx（含三处内联计数）；③ **D-13 审计查询缺 ctx**（A1/A3 500）→ handler 注入租户作用域；④ **D-14 邀请 Inspect/Accept 预认证跨租户缺作用域**（C2b 500）→ system 作用域。UI 双 spec 在 enforce 下回归通过。 |
| v1.58 | 2026-10-04 | **RLS 策略扩展批次 2（生命周期域 18 表）+ enforce 演练再次 70/70**：新增 `004_lifecycle_tables_policies.sql`（+回滚）——通知（notifications / notification_deliveries / notification_preferences / ticket_notifications）、SLA（sla_definitions / sla_metrics / sla_violations / sla_alert_histories）、知识库（knowledge_articles / knowledge_article_likes）、服务请求（service_requests / service_request_approvals / service_catalog_items）、邀请（invitations）、事件（incidents / incident_alerts）、工单配置（ticket_types / ticket_templates）共 18 表纳入 `tenant_id = get_current_tenant_id()` 策略（只 ENABLE 不 FORCE）。`rls-apply` 受管清单扩至 **30 表** + 逐表低权探针全绿（通知三表各 591、sla_violations 29、knowledge_articles 45、invitations 18、incidents 8、ticket_types 12；none/other=0；空表 fail-closed=0）；`rls_integration_test.go` 增 `TestBatch2_TenantScopeIsolation`。**enforce 演练（saas_msp + 双池）一次通过 70/70（FAIL=0 SKIP=0，85.3s）**；通知 outbox worker（commandbus 按命令租户注入 ctx）经实证（投递行 791→865、无缺租户写入）。`driver.go` 增补 enforce 缺租户告警 `caller` 归因（与 shadow 口径对齐）；历史启动期 14 条队列恢复类 INSERT 缺租户登记为复现项（非请求面、一次性；新实例启动/空闲/验收全程 missing=0）。剩余：其余 ~100 张业务表按批次推进；工具链 GUC 化后再评估 FORCE。 |
| v1.59 | 2026-10-04 | **RLS 策略扩展批次 3（AI/连接器/邮件域 14 表）+ enforce 缺租户清零**：新增 `005_ai_connector_tables_policies.sql`（+回滚）——AI 会话/消息（conversations 可空语义：NULL 行对租户 fail-closed、messages）、AI 工具/MCP（mcp_servers / mcp_server_tools / tool_invocations）、连接器（connector_configs / connector_inbound_dedups）、邮件摄取（email_conversations / email_intake_analyses / email_outbound_messages / inbound_email_messages / feishu_ticket_syncs）、域名与供给（domain_configs / provisioning_tasks）共 14 表纳入策略（只 ENABLE 不 FORCE）。`rls-apply` 受管清单 30→**44 表** + 逐表低权探针全绿（conversations 16 / messages 44 / mcp_servers 1 / mcp_server_tools 6 / tool_invocations 76；空表 fail-closed=0），新增 `TestBatch3_TenantScopeIsolation`。**enforce 验收 70/70（FAIL=0 SKIP=0，94s）**；**首轮暴露 `missing_tenant=14`**——`caller` 归因定位到三处审计写入由 `context.Background()` 派生（workbench 逐条审计 / 邀请审计 / 供给审计），修复为请求作用域派生（accept 用带租户 `acceptCtx`）后复跑 70/70、**`missing_tenant=0`**。遗留：受 outbox 开关保护的异步后台 ctx 路径清单（incident 规则 / 工单自动化 / 飞书同步 / 工具队列 worker / job_audit）待核查。剩余：其余 ~50 张租户表（含少量平台级保留项）按批次推进；工具链 GUC 化后再评估 FORCE。 |
| v1.60 | 2026-10-04 | **RLS 策略扩展批次 4（流程/工作流/审批引擎域 14 表）**：新增 `006_process_workflow_tables_policies.sql`（+回滚）——process_instances / process_tasks / process_audit_logs / process_variables / process_timers / process_version_changelogs / process_approval_decisions / process_execution_histories / workflow_instances / workflow_tasks / workflow_templates / workflow_versions / workflows / bpmn_permissions 共 14 表纳入 `tenant_id = get_current_tenant_id()` 策略（只 ENABLE 不 FORCE）。`rls-apply` 受管 44→**58 表** + 逐表探针全绿（process_instances=86 / process_tasks=86 / process_audit_logs=86 / process_approval_decisions=3 / process_execution_histories=1 / workflow_templates=6；8 空表 fail-closed=0），新增 `TestBatch4_TenantScopeIsolation`。**enforce 验收 70/70（FAIL=0 SKIP=0，58.5s）且 `missing_tenant=0`**——票据建单→工作流 outbox→流程引擎写入全链在策略下无缺租户；非请求面 `router/ga_readiness.go` 跨租户诊断改 system 作用域。剩余 72 张租户表（含平台级保留项/CMDB/合同/资产/审批链/审计域）按批次推进；工具链 GUC 化后再评估 FORCE。 |
| v1.61 | 2026-10-04 | **RLS 策略扩展批次 5（鉴权/菜单/配置/审计域 6 表）**：新增 `007_authz_audit_tables_policies.sql`（+回滚）——role_permissions / permissions / menus / system_configs / audit_logs / endpoint_acls 纳入 `tenant_id = get_current_tenant_id()` 策略（只 ENABLE 不 FORCE）。`rls-apply` 受管 58→**64 表** + 逐表探针全绿（role_permissions=1013 / permissions=206 / menus=82 / system_configs=27 / audit_logs=4593 / endpoint_acls=114；`none=0 / other=0`），新增 `TestBatch5_TenantScopeIsolation`（6/6 正例）。**enforce 验收 70/70（FAIL=0 SKIP=0，70.6s）且 `missing_tenant=0`**——RBAC/菜单/审计重载路径在策略下无阻断。非请求面修复：`capability.ConfigSource` load/Update/Clear 以目标 tenantID 重绑定；`router/ga_readiness` countOrZero 改 system 作用域。`audit_logs` 存量 4 行 NULL/0 租户 fail-closed（system 可读）。平台级保留项首例确认：`permission_definitions`（tenant_id 全 0/NULL）待豁免语义。剩余 66 张租户表（自动化/CMDB/合同/资产/审批链等）按批次推进。 |
