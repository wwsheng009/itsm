# 03 · 集成点：与本仓已有能力的结合

> 状态：**Draft v0.2（最佳实践裁决完成）**
> 更新日期：2026-10-17
> 目标读者：后端/前端研发、架构
> 前置阅读：[01 模块蓝图](./01-module-blueprint.md)、[02 数据模型](./02-data-model.md)

---

## 1. 总览

| # | 已有能力 | 结合方式 | 阶段 |
|---|---|---|---|
| 1 | 工单/事件 | 单据挂 `project_id`；项目任务生成工单/事件；项目工作台聚合 | P0 |
| 2 | 问题/Known Error | 项目风险升级为问题；CSI 项目由问题孵化 | P1/P2 |
| 3 | 变更/发布 | 变更挂项目；结项门禁校验未结变更 | P0/P1 |
| 4 | 服务请求/开通任务 | 交付执行项纳入项目进度 | P2 |
| 5 | CMDB | Project↔Application↔Microservice（已有边）；CI/影响分析带项目上下文 | P1 |
| 6 | BPMN 工作流 | `business_type=project` 注册流程绑定（立项/结项/预算） | P1 |
| 7 | 审批链 | 复用 `project_manager` 解析器（已存在）；预算/结项审批走审批链 | P1 |
| 8 | SLA | 任务/里程碑绑定交付 SLA；逾期进告警历史 | P1/P2 |
| 9 | 通知/outbox | 任务分派、里程碑到期、状态变更 | P1 |
| 10 | 知识库 | 交付物/复盘沉淀为 KB 文章 | P1 |
| 11 | 通用附件 | `attachments.biz_type=project\|project_task` | P0 |
| 12 | 审计 | 立项/结项/成员/预算动作写 auditlog | P0 |
| 13 | 报表/仪表盘 | 项目组合视图、交付准时率 | P2 |
| 14 | AI 能力 | 周报生成、风险预警、纪要转任务（LLM Gateway + 审计） | P2 |
| 15 | MSP 受控访问 | 工作台/深度切换（后置） | P2+（见 05） |
| 16 | 团队/群组归口 | 项目归口 team/group（`membership_org` 维度） | P1 |
| 17 | 全局搜索与个人待办 | `project`/`project_task` 纳入 `global_search`；"我的项目任务"聚合 | P1 |
| 18 | 满意度调查 | 结项 CSAT（复用 `survey`/`survey_response`） | P1 |
| 19 | 自动化/升级参考 | 任务逾期通知/升级（notification+outbox；参考 `escalation_matrix` 模式） | P1 |
| 20 | 工作日历 | 自然日先行；租户级日历后置（与 SLA 共享单源） | 后置（与依赖/甘特同步） |
| 21 | 采购/资产/许可/合同台账 | 结项清单「资产/许可登记」链接既有台账（`asset`/`asset_license`/`vendor`/`contract`，同租户弱引用）；不做采购流程 | P2（PE-26） |

## 2. 逐项说明

### 2.1 工单 / 事件（P0）

- **单据 → 项目**：`tickets`/`incidents` 加可空 `project_id`（见 02 §4）；单据详情显示所属项目并可"加入/移出项目"（权限 `project:write`）。
- **任务 → 单据**：项目任务一键生成工单（复用既有创建端点与工单类型/表单机制：`ticket_type` + `form_fields`），回写 `related` 关系；任务阻塞可生成事件。
- **聚合**：项目工作台按 `project_id` 查询单据列表（复用各域列表服务，传入项目过滤条件）。
- **注意**：任务生成单据属于跨模块写操作，需在服务层做权限与租户双重校验；事件侧沿用既有状态机，不新增旁路。

