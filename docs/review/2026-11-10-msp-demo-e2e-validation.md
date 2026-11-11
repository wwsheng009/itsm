# MSP Demo 多轮多用户 E2E 验证与缺陷修复报告（2026-11-10）

**Status: current** — 本报告基于 2026-11-10 的 E2E 验证与 2026-11-11 的多轮 238 全量重部署复核。

> **环境**：`itsm-demo`（`saas_msp`），当前部署版本 `1.8.0-dev.a0d4240a`
> **范围**：多用户、多角色、多租户、多单据关联、权限、前端 UI/UX、文档一致性
> **套件**：`itsm-frontend/tests/e2e/demo/`；配置 `itsm-frontend/playwright.demo.config.ts`
> **证据**：[`evidence/2026-11-10-msp-demo-e2e/run-summary.md`](./evidence/2026-11-10-msp-demo-e2e/run-summary.md)

---

## 0. 结论摘要

- 全量 demo E2E：**44 passed**（1 worker，约 2.6 分钟），**0 个预期失败**。
- 已确认并修复 **18** 个产品/契约/部署回归缺陷：D1–D18（D16 深链返回入口、D17 跨租户工单号全局唯一、D18 工单附件角色访问）。
- **存在 1 个待产品决策的开放缺口**：provider 深度切换到客户作用域后直接 `POST /tickets`，权限含 `ticket:create`，但 handler 强制 requester=当前 provider 用户，服务端按客户租户校验后返回 404「申请人不存在或不可用」；客户建单、provider 处理的现有主路径已通过 E2E。
- 仅剩 demo 库历史残留（`review-test`、31 张历史验收/E2E 工单、未关闭 SLA 全部超期），不属于本轮测试产生。
- 已校正当前文档：demo 版本/工单数量/SLA 现状/review-test 残留，并补充 238 E2E 运行手册；历史评审保持 `historical` 不改写。
- 本轮 `[E2E]` 数据 API 可见残留为 0；软删审计行仍保留在数据库但不可见，未触碰 `[DEMO]` 种子数据。

---

## 1. 执行方式与覆盖

### 1.1 运行

```bash
cd itsm-frontend
PLAYWRIGHT_BASE_URL=http://172.18.3.238:8089 \
PLAYWRIGHT_DEMO_CHANNEL=chrome \
npx playwright test -c playwright.demo.config.ts tests/e2e/demo --workers=1
```

关键设施：

- 不复用本地 `webServer`；同源相对 API 请求，保留 `:8089` 端口。
- httpOnly cookie 登录，不依赖响应体 `accessToken`；token 按角色缓存。
- 登录限流/网络抖动自动退避重试；1 worker 保证多用户串行稳定。
- 新建数据统一 `[E2E-<runId>]` 前缀；`finally` 清理；已审批 SR 先取消再删，平台 super_admin 仅作授权兜底。

### 1.2 轮次

| 轮次 | 内容 | 结果 |
|---|---|---|
| Round 0 | 环境/版本/ready、平台/服务商/客户代表登录、客户/MSP 页面 smoke、登录与 `/auth/me` 角色一致性 | 6 个用例，全部通过 |
| Round 1 | 权限与租户隔离：end_user/客户管理员/MSP manager/agents、跨租户 GET/LIST/PUT/DELETE、未认证 | 8 个用例，全部通过 |
| Round 2 | 工单↔工单、问题↔工单/事件/变更、事件→问题、变更↔CMDB/关联工单、工单↔CI、发布↔变更 | 8 个用例，全部通过 |
| Round 3 | 事件→问题→变更、服务请求 L1/L2/L3、工单协作、MSP 指派/回复/改状态、租户 readiness、租户管理员删除服务请求 | 7 个用例，全部通过 |
| Round 4 | 客户核心页面、工单详情 BPMN/i18n、MSP 工作台、readiness 向导、360/1440/768 响应式、服务目录统计、无 ai:read 的工单/变更 AI 门控、发布↔变更 UI | 10 个用例，全部通过 |

### 1.3 账号

