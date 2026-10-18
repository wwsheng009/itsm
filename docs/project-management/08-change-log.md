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

## 6. 单据挂接项目：创建时挂接 + 详情页「所属项目」（PE-5 增补）

> 状态：已实施、通过本地验证，并已部署 238 demo（`1.8.0-dev.cad4362e`，2026-10-18）。

### 背景

PE-5 此前仅落地「项目工作台查看/解除挂接」与「任务生成工单自动关联」；单据侧不感知项目：五域（工单/事件/问题/变更/服务请求）创建请求不接受 `projectId`，详情页无「所属项目」，也没有「把已有单据加入项目」的界面入口（`attachRecord` 全前端零调用）。

### 实现

- **创建时挂接（五域一致口径）**：五域创建请求 DTO 新增可选 `projectId`；前置校验收敛为单一端口 [`handlers/common/projectlink`](../../itsm-backend/handlers/common/projectlink/projectlink.go)（`GuardCreate` → 项目服务 `ValidateAttach`：`project:write` 动态复核 + 项目存在（租户内、可见）且非终态）；校验通过后 `project_id` **随 INSERT 一次写入**（避免「先建单再挂接」的部分成功）；未装配端口时携带 `projectId` 的创建请求 **fail-closed 503**，不静默丢弃关联意图。`bootstrap` 五域 `SetProjectLink(projectService)` 装配。
- **单据侧反向查询（新增 5 条端点）**：`GET /api/v1/{tickets|incidents|problems|changes|service-requests}/:id/project`（权限码各域 `read`），返回 `{project: {id,name,code,status} | null}`；项目可见性复用详情 `GetForViewer`（对查看者不可见按 `null` 返回，不泄露）。
- **前端**：新增共享组件 `ProjectSelect`（创建页选项目；仅列非终态项目；`project:read + project:write + 域写码` 门控）与 `RecordProjectLink`（详情页「所属项目」展示 + 加入/移出）；五域创建页/详情页全部接入；服务请求创建入口为服务目录申请页（`pages/(main)/service-catalog/request/$id`），`projectId` 经 `service-catalog-api` 映射层透传。
- **边界**：MSP 跨租户面保持只读，不暴露挂接写入口。

### 验证

| 检查 | 结果 |
|---|---|
| `go build -p 2 ./...` | `BUILD_EXIT=0` |
| `go test -p 1 -count=1 ./handlers/project/ ./middleware/ ./handlers/ticket/ ./handlers/incident/ ./handlers/problem/ ./handlers/change/` | 6 包全部 `ok`（project 含新增 3 用例：反查/附接语义/端点） |
| `handlers/service_request` 包测试 | 断言无失败；9 例在 Windows 下因 `TempDir` 清理报 `The process cannot access the file ... being used by another process`（SQLite 句柄回收的环境性问题）。基线复核：暂存本轮 4 个改动文件后同包跑出 **25 例同类清理失败**，确认与本轮改动无关 |
| 前端 `npx tsc --noEmit` | `TSC_EXIT=0` |
| 改动文件 ESLint（23 个前端文件） | 通过（0 error / 0 warning） |
| 相关前端单测（`npx jest`：project/incident/change/problem service + service-catalog-api/service-request-api） | **6 套 140/140 通过**（子集运行触发全局覆盖率阈值提示，属预期，非用例失败） |
| `node scripts/check-api-paths.js` | Matched **1075** / Missing **0** |
| `node scripts/generate-acl-manifest.js` | **948 → 953 条**（+5 条 `GET /{id}/project`），覆盖率 93.81% |
| `go run ./cmd/authz-gen` + `middleware` 预检新鲜度/路由对齐测试 | 通过 |
| Swagger（`make swagger-gen` 同口径） | `swag init` 重生成，仅新增（+115 行）：5 条端点摘要 + 五域创建请求 `projectId` 字段 + `project.RecordProjectResponse` 定义 |
| 238 demo 部署（`1.8.0-dev.cad4362e`） | backend/worker/frontend 三容器 healthy、init ExitCode=0、`/api/v1/version` 对齐；backend 二进制新校验文案 A/B **0→1**、frontend `所属项目` 产物 **1→13**；API E2E：携带 `projectId=3` 建单 `#65` → 反查 `project.id=3`，不存在项目 → **404 code=4004**。证据：[DEMO-PE5-20261018-cad4362e-EVIDENCE.md](../../.dev/deploy/DEMO-PE5-20261018-cad4362e-EVIDENCE.md) |

