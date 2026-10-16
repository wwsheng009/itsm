# 工单 REST API 完整契约（references/ticket-api.md）

> 基线：2026-10-16 工作树（`d8854f2f` + 当日乐观锁/行级枚举修复）。行号会漂移，以符号定位。
> 全局：Base `/api/v1`；信封 `{code,message,data}`；成功 `code=0`；字段一律 camelCase。
> 错误码映射：1001/1002/4000→400，2001/2002→401，2003/2004→403，4004→404，4090→409，4220→422，5001→500。
> 范围：本文聚焦核心工单操作；完整路由面（workflow/审批/自动化/评分/通知/依赖/导出等长尾）见 `router/ticket_routes.go`。

## 0. 认证 / 租户 / 限流

- `Authorization: Bearer itsm_pat_…`（PAT，个人中心创建）或 Bearer JWT；全部工单端点均接受 PAT（PAT 分支在 AuthMiddleware 内合成与 JWT 相同身份：user_id/username/role/tenant_id）。
- 登录换 JWT：`POST /auth/login {username,password,tenantCode?}`。**响应体不含 token**（`AuthResult.AccessToken` 为 `json:"-"`），JWT 仅经 httpOnly cookie（`Set-Cookie: access_token=…`）下发；脚本调用方从 Set-Cookie 提取后走 Bearer（Bearer 免 CSRF；cookie 写请求需先取 `X-CSRF-Token`）。
- 脚本封装：`scripts/itsm_ticket.py`（零依赖 Python 3）——login 提取 cookie token、统一 Bearer、update 409 读-改-写重试、multipart 上传、formFields 必填提示；优先复用，避免重复实现。
- 租户：令牌绑定租户为最强来源，**不需要** `X-Tenant-Code`；传不一致的 header → 401 `TENANT_MISMATCH_REJECTED`。租户须 active 未过期。
- 限流：全局按 IP 500/min（内存回退）；PAT 单令牌默认 120/min → 429。请求体上限 10MB。
- 行级权限（datascope）：admin/super_admin/manager 见全租户；其他角色**列表**仅「本人创建或被指派」（2026-10-16 修复枚举转换回归）。注意：**详情与评论读取目前未套行级过滤**（同租户可见），见 GAPS-12。

## 1. 创建

`POST /api/v1/tickets`（权限 `ticket:create`）—— `dto.CreateTicketRequest`：

| 字段 | 类型 | 必填 | 说明 |
|---|---|---|---|
| title | string | ✅ | 2–200 |
| description | string | ❌ | ≤5000；`descriptionHtml` ≤512KB（服务端 sanitize） |
| priority | string | ✅ | low/medium/high/critical/urgent（缺省 medium） |
| type | string | ❌ | incident(默认)/service_request/change/ticket/problem/improvement |
| ticketTypeId | int | ❌ | 平台类型 ID（来源 `GET /ticket-types`） |
| typeId | string | ❌ | legacy 配置类型 code |
| category / categoryId | string / int | ❌ | categoryId 优先 |
| templateId | int | ❌ | |
| assigneeId | int | ❌ | >0 时指定；缺省走一线分派 |
| parentTicketId | int | ❌ | 子任务挂父单 |
| tagIds / tags | []int / []string | ❌ | 按名解析 |
| formFields | object | ❌ | 自定义表单 |
| workflowDefinitionKey | string | ❌ | 显式流程键（优先于类型配置） |
| attachments | []string | ⚠️ | **死字段：未接线**，勿依赖；附件创建后补传 |

响应：`TicketResponse`（`ticketNumber` 如 `TKT-202610-000053`；含 `status/priority/type/ticketTypeId/assigneeId/version/slaResponseDeadline/…`；创建响应可能不回填 `ticketTypeId`，以详情回读为准）。租户配额超限 → 422 `{reasonCode:"TENANT_QUOTA_EXCEEDED", quota, limit, used}`。
> 带 `ticketTypeId` 创建时，该类型配置的必填自定义字段需在 `formFields` 提供（key = `customFields[].id`/`name`，如通用事件 `symptom`/故障现象）；缺失 → 1001「字段 X 为必填项」，传未定义 key → 1001「包含未定义字段」。

## 2. 元数据（创建前）

| 端点 | 权限 | 说明 |
|---|---|---|
| `GET /ticket-types`（分页 page/pageSize） | `ticket:read` | **平台工单类型**：`ticketTypeId` 用这里的 id |
| `GET /ticket-types/:id` | `ticket:read` | 类型详情 |
| `GET /tickets/types` | `ticket:read` | legacy 硬编码枚举（id 1–6），**与平台 id 不一致，勿混用** |
| `GET /tickets/templates`、`/tickets/templates/:id`、`/tickets/templates/categories` | `ticket_template:read` | 模板（含 fields/formFields/workflowSteps） |
| `GET /ticket-categories`、`/ticket-categories/tree`、`/ticket-categories/:id` | `ticket_category:read` | 分类 |
| `GET /ticket-tags`（别名 `GET /tags`） | `ticket_tag:read` | 标签 |
| `GET /users`（选处理人） | `user:read` | 管理员用于找 assigneeId |
| 优先级 | — | 无元数据端点；枚举 low/medium/high/critical（创建额外收 urgent） |

