# P1 阶段门（Stage-Gate）详细设计 — PE-12

> 状态：**已落地（2026-10-19）**；权威依据：[01 模块蓝图 §3「阶段门」](../01-module-blueprint.md)、
> [p0-integration-contracts §4](../plan/p0-integration-contracts.md)（BPMN/审批链集成契约）、
> [06 实施路线图 §2 P1](../06-implementation-roadmap.md)。
> 本文是 PE-12 的实现权威（规则/状态机/API/流程绑定/降级/验收证据）。

## 1. 设计目标与范围

**目标**：把「立项 → 计划 → 验收 → 结项」四道阶段门做成**可执行的门禁**——
提交即跑规则，规则不通过拒绝并给明细；通过则触发 BPMN 审批（复用 `process_binding` + 审批桥接），
并由状态机在两个关键动作上强制（`activate`、`complete`）。

**范围内**：`project_gates` 表、门禁规则引擎、四道门提交/裁决 API、`business_type=project` 的流程绑定与
两个内置审批模板（立项审批/结项审批）、审计留痕、状态机强制点、前端「阶段门」面板。

**范围外**（后续工作项）：结项清单（资产登记/成员回收/复盘 KB）与 CSAT 触发（PE-15）、
门级 SLA 升级、多级审批链可视化、门通过率报表（PE-24）。

## 2. 门定义与门禁规则（v1）

| 门 | key | 强制点 | 门禁规则（全部满足才放行） |
|---|---|---|---|
| 立项门 | `initiation` | `planning → active` | ① 已任命项目经理（`projects.manager_id > 0`）② 至少 1 名活跃成员（`project_members.status=active`，即"干系人登记"） |
| 计划门 | `plan` | 进入执行（进入 `active` 后的计划冻结点） | ① 至少 1 个未取消任务 ② 至少 1 个里程碑；**通过时冻结计划基线**（无基线时建 v1，不可变） |
| 验收门 | `acceptance` | 交付完成前 | ① 全部任务 `done\|cancelled` ② 全部里程碑 `reached` |
| 结项门 | `closure` | `→ completed` | ① 关联单据全部终态（`ticket/incident/problem/change/service_request` 各域终态集合，复用既有 `NonTerminalRecords`） |

**顺序前置**：四道门固定顺序 `initiation → plan → acceptance → closure`；后门要求前门 `passed`，
否则返回阻塞项 `{type: "gate_not_passed", status: <前门状态>}`。

**阻塞明细**：`blockers[]` 元素 = `{type, detail?, refId?, number?, status?, total?}`；
类型词表：`gate_not_passed | manager_missing | member_missing | task_missing | milestone_missing | task_open | milestone_open | record_open`。
明细截断时以 `total` 给出总量（如 `task_open` 的未完成任务数）。

## 3. 状态机

```
pending ──提交（规则通过）──► in_review ──approve（流程结束/无流程）──► passed
   ▲                              │
   │                              └─reject（必须带原因）──► rejected
   └──────────── 重新提交（规则通过）◄──────────────────────────┘
```

- `pending`：未提交（无记录时前端按 `pending` 展示）。
- `in_review`：已触发流程审批（`process_instance_id > 0`）；多级审批时**本级通过仍保持 in_review**。
- `passed`：门放行；重复提交 → `409`（`ErrGateAlreadyPassed`）。
- `rejected`：拒绝留原因（写入 `decision_comment` + 审计）；**可重新提交**（回到 `in_review` 或直接 `passed`）。
- 并发：`status + version` CAS（`GateSubmitCAS` / `GateDecisionCAS`），冲突 → `409` + `currentVersion`。
- 阻塞提交也会落库（状态不变、`blockers` 更新、version+1），保证"拒绝可追溯"。

## 4. 数据模型：`project_gates`

| 列 | 类型 | 说明 |
|---|---|---|
| `id` | BIGSERIAL | 主键 |
| `tenant_id` | BIGINT NOT NULL | 租户（RLS 谓词 `tenant_id = get_current_tenant_id()`） |
| `project_id` | BIGINT NOT NULL | 项目（同租户） |
| `gate` | VARCHAR(32) | `initiation\|plan\|acceptance\|closure` |
| `status` | VARCHAR(32) | `pending\|in_review\|passed\|rejected` |
| `process_instance_id` | BIGINT DEFAULT 0 | 关联 BPMN 流程实例（0=未触发/降级自动通过） |
| `approval_chain_id` | VARCHAR(128) | 取自 `process_binding.approval_chain_id` |
| `submitted_by` / `submitted_at` | BIGINT / TIMESTAMPTZ | 提交人与时间 |
| `decided_by` / `decided_at` | BIGINT / TIMESTAMPTZ | 裁决人与时间 |
| `decision_comment` | VARCHAR(512) | 裁决意见（拒绝必填；自动通过写 `auto_approved_no_flow`） |
| `blockers` | JSONB | 最近一次门禁阻塞明细（通过时清空） |
| `version` | INT DEFAULT 1 | 乐观锁 |
| `created_at` / `updated_at` | TIMESTAMPTZ | — |