### 遗留

- （已收口，见下）BPMN 服务任务 `create_incident` 直插 ent，不支持 `projectId`。
- （已部署）本节变更已上线 238 demo（`1.8.0-dev.cad4362e`，2026-10-18）；浏览器级 UI 走查仍待补做。`RecordProjectLink` 的「加入/移出」复用既有 `POST|DELETE /.../:id/project`（已在 `handlers/project` 单测覆盖）。

### 遗留收口：BPMN 服务任务创建时挂接项目（2026-10-18 补丁）

**问题**：BPMN 服务任务 `create_incident` / `create_change` / `create_request`（`service/bpmn/*.go`）绕过 HTTP 创建路径直插 ent，既不读取流程变量 `projectId`，也不经 `projectlink` 端口，导致「流程创建的单据无法挂项目」且与五域口径不一致。核查中还发现 `create_incident` 的存量缺陷：未写必填的 `incident_number`、`type/priority/severity` 为空串时覆盖 schema 默认值并触发枚举校验失败（即该任务此前已无法成功建单）。

**实现**：

- 新增 `service/bpmn/project_link.go`：`ProjectIDFromVars`（`projectId` 优先，兼容 `project_id`；int/int64/float64/数字串均支持，非法值视为不挂接）+ `GuardProjectAttach`（复用 `handlers/common/projectlink.GuardCreate` 与项目服务 `ValidateAttach`，校验失败不落库）+ `resolveInitiatorRoles`（优先受信上下文 `BPMNUserIDContextKey`，回退流程变量 `initiator_id/created_by/reporter_id/requester_id`；主角色 + 同租户生效 m2m 角色，与 RBAC 中间件口径一致）。
- 三个处理器新增 `SetProjectLink(projectlink.Validator)`；`CallbackRegistry.SetProjectLink` 统一注入；`ProcessEngine` 接口与 `CustomProcessEngine` 暴露同名方法；`bootstrap` 在 `projectService` 构造后与 HTTP 五域一起装配（`processEngine.SetProjectLink(projectService)`）。
- `create_incident` 顺带修复存量缺陷：`incident_number` 显式变量优先、否则生成 `INC-YYYYMM-<crypto/rand 六位>` 兜底；`type/priority/severity` 空串按 schema 默认值（`incident/medium/medium`）落库。
- 语义与 HTTP 一致：未装配端口时携带 `projectId` 的创建任务 fail-closed；不携带 `projectId` 的存量流程不受影响（直通）。



**验证**：`go test -p 1 -count=1 ./service/bpmn/ ./handlers/bpmn/ ./handlers/project/` 全绿；`service/bpmn` 新增 `project_link_test.go` 12 用例（变量解析矩阵、端口未装配 fail-closed、不携带 projectId 直通、发起人角色解析含跨租户排除、三域 create_* 单次 INSERT 落 `project_id`、校验失败零落库）；`go test -run NONE ./service/ ./internal/bootstrap/` 编译通过；定向 `go build -p 1 ./service/bpmn/ ./service/ ./internal/bootstrap/ ./handlers/bpmn/` ✅；238 demo 已部署 `1.8.0-dev.cad4362e` 并完成 API E2E。

## 7. 修复：项目列表「激活」失败提示不可行动（2026-10-18）

### 现象

`http://172.18.3.238:8089/projects` 点击「激活」恒提示「操作失败，请刷新后重试」。

### 根因

- PE-12 状态机要求 `planning → active` 前**立项门（initiation）必须通过**；示例项目 #3 尚未提交立项门。
- 后端返回 `409 {"code":4090,"message":"阶段门门禁未通过","gate":"initiation"}`（注意实际文案为「阶段**门门**禁未通过」）。
- 列表页 `runTransition` 只透传含「未终结」的错误，其余统一替换为通用文案，真实的门禁原因与处理路径被吞掉（详情页原本已有较明确的提示）。

### 修复

- `itsm-frontend/src/pages/(main)/projects/index.tsx`：剥离 `[RID: ...]` 后识别阶段门禁错误，改为 `Modal.warning` 展示真实原因 + 指引（激活=立项门，结项=结项门），提供「去项目工作台」按钮直达 `/projects/:id`；「未终结 / 版本冲突 / 刷新」等错误直接透出后端消息；兼容「阶段门门禁未通过 / 阶段门禁未通过」两种写法。
- 提交：`3266f4e3`（提示 + 引导）、`ce56a999`（修正双「门」文案匹配）。

