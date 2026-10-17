# P0 API 与前端页面设计（API & Pages）

> 状态：**Draft v1.0（2026-10-17）**
> 范围：P0 端点清单 + 前端页面/路由/权限/菜单映射
> 前置：[P0 集成契约](./p0-integration-contracts.md)、[P0 数据 DDL](./p0-data-ddl.md)
> 约定：`/api/v1` 前缀省略；分页统一 `page/pageSize`（`pageSize` ≤100，响应经 `common.NewListResponse` 输出领域别名）；错误语义 400/403/404/409（见契约 §2）。

---

## 1. API 总表（P0）

> **实现进度（2026-10-17）**：PE-1 数据层 ✅；PE-2 项目核心 API ✅；PE-3 成员 ✅；PE-4 任务 ✅；PE-5 单据关联 ✅；**PE-6 前端 ✅**（列表重写 + 工作台 `/projects/:id` 四 Tab + 路由/权限登记 + 菜单 seed 新增「项目管理」入口 + `applications` 契约适配）。P0（PE-1~PE-6）代码全部落地；收尾项（PE-7/PE-8）：真实 DB 运行迁移与 RLS `-verify`、README 成熟度/ROADMAP 登记、全量构建复跑。

### 1.1 项目

| 方法 | 路径 | 权限 | 说明 |
|---|---|---|---|
| GET | `/projects` | `project:read` | 过滤：`status/type/ownerTeamId/managerId/q/page/pageSize`；按可见性谓词收窄（PE-3 接入） |
| POST | `/projects` | `project:write` | 创建（body 化，替代现存 query 参数风格） |
| GET | `/projects/:id` | `project:read` | 详情（含进度/健康度/实际日期） |
| PUT | `/projects/:id` | `project:write` | 基础字段更新（不含状态） |
| DELETE | `/projects/:id` | `project:write`（现状口径；`project:delete` 保留未来拆分） | 软删；存在关联任务/单据时拒绝（409 + blockers） |
| POST | `/projects/:id/status` | `project:write` | 状态流转（§契约 3.3）；`complete` 走门禁 |
| GET | `/projects/my-tasks` | `project:read` | 跨可见项目的我的任务聚合（个人工作台用；静态段与 `:id` 共存有 `/tickets/stats` 先例，无路由冲突） |

### 1.2 成员

| 方法 | 路径 | 权限 | 说明 |
|---|---|---|---|
| GET | `/projects/:id/members` | `project:read` | 返回 `memberOrigin`（home/invite/allocation 派生）与 `expiresAt` |
| POST | `/projects/:id/members` | `project:write` | `{userId, projectRole}`；校验活跃 membership |
| PATCH | `/projects/:id/members/:userId` | `project:write` | 改角色 |
| DELETE | `/projects/:id/members/:userId` | `project:write` | 软失效（`status=removed`，保留历史） |
| GET | `/projects/:id/member-candidates?q=` | `project:write` | 活跃 membership 候选；仅返回最小字段（userId/name/team/expiresAt，前端 ≤7 天预警） |

### 1.3 任务

| 方法 | 路径 | 权限 | 说明 |
|---|---|---|---|
| GET | `/projects/:id/tasks?view=tree\|flat&status=&assigneeId=` | `project:read` | WBS 树/平铺 |
| POST | `/projects/:id/tasks` | `project:write` | 创建（`parentTaskId` 深度 ≤3 校验） |
| GET | `/projects/:id/tasks/:taskId` | `project:read` | 详情 |
| PUT | `/projects/:id/tasks/:taskId` | `project:write` | 更新（含 `sortOrder` 拖拽排序） |
| DELETE | `/projects/:id/tasks/:taskId` | `project:write` | 软删（有子任务时 409） |
| POST | `/projects/:id/tasks/:taskId/status` | `project:write` | `todo→in_progress→done` 等受控流转（CAS + 409） |
| POST | `/projects/:id/tasks/:taskId/tickets` | `project:write`∩`ticket:create` | 生成工单（契约 §3.2；重复 409） |
| DELETE | `/projects/:id/tasks/:taskId/tickets` | `project:write`∩`ticket:create` | 解绑主工单（审计 `ticket_unbind`；解绑后可重建） |

### 1.4 单据关联

| 方法 | 路径 | 权限 | 说明 |
|---|---|---|---|
| GET | `/projects/:id/records?type=&status=&cursor=&limit=` | `project:read` | 聚合读（按域批量，防 N+1） |
| POST | `/tickets/:id/project` 等五域 | 域写码 ∩ `project:write` | 挂接（契约 §3.1） |
| DELETE | `/tickets/:id/project` 等五域 | 同上 | 解除 |

### 1.5 P1（实施中）

**已实现（PE-14，2026-10-18）**：`GET|POST /projects/:id/milestones`、`PUT|DELETE /projects/:id/milestones/:milestoneId`、`POST /projects/:id/milestones/:milestoneId/status`（reach|miss）、`GET /projects/:id/dependencies`、`POST /projects/:id/tasks/:taskId/dependencies`（v1 仅 FS）、`DELETE /projects/:id/tasks/:taskId/dependencies/:dependsOnTaskId`。

