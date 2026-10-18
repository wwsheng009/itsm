# 项目管理模块文档集（Project Management）

> 状态：**P0–P2 已收口（PE-1~PE-26 全部落地并通过真实库 + HTTP E2E + 浏览器验证）**
> 更新日期：2026-10-28
> 定位：`docs/project-management/` 的**唯一入口索引**——模块蓝图、数据模型、集成点、身份与成员、MSP 交付、实施路线。
> 关联：多租户/MSP 权威口径以 [multi-tenant canon](../multi-tenant/plan/msp-concept-model-and-architecture-canon.md) 为准；现有产品架构文档将项目管理列在"组织架构支撑模块"下（`prd/ITSM产品模块架构设计.md` §3.1），本文件集将其**上移为一级业务模块**并给出演进方案。

---

## 0. 30 秒速查

| 问题 | 看这里 |
|---|---|
| 这个模块做什么、不做什么、分几期 | [01 模块蓝图](./01-module-blueprint.md) |
| 表怎么建、字段怎么设计、单租户原则 | [02 数据模型](./02-data-model.md) |
| 怎么跟工单/变更/CMDB/BPMN/SLA/知识库接 | [03 集成点](./03-integration.md) |
| 项目成员、外部人员怎么表示 | [04 身份与成员](./04-membership-and-external-users.md) |
| MSP 客户交付项目怎么支持 | [05 MSP 交付（后置）](./05-msp-delivery.md) |
| 怎么落地、每期验收什么 | [06 实施路线](./06-implementation-roadmap.md) |
| 方案有没有遗漏（通用 PM 能力/现有功能结合） | [07 完整性审查与裁决](./07-completeness-review.md)（B1–B7 结论 + 非目标） |
| 2026-10-18 修复了什么、怎么验证的 | [08 变更记录](./08-change-log.md) |
| P0 怎么落地（接口契约/DDL/页面） | [plan/ P0 详细设计](./plan/p0-integration-contracts.md)：集成契约 + 数据 DDL + API/页面 |

## 1. 文档清单

