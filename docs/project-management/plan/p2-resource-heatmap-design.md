# P2 · PE-23 资源负荷只读热图 — 设计

> 状态：**已实现 v1.0（2026-10-26，后端 + 前端）**
> 上游裁决：[07 §2-10 / §4.2-B4 / §4.2-B7](../07-completeness-review.md)（只读热力图，不做均衡；v1 自然日 + 明确标注）、[06 §2 PE-23 / §4.3](../06-implementation-roadmap.md)。
> 关联设计：[p2-budget-hours-design.md](./p2-budget-hours-design.md)（PE-22 任务级工时，本切片负荷计算的上游数据）。

## 1. 目标与范围

**目标**：按周给出「项目成员 × 周」的负荷热图（只读）：每格展示该成员本周的**计划工时**（估算摊分）、**剩余工时**与**周容量**，以颜色区分负荷率。

**范围（v1）**：

- 周维度、自然日摊分（不接工作日历，界面与响应显式标注）；
- 周容量可配置：项目级默认 + 成员级覆盖；
- 行覆盖：存活成员 ∪ 窗口内有任务的负责人（后者标注已移除，防静默漏载）；
- 只读：**不做**自动均衡、不做冲突强制、不做拖拽调整（01 §1.3 非目标）。

**非目标**：按人按日明细（worklog，见 D-36）、跨项目聚合热图（PE-24 报表口径）、工作日历实体（B7 后置，与依赖/甘特同步时引入租户级单一来源）。

## 2. 决策记录（D-37 ~ D-39）

| 编号 | 决策点 | 结论 | 依据 |
|---|---|---|---|
| **D-37** | 周容量配置落点 | **两级**：`projects.default_weekly_capacity_hours`（项目级默认，NULL=内置 40h）+ `project_members.weekly_capacity_hours`（成员级覆盖，NULL=继承项目默认）。写入走独立端点 `PUT /projects/:id/capacity`（幂等、部分更新语义：字段缺省=不改），审计 `capacity_update` | 容量是团队配置面（非项目计划数据），不入基线快照；两级覆盖满足"兼职/半天"真实场景，且不新建实体 |
| **D-38** | 热图口径 | 周对齐 ISO 周（周一为首、周日为末）；`plannedHours` = 任务 `estimate_hours` 在 `[plan_start, plan_end]` **自然日均摊**后按周切片；`remainingHours` = 非终态任务 `max(estimate − actual_hours, 0)` 在 `[max(plan_start, today), max(plan_end, today)]` 均摊（逾期任务剩余全量落在本周，形成可见压力）；终态（done/cancelled）剩余=0 | 复用 PE-22 工时字段；自然日口径与 B7 裁决一致（响应携带 `calendar.mode=natural_days` + note） |
| **D-39** | 快照与复用 | 热图**实时计算不落库**（无快照表）；容量配置**不进基线快照**（基线冻结计划/预算，容量属组织配置）；PE-24 资源利用率复用本切片同一口径（planned/capacity） | 避免双写漂移；报表口径单一来源 |

## 3. 数据层

### 3.1 列变更（无新表 → RLS/`rls-apply`/分页别名无变更）

| 表 | 列 | 类型 | 语义 |
|---|---|---|---|
| `projects` | `default_weekly_capacity_hours` | float NULL | 项目周容量默认（小时）；NULL=内置 40 |
| `project_members` | `weekly_capacity_hours` | float NULL | 成员周容量覆盖；NULL=继承项目默认；移除成员行保留该值（复活沿用） |

迁移：`migrations/20261026_project_management_pe23.sql`（幂等 `ADD COLUMN IF NOT EXISTS`）+ `_down`；列由 ent `Schema.Create` 先行，迁移兜底（对齐 pe11/pe21/pe22 双轨惯例）。

### 3.2 校验与写入语义

- 取值域：`0 < h ≤ 168`（7×24 上限）；越界 → 400 `ErrValidation`。
- 清空：`null` 或 `0` → 写 NULL（成员=回继承；项目=回内置 40）。
- `members[]` 项：`userId` 必须是**存活成员**（否则 400「成员不在册」）；重复 `userId` → 400。
- 审计：`resource=project`、`action=capacity_update`，detail 记录 `{defaultWeeklyCapacityHours:{old,new}, members:[{userId,old,new}]}`（未变化不写条目；审计写入失败不阻断主流程，沿用 P0 先例）。

## 4. API 契约（+2 路由）

### 4.1 `PUT /projects/:id/capacity`（`project:write`）

请求（字段缺省=不修改）：

```json
{ "defaultWeeklyCapacityHours": 40, "members": [{ "userId": 5, "weeklyCapacityHours": 24 }, { "userId": 6, "weeklyCapacityHours": null }] }
```

响应：

```json
{ "defaultWeeklyCapacityHours": 40, "members": [{ "userId": 5, "weeklyCapacityHours": 24 }, { "userId": 6, "weeklyCapacityHours": null }] }
```

