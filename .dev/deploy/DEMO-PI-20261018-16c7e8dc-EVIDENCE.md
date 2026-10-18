# 问题调查修复 + tenant_guard 豁免证据（238 demo · 2026-10-18）

> 主题：`http://172.18.3.238:8089` 问题详情「启动 RCA 无响应 / 创建调查失败」修复，以及随后的
> `tenant_guard` 豁免治理跟进。部署版本：`1.8.0-dev.16c7e8dc`（修复）→ `1.8.0-dev.fbb8fb0e`（治理跟进）。
> 范围：仅 demo 栈（itsm_demo / 8089）；prod 栈（8088）未触碰。

## 1. 用户症状与根因

症状（demo `/problems/9`）：

- 点击「启动 RCA」无反应（既不创建调查也不跳转）；
- 「创建调查失败」「创建根因分析失败」「创建解决方案失败」。

根因（三处叠加，全部验证过）：

| # | 根因 | 证据 |
|---|---|---|
| 1 | `itsm_demo` 缺 4 张表：`problem_investigations` / `problem_investigation_steps` / `problem_root_cause_analyses` / `problem_solutions`。`service/problem_investigation_repository.go` 全部 raw SQL，而迁移体系从未下发该 DDL（`20261030` 之前无任何迁移包含这 4 张表） | 接口报 `42P01 relation does not exist` |
| 2 | `dto/problem_investigation_dto.go` 的 `analystId`（RCA）/`proposedBy`（解决方案）标了 `binding:"required"`，但前端从来不传这两个字段（handler 已按会话用户兜底） → 请求恒定 400 | `{"error":"请求参数错误"}` + DTO 字段 |
| 3 | 前端 `handleStartRCA` 只做前端 Tab 切换，落到「请先开始调查」死路；不发起创建 | `ProblemDetail.tsx` 旧实现 |

设计说明：这 4 张表是**问题子表**（无 `tenant_id`），仓库层所有读写都经
`JOIN problems p ... WHERE p.tenant_id = $` / `IN (SELECT id FROM problems WHERE tenant_id=$)`
强制租户过滤（见仓库文件头部注释），与 `problem_changes`/`problem_incidents` 同一豁免模式。

## 2. 变更清单

### 2.1 迁移（commit `395f13dd`）

- 新增 `itsm-backend/migrations/20261030_problem_investigation_tables.sql`（幂等 DO 块，4 表 + 唯一索引
  `ux_problem_investigations_problem`）与 `_down.sql`（对应回滚）；
- 已对 `itsm_demo` 应用（`schema_migrations` 账本行按幂等策略留给下一次全量 init 记录）。

### 2.2 后端 DTO（commit `16c7e8dc`）

- `analystId` / `proposedBy` 去掉 `binding:"required"` + 注释说明（handler 缺省为会话用户）；
- `gofmt -w` 复核。

### 2.3 前端（commit `16c7e8dc`）

- `ProblemDetail.tsx` `handleStartRCA` 改为异步：GET summary → 无调查且 `canWriteProblem`（`problem:write`）
  → `ProblemInvestigationAPI.createInvestigation({problemId})` + `message.success(t('problemDetail.startRcaCreated'))`
  → `loadData()`；失败 → 错误 toast + 回退概览内页；只读用户 → 概览；按钮 `loading={startingRca}`；
- `translations.ts` 新增 `problemDetail.startRcaCreated`（zh：`调查已创建，已进入根因分析` / en：`Investigation created, entering root cause analysis`）。

## 3. 部署记录

### 3.1 修复部署（`1.8.0-dev.16c7e8dc`）

- 远端 `/home/n9e/itsm/repo`：bundle `d80cf753..16c7e8dc` → 分支 `deploy-demo-pi-16c7e8dc`，`.env.demo VERSION=1.8.0-dev.16c7e8dc`；
- 构建：`docker compose --env-file .env.prod -f docker-compose.prod.yml build itsm-backend itsm-frontend`（BUILD_EXIT=0）；
- 上线：`up -d itsm-backend-demo itsm-worker-demo itsm-frontend-demo` → 三容器 healthy；root=200 / health=200；`/api/v1/version` = `1.8.0-dev.16c7e8dc`。

### 3.2 治理跟进部署（`1.8.0-dev.fbb8fb0e`）

- bundle `16c7e8dc..fbb8fb0e` → 分支 `deploy-demo-tg-fbb8fb0e`，`.env.demo VERSION=1.8.0-dev.fbb8fb0e`；
- 仅重建 backend（frontend 内容未变：复用 `itsm-frontend:1.8.0-dev.16c7e8dc` 打新 tag，避免 tag 漂移）；BUILD_EXIT=0；
- 上线：`up -d itsm-backend-demo itsm-worker-demo itsm-frontend-demo` → 三容器 healthy；root=200 / health=200；`/api/v1/version` = `1.8.0-dev.fbb8fb0e`。

## 4. 验证证据（问题调查修复 · 16c7e8dc）