**已实现（PE-13，2026-10-18）**：`GET /projects/:id/baselines`（version 倒序、可见性谓词）、`POST /projects/:id/baselines`（冻结不可变快照：任务计划/里程碑/项目计划面；终态项目拒绝；v1 reason 可选、v≥2 必填；版本唯一索引 + 并发重试）、`GET /projects/:id/baselines/diff`（最新基线 vs 当前实时偏差：added/removed/changed/unchanged + 计划日期偏移天数 + 进度差；无基线 404）。

**已实现（P1 前端批次，2026-10-17）**：项目工作台 `/projects/:id` 新增「里程碑」Tab（计划日期逾期预警、达成/错过/编辑/删除）与「计划基线」Tab（冻结基线 + 版本表 + 最新基线偏差汇总与任务/里程碑偏差明细，未变项折叠），任务行新增「依赖」弹窗（前置任务增删，候选排除自身与已添加）；`lib/services/project-service.ts` 新增里程碑/依赖/基线 13 个方法与类型（单测 16/16、`check-api-paths` 1040/1040、生产构建通过）。

**已实现（PE-11，2026-10-19）**：`GET|POST /projects/templates`、`GET|PUT|DELETE /projects/templates/:templateId`、`POST /projects/:id/templates/apply`（一键生成阶段/任务/检查单/里程碑；一项目一模板，重复应用 409 幂等不产生重复数据；模板须 active；锚点=项目开始日期）。详细设计见 [p1-template-design.md](./p1-template-design.md)。

**已实现（PE-12，2026-10-19）**：`GET /projects/:id/gates`（`{items, gates[], projectGates[]}`）、`POST /projects/:id/gates/:gate/submit`（门禁规则 → 409 + `blockers`；通过触发 BPMN 审批 `in_review`，无绑定降级自动通过 `passed`）、`POST /projects/:id/gates/:gate/decision`（`approve|reject`；reject 必填原因）；`activate` 强制立项门、`complete` 强制结项门；计划门通过自动冻结基线 v1。详细设计见 [p1-gate-design.md](./p1-gate-design.md)。

**已实现（PE-15，2026-10-21）**：`GET /projects/:id/closure`（结项清单：自动项实时派生 + 人工/产物项落库；一次返回全部未通过项与 `blockers`）、`POST /projects/:id/closure/retro`（复盘 KB 草稿，幂等、不自动发布）、`POST /projects/:id/closure/assets`（`confirm|waive`，豁免必填原因）、`POST /projects/:id/closure/members/recycle`（回收 member 角色；保留 PM 与干系人）、`POST /projects/:id/closure/csat`（幂等重试）；`complete` 依序校验 未终结任务/单据 → 结项门 → 清单齐备，结项成功尽力触发 CSAT（不阻塞）。详细设计见 [p1-closure-design.md](./p1-closure-design.md)。

**已实现（PE-16，2026-10-22）**：`GET /projects/my-milestones`（我负责的 planned 里程碑，默认 30 天内含逾期、≤200；含 `projectName/projectCode/overdue/daysLeft`）；`global_search` 纳入 `project`/`project_task`（强制可见性谓词）。详细设计见 [p1-search-design.md](./p1-search-design.md)。

**已实现（PE-14 收尾，2026-10-23）**：里程碑到期通知（无新增 HTTP 端点；租户级扫描器入箱 `notification.deliver`，`resource_type=project_milestone`）。详细设计见 [p1-milestone-notification-design.md](./p1-milestone-notification-design.md)。

**已实现（PE-17，2026-10-23）**：`GET|POST /projects/report-templates`、`GET|PUT|DELETE /projects/report-templates/:templateId`（模板 CRUD：`status/q` 过滤 + 分页、名称唯一 409、version 乐观锁）、`GET /projects/report-groups`（按归口团队聚合 + 健康度分布）、`POST /projects/:id/reports/generate`（实时派生 `{markdown, data}`，不落快照；归档模板 409）。详细设计见 [p1-report-design.md](./p1-report-design.md)。

**待实现**：P1 收口的真实库迁移/RLS/UI 验证（无待开发 API）。（模板管理页 `/projects/templates`、工作台「应用模板」「阶段门」「结项清单」「状态报告」入口与「报表中心」页已随对应批次落地。）

## 2. 关键示例

**结项门禁阻塞（409）**

```json
{
  "code": "PROJECT_CLOSURE_BLOCKED",
  "message": "存在未终结的关联记录，无法结项",
  "data": {
    "blockers": [
      { "type": "change", "id": 42, "number": "CHG-0012", "status": "in_progress" },
      { "type": "task",   "id": 88, "title": "上线演练",     "status": "in_progress" }
    ],
    "total": 2
  }
}
```

**成员返回（派生标签）**

