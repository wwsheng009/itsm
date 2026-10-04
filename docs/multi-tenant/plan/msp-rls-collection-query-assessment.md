# RLS 集合查询评估（跨客户工作台）· IP-P1-7 前置

> 状态：**已评估（2026-09-30）**｜结论：**保留逐租户查询，不引入 `tenant_id IN (...)` 集合查询**；`tickets` 纳入 RLS policy 前须完成本页 §5 前置清单（实施归 IP-P1-7）。
> 适用：`MSPWorkbenchService`（列表/汇总/条目级动作/批量）、`TicketService.AssignMSPTechnician`。
> 关联：工作台方案 §3.4（WB-R2/WB-R4）、`01-architecture.md` §4（隔离四层）。

---

## 1. 结论（TL;DR）

1. **集合查询不做**。工作台维持"逐租户查询 + 内存合并"（P0 已实现），它是当前唯一与 RLS `enforce` 天然兼容的形态：每条 SQL 只面向一个租户，GUC 单值与查询语义一致，无需任何 bypass。
2. **发现 enforce 前置缺口**：工作台请求的 `ctx` 租户 = provider（JWT 家租户），而 per-tenant 查询/条目读取的目标是客户租户。一旦 `tickets` 启用 policy，`enforce` 下这些查询将被过滤为空（列表全空 / `Get` 报 not-found）。修复方式 = **在授权边界后为每次目标租户查询包 `tenantctx.WithTenantID(ctx, tid)`**（§5 清单第 1/2 条）。
3. 未来若性能触发集合查询，唯一可接受形态是 **B3 授权集合 GUC**（`app.allowed_tenants` + policy `tenant_id = ANY(...)`），需独立 RFC；**B2（SystemBypass/BYPASSRLS 走请求面）明确否决**。

## 2. 现状事实（证据）

| # | 事实 | 证据 |
|---|---|---|
| F1 | RLS 三档 `off`/`shadow`/`enforce`；enforce 下 autocommit 查询以 `entsql.WithVar` 在专用连接 `SET app.current_tenant`，结果集关闭自动 RESET；无租户且非 bypass → fail-closed | `database/rls/driver.go:230-307`、`config/config.go:306-322` |
| F2 | policy 单值 GUC：`tenant_id = NULLIF(current_setting('app.current_tenant', true),'')::bigint` | `database/rls/migrations/002_pilot_policies.sql:36-38`、`migrations/20260828_create_alerts.sql:31-33` 等 |
| F3 | 当前仅有试点表启用 policy：`changes` / `vectors` / `alerts` / `ai_analysis_result` / `attachments` / `llm_provider_configs` / `llm_user_preferences`；**`tickets` 未启用** | 同上 + `migrations/20260922/20260924_*` |
| F4 | 工作台列表 = 逐租户 `TenantIDEQ(tid)` + `limit+1` + 内存归并排序（非 `IN`） | `service/msp_workbench.go:164-199` |
| F5 | 汇总同形：每租户 3 个 `Count(ctx)`（open/slaRisk/unassigned） | `service/msp_workbench.go:456-475` |
| F6 | 条目级授权链先 `Ticket.Get(ctx, ticketID)` 再应用层比对 `declaredTenantID` | `service/msp_workbench.go:532-542` |
| F7 | 批量逐条动作复用同链路；`assign` 落到 `TicketService.AssignMSPTechnician`（内部 `client.Ticket.Get` + `TenantID` 比对） | `service/msp_workbench.go:684-704`、`service/ticket_service.go:2558-2575` |
| F8 | 服务端集合上限：租户集合 ≤50（`workbenchTenantMax`）、单页 ≤200（`workbenchLimitMax`）；查询索引已就绪 `(tenant_id, status, updated_at)` 等 | `service/msp_workbench.go:45-50`、`migrations/20260502_msp_workbench_indexes.sql` |
| F9 | `tenantctx.WithTenantID` 已具备（canonical ctx 租户写入点）；`WithSystemBypass` 注释将"MSP 跨租户操作"列为合法用途，但**当前生产代码无调用点**（仅 tenantctx/rls 自身与测试） | `common/tenantctx/tenantctx.go:37-39, 62-67`；全库 `WithSystemBypass` 检索 |

## 3. 方案对比

| 方案 | 描述 | RLS enforce 兼容性 | 结论 |
|---|---|---|---|
| **A. 逐租户查询（现行）** | k 次单租户 SQL + 内存归并；k ≤ 50 | ✅ 每次查询单租户 = GUC 单值；无 bypass | **保留**（配合 §5 修复） |
| B1. `tenant_id IN (...)` 直查 | 单 SQL 集合过滤 | ❌ GUC 仍是 provider 租户 → policy 把客户行全部滤掉 | 不可用 |
| B2. `IN` + SystemBypass / BYPASSRLS 连接 | 请求面走越权角色 | ⚠️ 可行但使**整条请求链路**失去 DB 兜底；需双连接池/角色治理；违背"唯一合法跨租户通道"最小化原则 | **否决** |
| B3. 授权集合 GUC | 新增 `app.allowed_tenants`（数组），policy 增补 `OR tenant_id = ANY(...)`；集合由服务端授权结果注入 | ✅ DB 层可表达"有界集合"；风险=集合注入错误即越权，需 fail-closed + 审计 | 备选（独立 RFC，见 §4 触发条件） |
| B4. SECURITY DEFINER 视图/函数 | 授权下沉数据库对象 | ⚠️ 第二套权限模型 + owner 运维；审计口径割裂 | 不推荐 |
| C. Provider 读模型投影 | outbox 物化工作台表 | —（投影表自身仍需租户列/策略） | 性能触发后再议（P2） |

## 4. 决策与升级触发条件