### 验证

| 检查 | 结果 |
|---|---|
| `npm run type-check` / 改动文件 `eslint` | ✅ |
| 238 demo `1.8.0-dev.ce56a999` | backend/worker/frontend healthy、init ExitCode=0、version 对齐 |
| Tabbit 真实浏览器 | 点击「激活」→ `modalCount=1`，文案「无法激活：立项门未通过 / 阶段门门禁未通过 / 请先在项目工作台「阶段门」提交并通过立项门（需项目经理任命 + 成员登记）。」 |
| 引导按钮 | 「去项目工作台」→ 跳转 `http://172.18.3.238:8089/projects/3` ✅ |
| 镜像 A/B | 新引导文案产物：旧 `cad4362e`=0 → 新 `ce56a999`=1 |

证据：[DEMO-PE5-GATEFIX-20261018-ce56a999-EVIDENCE.md](../../.dev/deploy/DEMO-PE5-GATEFIX-20261018-ce56a999-EVIDENCE.md)。

> 说明：本次只修失败提示与引导，不绕过 PE-12 门禁；项目仍需在工作台「阶段门」提交并通过立项门后才能激活。

## 8. 阶段门阻塞项下一步导航 + 详情页任命项目经理（2026-10-18）

### 背景

`/projects/3` 阶段门展示「未任命项目经理」「未登记项目成员/干系人」等阻塞项，但只有静态 Tag，用户不知道入口在哪里；且详情页此前无法任命项目经理（只能回列表页编辑弹窗）。

### 缺口与修复

| 缺口 | 修复 |
|---|---|
| 阻塞项纯静态、不可操作 | 每项右侧增加下一步入口：`manager_missing`→任命项目经理弹窗；`member_missing`→切「成员」Tab 并打开添加成员；`task/milestone_missing`→切 Tab 并打开新建；`task/milestone_open`→切 Tab；`record_open`→切「关联单据」 |
| 详情页无任命项目经理入口 | 新增「任命项目经理」弹窗（候选搜索 + `PUT /projects/:id` CAS）；概览「项目经理」为空时提供「未任命（去任命）」 |
| 阻塞文案重复 | `detail` 与 `type` 文案相同时不再重复拼接 |
| 失败提交的门显示 `提交：0001-01-01 00:00:00` | 零值时间不展示（`submittedAt`/`decidedAt`） |
| 只读用户可见写入口 | 任命/新建类入口仅 `canWrite` 展示；查看类导航保留 |
| 成员添加后门数据不刷新 | 添加成功同步 `fetchGates()`；阶段门提示「修复后在该门行重新提交」 |

提交：`bdbb015b`、`4590aed4`。

### 验证

| 检查 | 结果 |
|---|---|
| `npm run type-check` / 改动文件 `eslint` | ✅ |
| 238 demo `1.8.0-dev.4590aed4` | backend/worker/frontend healthy、init ExitCode=0、version 对齐 |
| Tabbit 真实浏览器 `/projects/3` | 4 门正常；立项门阻塞项无重复/无零值时间；按钮 `去任命项目经理 →`、`去添加成员 →`、`提交` |
| 任命入口 | 弹窗打开，标题「任命项目经理」 |
| 成员入口 | 自动切「成员」Tab（`aria-selected=true`）并打开「添加成员」弹窗 |

证据：[DEMO-PE5-GATENAV-20261018-4590aed4-EVIDENCE.md](../../.dev/deploy/DEMO-PE5-GATENAV-20261018-4590aed4-EVIDENCE.md)。

> 后续可选：把「任命项目经理 + 登记为成员」合并为一步（当前为两个独立动作，符合后端两个独立规则）。

## 9. 阶段门进入即实时预检（2026-10-18）

### 问题

此前阻塞项只在「提交」失败后落库并展示，用户必须先提交才知道缺什么。

### 实现