约束与索引：唯一 `uk_project_gates (tenant_id, project_id, gate)`；`project_gates (project_id, status)` 查询索引。

**迁移与 RLS**：ent schema `project_gates`（`go generate ./ent` 生成实体）+ 幂等兜底
`migrations/20261020_project_management_pe12.sql`（+`_down`）；RLS 覆盖清单
（`database/rls/migrations/015_project_management_policies.sql`）与 `cmd/rls-apply` 受管清单已加入 `project_gates`
（表创建后重跑 015 即生效）。

## 5. 审批集成（BPMN）

- **business 标识**：`business_type=project`（`dto.BusinessTypeProject`）、
  `business_sub_type=<gate>`（`initiation|plan|acceptance|closure`）、`business_key=project:<id>`（引擎按此反查）。
- **路由**：`process_binding` 按 `(business_type=project, business_sub_type=<gate>)` 匹配（`FindBestBinding`，
  priority 倒序 + is_default 兜底）；命中的 `approval_chain_id` 回写门记录。
- **内置模板**（`service/bpmn/*.bpmn`，随 `LoadAndDeployTemplates` 在租户开通时部署）：
  - `project_initiation_flow`（项目立项审批流程）：开始 →「立项审批」用户任务 → 排他网关（`approvalResult == 'approved'`）→ 通过/拒绝结束。
  - `project_closure_flow`（项目结项审批流程）：同构，「结项审批」用户任务。
  - 默认 binding（`config/seed/default.json` + `pkg/seeder` 基座）：`project/initiation → project_initiation_flow`、
    `project/closure → project_closure_flow`；**计划门/验收门默认无审批流程**（纯规则校验）。
- **裁决**：复用 `BPMNApprovalBridge.CompleteBusinessApprovalTask(businessType=project, businessID=<projectId>)`
  完成当前待办用户任务；随后按 `business_key=project:<id>` 检查是否仍有 running 实例：
  - 流程已结束 → 门按裁决结果置 `passed`/`rejected`；
  - 流程仍运行（多级审批）→ `approve` 保持 `in_review`（等待后续审批人），`reject` 立即置 `rejected`。
- **降级（fail-open 有据）**：
  - 无 binding → 提交即**自动通过**（`process_instance_id=0`，`decision_comment=auto_approved_no_flow`）；
  - binding 指向的流程定义不存在（租户误配/未部署）→ 记 warning 并同样降级自动通过，避免门提交永久失败；
  - 审批端口未注入（单测/极简部署）→ 同"无 binding"处理。
  - 触发/裁决**异常**不降级：返回 `409`（`ErrGateApprovalFailed` 语义，消息含底层原因），门保持原状态（不产生半成品）。
- **存量租户**：内置流程定义仅随租户开通部署；存量租户需在流程模板库导入/部署后配置 binding，
  未配置期间按上述降级自动通过（行为一致、不阻塞业务）。

## 6. API

| 方法 | 路径 | 权限 | 说明 |
|---|---|---|---|
| GET | `/api/v1/projects/:id/gates` | `project:read` | 四道门列表：`{items, gates[], projectGates[]}`（`gates` = 固定顺序） |
| POST | `/api/v1/projects/:id/gates/:gate/submit` | `project:write` | 提交：先跑规则（409 + `blockers`），通过后触发审批（`in_review`）或自动通过（`passed`） |
| POST | `/api/v1/projects/:id/gates/:gate/decision` | `project:write` | 裁决：`{decision: approve\|reject, comment}`；`reject` 必填 `comment`（400） |

**错误语义**（均为 `common.AppError` 风格）：

| 场景 | HTTP | 说明 |
|---|---|---|
| 未知门 / 非法 decision / 拒绝空原因 / 终态项目 | 400 | `PARAM_ERROR`（拒绝空原因复用 `ErrReasonRequired`） |
| 项目不存在 / 不可见 / 跨租户 | 404 | 防存在性泄露（复用可见性谓词） |
| 门禁阻塞 | 409 | `data = {gate, blockers[], total}` |
| 重复提交（`in_review`/`passed`）、重复裁决、流程推进失败 | 409 | `ErrGateInReview` / `ErrGateAlreadyPassed` / `ErrGateNoApproval` / `ErrGateApprovalFailed` |
| CAS 冲突 | 409 | `{currentVersion}` |

**状态机强制点**：

- `POST /projects/:id/status {action: activate}`：立项门必须 `passed`，否则 409 + `{gate: initiation, blockers}`；
- `POST /projects/:id/status {action: complete}`：先跑既有结项门禁（任务/单据），再要求结项门 `passed`，否则 409 + `{gate: closure, blockers}`。

**审计**（`auditlog`，`resource=project`）：`gate_submit | gate_pass | gate_reject | gate_approve_step`；
载荷含 `gate`、`projectId`、`status`、`processInstanceId`，拒绝额外写原因与阻塞项摘要（≤5 条）。
实现落点：`handlers/project/gate_handler.go`（复用 `tenantctx` + 2s 超时，写失败不阻断业务）。