```json
{
  "userId": 31, "name": "李工", "projectRole": "member",
  "memberOrigin": "allocation", "expiresAt": "2026-11-01T00:00:00Z"
}
```

**任务状态流转**：`POST /projects/5/tasks/88/status` `{"action":"start"}` → 200；并发/非法 → 409；非成员/无权限 → 403。

## 3. 错误码表

| 场景 | HTTP | code（示例） |
|---|---|---|
| 参数/枚举非法、深度超限、依赖成环 | 400 | `PROJECT_VALIDATION_FAILED` |
| 无权限/不可见 | 403 | `ACTION_NOT_ALLOWED` / `PROJECT_NOT_VISIBLE` |
| 项目/任务/单据不存在（含跨租户） | 404 | `PROJECT_NOT_FOUND` |
| 状态机冲突、并发 CAS、门禁阻塞、重复生成工单 | 409 | `STATE_CONFLICT` / `PROJECT_CLOSURE_BLOCKED` / `TASK_TICKET_EXISTS` |

> 说明：以上为 `common.AppError` 风格字符串码（对齐 `common/app_error.go`）；补充业务码与 metadata 约定见契约 §2。

## 4. 前端页面与路由

| 路由 | 页面 | 权限 | 说明 |
|---|---|---|---|
| `/projects` | 列表（重写现有页） | `project:read` | 去占位进度列；过滤（状态/类型/归口/负责人/关键字）；`canWrite` 控制操作 |
| `/projects/:id` | 工作台 | `project:read` | Tabs：概览 / 任务 / 关联单据 / 成员 / 附件 / 动态；P1 增里程碑/基线/风险 |
| （无独立路由） | 个人工作台卡片 | `project:read` | "我的项目任务"聚合（复用 dashboard 组件） |
| `/projects/templates`（P1） | 模板管理 | `project:write` | 模板 CRUD 与一键生成 |
| `/projects/reports`（P1，PE-17） | 报表中心 | `project:read`（写操作 `project:write`） | 归口分组 + 报告模板管理；报告在项目工作台「状态报告」Tab 生成/导出 |

> 落地说明（PE-16，2026-10-22）：个人工作台卡片已落地为 `/dashboard`「我的项目任务」区块（`project:read` 门控；`GET /projects/my-tasks` + 新增 `GET /projects/my-milestones` 并行聚合，含里程碑到期与逾期标记；局部降级不影响仪表盘其他分区）；全局搜索新增 `project`/`project_task` 结果且**强制可见性谓词**。设计与验证证据见 [p1-search-design.md](./p1-search-design.md)。

**登记点（四处同步，缺一不可）**：
1. `itsm-frontend/src/routes/route-paths.ts`（新增 `/projects/:id`）；
2. `itsm-frontend/src/routes/index.tsx`（lazy import 详情页）；
3. `itsm-frontend/src/routes/route-permissions.ts` + 页面 `useCan(['project:write'])` 门禁；
4. 菜单 seed（`pkg/seeder`，对齐 `/msp/*` 菜单补齐先例）+ `lib/i18n/translations.ts`。

**页面结构建议**：列表页沿用 `PageContainer` + AntD Table；详情页头部（名称/编号/状态/健康度/进度/负责人）→ 状态操作按钮（受 `project:write` + 状态机允许动作控制）→ Tabs 内容。任务 Tab 提供 列表/看板 切换（P0 先列表 + 树，看板可选）。

## 5. 页面-权限-数据映射

| 页面元素 | 门禁 | 数据来源 |
|---|---|---|
| 创建/编辑/删除按钮 | `project:write` / `project:delete` | — |
| 状态操作（激活/暂停/结项） | `project:write` + `allowedActions` | 详情接口返回 `allowedActions`（服务端计算，前端不推导） |
| 成员管理 | `project:write` | `/members`、`/member-candidates` |
| 任务操作 | `project:write` | `/tasks`；指派候选来自成员列表 |
| 生成工单 | `project:write` ∩ `ticket:create` | 前端按 `/auth/me` 权限面隐藏按钮 |

## 6. 前端验收要点

- [ ] 列表/详情/任务/成员/关联单据五个视图可用；占位数据全部移除；
- [ ] 无 `project:read` 用户菜单与路由不可达（403 页）；非成员不可见项目不在列表/搜索出现；
- [ ] 结项被阻塞时展示 blockers 明细（类型/编号/状态，可跳转单据）；
- [ ] 成员候选显示"外部/服务方"标签与过期预警（≤7 天）；
- [ ] `npm run type-check`、`npm run lint`、`npm run lint:antd` 通过；页面测试覆盖列表与详情关键路径。

## 7. 与实施项的对应（PE 映射）

PE-2/PE-3/PE-4/PE-5 对应 §1 各分组 API；PE-6 对应 §4 页面；PE-7 对应 §6 与契约 §8 测试矩阵；PE-8 文档同步（`docs/api/API_REFERENCE.md`、`CHANGELOG`、README 成熟度表）。