- **后端**：新增 `Service.PreviewGates`，对四道门复用 `evaluateGateRules`（与 `SubmitGate` 同源规则）做只读评估，返回 `readiness: [{gate, ready, blockers[]}]`；`GET /projects/:id/gates` 响应新增 `readiness`。预检不落库、不创建门记录、不触发审批；可见性沿用 `GetForViewer`。
- **前端**：`pending/rejected` 门展示实时预检（无需提交），`in_review/passed` 保留提交快照；预检通过时显示「条件已满足，可提交」；顶部预检标签区分「待补 N 项 / 已满足，可提交 / 审批中」；切回阶段门 Tab、任命经理、添加成员、提交门后自动刷新。
- 提交：`9a53da0a`、`945e5e4a`。

### 验证

| 检查 | 结果 |
|---|---|
| `go test -p 1 -count=1 ./handlers/project/` | 全绿（新增 `TestPreviewGates`：未提交即有阻塞项、修复后即时 ready、预检零落库） |
| 前端 `tsc --noEmit` / 改动文件 ESLint | ✅ |
| API（project #5，仅立项门有记录） | `plan` 返回 `gate_not_passed + task_missing + milestone_missing`；`acceptance/closure` 返回顺序前置门未通过 |
| Tabbit `/projects/5` 阶段门 | 计划门未提交即显示任务/里程碑阻塞项与「去新建」入口（未点击提交）；顶部标签「预检：立项门审批中」 |
| 238 demo `1.8.0-dev.945e5e4a` | 三容器 healthy、init ExitCode=0、version 对齐 |

证据：[DEMO-PE5-GATEPREVIEW-20261018-945e5e4a-EVIDENCE.md](../../.dev/deploy/DEMO-PE5-GATEPREVIEW-20261018-945e5e4a-EVIDENCE.md)。

> 后续可选：用预检结果驱动页头「激活」按钮的禁用/提示（当前仍由后端 409 + 前端引导兜底）。

## 10. 预检驱动激活/结项入口 + 阶段门深链（2026-10-18）

### 优化

- **顶部徽标**：详情页按目标门显示 `已通过 / 审批中 / 就绪 / 待补 N 项`（规划中看立项门，进行中/暂停看结项门）。
- **前置拦截**：预检已加载时，激活/结项前先判断门是否 `passed`；未提交/审批中/已拒绝直接切「阶段门」并给出对应提示，不再调用必然返回 409 的接口。预检有阻塞项时提示待补数量。
- **深链**：支持 `/projects/:id?tab=gates`；列表页激活失败弹窗按钮改为「去阶段门处理」，一键直达。
- **错误透出**：详情页转场失败展示后端真实消息（剥离 RID）；阶段门门禁错误自动切 Tab 并刷新预检。

提交：`12c2d7d2`。

### 验证

| 检查 | 结果 |
|---|---|
| `npm run type-check` / 改动文件 ESLint | ✅ |
| 238 demo `1.8.0-dev.12c2d7d2` | 三容器 healthy、init ExitCode=0、version 对齐 |
| Tabbit：`/projects/5?tab=gates` | 阶段门 Tab 直接选中 |
| Tabbit：概览点击「激活」（立项门 in_review） | 提示「立项门审批中，请等待通过后再激活」+ 自动切阶段门；未产生无意义请求 |
| Tabbit：列表页「激活」→「去阶段门处理」 | 跳转 `/projects/5?tab=gates` 且阶段门选中 |

证据：[DEMO-PE5-GATECTA-20261018-12c2d7d2-EVIDENCE.md](../../.dev/deploy/DEMO-PE5-GATECTA-20261018-12c2d7d2-EVIDENCE.md)。

## 11. 修复：项目 AI 助手恒降级（LLM 模型被静态配置覆盖，2026-10-18）

### 现象

`/projects/1` AI 助手三端点均降级为确定性输出；后端日志：
`403 The current model does not support Token Plan Person`。

### 根因

租户 DB provider 为千帆 `deepseek-v4.1-flash`（Token Plan Personal，连通性 ok），但 `ProjectAIAdapter` 把静态 env `LLM_MODEL=gpt-4o-mini` 传给 `LLMGateway.Chat`，`OpenAIProvider.Chat` 在 model 非空时覆盖自身配置 → 实际请求 `gpt-4o-mini` → 403。`bpmn_ai_generator_service.go` 已有「空模型委派 provider」的同类修复，项目 AI 遗漏。

### 修复

- `ProjectAIAdapter.chat` 改传空模型并使用 `ChatWithRequest`（租户/用户解析链）；不再覆盖 provider 模型。
- `ProviderResolution` 新增 `Model`（来自 slot）；适配器回带最近一次实际生效模型，响应 `model` 展示真实模型（静态回退仍用 `LLM_MODEL`）。
- `ai_llm_calls.model` 记录解析后的实际模型（非流式 + 流式）。
- 新增 `TestProjectAIAdapterDelegatesModelToProvider` 回归测试。

