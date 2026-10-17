# P1 搜索与待办设计（PE-16）

> 状态：**已实现 v1.0**（2026-10-22）
> 上游：`01-module-blueprint.md` 能力矩阵「搜索与待办」、`03-integration.md` §2.17、`06-implementation-roadmap.md` PE-16
> 范围：全局搜索纳入 `project`/`project_task`（**强制可见性谓词**）；个人工作台「我的项目任务」聚合（任务 + 里程碑到期）

---

## 1. 目标与范围

| 目标 | 落地 |
|---|---|
| 全局搜索可命中项目（名称/编号）与项目任务（任务名） | `handlers/global_search` 新增两类结果（`type=project`/`project_task`） |
| 搜索结果不泄露不可见项目（契约 §12） | 项目/任务查询强制可见性谓词：管理角色 ∨（活跃成员 ∪ 项目经理） |
| 个人工作台「我的项目任务」聚合（含里程碑到期） | `/dashboard` 新增「我的项目任务」卡片：并行聚合 `GET /projects/my-tasks`（已有）+ `GET /projects/my-milestones`（新增） |

非目标（后置）：搜索排序权重/高亮、里程碑到期**通知**（outbox 投递，与 PE-14 验收项一并跟踪）、搜索范围偏好（P2）。

## 2. 决策

| 编号 | 决策 | 理由/影响 |
|---|---|---|
| D-21 | 搜索可见性谓词 = `role.IsAdminLike` 直通租户全量；否则 = **活跃成员（`project_members.status=active`）∪ 项目经理（`projects.manager_id`）** | 与项目域可见性口径一致（D-06）：非管理角色须为成员；项目经理为 `manager_id` 唯一权威且受移除保护（D-19），故必须可见自己的项目。谓词在查询层收窄，不做"先查后过滤"（防分页/上限泄漏） |
| D-22 | 「我的项目任务」聚合**不改动**已交付的 `GET /projects/my-tasks`（`[]MyTaskResponse`），新增只读端点 `GET /projects/my-milestones`；前端卡片**并行聚合两个端点** | ① 已有端点契约/测试/文档（P0 交付）零破坏；② 里程碑到期（planned + 截止窗口）与任务（未终结）语义不同，独立端点可独立测试；③ 与 dashboard「多源并行 + 局部降级」的数据获取模式一致（03 §2.17） |

## 3. 全局搜索（`itsm-backend/handlers/global_search`）

### 3.1 结果字段

`SearchResult` 在原有字段上新增（仅项目/任务结果返回）：

| 字段 | 类型 | 说明 |
|---|---|---|
| `code` | string | 项目编号（`project`/`project_task` 均返回，供展示×跳转） |
| `projectId` | int | 任务所属项目 ID（前端跳转到项目工作台） |
| `projectName` | string | 任务所属项目名 |

命中规则：`project` = 名称 `name` **或** 编号 `code` 模糊匹配（`ContainsFold`）；`project_task` = 任务名 `name` 模糊匹配；两者均排除软删（`deleted_at IS NULL`），每类上限 `perTypeLimit=10`。

### 3.2 可见性谓词（实现：`Service.visibleProjectIDs`）

```text
admin（role.IsAdminLike ∈ {super_admin, admin, manager, sys_admin}）
  → 租户内全部未软删项目 ID
否则
  → project_members(tenant_id, user_id, status='active') 的项目 ID
    ∪ projects(tenant_id, manager_id=user_id, deleted_at IS NULL) 的项目 ID
    （去重升序；viewer 缺失/无命中 → 空集 → 项目与任务零命中，fail-closed）
```

项目与任务查询均以该集合做 `IDIn` 收窄（任务不另做项目存在性回查，集合本身即"未软删项目"或"已核验可见项目"）。任务结果的项目名/编号：优先复用同批命中的项目行，未命中时按项目 ID 补查一次（`fillProjectLabels`），避免 N+1。

### 3.3 边界

- 跨租户：查询首条件即 `tenant_id`，测试覆盖"B 租户同关键词只回 B 的项目/任务"；
- 无用户上下文（viewer=0）时非管理分支自然空集，不放大可见范围；
- 与原五类结果（ticket/incident/problem/change/knowledge）共存，顺序为原五类 → 项目 → 任务。