- **决策**：维持 A。理由：k 有界（≤50）+ 已有复合索引 + `limit+1` 早停；跨租户合并正确性已在 P0 单测/回归覆盖；零 bypass 使 enforce 切换的爆炸半径最小。
- **升级 B3 的触发条件**（满足其一再开 RFC）：
  1. 常态 `k > 20` 且列表 P95 > 800ms（staging/生产实测）；
  2. 产品要求跨客户**强一致**的全局排序分页（内存归并须全量拉取时会放大成本）。
- B3 实施前置：driver 双层 SET、policy 迁移、集合签名/校验、shadow 双跑对比、越权注入反例测试。

## 5. `tickets` 纳入 policy / 切 `enforce` 前置清单（归 IP-P1-7）

1. **列表/汇总逐租户查询包 ctx**：`service/msp_workbench.go` 循环体内 `qctx := tenantctx.WithTenantID(ctx, tid)`，`All/Count(qctx)`；`loadTenantMeta`/`loadAssigneeNames` 中面向客户租户的数据查询同样处理。
2. **条目级动作包 ctx**：`authorizeTicketAction` 在 mspguard 授权通过后，用 `tenantctx.WithTenantID(ctx, declaredTenantID)` 执行 `Ticket.Get`；`Reply/ChangeStatus` 及批量逐条循环、`TicketService.AssignMSPTechnician`（单条+批量 assign 共用）同口径。
3. **平台表核对**：`tenants`/`users`/`msp_allocations`/`user_tenant_memberships` 等确属 `TenantExemptTables` 且无 policy；有 policy 的表必须逐查询确认 scope。
4. **shadow 观察**：MSP 场景 `rls: query without tenant scope` 计数为 0；staging 开 `tickets` policy 后 `enforce` 跑通 WB-A1–WB-A6 + 批量链路。
5. **审计口径**：请求面不引入 bypass；如未来引入，绑定"越权尝试/bypass 使用"审计（WB-R2）。

## 6. 反向论证（为什么不在 P1 直接做集合查询）

- 收益有限：k 小 + limit+1 早停，单请求成本近似 O(k·log n)，实测满足当前 SLO；
- 成本显著：B3 需要新 GUC 注入面 + policy 变更 + 集合授权可信链，任一环节出错即静默越权；
- 时机不当：`tickets` 尚未纳入 policy，enforce 尚未灰度；先做 A 的 ctx 修复可在零新机制下获得 DB 层兜底。

## 7. 证据索引

- `itsm-backend/database/rls/driver.go:142-165, 186-201, 230-307`；`database/rls/rls.go:63-75`
- `itsm-backend/config/config.go:306-322`；`docs/multi-tenant/02-deployment-and-configuration.md:53-57`
- `itsm-backend/migrations/20260828_create_alerts.sql:29-33`、`20260831_ai_analysis_result_expand.sql:36-40`、`20260922_create_attachments_expand.sql:48-52`、`20260924_create_llm_providers_expand.sql:63-73`、`database/rls/migrations/002_pilot_policies.sql:30-59`
- `itsm-backend/service/msp_workbench.go:45-50, 115-199, 442-475, 532-542, 684-704`
- `itsm-backend/service/ticket_service.go:2558-2575`；`itsm-backend/common/tenantctx/tenantctx.go:37-67`
- `docs/multi-tenant/plan/msp-cross-customer-workbench-and-filter-plan.md:166-169, 224-228`

## 8. 落地进度（2026-09-30，IP-P2-2）

- §5-1/2 **代码完成**：`MSPWorkbenchService` 列表/汇总逐租户查询与条目级动作（`authorizeTicketAction` / `Reply` / `ChangeStatus` / 批量 assign）已按目标客户租户 `tenantctx.WithTenantID` 重绑定；`TicketService.GetCustomerTicketsForMSP` / `AssignMSPTechnician` 同口径——`tickets` 纳入 policy 后即可通过 GUC 单值校验（enforce 下跨租户探测 fail-closed）。
- §5-3 **核对完成**：`tenants` / `msp_allocations` 属 `TenantExemptTables`（无 policy）；`users` / `user_tenant_memberships` 含 `tenant_id` 且未纳 policy（§2 F3 清单）；`loadTenantMeta` / `loadAssigneeNames` 读路径无需重绑定，待其纳 policy 时按同口径处理。
- §5-4 **待环境**：staging 开 `tickets` policy → `shadow` 观察 `rls: query without tenant scope` = 0 后再切 `enforce`（离线环境无法执行，留待部署批次）。
- §5-5 维持：请求面零 bypass（`WithSystemBypass` 仍无生产调用点）。

## 9. 落地进度（2026-10-04，R2 批次 1）

- **`tickets` 已纳入 policy**（`003_business_tables_policies.sql`，与工单外围 5 表 + 组织/成员/工作台同批，共 10 表；`get_current_tenant_id()` 谓词、只 ENABLE 不 FORCE）。
- §5-4 的 shadow/enforce 验证已在本地以**更强口径**完成：`rls-apply -verify` 逐表低权探针（scoped>0 / none=0 / other=0）全绿；`RLS_MODE=enforce` + 双池起服下跑**全量业务验收 70/70（FAIL=0 SKIP=0）** 与 UI 操作链 spec。
- enforce 演练暴露并修复 4 类阻断（D-11 工单号全局探针被收窄 / D-12 配额计数清零可绕过 / D-13 审计缺 ctx / D-14 邀请预认证缺作用域）——均为「请求面零 bypass、按目标租户重绑定或显式 system 作用域」口径，无新增 B2 bypass。
- 结论：本页 §1 决策（逐租户查询 + ctx 重绑定）在 `tickets` 纳入 policy 后成立；集合查询（B3）仍无触发条件。