> end_user 默认绑定仅含 `ticket:read/write/create/update` + `user:read` 等；**无** template/category/tag 读码 → 这些元数据端点对 end_user 403，调用方应降级（不传对应字段）。

## 3. 列表 / 搜索 / 统计

- `GET /tickets`（`ticket:read`）——查询参数（form 绑定，camelCase）：`page, pageSize, status, priority, type, category, categoryId, assigneeId, requesterId, parentTicketId, templateId, keyword, dateFrom, dateTo(RFC3339), isOverdue, sortBy(created_at|updated_at|title|status|priority|ticket_number), sortOrder(asc|desc)`。响应 `items`（=`tickets` 别名）+ `total/page/pageSize/totalPages/pagination{...}`。
- `GET /tickets/search?q=<关键字>`（参数名是 **`q`**）——返回裸数组。
- `GET /tickets/stats` → `{total, open, inProgress, resolved, pending, highPriority, overdue}`。
- `GET /tickets/overdue` → 裸数组。
- 视图：GET/POST `/tickets/views`、GET/PUT/DELETE `/tickets/views/:id`（写 `ticket:write`）。

## 4. 详情与关联

- `GET /tickets/:id`（`ticket:read`）——`:id` 接受数字 ID **或工单号**（如 `TKT-202609-000010`）。
- `GET /tickets/:id/history` —— 活动时间线（创建/评论/指派/首次响应/解决合并），项含 `action/details/oldValue/newValue/userName/createdAt`。
- `GET /tickets/:id/sla` —— `ticketId/slaDefinitionId/slaResponseDeadline/slaResolutionDeadline/firstResponseAt/status/resolutionDeadlineBreached`（含 legacy snake_case 双写，deprecated）。
- SLA 暂停/恢复：`PUT /tickets/:id/sla/pause`（body `{reason}` 必填）/ `resume`（`ticket:update`）。
- 子任务：GET/POST `/tickets/:id/subtasks`、PATCH/DELETE `/tickets/:id/subtasks/:subtask_id`。
- 关系：GET `/tickets/:id/relations`、POST/DELETE `/tickets/relations`（`ticket:update`）。
- CC：GET `/tickets/:id/cc`、`/tickets/cc/my`（`workflow:read`）。

## 5. 更新 / 状态机

