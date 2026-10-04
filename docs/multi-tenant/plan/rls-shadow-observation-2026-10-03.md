# RLS shadow 观察记录（联调环境）· 2026-10-03

> 状态：**观察完成（shadow，一轮）**｜范围：联调库（`127.0.0.1:15433/itsm_prod`）+ 8090 实例
> 关联：`database/rls/driver.go`（三档开关）、`database/rls/migrations/001_roles.sql`、`002_pilot_policies.sql`、方案 §7 灰度表（`RLS off→shadow→enforce`）
> 工具：`scripts/msp/rls-shadow-observation.ps1`（本页流量脚本；只发 GET 读请求）

---

## 1. 目标与判据

enforce 前置的"阴影观察"：以 `RLS_MODE=shadow` 运行真实实例，采集**缺租户上下文**（`rls: query without tenant scope`）与**已带租户**（`rls: shadow query`）两类日志，产出：

1. 需在 enforce 前补齐 ctx 的调用点清单（可归因到具体查询）；
2. 可白名单化的路径（预认证 / 平台面 / 系统任务——按设计走 system bypass 或 BYPASSRLS 连接）；
3. DB 侧与连接侧的差距清单（角色 / 策略 / 应用连接身份）。

判据：warn 归零，或剩余项全部落入白名单分类并有对应 system 路径。

## 2. 环境事实（2026-10-03 实测，psycopg2 只读探针）

| 项 | 实测 | 结论 |
| --- | --- | --- |
| 应用连接角色 `itsm` | `rolsuper=true, rolbypassrls=true` | 当前连接**天然绕过** RLS；enforce 只能触发装饰器的客户端 fail-closed，无法验证策略本身 |
| `itsm_app` / `itsm_admin` 角色 | **不存在** | 需按 `001_roles.sql` 建立（密码策略部署时注入） |
| `changes` / `vectors` 策略 | `rowsecurity=false`，策略 0 条 | 需执行 `002_pilot_policies.sql`（幂等） |
| `messages.tenant_id` | `nullable=NO`、空值 0 行（v1.46 收尾） | 与 RLS 无关的一致性核对项 |

## 3. 观察结果

运行方式：实例以 `RLS_MODE=shadow` + `LOG_LEVEL=debug` 启动；流量 = `admin`（超管）+ `mspagent`（**低权**：provider_agent/msp_tech）+ `custa_admin`（客户管理员），覆盖 `auth/me|menus|tenants`、`users`、`tickets`、`tickets/stats`、`dashboard/stats`（MSP 端点在 `DEPLOYMENT_MODE=private` 下 404，未覆盖）。

| 轮次 | 缺租户 warn | 带租户 debug | 说明 |
| --- | --- | --- | --- |
| 首轮（driver 仅 op+首词） | 131 | 28 | 无法归因 → 先补 `query` 预览字段 |
| 复跑（含 ACL/审计修复） | 100 | 52 | `endpoint_ac_ls` 21→0；`audit_logs` 4→1 |
| run8（最终构建，含全部 ctx 修复） | **25** | **131** | `audit_logs`=0、`endpoint_ac_ls`=0、roles/permissions/role_permissions=0、users 列表/菜单链路=0；残余见下表 |
| run10/11（`saas_msp` 模式，覆盖 MSP 面） | 27 → 11 | 186 | MCP store 修复（工具缓存/状态回写）；登录链路收口 |
| run13/14（含 caller 归因） | 2 → 0 | 192 | 仅剩 `msp_allocation:toDTO`（1 对 DISTINCT 查询）→ 修复后归零 |
| **run15（终验，窗口含启动）** | **0** | **192** | `DEPLOYMENT_MODE=saas_msp` + `RLS_MODE=shadow` + debug；三类身份全端点 + 启动序列零缺租户告警 |

**已修复（本轮，代码）**：

