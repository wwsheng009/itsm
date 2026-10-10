# 前端权限门禁改造清单（PermissionGate Sweep）

> 日期：2026-10-10 ｜ 基线：L1 路由级门禁（`src/routes/route-permissions.ts`，22 条规则 + 守卫测试）+ L2 入口级 `PermissionGate`。
> 目的：把全部写操作入口收敛到与后端 `RequirePermission` **同一权限码**，消灭「死按钮 / 填完才 403 / 无权限可见入口」三类问题。
> 已改造（入口级）：知识库 3 文件（列表 `ArticleList`、详情 `ArticleDetail` 写操作组、列表页 `knowledge/index`）——新建/编辑页为路由级兜底 + 403 文案；服务目录为页面 `hasPermission` 条件渲染（旧模式，未用 PermissionGate）。**遗留**：知识库版本历史 tab、审核页入口（见 §8）。其余按下表推进。

## 0. 判定口径（改造前必读）

- **权限码单一事实源**：后端路由注册（`itsm-backend/router/*_routes.go`、`handlers/<domain>/*.go`）+ 预检兜底表（`middleware/rbac_precheck_gen.go`、`middleware/precheck_fallback.go`）。禁止前端自造码。
- **判定语义**与后端 `middleware/rbac.go checkPermissionMatch` 一致（`*` / `resource:*` / `resource:admin` 蕴含、MCP 例外）。统一使用 `permissionGranted()` / `<PermissionGate>` / `useCan()`，**不要再手写 `permissions.includes(...)`**。
- 已在映射表但易被忽略：`/tickets/ai-create`（`ai:write`）、`/knowledge/reviews`（`knowledge:write`）。
- **改造方式**：
  - 入口按钮（新建/编辑/导入/安装/审批/删除…）：`<PermissionGate anyOf={['resource:action']}>…</PermissionGate>`（默认隐藏；需保留占位的用 `mode="disable"`）。
  - 空态/引导按钮（`Empty`、`Result` 的 action）：同上包一层。
  - 表格行内操作：逐按钮包裹；若整列均为写操作，建议在 `columns` 里条件过滤整列（无权限时整列隐藏）。
- **路由级已覆盖**：全部 `new/create/edit` 写入口路由（22 条，守卫测试规则 A/B 保障）。因此本清单聚焦**入口级**；L1 缺口单列 §5。

## 1. 总览

| 级别 | 范围 | 规模（约） | 优先级理由 |
|---|---|---|---|
| P0 | 主区（非 admin/MSP）新建/编辑/导入入口 | 40+ 入口 / 24 个页面 | 用户直接可见、点击即失败 |
| P1 | 详情页动作（删除、状态流转、审批、安装/卸载） | 30+ | 破坏性/不可逆动作暴露给只读用户 |
| P2 | admin 区细粒度 | 30+ 页面 | 区域守卫已 fail-closed，可后置 |
| P3 | 全局入口（快捷键 / Dashboard 快捷卡 / AI 会话内跳转） | 4 | 触达面广但数量少 |

状态图例：✅ 已改造（2026-10-10）｜🟡 部分/间接｜⬜ 待改造。

## 2. P0：主区写入口清单

### 2.1 ITIL 核心（事件 / 变更 / 问题 / 发布 / 改进）

> 进度（2026-10-10）：**批次 1** 列表入口/空态/行内编辑删除/详情「编辑」✅；**批次 2** 详情生命周期动作（变更 approve/reject/rollback 独立码、事件动作栏与根因/影响/分类入口、问题状态流转/KEDB）、PIR（增改删）、工单看板新建/删除 ✅；**批次 3** 发布域（详情 approve/reject/rollback/status + 列表新建）、工单详情动作栏（批准/拒绝=`approval:write`、分配/编辑/抄送/删除）、服务请求审批与新建入口、问题调查 Tab 写面 ✅；**批次 4** 工单列表行内编辑/关闭（`ticket:update`）、批量操作按权限逐项过滤（分配/状态/标签/优先级/通知/导出/删除）、改进新建（`ticket:create`）、已知错误 CRUD（`problem:write`/`problem:delete`）、标准变更模板 CRUD（`change:write`/`change:delete`）✅；剩余：§2.2–2.5 主区（CMDB/资产/许可证、服务目录、SLA、工作流、市场、组织配置）、notifications/dashboard、AI 审批卡、全局快捷键等。

| 页面/文件 | 入口（行号） | 动作 | 权限码 | 路由级 | 状态 |
|---|---|---|---|---|---|
| `pages/(main)/incidents/index.tsx` | 88 | 新建事件 → `/incidents/create` | `incident:write` | ✅ | ⬜ |
| `components/incident/IncidentList.tsx` | Modal.confirm | 删除事件 | `incident:delete` | — | ⬜ |
| `components/incident/IncidentDetail.tsx` | 563 | 编辑 → `/incidents/:id/edit` | `incident:write` | ✅ | ⬜ |
| `components/change/ChangeList.tsx` | 252 / 293 / 209 | 新建 / 空态新建 / 编辑 | `change:write` | ✅ | ⬜ |
| `components/change/ChangeList.tsx` | Modal.confirm | 删除变更 | `change:delete` | — | ⬜ |
| `components/problem/ProblemList.tsx` | 245 / 296 / 202 | 新建 / 空态新建 / 编辑 | `problem:write` | ✅ | ⬜ |
| `components/problem/ProblemList.tsx` | Modal.confirm | 删除问题 | `problem:delete` | — | ⬜ |
| `components/problem/ProblemDetail.tsx`、`EnhancedProblemDetail.tsx` | 176 / 600 | 编辑 → `/problems/:id/edit` | `problem:write` | ✅ | ⬜ |
| `pages/(main)/releases/**`（列表/新建页） | — | 新建 → `/releases/new`；编辑 → `/releases/:id/edit` | `release:write` | ✅ | ⬜ |
| `pages/(main)/improvements/index.tsx` | 145 / 172 | 新建改进 → `/tickets/create?type=improvement` | `ticket:create` | ✅ | ⬜ |

> 详情页状态流转（approve/resolve/close 等）见 §3 P1。

### 2.2 CMDB / 资产 / 许可证

