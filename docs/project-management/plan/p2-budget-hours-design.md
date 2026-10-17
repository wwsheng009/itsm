# PE-22 预算与工时设计（D-33~D-36）

> 阶段：P2（经营化与 AI）· 工作项：PE-22「预算与工时」· 状态：**已实现 v1.0（2026-10-25）**
> 上游依据：[06 §2/§4.3](../06-implementation-roadmap.md)、[01 §3.2 能力矩阵](../01-module-blueprint.md)、[02 §3.2/§3.7](../02-data-model.md)、[07 §2/§4.2 B3](../07-completeness-review.md)。

## 1. 范围（路线图冻结口径）

| # | 内容 | 来源 |
|---|---|---|
| 1 | **轻量预算字段**（不做财务级核算） | 06 PE-22 / 07 §2-8 |
| 2 | **任务级 `actual_hours`**（先任务级实际） | 06 PE-22 / 07 §4.2-B3 |
| 3 | **worklog 评估**（结论 + 触发条件） | 06 PE-22 / 07 §2-9 |

非目标：成本/费用条目、财务核算、EVM、按人按日 timesheet（见 §3 D-36 触发条件）。

## 2. 现状盘点（实施前）

| 事实源 | 现状 |
|---|---|
| `projects` 表 | 无预算字段（`ent/schema/project.go`） |
| `project_tasks` 表 | `estimate_hours`/`actual_hours` 字段已存在（P0 schema 预留），create/update DTO 已透出，但**无 ≥0 校验、前端无实际工时录入、无任何汇总聚合** |
| `project_baselines.snapshot` | 冻结任务计划日期/里程碑/进度；**未含预算**（02 §3.7 要求含预算 → 本次补齐） |
| 审计 | 通用 `AuditMiddleware` 兜底 + 各子域显式审计先例（`risk_*`/`gate_*`/`closure_*`）；预算变更需显式 `budget_update` 留痕 |

## 3. 决策记录

| 编号 | 决策 | 理由/影响 |
|---|---|---|
| D-33 | 预算 = 项目主表两个可空字段：`budget_amount`（double precision，≥0）+ `budget_currency`（varchar(8)，可空=租户默认币种）。**不建预算/成本独立表**；写入复用 `POST /projects` 与 `PUT /projects/:id`，金额与币种**同写同清**（只给其一 → 400 `budget amount and currency must be provided together`）。变更经 `PUT` 落库后追加审计 `budget_update`（metadata 带 old/new），失败不阻断业务 | 轻量、零新表；币种沿用 `msp_contract.currency` 先例（可空 + MaxLen）；"轻量预算"留给报表与基线消费 |
| D-34 | **预算进基线快照**：`BaselineProjectSnapshot` 增 `budgetAmount`/`budgetCurrency`，`BaselineSnapshotSchemaVersion` 1→2；旧基线（v1）解码零值容忍、diff 中 `budget.frozen=false` 且不参与变更判定；新基线 diff 输出项目级预算偏差（`baseAmount`/`baseCurrency`/`currentAmount`/`currentCurrency`/`changed`） | 02 §3.7「冻结内容：…/预算/…」；不可变快照加字段必须走 schemaVersion，保证历史基线可读 |
| D-35 | **任务级实际工时**：`estimate_hours`/`actual_hours` 服务层校验 ≥0（创建 + 更新）；`actual_hours` 为空=未填报，由执行者随任务完成回填（`complete` 只自动写 `actual_end`/进度，**不自动推算工时**）；新增只读汇总端点 `GET /projects/:id/hours-summary`，口径 = 该项目**全部未删任务**求和（不做状态过滤），返回 `taskCount`/`estimatedTaskCount`/`estimateHours`/`actualHours`，供概览与 PE-24 报表复用 | 07 §4.2-B3「先任务级实际」；汇总口径固定可对账（PE-24 若需按状态收窄由报表侧二次过滤） |
| D-36 | **worklog 评估结论：P2 不建 worklog/timesheet 实体**。任务级 `actual_hours`（+`actual_start`/`actual_end`）已满足"实际工时落库 + 可聚合 + 可审计"；触发条件 = 出现**按人按日明细、对外结算或成本分摊**的真实场景时，单独立项 `project_worklogs`（含按日颗粒度与审批口径），并同步 02/06 | 07 §2-9/B3 的"后评估"结论落地；避免过早引入 timesheet 的填报/审批/结算链路 |