1. `database/rls/driver.go`：shadow/enforce 告警与 debug 增补 `query`（单行化、截断 160 字符、仅占位符）——可归因到具体 SQL；此前只有 `op+SELECT/INSERT`，无法定位调用方。
2. `middleware/smart_permission.go` → `loadACLsFromDB`：按 tenant 过滤但未补 ctx；现显式 `tenantctx.WithTenantID(ctx, tenantID)`（预检可能先于租户中间件）。**实测告警 21→0**。
3. `middleware/audit.go`（3 个写入点：`RecordAuthAudit`、`RecordTenantDeniedAudit`、gin 审计中间件）：写入 ctx 由 `context.Background()` 派生且未带租户；现已知租户 → `WithTenantID`，未知（登录失败/预认证）→ `WithSystemBypass`（审计不丢）。**run8 实测 0**。
4. `middleware/rbac.go`：RBAC 预检先于租户中间件（router.go 先挂 `RBACMiddleware`），且 auth-scoped 路由（`/auth/me|menus|tenants`）不挂租户中间件——预检在 JWT 解析出 tenantID 后**显式注入请求 ctx**，并对 `loadPermissionsFromDB`/`DBOnlyState` 两处加载器补租户 ctx。**roles/permissions/role_permissions 告警归零**。
5. `middleware/membership_permission.go`（`ResolvePermissions`）、`service/menu_service.go`（`GetUserMenus`）、`handlers/common/service.go`（`GetUserScoped`）补租户 ctx；`GetUserTenants`（跨租户聚合，IP-P0-6）显式 **system bypass**（平台面白名单）。**用户/菜单链路告警归零**。

**残余 warn 收口（run10→run15，全部清零）**：

driver 告警增补 `caller` 归因（栈回溯跳过 `ent/` 生成码与 `database/` 拦截器帧），逐条定位并修复：

| 站点 | 处置 |
| --- | --- |
| 登录落 home 后租户视图加载（`handlers/common/service.go`） | `WithTenantID`（home 租户已知）；`last_active` 回写同 |
| 登录/注册/找回/密码策略、租户代码解析（预认证跨租户） | `tenantctx.SystemContext(...)` 显式平台作用域 |
| MCP 组件启动（`bootstrap:mcp-startup`）+ 工具缓存 `List/Replace` + 服务状态回写（`mcp/admin/store.go`） | system 作用域（平台组件运行态） |
| 连接器重水合（bootstrap）+ 连接器管理器内部 ctx（`connector/manager.go`） | system 作用域播种 |
| 工具队列启动恢复扫描（`service/tool_queue_durable.go`，跨租户运维语义） | system 作用域 |
| allocation 展示名解析（`service/msp_allocation_service.go:toDTO`，跨 provider/客户） | system 作用域 |

> MSP 专属路径已覆盖：run10 起实例以 `DEPLOYMENT_MODE=saas_msp` 运行，`/msp/*` 全端点 200（`mspagent` provider_agent 低权身份 + allocation 数据就绪），与 private/平台面合并观察至 run15 清零。

## 4. 复现步骤

```powershell
# 1) 以 shadow + debug 启动（勿改 .env，仅进程级覆盖）
$env:RLS_MODE='shadow'; $env:LOG_LEVEL='debug'
Start-Process -FilePath 'E:\projects\itsm\itsm-backend\main.exe' -WorkingDirectory 'E:\projects\itsm\itsm-backend' -WindowStyle Hidden `
  -RedirectStandardOutput "$env:TEMP\itsm-rls.out.log" -RedirectStandardError "$env:TEMP\itsm-rls.err.log"

# 2) 低权 + 平台 + 客户三类会话发流（只读）
pwsh -NoProfile -File scripts/msp/rls-shadow-observation.ps1

# 3) 归因统计（日志在 itsm-backend/logs/itsm.log）
Get-Content itsm-backend\logs\itsm.log -Tail 20000 | Where-Object { $_ -match 'query without tenant scope' } |
  ForEach-Object { ($_ | ConvertFrom-Json).query } | Group-Object { $_.Substring(0,[Math]::Min(60,$_.Length)) } |
  Sort-Object Count -Descending | Select-Object -First 15 Count,Name