| 页面/文件 | 入口（行号） | 动作 | 权限码 | 路由级 | 状态 |
|---|---|---|---|---|---|
| `components/asset/AssetList.tsx`、`pages/(main)/assets/index.tsx` | 281 / 304 / 226 / 80 | 新建（页头 + 空态）/ 编辑 | `asset:write` | ✅ | ✅（批次 7） |
| `components/asset/AssetDetail.tsx` | 243 / 246 / 257 | 编辑 / 分配 / 退役（UI 无删除入口） | `asset:write`（`asset:delete` 后端保留） | ✅/— | ✅（批次 7） |
| `components/license/LicenseList.tsx` | 329 / 346 / 231 | 新建 / 空态新建 / 编辑 | `license:write` | ✅ | ✅（批次 7） |
| `components/license/LicenseDetail.tsx` | 271 / 274 | 编辑 / 分配用户 | `license:write` | ✅ | ✅（批次 7） |
| `components/cmdb/CSDMHub.tsx`、`CIList.tsx` | 125 / 314 / 370 / 255 | 新建 CI / 编辑 CI | `cmdb:write` | ✅ | ✅（批次 6） |
| `components/cmdb/CIList.tsx` | Modal.confirm ×2 | 删除 / 批量删除 CI | `cmdb:delete` | — | ✅（批次 6） |
| `components/cmdb/CIRelationshipManager.tsx` | 144+ | 新建/删除关系（编辑关系无独立入口） | `cmdb:write` / `cmdb:delete` | — | ✅（批次 6） |
| `pages/(main)/cmdb/reconciliation/index.tsx` | 142 | 从云资源建 CI / 绑定已有 | `cmdb:write` | ⚠️ §5 | ✅（批次 6） |
| `pages/(main)/cmdb/cloud-resources/index.tsx` | 261 / 274 / 419 | 建 CI / 绑定已有 / 详情弹窗建 CI | `cmdb:write` | ✅/— | ✅（批次 7） |
| `pages/(main)/cmdb/cloud-accounts/index.tsx` | 296 / 221 | 新建云账号 / 编辑（删除 → `cmdb:delete`） | `cmdb:write` / `cmdb:delete` | — | ✅（批次 7） |
| `pages/(main)/cmdb/cloud-services/index.tsx` | 286 / 242 | 新建云服务 / 编辑（删除 → `cmdb:delete`） | `cmdb:write` / `cmdb:delete` | — | ✅（批次 7） |

> 进度（2026-10-10，批次 6/7）：§2.2 全部收口 ✅——批次 6：CI 列表/CSDM Hub/关系管理/对账中心；批次 7：云账号（新增/编辑/删除）、云服务（新增/编辑/删除）、云资源（新建CI/绑定）、资产（页头+空态新增/行内编辑/详情编辑·分配·退役）、许可证（新增/空态新增/行内编辑/详情编辑·分配）。

### 2.3 服务目录 / 服务请求 / 审批

| 页面/文件 | 入口（行号） | 动作 | 权限码 | 路由级 | 状态 |
|---|---|---|---|---|---|
| `pages/(main)/service-catalog/index.tsx` | 269 | 新建服务（`canManageCatalog`，模态框直提） | `service_catalog:write` | —（无创建路由；编辑路由已覆盖） | ✅（页面级 `hasPermission`） |
| `pages/(main)/service-catalog/components/ServiceItemCard.tsx` | 117 | 编辑 → `/service-catalog/edit/:id`（`canManageCatalog` 控制 `onEdit` 注入） | `service_catalog:write` | ✅ | ✅（批次 8） |
| `components/service-request/ServiceRequestList.tsx` | 186 | 空态新建 → `/service-requests/new` | `service_request:write` | ✅ | ✅（批次 8） |
| `components/service-request/ServiceRequestDetail.tsx` | 189 | 404 空态「新建服务请求」（PermissionGate 既有；无「再次申请」入口） | `service_request:write` | ✅ | ✅（既有） |
| `pages/(main)/my-requests/$requestId/index.tsx` | 296 / 299 / 319 / 402 / 435 | 通过/驳回；启动交付；执行任务 | `service_request:approve` / `service_request:write` | — | ✅（批次 8） |
| `pages/(main)/service-catalog/approvals/**` | 118 / 135 | 通过 / 驳回（无权限时页内告警 + 隐藏按钮） | `service_request:approve` | — | ✅（批次 8） |
| `pages/(main)/approvals/index.tsx` | 400–443 | 领取/批准/拒绝/委托/加签（权限码修正为 `task:update`，对齐 `handlers/bpmn/workflow.go`） | `task:update` | — | ✅（批次 8） |
| `pages/(main)/settings/approvals/index.tsx` | 14 | **重定向页**（→ `/admin/workflows`），非写面 | 无需门禁 | — | ✅ |

> 进度（2026-10-10，批次 8）：§2.3 全部收口 ✅——服务目录「新建服务/编辑」（页面级 `canManageCatalog` 既有）、服务请求空态新建、服务请求详情审批（通过/驳回）+ 交付（启动交付/执行任务）、待我审批-目录（通过/驳回 + 无权限告警）、审批中心 BPMN 任务动作（领取/批准/拒绝/委托/加签，`task:update`）。

### 2.4 SLA / 工作流 / 邮件接入 / 市场

| 页面/文件 | 入口（行号） | 动作 | 权限码 | 路由级 | 状态 |
|---|---|---|---|---|---|
| `components/sla/SLAList.tsx` | 132 / 146 / 174 / 201 / Switch | 编辑/新建/空态新建/启停；删除 | `sla:write` / `sla:delete` | ⚠️ 组件当前未被路由引用（防御性门禁） | ✅（批次 9） |
| `components/sla/SLADetail.tsx` | 92 | 编辑 | `sla:write` | — | ✅（批次 9） |
| `pages/(main)/sla/index.tsx` | 132 / 160 | 导出报告 / 监控（均读面；监控跳 `/sla-monitor`） | `sla:read` | — | ✅（读面无需写门禁） |
| `pages/(main)/workflow/index.tsx` | 954–1013 / 763 / 789 / 1082 | 新建/导入/发起/设计/编辑/部署/启停/克隆（**勘误：`bpmn:write`**）；删除/批量删除 `bpmn:delete` | `bpmn:write` / `bpmn:delete` | ⚠️ designer（§5） | ✅（批次 9） |
| `pages/(main)/workflow/automation/index.tsx` | 179 / 197 / 207 / 323 | 新建规则 / 编辑 / 删除 / 启停（**勘误：`system_config:write`**，端点 `/tickets/automation-rules`） | `system_config:write` | — | ✅（批次 9） |
| `pages/(main)/workflow/instances/index.tsx` | 384 / 247 / 301 | 发起 / 暂停 / 恢复 / 终止实例 | `bpmn:write` | — | ✅（批次 9） |
| `components/workflow/WorkflowTemplateCatalog.tsx` | 171 / 191–194 | 创建草稿 / 编辑 / 发布 / 新版本 / 停用 | `workflow:write` | — | ✅（批次 9） |
| `components/workflow/designer/WorkflowToolbar.tsx`（`WorkflowDesigner` 工具栏） | 170 / 179 / 191 | 保存 / 保存并部署 / 重新部署 | `bpmn:write`（**勘误**） | ⚠️ §5 | ✅（批次 9） |
| `pages/(main)/email-intake/index.tsx` | 363–376 / 398 / 461 / 549 / 598 / 672–716 | 会话动作：纠正（保存并重新核验）/仅重新核验/确认开单/拒绝 `email_intake:review`、重试 `:retry`、强制开单 `:override`；主数据区新增/编辑/停用客户·分部·来源 `customer_master:write`、合同·外部映射 `support_contract:write`、排班·班次 `on_call:write` | 见左 | — | ✅（批次 10） |
| `pages/(main)/email-intake/customers、sources/**` | 88 / 110 / 132（customers）；65 / 87（sources） | 客户/分部/来源组织增删改 | `customer_master:write` | — | ✅（批次 10） |
| `pages/(main)/email-intake/contracts/**` | 139 / 165 / 181 / 193 | 支持合同增删改 + 外部合同映射 | `support_contract:write` | — | ✅（批次 10） |
| `pages/(main)/email-intake/on-call/**` | 217 / 259 / 283 | 排班/班次增删改 | `on_call:write` | — | ✅（批次 10） |
| `pages/(main)/marketplace/$id/index.tsx` | 226 / 227 / 232 | 安装 / 卸载（能力位 + RBAC 双闸） | `marketplace:write` | — | ✅（批次 9） |