| 文档 | 状态 | 定位 |
|---|---|---|
| [README.md](./README.md)（本文件） | Draft v1.0 | 入口索引、阅读路径、决策记录（D-01~D-53） |
| [01-module-blueprint.md](./01-module-blueprint.md) | Draft v0.2 | 模块定位与边界、能力矩阵、阶段门、命名约定 |
| [02-data-model.md](./02-data-model.md) | Draft v0.2 | 数据模型草案、单租户原则、基线快照、工程约定 |
| [03-integration.md](./03-integration.md) | Draft v0.2 | 与本仓已有能力的结合点清单（20 项） |
| [04-membership-and-external-users.md](./04-membership-and-external-users.md) | Draft v0.1 | membership 单源、外部人员简化方案、provider 投影 |
| [05-msp-delivery.md](./05-msp-delivery.md) | Draft v0.1 | MSP 客户交付项目（P2 后置）与前置改造 |
| [06-implementation-roadmap.md](./06-implementation-roadmap.md) | Draft v0.2 | P0–P3 分期（PE-1~PE-26）、工程落地清单、验收要点 |
| [07-completeness-review.md](./07-completeness-review.md) | **裁决版 v1.0** | 完整性审查 + 最佳实践裁决（B1–B7 结论、非目标、回写记录） |
| [08-change-log.md](./08-change-log.md) | **已实施 v1.0** | 2026-10-18 进度汇总/MSP 顶栏 403/日期编辑崩溃的变更、验证与部署证据 |
| [plan/p0-integration-contracts.md](./plan/p0-integration-contracts.md) | Draft v1.0 | P0 集成契约：14 项决策收敛、接口/时序/错误码/审计/事件清单/测试矩阵 |
| [plan/p0-data-ddl.md](./plan/p0-data-ddl.md) | Draft v1.0 | P0 表 DDL 草案、迁移兜底、RLS 策略、登记清单 |
| [plan/p0-api-and-pages.md](./plan/p0-api-and-pages.md) | Draft v1.0 | P0 API 总表、错误码、前端页面/路由/权限/菜单映射 |
| [plan/p0-design-review.md](./plan/p0-design-review.md) | 审查记录 v1.0 | P0 设计审查：11 项发现与处置、实施前复核清单 |
| [plan/p1-template-design.md](./plan/p1-template-design.md) | **已实现 v1.0** | P1 项目模板（PE-11）：表结构/payload/生成规则/API/幂等与并发/验证记录 |
| [plan/p1-gate-design.md](./plan/p1-gate-design.md) | **已实现 v1.0** | P1 阶段门（PE-12）：门禁规则/状态机/表结构/流程集成与降级/API/审计/验证证据 |
| [plan/p1-closure-design.md](./plan/p1-closure-design.md) | **已实现 v1.0** | P1 结项清单与 CSAT（PE-15）：清单项与门禁规则/表结构/端点/成员回收与 CSAT 装配/验证证据 |
| [plan/p1-search-design.md](./plan/p1-search-design.md) | **已实现 v1.0** | P1 搜索与待办（PE-16）：全局搜索可见性谓词/D-21·D-22/`my-milestones` 端点/个人工作台聚合/验证证据 |
| [plan/p1-milestone-notification-design.md](./plan/p1-milestone-notification-design.md) | **已实现 v1.0** | P1 里程碑到期通知（PE-14 收尾）：扫描入箱/幂等键/收件人解析/投递审计/双词表/验证证据（D-23） |
| [plan/p1-report-design.md](./plan/p1-report-design.md) | **已实现 v1.0** | P1 归口与状态报告（PE-17）：归口校验/降级/报表分组/通知候选、报告模板与实时派生（D-24/D-25）/验证证据 |
| [plan/p2-risk-design.md](./plan/p2-risk-design.md) | **已实现 v1.0** | P2 风险/问题台账（PE-21）：表结构/状态机/转换契约（D-26~D-32）/API/审计/验证证据 |
| [plan/p2-budget-hours-design.md](./plan/p2-budget-hours-design.md) | **已实现 v1.0** | P2 预算与工时（PE-22）：轻量预算/任务级实际工时/基线预算冻结（D-33~D-36）/API/审计/验证证据 |
| [plan/p2-resource-heatmap-design.md](./plan/p2-resource-heatmap-design.md) | **已实现 v1.0** | P2 资源负荷（PE-23）：周容量两级配置/只读热图口径（计划摊分/剩余/逾期落本周）/日历标注（D-37~D-39）/API/验证证据 |
| [plan/p2-portfolio-metrics-design.md](./plan/p2-portfolio-metrics-design.md) | **已实现 v1.0** | P2 组合视图与度量（PE-24）：组合聚合/四项度量口径（交付/里程碑/门一次通过/资源利用率）/明细对账（D-40~D-43）/API/验证证据 |
| [plan/p2-ai-design.md](./plan/p2-ai-design.md) | **已实现 v1.0** | P2 AI（PE-25）：周报摘要/风险预警/纪要转任务、降级矩阵与审计（D-44~D-48）、LLM 适配器与提示词策略/API/验证证据 |
| [plan/p2-procurement-design.md](./plan/p2-procurement-design.md) | **已实现 v1.0** | P2 采购/资产结项登记（PE-26）：`project_asset_links`（同租户弱引用/软删/未删行唯一）、列表/新增/删除/候选 4 端点、结项清单明细与门禁语义（D-49~D-53）/验证证据 |

> 维护规则：新增/重命名/停用文档必须同步本索引。P2 详细设计（PE-21~PE-26）均已登记；后续批次（P2 收口验证/P2+/P3）继续放 `plan/` 子目录并在本索引登记。

## 2. 按角色的阅读路径

| 角色 | 推荐顺序 |
|---|---|
| 产品/决策 | 01 蓝图 → 06 实施路线 §分期 → README §决策记录 |
| 架构评审 | README §设计基线 → 02 数据模型 → 04 身份与成员 → 05 MSP 交付 |
| 后端研发 | 02 数据模型 → 03 集成点 → 06 实施路线 §工程清单 |
| 前端研发 | 01 蓝图 §能力矩阵 → 06 实施路线 §前端 |
| 测试/QA | 06 实施路线 §验收要点 → 各文档"验收/注意事项"小节 |
| MSP/多租户方向 | 04 身份与成员 → 05 MSP 交付 → [multi-tenant canon](../multi-tenant/plan/msp-concept-model-and-architecture-canon.md) |