> **落地说明（PE-5 增补，2026-10-18）**：**「创建时挂接项目」与「单据侧所属项目反查」已落地（五域：工单/事件/问题/变更/服务请求）**——
> ① **创建时挂接**：五域创建请求新增可选 `projectId`；前置校验收敛为单一端口 `handlers/common/projectlink`（`GuardCreate`，
> 由项目服务 `ValidateAttach` 实现：`project:write` 动态复核 + 项目存在（租户内、可见）且**非终态**），校验通过后 `project_id` **随 INSERT 一次写入**
> （避免「先建单再挂接」的部分成功；未装配端口时携带 `projectId` 的创建请求 **fail-closed 503**，不静默丢弃关联意图）。
> ② **反向查询**：新增 `GET /api/v1/{tickets|incidents|problems|changes|service-requests}/:id/project`（各域 `read` 码），
> 返回 `{project: {id,name,code,status} | null}`；可见性复用项目详情的 `GetForViewer` 谓词，项目对查看者不可见时按 `null` 返回（不泄露）。
> ③ **前端**：共享组件 `ProjectSelect`（创建页选择；仅列出非终态项目，`project:read + project:write + 域写码` 门控）与
> `RecordProjectLink`（详情页展示"所属项目"+ 加入/移出，加入/移除走既有 `POST|DELETE /.../:id/project`）已接入五域创建页/详情页。
> 创建前置校验与既有挂接端点（§2.1 首条）共用 `ValidateAttach` 同一口径；MSP 跨租户面保持只读，不暴露写入口。

### 2.2 问题 / Known Error（P1/P2）

- 项目风险（`project_risks.kind=risk`）可"升级为问题单"（复用 `problem` 创建）；
- CSI 项目反哺：问题解决后发起改进项目，结项沉淀 Known Error 与知识库文章。
- **创建时挂接/详情展示**：问题域同样支持创建请求携带 `projectId` 与详情页「所属项目」加入/移出（见 §2.1 落地说明）。

> **落地说明（PE-21，2026-10-24）**：**风险/问题台账 + 转换链路已落地**——`project_risks`（kind=risk|issue；等级/概率/影响/负责人/状态机；close 必填结论）；
> 转换 `POST /projects/:id/risks/:riskId/convert {target: problem|change}` 经**端口 + 适配器**（`RiskProblemAdapter`/`RiskChangeAdapter`，bootstrap 装配，未装配 503）复用问题/变更域服务，
> 问题映射 priority（low/medium/high）、变更映射 riskLevel 且置 draft；成功后 CAS 回写 `related_problem_id`/`related_change_id`（同目标二次转换 409 + `existingId`；回写失败返回产物 ID 提示人工关联）；
> 审计 `resource=project`：`risk_create|risk_update|risk_delete|risk_status|risk_convert`。设计：[plan/p2-risk-design.md](./plan/p2-risk-design.md)。

### 2.3 变更 / 发布（P0/P1）

- `changes.project_id` 关联；项目工作台展示进行中的变更；
- **结项门禁**：存在非终态变更（`{closed,cancelled,rejected,rolled_back}` 之外）时拒绝结项，错误信息列出阻塞单号；
- **双轨制（B6 裁决）**：交付变更走 ITSM Change（`changes.project_id` + 双向引用）；项目计划/基线变更走项目内变更日志（`project_baselines.reason` + 审计），两轨不混；
- 发布/标准变更（P2 评估）：发布挂项目为可选关联；标准变更沿用既有流程，不新增机制。
- **创建时挂接/详情展示**：变更域同样支持创建请求携带 `projectId` 与详情页「所属项目」加入/移出（见 §2.1 落地说明）。

### 2.4 服务请求 / 开通任务（P2）

- 交付类项目把 `ProvisioningTask` 的执行状态汇总进项目进度；
- 审批类动作仍留在服务请求自身审批链，项目侧只做展示与门禁，避免两套审批。
- **创建时挂接/详情展示**：服务请求域同样支持创建请求携带 `projectId` 与详情页「所属项目」加入/移出（见 §2.1 落地说明）；创建入口为服务目录申请页（`pages/(main)/service-catalog/request/$id`），`projectId` 经 `service-catalog-api` 映射层透传至 `POST /service-requests`。

### 2.5 CMDB（P1）

- 已有：`application.project_id`、`microservice.application_id`、CI 关系（`ci_relationships`）；
- 增补：项目详情展示"关联应用/微服务/CI"；CI 影响分析（`cmdb` 影响分析端点）纳入"所属项目"上下文；
- 结项时校验项目应用/CIs 的归属与状态（可选提示，不强制）。

### 2.6 BPMN 工作流（P1）

- `process_binding.business_type` 现有注释枚举 `ticket/change/incident/problem/service_request`，扩展 `project`（与 `scenario`/`category` 字段配合）；
- 启动流程：立项审批、结项审批、预算变更审批；`process_instance.business_key` 约定 `project:<id>`；
- 流程变量注入项目上下文（tenant_id、project_id、manager_id），异步执行必须携带 `tenantctx`（对齐多租户不变量 I6）。

