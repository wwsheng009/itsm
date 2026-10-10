# 剩余缺口治理方案（权限门禁 Sweep §8.7 承接）

> 生成：2026-10-10 ｜ 来源：`docs/plan/frontend-permission-gate-sweep-2026-10-10.md` §8.7 缺口复核
> 性质：门禁 Sweep（批次 1–15）完成后的**剩余缺口**治理计划；按批次执行、逐批留痕。
> 执行原则：可本地验证的优先（清理/前端）；需产品决策或后端端点的单独批次；每批 tsc/eslint/测试 + CHANGELOG + 提交。

## 0. 缺口总表（承接 §8.7）

| # | 类别 | 缺口 | 影响 | 批次 | 状态 |
|---|---|---|---|---|---|
| G1 | 死代码 | 30 个未接线组件/测试/桶文件（含 templates、batch-operations UI、14 个 business 组件等） | 维护噪音、误引用风险 | **B1** | ✅ 已清理（2026-10-10） |
| G2 | 验证 | 批次 13–15 门禁项缺浏览器双账号实测（§7 清单第 1/2/5 项） | 证据链缺口 | **B7** | ⏳ 待浏览器环境 |
| G3 | 产品-前端 | SLA 新建/编辑页面缺失（路由不存在；详情编辑按钮已禁用） | 无法创建/编辑 SLA 定义 | **B2** | ✅ 已落地（2026-10-10） |
| G4 | 产品-全栈 | `/profile` 自助面：改密应走 `POST /auth/change-password`（需旧密码字段）；资料自更新缺 `PUT /users/me` | 非 `user:write` 用户无法自助 | **B3** | ✅ 已落地（2026-10-10） |
| G5 | 产品-后端 | `POST/DELETE /tickets/:id/relations` 未注册（仅 GET 桩） | RelationPanel 写动作 404 | **B4** | ✅ 已落地（2026-10-10；契约勘误：前端实为 `/tickets/relations` 工单对契约） |
| G6 | 产品-后端 | `/tickets/batch-assign`、`batch/assign*`、`batch-notify` 未注册（TicketList 批量工具栏入口存活，受权限过滤） | 批量分配/通知 404 | **B5** | ⏳ 待决策（补端点 or 下线入口） |
| G7 | 门禁粒度 | `/admin/*`、`/msp/*` 区域守卫粗粒度（`ADMIN_AREA_PERMISSIONS` 4 码） | 跨权限管理员可见不可用入口 | **B6** | ⏳ 待执行（Sweep §4 表） |
| G8 | 能力面 | `rootCauseWorkflowActions`、`priorityMatrix`、`advancedReporting`、`genericTemplateMarketplace` 关闭 | 对应 UI/API 为 roadmap-only（已登记） | — | 保持登记 |
| G9 | 测试基建 | `handlers/sla` 等 sqlite 用例在 Windows 下 `t.TempDir` 清理锁失败（`enttest.Open` 不注册 Close，文件句柄阻塞 RemoveAll） | 本地验证噪音（断言实际全过） | **B8** | ✅ 已修复（2026-10-10） |

## B1：死代码集中清理（P1，✅ 已执行）

**范围（词边界引用判定，仅自引用/桶文件/自测引用）：**

- `components/templates/` 整目录：`index.ts`、`TemplateEditor.tsx`、`FieldDesigner.tsx`、`TemplateCard.tsx`、`TemplateList.tsx`；
- `components/batch-operations/` 整目录：`index.ts`、`BatchOperationBar.tsx`、`BatchOperationModal.tsx`、`BatchProgressModal.tsx`（+ `lib/hooks/useBatchOperations.ts` 及其测试）；
- `components/business/`：`TicketKanbanBoard`、`SmartAssignmentModal`、`SLAAlertSystem`、`TicketCategoryImport`、`TicketCategoryExport`、`TicketCategoryDragSort`、`TicketDependencyManager`、`TicketSubtasks`、`TicketRatingSection`、`TicketNotificationSection`、`TicketMultiLevelApproval`、`TicketTrendPrediction`、`TicketRootCauseAnalysis`、`AutomationRuleForm`、`TicketAttachmentSection`（+ 其测试）；
- `components/business/sla-monitor/SLAViolationMonitor.tsx`（目录其余部分为 sla-monitor 页在用，保留）；
- `components/sla/SLAList.tsx`（`SLADetail` 在用，保留）；
- `components/reports/ReportGenerator.tsx`（`reports/index.ts` 与其余组件保留）；
- 代码残留：`ticket-api.batchUpdateTickets`（死方法，指向未注册端点）+ 其测试块；`TicketKanban.handleStatusChange`/`cursor-move`（无 DnD 实现）；`TicketViewSelector` 保存模态（`setSaveModalVisible(true)` 无调用点）。