## 3. 模块一句话

**项目 = 有明确起止的一次性交付工作**，与 ITSM 管理的"持续性服务过程"互补；P0 先在**单租户内**闭环（项目主数据 + 任务 + 成员 + 单据关联），MSP 客户交付场景**后置**，且**不引入任何跨租户数据引用**。

## 4. 设计基线（2026-10 代码调研结论）

现有 `Project` 是"组织架构附属实体"，已有以下可复用资产：

| 资产 | 位置 | 现状 |
|---|---|---|
| 项目实体 | `itsm-backend/ent/schema/project.go` | name/code/description/manager_id/department_id/start_date/end_date/status/tenant_id；租户内 code 唯一（`uq_project_tenant_code`） |
| 关系边 | 同文件 + `ent/schema/application.go` | Project↔Department、Project↔Application（`application.project_id`）、Project↔Tag |
| CRUD API | `itsm-backend/handlers/project/handler.go` | `/api/v1/projects` 列表/创建/详情/更新/删除，租户中间件 + `project:read/write` |
| 权限码 | `itsm-backend/internal/authz/catalog.go` | `project:read`、`project:write`、`project:delete`（单一权威源 + codegen 预检） |
| 审批人解析器 | `itsm-backend/service/approver/project_mgr_resolver.go` | 审批链支持 `project_manager` 解析（要求项目 active） |
| 组织成员载体 | `ent/schema/user_tenant_membership_org.go` | `org_type` 已含 `project`（membership 为唯一载体） |
| RLS/租户守卫 | `cmd/rls-apply`、`internal/schema/tenant_guard.go` | `projects` 已入 RLS 表清单；`project_tags` 为平台级豁免（衍生表） |
| 前端 | `itsm-frontend/src/pages/(main)/projects/index.tsx` | 列表 + 弹窗 CRUD；"进度"列为占位（无 API 支撑）；路由 `/projects` |

**结论**：不需要新建平行体系，把现有 `Project` 升级为一级业务模块即可；身份、审批、通知、附件、审计、工作流等全部复用既有扩展点。

## 5. 决策记录（讨论已冻结项）