## 7. 前端（项目工作台）

- 「阶段门」Tab：四道门固定顺序表格（门/状态/提交与裁决信息/门禁阻塞项/操作），顶部进度 `已通过 n/4`；
  提交按钮对 `pending|rejected` 可见，`in_review` 时提供「通过 / 拒绝」（拒绝弹窗必填原因）；
  `plan` 门通过后自动刷新「计划基线」Tab（基线冻结可视化）。
- 状态操作失败提示统一提示"检查阶段门与结项门禁"。

## 8. 关键决策（与蓝图/契约一致）

| # | 决策 | 理由 |
|---|---|---|
| D1 | 门记录一项目一门一行（不是每次提交一条） | 门是"当前关口状态"，历史由审计承载；避免历史表膨胀与查询复杂度 |
| D2 | 前置门顺序强制（后门要求前门 passed） | 蓝图四道门是阶段边界而非并列开关；无需额外配置 |
| D3 | 无 binding 时自动通过（而非报错/阻塞） | 默认安装可用、升级不阻塞存量租户；抑制项写 `auto_approved_no_flow` 且审计留痕 |
| D4 | 计划门通过时冻结基线（已存在基线则不重复冻结） | 契约"计划门通过时冻结 project_baselines（不可变）"；后续变更走 v2+reason |
| D5 | 结项门与既有结项门禁**并存**（先业务阻塞项、后门） | 先暴露真实缺口（任务/单据）再提示门，UX 与语义都更清晰 |
| D6 | 多级审批中途 approve 不置 passed | 避免"一级通过即放行"破坏审批语义（`flowFinished` 判定） |
| D7 | 门记录不软删、终态项目禁止提交 | 门是治理留痕；取消/结项后的门状态冻结 |

## 9. 验证证据（2026-10-19）

| 项 | 命令/用例 | 结果 |
|---|---|---|
| 门禁规则/顺序/基线冻结/全链路放行 | `go test ./handlers/project/ -run TestGateRulesAndSequence -v` | ✅ |
| 流程审批（in_review/拒绝必填原因/重提交/多级审批/端口故障不留半成品） | `go test ./handlers/project/ -run TestGateApprovalFlow -v` | ✅ |
| 跨租户与不可见 404、终态 400、非法门 400、列表可见性 | `go test ./handlers/project/ -run TestGateIsolationAndTerminal -v` | ✅ |
| 端点信封/别名、409 + blockers、未知门 400、审计落库 | `go test ./handlers/project/ -run TestHandlerGateEndpoints -v` | ✅ |
| 既有回归（状态机/模板/基线/里程碑/依赖/单据） | `go test ./handlers/project/` | ✅ 全绿（含新增 4 个门用例） |
| 内置 BPMN 模板全量 lint（含新增 2 个） | `go test ./service/ -run TestBuiltinBPMNTemplatesLint -v` | ✅ |
| 路由权限预检 + 守卫 | `go test ./middleware/ -run Precheck`、`go test ./router/ -run "Permission\|Precheck\|PlatformFace\|Menu"` | ✅ |
| 预检映射/ACL 清单重生成 | `go run ./cmd/authz-gen`、`node scripts/generate-acl-manifest.js` | ✅ **914 路由**（+3 门端点） |
| 全量后端构建 | `go build -p 2 ./...` | ✅ exit 0 |
| 迁移/种子/ent 回归 | `go test ./migration/ ./pkg/seeder/ ./ent/...` | ✅ |
| 前端类型/规范 | `npm run type-check`、`npx eslint "src/pages/(main)/projects/detail.tsx" src/lib/services/project-service.ts` | ✅（0 error / 0 warning） |
| 前端服务单测 | `npx jest src/lib/services/__tests__/project-service.test.ts --coverage=false` | ✅ **26/26**（+4 门用例） |
| 前端 API 路径静态校验 | `node scripts/check-api-paths.js` | ✅ **1047/1047**（+3 门端点） |
| 前端生产构建 | `npm run build` | ✅ |
| antd 规则（改动文件） | `npm run lint:antd` | 改动文件零命中（仓库级存量 42 处，非本次引入） |

**P1 收口待办**（与本工作项相关）：真实 PostgreSQL 上重跑 ent 迁移 + `20261020_pe12.sql` + RLS 015 `-verify`，
以及浏览器级 UI 冒烟（阶段门 Tab 提交/裁决）。这两项按 `06` §2 约定在 **P1 收口**统一执行。

**观察项（非本工作项引入）**：前端全量页面用例抽查（`npx jest src/pages`）出现的失败集中在
`src/pages/(main)/tickets/types/__tests__/index.test.tsx`（导入仅 `TicketTypesPage` + `TicketTypeApi`，
与本批改动零交集），与本工作项无关；本批改动文件（`projects/detail.tsx`、`project-service.ts` 及其单测）
的用例全绿。