- 平台：`admin / Demo@2026!Admin`
- 租户：`Demo@2026!`
- 服务商：`demo.msp.manager`、`demo.msp.agent1`、`demo.msp.agent2`
- 客户：`demo.{mfg,med,edu}.{admin,agent,lead,user,itadmin,secadmin}`

---

## 2. 已确认并修复的缺陷

### D1 · 变更 affectedCis 使用 ciNumber 时影响分析为 0

**现象**：变更创建/编辑页提示填写 `CI-ECS-001` 这类 CI 编号；填编号后 `/api/v1/changes/:id/cmdb-impact` 返回 `totalAffectedCIs=0`，填数字 ID 则正常。排期冲突检测也只做字符串精确匹配，编号/ID 混用会漏检。

**修复**：

- `handlers/change/service.go` 新增 `resolveAffectedCIIDs`：同时接受 CI 数字 ID 与 `ciNumber`，按租户查询并去重。
- 影响分析使用规范化 ID；排期冲突检测使用 `hasIntIntersection`。
- 单测：`handlers/change/affected_ci_resolution_test.go`。
- E2E：`02-associations` A4（数字 ID）+ A4-D1（ciNumber）均通过。

### D2 · BPMN 审批桥接误完成非审批任务，服务请求 L2/L3 间歇 500

**现象**：服务请求审批链 L1 通过后，BPMN 实例可能停在“请求受理/任务分配”这类非审批用户任务；L2/L3 审批触发桥接时，把该任务当作审批任务完成，操作人不是任务受理人 → 500「同步流程审批任务失败」。

**修复**：

- `service/bpmn_approval_bridge_service.go`：`CompleteBusinessApprovalTask` / `DelegateBusinessApprovalTask` 只匹配 `taskVariables.taskPurpose=approval` 的待办任务；无审批任务时返回 `handled=false`，业务审批按旧逻辑推进。
- 生命周期 `AdvanceBusinessWorkflow` 仍使用任意待办用户任务，不受影响。
- 单测：`TestServiceRequestApproval_BridgesBPMNTask`、`...FailClosedForUnauthorizedActor`、`...SkipsNonApprovalTask`。
- E2E：`03-workflows` B2 覆盖 L2/L3；B2-D2 稳定等待非审批任务后完成审批，验证不再 500。

### D3 · 跨租户 DELETE /tickets/:id 返回 500

**现象**：制造管理员创建工单后，医疗管理员 DELETE 该工单，返回 500 `操作失败`，而非 403/404。

**修复**：`handlers/ticket/handler.go` 对 `ent.IsNotFound` 映射 404「工单不存在」；不泄露资源存在性。
**回归**：单测 `TestDeleteTicket_NotFoundReturns404`；E2E `01-permissions` 通过。

### D4 · 跨租户问题关联返回 500

**现象**：问题关联其他租户的工单/事件/变更时，repository 返回裸 error，handler 兜底 500。

**修复**：`handlers/problem/repository_impl.go` 对租户归属不匹配返回 `ValidationError(400)`；handler 使用 `RespondError`。
**回归**：`TestProblemAssociationsEnforceTenantBoundary` 断言 typed AppError + 400。

### D5 · 事件转问题后事件详情不返回 problemId

**现象**：`POST /incidents/:id/convert-to-problem` 成功且问题侧关联可见，但 `GET /incidents/:id` 无 `problemId`，前端 `IncidentDetail` 的“查看问题”入口永远不出现。

**修复**：

- `Incident` 领域模型 + `dto.IncidentResponse` 增加 `ProblemID`。
- `EntRepository.Get` 预加载 `WithProblems()`；`toDomain` 从反向边取首个问题 ID。
- 单测：`handlers/incident/problem_link_test.go`。
- E2E：`02-associations` A3（关联）+ A3-D2（problemId 回填）均通过。

### D6 · 768 平板宽度 Dashboard body 横向溢出

**现象**：768×900 时 `document.scrollWidth=844`，body 出现横向滚动；侧栏 256px + 内容最小宽度超过视口。

**修复**：`layouts/MainLayout.tsx` 将 768–991 识别为 `isTablet`，默认收起侧栏，并且不再用“恢复展开”覆盖平板默认值。
**回归**：`04-ui-ux` 的 768、360、1440 用例均通过。