> `members` = **存活成员全量**（按加入顺序；`null`=继承项目默认），便于前端整体回填容量设置弹窗。

### 4.2 `GET /projects/:id/resource-heatmap?start=YYYY-MM-DD&weeks=6`（`project:read`）

- `weeks` 1..12（默认 6）；`start` 可省（默认本周周一），服务端 snap 到所在 ISO 周周一；非法 → 400。
- 可见性：同详情（非管理角色必须是存活成员，否则 404，不泄露存在性）。
- 响应：

```json
{
  "projectId": 3,
  "range": { "start": "2026-10-19", "end": "2026-11-29" },
  "calendar": { "mode": "natural_days", "businessDaysOnly": false, "note": "v1 按自然日均摊（含周末），未接入租户工作日历；周容量为整周口径" },
  "defaults": { "defaultWeeklyCapacityHours": 40, "source": "fallback" },
  "weeks": [
    { "weekStart": "2026-10-19", "weekEnd": "2026-10-25",
      "members": [
        { "userId": 5, "name": "张三", "username": "zhangsan", "projectRole": "member", "memberStatus": "active",
          "capacityHours": 24, "plannedHours": 12, "remainingHours": 6, "plannedRatio": 0.5, "remainingRatio": 0.25 }
      ] }
  ],
  "unplanned": { "taskCount": 2, "estimateHours": 9 }
}
```

- `defaults.source`：`project`（项目级已配置）| `fallback`（内置 40）。
- `unplanned`：窗口内**有负责人且有估算但无计划日期**的任务（未排期，无法摊分），提示补齐计划日期。
- 行内 `memberStatus`：`active` | `removed`（任务负责人在窗口内但成员已被移除，或缺成员行时按离册兜底）；用户主数据缺失时 `name`/`username` 降级为空串，不阻塞展示。

## 5. 计算口径（权威）

设任务 T（未删、assignee 非空、estimate>0）：

1. **计划摊分**：窗口 `[plan_start, plan_end]`（仅一端时=该端单日；`plan_end < plan_start` 已由任务校验拦截，不会出现）；`每日 = estimate / 窗口天数`；周切片 = `每日 × 该周与窗口的交集天数`。
2. **剩余摊分**：终态（done/cancelled）→ 0；否则 `rem = max(estimate − actual_hours, 0)`；窗口 `[anchor, end']`，其中 `anchor = max(plan_start, today)`（plan_start 缺省=today），`end' = max(plan_end, today)`（plan_end 缺省=today；逾期 → `end' = today` → 剩余全量落本周）。
3. **容量解析**：`member.weekly_capacity_hours ?? project.default_weekly_capacity_hours ?? 40`。
4. **比率**：`plannedRatio = planned / capacity`、`remainingRatio = remaining / capacity`（capacity>0 恒真，无需防零分支）。
5. 浮点：服务端保留原始 float，比对/前端展示自行取整（前端 1 位小数）。

## 6. 前端交互

- 工作台新增 Tab「资源负荷」（`projectService.resourceHeatmap`）：
  - 表头 = 周区间（如 `10-19 ~ 10-25`），行 = 成员（姓名 + 角色/状态标签）；
  - 单元格：主数字=计划工时，副行=剩余/容量；配色按 `plannedRatio`：0 灰、≤50% 绿、≤80% 黄、≤100% 橙、>100% 红；
  - 顶部：`Alert`（自然日标注，来自 `calendar.note`）+ 未排期提示（`unplanned`）+ 周数切换（4/6/8/12）+「容量设置」按钮（`project:write` 可见）；
  - 容量设置弹窗：项目默认 + 每成员覆盖（留空=继承/回内置），保存走 `PUT /capacity`。
- 列表/概览不改；成员列表在「成员」Tab 展示容量值时随 `MemberView.weeklyCapacityHours`（若已展示则顺带，不强求）。

## 7. 测试计划

- 后端 `pe23_test.go`：
  1. 容量配置：默认+覆盖写入/清空/越界 400/非成员 400/重复 400 + 审计 `capacity_update`；
  2. 热图口径：跨界任务均摊（两周切片）、完成→剩余 0、逾期→剩余落本周、未排期入 `unplanned`、容量覆盖优先于项目默认、`weeks` 越界 400、可见性 404；
  3. 移除成员仍出行（`memberStatus=removed`）。
- 前端：`project-service` 单测（2 方法）+ 配色/格式化纯函数单测。
- 路由：`cmd/authz-gen` + ACL 清单重生成（预期 **937**）；`check-api-paths` 全量。

## 8. 变更记录

| 日期 | 版本 | 变更 |
|---|---|---|
| 2026-10-26 | v1.0 | 首版（D-37~D-39）：数据层两列 + 2 API + 前端热图 Tab + 容量设置；实现与验证证据见 06 PE-23 条目 |