提交：`7b383714`、`d80cf753`。

### 验证

| 检查 | 结果 |
|---|---|
| `go test ./handlers/project/` | 全绿（含新增适配器测试） |
| `go test ./service/ -run "TestLLMGateway\|TestGateway"` | 全绿 |
| 238 demo `1.8.0-dev.d80cf753` | 三容器 healthy、version 对齐 |
| `POST /projects/1/ai/summary` | `ai` / `degraded=false` / `model=deepseek-v4.1-flash` |
| `POST /projects/1/ai/risk-warnings` | `ai` / `degraded=false` / warnings=6 |
| `POST /projects/1/ai/minutes/tasks` | `ai` / `degraded=false` / proposals=2 |
| `ai_llm_calls` | 新行 `model=deepseek-v4.1-flash success=t`；旧失败行为 `gpt-4o-mini` |

证据：[DEMO-PROJECT-AI-20261018-d80cf753-EVIDENCE.md](../../.dev/deploy/DEMO-PROJECT-AI-20261018-d80cf753-EVIDENCE.md)。

## 12. 修复：LLM 网关未绑定调用方走租户 provider 解析链（BE-8，2026-10-18）

**背景**：`/projects/1` AI 助手修复（§11）后复查「其它 AI 入口是否存在同类静态 provider 覆盖」，确认缺陷不止单一入口。

**根因（统一处理器级）**：`LLMGateway.resolveProvider` 的解析链（请求级 → 个人默认 → 租户默认 → 静态）只在 `ChatWithRequest`/`ChatStreamWithRequest`/`WithProviderRequest`（带 `boundRequest` 的副本路径）生效；`Chat`/`ChatStream`/`ChatStreamWithTools` 在未绑定请求时直接使用静态 `g.provider`。因此所有「拿基础网关直接调」的调用方（工单总结/分析 `/ai/tickets/:id/summary`、网关分支的 `/ai/triage`、RCA/根因、BPMN 生成、SLA 预测、A2UI、影响解释、邮件提取）在 demo 上都绕过租户 DB provider（千帆 `deepseek-v4.1-flash`），落到静态 env provider；demo 静态 `LLM_API_KEY` 缺失/模型不受支持时表现为静默降级（`ai_llm_calls` 里 `provider_key=<static> success=f`）。

**影响面审计（非测试调用点逐一过）**：
- 传静态模型（同类缺陷）：`service/impact_explanation_service.go`（`s.model` 来自 bootstrap）、`handlers/email_intake/extractor.go`（空则默认 `gpt-4o-mini`）；
- 传空模型已正确：BPMN 生成器、RAG、根因、RCA、总结、分诊、SLA 预测技能（但基础网关未绑定 → 依然命中静态快速路径，本次统一修复后一并走解析链）；
- 有意保留显式模型：provider 管理端「连通性测试」用 `record.Model`（正确，不改）。

**修复**：
1. `service/llm_gateway.go`：`Chat`/`ChatStream`/`ChatStreamWithTools` 在 `boundRequest == nil && resolver != nil` 时改走 `ChatWithRequest(ctx, ProviderRequest{}, model, messages)`（流式/tools 等价路径）；`resolver == nil`（能力开关关闭）时保持原静态快速路径逐字节不变。
2. `service/impact_explanation_service.go`：改 `ChatWithRequest(ctx, ProviderRequest{TenantID: tenantID}, "", messages)`，`Model` 取解析结果（空回退 `s.model`）。
3. `handlers/email_intake/extractor.go`：不再传 `e.model`（改传空，由解析链决定）。
4. 新增回归测试 4 个（网关：未绑定走解析链 / 无租户回落静态 / 流式未绑定 / 无解析器保持静态）+ 1 个（邮件提取委派模型给网关）。

**验证**：