### D7 · 服务目录顶部统计与标签页计数矛盾

**现象**：顶部“账号与权限 2 / 安全服务 1”，底部标签页“终端支持 1 / 网络与访问 1 / 账号与权限 1”。固定统计桶把“终端支持”并入账号、把“网络与访问”并入安全。

**修复**：

- `useServiceCatalogData` 改为按真实 `category` 精确聚合，顶部展示前 3 个真实分类。
- `ServiceCatalogStats` 动态渲染分类名/计数。
- 单测：`ServiceCatalogStats.test.tsx`。
- E2E：`04-ui-ux` 服务目录统计通过。

### D8 · end_user Dashboard 请求 dashboard:read 专属接口导致 403/console error

**现象**：`demo.mfg.user` 无 `dashboard:read`，但 `/dashboard` 仍请求 `/api/v1/dashboard/overview` 与 `ticket/stats`，产生 403 与 `Dashboard Overview API调用失败` console error。

**修复**：

- `dashboard/hooks/useDashboardData` 增加 `enabled` 选项；React Query 在无权限时不执行 queryFn。
- `dashboard/index.tsx` 通过 `hasPermission('dashboard','read')` 门控，并显示“当前账号无仪表盘总览权限”的说明；仍保留个人请求/工单/知识库/服务目录入口。
- E2E：`01-permissions` 的 end_user Dashboard 通过；断言无 `/api/v1/dashboard/*` 请求。

### D9 · `/auth/me` 返回的 role 在刷新后掉回数据库主角色

**现象**：`demo.msp.manager` 登录响应 `role=msp_manager`（users.role=end_user + msp_role=provider_admin 的 effective role），但刷新后 `GET /api/v1/auth/me` 返回 `role=end_user`；前端只能通过 `mspRole` 做展示兜底，角色契约不一致。

**修复**：

- `handlers/common` 的 `GetMe` 使用认证中间件解析出的 JWT role 覆盖返回对象，并重算 `isPlatformRole` / `isElevatedMenuRole`。
- 该值与登录、refresh、switch-tenant 签发的 role 保持同源；客户作用域不会被错误提级。
- 单测：`TestGetMe_ReturnsTokenEffectiveRole`。
- E2E：`00-smoke` D9，同时断言 `/auth/me.mspRole=provider_admin`。

### D10 · 租户管理员无 `service_request:delete`

**现象**：租户管理员删除服务请求返回 403，E2E 测试数据只能降级为 `cancelled`，无法清理。

**修复**：

- `internal/authz/roles.go` 与 `middleware/rbac.go` 为 `admin` 增加 `service_request:delete`；`provider_tenant_admin` 自动继承。
- 新增迁移 `20261111_service_request_delete_grants.sql`，为存量租户的 `admin` / `provider_tenant_admin` 幂等补齐授权；回滚脚本同步提供。
- 单测：`TestAdminCanDeleteServiceRequest`。
- E2E：`03-workflows` D10 验证租户管理员 DELETE 200。

### D11 · 无 `ai:read` 的工单详情仍自动请求 AI 端点并产生 403

**现象**：`demo.mfg.user` 无 `ai:read`，但打开 `/tickets/7` 时仍渲染 `TicketAIInlineAssistant` 与 `AISuggestionPanel`，立即请求 `/api/v1/ai/rag/search`、`/api/v1/ai/triage`；后端按 `ai:read` 拒绝，用户还会看到“知识检索暂不可用”这类误导性提示。知识库页面的 AI 搜索框同样对无权限用户可见。

**修复**：

- `components/ticket/TicketDetail.tsx` 增加 `canUseAI = hasPermission('ai:read')`，无权限时不渲染 Inline 助手与分诊面板。
- `pages/(main)/knowledge/index.tsx` 同样按 `ai:read` 隐藏 AI 智能搜索框。
- 安全边界仍由后端 `RequirePermission("ai","read")` 兜底；前端门控只解决无意义请求和错误提示。
- E2E：`04-ui-ux` D11 断言无 `ai:read` 时不出现 `ticket-ai-inline` 且不发起 `/api/v1/ai/*` 请求；核心页面 smoke 已并回 `/tickets/7`。

