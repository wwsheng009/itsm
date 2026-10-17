# 🛣️ ITSM Roadmap

> **Source of truth for what is shipping, what is shipping next, and what
> is parked.** Updated as part of every release. Last synced: 2026-10-17.
>
> Cross-references:
> - PRD library: [docs/prd/](./docs/prd)
> - v1.0 GA readiness: [docs/v1-ga-readiness.md](./docs/v1-ga-readiness.md)
> - Architecture: [docs/architecture/](./docs/architecture)
> - Open issues & milestones: GitHub [Issues](https://github.com/heidsoft/itsm/issues) and [Projects](https://github.com/heidsoft/itsm/projects)

---

## 🎯 North Star

**Become the de-facto open-source AI-Native ITSM for enterprises that need
ServiceNow-class workflows without the lock-in or the footprint.**

Concretely that means:
1. **Process completeness** across the ITIL core, measured by executable business journeys rather than menu count.
2. **AI that earns its seat** — classification, summarization, RAG, and
   impact analysis that are measurable, not vibes.
3. **Native integration surface** — Feishu / DingTalk / WeCom / Webhook
   ship as first-class connectors, not bolt-ons.
4. **Operational discipline** — coverage, observability, security, and
   release hygiene as defaults, not afterthoughts.

---

## 📅 Release Timeline

| Version | Target | Theme | Status |
|:---|:---|:---|:---|
| **v1.0 GA** | 2026-Q2 | ITIL core + AI-Native scaffolding + private deploy | ✅ Shipped |
| **v1.6.x**   | 2026-Q3 | TicketType platform + reliability + RBAC/tenant hardening | 🟡 In progress |
| **v1.7**     | 2026-Q4 | Connector productionization + AI evaluator + business E2E | 🟢 Planned |
| **v2.0**     | 2027-Q2 | Coverage 70% + AI auto-triage GA + MSP billing + multi-region | 🔵 Roadmap |
| **v3.0**     | 2027-Q4 | Self-hostable AI inference + Plugin marketplace v2 + agent ecosystem | ⚪ Parked |

---

## 🟢 v1.0 GA — Shipped (2026-Q2)

**Theme:** Get the foundation right.

### Capability

- [x] **ITIL core flows** — ticket / incident / problem / change / release / service request
- [x] **Service catalog** — request templates, approval routing, SLA binding
- [x] **BPMN workflow engine** — process definitions, instances, user tasks,
      variable persistence, candidateGroups-driven approval (replaces the
      old dual-track approval system)
- [x] **CMDB v1** — CI types, configurations items, relationships, impact
      analysis, cloud discovery scaffold
- [x] **Knowledge base** — articles, versioning, RAG retrieval
- [x] **SLA** — multi-level policies, escalation matrix, alert rules
- [x] **AI capabilities (scaffold)** — Guidance-Harness-Skill framework,
      LLM Gateway, Triage / Summarize / KB skills
- [x] **RBAC + multi-tenant** — roles, permissions, menu gating, MSP mode
- [x] **Deployment** — Docker Compose (private / saas / saas_msp), GHCR
      images, multi-platform Release zip

### Quality

- [x] GA gate (4 checks): backend tests, frontend build, compose health,
      E2E smoke (11 core APIs)
- [x] Staticcheck + gofumpt + ESLint + tsc
- [x] Dependabot weekly scans
- [x] Security policy + Code of Conduct

### 后续持续治理项

- 🟡 关键业务旅程的服务层、集成与 E2E 覆盖继续提升。
- ✅ 超大 Controller 按现有领域边界渐进拆分——**已完成**：`controller/` 已清空（仅余空目录），新代码统一进入 `handlers/<domain>`；拆分过程中同步完成 `router.go` 按域拆分与六域接口化样板。
- 🟡 连接器从生命周期框架推进到真实渠道生产验收——钉钉 / 企微入站已落地，待真实渠道联调与飞书补齐。

---

## 🟡 v1.6.x — In Progress (2026-Q3)

**Theme:** Cover the seams and harden the foundation.

### 已落地

- [x] **TicketType 平台化** — 类型持久化、动态字段、创建快照、Preset Library、归档恢复和管理 UI。
- [x] **统一绑定解析** — Ticket 创建从已解析 TicketType 执行 Workflow、SLA 与 Assignment。
- [x] **权限与审计** — TicketType 独立管理/归档/Preset 安装权限，ACL manifest 覆盖；Preset 安装、归档恢复和绑定变更独立审计。
- [x] **可靠异步执行** — 工单与事件的流程启动进入持久化 command/outbox；关键通知具备 outbox、租约、重试和死信基础。
- [x] **租户与输入防线** — 覆盖跨租户、禁用类型、非法动态字段与非法绑定引用的回归测试。
- [x] **发布与安全加固** — HttpOnly cookie、初始化 migration ledger、PostgreSQL RLS、Endpoint ACL、依赖与运行时安全基线。
- [x] **分层迁移收尾** — legacy controller 全部退役，新代码统一进入 `handlers/<domain>` 垂直分层；swagger 路由冲突、菜单/认证契约断裂等收敛问题清零。
- [x] **状态机与错误语义加固** — 变更状态推进 CAS 并发防护；问题/事件状态机违规返回 409 业务语义而非 500；`super_admin` 通配权限链路（登录 / `/auth/me` / 前端判定）对齐。
- [x] **可靠执行补强** — commandbus 对聚合已删除的命令立即死信；审批链收口 SQL 缺列修复；业务流程回归套件（63 项集成 + 27 项生命周期深度）全绿。
- [x] **开源体验补强** — `make dev-seed-demo` 一键演示数据集（事件/问题/变更/知识库，幂等），README 快速开始接入；产品定位明示 **v1.6.x 界面中文优先**，完整界面 i18n 规划至 v1.7。
- [x] **行级权限守卫（写路径对称）** — incident 生命周期（ack/resolve/close/reopen/escalate）、ticket 四操作、change 非审批路径、release 写路径统一补 owner/role 守卫；四域 Update/Delete 接入 DataScope 校验；RBAC ResourceActionMap 补齐 releases 显式映射（`controller/` 已清空，仅余空目录）。
- [x] **错误语义与可观测性统一** — 行级拒绝 403 被 handler 兜底吞成 500 的根因修复，四域错误映射统一走 `common.RespondError`；AI 持久化失败经 zap 与计数器暴露，静默失败不再不可观测。
- [x] **租户隔离与上下文防线** — 租户/用户上下文缺失统一为 401（`TenantIDOrUnauthorized`）；change / cloud / BPMN 等域 30+ 处无保护类型断言与 `tenantID=0` fail-open 跨租户风险清零；`tenant_id` 豁免单一源 + 启动扫表 guard 接入长驻进程与 cmd/cmdb 路径。
- [x] **迁移与升级安全** — `migrations/` 目录即真相（discovery + 重写）、migration ledger 调和、PII 脱敏注解与 `migration-lint` CLI、升级前 preflight 与发布证据；索引缺口 batch2 补齐及 adoption 日期边界缺陷修复。
- [x] **连接器入站能力** — 钉钉 / 企微入站回调、持久化入站去重（`connector_inbound_dedups`）、连接器健康度与凭据轮换。
- [x] **工作流引擎加固** — 出边 fallback 声明、ServiceTask metaData 寻址、handler 双键注册、内置模板 16/16 lint 零错误、`workflowDefinitionKey` 全链路透传与 lint 门禁加固；工作流模板管理与 BPMN 前端集成。
- [x] **CMDB AI-Native P0/P1** — 关系词表（13 种关系单一源）、本体端点、`ci_number` 全局唯一序列；List/Search 合并、AI 工具与影响解释；CMDB 前端完成 React Query 迁移。
- [x] **架构收敛与可维护性** — `router.go` 巨石按域拆分为独立 routes 文件；user / tenant / rbac / notification / application / cloud 六域接口化并补冒烟测试（作为 58 域迁移样板）；包名与目录名统一、分层守卫增加包名≠目录名检查；双 BPMN 引擎死代码清理。
- [x] **授权平面收敛（批次 1–5）** — A 类越权写收口（bpmn/流程触发等 39+ 条写路由补挂权限门，任务面 `task:*` 与流程面 `bpmn:*` 分权）；B 类权限码词表统一（95 种未定义码收敛到既有码空间）；C 类预检映射全量对齐（112 处声明/预检错配清零）；批次 5 治本：**路由声明成为权限单一真源**——预检映射路由条目由 `cmd/authz-gen` 从声明 AST 生成（698 条），族级回退策略显式化（120 条），4 道守卫（写路由必挂门 / 声明码⊆码空间 / 声明-预检对齐 / 生成物新鲜度）构成防漂移闭环；admin/technician DBOnly 空集修复 + seeder 同义动作奇偶补齐。
- [x] **性能与运维** — 变更列表与 RAG 向量检索两处热路径 N+1 修复；prod 备份自动化（`scripts/prod-backup.sh` + 恢复演练 + launchd 每日调度）、compose 项目名隔离（`itsm-prod` / `itsm`）与诊断端口参数化。
- [x] **项目管理 P0** — 项目主数据升级（状态机/类型/健康度/进度/团队归口/软删）+ `project_members`/`project_tasks` + 五类单据 `project_id`；vertical slice API（自动编号/乐观锁/结项与删除门禁/成员候选（活跃 membership 派生标签）/任务 WBS ≤3 层/五域挂接与聚合读/任务→工单生成）；可见性谓词（管理角色 ∨ 项目成员）；前端列表 + 工作台（概览/任务/关联单据/成员）与一级菜单入口。验证：后端 `handlers/project` 测试（含跨租户隔离）与路由/预检/菜单守卫全绿；前端 type-check、目标单测、路由权限守卫与生产构建全绿。**环境项已闭环**：本机真实 PostgreSQL（embedded-postgres）完成迁移 + RLS 001–015 `-verify` + 结构/行为探针 6/6 + HTTP E2E 13/13 + 浏览器 UI 冒烟 8/8（证据见 `docs/project-management/06-implementation-roadmap.md`）。

### 当前收敛项

> 2026-09-12 复核：以下各项均按“已完成部分 / 剩余缺口”标注，避免把部分进展误读为闭环。

- [ ] **业务旅程 E2E** — 固化 TicketType 安装与绑定、工单创建、Workflow 实例/任务/历史、SLA、Assignment、审计的完整断言。
  - 已完成：服务层 TicketType 安装到审计六环节全链路断言；工作流节点 e2e spec 入库。
  - 剩余：跨服务 E2E 固化与 CI 常驻。
- [ ] **可靠执行统一** — 将剩余 ITIL 域从非可靠触发路径迁移到 command/outbox，并提供积压、重放和死信运维。
  - 已完成：incident 告警与 provisioning 履约接入 outbox；运维命令批量 replay/cancel 与命令类型汇总。
  - 剩余：其余 ITIL 域迁移。
- [ ] **生产数据升级门禁** — 对每次 Schema 变化执行脱敏 PostgreSQL 副本迁移、兼容、回滚与耗时验证。
  - 已完成：migration ledger 调和、preflight、`migration-lint` 与发布证据。
  - 剩余：脱敏副本实跑与回滚演练常态化。
- [ ] **Connector marketplace 生产化** — Feishu、DingTalk、WeCom、Webhook 的真实渠道健康检查、验签、重放与密钥治理。
  - 已完成：钉钉 / 企微入站回调、持久化去重、健康度与凭据轮换。
  - 剩余：真实渠道联调验收；飞书渠道补齐。
- [ ] **AI Audit/Evaluator** — 对建议保留接受/拒绝反馈，并形成可重复的质量基线。
  - 已完成：AI 审计上报链路打通；评测集去占位。
  - 剩余：接受/拒绝反馈闭环与 CI 质量基线门禁。
- [ ] **CMDB 数据治理** — 发现 Job、Diff、调和、退役、质量指标与规模测试。
  - 已完成：AI-Native P0/P1（见上）。
  - 剩余：数据治理本身尚未启动。
- [ ] **用户侧 Bot 能力落地（B0–B4）** — Bot 工作区、工具治理、运行档案与运行维度看板（方案：`docs/plan/ai-bot-capability-landing-implementation-plan-2026-09-27.md`）。
  - 已完成：B0 工具元数据与一次迁移（`integration_verified`）；B1 运行档案/确认五态/持久队列/SSE v2（`flow_verified`）；B2 模板与授权治理（`flow_verified`）；B3 三入口与场景 pilot S1/S2/S3（`flow_verified`，条件达标）；B4-01 E2E 双通道章程与 run-summary（`flow_verified`）、B4-02 运行维度指标与看板（`integration_verified`）。
  - 剩余：B4 `accepted` 评审签署（产品 + 测试）；browser E2E 首轮 CI 校准（摘 `continue-on-error`）；token 计量接线；S3 `create_kb_draft` 写工具与产物读取端点。

- [x] **项目管理 P1** — 交付治理（模板/阶段门/基线/里程碑与依赖/结项清单/搜索/归口/状态报告）——**已收口**。
  - 已完成：数据层六表（`project_gates`/`project_closures` 已入）与迁移兜底；里程碑与依赖（v1 仅 FS + 全项目环检测）API；计划基线不可变快照（冻结/版本/偏差）API；项目模板（CRUD/一键生成幂等，CAS 认领 + 单事务）；**阶段门（四道门门禁规则 + `process_binding business_type=project` + 内置立项/结项审批模板 + 降级自动通过 + 审计；`activate`/`complete` 状态机强制点；计划门通过自动冻结基线 v1）**；**结项清单 + CSAT（自动项实时派生 + 复盘 KB 草稿（结项门提交自动生成、不自动发布）/资产登记确认或豁免/成员回收（保留 PM 与干系人）/CSAT 尽力触发；`complete` 依序校验业务阻塞 → 结项门 → 清单齐备；`survey_responses.project_id` 补齐）**；前端工作台「里程碑/计划基线/**阶段门/结项清单**」Tab 与任务「依赖」弹窗、「项目模板」管理页与工作台「应用模板」入口（`project-service` 单测 31/31、`check-api-paths` 1052/1052、生产构建通过）；**搜索与待办（全局搜索纳入 `project`/`project_task` 且强制可见性谓词；新增 `GET /projects/my-milestones`，`/dashboard`「我的项目任务」卡片并行聚合未终结任务与 30 天内到期里程碑，前端 40/40 单测）**；**里程碑到期通知（租户级扫描器 `planned` 且逾期/窗口内 → 入箱 `notification.deliver`（outbox）+ 幂等键收敛 + PM/干系人收件（fail-closed）+ `project_milestone_due` 双词表 + `source=job` 审计；`service` 单测 4 + 投递 2 用例）**；**归口与状态报告（归口写入校验 + 读取软删降级 + 报表分组 + 归口团队负责人并入里程碑通知候选（D-24：不扩大成员可见性）；`project_report_templates` 模板 + `POST /projects/:id/reports/generate` 实时派生 Markdown/JSON（D-25：不落快照）；前端「报表中心」页 + 工作台「状态报告」Tab；`handlers/project` 5 用例，ACL 927 路由，前端 47/47 单测）**。
  - P1 收口已完成（真实库：ent 全量迁移 + 六份兜底 SQL + RLS 001–015 `-verify` + 结构/行为探针 8/8 + HTTP E2E 35/35 + 里程碑通知链路 smoke + 浏览器 UI 冒烟 11/11；含生产构建 JSX 运行时修复）。后续增量（甘特/资源热图/风险台账/AI 周报等）见 P2。

- [x] **项目管理 P2** — 经营化与 AI（风险台账/预算与工时/资源热图/报表度量/AI/采购登记）——**已收口**。
  - 已完成：**PE-21 风险/问题台账（`project_risks`：kind=risk|issue、等级/概率/影响、负责人、状态机（close 必填结论）、软删 + 乐观锁；API 7 条）+ 转换链路（端口 + 适配器复用问题/变更域服务，未装配 503；CAS 回写关联；同目标二次 409 + `existingId`；审计 `risk_convert` 等 5 类）+ 前端工作台「风险/问题」Tab（筛选/新建/编辑/缓解/关闭/重开/转问题·变更/删除）；`handlers/project` 3 用例，ACL 934 路由，前端 44/44 单测，`go build -p 2 ./...` + `npm run build` 通过）**。
  - 已完成：**PE-22 预算与工时（`projects.budget_amount`/`budget_currency` 两列 + 成对写入（只给其一 400）+ `budget_update` 审计；任务 `estimate_hours`/`actual_hours` ≥0 校验 + `GET /projects/:id/hours-summary` 汇总（全部未删任务口径）；基线快照 `schemaVersion 1→2` 冻结预算 + diff `budget{...}`（v1 容忍）；worklog 评估结论 = P2 不建实体（D-36）；前端列表「预算」列 + 创建/编辑弹窗预算字段 + 概览预算/工时卡片 + 任务表单实际工时回填与「工时（估/实）」列；`handlers/project` 4 用例，ACL 935 路由，前端 49/49 单测，`go build -p 2 ./...` + `npm run build` 通过）**。
  - 已完成：**PE-23 资源负荷（周容量两级配置 `projects.default_weekly_capacity_hours`/`project_members.weekly_capacity_hours`（NULL=继承/回内置 40）+ `PUT /projects/:id/capacity`（缺省=不修改，null/0=清空；成员须存活；`capacity_update` 审计 old/new）+ `GET /projects/:id/resource-heatmap`（ISO 周对齐、weeks 1..12；计划=估算按计划窗口自然日均摊、剩余=estimate−actual（逾期落本周、完成归零）；行=存活成员 ∪ 窗口内负责人（离册标 removed）；v1 自然日口径随响应/UI 显式标注；实时计算不落库）+ 前端工作台「资源负荷」Tab（周数切换 + 配色热图 + 未排期提示 + 容量设置弹窗）；`handlers/project` 3 用例，ACL 937 路由，前端 58/58 单测，`go build -p 2 ./...` + `npm run build` 通过）**。
  - 已完成：**PE-24 组合视图与度量（`project_gates.reject_count`（reject CAS 内原子 +1）+ `GET /projects/portfolio`（五维聚合 + 与列表同谓词/同可见性/同行序项目行；上限与 `truncated` 显式）+ `GET /projects/metrics`（按时交付率/里程碑达成率/阶段门一次通过率/资源利用率：分子分母 + 明细 200/组（对账勾稽字段）+ `rate=null` 语义 + `from/to` 口径日期 + 资源复用 PE-23 热图函数）+ 前端独立页 `/projects/portfolio`（度量卡片 + 分布 Tab + 组合项目表 + 4 组明细 Tab；报表中心互链）；`handlers/project` 5 用例（含组合 vs 列表、资源 vs 热图两组对账），ACL 939 路由，前端 62 单测，`check-api-paths` 1067/1067，Swagger 重生成对齐注解）**。
  - 已完成：**PE-25 AI 能力（周报摘要/风险预警/纪要转任务：三端点 `/projects/:id/ai/{summary,risk-warnings,minutes/tasks}`（`project:read`，不落库）+ **降级一律 200**（摘要→事实派生 `deterministic`、预警→规则信号 `rules`（六类、每类前 5 条 + `signalTotal`/`truncated`）、纪要→空提案 `none`；`degradeReason` 受控词表）+ 提示注入防护（纪要=不可信数据块）与词表收敛（priority 仅取任务词表、工时仅正数）+ 审计三 action + `ai_llm_calls` 遥测 + 前端工作台「AI 助手」Tab（勾选建议批量建任务）；`handlers/project` 5 用例（含「降级后仍可建任务/读列表」=主流程不受影响），ACL 942 路由，前端 58/58 单测，`check-api-paths` 1070/1070，Swagger 重生成对齐注解**。
  - 已完成：**PE-26 采购/资产结项登记（`project_asset_links`：`link_type=asset|asset_license|vendor|contract` + 同租户弱引用 `target_id`（跨租户/不存在 400）+ 备注 + 软删 + 未删行唯一 partial index + 迁移/RLS 015/rls-apply 清单同步）+ 4 端点（`GET|POST /projects/:id/asset-links`、`DELETE .../asset-links/:linkId`、`GET .../asset-link-candidates`（最小字段候选：编号/名称/标题 + 辅标签，四域 tenant 过滤））+ 结项清单 `assetsLinks`/`assetsLinkSummary` 明细（目标缺失标 `targetMissing`；门禁语义不变=确认/豁免）+ 审计 `asset_link_create|asset_link_delete` + 前端结项清单「采购/资产登记明细」区与「添加登记」弹窗（候选 300ms 防抖远程搜索）；`handlers/project` 4 用例（含跨租户拒绝、零登记确认仍通过、completed 补登记、cancelled 拒绝写），ACL 946 路由，前端 62/62 单测，`check-api-paths` 1070/1070，Swagger 重生成对齐注解**。
  - P2 收口已完成（真实库：ent 全量迁移 + P2 兜底 SQL 五份 + RLS 001–015 全量应用与 `-verify` + P2 结构/行为探针 **11/11** + HTTP E2E **38/38** + 浏览器 UI 冒烟 **15/15**，覆盖 PE-21~PE-26 主链路；harness 见 `.tmp/pgverify`，证据见 `docs/project-management/06-implementation-roadmap.md`「P2 收口已完成」）。

### 发布门禁

- [x] 后端全量测试、静态分析、前端类型检查/构建、API 契约和 Endpoint ACL 均有自动化入口。
- [ ] 每个候选版本保留 Git SHA、镜像 digest、数据库版本、迁移结果、E2E 与恢复演练证据。
  - 已完成：迁移 preflight 与发布证据、prod 备份与恢复演练脚本具备。
  - 剩余：固化为每个候选版本的门禁清单，并留存执行产物。
- [ ] 生产放行继续按部署环境验收，不能由仓库中的历史“全部通过”报告替代。
  - 补充：仓库内历史测试/部署报告一律标注 `Status: superseded`（见 `docs/documentation-governance.md`），只作证据索引，不作放行依据。

---

## 🟢 v1.7 — Planned (2026-Q4)

**Theme:** AI earns its seat, integrations go live.

### Engineering

- [ ] **AI Evaluator v1** — classification accuracy ≥85%, summarization
      ROUGE ≥0.6, RAG hit-rate ≥70%. Regression suite in CI.
- [ ] **AI telemetry** — capture prompt/response/cost/latency for every
      skill invocation; dashboard at `/api/v1/ai/audit`.
- [ ] **Knowledge base RAG v2** — chunking strategy improvements,
      re-ranking, hybrid search (BM25 + vector).
- [ ] **Skill registry v1** — declarative skill manifests, hot-pluggable
      pipeline, registry UI.

### Product

- [ ] **Feishu / DingTalk / WeCom native connectors** — end-to-end:
      account / approval / IM notification / webhook relay.
- [ ] **Auto-triage (human-in-the-loop)** — AI suggests category,
      assignee, SLA tier; engineer accepts with one click.
- [ ] **SLA forecast skill** — predict SLA breach risk per ticket,
      surface on dashboards.
- [ ] **Full UI i18n** — the UI is Chinese-first through v1.6.x; extract
      the remaining hardcoded strings (currently ~83% of pages) into
      `src/lib/i18n` message catalogs and ship an en-US locale with a
      per-user language switch.

### Quality

- [ ] **Backend coverage** 40% → **55%** overall.
- [ ] **Performance budgets** — k6 baselines for top 10 endpoints,
      enforced in CI.
- [ ] **Trivy + govulncheck** — daily scans, high-severity blockers.

---

## 🔵 v2.0 — Roadmap (2027-Q2)

**Theme:** MSP-friendly, AI-assisted, multi-region.

### Engineering

- [ ] **Coverage 55% → 70%**.
- [ ] **Service decomposition** — split monolithic `itsm-backend` into
      `core` + `workflow` + `ai` + `cmdb` services along bounded contexts.
- [ ] **Event-driven architecture** — Watermill is already in deps;
      promote to first-class pub/sub for incident events.
- [ ] **Multi-region active-active** — Redis Streams + region-aware
      routing.

### Product

- [ ] **MSP billing** — usage metering, invoicing, allocation reports.
- [ ] **AI auto-triage (full)** — replaces the human-in-the-loop step
      from v1.7 with confidence-based auto-accept.
- [ ] **Impact analysis skill** — given a change, predict affected CIs,
      tickets, and downstream SLAs.
- [ ] **Plugin marketplace v2** — signed plugins, sandboxed execution,
      revenue share for authors.

### Quality

- [ ] **SOC 2 Type II readiness** — control mapping, evidence collection,
      audit-ready logging.
- [ ] **Customer-managed keys (BYOK)** for LLM Gateway.

---

## ⚪ v3.0 — Parked (2027-Q4)

**Theme:** Self-hostable AI, agent ecosystem.

- Self-hostable LLM inference (Ollama, vLLM, llama.cpp) — drop the
  external OpenAI dependency for privacy-sensitive deployments.
- Agent marketplace — third-party agents that can act on the ITSM
  data model under strict RBAC.
- Mobile PWA with offline-first ticket intake.
- Multilingual UI (zh-CN baseline; en-US, ja-JP, ko-KR planned).

---

## 🛠️ Always-On Tracks

These don't belong to a single release; they ship incrementally:

### Testing & Quality

- Incremental coverage gate (60% on new code) — landed
- End-to-end smoke on every PR — landed v1.0
- Frontend visual regression — planned v1.7
- Property-based tests for critical parsers (BPMN XML, RAG chunking)
  — planned v1.7

### Security

- CodeQL + Trivy + govulncheck — landed
- Quarterly threat-model review
- Annual pen-test

### Open-Source Governance

- Issue triage SLA (48h first response, 14d close-or-fix) — ongoing governance target
- Monthly community digest
- Quarterly maintainer rotation review

### Developer Experience

- `make dev-*` unified dev environment (already landed v1.0)
- `itsm-cli` for ops (deploy/seed/inspect) — landed v1.0
- `itsm-skill` for OpenClaw / Codex agents — landed v1.0
- Container image size reduction (distroless base) — planned v1.7

---

## 📊 Key Metrics

We track these on every release. Numbers below are post-v1.0 GA baseline
and the **target** for the next major release.

| Metric | Historical v1.0 baseline | v1.7 target | v2.0 target |
|:---|---:|---:|---:|
| Backend coverage | ~2% | 55% | 70% |
| Frontend coverage | ~10% (UI only) | 30% | 60% |
| E2E smoke coverage | 11 APIs | 25 APIs | 50 APIs |
| Mean PR → first review | TBD | < 48h | < 24h |
| Mean issue → first response | TBD | < 48h | < 24h |
| AI triage accuracy | — | 85% | 92% |
| Open stale issues | varies | < 30 | < 15 |

> **2026-09-12 实测基线**（防止与上表历史口径混淆）：`go test -cover ./handlers/...` 为 **26.2%**。
> 该口径仅覆盖 `handlers/` 垂直分层包；历史 v1.0 基线的 ~2% 为 `service` + `controller` 口径，
> 两者不可直接比较。覆盖率数据应由 CI 生成并写入，避免手抄导致漂移。

---

## 🤝 How to Influence the Roadmap

1. **File an issue** with the `feature-request` template and link to
   the milestone you think it belongs in.
2. **Vote** on issues with 👍 — we sort milestone backlogs by reactions.
3. **Propose a major change** via the RFC process:
   `docs/rfcs/0000-template.md`.
4. **Pick up a "good first issue"** — every track has at least one.

---

## 📜 Changelog

Major releases are tracked in [CHANGELOG.md](./CHANGELOG.md) and via
GitHub [Releases](https://github.com/heidsoft/itsm/releases).