| 编号 | 决策 | 理由/影响 |
|---|---|---|
| D-01 | **单租户先行**：P0 所有 PM 实体 `tenant_id=owner`，不建任何指向他租户的字段/外键；跨租户引用后置单独立项 | 与租户隔离、RLS、tenant guard 一致；避免过早引入跨租户耦合 |
| D-02 | **外部人员不做独立实体**：不建 `external_person`；`project_member` 只认 `user_id`；"外部"为派生标签（membership 的 `source/account_kind/msp_role`） | 标识机制本仓已内置（`source=invite/allocation`），零 schema 变更；避免第二事实源 |
| D-03 | **成员/指派候选查目标租户活跃 membership**，不是 `users.tenant_id`（归属租户） | provider 用户投影后归属 provider 租户、成员关系在客户租户；查错会漏人/越权 |
| D-04 | **MSP 客户交付项目后置（P2+）**，走既有受控访问通道（工作台 + 深度切换），PM 表零改动 | 遵循 canon"记录只属一个租户、跨租户只做受控读写" |
| D-05 | **不做 Jira 式无限模型**（不做 issue type 集市、sprint 体系）；任务实体命名 `ProjectTask`，与 `ProcessTask`（BPMN）/`ProvisioningTask`（开通任务）消歧 | 控制复杂度，保住"计划-执行-复盘"闭环 |
| D-06 | 项目成员可见性用 `handlers/common/datascope` 行级守卫收口（同租户内非成员不可见） | 复用既有原语，不新造可见性机制 |
| D-07 | 需求/范围管理**默认不做实体**：范围说明 + 交付物清单（KB/附件）承载，需求入口走服务请求；出现"逐条评审 + 变更跟踪 + 逐条验收"再立项 | 需求管理是独立学科，避免 IT 内部项目背上研发需求管理 |
| D-08 | 计划基线做**轻量不可变快照**（P1），不做挣值（EVM） | PRINCE2 阶段边界冻结点；EVM 收益/成本比低 |
| D-09 | 工时先**任务级实际**（`actual_hours`）；worklog/timesheet P2 再评估 | "先 estimate/actual、后 timesheet"是主流工具的共同演进路径 |
| D-10 | 资源负荷只做**只读热图**（P2），不做自动均衡 | 调度算法复杂度高、收益低 |
| D-11 | 变更**双轨制**：交付变更 → ITSM Change；计划/基线变更 → 项目内变更日志 | ITIL 4 变更使能 vs 项目治理边界 |
| D-12 | 工作日历 v1 自然日 + 标注；依赖/甘特落地时引入租户级日历（与 SLA 单源） | 排期与 SLA 必须共享同一日历口径 |
| D-13 | 采购/资产**不做流程**，结项清单登记（链接 asset/license/vendor/contract） | 属供应商管理/收尾实践，非 PM 核心 |
| D-14 | P1 核心 = **模板 + 阶段门**（最高杠杆）；依赖 v1 仅 FS | ServiceNow PPM 轻量落地路径 + 阶段边界管理 |
| D-15 | 阶段门**一项目一门一行**（当前关口状态），历史由 `auditlog` 承载；门状态机 `pending→in_review→passed\|rejected`（拒绝可重提交） | 门是"当前关口"而非流水；避免历史表膨胀与查询复杂度（[plan/p1-gate-design.md](./plan/p1-gate-design.md) §3/§8） |
| D-16 | 四道门**顺序强制**：后门要求前门 `passed`；`activate` 强制立项门、`complete` 强制结项门 | 蓝图中四道门是阶段边界而非并列开关；状态机强制点保证"门真的挡得住"（同上 §2/§6） |
| D-17 | 审批集成**复用** `process_binding`（`business_type=project`）+ 审批桥接；无绑定/定义缺失时**降级自动通过**并留痕（`auto_approved_no_flow`） | 存量租户零配置可用、升级不阻塞；抑制项与审批轨迹可审计（同上 §5/§8 D3） |
| D-18 | 结项清单**一项目一行** `project_closures`；自动项（任务/单据/结项门）实时派生不入库，人工/产物项（复盘草稿/资产登记/成员回收/CSAT）落库 | 避免与事实源双写漂移；复用 CAS 乐观锁（[plan/p1-closure-design.md](./plan/p1-closure-design.md) §2/§3） |
| D-19 | 成员回收范围 = `member` 角色活跃成员（**保留 `pm`/`stakeholder`**）；provider 投影成员同样回收但**不动 membership 本体** | PM 为 `manager_id` 权威且受移除保护；干系人为复盘/CSAT 接收方；跨租户 allocation 撤销属 MSP 域（同上 §2/§4） |
| D-20 | **CSAT 不阻塞结项**（以契约 §4 为准）：结项成功时尽力触发（失败仅日志），并提供幂等手动重试端点 | 外部调研依赖不可靠；契约"缺失不阻塞、登记为待办"（同上 §2/§5） |
| D-21 | 全局搜索的项目/任务**只返回可见集合**：管理角色（`role.IsAdminLike`）直通租户全量；否则 = 活跃成员 ∪ 项目经理（`manager_id` 权威且受移除保护） | 契约 §12 防搜索泄露不可见项目；谓词在查询层收窄，与项目域可见性口径一致（[plan/p1-search-design.md](./plan/p1-search-design.md) §2/§3） |
| D-22 | 「我的项目任务」聚合**不动**已交付的 `/projects/my-tasks`，新增只读端点 `/projects/my-milestones`；前端卡片并行聚合两源（局部降级） | 零破坏既有契约；里程碑到期与任务语义不同→独立可测；与 dashboard 多源聚合模式一致（同上 §2/§4） |
| D-23 | 里程碑到期通知 = **扫描入箱（outbox）+ 既有投递链路**（不做 fire-and-forget）；幂等键 = 里程碑 + 目标日期 + 触发类型；收件人 = `manager_id` ∪ 项目内 `pm`/`stakeholder` 活跃成员；终态项目与 `reached`/`missed` 不提醒 | 与 `bpmn_timeout_scanner` 同模型（可靠投递 + 幂等收敛 + job 审计）；`manager_id` 是 PM 唯一权威（契约 §3）；同一触发生命周期内只投递一次（[plan/p1-milestone-notification-design.md](./plan/p1-milestone-notification-design.md) §2/§3） |
| D-24 | **归口不扩大项目可见性**（保持 D-06 成员可见性）；归口用于列表过滤/展示、**报表分组**、**通知候选**（里程碑到期通知追加归口团队负责人）；团队软删 → 读取降级 `ownerTeamDeleted` + 写入拒绝 | "整队可见"会让团队读到项目明细（越权风险）；可见性仍由成员关系权威控制，fail-closed（[plan/p1-report-design.md](./plan/p1-report-design.md) §2/§5） |
| D-25 | 状态报告 = **模板（周期 + 章节）+ 事实数据实时派生**，不落报告快照；先导出 Markdown/JSON，AI 摘要后置 P2；与计划模板**分表**（`project_report_templates` / `project_templates`） | 契约"模板 + 周期由配置驱动，数据派生，先导出后 AI"；避免快照与事实源双写漂移（同上 §2/§6） |
| D-26 | 风险/问题台账 = **单表 `project_risks`**（`kind=risk|issue`），不拆两表 | 02 §3.5 权威定义；两类字段与状态机同构（[plan/p2-risk-design.md](./plan/p2-risk-design.md) §2/§3） |
| D-27 | 状态机：`open→mitigating→closed`（`open` 可直达 `closed`），**close 必填 resolution**，`closed` 可 `reopen` | 关闭=有结论；复盘需要可重开，审计留痕（同上 §2/§4） |
| D-28 | 转换链路走**端口 + 适配器**（bootstrap 装配，未装配 503）；成功后 CAS 回写 `related_problem_id`/`related_change_id`；同目标二次转换 409 + `existingId` | 03 §2.2/§2.3；跨模块写必须服务层校验 + 审计（任务→工单同先例）（同上 §2/§5） |
| D-29 | 字段映射：问题 `priority` 由 level 映射；变更 `riskLevel` 同映射且置 `draft`，`justification` 附项目来源 | 最小复制、可追溯；不复制字段族（同上 §5） |
| D-30 | 终态项目（completed/cancelled）**拒绝风险写操作**（400）；读放行（只读回看） | 与里程碑/任务既有语义一致（同上 §2） |
| D-31 | 风险负责人须为**项目活跃成员**（`owner_id` 复用 `ensureAssignee` 同口径） | 成员有效性联动（同上 §2/§4） |
| D-32 | `related_ticket_id` 为**弱引用**但做同租户存在性校验；不约束工单终态 | 02 §3.5 原义；避免跨租户脏引用（同上 §2/§4） |
| D-33 | **轻量预算 = 项目主表两列**（`budget_amount` float ≥0 / `budget_currency` varchar(8)，空=租户默认）；**不建预算/成本独立表**；写入复用 `POST /projects`、`PUT /projects/:id` 且金额与币种**成对提供**（只给其一 400；amount<=0 = 清空）；变更写审计 `budget_update`（old/new；未变化不重复留痕） | 维持"轻量、非财务级"（07 §2-8）；零新表；币种对齐 `msp_contract.currency` 先例（[plan/p2-budget-hours-design.md](./plan/p2-budget-hours-design.md) §3/§5） |
| D-34 | **预算进基线快照**：`BaselineProjectSnapshot` 增 `budgetAmount`/`budgetCurrency`，`schemaVersion` 1→2；v1 旧快照解码容忍，diff 输出 `budget.frozen=false` 且不参与变更判定；v2 起输出项目级预算偏差（base/current/changed） | 02 §3.7「冻结内容：…/预算/…」；不可变快照加字段必须走 schemaVersion，保证历史基线可读（同上 §3/§5） |
| D-35 | **任务级实际工时**：`estimate_hours`/`actual_hours` 服务层 ≥0 校验（`MaxTaskHours=1e6`）；空=未填报、由执行者回填（`complete` 只自动写 `actual_end`/进度，不推算工时）；新增只读汇总 `GET /projects/:id/hours-summary`（口径=全部未删任务求和，不做状态过滤），供概览与 PE-24 报表复用 | 07 §4.2-B3「先任务级实际」；口径固定可对账；不可见统一 404（同上 §3/§5） |
| D-36 | **worklog 评估结论：P2 不建 worklog/timesheet 实体**。任务级 `actual_hours`（+`actual_start`/`actual_end`）已满足"落库 + 可聚合 + 可审计"；触发条件 = 出现**按人按日明细、对外结算或成本分摊**的真实场景时单独立项 `project_worklogs`（含按日颗粒度与审批口径）并同步 02/06 | 07 §2-9/B3 的"后评估"结论落地；避免过早引入 timesheet 填报/审批/结算链路（同上 §3/§7） |
| D-37 | **周容量两级配置**：`projects.default_weekly_capacity_hours`（项目默认，NULL=内置 40h）+ `project_members.weekly_capacity_hours`（成员覆盖，NULL=继承项目默认）；写入走独立端点 `PUT /projects/:id/capacity`（缺省=不修改、null/0=清空；成员须存活、重复/越界 400；`capacity_update` 审计带 old/new）；**容量不入基线快照**（组织配置面，非计划数据） | 07 §2-10/B4「周容量可配置」；零新表；两级覆盖满足兼职/半天真实场景（[plan/p2-resource-heatmap-design.md](./plan/p2-resource-heatmap-design.md) §2/§3） |
| D-38 | **热图口径**：ISO 周对齐（周一~周日；start 缺省=本周并 snap 到周一）；`plannedHours`=任务估算在 `[plan_start, plan_end]` **自然日均摊**后按周切片；`remainingHours`=非终态 `max(estimate−actual,0)` 在 `[max(start,today), max(end,today)]` 均摊（**逾期落本周**，完成/取消归零）；行=存活成员 ∪ 窗口内负责人（离册标 `removed`）；`calendar.mode=natural_days` 随响应/UI 显式标注 | 07 §4.2-B4（估算/剩余 vs 容量）与 B7（v1 自然日 + 明确标注）；复用 PE-22 工时字段，口径单一来源（同上 §2/§5） |
| D-39 | **热图实时计算不落库**（无快照表）；PE-24 资源利用率复用本切片同一口径（planned/capacity） | 避免双写漂移；报表与热图对账天然一致（同上 §2/§7） |
| D-40 | **组合视图与列表口径同源**：`GET /projects/portfolio` 与 `GET /projects` 共用同一 `ListFilter` 与服务层可见性收窄路径（非管理员=参与项目），聚合在同一批行上完成；**不给组合视图增加列表没有的过滤维度**（健康度等只作聚合列），保证「同 params → 组合行 == 列表行」等式成立；实时派生不落库；聚合取数上限 2000 / 行上限 500（`truncated=true` 不静默），只读不写审计 | 07 §P2 验收「项目组合报表数据与列表口径一致（对账用例）」；从结构上消除双谓词漂移（[plan/p2-portfolio-metrics-design.md](./plan/p2-portfolio-metrics-design.md) §2/§4.1） |
| D-41 | **四项度量口径（全周期累计 + 可选时间窗）**：① 按时交付率=已完成 ∧ `actual_end≤end_date` / 已完成 ∧ `end_date` 非空（`end_date` 空单列 `noPlanDate`，不并入分母）；② 里程碑达成率=reached / 已判定（reached ∪ missed ∪ 逾期 planned，未来 planned 不判定）；③ 阶段门一次通过率=`passed ∧ reject_count=0` / passed；④ 资源利用率=Σplanned/Σcapacity（PE-23 同函数）。`from/to` 口径日期：交付=`actual_end`、里程碑=`reached_at`/`due_date`、门=`decided_at`；**分母 0 → `rate=null`**（不臆造 0%）；明细 200/组且分子分母恒为全量（同上 §2/§5） |
| D-42 | **阶段门 `reject_count` 列**（`project_gates`，int 默认 0）：reject 裁决在 CAS 同一条 UPDATE 内原子 +1；作为"一次通过率"的**唯一事实来源**（`version` 会被门禁阻塞/重复提交污染，审计表受保留策略影响，均不适合作业务度量源）；历史行默认 0=未被打回（不追溯制造证据）；**无新表 → RLS/清单无变更**（同上 §2/§3） |
| D-43 | **前端落点：独立组合视图页 `/projects/portfolio`**（`project:read` 登记；报表中心与项目列表页互链）：过滤条（与列表同参数）+ 4 张度量卡片（rate=null 显示「—」）+ 五维分布 Tab + 组合项目表 + 4 组明细 Tab（直接支撑抽样对账）；资源口径标注复用响应 `calendar.note`（单一来源） | 03 §2.13（复用既有报表/图表组件、独立视图）；页面即"可对账的报表"（同上 §6） |
| D-44 | **AI 端口 + 降级矩阵**：`handlers/project` 定义窄端口 `ProjectAIPort`（`Summarize`/`AssessRisks`/`ExtractTasks`），bootstrap 注入 LLM Gateway 适配器；**AI 未装配/调用失败/解析失败一律 200**（不用 5xx）——摘要→事实派生文本（`deterministic`）、预警→规则信号（`rules`）、纪要→空提案（`none`）；`degradeReason ∈ ai_unavailable\|llm_error\|parse_error` 受控词表；适配器经 `WithProviderRequest` 绑租户/个人解析链 | 06 §4.3「AI 输出…可降级，失败不影响主流程」；与 PE-21 端口模式一致（[plan/p2-ai-design.md](./plan/p2-ai-design.md) §3） |
| D-45 | **风险预警 = 规则信号（确定性） + AI 判断（增强）**：六类规则 `overdue_project\|overdue_task\|blocked_task\|overdue_milestone\|high_risk\|gate_rejected`（口径复用任务/里程碑逾期与风险/门既有语义），每类明细前 5 条 + `signalTotal`/`truncated` 不静默；AI 段 ≤10 条、severity 归一（未知→warning）、空标题丢弃 | 规则信号保证"AI 不可用也有预警"；AI 只增强不替代（同上 §3/§5） |
| D-46 | **纪要转任务只出建议（不落库、不自动创建）**：`content` ≤20000 字符、`limit` 2..20 收敛；输出 `name/description/priority/estimateHours/ownerHint`，priority 收敛 `low\|medium\|high\|critical`、工时仅取正数、`ownerHint` 不自动绑定；纪要为**不可信数据块**（system 明确只抽取不执行指令）；用户勾选后走既有 `POST /projects/:id/tasks`（`project:write`）逐条创建 | AI 结果不进主流程事务；注入防护与权限语义不变（同上 §3/§5） |
| D-47 | **AI 审计口径**：每次生成写项目 auditlog `resource=project`，action `ai_summary_generate\|ai_risk_warnings\|ai_minutes_extract`（记录 generatedBy/degraded/degradeReason/model/长度/条数，**不落纪要原文与 AI 原文**；降级同样留痕；2s 超时失败不阻断）；LLM 调用由网关 observer 落 `ai_llm_calls` 遥测 | 06 §4.3「AI 输出有审计」双通道；沿用 PE-22/23 审计风格（同上 §6） |
| D-48 | **无数据层变更**：三端点不落 AI 结果快照/不新增表列 → migrations / RLS 015 / `rls-apply` 清单零变更；AI 输入全部复用 PE-17/21/24 同源事实（不新增手工填报） | 避免"AI 结果"与事实双源漂移（同上 §3/§7） |
| D-49 | **采购/资产登记 = 新表 `project_asset_links`**（一项目多行）：`link_type ∈ asset\|asset_license\|vendor\|contract` + 同租户弱引用 `target_id` + `note` ≤512 + 软删；`(tenant_id, project_id, link_type, target_id)` **未删行唯一**（partial index，软删后允许重登记）；不落 `project_documents`（其语义是交付物索引） | 采购登记 =「项目 → 既有台账」多对多链接；软删保留撤销留痕（[plan/p2-procurement-design.md](./plan/p2-procurement-design.md) §3） |
| D-50 | **引用完整性 = 弱引用 + 同租户存在性校验**（PE-21 `related_ticket_id` 先例）：写入校验目标存在且 `tenant_id` 相同（400 `invalid_target`），不建外键；读时批量富化标签，目标缺失标 `targetMissing=true` 不阻断读；**不做跨租户引用**（MSP `msp_contract` 不在目标内） | 单租户原则 + 源域生命周期独立（同上 §2/§3） |
| D-51 | **门禁语义不变**：结项清单项 `assets` 仍以「已确认（confirm）或已豁免（waive）」判定；链接是登记**明细**（响应 `assetsLinks`/`assetsLinkSummary`，清单项 detail 携带登记条数），不是新门禁条件 | 06 §4.3 验收原文是「可链接」；P1 门禁已验收不回退、存量项目不受升级影响（同上 §2） |
| D-52 | **候选选择器统一入口**：`GET /projects/:id/asset-link-candidates`（`project:read`，可见性同详情 404）：`type` 四词表必填、`q` 关键词（`ContainsFold`）、`limit` 默认 20/上限 50；返回**最小字段** `{id,label,sublabel}`（不回传价格/密钥/联系方式/合同金额），各域强制 `tenant_id` 过滤 | 统一选择器（`contract` 无 HTTP 面、`vendor` 前端无页面）；字段最小化控制跨域读暴露面（同上 §2/§4） |
| D-53 | **写权限与终态**：新增/删除 = `project:write` + 可见性谓词；**cancelled 拒绝写（400）**；`completed` 允许补登记（采购/发票后到的真实场景，与 PE-15 结项动作同口径）；审计 `asset_link_create\|asset_link_delete`（记录 linkType/targetId/label/note 长度，不落敏感字段） | 结项动作一致口径 + 审计留痕替代事后禁止（同上 §2/§5） |