### D12 · 无 `ai:read` 的变更详情仍展示 AI 影响分析入口

**现象**：`demo.mfg.lead` / `demo.mfg.itadmin` 都有 `change:read` 但没有 `ai:read`；打开变更详情时仍渲染“AI 变更影响分析（plan 产物）”卡片，点击“生成影响分析计划”会调用 `/agent/tools/execute`（要求 `ai:read`）并失败。

**修复**：

- `ChangeDetail` 按 `useCan(['ai:read'])` 门控 `ChangeAIInlinePlanCard`，无权限时不渲染。
- `SLATrendPredictionCard` 也增加 `useCan(['ai:read'])` 防御性门控；当前 demo 角色中具备 `sla:read` 的均有 `ai:read`，因此该路径暂不可达，但保留前端边界。
- E2E：`04-ui-ux` D12 用 `mfgLead` 打开已指派变更，断言不出现 AI 影响分析卡且不发起 `/api/v1/ai/*` 请求。

### D13 · 发布单接受其他租户的变更/负责人/部门/客户 ID

**现象**：`demo.mfg.admin` 创建发布单时传入医疗租户的 `changeId`，238 返回 200 并落库；`ownerId` 等纯 ID 关联字段同样未校验租户归属。详情响应会按 `ownerId` 反查用户姓名，存在跨租户信息泄露风险；变更关联则破坏单据关联一致性。

**修复**：

- `service/release_service.go` 新增 `validateReleaseReferences`：创建/更新前校验 `changeId`、`ownerId`、`departmentId`、`customerId` 均属于当前租户。
- `handlers/release/handler.go` 的创建/更新错误统一走 `common.RespondError`，校验失败返回 400，而非兜底 500。
- 单测：`TestReleaseService_RejectsCrossTenantReferences`（覆盖四类引用 + 同租户正例）。
- E2E：`02-associations` A6 用制造管理员引用医疗租户变更，期望 400，并验证测试发布单已清理。

### D14 · 发布表单无法选择关联变更，详情不展示变更

**现象**：后端 `releases.change_id` 与 `ReleaseResponse.changeId` 已支持发布↔变更关联，但 `ReleaseForm` 只有 `changeId` 字段类型与提交映射，没有对应表单项；`ReleaseDetail` 也不展示 `changeId`。用户从 UI 既不能建立该关联，也看不到已有关系，属于多单据关联链路断裂。

**修复**：

- 新增 `components/release/ChangeSelect.tsx`：按标题搜索当前租户变更（`GET /changes?search=`），编辑态按 ID 补全已选变更；仅提交数字 ID。
- `ReleaseForm` 增加“关联变更”表单项，复用现有 `ChangeSelect`。
- `ReleaseDetail` 增加“关联变更”描述项；可读取变更标题并提供跳转 `/changes/:id`，读取失败时降级显示 `变更 #ID（不可见或已删除）`。
- E2E：`04-ui-ux` D14 覆盖“创建变更 → 发布表单选择变更 → 创建发布 → 详情展示并可回看变更”的 UI 链路。

### D15 · 服务请求软删后 GET 返回 HTTP 200（仅包体 code=404）

**现象**：发布后 D10 E2E 发现租户管理员 DELETE 服务请求成功，但随后 `GET /service-requests/:id` 的 HTTP 状态仍为 200，包体为 `{"code":404,"message":"Not Found"}`。客户端按 HTTP 状态判断会把已删除资源当成成功响应。

**修复**：

- `handlers/service_request/handler.go` 的 GET not-found 分支改用 `common.NotFound(c, "服务请求不存在")`，统一返回 HTTP 404 + 业务码 4004。
- 单测 `TestServiceRequestHandler_Delete` 增补 HTTP 404 断言。
- E2E：`03-workflows` D10 同一链路断言 DELETE 200 且后续 GET 为 403/404。

### D16 · provider 进入客户租户后返回工作台入口丢失

**现象**：`demo.msp.manager`（`provider_admin`）从服务商工作台进入客户租户后，Header 中“当前客户（深度操作中）/ 返回工作台”入口消失，用户无法直接回到 provider 作用域。

