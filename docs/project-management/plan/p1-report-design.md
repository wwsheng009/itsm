# P1 · 归口与状态报告（PE-17）设计

> 状态：**已实现 v1.0**（2026-10-23）
> 上游：`01-module-blueprint.md` 能力矩阵「团队归口 / 状态报告」、`03-integration.md` §2.16、`06-implementation-roadmap.md` PE-17 与 §4.2 验收第 7 条
> 契约：`plan/p0-integration-contracts.md` §4（团队归口 / 状态报告）、§1-2（`owner_team_id` 单列归口）

---

## 1. 验收映射

| 验收点 | 落地 |
|---|---|
| 项目归口 team **生效** | `projects.owner_team_id` 写入校验（同租户、未软删、active）→ 400；读取富化 `ownerTeamName` + 软删降级 `ownerTeamDeleted`（列表/详情/创建/更新一致，批量查询防 N+1） |
| 归口参与**可见范围** | 归口参与列表过滤（`ownerTeamId`，P0 已有）与**报表分组**（`GET /projects/report-groups` 按归口团队聚合；可见性谓词与列表一致收窄）；**不扩大项目可见性**（见 D-24） |
| 归口参与**通知候选** | 里程碑到期扫描器收件人追加**归口团队负责人**（team 软删/停用自动降级不计入），见 `plan/p1-milestone-notification-design.md`（D-23 修订） |
| 状态报告**模板 + 周期**（数据驱动） | 新表 `project_report_templates`（name/cadence/sections/status/version/软删）+ CRUD API；仅 `active` 模板可生成 |
| 报告**数据派生**、先导出后 AI | `POST /projects/:id/reports/generate` 实时派生 Markdown + JSON（任务/里程碑/阶段门/基线偏差/归口），**不落报告快照**；AI 增强后置 P2 |

## 2. 决策

| 编号 | 决策 | 理由 |
|---|---|---|
| D-24 | **归口不扩大项目可见性**（保持 D-06：管理角色 ∨ 项目活跃成员）；归口用于列表过滤/展示、**报表分组**、**通知候选**（里程碑到期通知追加归口团队负责人）；团队软删 → 读取降级 `ownerTeamDeleted=true`（UI 提示重新归口）、写入拒绝（400） | 契约"归口参与可见范围默认值"若按"整队可见"实现会让整个团队读到项目明细（越权风险）；收窄为"归口作用于分组/通知/筛选"，可见性仍由成员关系权威控制（fail-closed） |
| D-25 | 状态报告 = **模板（周期 + 章节）+ 事实数据实时派生**，不落报告快照；先导出 Markdown/JSON，AI 摘要后置 P2；**与计划模板分表**（`project_report_templates` vs `project_templates`） | 契约"模板 + 周期由配置驱动，数据派生（§7），先导出后 AI"；避免报告快照与事实源双写漂移；计划模板生成计划、报告模板生成报告，语义不同不混表 |

## 3. 数据层（`project_report_templates`）

| 列 | 类型 | 说明 |
|---|---|---|
| `tenant_id` | BIGINT NOT NULL | 租户 |
| `name` | VARCHAR(128) NOT NULL | 模板名（租户内唯一，软删不释放） |
| `description` | TEXT NULL | 描述 |
| `cadence` | VARCHAR(16) NOT NULL DEFAULT 'weekly' | `weekly|biweekly|monthly` |
| `sections` | JSONB NOT NULL DEFAULT '[]' | `overview|tasks|milestones|gates|variance|team`（归一化去重保序） |
| `status` | VARCHAR(16) NOT NULL DEFAULT 'draft' | `draft|active|archived`（仅 active 可生成） |
| `version` | INT NOT NULL DEFAULT 1 | 乐观锁（PUT 带 version CAS） |
| `created_by` | BIGINT NULL | 创建人 |
| `created_at/updated_at/deleted_at` | TIMESTAMPTZ | 软删不释放名称 |

- 索引：`uq_project_report_templates_tenant_name`（唯一）、`idx_project_report_templates_tenant_status`；
- 迁移兜底：`migrations/20261023_project_management_pe17.sql`（+`_down`）；
- RLS：015 覆盖清单 + `cmd/rls-apply` 受管清单新增 `project_report_templates`；
- 分页别名：`projectReportTemplates`。

## 4. API（7 条，`project:read|write`）

| 方法 | 路径 | 权限 | 说明 |
|---|---|---|---|
| GET | `/api/v1/projects/report-templates` | `project:read` | 列表（`status/q` 过滤 + 分页；软删不可见） |
| POST | `/api/v1/projects/report-templates` | `project:write` | 创建（名称唯一 409；词表校验 400） |
| GET | `/api/v1/projects/report-templates/:templateId` | `project:read` | 详情（404 语义） |
| PUT | `/api/v1/projects/report-templates/:templateId` | `project:write` | 更新（version 乐观锁 409；status 显式切换） |
| DELETE | `/api/v1/projects/report-templates/:templateId` | `project:write` | 软删（名称不释放） |
| GET | `/api/v1/projects/report-groups` | `project:read` | 报表分组（按归口团队聚合项目数 + 健康度分布；含未归口行） |
| POST | `/api/v1/projects/:id/reports/generate` | `project:read` | 生成报告 `{markdown, data}`（`templateId` 缺省默认章节；归档模板 409） |