### 2.7 审批链（P1）

- 复用 `service/approver/project_mgr_resolver.go`：`project_manager` 解析器已实现（要求项目 active、manager 存在）；
- 如需"项目干系人/成员"作为审批人，新增 resolver（type=`project_member`）时遵循既有 resolver 接口与测试模式；
- 审批类动作保持在本租户治理面（MSP 场景同口径）。

### 2.8 SLA（P2 评估；P1 先轻量时限）

- P1 轻量：任务/里程碑按计划时间做到期/逾期提醒（notification + outbox），不动 SLA 引擎；
- SLA 引擎级绑定为 P2 评估项：现状 `service/sla_monitor_service.go` 仅扫描票据（`Ticket` + `SLADefinitionIDNEQ`），绑定项目任务需先评估 monitor 域扩展（口径见 [plan §1-4](./plan/p0-integration-contracts.md)）；
- MSP 后置：里程碑/验收节点绑 `msp_contract_sla_binding`，验收结果进结算口径（见 05）。

### 2.9 通知 / outbox（P1）

- 事件：任务分派、任务逾期、里程碑到期、项目状态变更、成员变更；
- 复用 `notification` + 事务 outbox 模式（不 fire-and-forget）；收件人解析限定目标租户内 membership（不跨租户投递）。

> **落地说明（PE-14 收尾，2026-10-23）**：**里程碑到期通知已落地**——租户级扫描器（`service/project_milestone_due_scanner.go`，bootstrap 30 分钟 tick、提醒窗口默认 72h 可配）把「`planned` 且已逾期或窗口内到期」的里程碑入箱（`notification.deliver` outbox），收件人 = `projects.manager_id`（权威）∪ 项目内 `pm`/`stakeholder` 活跃成员，幂等键 = 里程碑 + 目标日期 + 触发类型（重复扫描不重复投递）；投递走既有 in_app 链路并落 `notification_deliveries` 投递审计（项目通知不写 `ticket_notifications`），入箱轮次落 `source=job` 审计 `project.milestone_due_scan`；偏好双词表新增 `project_milestone_due`（后端 `ListNotificationEventTypes`/默认偏好 + 前端 `EVENT_TYPES` 与 zh/en 翻译同步）。任务级 `project_task_due_soon`/`project_task_overdue` 词表已预留、后续批次实现。详见 [plan/p1-milestone-notification-design.md](./plan/p1-milestone-notification-design.md)（D-23）。

### 2.10 知识库（P1）

- 结项复盘自动生成 KB 文章草稿（复用 `knowledge` 创建/发布端点，租户内分类与可见性）；
- 交付文档引用 KB 文章（`project_documents.kb_article_id`）。

> **落地说明（PE-15，2026-10-21）**：草稿经知识库域服务创建（新建即草稿、不自动发布；分类 `项目复盘`），引用先落 `project_closures.retro_kb_article_id`（`project_documents` 属 P2 可选表）。

### 2.11 通用附件（P0）

- 复用 `ent/schema/attachment.go` 多态设计：`biz_type=project | project_task`，`biz_id` 为项目/任务 ID；
- 不新建附件表；沿用 `uq_attachments_path`、`tenant_isolation_attachments` 等既有约束与幂等键。

### 2.12 审计（P0）

- 关键动作：`project.create/update/status_transition/close`、成员增删、任务状态流转、门禁拒绝；
- 写 `auditlog`（复用 `middleware/audit.go` 通道），保留操作者/目标/原因。

### 2.13 报表 / 仪表盘（P2）

- 项目组合视图：按状态/健康度/负责人聚合；交付准时率（计划 vs 实际）；
- 复用 `handlers/dashboard`、`reports` 与前端图表组件目录（`components/charts/`）。

> **落地说明（PE-24，2026-10-27）**：组合视图 `GET /projects/portfolio`（五维聚合 + 与列表**同谓词/同可见性/同行序**的项目行）与
> 度量 `GET /projects/metrics`（按时交付率/里程碑达成率/阶段门一次通过率/资源利用率，均含明细供抽样对账）已落地；
> **口径全部由执行数据实时派生**（不新增手工填报、不落快照表），与列表共用同一 `ListFilter` 收窄路径（D-40）；
> 资源利用率复用 PE-23 热图 planned/capacity 同一实现（D-39/D-41）。前端独立页 `/projects/portfolio`（报表中心互链）。
> 设计与公式见 [plan/p2-portfolio-metrics-design.md](./plan/p2-portfolio-metrics-design.md) §4/§5。