**根因**：返回入口原先位于 `CustomerFilter` 内，而 Header 仅在 `canUseMspWorkbench=true` 时渲染 `CustomerFilter`。切到客户租户后当前租户类型变为 `msp_customer`，该条件为 false，整个组件被卸载，返回入口随之消失。更早的 238 部署 `f7dd2974` 来自独立裁剪分支，未包含 `DeepTenantReturn` 独立入口修复。

**修复**：

- 将返回入口拆为独立 `DeepTenantReturn`，由 Header 在客户端稳定渲染，不依赖 `CustomerFilter` 与 `/msp/*` 数据面。
- 可见性统一由 `canReturnToProviderTenant` 判定：`homeTenantId` 存在、当前租户为不同客户租户、用户为服务商员工；缺少权威字段时 fail-closed。
- 返回动作先 `switchTenant(homeTenantId)` 切回 provider 作用域，成功后再导航 `/msp/workbench`。
- 新增 E2E：`04-ui-ux` 多租户跳转，断言客户作用域显示返回入口、点击后回到工作台；后续 Round 5 再增至 43 条。

### D17 · 多租户连续建单时工单号全局唯一冲突导致 500

**现象**：Round 5 全量复跑时，医疗租户建单连续返回 500；后端日志出现 `tickets_ticket_number_key` 唯一约束冲突，候选号 `TKT-202611-000181/2/3` 均被他租户占用，3 次重试后失败。

**根因**：`tickets.ticket_number` 是**全局唯一**约束，但 Redis/DB 序列按租户独立。DB fallback 的 `numberRepo` 路径只做租户内 `existsNumber` 检查后直接返回候选号，没有走 system 作用域的全局占用探测；租户序列落后时会反复返回同一段已占用号。

**修复**：

- 抽出 `nextFreeTicketNumber` 全局探针，Redis 序列和 DB fallback 共用；先读取全局最大月序列，再在 system bypass 作用域逐号确认未占用。
- DB fallback 的 `numberRepo` 路径不再直接返回 tenant-local 候选号。
- 单测：`TestGenerateTicketNumber_SkipsGloballyOccupied` 增加 DB fallback helper 断言。
- 回归：当轮 43/43、后续全量 44/44 通过；发布后仍保留唯一约束重试作为并发竞态兜底（日志可偶见 collision retry，但不再 500）。

### D18 · 工单附件列表对非 staff 角色返回 500

**现象**：`/tickets/74` 对 `demo.mfg.user`、`demo.mfg.itadmin`、`demo.mfg.secadmin` 以及 provider 深度切换到客户租户的账号显示「获取附件列表失败」；`GET /api/v1/tickets/74/attachments` 返回 HTTP 500，而 admin/agent/lead 正常。

**根因**：旧工单附件列表在路由 `ticket:read` 之外又做了一次硬编码角色白名单（仅 super_admin/admin/manager/agent/technician/security）。`end_user` / `it_admin` / `security_admin` 被误判为无权限，handler 又把所有错误统一映射成 500。provider 深度切换后用户行仍归属 provider 租户，按目标租户过滤用户也会误判。

**修复**：

- 附件列表授权改为跟随路由级 `ticket:read`，服务层只确认当前认证用户仍为 active；不再按目标租户过滤 provider 用户行。
- 新增 `ErrTicketAttachmentForbidden` / `ErrTicketAttachmentNotFound`，handler 分别映射 HTTP 403 / 404；其余异常才返回 500。
- 单测覆盖 end_user / it_admin / security_admin / provider 用户；E2E 新增附件上传后多角色列出附件，44/44 通过。

---

## 3. 开放项

**1 个待产品决策的缺口**：

- provider 深度切换到客户作用域后，`POST /api/v1/tickets` 的权限清单含 `ticket:create`，但 handler 强制 requester=当前 provider 用户；服务端按目标客户租户校验后返回 404「申请人不存在或不可用」。当前已验证可用的主路径是客户侧建单、provider 深度切换后读取/评论/改状态/工作台回复/指派。
- 需产品确认修复方向：客户 requester 选择器并允许 `requesterId` 覆盖；允许 provider 员工作为客户租户 requester；或在深度客户态禁用建单并返回明确的 403/文案。未在未授权的情况下自行选择跨租户 requester 语义。

