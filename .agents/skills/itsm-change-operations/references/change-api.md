# 变更管理 REST API 契约（references/change-api.md）

> 基线：2026-10-17（router/change_routes.go 35 路由 + cab_routes 4 + approval_chain_routes 6）。全局信封 `{code,message,data}`；成功 `code=0`；错误码映射同全站。

## 0. 认证 / 权限

- 认证同全站（PAT/JWT Bearer）。**权限码分四类**（H-15 收口，write 不再泛化）：
  - 读：`change:read`；CRUD/流转：`change:write`；**审批：`change:approve`**（approve/reject）；**回滚：`change:rollback`**
- 行级写权限（DataScope）：非审批/回滚动作仅 创建人 / 被指派人 / 管理员 可执行（`datascope.CanWriteResource`）。

## 1. 状态机（单一事实源 `service.IsValidChangeStatusTransition`）

状态：`draft → pending(=submitted 等价别名) → approved/rejected → scheduled → in_progress → completed/failed → closed`；`rolled_back → closed`；`cancelled` 终态。

**按类型分表**（合并非空转换）：

| 当前 \ 目标 | normal（默认严格 ITIL） | standard（预授权可跳步） | emergency（可跳过排期） |
|---|---|---|---|
| draft | submitted, cancelled | submitted, approved, scheduled, in_progress, cancelled | submitted, approved, in_progress, cancelled |
| submitted(pending) | approved, rejected, cancelled | 同左 | 同左 |
| approved | scheduled, cancelled | scheduled, in_progress, cancelled | in_progress, cancelled |
| scheduled | in_progress, cancelled | 同左 | in_progress, cancelled |
| in_progress | completed, failed, rolled_back, cancelled | 同左 | 同左 |
| failed | scheduled, rolled_back, cancelled | 同左 | 同左 |
| completed | closed | closed | closed |
| rolled_back | closed | closed | closed |
| rejected / cancelled / closed | 终态（无转换） | 同左 | 同左 |

- 转换非法 → **409**（`ErrInvalidTransition`）；并发冲突（审批已被处理）→ **409**；非审批人 → **403**。
- `pending` 与 `submitted` 等价（历史别名，入口归一）。

## 2. 端点契约

| 端点 | body / query | 说明 |
|---|---|---|
| `GET /changes` | query：`page(1)/pageSize(10)/status/search/riskLevel(兼容 risk_level)` | 响应 `{changes[], total, page, pageSize}`（**键是 changes**，非 items） |
| `POST /changes` | `CreateChangeRequest`：`title`(必填)、`description/justification`、`type`(normal/standard/emergency)、`priority`(low/medium/high/critical)、`impactScope/riskLevel`(low/medium/high)、`plannedStartDate/plannedEndDate`(RFC3339)、`implementationPlan/rollbackPlan`、`affectedCis[]`(字符串)、`relatedTickets[]` | 创建即 draft |
| `GET /changes/:id`、`PUT /changes/:id`、`DELETE /changes/:id` | Update 为指针字段（同 Create 字段，均可选） | |
| `GET /changes/stats` | — | 统计 |
| `POST /changes/:id/submit` | `{approverIds?: int[], comment?}` | 提交审批（draft→submitted，可指定审批人链） |
| `POST /changes/:id/assign` | `{assigneeId: int}`（必填） | 指派（需行级写权限） |
| `POST /changes/:id/{approve,reject,schedule,start,complete,close,rollback,cancel}` | `{comment?}` 或 `{reason?}`（任一，服务端归并为 comment） | **目标状态由 URL 末段决定**；approve/reject 需 `change:approve` 且为链上未决审批人；rollback 需 `change:rollback`；与 BPMN 桥接（change_normal_flow / change_emergency_flow） |
| `GET/POST /changes/:id/approvals` | POST：`{changeId(必填), approverId(必填), comment?}` | 审批记录列表 / 登记 |
| `GET /changes/:id/approval-summary` | — | 审批汇总 |
| `GET /changes/:id/risk`（别名 risk-assessment）、`PUT /changes/:id/risk` | `ChangeRiskAssessment`：`riskLevel/riskDescription/impactAnalysis/mitigationMeasures/contingencyPlan/riskOwner/riskReviewDate` | 风险评估 |
| `GET /changes/:id/cmdb-impact` | — | 关联 CI 影响面汇总 |
| `GET /changes/calendar` | query：`startDate/endDate`(必填 YYYY-MM-DD)、`status` | 变更日历 |
| `GET /changes/pirs`、`GET /changes/:id/pir`、`POST /changes/:id/pir`、`PUT/DELETE /changes/pir/:id` | Create：`overallResult`(必填 oneof `successful/partially_successful/failed`)、`objectivesAchieved/actualStartTime/actualEndTime/rollbackPerformed…`；Update：同字段指针 | PIR 实施后评审 |
| 附件 | `/changes/:id/attachments`（GET/POST/GET :ref/download|preview/DELETE :ref） | 域内别名，宿主权限码 change 族，multipart `file` |

## 3. CAB（/api/v1/cab，权限 change:read/write）

`GET/POST /cab/members`、`PUT/DELETE /cab/members/:id`。

## 4. 审批链（/api/v1/approval-chains，权限 approval:read/write）

`GET ""`、`GET /stats`、`POST ""`、`GET /:id`、`PUT /:id`、`DELETE /:id`（独立 approval 权限族，非 change 族）。

## 5. GAPS（调用方注意）

1. **列表响应键为 `changes`**（非 items）——脚本直通输出。
2. **流转动作靠 URL 末段路由**：`POST /:id/approve` 等 8 个端点共用 handler；body 仅 `{comment|reason}`，传其他字段被忽略。
3. **审批动作双重校验**：路由权限 `change:approve` + 链上「未决审批人」行级校验；非审批人 403，审批已被他人完成 409。
4. transition 与 BPMN 流程桥接（change_normal_flow/emergency_flow）；桥接失败会阻断业务流转（fail-closed），排障时需同时看流程实例状态。
5. `CreateChangeRequest.affectedCis/relatedTickets` 为**字符串数组**（CI 编号/工单号，非数字 ID）。
6. `UpdateRisk`/PIR 等 DTO 含 `id/changeId` 字段但以 URL 为准；riskReviewDate 为 RFC3339。
7. **`CreateChangePIRRequest.changeId` 必须 int（2026-10-17 实测）**：传字符串 400 `cannot unmarshal string into ... int`——脚本已强制 int(id)。
8. **标准链路实测（2026-10-17，standard 类型 + demo admin）**：create(draft)→submit(pending)→approve(approved)→schedule→start(in_progress)→complete(completed)→close(closed) 全通；closed 后再 `start` 返回 **409/4090**（终态守卫生效）；`GET /changes/calendar` 响应键 `{items,total}`；`GET /changes/stats` 键为 `{total,pending,approved,scheduled,inProgress,completed,failed,rolledBack,rejected,cancelled}`。
9. `GET /cab/members` 空成员时响应为 `{ok:true}`（无 items 键）——调用方按两形态兼容。