## 6. 开放问题（待确认）

| 编号 | 问题 | 影响 |
|---|---|---|
| Q-01 | 任务依赖/甘特范围 | ✅ 已裁决：依赖 v1 仅 FS 进 P1；甘特可选后置（D-14） |
| Q-02 | 工时/预算/成本范围 | ✅ 已裁决：预算轻量 P2；任务级实际工时先行（D-08/D-09） |
| Q-03 | 成员"即将过期"提示与外部标签筛选 | ✅ 已裁决：候选返回 `expiresAt` + 前端 ≤7 天预警；外部标签按 `source` 过滤（契约 §1-14） |
| Q-04 | 业务单据挂项目方式 | ✅ 已裁决：单据表加可空 `project_id` + 索引（契约 §1-1） |
| Q-05 | 完整性审查 7 项范围决策 | ✅ 已裁决并回写（[07 §4.2 B1–B7](./07-completeness-review.md)） |
| Q-06 | 外部干系人只读面（客户/供应商看项目进度） | ⏳ 后置：与 [05 MSP 交付](./05-msp-delivery.md) 一并定义（不做跨租户引用） |

## 7. 适用场景与前置条件

- 适用：IT 内部项目（系统上线/搬迁/整改）、服务改进项目（CSI）、（后置）MSP 客户交付项目；
- 前置：Go/Ent/Postgres 后端、Vite/React 前端、RBAC 与多租户机制按本仓现状；阅读本文档集不需要环境即可，实施阶段需要本地开发栈（见 [README 快速开始](../../README.md#快速开始)）。

## 8. 验证方式（文档集自身）

- 链接有效性：`bash scripts/docs-gate/check-broken-links.sh`；
- 决策落地时按 [文档治理规则](../documentation-governance.md) 同步 `CHANGELOG.md`、`ROADMAP.md`、README 成熟度表；
- 代码事实变更（如 `project.go` 字段调整）时必须回改本文件集 §4 基线表。