### 2.14 AI（P2）

- 场景：项目周报/状态摘要、风险预警、会议纪要转任务、任务拆分建议；
- 全部经 LLM Gateway（`itsm-backend/ai`、`internal/llm`），保留 `tool_invocation` 审计与降级路径（AI 失败不影响主流程）。

> **落地说明（PE-25，2026-10-28）**：**已落地**（周报摘要/风险预警/纪要转任务；**任务拆分建议**不在 PE-25 验收范围，端口已预留可低成本追加）——三端点 `POST /projects/:id/ai/{summary,risk-warnings,minutes/tasks}`（`project:read`，POST=执行一次生成，**不写业务状态/不落库**，可见性同详情 404）：摘要经 PE-17 同源事实派生后交 LLM（AI 不可用→事实派生文本 `generatedBy=deterministic`）；风险预警 = 规则信号（恒返回，六类逾期/阻塞/高风险/门被拒，每类明细前 5 条 + `signalTotal`/`truncated` 不静默）+ AI 判断增强段（降级→`rules`）；纪要为**不可信数据块**（只抽取不执行指令），建议经用户勾选后走既有 `POST /projects/:id/tasks`（`project:write`）。**降级矩阵**：未装配/`llm_error`/`parse_error` 一律 **200** + `degradeReason` 受控词表（AI 失败不影响主流程）。**审计**：`resource=project`，`ai_summary_generate|ai_risk_warnings|ai_minutes_extract`（不落原文）+ 网关 `ai_llm_calls` 遥测（`WithProviderRequest` 绑租户解析链）。设计与证据：[plan/p2-ai-design.md](./plan/p2-ai-design.md)（D-44~D-48）。

### 2.15 MSP 受控访问（P2+，后置）

- 详见 [05 MSP 交付](./05-msp-delivery.md)；核心口径：不做跨租户数据引用，只做"受控访问目标租户数据"。

### 2.16 团队/群组归口（P1）

- 项目归口到 team/group：优先复用 `user_tenant_membership_org`（已支持 team/group/project 多态维度），不新建 team 关联表；
- 归口用于：默认可见范围、通知收件人候选、报表分组；
- 落库前校验 team/group 在目标租户内的有效性与软删状态。

> **落地说明（PE-17，2026-10-23）**：**归口已生效**——`projects.owner_team_id` 写入校验（同租户、未软删、active，否则 400）；读取富化 `ownerTeamName` + 软删降级 `ownerTeamDeleted`（列表/详情/工作台提示重新归口）；**报表分组** `GET /projects/report-groups` 按归口团队聚合项目数与健康度分布（可见性谓词与列表一致收窄）；**通知候选** 里程碑到期通知收件人追加归口团队负责人（团队软删/停用自动降级）。**决策 D-24：归口不扩大项目可见性**（保持 D-06 成员可见性，避免"整队可见"越权）。状态报告模板与周期见 §2.21。

### 2.17 全局搜索与个人待办（P1）

- `handlers/global_search` 现覆盖 ticket/incident/problem/change/knowledge，**未覆盖项目/任务**：增加 `project`（名称/编号）与 `project_task`（任务名）两类结果；
- 个人工作台增加"我的项目任务"聚合（含里程碑到期），复用 dashboard 数据获取模式。

> **落地说明（PE-16，2026-10-22）**：`global_search` 已新增 `project`（名称/编号）与 `project_task`（任务名）两类结果，均**强制可见性谓词**（管理角色 ∨ 活跃成员 ∪ 项目经理；查询层收窄，跨租户隔离；防搜索泄露不可见项目）；个人工作台在 `/dashboard` 落地「我的项目任务」卡片（`project:read` 门控），并行聚合 `GET /projects/my-tasks` 与**新增** `GET /projects/my-milestones`（我负责的 planned 里程碑，默认 30 天内含逾期、≤200；局部降级不影响其他分区）。详见 [plan/p1-search-design.md](./plan/p1-search-design.md)。

### 2.18 结项满意度（P1）