| 检查 | 结果 |
|---|---|
| `go build ./...` | ✅ |
| `go test ./service/ -run "TestLLMGateway\|TestImpactExplanation"` | 全绿（含 4 个新用例） |
| `go test ./handlers/email_intake/ ./handlers/project/ ./handlers/ai/` | 全绿（含提取器委派用例） |
| 238 demo `1.8.0-dev.00aaa26e` | 三容器 healthy、`itsm-init` Exit 0、version 对齐、远端 HEAD `00aaa26e` |
| `GET /ai/tickets/65/summary` | `code=0`、`method=llm`、真实摘要；`ai_llm_calls` 新行 `provider_key=baidu model=deepseek-v4.1-flash success=t latency=2310ms` |
| `POST /projects/1/ai/summary`（§11 回归） | `ai` / `degraded=false` / `model=deepseek-v4.1-flash` / 457 字 |
| `ai_llm_calls` 前后对比 | 修复前样本 `#48 openai/<static> success=f 15906ms`；部署后 #49/#50 均为 baidu 成功 |
| 降级日志 | 10 分钟内无 `degraded` / `falling back` / `LLM failed` |

**范围外发现（Follow-up）**：
- F1 `/ai/triage` 恒退化关键词：后端分诊优先走 Guidance sidecar，`itsm-ai-service-demo` 的 `LLM_API_KEY` 为空（其日志 `LLM_API_KEY not configured`），sidecar 静态 `LLM_PROVIDER=openai/LLM_MODEL=gpt-*` 与租户 provider 不一致 → 需单独配 key 或调整 sidecar/gateway 优先级。
- F2 `get_ci_impact`（`/agent/tools/execute`）返回 5001：15.8ms、无 LLM 记录，与本次修复无关（`tool_registry` 的 `cmdb relationship service not initialized` 分支嫌疑），单独跟进。

证据：[DEMO-BE8-20261018-00aaa26e-EVIDENCE.md](../../.dev/deploy/DEMO-BE8-20261018-00aaa26e-EVIDENCE.md)。

## 13. 变更：AI 能力开关迁移至系统配置 + Bot 平台级开关免重启（2026-10-18）

**背景（用户反馈）**：① `bot.enabled=false` 时后端不注册 Bot 管理路由，页面只能提示「需管理员打开开关后**重启服务**」；② 能力开关藏在 `/admin/mcp-servers` 的卡片里，与「应用配置」直觉不符；③ MCP 页上的 Bot 能力配置「保存后不生效」。

**方案**：

1. **后端常驻装配 + 请求期求值**：`internal/bootstrap/app.go` 去掉 `if cfg.Bot.Enabled` 短路，Bot 管理组件/路由/运行门禁/指标/确认单 sweeper 一律装配；静态 `cfg.Bot.Enabled` 仅作为 `system_configs.bot.enabled` 缺行时的默认值，实际生效由 `capabilitySource` 在**请求期**求值——关闭态：管理写端 403（文案指引「系统配置 → AI 配置」）、读端保留（页面可展示关闭态）、聊天不落运行档案（与「未注入 RunStore」兼容态一致）；开启/关闭**无需重启**。
2. **前端迁移**：`mcp-servers/CapabilitySwitchesCard` → `system-config/AICapabilitiesCard`（testid `ai-capability-*`），挂到系统配置新增「AI 配置」页签（`?tab=ai` 深链 + 点击回写 / 只读降级 / 恢复默认二次确认）；MCP 页保留 `Alert` + 「前往 AI 配置」链接；i18n 中英文案同步改为平台级/免重启表述。

**验证**：

| 检查 | 结果 |
|---|---|
| `go build ./handlers/ai/... ./router/... ./internal/bootstrap/... ./capability/...` | ✅ |
| `go test ./router/ -run "TestSetupRoutes_BotRoutes\|TestBotRouteDeclarationsUseAICodes\|TestBotSelectorRouteDeclaresAIRead"` | ✅ |
| `go test ./handlers/ai/ -run "TestBotAdminHandler_\|TestGetBotMetrics\|TestChatToolDecisionBlacklistNeverDispatched\|TestValidateBotSelection"` | ✅ |
| `npm run type-check` / `npx eslint`（8 文件） | ✅ |
| jest（`AICapabilitiesCard` 5 / `AICapabilitiesSection` 3 / `mcp-servers` 4） | ✅ |
| 238 demo `1.8.0-dev.fd7514a9` | 三容器 healthy、`itsm-init` Exit 0、version 对齐、远端 HEAD `fd7514a9` |
| 关闭态 `POST /admin/bots` / `GET /admin/bots` | **403**（新文案）/ **200**（读端保留） |
| 开启态 `POST /admin/bots`（空 body） | **400** 校验错误（门禁放行）；开关切换全程容器 `StartedAt` 不变（**免重启**） |
| Playwright 浏览器验收（5 项） | 5/5 PASS：AI 页签选中 + 三开关渲染 + URL 联动 + MCP 迁移提示 + 链接跳转；截图 `ui-shots/CAP-*.png` |
| Playwright UI 闭环（开关 → 保存 → API 生效） | ✅ `before=true → offState=false → backOn=true`，恢复初始；截图 `CAP-5/6-*.png` |

