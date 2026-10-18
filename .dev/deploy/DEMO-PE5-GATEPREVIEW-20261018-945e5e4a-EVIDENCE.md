# 238 demo 部署证据：阶段门进入即实时预检（2026-10-18，1.8.0-dev.945e5e4a）

## 需求

此前只有点击「提交」后才知道哪些门禁条件未完成；要求在进入阶段门页面时就提前显示/检查待办项。

## 实现

### 后端（只读预检，与提交共用规则）

- 新增 `Service.PreviewGates`：对四道门逐一调用现有 `evaluateGateRules`（`SubmitGate` 同源规则），返回 `readiness: [{gate, ready, blockers[]}]`。
- **不落库、不创建门记录、不触发审批**；`GET /api/v1/projects/:id/gates` 响应新增 `readiness` 字段。
- 可见性沿用 `GetForViewer`（非管理角色须为项目成员）。
- 单测 `TestPreviewGates`：未提交即返回 `manager_missing/member_missing` + 计划门 `gate_not_passed`；任命经理 + 登记成员后即时 `ready=true`；两次预检前后门记录仍为空。

### 前端（进入即显示）

- 阶段门表格的「门禁阻塞项」：`pending/rejected` 使用实时预检（无需提交）；`in_review/passed` 保留提交快照。
- 预检通过且未提交 → 显示绿色「条件已满足，可提交」。
- 顶部新增预检标签：「预检：立项门待补 N 项 / 已满足，可提交 / 审批中」。
- 切回阶段门 Tab 自动重新预检；任命经理、添加成员、提交门后同步刷新。
- 阻塞项右侧保留下一步入口（任命项目经理 / 添加成员 / 新建任务 / 新建里程碑 / 查看关联单据）。

提交：`9a53da0a`（预检）、`945e5e4a`（顶部标签区分审批中）。

## 验证

### API（project #5，仅立项门有记录）

```
record-gates: ['initiation']
initiation ready=True  blockers=[]
plan       ready=False blockers=[gate_not_passed, task_missing, milestone_missing]
acceptance ready=False blockers=[gate_not_passed(plan pending)]
closure    ready=False blockers=[gate_not_passed(acceptance pending)]
```

→ 计划/验收/结项门**没有提交过**，预检已返回实时阻塞项。project #3 的 readiness 也正确反映结项门被 PE-5 冒烟工单阻塞（`record_open`）。

### 浏览器（Tabbit）

- `/projects/5` 阶段门：计划门（未提交）直接显示
  `前置阶段门未通过`、`计划为空：至少需要 1 个任务`（去新建任务 →）、`计划为空：至少需要 1 个里程碑`（去新建里程碑 →），**全程未点击提交**。
- 顶部标签：`预检：立项门审批中`。
- 4 行门表格正常。

### 其他检查

| 检查 | 结果 |
|---|---|
| `go test -p 1 -count=1 ./handlers/project/` | 全绿（含新增 `TestPreviewGates`） |
| 前端 `tsc --noEmit` / 改动文件 ESLint | ✅ |
| 238 demo `1.8.0-dev.945e5e4a` | backend/worker/frontend healthy、init ExitCode=0、version 对齐 |

## 说明

- 预检每次进入阶段门/刷新时实时计算（4 门共若干只读查询），不缓存、不落库，避免数据滞后。
- 后续可选：把预检结果用于页头「激活」按钮的禁用/提示（当前激活仍由后端 409 + 前端引导兜底）。
