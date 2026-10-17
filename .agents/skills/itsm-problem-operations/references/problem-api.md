# 问题管理 REST API 契约（references/problem-api.md）

> 基线：2026-10-17（router/problem_routes.go，24 路由）。全局信封 `{code,message,data}`；成功 `code=0`。

## 0. 认证 / 权限

- 读 `problem:read` / 写 `problem:write` / 删除 `problem:delete`。
- 行级写权限：仅创建人 / 受理人 / 管理员（`datascope.CanWriteResource`），违规 403。

## 1. 状态机（单一事实源 `handlers/problem/service.go:isValidProblemStatusTransition`）

状态：`open → investigating/identified/resolved → closed`；`closed` 终态。存量 `in_progress` 兼容（仅可进入 identified/resolved）。

```
open          → investigating, identified, resolved
investigating → identified, resolved
identified    → investigating, resolved
resolved      → investigating(重开), closed
closed        → （终态，无转换）
```

- 违规转换 → **409/BusinessError**（`当前问题状态不允许此操作`）；跳过分析直接 closed 必被拒（单测锁定）。
- 状态时间戳自动维护：首次进入 resolved 写 `resolvedAt`（MTTR 源）、closed 写 `closedAt`；回到 investigating 清空。

## 2. 端点契约

| 端点 | 契约要点 |
|---|---|
| `GET /problems` | query：`page/pageSize/status/priority/category/keyword/dateFrom/dateTo/sortBy/sortOrder`（form 绑定 camelCase） |
| `POST /problems` | `title`(2-200 必填)、`description`(10-20000 必填)、`priority`(必填 low/medium/high/critical)、`category/rootCause/impact/impactScope` |
| `GET/PUT/DELETE /problems/:id` | Update 指针字段（含 `status`，走状态机）；`description` 更新时同样 ≥10 字符 |
| `GET /problems/stats`、`/trend`、`/hotspots` | 统计/趋势/热点 |
| `POST /problems/:id/investigate` | **无 body**（open→investigating） |
| `PUT /problems/:id/root-cause` | `{rootCause(必填)}` |
| `PUT /problems/:id/solution` | `{solutionType?, solutionDescription?, priority?, status?, estimatedEffortHours?, estimatedCost?, riskAssessment?, approvalStatus?}`（指针字段；solutionDescription/workaround 至少其一在 service 层另一入口） |
| `POST /problems/:id/close` | `{resolution?}`；仅 resolved 可 close |
| `GET /problems/:id/sla` | 问题 SLA 视图 |
| `GET/POST /problems/:id/comments`、评论 | POST `{content}` |
| `GET/POST/DELETE /problems/:id/associations` | 关联：`{relatedType(oneof ticket/incident/change), relatedIds[]}`；移除 `{relatedType, relatedId}`（DELETE 带 body） |
| 附件 | `/problems/:id/attachments`（域内别名，multipart `file`，宿主权限码 problem 族） |

## 3. GAPS（调用方注意）

1. **状态流转无专用端点**：全部走 `PUT /problems/:id` 的 `status` 字段（与 ticket 的专用 resolve/close 端点不同）；investigate/close 是语义化快捷端点。
2. **association-remove 用 DELETE + JSON body**（非 query）。
3. `UpdateProblemSolutionRequest` 的 `solutionType/status` 为枚举指针（`SolutionType`/`SolutionStatus`，DTO 定义在 problem 域），传非法值 400。
4. 列表 `dateFrom/dateTo` 为 RFC3339。
5. 统计/趋势/热点响应为自由结构（图表契约），脚本直通。