> 进度（2026-10-10，批次 9）：§2.4 前半收口 ✅——SLA 定义（列表/详情/启停/删除）、工作流管理页（新建/导入/发起/设计/部署/启停/克隆/删除/批量，`bpmn:write/delete`）、BPMN 实例（发起/暂停/恢复/终止）、工作流模板目录（创建/编辑/发布/新版本/停用）、设计器工具栏（保存/保存并部署/重新部署）、自动化规则（新建/编辑/删除/启停，`system_config:write`）、应用市场（安装/卸载）。
>
> 进度（2026-10-10，批次 10）：§2.4 后半（邮件接入族）收口 ✅——会话详情动作按 `handler.go` 三码分离（review/retry/override），主数据区与 4 个子页（customers/sources/contracts/on-call）按 `customer_master:write` / `support_contract:write` / `on_call:write` 门禁。§2.4 全部完成。剩余：§2.5 组织与配置（主区）与 §3 附件/评论面板、§5 路由缺口。

### 2.5 组织与配置（主区）

| 页面/文件 | 入口（行号） | 动作 | 权限码 | 路由级 | 状态 |
|---|---|---|---|---|---|
| `pages/(main)/tags/index.tsx` | 74 / 144 | 编辑 / 删除标签；新建 | `ticket_tag:update` / `:delete` / `:create` | — | ✅（批次 11） |
| `pages/(main)/applications/index.tsx` | 93 / 147 / 254 | 应用·微服务 编辑/删除；新建 | `application:write` | — | ✅（批次 11） |
| `pages/(main)/projects/index.tsx` | 139 / 212 | 编辑/删除（删除亦 write）；新建 | `project:write` | — | ✅（批次 11） |
| `pages/(main)/enterprise/departments/index.tsx`、`pages/(main)/enterprise/teams/index.tsx`、`pages/(main)/teams/**` | 96 / 113 / 175 / 192 | 部门增删改（create/update/delete 三码）、团队增删改（`team:write`）；`/teams` 为跳转 `/admin/teams` 的空壳页 | `department:create/update/delete`、`team:write` | — | ✅（批次 11） |
| `pages/(main)/tickets/templates/index.tsx` | 277 / 419 / 465 / 592 | 编辑 / 复制 / 删除 / 新建模板（含空态） | `ticket_template:update` / `:create` / `:delete`（**勘误**：端点细分 create/update/delete，非 `:write`） | — | ✅（批次 11） |
| `components/templates/TemplateList.tsx` | 批量菜单 / 创建 | 批量删除（`:delete`）/ 批量启停（`:update`）/ 创建（`:create`） | `ticket_template:create/update/delete` | — | ✅（批次 11，防御性；组件当前未被路由引用） |
| `pages/(main)/tickets/types/index.tsx` | 96 / 117 | 新建 / 编辑 / 复制 / 启停 / 恢复 | `ticket_type:manage`（归档/删除 `:archive`、安装预设 `:install_preset`） | — | ✅（批次 11；manage 全量收口） |
| `pages/(main)/standard-changes/index.tsx` | 332 / 348 / 415 | 删除 / 新建 / 实例化 | `change:write / delete` | — | ✅（批次 4） |
| `pages/(main)/problems/known-errors/index.tsx` | 463 / 580 | 删除 / 新增 | `problem:write / delete` | — | ✅（批次 4） |
| `pages/(main)/notifications/index.tsx` | 510 / 666 / 790 | 删除 / 批量删除 / 保存偏好 | `notification:write`（发送 → `:create`） | — | ✅（批次 5） |
| `pages/(main)/dashboard/index.tsx` | 299 | 快捷「创建工单」 | `ticket:create` | ✅ | ✅（批次 5） |
| `pages/(main)/ai/approval/index.tsx` | 337 / 341 | 批准 / 拒绝工具调用 | `ai:write` | — | ✅（批次 5） |
| `components/ai/AIChat.tsx` | 489 / 1335 | 消息级「补充为知识文章」+ 标题栏「保存成文章」→ `/knowledge/articles/new` | `knowledge:write` | ✅ | ✅（批次 11） |
| `components/problem/ProblemInvestigationTab.tsx` | 709 | 「沉淀到知识库」（Modal 提交） | **勘误**：端点 `POST /problem-knowledge-articles` 实为 `problem:write`（非 `knowledge:write`）；既有 `PermissionGate anyOf={['problem:write']}` 已正确 | ✅ | ✅（批次 11，无需改造） |
| `components/ticket-relations/RelationPanel.tsx` | 132 / 164 / 243 | 新建/删除工单关联 | `ticket:update`（已核） | — | ✅（批次 11）；⚠️ 端点 `POST/DELETE /api/v1/tickets/relations*` 后端未注册（capability `advancedTicketRelations=false`），入口保留但受权限过滤（与批次 4 先例一致） |