**审计**（`resource=project`）：`report_template_create|report_template_update|report_template_delete|report_generate`。

## 5. 归口集成点

1. **写入**：`Create`/`Update` 时 `ownerTeamId>0` → 团队须同租户且未软删/未停用，否则 `ErrOwnerTeamUnavailable`（400）；`ownerTeamId=0` 表示清空。
2. **读取**：`ToResponseWithOwnerTeams` / `ToResponseListWithOwnerTeams` 批量富化 `ownerTeamName`；团队缺失/软删 → `ownerTeamDeleted=true`（前端列表以 `团队 #ID` + Tag「已停用」降级提示重新归口）。**说明**：仓库全局软删读拦截器（`database/security.go`）对读透明过滤 `deleted_at` 行，故软删团队的**名称不可回显**（真实库 E2E 已确认该降级口径）；报表数据的归口块同样降级为「已删除团队」。
3. **通知候选**：里程碑到期扫描器 `resolveRecipients` = `manager_id` ∪ 项目内 `pm`/`stakeholder` 活跃成员 ∪ **归口团队负责人**（`teams.manager_id`，团队软删/停用不计入）。
4. **报表分组**：`GET /projects/report-groups` 聚合行含 `ownerTeamName/OwnerTeamDeleted` + 项目数与 `green/yellow/red` 分布；未归口单独一行（`ownerTeamName='未归口'`）。

## 6. 报告派生算法（§7 口径）

- **任务**：`total/done/cancelled/open(未终结)/overdue(未终结且 plan_end<now)/dueSoon(未终结且 3 天内到期)`；
- **里程碑**：`total/reached/planned/missed/overdue(planned 且已逾期)/onTime(reached 且 reached_at<=due_date)`；`rate=reached/total`、`overdueRate=overdue/total`；
- **阶段门**：四道门 `gate/status/updatedAt`；
- **偏差**：最新基线 vs 当前（复用 `BaselineDiff`：任务+里程碑偏差汇总；无基线省略该章节并输出"未冻结计划基线"）；
- **Markdown**：概览/任务/里程碑/阶段门/计划偏差/团队归口（仅输出模板选中章节；纯函数可单测）。

## 7. 前端

- 项目列表：新增「归口」列（团队名 + 软删 Tag）与「报表中心」入口；
- 新页 `/projects/reports`「报表中心」：归口分组表 + 报告模板管理（新建/编辑/归档/启用/删除；cadence/sections 可视化）；
- 项目工作台新增「状态报告」Tab：选择 active 模板 → 生成（Markdown 预览 + 统计摘要）→ 复制 / 下载 `.md`；
- 路由三处登记（`route-paths.ts`/`routes/index.tsx`/`route-permissions.ts`）。

## 8. 非目标（后续）

- AI 摘要/周报生成（LLM Gateway，P2）；
- 报告快照与投递（邮件/IM）排程；
- 归口 team 的成员级可见性扩展（D-24 明确不做）。

## 9. 验证证据

| 项 | 命令 / 证据 | 结果 |
|---|---|---|
| 归口治理 | `go test ./handlers/project/ -run TestOwnerTeamGovernance`（写入校验/软删降级/清空） | ✅ |
| 通知候选 | `go test ./service/ -run TestProjectMilestoneDueScanner`（含归口负责人候选 + 软删降级用例） | ✅ |
| 模板生命周期 | `go test ./handlers/project/ -run TestReportTemplateLifecycle`（词表/唯一/乐观锁/软删） | ✅ |
| 报告派生 | `go test ./handlers/project/ -run TestGenerateProjectReportDataDerived`（统计/偏差/章节裁剪/归档拒绝/可见性） | ✅ |
| 报表分组 | `go test ./handlers/project/ -run TestReportGroupsByOwnerTeam`（聚合/健康度/未归口/可见性收窄/软删） | ✅ |
| 端点 + 审计 | `go test ./handlers/project/ -run TestReportEndpoints`（HTTP 全链路 + 四类审计留痕） | ✅ |
| 全包回归 | `go test ./handlers/project/ -count=1`、`./router`、`./middleware -run Precheck`、`./migration`、`go build -p 2 ./...` | ✅ |
| 生成物 | `go run ./cmd/authz-gen` + `node scripts/generate-acl-manifest.js`（**927 路由**） | ✅ |
| 前端 | `tsc --noEmit`、改动文件 `eslint`、`project-service`/`route-permissions` 单测、`check-api-paths` | ✅ |
| 真实库（P1 收口） | embedded-postgres 55432：ent 全量迁移 + 六份兜底 SQL（含 `20261023_..._pe17.sql`）+ RLS 001–015 `-verify`；结构/行为探针 **8/8**（含 `project_report_templates` 策略 + `itsm_app` 跨租户写拒绝/读隔离） | ✅ |
| 真实库 HTTP E2E | 后端 + seed 演示数据：归口校验/富化/软删降级、模板 CRUD 与 409/CAS、报告生成（章节裁剪/默认章节/归档 409）、报表分组、成员/里程碑/my-milestones、搜索命中、依赖环 409、基线/阶段门/结项读、审计留痕 —— **35/35** | ✅ |
| 浏览器 UI 冒烟 | 生产构建 + preview：列表「归口」列、报表中心（归口分组/模板行）、工作台「状态报告」Tab 生成 Markdown、零运行时错误 —— **11/11** | ✅ |