## 4. 数据层

- `ent/schema/project.go`：新增字段（生成后 `ent` 全量重生成）：
  - `budget_amount` float，Optional，Comment「项目预算金额（P2 轻量预算）」；
  - `budget_currency` string(8)，Optional，Comment「预算币种（P2；空=租户默认）」。
- 迁移兜底：`migrations/20261025_project_management_pe22.sql`（`ALTER TABLE projects ADD COLUMN IF NOT EXISTS ...`）+ `_down`（`DROP COLUMN IF EXISTS`）。
- 无新表 → RLS 015 覆盖清单 / `cmd/rls-apply` 受管清单 / `common/pagination.go` 别名**均无变更**。

## 5. API

| 方法 | 路径 | 变更 |
|---|---|---|
| POST | `/projects` | body 增 `budgetAmount`/`budgetCurrency`（同写同清校验） |
| PUT | `/projects/:id` | 同上；变更写审计 `budget_update`（resource=project） |
| GET | `/projects/:id`、列表 | 响应增 `budgetAmount`/`budgetCurrency`（omitempty） |
| GET | `/projects/:id/hours-summary` | **新增**（`project:read`）：`{taskCount, estimatedTaskCount, actualTaskCount, estimateHours, actualHours}` |

错误口径：金额 <0 或 >1e12 → 400（`ErrValidation`）；币种长度 >8 → 400；金额/币种只给其一 → 400。

## 6. 前端

- 项目创建/编辑 Modal：新增「预算金额」「币种」输入（同填同清提示）；
- 概览 Tab：预算展示（金额 + 币种，未设置显示占位）+ 工时汇总卡片（调 `hours-summary`：任务数/估算合计/实际合计/已填报数）；
- 任务编辑表单：新增「实际工时」输入（≥0）；任务表格新增「工时（估/实）」列。

## 7. 验证证据（实测回填）

| 项 | 命令/方式 | 结果 |
|---|---|---|
| 后端单测 | `go test ./handlers/project/ -run 'Budget|Hours|Baseline' -count=1` | ✅ **4 用例**（`TestBudgetWorkflow`/`TestTaskHoursSummary`/`TestBaselineBudgetSnapshot`/`TestHandlerBudgetAudit`：预算 CRUD 成对校验/清空/审计、工时汇总口径+软删排除+可见性、基线 v2 预算冻结与偏差+v1 容忍、HTTP 端点与 400 口径） |
| 基线回归 | `go test ./handlers/project/ -run Baseline -count=1` | ✅ 旧 v1 快照解码容忍 + 新 v2 预算冻结/偏差 |
| 全包回归 | `go test ./handlers/project/ ./middleware/ ./router/ ./migration/ -count=1` | ✅ 全绿（含 `TestPrecheckMapIsFresh` 重生成后） |
| 构建 | `go build -p 2 ./...` | ✅ exit 0 |
| 路由/ACL | `go run ./cmd/authz-gen` + `node scripts/generate-acl-manifest.js` | ✅ **935 路由**（+1 `hours-summary`）；权限覆盖 93.69% |
| 前端 | `npx tsc --noEmit` / 改动文件 eslint / `project-service` 单测 / `check-api-paths` / `npm run build` | ✅ 0 错 / 0 告警 / **49/49** / **1063/1063（0 missing）** / exit 0 |
| 迁移发现 | `go test ./migration/ -count=1` | ✅ 目录即真相自发现（含 pe22 双文件） |
| 文档 | `bash scripts/docs-gate/check-broken-links.sh` | ✅ `docs/project-management/**` 0 断链（仓库全局 88 条为 WSL/存量噪声，与本批无关） |