> 进度（2026-10-10，批次 11）：§2.5 组织与配置（主区）收口 ✅——标签（create/update/delete 三码）、应用/微服务、项目（删除亦 `project:write`）、部门（三码）/团队、工单模板（create/update/delete 细分 + 死组件 TemplateList 防御性门禁）、工单类型（`ticket_type:manage` 全量）、知识入口（AIChat 双入口 `knowledge:write`；ProblemInvestigationTab 勘误为 `problem:write` 已正确）、工单关系面板（`ticket:update`，端点缺失登记）。

### 2.6 全局入口（P3）

| 文件 | 入口 | 动作 | 权限码 | 状态 |
|---|---|---|---|---|
| `components/common/GlobalShortcutProvider.tsx` | 72 | ctrl+n → `/tickets/new`（旧别名，§5） | `ticket:create` | ✅ |
| `pages/(main)/notifications/index.tsx` | 491/504/655/660/722/789 | 标记已读/删除/全部已读/清空/偏好保存/重置默认 | `notification:write` | ✅ |
| `pages/(main)/ai/approval/index.tsx` | 331 | 通过/驳回工具调用审批（POST /agent/tools/:id/approve） | `ai:write` | ✅ |
| `components/ai/tool-approval-card.tsx` | 140 | 对话内确认（内联「确认/拒绝」；当前注入未启用，防御性） | `ai:write` | ✅ |
| `pages/(main)/dashboard/index.tsx` | 299 | 空态「去创建」→ `/tickets/create` | `ticket:create` | ✅ |

> 进度（2026-10-10，批次 5）：全局快捷键 ctrl+n（`ticket:create`）、通知写面（`notification:write`）、AI 审批「通过/驳回」与对话内确认卡（`ai:write`）、dashboard 空态创建入口（`ticket:create`）✅。

## 3. P1：详情页动作（破坏性 / 状态流转 / 审批）

> 特征：不在路由级覆盖范围（无 `new/create/edit` 路径段），入口级是唯一防线；多为不可逆动作。

> 进度（2026-10-10）：批次 2 落地事件动作栏/变更生命周期/问题 KEDB/状态流转/PIR/工单看板；**批次 3** 落地发布域动作（approve/reject=`release:approve`；rollback=`release:rollback`；status=`release:write`）、工单详情动作栏（批准/拒绝=`approval:write`、分配=`ticket:assign`、编辑=`ticket:update`、抄送=`workflow:write`、删除=`ticket:delete`；AI 建议采纳随 `ticket:update`）、服务请求审批（状态 + `service_request:approve` 双条件）与新建入口、问题调查 Tab（`problem:write`）✅；**批次 4** 落地工单列表行内编辑/关闭（`ticket:update`）、批量操作按实际后端码逐项过滤（删除=`ticket:delete`、导出=`ticket:export`、分配=`ticket:assign`、状态/优先级/标签=`ticket:update`、通知=`workflow:write`；勘误：`ticket:batch_*` 词表实际不存在）、已知错误（`problem:write`/`problem:delete`）、标准变更模板（`change:write`/`change:delete`）、改进新建（`ticket:create`）✅；剩余：服务请求状态流转、发布余项、批量操作端点缺失（`batch-assign`/`batch-notify` 后端未实现，前端入口保留但受权限过滤；**2026-10-10 B5 核实：活跃 UI 无 batch 端点调用，该项关闭**）等。

> 进度（2026-10-10，批次 12）：§3 附件/评论面板按域内码收口 ✅——`AttachmentPanel` 新增 `canUpload`/`canDelete`、`CommentPanel` 新增 `canCreate`/`canEdit`/`canDelete`（缺省 true 保持兼容）；工单详情按 `ticket:create/update/delete`、事件详情按 `incident:write` 传入。§3 全部完成。

| 域 | 动作 | 权限码 | 代表文件 |
|---|---|---|---|
| 事件 | escalate / acknowledge / resolve / close / reopen / assign / SLA 暂停恢复 / convert-to-problem | `incident:write` | `components/incident/IncidentDetail.tsx` |
| 事件 | 删除 | `incident:delete` | `pages/(main)/incidents/**` |
| 变更 | approve / reject | `change:approve` | `components/change/**`（详情） |
| 变更 | schedule / start / complete / close / cancel | `change:write` | 同上 |
| 变更 | rollback | `change:rollback` | 同上 |
| 变更 | 删除 | `change:delete` | 同上 |
| 问题 | investigate / root-cause / solution / close / associations | `problem:write` | `components/problem/**` |
| 问题 | 删除 | `problem:delete` | 同上 |
| 发布 | approve / reject | `release:approve` | `pages/(main)/releases/**` |
| 发布 | rollback | `release:rollback` | 同上 |
| 发布 | status 流转 / 删除 | `release:write` / `release:delete` | 同上 |
| 工单 | accept / reject / approve / resolve / close / reopen / forward / cc / withdraw | `workflow:write` | `components/business/TicketWorkflowActions.tsx`、`TicketDetail.tsx`、`TicketMultiLevelApproval.tsx` |
| 工单 | 批量操作（分配/状态/标签/优先级/通知/导出/删除） | `ticket:assign` / `ticket:update` / `workflow:write` / `ticket:export` / `ticket:delete`（✅ 批次 4；勘误：`ticket:batch_*` 词表实际不存在） | `components/ticket/TicketBatchOperations.tsx` |
| 工单 | 编辑模式入口 `/tickets/:id?mode=edit` | `ticket:update` | `components/ticket/TicketList.tsx:115` |
| 服务请求 | approval（通过/拒绝，状态+权限双条件）/ provision（启动交付/执行任务）| `service_request:approve` / `service_request:write` | `components/service-request/ServiceRequestDetail.tsx`（`useCan` 已落地）、`pages/(main)/my-requests/$requestId/index.tsx`（批次 8） |
| 知识库 | 发布/归档/编辑/删除 | `knowledge:write` / `knowledge:delete` | ✅ 已改造 |
| 通用 | 附件上传/删除、评论发表/编辑/删除 | 域内码：ticket——上传 `ticket:create`、附件删除 `ticket:delete`、评论创建 `ticket:create`/编辑 `ticket:update`/删除 `ticket:delete`；incident——评论创建/编辑/删除 `incident:write` | `components/business/detail-tabs/AttachmentPanel.tsx`（新增 `canUpload`/`canDelete`）、`CommentPanel.tsx`（新增 `canCreate`/`canEdit`/`canDelete`，缺省 true 保持兼容）；调用点 `components/ticket/TicketDetail.tsx:1534/1555`、`pages/(main)/incidents/$id/index.tsx:245` | ✅（批次 12） |

## 4. P2：admin 区（区域守卫已覆盖，按需细化）