仅剩 demo 库历史状态：`review-test`（tenant 6）、31 张历史验收/E2E 工单、未关闭 SLA 全部超期；本轮未清理，已在 `demo-environment.md` 标注。

---

## 4. 验证结果

### 4.1 E2E

```text
Running 44 tests using 1 worker
44 passed (2.6m)
```

> 0 个预期失败；所有缺陷回归均为普通用例并通过。详见 run-summary。

> 回归已移除旧的 known-defect 容忍分支：跨租户删除/问题关联不再接受 500，事件转换后必须返回
> `problemId`，L1/L2/L3 审批必须 200，D10 删除后 GET 必须 404，Dashboard/AI 不再允许 403 白名单。

### 4.2 前端

- `npx tsc --noEmit`：通过。
- `npx eslint tests/e2e/demo playwright.demo.config.ts`：通过。
- `npm run build`（`tsc -b && vite build`）：通过（仅有既有 chunk size 警告）。
- `ServiceCatalogStats.test.tsx`：1/1 通过。
- `TicketDetailAssignSearch.test.tsx`：4/4 通过（`--coverage=false`；确认 AI 门控改动未破坏工单详情/分配弹窗）。

### 4.2.1 本地生产构建 + 238 后端预发布验证

前端门控/布局修复不依赖后端发布，因此先 `npm run build`，再用 `vite preview`（`ITSM_BACKEND_URL=http://172.18.3.238:8089`）代理真实 238 后端，运行生产构建：

| 用例 | 结果 |
|---|---|
| `04-ui-ux.spec.ts` 全文件（核心页面、D11、D12、D14、D6、D7） | **10 passed** |
| D8 end_user Dashboard 不请求 dashboard 专属接口 | 1 passed |

> 本地 dev 曾出现 antd `useForm` development-only warning；生产构建 preview 中该告警消失，所有 UI 用例通过，说明不是产品缺陷。

### 4.3 后端

已执行并通过（Windows 低内存模式 `GOGC=20 GOMAXPROCS=2`）：

- `go test ./handlers/ticket -run TestDeleteTicket_NotFoundReturns404`
- `go test ./handlers/problem -run TestProblemAssociationsEnforceTenantBoundary`
- `go test ./handlers/incident -run 'TestToDomain_ExposesProblemID|TestFirstProblemID_NilOrEmpty'`
- `go test ./handlers/change -run 'TestResolveAffectedCIIDs_AcceptsCiNumber|TestHasIntIntersection|TestHasCIIntersection'`
- `go test ./handlers/service_request -run 'TestServiceRequestApproval_(BridgesBPMNTask|BridgeFailClosedForUnauthorizedActor|SkipsNonApprovalTask)'`
- `go test ./handlers/service_request -run TestServiceRequestHandler_Delete`
- `go test ./handlers/common -run 'TestGetMe_ReturnsTokenEffectiveRole|TestLogin_JWTRoleRankSelection'`
- `go test ./service -run 'TestReleaseService_'`
- `go test ./internal/authz -run TestAdminCanDeleteServiceRequest`
- 编译检查：`go test ./handlers/ticket ./handlers/problem ./handlers/incident ./handlers/service_request ./service -run '^$'`
- 全量编译：`GOGC=20 GOMAXPROCS=2 go build ./...` 通过。
- Windows 测试库清理：服务请求测试统一 `t.Cleanup(client.Close)`，修复 SQLite 文件句柄导致的 TempDir 假红；
  BPMN 桥接夹具补齐 `taskVariables.taskPurpose=approval`。

> 238 已从当前工作区 HEAD `a0d4240ad` 全量发布 `1.8.0-dev.a0d4240a`；所有回归均为普通用例，按 spec 分组复跑累计 44/44 通过。

### 4.4 238 部署与运行态验证