```

### 4.1 RLS 迁移与低权校验（无 psql 环境的替代路径）

`database/rls/migrations/*.sql` 可用仓库内工具执行（读取 `.env` 的管理连接；脚本幂等）：

```powershell
cd itsm-backend
go run -tags rlsapply ./cmd/rls-apply -files 001_roles.sql,002_pilot_policies.sql  # 应用
go run -tags rlsapply ./cmd/rls-apply -set-passwords                               # 用 DB_APP_ROLE_*/DB_ADMIN_ROLE_* 替换占位密码
go run -tags rlsapply ./cmd/rls-apply -verify                                      # 角色/策略状态 + itsm_app 低权探针（空表自动播种/清理）
go run -tags rlsapply ./cmd/rls-apply -rollback                                    # 回滚（002→001）
```

真实库集成测试（低权角色 + 策略隔离；两个 DSN 可同为超管，测试内部 `SET ROLE itsm_app`）：

```powershell
$env:RLS_TEST_DSN='host=... port=... user=itsm dbname=... password=... sslmode=disable'
$env:RLS_SETUP_DSN=$env:RLS_TEST_DSN
go test -tags integration_rls -v ./database/rls/...
```

## 5. enforce 推进清单（按序）

1. ✅ **DB 侧（2026-10-03 完成）**：`001_roles.sql` → `itsm_app`（login、非 superuser、非 BYPASSRLS，740 表授权）/ `itsm_admin`（BYPASSRLS），密码经 `rls-apply -set-passwords` 注入（`.env` 的 `DB_APP_ROLE_*`）；`002_pilot_policies.sql` → `changes`/`vectors` `rowsecurity+forcerowsecurity=true` + `tenant_isolation` 策略。
2. ✅ **连接侧（2026-10-03 完成）**：`rls.Driver` 增补**按作用域分流**——enforce 下租户语句路由低权请求池（`itsm_app`，policy 强制），系统绕过/平台语句走管理池（BYPASSRLS）；`InitDatabaseWithRLS` 在 `DB_APP_ROLE_*` 配置且 enforce 时开通请求池（启动期 schema DDL/水合仍在管理池），`WithTenantSQL/WithTenantTx` 与系统编号分配同步分流（`requestDB`）。启动探针与首路由日志已内建（见下）。**pilot 两表（changes/vectors）的真实 DB 级强制自此生效**；其余表待策略扩展（第 6 条）。
3. ✅ **调用点收口（2026-10-03 完成）**：按 §3 残余表逐条补齐 ctx 或显式 system 作用域；`LOG_LEVEL=debug` 复跑至 **warn=0**（run15，含启动窗口与 MSP 面；带租户 192 条）。
4. ✅ **enforce 演练（2026-10-03，含分流）**：`RLS_MODE=enforce` + 双池起服——逐请求与 `RLS_MODE=off` 基线**完全一致（diff=0；22×200 + 1×400 报表参数 + 1×404 视图 flag 关闭）**；无 `enforce mode requires tenant_id`、无 `permission denied`。**现场修复**：`Tx/BeginTx` 的 `SET LOCAL` 参数由 untyped `nil` 改为定型 `[]any{}`（否则 `dialect/sql` 报 `invalid type <nil>. expect []any for args`，「记录 last_active 租户」事务静默失败；新增单测锁定）。
5. ✅ **监控（2026-10-03 完成）**：`Driver.Stats()` 计数器经拉取式收集器桥接 Prometheus（`/metrics` 暴露 `itsm_rls_*`：`info{mode}` / `queries_off` / `queries_shadow` / `missing_tenant` / `system_bypass` / `enforce_applied` / `app_routed` / `app_pool_configured`）；同时提供 `GET /api/v1/admin/rls/stats`（`system:read`）JSON 快照，供无 Prometheus 抓取链路的现场排障。**告警口径**：`itsm_rls_missing_tenant_total > 0` 即暂停灰度并评估回滚；`itsm_rls_app_pool_configured=0` 且 `mode=enforce` 表示分流未生效（检查 `DB_APP_ROLE_*`）。实测（enforce + 分流）：missing=0、app_routed=206、enforce=212、bypass=338，与 `/metrics` 序列一致；匿名访问端点 401。
6. ✅ **策略扩展批次 1（2026-10-04 完成）**：`003_business_tables_policies.sql`（+回滚）将 **tickets / ticket_comments / ticket_attachments / ticket_ccs / ticket_workflow_records / user_tenant_memberships / user_tenant_membership_orgs / groups / projects / workbench_views** 十表纳入 `tenant_id = get_current_tenant_id()` 策略（只 ENABLE 不 FORCE，与 009 旧表约定一致）。`rls-apply -verify` 泛化为 12 受管表状态 + 逐表低权探针；**enforce 演练（saas_msp + 双池）全量业务验收 70/70（FAIL=0 SKIP=0）**，并修复演练暴露的 4 类阻断（见变更记录）。剩余：其余业务表按批次推进；工具链 GUC 化后再评估 FORCE。
7. ✅ **策略扩展批次 2（2026-10-04 完成）**：`004_lifecycle_tables_policies.sql`（+回滚）将**通知（notifications / notification_deliveries / notification_preferences / ticket_notifications）、SLA（sla_definitions / sla_metrics / sla_violations / sla_alert_histories）、知识库（knowledge_articles / knowledge_article_likes）、服务请求（service_requests / service_request_approvals / service_catalog_items）、邀请（invitations）、事件（incidents / incident_alerts）、工单配置（ticket_types / ticket_templates）** 共 18 表纳入 `tenant_id = get_current_tenant_id()` 策略（只 ENABLE 不 FORCE）。`rls-apply -verify` 受管清单扩至 **30 表** + 逐表低权探针全绿（notifications 591/591、notification_deliveries 591/591、ticket_notifications 591/591、sla_violations 29、knowledge_articles 45、invitations 18、incidents 8、ticket_types 12…反例 none/other=0；空表 fail-closed=0）；`rls_integration_test.go` 增 `TestBatch2_TenantScopeIsolation`。**enforce 演练（saas_msp + 双池）一次通过 70/70（FAIL=0 SKIP=0，85.3s）**；commandbus 按命令租户注入 ctx 的设计经实证（投递行随验收 791→865 增长，无缺租户写入错误）。**遗留（归因复现项）**：启动期观测到 14 条队列恢复类 INSERT 缺租户被 fail-closed 拒绝（非请求面、一次性、不随流量/空闲增长）；已为 enforce 缺租户告警补 `caller` 归因（driver.go，与 shadow 口径对齐）；队列排空后新实例启动/空闲/验收全程 `missing=0`。剩余：其余业务表按批次推进；工具链 GUC 化后再评估 FORCE。
8. ✅ **策略扩展批次 3（AI/连接器/邮件域 14 表，2026-10-04 完成）**：`005_ai_connector_tables_policies.sql`（+回滚）覆盖 **conversations / messages / mcp_servers / mcp_server_tools / tool_invocations / connector_configs / connector_inbound_dedups / email_conversations / email_intake_analyses / email_outbound_messages / inbound_email_messages / feishu_ticket_syncs / domain_configs / provisioning_tasks**（统一 `tenant_id = get_current_tenant_id()`，只 ENABLE 不 FORCE；**conversations 可空列语义**：NULL 行对租户会话不可见 = fail-closed，写入侧 `Conversation.Create` 显式 `SetTenantID`，与 messages 收尾口径一致）。`rls-apply` 受管清单 30→**44 表**、逐表探针全绿（conversations 16、messages 44、mcp_servers 1、mcp_server_tools 6、tool_invocations 76；其余空表 fail-closed=0）；`TestBatch3_TenantScopeIsolation` 逐表回归（5 表正/反例 + 9 空表 fail-closed）。**enforce 演练 70/70（FAIL=0 SKIP=0，94s）**。**新增缺租户归因复核**：首轮暴露 14 条 fail-closed 审计写入（workbench×12 + invitation×2；根因：审计 ctx 由 `context.Background()` 派生、丢失租户作用域），修复四处审计助手（`recordWorkbenchAudit` / `recordScopeDenied` / `recordInvitationAudit` / `recordProvisionAudit`，改为从请求作用域派生；accept 用带租户的 `acceptCtx`）后 **`missing_tenant=0`**（70/70 复跑）。**遗留**：受 outbox 开关保护的异步 `Background` ctx 路径清单（incident 规则 / 工单自动化 / 飞书同步 / 工具队列 worker / job_audit）待 outbox 化核查。剩余：其余 ~50 张租户表（含少量平台级保留项）按批次推进；工具链 GUC 化后再评估 FORCE。

9. ✅ **策略扩展批次 4（流程/工作流/审批引擎域 14 表，2026-10-04 完成）**：`006_process_workflow_tables_policies.sql`（+回滚）覆盖 **process_instances / process_tasks / process_audit_logs / process_variables / process_timers / process_version_changelogs / process_approval_decisions / process_execution_histories / workflow_instances / workflow_tasks / workflow_templates / workflow_versions / workflows / bpmn_permissions**（统一 `tenant_id = get_current_tenant_id()`，只 ENABLE 不 FORCE）。`rls-apply` 受管清单 44→**58 表**、逐表探针全绿（process_instances=86、process_tasks=86、process_audit_logs=86、process_approval_decisions=3、process_execution_histories=1、workflow_templates=6；其余 8 空表 fail-closed=0），新增 `TestBatch4_TenantScopeIsolation`（6 正例表 + 8 空表）。**enforce 演练 70/70（FAIL=0 SKIP=0，58.5s）且 `missing_tenant=0`**——票据建单 → 工作流 outbox → 流程引擎写入全链在策略下无缺租户。租户上下文来源核查：HTTP 路径由 Tenant/RBAC 中间件注入 + BPMN 专用 key；outbox 由 commandbus 按命令租户注入；超时扫描双注入；**非请求面修复**：`router/ga_readiness.go` 平台级诊断（跨租户统计逾期/孤儿任务）显式改 system 作用域。剩余：**72 张**租户表（含平台级保留项与 CMDB/合同/资产/审批链/审计域）按批次推进；工具链 GUC 化后再评估 FORCE。

10. ✅ **策略扩展批次 5（鉴权/菜单/配置/审计域 6 表，2026-10-04 完成）**：`007_authz_audit_tables_policies.sql`（+回滚）覆盖 **role_permissions / permissions / menus / system_configs / audit_logs / endpoint_acls**（统一 `tenant_id = get_current_tenant_id()`，只 ENABLE 不 FORCE）。`rls-apply` 受管清单 58→**64 表**、逐表探针全绿（role_permissions=1013、permissions=206、menus=82、system_configs=27、audit_logs=4593、endpoint_acls=114；`none=0 / other=0`），新增 `TestBatch5_TenantScopeIsolation`（6/6 正例）。**enforce 演练 70/70（FAIL=0 SKIP=0，70.6s）且 `missing_tenant=0`**。写路径核查：seeder 与 `cmd/provision_tenant` 走 owner/管理池不受约束；role/menu/system-config 服务按本租户过滤（中间件注入 ctx）；audit 写入沿用批次 3 收口链路。**非请求面修复**：`capability.ConfigSource` 的 load/Update/Clear 以目标 tenantID 重绑定 ctx（平台管理员管理指定租户覆盖行）；`router/ga_readiness` countOrZero 平台诊断改 system 作用域。`audit_logs` 存量 4 行 tenant NULL/0 对租户会话 fail-closed（system 可读）。剩余：**66 张**租户表（自动化/CMDB/合同/资产/审批链等）按批次推进；**平台级保留项**已确认首例——`permission_definitions`（10 行 tenant_id 全 0/NULL，需专门豁免语义）。

11. ✅ **策略扩展批次 6（自动化/机器人与运维命令域 10 表，2026-10-04 完成）**：`008_automation_tables_policies.sql`（+回滚）覆盖 **bot_runs / bot_steps / bot_events / bot_artifacts / bot_templates / bot_tool_grants / ai_analysis_results / ai_feedbacks / llm_user_preferences / operational_commands**（统一 `tenant_id = get_current_tenant_id()`，只 ENABLE 不 FORCE）。`rls-apply` 受管清单 64→**74 表**、逐表探针全绿（bot_runs=10、bot_steps=27、bot_events=37、bot_templates=9、bot_tool_grants=36、ai_feedbacks=6、operational_commands=1672；`none=0 / other=0`；bot_artifacts/ai_analysis_results/llm_user_preferences 空表 fail-closed），新增 `TestBatch6_TenantScopeIsolation`（7 正例 + 3 空表）。**enforce 演练 70/70（FAIL=0 SKIP=0，133.8s）且 `missing_tenant=0`**——commandbus 持久化 outbox（1672 条命令在策略下照常领取/心跳/执行）实测通过。**本批次修复**：bot `RunStore`（StartRun/AppendStep/AppendEvent）与 `ArtifactStore`（Create/Get/List）以运行/产物租户重绑定 ctx，覆盖入口异步触发场景。**平台级豁免登记 +1**：`ai_llm_calls`（无 tenant_id 的 LLM 网关时延指标）与 `permission_definitions` 同列「平台级保留项」。剩余：**56 张**租户表（CMDB/合同/资产/审批链/事件规则引擎等）按批次推进；工具链 GUC 化后再评估 FORCE。

**DB 级证据（联调库，2026-10-03）**：

- `rls-apply -verify`：`changes`/`vectors` RLS+FORCE 开启、`tenant_isolation` 策略存在；
- 低权探针（`SET LOCAL ROLE itsm_app`）：tenant=990001 可见 **1** 行（播种的自身探针）、无租户 **0** 行、tenant=999999 **0** 行（探针行自动清理）；
- 集成测试 `integration_rls`（真实库）：通过——session 变量注入/发放/回收、无租户拒绝（`ErrNoTenant`）、system bypass 跳过 SET；租户可见性正例因 `changes` 表空按设计 SKIP，由 `rls-apply` 播种探针覆盖。
- 连接侧分流证据（启动与首流量）：`rls: app pool ready user=itsm_app current_user=itsm_app`、`rls: first statement routed to app pool tenant_id=1`；`pg_stat_activity` 可见 `itsm_app` 会话。
- 批次 1 证据（2026-10-04）：`rls-apply -verify` 12 受管表 RLS 状态 + 策略存在全绿；逐表低权探针 `tickets(tenant 4)=31 / ticket_comments=34 / ticket_attachments=15 / user_tenant_memberships=9 / groups=6` 正例>0、`none=0 / other=0`，空表（ticket_ccs / ticket_workflow_records / 组织子表 / workbench_views）fail-closed=0；enforce 全量业务验收 **70/70**。
- 批次 2 证据（2026-10-04）：`rls-apply -verify` 30 受管表 RLS+策略校验 + 逐表低权探针全绿（notifications / notification_deliveries / ticket_notifications 各 591、sla_definitions 12、sla_violations 29、knowledge_articles 45、service_catalog_items 15、invitations 18、incidents 8、ticket_types 12；`none=0 / other=0`；空表 fail-closed=0）；enforce 全量业务验收 **70/70（FAIL=0 SKIP=0，85.3s）**；`TestBatch2_TenantScopeIsolation` 逐表回归（真实库）。
- 批次 3 证据（2026-10-04）：`rls-apply -verify` **44 受管表** RLS+策略全绿；探针 conversations=16 / messages=44 / mcp_servers=1 / mcp_server_tools=6 / tool_invocations=76 正例、`none=0 / other=0`，其余空表 fail-closed=0；`TestBatch3_TenantScopeIsolation` 通过；enforce 业务验收 **70/70** 且 `itsm_rls_missing_tenant_total` **14→0**（三处审计 ctx 修复后复跑）。
- 批次 4 证据（2026-10-04）：`rls-apply -verify` **58 受管表**全绿；探针 process_instances=86 / process_tasks=86 / process_audit_logs=86 / process_approval_decisions=3 / process_execution_histories=1 / workflow_templates=6（`none=0 / other=0`），其余 8 空表 fail-closed=0；`TestBatch4_TenantScopeIsolation` 通过（真实库，6.4s）；enforce 业务验收 **70/70、`missing_tenant=0`**（58.5s）。
- 批次 5 证据（2026-10-04）：`rls-apply -verify` **64 受管表**全绿；探针 role_permissions=1013 / permissions=206 / menus=82 / system_configs=27 / audit_logs=4593 / endpoint_acls=114（`none=0 / other=0`）；`TestBatch5_TenantScopeIsolation` 通过（真实库，1.6s）；enforce 业务验收 **70/70、`missing_tenant=0`**（70.6s）。
- 批次 6 证据（2026-10-04）：`rls-apply -verify` **74 受管表**全绿；探针 bot_runs=10 / bot_steps=27 / bot_events=37 / bot_templates=9 / bot_tool_grants=36 / ai_feedbacks=6 / operational_commands=1672（`none=0 / other=0`），3 空表 fail-closed；`TestBatch6_TenantScopeIsolation` 通过（真实库，6.5s）；enforce 业务验收 **70/70、`missing_tenant=0`**（133.8s）。

## 6. 变更记录

| 日期 | 变更 |
| --- | --- |
| 2026-10-03 | 首轮 shadow 观察：driver 告警增补 query 预览；修复 ACL/审计 4 处 ctx；产出剩余分类与 enforce 清单 |
| 2026-10-03 | run8 复跑（最终构建）：warn 131→**25**、带租户 28→**131**；审计/ACL/RBAC/用户/菜单链路归零；残余 25 = Tx 12 + MCP/连接器 7 + 预认证/会话 6。补充 RBAC 预检 ctx 注入与 `/auth/me`/menus/tenants 收口 |
| 2026-10-03 | run10–15（`saas_msp` 全表面）：driver 告警增补 **caller 归因**；修复 MCP store/组件启动、连接器管理器/重水合、工具队列启动恢复、allocation 展示名、登录落 home 租户视图与预认证 system 作用域；**run15 warns=0 / dbg=192（含启动窗口）** |
| 2026-10-03 | **enforce 前置推进**：`rls-apply` 工具落地（角色/策略应用、密码注入、状态校验与低权探针）；DB 侧完成（roles + pilot 策略 + 探针实证）；集成测试 `integration_rls` 通过；`RLS_MODE=enforce` 演练与 `off` 基线逐请求一致（diff=0，无 fail-closed 错误）。剩余：连接侧双池化（请求=itsm_app / 平台=itsm_admin）与监控接入 |
| 2026-10-03 | **连接侧分流落地（enforce 全链路）**：`rls.Driver` 按作用域选池（租户→`itsm_app` / 系统绕过→管理池）；`InitDatabaseWithRLS` 开通请求池并内建启动探针（`current_user`）；`WithTenantSQL/Tx`、系统编号分配经 `requestDB` 同步分流。现场修复 `Tx/BeginTx` 的 `SET LOCAL` 参数定型缺陷（untyped `nil` → `[]any{}`，否则事务静默失败）。复核：`app pool ready(current_user=itsm_app)` + `first statement routed to app pool(tenant_id=1)`；流量与基线 diff=0；**changes/vectors 两表 DB 级强制生效**。剩余：监控接入 + 策略扩展（R2） |
| 2026-10-03 | **监控接入（enforce 灰度观测面）**：`database/rls/metrics.go` 拉取式收集器把 `Driver.Stats()` 桥接 Prometheus（`itsm_rls_*` 八项指标，`RegisterMetrics` 幂等）；新增 `GET /api/v1/admin/rls/stats`（`system:read`，认证组内，匿名 401）+ 预检映射再生成（`cmd/authz-gen`）。实测 enforce 运行态：missing=0 / app_routed=206 / enforce=212 / bypass=338，`/metrics` 序列一致。告警口径：`itsm_rls_missing_tenant_total>0` → 暂停灰度并评估回滚。剩余：策略扩展（R2） |
| 2026-10-04 | **策略扩展批次 1（R2）落地 + enforce 全量验收 70/70**：`003_business_tables_policies.sql` 十表纳策略（工单核心/组织/成员/工作台）；`rls-apply` 受管清单与逐表低权探针；`rls_integration_test.go` 新增批次 1 逐表隔离回归。enforce 演练首轮 59/70 → 归因修复 4 类阻断后 **70/70**：① **D-11 工单号全局探针被策略收窄**（23505 建单 500）→ 探针/序列播种改 system 作用域与全表 max；② **D-12 配额计数被 RLS 静默清零**（Q2 误判 422、Q5 超限仍建号=配额可绕过）→ `TenantQuotaService` 全方法目标租户重绑定；③ **D-13 审计查询缺 ctx**（A1/A3 500）→ handler 注入租户作用域；④ **D-14 邀请 Inspect/Accept 预认证跨租户缺作用域**（C2b 500）→ system 作用域。 |
| 2026-10-04 | **策略扩展批次 2（生命周期域 18 表）落地 + enforce 演练一次通过 70/70**：`004_lifecycle_tables_policies.sql`（通知/SLA/知识库/服务请求/邀请/事件/工单配置）；`rls-apply` 扩至 30 受管表 + 逐表探针全绿；`TestBatch2_TenantScopeIsolation`。enforce 下 `missing=0`（新实例启动/空闲/验收全程）；历史两实例启动期各 14 条队列恢复类 INSERT 缺租户（非请求面、一次性），已补 enforce 缺租户告警 `caller` 归因并登记复现项。UI 工作台 spec 在重载环境（enforce + 百余工单）多次运行至 U5 后于 U6/弹窗交互处超时（服务端效应均已落库：回复评论 / 批量「成功 2」），慢环境宽容调整随附、U6 稳定性登记遗留。 |
| 2026-10-04 | **策略扩展批次 3（AI/连接器/邮件域 14 表）+ 缺租户审计修复（14→0）**：`005_ai_connector_tables_policies.sql`（AI 会话/消息、AI 工具/MCP、连接器、邮件摄取、域名/供给）；`rls-apply` 受管 30→44 表；`TestBatch3_TenantScopeIsolation`。enforce 首轮 70/70 但 `missing_tenant=14` → 归因（`caller`）为三处审计写入由 `Background` 派生丢失租户作用域（workbench×2 函数 / invitation / provisioning）→ 修复为请求作用域派生（accept 用 `acceptCtx`）→ 复跑 70/70 且 **missing=0**。遗留：outbox 开关保护的异步后台 ctx 路径清单（incident/工单自动化/飞书/工具队列/job_audit）。 |
| 2026-10-04 | **策略扩展批次 4（流程/工作流/审批引擎域 14 表）**：`006_process_workflow_tables_policies.sql`（process_*×8 / workflow_*×5 / bpmn_permissions）；`rls-apply` 受管 44→58 表；`TestBatch4_TenantScopeIsolation`。enforce 演练 **70/70（58.5s）且 `missing=0`**——票据→outbox→引擎写入全链在策略下通过；`router/ga_readiness.go` 跨租户诊断改 system 作用域。剩余 72 张租户表按批次推进。 |
| 2026-10-04 | **策略扩展批次 5（鉴权/菜单/配置/审计域 6 表）**：`007_authz_audit_tables_policies.sql`（role_permissions / permissions / menus / system_configs / audit_logs / endpoint_acls）；`rls-apply` 受管 58→64 表；`TestBatch5_TenantScopeIsolation`。enforce 演练 **70/70（70.6s）且 `missing=0`**；capability 读写按目标租户重绑定、ga_readiness 计数改 system 作用域。`permission_definitions`（tenant_id 全 0/NULL）确认为平台级保留项首例。剩余 66 张租户表按批次推进。 |
| 2026-10-04 | **策略扩展批次 6（自动化/机器人与运维命令域 10 表）**：`008_automation_tables_policies.sql`（bot×6 / AI 结果·反馈·个人默认 / operational_commands）；`rls-apply` 受管 64→74 表；`TestBatch6_TenantScopeIsolation`。enforce 演练 **70/70（133.8s）且 `missing=0`**——commandbus outbox 在策略下照常领取/执行；bot 运行/产物存储按租户重绑定 ctx。`ai_llm_calls`（无 tenant_id）登记平台级豁免。剩余 56 张租户表按批次推进。 |