- 现状：`routes/index.tsx` 中 `/admin/*`、租户管理、`/msp/*` 已由 `RequireCapability`（`ADMIN_AREA_PERMISSIONS`）整区拦截，**fail-closed 已达成**；页面内按钮未按更细权限码收敛。
- 区域守卫码（`routes/index.tsx:29-32`）：`ADMIN_AREA_PERMISSIONS=['user:write','role:write','system_config:write','ticket_type:manage']`；`TENANT_ADMIN_PERMISSIONS=['system:write','tenant:read','tenant:write']`；`MSP_AREA_PERMISSIONS=['msp:read','msp_ticket:read','msp_customer:read','msp_allocation:read','msp_report:read']`。**粗粒度偏差**：仅有 `approval:write`/`sla:write` 的专职管理员会被 admin 区整体拦下（与后端细粒度不一致），登记为下轮优化（**2026-10-10 B6 已落地**：并集 20 码 + 页级细化，见文末进度）。
- 建议：本轮不逐页改造；下轮按下表细化（页面 → 建议码）：

| 页面 | 建议权限码 |
|---|---|
| `admin/users`、`admin/groups`、`admin/departments`、`admin/teams` | `user:write` / `group:write`（待核）/ `department:write`（待核）/ `team:write` |
| `admin/roles`、`admin/permissions`、`admin/menus` | `role:write` / `permission:write` / `menu:write` |
| `admin/tenants` | `tenant:write`（平台角色限定） |
| `admin/workflows`、`admin/approval-chains`、`admin/approvals` | `workflow:write` / `approval:write` |
| `admin/sla-definitions`、`admin/sla-templates`、`admin/escalation-rules`、`admin/escalation-matrices` | `sla:write`（escalation_matrix handler + seeder 菜单码；**无 `escalation:*` 词表**） |
| `admin/cmdb-types`、`admin/config-inheritance` | `cmdb:write` / `system_config:write` |
| `admin/ticket-categories`、`admin/tickets/*-rules` | `ticket_type:manage`（待核）/ `ticket:admin` |
| `admin/system-config`、`admin/connectors`、`admin/vector-store` | `system_config:write` / `connector:write` / `system_config:write` |
| `admin/bots`、`admin/mcp-servers`、`admin/tools` | `ai:write` / `mcp:admin` / `mcp:admin` |
| `admin/cab`、`admin/process-routing`、`admin/escalation-matrices` | `change:approve` / `workflow:write` / `escalation:write` |

> 标注「待核」的码在改造前需用 `rg 'RequirePermission\("' itsm-backend/router itsm-backend/handlers` 对照端点确认。

> 进度（2026-10-10，剩余缺口治理 B6）：admin 区守卫从 4 码改为并集 20 码（单源 `routes/area-permissions.ts`，码表实测 backend `RequirePermission` 词表）+ 24 个页面级 `RequireCapability` 细化 + `Sidebar.isAdmin` 同源；专职管理员（仅 `sla:write`/`approval:write` 等）可入对应页面，不匹配页面落 403。验证：tsc/eslint 0；jest routes 22/22 + sidebar 1/1。

## 5. 路由级（L1）补漏

| # | 路径 | 问题 | 建议 |
|---|---|---|---|
| 1 | `/workflow/designer` | 写面（设计器保存/发布）无路由级规则 | 补规则 **`bpmn:write`**（按保存端点核对为 bpmn 词表）✅ 已落地（批次 12） |
| 2 | `/cmdb/reconciliation` | 写面（对账落库/建 CI）无路由级规则 | 补规则 `cmdb:write` ✅ 已落地（批次 12） |
| 3 | `/tickets/:id?mode=edit` | 编辑态经 query 进入，`matchPath` 不覆盖 | 已核：`TicketDetail` 无 `mode=edit` 消费逻辑；编辑入口在 `TicketList.tsx:115` 已按 `ticket:update` 门禁（批次 4）✅ 无需处理 |
| 4 | `/sla/definitions/new`、`/sla/definitions/:id/edit` | **路由不存在**（`SLAList.tsx:136/172/194` 指向，点击 404；`SLADetail` 编辑按钮同样指向） | 清理死链：`SLADetail` 编辑按钮改禁用；`SLAList` 为死组件（未被路由引用，未接入）；**SLA 新建/编辑页面缺失登记为产品缺口** ✅ 清理完成（批次 12）；**页面已补**（剩余缺口治理 B2，2026-10-10：新建/编辑页 + 路由规则 + 入口） |
| 5 | `GlobalShortcutProvider` → `/tickets/new` | **旧别名非路由**（ctrl+n 会 404） | 改指 `/tickets/create`（已有路由级门禁），并包 `ticket:create` ✅ 已落地（批次 12） |
| 6 | `reports/*` 导出 | 读端点 `dashboard:read`；**生成/导出** `POST /dashboard/reports/:type`、`POST /dashboard/export` = `dashboard:admin`（`dashboard_routes.go:243/259`）；前端 `report:export` 常量未对后端生效 | 已核：上述两个端点**无前端调用方**；各导出均为本地 CSV 生成（无后端端点）；`report:export` 常量未被使用 ✅ 无需处理（批次 12） |
| 7 | `/service-catalog/request/:id` | 提交申请写面（路径无 `new/create/edit` 段，未覆盖） | 补规则 `service_request:write` + 三处申请入口同步门禁（`service-catalog/index.tsx`、`components/service-catalog/ServiceItemCard.tsx`、`detail/$id/index.tsx`；勘误：ServiceItemCard 实际路径含 `service-catalog/components/` 前缀）✅ 已落地（批次 12） |
| 8 | `/enterprise/departments`、`/enterprise/teams` | 无区域守卫且路径无写入口段 | 入口门禁 `department:create/update/delete`、`team:write` ✅ 已落地（批次 11） |
| 9 | 只读确认（经扫描无写入口，登记备查） | `/noc`、`/sla-monitor`、`/cmdb/registry、topology、relationships`、`/audit-logs`、`/system/users`（只读展示组件 `UserList`）、`/system/organization`（`DepartmentTree`） | 无需处理 |
| 10 | 遗留重定向（`templates`→`/tickets/templates` 等） | 目标路由已有门禁 | 无需处理 |

> 进度（2026-10-10，批次 12）：§5 收口——缺口 1/2/5/7 已修复（新增路由规则 ×3 + ctrl+n 改指 + 三处申请入口门禁），3/6 经核无需处理，4 已清理死链（页面待补），8 已于批次 11 落地。

## 6. 旧守卫收编（一致性，防止双源漂移）

