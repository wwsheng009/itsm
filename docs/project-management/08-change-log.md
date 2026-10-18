# 项目管理 · 变更记录（2026-10-18）

> 状态：已实施并部署到 238 demo
> 当前 demo 版本：`1.8.0-dev.f2e32b4e`
> 范围：项目进度汇总、MSP 顶栏 403、里程碑/任务/项目日期编辑预填
> 说明：本文件是上述变更的**文档化变更记录**；不再在 `.dev/deploy/` 新增证据文件。

## 1. 项目进度自动汇总（`7c1389e1`）

### 背景

全场景 E2E 结项后项目 #4 的 `progress` 仍为 `0%`，与 `02-data-model.md`「P1 由任务汇总」不一致。

### 修复

- `projects.progress` 改由任务派生，任务创建/更新/流转/软删在**同一事务**内重算。
- 口径：
  - 仅统计未软删、非 `cancelled` 的**叶子任务**；
  - 父任务不计入（由子任务汇总，避免模板阶段行稀释）；
  - `done` 恒按 100 计，其余取任务 `progress`；
  - 叶子任务算术平均并四舍五入；无有效叶子任务为 0；
  - 派生更新**不递增 `projects.version`**，不干扰用户编辑的乐观锁。
- 项目 `complete` 时强制进度为 100%。
- 存量回填迁移：[`20261029_project_management_progress_backfill.sql`](../../itsm-backend/migrations/20261029_project_management_progress_backfill.sql)。
- 模板应用在生成任务后同一事务内重算。

### 验证

- `go test ./handlers/project/ -count=1` 通过（新增叶子任务汇总/取消/软删/父任务不计入/版本不变用例 + 结项 100% 断言）。
- `go build -p 2 ./...`、`go vet ./handlers/project/` 通过。
- 迁移在真实 PostgreSQL 事务内 dry-run 通过；init 应用后项目 #4 `progress 0 → 100`。
- 真实 API（项目 #3）：创建任务 0 → 任务 `progress=100` 后项目 **100** → 软删任务回 **0**。
- 浏览器（Tabbit，`admin`）：项目详情/列表进度条 `aria-valuenow=100`；0 console error / 0 failed request。

## 2. MSP 顶栏 403（`957a08cf`）

### 背景

项目页顶栏对 `admin` 请求 `/api/v1/msp/customers` 与 `/api/v1/msp/workbench/summary` 返回 403「仅MSP员工可访问」。根因是 `admin` 所在租户为 `msp_provider` 且持 `*` 通配权限，但 `msp_role` 为空；前端仅按权限码/租户类型判断，误渲染了 `CustomerFilter`。

### 修复

- `canUseMspWorkbench` 与 `MspProviderGate` 统一为三重判定：
  1. 当前租户为 `msp_provider` / `msp`；
  2. 用户持有服务商员工角色（`provider_*` / 历史 `msp_viewer|tech|specialist|manager|admin`）；
  3. 持有 `msp_ticket:read`。
- 平台超级管理员无 `msp_role` 时不再渲染客户过滤器，也不再请求 MSP 跨租户接口。
- 合法服务商员工（如 `demo.msp.manager`）仍显示「全部客户」。

### 验证

- `tsc --noEmit`、改动文件 ESLint、`npm run build` 通过。
- `route-permissions` 单测 **13/13** 通过。
- 浏览器（Tabbit，`admin`）：项目页/列表 0 console error / 0 failed request，客户过滤器消失。
- 浏览器（Tabbit，`demo.msp.manager`）：`/msp/workbench` 正常，「全部客户」保留。
- 镜像 A/B：前端静态资源含 `provider_tenant_admin` 的文件数 `0 → 1`；后端二进制含 `recalcProjectProgressTx` 符号数 `0 → 6`。

## 3. 日期编辑预填崩溃（`3eb8bcc5` + `f2e32b4e`）

### 现象与根因