证据：[DEMO-CAPSW-20261018-fd7514a9-EVIDENCE.md](../../.dev/deploy/DEMO-CAPSW-20261018-fd7514a9-EVIDENCE.md)。

## 14. 修复：AI 分诊识别 Guidance 静默降级并回落网关 + 按上下文租户调用（F1，2026-10-18）

**背景（BE-8 §12 的后续项 F1）**：BE-8 收尾时记录的「范围外发现」之一——demo `/ai/triage` 恒返回关键词分类（`confidence=0.7`、`reasoning="keyword-based classification (LLM unavailable)"`）且不产生 `ai_llm_calls` 遥测，sidecar 降级被静默吞掉。

**根因**：

1. `itsm-ai-service/api/guidance.py` 在 `LLM_API_KEY` 缺失时以 **HTTP 200** 返回关键词降级结果（`method="guidance-fallback"`），而 `TriageService.Suggest` 仅在 sidecar 抛错/不可达时才进入优先级 2（LLM 网关）→ 网关永不触发；
2. `/ai/triage`、`/ai/ticket/create` 调用 `Suggest`（内部硬编码租户 1），跨租户会话下 sidecar 审计与网关回落解析链拿到错误租户。

**修复**（提交 `eb214870`，4 文件）：

- `service/triage_service.go`：新增 `guidanceMethodFallback` 常量 + `isGuidanceDegraded`（`method=guidance-fallback` 视同 sidecar 不可用，记 warn 后继续走网关；旧版空 method 兼容按正常处理）；guidance 成功路径日志补 `method` 字段；网关失败仍回落关键词结果。
- `handlers/ai/service.go`：`TriageTicket`、`CreateTicketByAI` 改 `SuggestForTenant(ctx, …, tenantID)`。
- 测试：`service/triage_service_test.go` 新增 3 例（降级走网关 / 网关失败兜底 / 正常 guidance 不咨询网关）+ `handlers/ai/handler_test.go` 端到端租户透传（httptest sidecar 断言 `tenantId=7`）。

**验证**：

| 检查 | 结果 |
|---|---|
| `go build ./...`（GOMAXPROCS=2） | ✅ BUILD_EXIT=0 |
| `go test ./service/ -p 1` / `go test ./handlers/ai/ -p 1` | ✅ 全绿（含新增 4 测试） |
| 238 demo `1.8.0-dev.eb214870` | backend/worker/frontend healthy、`itsm-init` Exit 0、version 对齐、sidecar 未重启；A/B 指纹 `degraded to keyword fallback` 0→1 |
| `POST /ai/triage` ×2 | ✅ `network/0.95`、`application/0.82`，reasoning 为模型生成（修复前恒 `0.7 + LLM unavailable`） |
| `ai_llm_calls` | ✅ 新增 2 行 `provider=openai_chat_completions, pkey=baidu, model=deepseek-v4.1-flash, success=t`（1508ms） |
| backend 日志 | ✅ warn `TriageService: Guidance sidecar degraded to keyword fallback, using LLM gateway` |

**遗留**：sidecar 自身 `LLM_API_KEY` 仍为空（配置面可选补配，不配也不影响分诊）；F2 `get_ci_impact` 的 `ci_id` 分支 5001 单独跟进。

证据：[DEMO-F1-20261018-eb214870-EVIDENCE.md](../../.dev/deploy/DEMO-F1-20261018-eb214870-EVIDENCE.md)。
## 15. 变更：应用内 LLM 配置请求级下发 guidance sidecar（F2，2026-10-18）

**背景**：§14（F1）解决了「sidecar 静默降级 → 后端回落网关」，但应用内（`llm_provider_configs`，逐租户 + AES-GCM 密文）配置始终到不了 sidecar——sidecar 是单进程单配置服务，只读自身 env/config.yaml；Go 注册表里的 DB 解析链与它完全隔离。