- 存量：`components/auth/PermissionGuard.tsx`（`PermissionGuard`/`RoleGuard`/`AdminGuard`）、`components/auth/AuthGuard.tsx`、`lib/hooks/use-permissions.ts` 与新 `PermissionGate`/`useCan` 并存。
- 建议：
  1. `PermissionGuard` 内部改用 `permissionGranted()`（与后端同语义），消除「前端 includes vs 后端蕴含」差异；
  2. 新代码统一 `PermissionGate`/`useCan`；存量手写判定（`tickets/types` 的 `canInstallPreset`、`service-catalog` 的 `canManageCatalog`、`AIChat` 的 `canManageProviderPreference`）行为保留、实现换 `useCan`（可选）；
  3. `usePermissions().hasPermission(resource, action)` 保持兼容（内部已是 `${resource}:${action}` 查询）。

## 7. 每域验收模板（改造时逐项勾选）

- [ ] 低权限账号（如 technician/end_user）：入口不可见（或禁用）；直达 URL 落 `/403`（如属 L1 覆盖）
- [ ] 管理员账号：入口可见、功能回归通过
- [ ] 空态/引导入口（`Empty`/`Result` action）一并处理
- [ ] 若新增路由级规则：`npx jest route-permissions` 通过（规则 A/B）
- [ ] 浏览器双账号实测（隔离会话）+ 截图/断言留痕
- [ ] CHANGELOG 登记

## 附录 A：本清单生成方式（可复跑）

```powershell
# 写入口导航
rg -n "navigate\(['\`\"][^'\`\"]*(new|create|edit)" itsm-frontend/src -g "*.tsx"
# 破坏性动作（Modal.confirm 密度）
rg -c "Modal\.confirm" itsm-frontend/src -g "*.tsx" | Sort-Object
# 路由写入口守卫测试
npx jest --coverage=false route-permissions   # 在 itsm-frontend 下执行
# 后端权限码核对
rg -n 'RequirePermission\("' itsm-backend/router itsm-backend/handlers
```

## 附录 B：维护规则（防漂移）

1. 新增**写入口路由**（路径含 `new/create/edit`）→ 必须登记 `src/routes/route-permissions.ts`，否则守卫测试规则 A 直接红；
2. 新增**写操作按钮** → 必须包 `PermissionGate`（或 `useCan` 条件渲染），权限码取自后端注册；
3. 权限码核对以**后端**为准；前端出现新码时同步 `internal/authz/catalog.go` 词表检查；
4. 每轮 sweep 完成后，在本清单对应行标 ✅ 并在 CHANGELOG 登记。

## 8. 完整性审查记录（2026-10-10）

审查方式：①自查——写入口导航 / `Modal.confirm` / 写动词三路扫描（60+ 文件）与后端 `RequirePermission` 逐条核对；②独立只读子代理复核（与本文档并行执行，结论见 §8.5）。

### 8.1 已修正（阻断级：原文档表述有误，照做会出错）

| # | 原表述 | 修正 | 依据 |
|---|---|---|---|
| 1 | `department:write` | `department:create / update / delete`（部门树/组织管理）；`department:write` 仅出现在 `handlers/bpmn/process_trigger.go`（部门流程初始化） | `router/common_system_routes.go:95-99` |
| 2 | `escalation:write（待核）` | **无 `escalation:*` 词表**；升级规则/矩阵 = `sla:write` | `handlers/escalation_matrix/handler.go:85-87`、`pkg/seeder/seeder.go:1826`（菜单码） |
| 3 | `settings/approvals` 审批链配置 `approval:write` | 实为**重定向页**（`useEffect` → `/admin/workflows`），非写面、无需门禁 | `pages/(main)/settings/approvals/index.tsx:12-15` |
| 4 | `ticket_type:manage`（仅安装预设） | 归档/删除用 `ticket_type:archive`；启停/复制/恢复用 `ticket_type:manage` | `router/ticket_type_routes.go:14-23` |

### 8.2 补充遗漏（§2 表格之外新增，执行时合并处理）

| # | 文件:行 | 入口 | 动作 | 权限码 |
|---|---|---|---|---|
| 1 | `components/ticket/TicketKanban.tsx:366 / 171` | 新建工单 / 删除工单 | 看板写入口 + 破坏性动作 | `ticket:create` / `ticket:delete` |
| 2 | `pages/(main)/installations/index.tsx:161 / 346 / 244` | 卸载 / 安装应用 | 市场安装管理面 | `marketplace:write` |
| 3 | `pages/(main)/changes/pirs/index.tsx:65-179`、`pages/(main)/changes/$id/pir/index.tsx:114-343` | PIR 创建 / 更新 / 删除 | 实施后审查写面 | `change:write` / `change:delete` |
| 4 | `pages/(main)/workflow/instances/index.tsx:255-324` | 暂停 / 恢复 / 终止实例 | 流程实例状态操作 | `bpmn:write` |
| 5 | `pages/(main)/workflow/versions/index.tsx:148-210 / 277` | 回滚 / 激活 / 创建版本 | 流程版本写面 | `bpmn:write`（`handlers/bpmn/workflow.go:111-113`）；**删除版本**后端未见 DELETE 端点（疑死功能，待核）；导出为 `bpmn:read` |
| 6 | `components/ai/tool-approval-card.tsx`、`components/ai/AIChat.tsx:452` | 工具调用批准 / 拒绝（会话内） | AI 治理动作（与 `/ai/approval` 页同码） | `ai:write` |
| 7 | `components/business/TicketDependencyManager.tsx:140-192 / 510`、`components/ticket-relations/RelationPanel.tsx:93` | 依赖/关联关系增删 | **后端无写端点**（仅 GET 桩：`router/ticket_routes.go:83-149,288`；前端 `ticket-relations-api.ts` 的 POST/PUT/DELETE 会 404）→ 死功能，登记清理；未来补端点按 `ticket:write` |
| 8 | `components/business/TicketSubtasks.tsx:110-148 / 420` | 子任务创建 / 更新 / 删除 | 工单子任务写面 | `ticket:create` / `ticket:update` / `ticket:delete`（`router/ticket_routes.go:76-79`） |
| 9 | `components/business/TicketViewSelector.tsx:97-195` | 视图保存 / 删除 | 工单视图写面 | `ticket:write`（`router/ticket_routes.go:22-26`） |
| 10 | `components/business/NotificationCenter.tsx:428 / 707` | 通知操作（已读 / 删除） | 通知中心内联动作 | `notification:write` |
| 11 | `pages/(main)/service-catalog/index.tsx:200`、`components/ServiceItemCard.tsx:183`、`detail/$id/index.tsx:71` | 申请服务 → `/service-catalog/request/:id` | 申请入口（路由级未覆盖，见 §5 #7） | `service_request:write` |
| 12 | `pages/(main)/enterprise/departments/index.tsx:94-95 / 169-180` | 部门新建 / 编辑 / 删除 | 组织管理（无区域守卫） | `department:create/update/delete` |
| 13 | `components/knowledge/ArticleVersionControl.tsx:99-217` | 「恢复到此版本」（详情页「版本历史」tab，`ArticleDetail.tsx:343` 无条件渲染） | **知识库「已改造」的遗留反例** | `knowledge:write` |
| 14 | `pages/(main)/installations/index.tsx:406 / 411` | 保存配置 / 飞书授权 | 安装配置写面 / OAuth 链接 | `marketplace:write` / `connector:write` |
| 15 | `pages/(main)/my-requests/$requestId/index.tsx:308-313` | 「执行交付」 | 履约动作 | `service_request:write`（`service_request_routes.go:53`） |
| 16 | `pages/(main)/knowledge/reviews/index.tsx:331 / 394-426` | 批准发布 / 拒绝发布 | 文章审核（仅路由级兜底，入口未 gate） | `knowledge:write` |
| 17 | `components/knowledge/KnowledgeCollaboration.tsx:136-157 / 306` | 添加评论 / 保存文档 | 协作写面 | `knowledge:write`（保存端点待核） |
| 18 | `components/ai/AIChat.tsx:1166-1169` | 删除此会话 | AI 会话写面 | `ai:write` |