## 4. 个人工作台聚合

### 4.1 `GET /api/v1/projects/my-milestones`（新增，`project:read`）

| 项 | 约定 |
|---|---|
| 语义 | 我（`owner_id=当前用户`）负责的**未达成/未错失**里程碑（`status=planned`）跨可见项目聚合 |
| 可见性 | 同 §3.2 非管理分支的**成员口径**（与 `/projects/my-tasks` 完全一致，保持个人聚合口径统一） |
| 参数 | `dueWithinDays`（默认 30，`0`=不限，否定/非法 400）；`limit`（默认 50，服务端收敛 ≤200，非正 400） |
| 排序 | `due_date` 升序（逾期在前），同日期按 ID |
| 响应 | `MyMilestoneResponse = ProjectMilestoneResponse + { projectName, projectCode, overdue, daysLeft }`；`daysLeft` 为按天截断的相对天数（逾期为负） |
| 空态 | 无可见项目/无到期项 → `[]`（不 404） |

`owner_id` 为创建时校验过的项目活跃成员（PE-14 规则），因此"我的里程碑"天然限定在曾参与的项目内。

### 4.2 前端

- `components/layout/header/GlobalSearch.tsx`：`project` → `/projects/:id`；`project_task` → `/projects/:projectId`（缺失回退 `/projects` 列表）；结果副标题合并展示 `status · projectName · code`；
- `lib/services/project-service.ts`：新增 `MyProjectTask`/`MyMilestone` 类型与 `myMilestones(dueWithinDays, limit)` 方法（`myTasks` 返回类型升级为 `MyProjectTask[]`，字段超集，兼容原调用）;
- `pages/(main)/dashboard/components/MyProjectWorkloadCard.tsx`：`PermissionGate anyOf=['project:read']` 门控，`Promise.allSettled` 并行拉取两源——单侧失败仍展示另一侧，双侧失败仅本卡片降级为错误态，不影响仪表盘其余分区；任务行/里程碑行点击进入项目工作台；里程碑 Tag 显示「逾期 N 天 / 今天到期 / N 天后到期」。

## 5. 验证证据（2026-10-22）

| 项 | 结果 |
|---|---|
| `go test ./handlers/global_search/ -count=1` | ✅ 含 `pe16_test.go`：成员（仅自己项目+任务）/ 项目经理（manager_id 直通）/ 无项目用户（零命中）/ admin（全量）/ 编号命中 / 跨租户隔离 |
| `go test ./handlers/project/ -run "MyMilestones" -v` | ✅ 2 用例：聚合语义（owner/planned/窗口含逾期/排序/项目标签/limit 收敛/admin/移除成员后收窄）+ 端点（映射、`dueWithinDays=abc` 400、`limit=0` 400、窗口 0 不限） |
| `go test ./handlers/project/ ./handlers/global_search/ -count=1` | ✅ 全包回归 |
| `go test ./middleware/ -run Precheck` + `go test ./router/` | ✅（新路由挂 `project:read`，预检/守卫同步） |
| `cmd/authz-gen` + `node scripts/generate-acl-manifest.js` | ✅ 预检重生成；ACL 清单 **920 路由** |
| 前端 `npx tsc --noEmit` | ✅ |
| 前端 `npx jest --runTestsByPath MyProjectWorkloadCard / global-search-api / project-service` | ✅ 40/40（卡片：并行聚合与参数、逾期文案、单侧降级、双侧错误态、空态；API：project/project_task 透传） |
| 改动文件 `eslint` | ✅ 0 error/0 warning |
| `node scripts/check-api-paths.js` | ✅ 1052/1052 |
| `npm run build` | ✅ |

> 已知非本批问题：`src/pages/(main)/tickets/types/__tests__/index.test.tsx` 与 `src/pages/(main)/ai/approval/__tests__/b1-09.test.tsx` 为存量失败（与项目管理改动无关，本批未触碰）。

## 6. 后续（P2/收口）

- 里程碑到期**通知**（outbox/通知中心）与 PE-14 验收项一并跟踪；
- 全局搜索可考虑：结果分组折叠、类型过滤参数、命中高亮（P2 报表/AI 批次统一评估）；
- P1 收口统一执行真实库迁移/RLS/UI 端到端验证（含本批新端点）。