**保留（不删）**：`lib/api/batch-operations-api.ts` + 其测试（`DISABLED_API_CONTRACTS` 审计白名单要求存在且被审计命中）；`components/ticket/TicketBatchOperations.tsx`（TicketList 在用）；`TicketDeepAnalytics`（按钮禁用但组件在用）。

**验收**：`tsc -b` 0；eslint 0；`ticket-api.test`、`api-contract.test`、受影响目录 jest 全绿；全仓词边界引用复核 0 残留。

## B2：SLA 新建/编辑页面（P2）

**背景**：后端端点齐备——`POST /sla/definitions`、`PUT/DELETE /sla/definitions/:id`（`sla_routes.go:20/32/33`，码 `sla:write`/`sla:delete`）；缺口为前端页面与路由。

**范围**：
1. 新增 `pages/(main)/sla/definitions/new/index.tsx` 与 `pages/(main)/sla/definitions/$id/edit/index.tsx`（共用表单：名称/描述/服务类型/优先级目标（响应/解决时限）/工作日历等，以 `SLADefinition` DTO 为准）；
2. `routes/index.tsx` 注册两条路由；`routes/route-permissions.ts` 登记 `sla:write`（写入口规则 A/B）；
3. SLA 列表页「新建」按钮、`SLADetail` 编辑按钮解禁（当前 `disabled`）并接路由；
4. 复用 `SLAApi`（create/update/get），提交成功回列表/详情。

**验收**：`npx jest route-permissions` 通过；`sla:write` 用户可建/改、无码用户入口隐藏且直达落 403；表单校验与后端 400 语义一致。

## B3：`/profile` 自助面（P2）

**范围**：
1. 后端新增 `PUT /users/me`（AuthMiddleware + 本人白名单字段 name/email/phone，禁改 role/status 等）与 handler/service；`GET /users/profile` 已有；
2. 前端 `profile` 页切换：保存资料 → `PUT /users/me`；修改密码模态补「旧密码」字段 → `POST /auth/change-password`（`AuthService.changePassword` 已存在）；
3. 门禁回收：`/profile` 三个入口从 `user:write` 禁用改为自助可用（写面本身仍需登录态）。

**验收**：非 `user:write` 用户可改自己资料/密码；管理员路径不受影响；后端路由权限守卫测试更新（新端点入清单）。

## B4：`tickets/relations` 写端点（P2）

**背景**：`TicketAssociationService` 已接入（`ticket_routes.go:81-141`），仅注册了 GET。

**范围（执行中勘误）**：前端实际契约为**工单对**（非 `:id` 路径）——`POST /tickets/relations` `{sourceTicketId,targetTicketId,relationType?,description?}` 与 `DELETE /tickets/relations?sourceTicketId=&targetTicketId=`，均为 `ticket:update`。数据模型为 `tickets.related_tickets` M2M 双向边（无类型化关系实体）→ 仅支持「相关」（relates_to，空值默认），blocks/duplicates 等显式 400；全部按 tenantID 收口 + 幂等。前端 `deleteRelation` 改为工单对签名（hook/RelationPanel 同步，面板行兼容工单对象/关系对象两形态，模态仅保留「相关」选项）。

**验收（已达成）**：`service` 新用例（双向边/幂等/自身拒绝/类型白名单/跨租户 fail-closed/删除双向清除）全绿；`go test ./router` 全绿（写路由挂 `RequirePermission`，守卫测试 0 豁免新增）；前端 tsc/eslint 0；jest ticket-relations-api + useTicketRelations + api-contract 67/67。

## B5：批量分配/通知端点（P3，待产品决策）

**背景**：TicketList 批量工具栏入口存活且按权限过滤（分配 `ticket:assign`、通知 `workflow:write`），但 `/tickets/batch-assign`、`batch/assign*`、`batch-notify` 未注册。

**方案（二选一）**：
- A：后端实现 batch-assign/batch-notify（推荐：复用现有单条分配/通知 service，事务批处理 + 部分失败报告）；
- B：前端下线批量分配/通知入口（保留批量删除/导出/状态/优先级/标签）。