> 进度（2026-10-10，批次 13）：§8.2 十八项全部闭环 ✅——#1 Kanban（批次 2）、#2/#14 installations（批次 13：**勘误** `marketplace:manage`→`marketplace:write`，飞书 OAuth 加 `connector:write`）、#3 PIR（批次 2）、#4 实例（批次 9）、#5 版本（批次 13：激活/回滚 `bpmn:write`、删除 `bpmn:delete`；端点实存于 `handlers/bpmn/workflow.go:111-114`）、#6 AI 审批（批次 5）、#7 RelationPanel（批次 11）、#8 子任务（批次 13：`canCreate/canEdit/canDelete` 防御性，组件当前未被引用）、#9 视图（批次 13：管理下拉 `ticket:write`；保存模态为未接线死 UI）、#10 通知中心（批次 13：已读/删除 `notification:write`）、#11 服务申请（批次 12）、#12 departments（批次 11）、#13 版本恢复（批次 13：`knowledge:write`）、#15 my-requests（批次 8）、#16 审核（批次 13：行内批准/拒绝禁用 + 详情页脚条件渲染 `knowledge:write`）、#17 协作（批次 13：评论/保存 `knowledge:write`）、#18 删除会话（批次 13：`ai:write`）。

### 8.3 断言核验（抽查）

- 「✅ 已改造」知识库：列表（`ArticleList`）/ 详情写操作组 / 新建页 / 空态 属实；**唯一反例 = §8.2 #13 版本历史**。
- 「路由级 ✅」抽查：`/tickets/ai-create`（`ai:write`）、`/knowledge/reviews`（`knowledge:write`）、`/service-catalog/edit/:id`、`/service-requests/new`、`/improvements/new` 均确在 `route-permissions.ts`（22 条规则）。
- 区域守卫：`/admin/*`、`/admin/tenants`、`/msp/*` 三处 `RequireCapability` 属实（`routes/index.tsx:247-350`）；`/system/*`、`/enterprise/*` 无区域守卫（§5 #8）。
- 只读确认（扫描无写入口）：`/noc`、`/sla-monitor`、`/cmdb/registry|topology|relationships`、`/audit-logs`、`/system/users`（`UserList` 只读展示）、`/system/organization`（`DepartmentTree`）。

### 8.4 审查结论

**needs-revision → 已修订（§8.1 四处修正 + §8.2 十八项补充，含独立复核新采纳 5 项、驳回误报 2 项、§8.6 组件候选 8 组）→ ready-to-implement。**
执行提示：开工某域时先按 §8.2 合并该域遗漏项，再按 §7 验收模板双账号验证；「待核」码在对应改造轮次即时按端点核对，不阻塞开工。

### 8.5 独立子代理复核结果（2026-10-10，只读子代理）

复核方法：独立扫描 `navigate(new|create|edit)` / `Modal.confirm` / 写动词 / API 变更调用，逐条与 `route-permissions.ts`、后端 `RequirePermission` 比对。全程只读。

**交叉确认（已并入 §8.2）**：installations 卸载/保存配置/飞书授权、变更 PIR、流程版本/实例、服务目录申请入口、my-requests 执行交付、知识库版本恢复与审核入口、AI 会话删除、子任务/视图/通知中心。

**新采纳**：§8.2 #14–#18（保存配置/飞书授权、执行交付、审核入口、KnowledgeCollaboration、删除会话）；§8.6 组件级候选。

**反例核验（对本文档断言的修正，已就地更新）**：
1. 「知识库 4 文件已改造」不准确 → 实为 **3 个文件**入口级 gate（`ArticleList`/`ArticleDetail`/`knowledge/index`）；`articles/new`、`articles/$id/edit` 无 gate（路由级兜底 + 403 文案）——§0 已修正；
2. 「服务目录 `ServiceCatalogFilters` 已改造」→ 判定实际在页面 `hasPermission('service_catalog','write')`（旧模式），组件仅按 prop 渲染——§0/§2.3 已修正；
3. 「service-catalog 新建服务 路由级 ✅」→ 实为**模态框直提**（无创建路由）——§2.3 已修正；
4. 「reports 预检码 dashboard:read」→ 读端点 read、**生成/导出 admin**——§5 #6 已修正。

**驳回（子代理误报，附反证）**：
- 「`email_intake:*` 权限码不存在」→ **不成立**：`handlers/email_intake/handler.go:29-38` 注册 `RequirePermission("email_intake","read|review|retry|override")`，`middleware/rbac_precheck_gen.go:678-690` 预检表同码；文档原码正确；
- 「enterprise/departments 无写入口」→ **不成立**：`index.tsx:94-95/169-180` 有编辑/删除/新建 + 表单 Modal；§8.2 #12 保留。

**遗留待核（执行对应域时按端点即时核对）**：workflow/versions 删除端点、KnowledgeCollaboration 保存端点、notifications「清空全部」、admin 区细粒度码（assignment-rules/automation-rules/categories 等）、`ticket:update`（关系）。

