# 238 demo 部署证据：项目进度自动汇总 + MSP 顶栏 403 修复（2026-10-18，1.8.0-dev.957a08cf）

## 变更内容

- 代码提交：
  - `7c1389e1` fix(project): 自动汇总项目进度并修复 MSP 顶栏 403（后端 + 前端 + 迁移）
  - `957a08cf` fix(msp): 顶栏过滤器要求服务商员工角色（首轮部署后浏览器复核发现 `admin` 在 provider 租户仍持 `*` 但 `msp_role` 为空，补充角色判定）
- 交付镜像：
  - `itsm-backend:1.8.0-dev.957a08cf`（由 `1.8.0-dev.7c1389e1` retag；两提交间后端源码无差异；sha256 `692825e5…`）
  - `itsm-frontend:1.8.0-dev.957a08cf`（sha256 `061c8d3e…`）
- 数据库：`migrations/20261029_project_management_progress_backfill.sql` 随 init 容器应用；存量项目 #4 `progress 0 → 100`。

## 部署路径

| 步骤 | 内容 | 结果 |
|---|---|---|
| bundle | `7c1389e1..957a08cf`（基线=远端 7c1389e1） | fetch/checkout OK（HEAD=957a08cf，分支 `deploy-demo-progressfix`） |
| A | checkout + `.env.demo VERSION=1.8.0-dev.957a08cf` | `A-STATUS: DONE`，`LLM_TIMEOUT=5` 保持 |
| A2 | retag backend → 后台构建 frontend | `retag-backend-ok`、`BUILD_FRONTEND_EXIT=0` |
| C | `up -d init/backend/worker/frontend` | init `exited|0`（run 43）；backend/worker/frontend healthy；root=200、health=200 |
| version | `GET /api/v1/version` | `1.8.0-dev.957a08cf` |

## 验证证据

### 1. 存量回填（真实 PostgreSQL，事务内 dry-run + 正式应用）

| 检查 | 结果 |
|---|---|
| dry-run（`BEGIN; migration; ROLLBACK`） | `UPDATE 1` / `UPDATE 0` / `ROLLBACK`，项目 #4 保持 0（dry-run 不落库） |
| init 正式应用后 | `SELECT id,tenant_id,code,status,progress` → `4\|1\|PRJ-20261018-7226\|completed\|100` |

### 2. 运行时任务写操作重算（真实 API，项目 #3）

| 步骤 | 项目 progress |
|---|---|
| 前置 | 0 |
| 创建临时任务 | 0 |
| 任务 `progress=100`（PUT） | **100** |
| 软删该任务 | **0** |

脚本：`.tmp/verify-progress-live.js`（`pass=true`）。

### 3. 浏览器（Tabbit，`admin` / 默认 provider 租户 1）

- 项目详情 `http://172.18.3.238:8089/projects/4`：
  - API `GET /api/v1/projects/4` → 200，`progress=100`、`status=completed`；
  - 概览进度条 `role=progressbar aria-valuenow=100`；
  - 顶栏 **不再渲染「全部客户」**；
  - **0 console error、0 failed response、0 pageerror**；
  - 无 `/api/v1/msp/customers`、`/api/v1/msp/workbench/summary` 请求。
- 项目列表：行 `PRJ-20261018-7226` 进度条 `aria-valuenow=100`；0 console error / 0 failed response。
- 截图：
  - 详情：`C:\Users\vince\AppData\Local\Tabbit Browser\User Data\LocalAgent\Runtime\artifacts\task-8721cd1f-d63b-4e28-8d19-056bafc3b2f2\screenshot-shot-a96f3772-2606-42b9-b32f-0002fc30010e.png`
  - 列表：`...\screenshot-shot-3746b77d-24c5-436e-a8db-8319b49aaaca.png`

### 4. 服务商侧回归（Tabbit，`demo.msp.manager`）

- `/msp/workbench` 正常打开，顶栏「全部客户」**保留**（合法 provider 员工不被误伤）。
- 观察项（与本修复无关）：`GET /api/v1/msp/workbench/views` 返回 404「自定义视图未开启（WORKBENCH_VIEWS_ENABLED）」，为既有 feature flag 行为。
- 截图：`...\screenshot-shot-a2042e1a-0fc6-44ab-98e2-3db997e88854.png`

### 5. 镜像内容 A/B（证明新代码确实进镜像）

| 检查 | 旧 `e536c6c8` | 新 `957a08cf` |
|---|---|---|
| 前端静态资源含 `provider_tenant_admin` 的文件数 | 0 | **1** |
| 后端二进制含 `recalcProjectProgressTx` 符号数 | 0 | **6** |

## 复跑方式

```bash
NEW=1.8.0-dev.957a08cf BUNDLE=/tmp/itsm-progressfix-957a08cf.bundle bash /tmp/deploy238_progressfix_a.sh
NEW=1.8.0-dev.957a08cf BASE=1.8.0-dev.7c1389e1 bash /tmp/deploy238_progressfix_a2b.sh   # sudo
NEW=1.8.0-dev.957a08cf bash /tmp/deploy238_progressfix_w2.sh                              # sudo（轮询）
NEW=1.8.0-dev.957a08cf bash /tmp/deploy238_progressfix_c.sh                               # sudo
node .tmp/verify-progress-live.js
```

## 遗留

- 前端 `MspProviderGate` 已与顶栏共用角色判定；其它直接按租户/权限码渲染的 MSP 入口若存在，建议按同一单源函数审计。
- `/msp/workbench/views` 的 404 属既有 `WORKBENCH_VIEWS_ENABLED` 未开启，建议前端在调用前按 capability 门控（单列跟进，不阻塞本次修复）。