- **前端镜像 A/B**：`startRcaCreated` 命中 old(`d80cf753`)=0 → new=2（产物路径 `/usr/share/nginx/html/static/`）；中文文案命中 new=1。
- **API（`.tmp/pi_verify.py`，problem 9 / problem 10 两轮）**：`create_rca_no_analyst_id` ok（rca id 1 / 2，`analystId` 缺省 → 1）、`create_solution_no_proposed_by` ok（solution id 1 / 2，`proposedBy` 缺省 → 1）、summary 回读含 rca + solution。
- **UI（Tabbit 实弹）**：
  - problem 9（已有调查）：点击「启动 RCA」→ 外层 `问题调查` + 内层 `根因分析` active，`notices: []`，仅 GET（无 POST），面板展示既有 RCA（5-whys / 高 / 系统管理员）；
  - problem 10（`open`，无调查）：点击「启动 RCA」→ `POST /api/v1/problem-investigation/investigations` 200 建调查 id 2（investigatorId 1），toast「调查已创建，已进入根因分析」，`/problems/10` 重取，Tab 正确，面板空态「开始分析 / 暂无根因分析」；0 console error / 0 failed request。

## 5. 治理跟进：`tenant_guard` 豁免登记（fbb8fb0e）

### 5.1 修复前（`1.8.0-dev.16c7e8dc` 启动日志，12:33:10）

```
warn tenant_guard: 5 table(s) without tenant_id and not in exempt list:
  problem_investigation_steps, problem_investigations,
  problem_root_cause_analyses, problem_solutions, user_pir_reviews
  action: set ITSM_TENANT_GUARD_POLICY=fatal or register exemption
```

即：新迁移的 4 张调查表 + 历史遗留 `user_pir_reviews`（`migrations/20260523_create_change_pir.sql`，
User↔ChangePIR 纯关联表）未登记豁免；一旦 `ENV=production` 或显式
`ITSM_TENANT_GUARD_POLICY=fatal`，生产将**拒绝启动**。

### 5.2 变更内容（commit `fbb8fb0e`）

- `internal/schema/tenant_guard.go` `TenantExemptTables` 新增 5 条（scope 全部 `derived`）：
  - `problem_investigations` / `problem_investigation_steps` / `problem_root_cause_analyses` /
    `problem_solutions`：owner=`problem`，reason 指向「仓库层 JOIN problems 强制租户过滤」；
  - `user_pir_reviews`：owner=`change`，reason 指向「租户维度由父表 `change_pi_rs` 承载（RLS 009 已纳管父表）」；
- 新增单测 `TestTenantExemptTables_DerivedChildTables`（5 表 owner/scope/reason 引用/复核日期全覆盖）；
- 文档：`docs/multi-tenant/01-architecture.md §7` 豁免清单同步；修正 `tenant_guard.go` 头部指向
  已不存在路径 `docs/architecture/tenant-isolation.md` 的注释（改指 §7 实际章节）。

### 5.3 验证

- 本地：`gofmt -l` 空；`go test -p 1 -count=1 ./internal/schema/` → `ok ... 0.614s`（含新增用例）。
- 部署后启动日志（backend 12:47:01 / worker 12:47:03）：
  ```
  info tenant_guard: exempt review status healthy
  info tenant_guard: pass {"scanned_tables": 206, "exempt_entries": 36}
  info tenant_guard: consistency pass {"checks": 4}
  ```
  violations 告警消失（豁免条目 31 → 36）。
- 镜像 A/B（`itsm-backend` 二进制字符串）：`强制租户过滤` old(`16c7e8dc`)=0 → new=3。
- 复跑 `.tmp/pi_verify.py`（problem 9）：`login / summary_before / update_investigation_summary /
  summary_final` 全 ok —— 运行链路无回退。
- 重启后 `ERROR|FATAL|PANIC` 级日志计数 = 0（其余 6 条为既有 warn：RAG embedder 未配置、BPMN 模板门禁）。
- **prod 未触碰**：prod 栈仍 `1.8.0-dev.586bb07a`（Up 21h healthy）、8088 root=200。

## 6. 观察点（本次未改）

- `ResolvePolicy` 读 `os.Getenv("ENV")`，但 prod 容器只注入 `SERVER_ENV=production`
  （`printenv ENV` 为空）→ 生产当前实际策略为 warn，`fatal` 未生效。是否统一变量名或显式
  设 `ITSM_TENANT_GUARD_POLICY=fatal` 属运维决策（本次不擅自改变 prod 启动门禁）。
- `user_pir_reviews` 为历史遗留表（20260523 迁移、无应用层读写引用）；change 域若重构 PIR
  关系可回访本豁免。

## 7. 回滚

- 代码：部署上一 tag `1.8.0-dev.16c7e8dc` / `1.8.0-dev.d80cf753`，或 revert `fbb8fb0e`、`16c7e8dc`；
- DDL：执行 `itsm-backend/migrations/20261030_problem_investigation_tables_down.sql`；
- 注意：本次是「补缺失能力」，回滚后 RCA/解决方案创建将回到 500/400（表不存在）。

## 8. 结论

demo 8089 问题调查全链路可用（API + 浏览器双证：自动建调查 → 根因分析 → 解决方案），
启动租户门禁由 5 条违规转为 pass（exempt_entries=36）；prod 栈未受影响。