**方案**（请求级下发）：`TriageService` 调用 sidecar 前解析**本租户默认实例**，把 `{protocol,variant,url,model,apiKey}` 随 `POST /triage` 的 `llm` 字段下发；sidecar 收到 `llm` 时不再读自身配置。能力门（sidecar 仅支持 `openai_chat_completions`/`anthropic_messages` 两形态）之外、或租户无可用目标 → **跳过 sidecar 直接走网关**（应用内配置为唯一事实源，不静默回落 sidecar 自身旧配置）。未接线（多 Provider 关闭）= 旧行为，前向兼容。

**实现**（提交 `c05cc49e`，5 源文件 + 1 新增）：

- `service/guidance_llm_spec.go`（新）：`GuidanceLLMSpec`、`GuidanceSpecResolver` 窄接口、`buildGuidanceSpec`（协议/变体能力门）、`(*LLMProviderRegistry).ResolveGuidanceSpec`（无 DB 客户端/非法租户/无默认/形态不支持 → `ok=false`）。
- `service/llm_registry.go`：entry 预计算 `guidanceSpec`——URL 用 `resolveProtocolURL` + `adapter.GetAPIPath()` + `protocol.DefaultEndpoint`，与网关**逐字节同口径**，无拼接漂移。
- `service/guidance_client.go`：`GuidanceTriageRequest` 增 `LLM *GuidanceLLMSpec json:"llm,omitempty"`；`Triage(ctx,title,desc,tenantID,spec)`。
- `service/triage_service.go`：`SetGuidanceSpecResolver` + `resolveGuidanceSpec`；`ok=false`/error → 跳过 sidecar；成功路径记 Info（含 method/confidence/latency）。
- `internal/bootstrap/app.go`：多 Provider 装配处 `triageService.SetGuidanceSpecResolver(providerRegistry)`。
- `itsm-ai-service/api/guidance.py`：`GuidanceLLMTarget` + `llm` 分支 + `_call_llm_override`（URL 由后端给，sidecar 只按协议 POST）；日志仅记 tenant/protocol/variant/url/model，**apiKey 禁入日志**。

**验证**：

| 检查 | 结果 |
|---|---|
| `go test ./service/ -run 'Guidance' -count=1 -v`（GOMAXPROCS=2） | ✅ 26 项 PASS（含 `ResolveGuidanceSpec_*` ×5、`GuidanceSpecResolver_*` ×4、F1 回归 ×3）；`ok 2.756s` |
| `go test ./handlers/ai/ -run 'Triage'` / `python -m py_compile api/guidance.py` | ✅ ok 1.680s / EXIT=0 |
| 238 demo `1.8.0-dev.c05cc49e` | ✅ **四容器**（backend/worker/frontend/ai-service）healthy、version 对齐；A/B 指纹 backend `sidecar LLM target` 0→1、ai-service `_call_llm_override` 0→2 |
| `POST /ai/triage` ×2（tenant 1 = `admin`） | ✅ 真实 LLM：`network/0.92`、`application/0.87`，reasoning 为模型生成中文 |
| sidecar 日志 | ✅ `Guidance triage using request-scoped LLM target tenant=1 protocol=openai_chat_completions url=https://qianfan.baidubce.com/v2/tokenplan/personal/chat/completions model=deepseek-v4.1-flash`（与 DB 行逐字一致） |
| sidecar 自身 env | ✅ `LLM_API_KEY` 仍为空（结果来自请求级下发，非 sidecar 自身配置） |
| `ai_llm_calls` | ✅ **新增 0 行**（F1 下每次分诊因降级回落各新增 1 行；F2 sidecar 直连成功，网关未被调用） |
| 负向对照（`demo.mfg.admin`=tenant 3，无 provider） | ✅ sidecar 未被调用（`override_log_lines=0`）、网关静态 provider 失败、终态 `keyword heuristic`——跨租户不下发，无 DB 目标租户与 F1 终态一致 |

**遗留**：① sidecar 日志 `variant=None`（空变体 cosmetic）；② `ok=false` 路径后端记 Debug（demo info 级别不可见），建议后续加指标/提级；③ sidecar 其它端点（rca/risk）仍读自身 env，本次仅 `/triage`；④ 未做浏览器级 UI 走查（承接 F1）。

证据：[DEMO-F2-20261018-c05cc49e-EVIDENCE.md](../../.dev/deploy/DEMO-F2-20261018-c05cc49e-EVIDENCE.md)。