- `PUT`/`PATCH /tickets/:id`（`ticket:update`）—— `dto.UpdateTicketRequest`：title/description/descriptionHtml/priority(low–critical)/status/type/categoryId/assigneeId/tags/resolution/formFields/**version（乐观锁，0=取当前）**。非法转换 400；terminal（resolved/closed）必须走专用端点（400 `TICKET_TERMINAL_STATUS_REQUIRES_DEDICATED_ENDPOINT`）；版本冲突 409 + `{ticketId, currentVersion, serverVersion}`（2026-10-16 修复：repo/service 全链路类型化 `VersionConflictError`；此前曾回落 500/5001）。
- `PUT /tickets/:id/status`（`ticket:update`）—— body **仅 `{status}`**（无评论字段）。
- `POST /tickets/:id/assign`（`ticket:assign`）—— `{assigneeId}` >0。
- `POST /tickets/:id/resolve`（`ticket:update`）—— `{resolution?|solution?|resolutionCategory?|workNotes?}`；当前状态须可 → resolved。
- `POST /tickets/:id/close`（`ticket:update`）—— `{closeReason?, closeNotes?, feedback?}`；仅自 resolved。
- `POST /tickets/:id/escalate`（`ticket:escalate`）—— `{reason}` 必填。
> PUT/PATCH 为部分更新语义：仅字段非零值时下发。注意绑定下限——`title` min=2，**更新时 `description` min=10**（短于 10 字符会被 400 拒绝）。

**状态机**（单一事实源 `common.IsValidTicketStatusTransition`，common/constants.go:146）：

```
new        → open | assigned | in_progress | resolved | cancelled
assigned   → in_progress | pending | resolved | cancelled
open       → in_progress | pending | resolved | cancelled
in_progress→ resolved | pending | cancelled
pending    → in_progress | resolved | open | cancelled
resolved   → closed | in_progress | open
approved   → in_progress | resolved | closed
rejected   → open | cancelled
closed / cancelled → 终态（同状态重放也拒绝）
```

## 6. 回复 / 评论

`/api/v1/tickets/:id/comments`（嵌套路径）：

| 端点 | 权限 | 说明 |
|---|---|---|
| GET `/:id/comments` | `ticket:read` | `{items, total}` |
| POST `/:id/comments` | `ticket:create` | `{content(1–5000), isInternal(内部备注), mentions[]int, attachments[]int(≤10)}` |
| PUT `/:id/comments/:comment_id` | `ticket:update` | content?/isInternal?/mentions?/attachments?（nil=不改，[]=清空，列表=全量替换） |
| DELETE `/:id/comments/:comment_id` | `ticket:delete` | |

响应含 `attachmentRefs[{id,fileName,fileSize,mimeType,downloadUrl,previewUrl?}]`（域内下载地址）。

## 7. 附件（先上传后绑定）

| 面 | 端点 | 权限 | multipart 字段 |
|---|---|---|---|
| 工单域 | `POST /tickets/:id/attachments` | `ticket:create` | `file`（必填）+ 可选 `usage`、`clientToken`(≤64) |
| 工单域 | `GET /tickets/:id/attachments`（`{attachments,total}`）、`GET /tickets/:id/attachments/:aid[/download|/preview]`、`DELETE /tickets/:id/attachments/:aid` | `ticket:read`/`ticket:delete` | — |
| 通用 | `POST /attachments`（`bizType=ticket&bizId={id}`）、`GET /attachments?bizType&bizId`、`GET /attachments/:id[/content]`、`POST /attachments/batch-query`、`DELETE /attachments/:id` | `attachment:write`/`read`/`delete` | `file` + `bizType` + `bizId` + 可选 `usage`/`clientToken` |

限制：≤10MB；MIME/扩展白名单 + 内容嗅探（违规 415/6104）；引用中（被正文/评论引用）删除 → 409/6105。响应 `AttachmentRef{id,bizType,bizId,usage,fileName,fileSize,mimeType,fileUrl?,previewUrl?,sha256?,createdAt?}`。成功一律 HTTP 200 code=0。
> 评论附件只接受**通用附件面**（`POST /attachments`，`bizType=ticket&bizId={id}&usage=comment_attachment`）上传的 ID；工单域 `/tickets/:id/attachments` 的附件 ID 会被评论绑定校验拒绝（`validateCommentAttachments`）。

## 8. 错误码速查

| 场景 | HTTP | code |
|---|---|---|
| 未认证 / PAT 无效或已吊销 | 401 | 2001 |
| 无权限 / 越权（行级） | 403 | 2003 |
| 状态机非法转换 / 参数错 | 400 | 1001/4000 |
| 工单不存在 | 404 | 4004 |
| 乐观锁版本冲突 | 409 | 4090 |
| 租户配额超限 | 422 | 4220 |
| PAT 限流 | 429 | 429 |
| 附件过大/类型/配额/限流 | 413/415/422/429 | 6103/6104/6106/6107 |

## 9. GAPS（现状缺口，调用方注意）

1. `CreateTicketRequest.attachments` 死字段——创建后走 `/tickets/:id/attachments` 补传。
2. 状态变更无评论字段——先评论后流转。
3. `/tickets/search` 仅支持 `q`，无分页/状态过滤。
4. `/tickets/export` 未实现（恒 500），勿调用。
5. `/tickets/:id/sla` 含 legacy snake_case 双写（已并行下发 camelCase， migrating 中）。
6. 无优先级矩阵元数据端点（枚举硬编码）。
7. end_user 默认无 template/category/tag 元数据读码。
8. `/tickets/types` 硬编码 id ≠ 平台 `ticketTypeId`。
9. WS 通知不走 Bearer：`POST /api/v1/ws/ticket`（PAT 可用）换短期票据 → `GET /ws/notifications?ticket=…`。
10. （已修复 2026-10-16）ticket 域乐观锁冲突此前回落 500/5001：现 `repository/ticket/repository_impl.go` 的版本检查与 CAS 未命中均返回 `common.VersionConflictError`，service/handler 经 `errors.As` 透传 → 409/4090 + `{ticketId,currentVersion,serverVersion}`；回归测试 `TestHandler_Update_VersionConflict409`、`TestRepository_Update_RejectsStaleVersion`。
11. 登录响应体不含 token（`AccessToken` 为 `json:"-"`），JWT 仅经 httpOnly cookie 下发；外部脚本应从 `Set-Cookie` 提取后走 Bearer。
12. 行级范围边界（2026-10-16 部分修复）：**列表**对非管理角色强制「本人创建/被指派」（修复前跨包枚举直接转换 `datascope.OwnedOrAssigned=2 → ticket.DataScope=0=All` 使收窄静默失效；回归 `TestHandler_List_DataScopeNarrowing`、`TestRepository_List_DataScopeOwnedOrAssigned`）；**详情 GET /:id 与评论读取未套行级过滤**（同租户可见/可评，closed 仍可评）；写侧 PUT/DELETE 越权 403（`enforceTicketRowScope`）。