**验收**：选定方案后端到端；`ticket-api.batchUpdateTickets` 残留已随 B1 删除。

## B6：admin/msp 区域守卫细化（P3）

**范围**：按 Sweep §4 表逐页细化（bots/mcp-servers/sla-definitions/cab 等 → `ai:write`/`mcp:admin`/`sla:write`/`change:approve`）；区域守卫从 4 码改为「任一细码可入 + 页内门禁」；`ADMIN_AREA_PERMISSIONS`/`MSP_AREA_PERMISSIONS` 与 Sidebar 可见性口径同步。

**验收**：专职管理员（仅 `approval:write`/`sla:write`）可入对应页面且只见自己可用的写入口；`route-permissions` 测试扩展。

## B7：浏览器双账号实测（P1，需浏览器环境）

**范围**：批次 13–15 新增门禁项（工作流版本、知识审核/协作、通知中心、AI 审批、AIChat、问题关联、审批面板、profile、Header 通知抽屉）逐项双账号（admin + 低权/无码）实测；留痕截图/断言。
**验收**：无码入口不可见/禁用；直达 URL 落 403（路由级）；管理员功能回归。

## B8：测试基建 Windows 兼容（P1，✅ 已执行）

**背景**：`go test ./handlers/sla` 在 Windows 上 24+ 用例全部因 `TempDir RemoveAll cleanup: unlinkat ... file is being used by another process` 失败（断言本身全过）。根因：`enttest.Open` 不注册清理，sqlite 句柄在 `t.TempDir` 删除时仍打开。

**修复**：`handlers/sla/handler_test.go` setup 后显式 `t.Cleanup(func() { _ = client.Close() })`（Cleanup LIFO → Close 先于 TempDir 清理）。同类 setup（其他 handlers/repository 测试包）如复用 `enttest.Open` 且有 TempDir，可按同一模式治理。

**验收**：`go test ./handlers/sla -count=1` 全绿（3.2s）。

## 执行记录

| 日期 | 批次 | 结果 |
|---|---|---|
| 2026-10-10 | B1 | ✅ 30 个死文件删除 + 4 处代码残留清理（`ticket-api.batchUpdateTickets`、`TicketKanban.handleStatusChange/cursor-move`、`TicketViewSelector` 保存模态、2 处陈旧注释）；tsc/eslint 0；jest 136 套件 / 2054 用例全绿（含 `api-contract`）；提交见 CHANGELOG |
| 2026-10-10 | B2 | ✅ SLA 新建/编辑页面落地：`components/sla/SLADefinitionForm.tsx`（共享表单，对齐 `dto.CreateSLADefinitionRequest`）+ `pages/(main)/sla/definitions/new`、`$id/edit` 两页（组件级 `sla:write` 双保险 + 路由级规则 ×2）；`SLADetail` 编辑按钮解禁接路由；`/sla` 工作台加「新建 SLA 定义」门禁入口。验证：tsc/eslint 0；`route-permissions`+`guards` 18/18；`go test ./handlers/sla` 全绿 |
| 2026-10-10 | B8 | ✅ Windows 测试基建修复：`setupSLAHandler` 显式 `t.Cleanup(Close)`；`go test ./handlers/sla` 全绿（3.2s，此前 24+ 用例因文件锁清理失败） |
| 2026-10-10 | B3 | ✅ `/profile` 自助面闭环：后端 `PUT /users/me`（`handlers/common/user_profile.go`，name/email/phone 白名单 + 邮箱唯一性 + 跨租户 fail-closed；路由挂 `AuthMiddleware` 并登记守卫豁免）+ 前端切换 `updateMyProfile` / `AuthService.changePassword`（旧密码字段已存在）；三处 `user:write` 禁用移除，部门改只读。验证：`go test ./handlers/common ./router` 全绿；前端 tsc/eslint 0；jest user-api/api-contract/auth-service 51/51（契约 0 失配） |
| 2026-10-10 | B4 | ✅ 工单关联写面：后端 `service/ticket_relation_write.go`（Add/Remove，租户收口 + 幂等 + 类型白名单）+ `POST/DELETE /tickets/relations`（`ticket:update`）+ 新 service 用例；前端 `deleteRelation` 工单对契约 + hook/RelationPanel 同步（模态仅「相关」）。验证：`go test ./service ./router` 全绿；tsc/eslint 0；jest 67/67 |
| — | B5–B7 | ⏳ 待执行 |