### 8.6 组件级写入口候选（改造对应页面时一并门禁）

| 组件 | 动作 | 建议码 |
|---|---|---|
| `components/business/TicketKanbanBoard.tsx` | 拖拽状态流转 | `ticket:update` |
| `components/business/tickets/TicketsToolbar.tsx`、`batch-operations/*`、`BatchActionBar.tsx`、`BatchOperationConfirm.tsx` | 批量操作 / 导出 | `ticket:batch_*` / `ticket:export` |
| `components/business/SmartAssignmentModal.tsx` | 智能指派 | `ticket:assign` |
| `components/business/TicketCategoryImport.tsx`、`TicketCategoryExport.tsx`、`TicketCategoryDragSort.tsx`、`TicketTypeFormModal.tsx` | 分类导入导出/排序、类型表单 | `ticket_type:manage` |
| `components/business/SLAAlertSystem.tsx` | 告警处理 | `sla:write` |
| `components/workflow/BPMNDesigner.tsx`、`workflow/designer/WorkflowToolbar.tsx` | 设计器保存/发布 | `workflow:write` / `bpmn:write`（按端点） |
| `components/templates/TemplateEditor.tsx`、`templates/FieldDesigner.tsx` | 模板编辑 | `ticket_template:write` |
| `components/reports/ReportGenerator.tsx` | 报表生成/导出 | `dashboard:admin`（生成/导出） |

> 进度（2026-10-10，批次 14 收口）：逐项核毕——①`TicketKanbanBoard`、`SmartAssignmentModal`、`SLAAlertSystem`、`TemplateEditor`、`FieldDesigner`、`ReportGenerator`、`TicketCategoryImport/Export/DragSort` 均**未被任何页面/组件引用（未接线死组件，登记为清理候选）**，不新增门禁；②`TicketKanban`（活组件）菜单删除已按 `ticket:delete` 门禁（:158），`handleStatusChange`/`cursor-move` 为**未接线的拖拽残留**（全文件无 DnD 实现，状态变更不可达）；③`TicketTypeFormModal` 由已门禁（`ticket_type:manage`，批次 11）的工单类型页按钮触发，经核无需处理；④`BPMNDesigner` 由工作流页设计器模态调用（入口 `canBpmnWrite` 已门禁，批次 9），补防御性 `readOnly={!canBpmnWrite}` 传入（组件内保存/部署/撤销/删除/复制粘贴/对齐均已按 `readOnly` 短路）。

### 8.7 独立复审与缺口复核（2026-10-10，批次 15）

复审方式：只读子代理独立扫描「仍在使用的写入口 × 门禁包裹 × 后端端点码」，与本地复核交叉；后端守卫测试（`go test ./router -run Guard`、`./internal/authz`）全绿。

- **新增 GAP 5 处（已修复）**：
  1. `components/problem/ProblemAssociationsTab.tsx:192/222`（移除/添加问题关联）→ `problem:write`（`problem_routes.go:35-36`）；组件级 `useCan` 门禁（宿主 `problems/$id` 原无门禁）。
  2. `components/business/detail-tabs/ApprovalWorkflowPanel.tsx:576`（批准/拒绝/委托）→ `workflow:write`（`ticket_routes.go:219-226`）；`canApprove` 追加 `canWorkflowWrite`。
  3. `pages/(main)/profile/index.tsx:391`（编辑资料）→ `user:write`（`PUT /users/:id`，`common_system_routes.go:51`）；入口禁用。（**已被 B3 自助面取代**：改走 `PUT /users/me`，禁用移除）
  4. `pages/(main)/profile/index.tsx:630/813`（保存修改/修改密码）→ `user:write`（`PUT /users/:id`、`PUT /users/:id/reset-password`，`:51/56`）；入口禁用。（**已被 B3 自助面取代**：`PUT /users/me` + `POST /auth/change-password`，禁用移除）
  5. `components/layout/header/Header.tsx` + `NotificationDrawer.tsx`（标记已读/全部已读/点击自动已读）→ `notification:write`（`:247-248`）；新增 `canMarkRead` 开关。
- **产品缺口（已闭环，B3/2026-10-10）**：`/profile` 自助面已补——后端新增 `PUT /users/me`（`handlers/common/user_profile.go`，name/email/phone 白名单 + 邮箱唯一性 + 跨租户 fail-closed）；改密改走 `POST /auth/change-password`（模态旧密码字段已存在）；三处 `user:write` 禁用移除，部门字段改只读（治理字段由管理员维护）。
- **死组件补充清单（清理候选）**：`TicketRatingSection`、`TicketNotificationSection`、`TicketMultiLevelApproval`、`TicketTrendPrediction`、`TicketRootCauseAnalysis`、`AutomationRuleForm`、`TicketAttachmentSection`、`SLAViolationMonitor`、`batch-operations/*`（`BatchOperationBar/Modal/ProgressModal` + `useBatchOperations`）、`ticket-api.batchUpdateTickets`。
- **已登记缺口复核（状态更新，2026-10-10）**：①`tickets/relations` 写端点未注册（仅 GET，`ticket_routes.go:83/102/143/146`）；②`/tickets/batch-assign`、`batch/assign*`、`batch-notify` 未注册（仅 `/export`、`/batch-delete`，`:32-33`；**B5 核实：无活跃 UI 调用面，roadmap 禁用契约维持**）；③SLA 新建/编辑页面缺失 ✅ 已补（B2）；④admin 区区域守卫粗粒度（`ADMIN_AREA_PERMISSIONS` 4 码）✅ 已细化（B6：并集 20 码 + 24 页级门禁）；msp 区维持现并集（无专职细码需求）；⑤`TicketViewSelector` 保存模态未接线 ✅ 已清理（B1 删除死 UI）。
- **验证缺口（✅ 已补，2026-10-10）**：批次 13–15 + B6 门禁项已做脚本化浏览器双账号实测（正例 `admin_demo_customer` / 反例探针 `b7_probe_tech`(technician)，隔离上下文；Playwright 脚本 + 24 张截图留痕）；证据见 `docs/plan/evidence/b7-dual-account-2026-10-10.md`，原始 JSON/截图在 `.tmp/web-check/b7/`（未跟踪）。观察项：①AI 审批队列无 pending 数据，行级通过/驳回沿用批次 5 证据；②工单 224 正例为申请人身份，「批准/拒绝」按申请人规则隐藏（非权限缺口）；③无 `notification:write` 用户偏好开关可点但无保存入口（体验项，无 API 写入面）。
- 验证：`tsc -b` 0、eslint 0；jest detail-tabs/header/problem/profile 39/39。