- 结项门触发 CSAT 调研（`survey`/`survey_response` 现成，`survey_type` 支持 CSAT）；
- 结果进项目复盘视图；满意度缺失不阻塞结项，登记为待办。

> **落地说明（PE-15，2026-10-21）**：结项成功时尽力触发（失败仅日志、不阻塞结项，可手动重试；D-20）；答卷经 `survey_responses.project_id`（P1 补齐）关联项目，复盘视图与趋势报表随 P2 报表批次。

### 2.19 自动化与升级（P1）

- 任务逾期/阻塞提醒走 notification + outbox（与既有可靠投递一致）；
- 升级策略参考 `escalation_matrix`（SLA 配置面）的模式，**不硬接**：先在项目内配置"逾期 N 天 → 通知 PM/干系人"，后续再评估与 SLA 面统一。

### 2.20 工作日历（后置）

- v1 排期按自然日计算，并在 UI/文档明确标注限制；
- 依赖/甘特落地时引入**租户级工作日历**（节假日/工作日），作为 SLA 与项目排期的共享单一来源；禁止项目各自定义日历（口径漂移风险）。

> **落地说明（PE-23，2026:10: 26）**：资源负荷热图（`GET /projects/:id/resource-heatmap`）按本口径执行——工时按**自然日**均摊、周容量为整周口径，响应携带 `calendar{mode:natural_days,businessDaysOnly:false,note}` 且前端常驻标注；租户级工作日历仍后置（与依赖/甘特同步），届时热图与排期同步切换单源。

### 2.21 状态报告模板与周期（P1）

- 模板（周期 + 章节）数据驱动，报告正文由事实数据实时派生（§7 指标口径），先导出 Markdown/JSON，AI 增强后置 P2；

> **落地说明（PE-17，2026-10-23）**：**已落地**——新表 `project_report_templates`（`cadence`=weekly|biweekly|monthly、`sections`=overview|tasks|milestones|gates|variance|team、`version` 乐观锁、软删）+ CRUD API；`POST /projects/:id/reports/generate` 实时派生 `{markdown, data}`（任务/里程碑统计、阶段门、基线偏差、归口），**不落报告快照**（决策 D-25）；审计 `resource=project`（`report_template_create|update|delete`、`report_generate`）。设计与证据：[plan/p1-report-design.md](./plan/p1-report-design.md)。

### 2.22 采购/资产/许可/合同台账（P2/PE-26）

- 结项清单「资产/许可登记」链接既有台账，不做采购流程（B5 裁决）；
- 链接目标（**同租户**弱引用，不做跨租户引用）：`asset`（资产）、`asset_license`（许可证）、`vendor`（供应商）、`contract`（合同，通用 `contracts` 表）；
- 写入走项目域（不直连他域 repository）：`project_asset_links` 表 + 存在性校验；

> **落地说明（PE-26，2026-10-28）**：**已落地**——`project_asset_links`（`link_type` 四词表 + `target_id` 弱引用 + 备注 + 软删；未删行唯一 partial index；RLS 015/`rls-apply` 清单同步）；
> 4 端点：`GET|POST /projects/:id/asset-links`、`DELETE /projects/:id/asset-links/:linkId`、`GET /projects/:id/asset-link-candidates?type=&q=&limit=`（候选=同租户最小字段：编号/名称/标题 + 辅标签，不回传价格/密钥/联系方式）；
> 结项清单响应携带 `assetsLinks`/`assetsLinkSummary`（目标缺失标 `targetMissing`），门禁语义不变（确认/豁免）；
> 审计 `asset_link_create|asset_link_delete`（`resource=project`）。设计与证据：[plan/p2-procurement-design.md](./plan/p2-procurement-design.md)。

## 3. 落地注意（跨项）

1. 所有跨模块写操作（任务→工单、风险→问题）必须走服务层校验 + 审计，禁止 handler 直连他域 repository；
2. 新增查询一律带 `tenant_id` 条件，RLS 为最后防线而非唯一防线；
3. 前端聚合页对 N+1 敏感：项目工作台的单据聚合按域批量查询（参考既有工作台/门户实现），避免逐条拉取；
4. 与工作流/通知的异步联动必须事务内写 outbox（对齐"可靠执行架构"）；
5. 度量口径统一：按时交付率/里程碑达成率/资源利用率等指标全部由执行数据派生（任务/里程碑/单据），不新增手工填报；口径以基线（`project_baselines`）为参照。