| 项 | 结果 |
|---|---|
| 部署版本 | `1.8.0-dev.a0d4240a`（Git worktree commit `a0d4240ad`） |
| 部署入口 | `/home/n9e/itsm/repo-e2e-6745c998`（branch `deploy/full-a0d4240a`，从当前工作区 HEAD 全量构建） |
| Init/migration | `itsm-init-demo` exit=0；`20261111_service_request_delete_grants` 已应用 |
| 容器 | backend/worker/frontend/nginx/redis/ai-service 6/6 healthy |
| 健康 | `/healthz`、`/readyz` ready；`/api/v1/version` 返回 a0d4240a |
| D10 授权 | `admin` / `provider_tenant_admin` 的 `service_request:delete` 已在存量租户落库 |
| 附件 smoke | 真实评论附件上传/预览/SHA256/删除通过 |
| 全量 E2E | `44 passed`，0 expected failure |

> 部署前备份：`/home/n9e/itsm/repo-e2e-6745c998/backups/demo-release-1.8.0-dev.a0d4240a-20261111T080506Z`。

> 本轮全量发布包含硬编码角色白名单审计修复（Header AI `isAdmin()` 旁路、AI 通知无效角色码、3 处角色判定漂移）；详见 [`2026-11-11-role-whitelist-audit.md`](./2026-11-11-role-whitelist-audit.md)。

---

## 5. 文档与工具修正

| 文件 | 修正 |
|---|---|
| `docs/multi-tenant/demo-environment.md` | 版本号更新为 `1.8.0-dev.a0d4240a`；工作台 34 `[DEMO]` + 31 残留 = 65；SLA `slaDueSoon=0`；review-test 说明；E2E 差异表更新为已发布验证 |
| `itsm-frontend/tests/e2e/BEST_PRACTICES.md` | 标明 `admin/admin123` 等为历史本地 seeder，不适用于 238；链接 demo README |
| `docs/multi-tenant/scenarios/README.md` / `00-environment-and-accounts.md` | 增加 238 demo 账号矩阵与本地开发账号的区分提示 |
| `itsm-frontend/tests/e2e/demo/README.md` | 新增 238 E2E 运行手册、账号、清理与已修复项清单 |
| `.agents/skills/itsm-msp-operations/scripts/itsm_msp.py` | 修正 workbench-tickets 参数映射：`customerTenantIds/q/limit/cursor/sort/routedOnly/assigneeId/updatedAfter`；238 实测 `--customer-id 3 --search '[DEMO]' --limit 50` 返回 12 条 |
| `itsm-frontend/package.json` | 新增 `test:e2e:demo` |

---

## 6. 证据与复跑

- 测试套件：`itsm-frontend/tests/e2e/demo/`
- 配置：`itsm-frontend/playwright.demo.config.ts`
- 运行手册：`itsm-frontend/tests/e2e/demo/README.md`
- 本轮摘要与遗留：`docs/review/evidence/2026-11-10-msp-demo-e2e/run-summary.md`
- 2026-11-11 当前全量部署证据：`docs/review/evidence/2026-11-11-demo-full-deploy-a0d4240a.md`
- 角色白名单审计：`docs/review/2026-11-11-role-whitelist-audit.md`
- 上一轮全量部署证据（historical）：`docs/review/evidence/2026-11-11-demo-full-deploy-af3cba9c.md`
- 上一轮全量部署证据（historical）：`docs/review/evidence/2026-11-11-demo-full-deploy-5699b5a8.md`
- 上一轮全量部署证据（historical）：`docs/review/evidence/2026-11-11-demo-full-deploy-44faf311.md`
- Playwright HTML：`.dev/playwright-demo-report/`（本地）
- 失败 trace/screenshot：`.dev/playwright-demo-results/`（本地）

**发布后复核已完成**：

1. 238 已发布 `1.8.0-dev.a0d4240a`，6/6 容器 healthy，readyz ready；
2. 原 12 个预期失败回归已改为普通用例，`/tickets/7` 已并回核心页面 smoke；
3. demo 套件按 spec 分组累计 `44 passed`，0 expected failure；
4. 版本/工作台数量/SLA/review-test 事实已回填 `demo-environment.md`；
5. D10 已按“授予租户管理员删除”决策落库并完成 E2E。