- 入口：`/projects/4` → 「里程碑」→「编辑」。
- 修复前：页面进入 ErrorBoundary「页面出现错误」。
- 控制台错误：
  ```
  TypeError: t.isValid is not a function
  ErrorBoundary caught an error: TypeError: t.isValid is not a function
  ```
- 根因：`ProjectMilestone.dueDate` 是 API 字符串，直接写入 Ant Design `DatePicker`；组件调用 `value.isValid()` 时崩溃。
- 同类隐患：任务 `planStart/planEnd`、项目列表编辑计划起止 `RangePicker`。

### 修复

- 新增 [`date-value.ts`](../../itsm-frontend/src/lib/utils/date-value.ts)：
  - `dateValue(apiString)`：API 字符串 → Dayjs；空值/非法值 → `undefined`；
  - `isoValue(dayjs)`：Dayjs → 项目域 date-only ISO（UTC 00:00）；空值/非法值 → `undefined`。
- 接入里程碑 `dueDate`、任务 `planStart/planEnd`、项目编辑 `range`。
- 项目域日期按 **UTC 日历日** 预填，与列表 `slice(0,10)` 口径一致，避免 `2026-11-14T16:00Z` 在 CST 下显示为 11-15。
- 提交时显式转 ISO，避免依赖 JSON 对 Dayjs 的隐式序列化。

### 验证

| 检查 | 结果 |
|---|---|
| `tsc --noEmit` | 通过 |
| `date-value` 单测 | **4/4**（含 UTC 跨时区用例） |
| 改动文件 ESLint | 通过 |
| 前端生产构建 | `BUILD_FRONTEND_EXIT=0` |
| 修复前 Tabbit 复现 | ErrorBoundary + `t.isValid is not a function` |
| 修复后里程碑编辑 | 弹窗正常打开，计划日期 `2026-11-14`，保存成功 |
| console error / pageerror / failed response | **0 / 0 / 0** |
| 数据库复核 | `project_milestones.due_date = 2026-11-14 00:00:00+00`（date-only 归一） |

浏览器截图：

- 里程碑编辑：`C:\Users\vince\AppData\Local\Tabbit Browser\User Data\LocalAgent\Runtime\artifacts\task-3119dfd8-3207-4e9b-9087-25ce334b318b\screenshot-shot-4a148569-70d4-4f3a-a825-69105c5790c5.png`

## 4. 部署与验证汇总

| 项 | 值 |
|---|---|
| demo 版本 | `1.8.0-dev.f2e32b4e` |
| 前端镜像 | `itsm-frontend:1.8.0-dev.f2e32b4e`（sha256 `95271510…`） |
| 后端镜像 | `itsm-backend:1.8.0-dev.f2e32b4e`（由 `3eb8bcc5` retag；后端源码未变） |
| init | `exited(0)`，run 45；迁移/播种完成 |
| 容器 | backend / worker / frontend healthy |
| 版本接口 | `GET /api/v1/version` → `1.8.0-dev.f2e32b4e` |

## 5. 备注与遗留

- 本地 `feat/msp-p0-hardening` 曾出现独立的 `fb01af39 feat(llm): 支持完整 Endpoint 路径配置` 提交（非本次变更）；部署分支未包含该提交，demo 未部署 LLM 变更。
- `/api/v1/msp/workbench/views` 在服务商侧返回 404「自定义视图未开启」，属既有 `WORKBENCH_VIEWS_ENABLED` feature flag 行为，与本次 403 修复无关。
- 本次变更的提交：
  - `7c1389e1` 进度汇总 + MSP 顶栏（后端/前端/迁移）
  - `957a08cf` MSP 顶栏要求服务商员工角色
  - `3eb8bcc5` 日期编辑预填崩溃修复
  - `81787af0` 日期预填统一按 UTC 日历日（主分支）
  - `f2e32b4e` 部署分支上的 UTC 日期修复（cherry-pick）
- 上述提交尚未 push 到 origin。
