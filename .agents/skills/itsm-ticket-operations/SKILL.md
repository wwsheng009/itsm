---
name: itsm-ticket-operations
description: 通过 ITSM REST API 操作工单业务单据（创建工单/查询元数据/搜索列表/状态流转/回复评论/附件管理）。Use when the agent needs to 调用 ITSM 工单 API、创建工单、搜索/列出工单、更新工单状态、回复工单、上传工单附件、查询工单类型/模板/分类等元数据，或为自动化脚本/CI 生成工单调用代码；covers POST/GET/PUT /api/v1/tickets 及 comments/attachments/metadata 端点，含认证（PAT/JWT）、权限码、状态机与字段契约。Do NOT use for 部署（itsm-deploy）或工单模块代码开发（.trae/skills/ticket-module-dev）。
---

# ITSM 工单业务操作（API 调用手册）

面向**外部调用方**（自动化脚本 / CI / CLI / Agent）通过 REST API 操作工单。认证用个人访问令牌（`Authorization: Bearer itsm_pat_…`，在个人中心 → 安全设置创建）或 JWT；PAT 的权限与所属用户完全一致。所有端点在 `Authorization` 头下工作，**租户绑定令牌无需 `X-Tenant-Code`**（传了且不一致会被 401 拒绝）。

完整端点表、字段契约、错误码与示例：先读 [references/ticket-api.md](./references/ticket-api.md)。部署/环境：见 `itsm-deploy` 技能。

## 核心流程：创建一张工单

1. **取元数据**（创建前按需）：
   - 工单类型：`GET /api/v1/ticket-types`（平台类型，`ticketTypeId` 用这里的 id；`GET /tickets/types` 是 legacy 硬编码枚举，id 与平台不一致，勿混用）
   - 模板/分类/标签：`GET /tickets/templates`、`GET /ticket-categories`、`GET /ticket-tags`（需 `ticket_template/ticket_category/ticket_tag:read`——**end_user 默认无**这些码，403 时降级为不传对应字段）
2. **（可选）上传附件**：`POST /api/v1/tickets/{id}/attachments`（multipart，字段名 `file`，≤10MB）或创建后补传；创建请求体里的 `attachments` 字段**未接线，勿依赖**
3. **创建**：`POST /api/v1/tickets`，必填 `title`（2-200）+ `priority`（low/medium/high/critical/urgent）；可选 `description`、`type`（incident 默认）、`ticketTypeId`、`categoryId`、`assigneeId`、`formFields`。响应 `data.ticketNumber`（如 `TKT-202610-000053`）
4. **核验**：`GET /api/v1/tickets/{ticketNumber}`（`:id` 同时接受数字 ID 与工单号）

## 常用操作速查

```bash
BASE=http://localhost:8090/api/v1; PAT="Authorization: Bearer itsm_pat_xxx"
# 列表（分页 + 过滤）
curl -H "$PAT" "$BASE/tickets?page=1&pageSize=20&status=open&priority=high&assigneeId=9&dateFrom=2026-01-01T00:00:00Z"
# 关键字搜索（注意参数是 q）
curl -H "$PAT" "$BASE/tickets/search?q=磁盘"
# 统计 / 超时
curl -H "$PAT" "$BASE/tickets/stats";   curl -H "$PAT" "$BASE/tickets/overdue"
# 详情 + 时间线 + SLA
curl -H "$PAT" "$BASE/tickets/TKT-202610-000053";  curl -H "$PAT" "$BASE/tickets/1/history";  curl -H "$PAT" "$BASE/tickets/1/sla"
# 回复（isInternal=true 为内部备注） / 附件列表
curl -H "$PAT" -H 'Content-Type: application/json' -X POST "$BASE/tickets/1/comments" -d '{"content":"已定位","isInternal":false}'
curl -H "$PAT" "$BASE/tickets/1/attachments"
# 状态流转（状态机见 references；resolved 需先有 resolution）
curl -H "$PAT" -H 'Content-Type: application/json' -X POST "$BASE/tickets/1/resolve" -d '{"resolution":"更换磁盘"}'
curl -H "$PAT" -H 'Content-Type: application/json' -X POST "$BASE/tickets/1/close"   -d '{"closeReason":"已恢复"}'
```

## 决策要点（硬规则）

- **状态机单一事实源** `common.IsValidTicketStatusTransition`（常见链：`new → in_progress → resolved → closed`；`closed/cancelled` 终态，同状态重放也拒绝；PUT 改 status 到 resolved/closed 被 400 拒绝，必须走 `/:id/resolve`、`/:id/close`）
- **PUT 更新带 `version` 乐观锁**，冲突返回 409 + `{ticketId, currentVersion, serverVersion}`——自动化应读-改-写并处理 409 重试
- **更新状态不带评论字段**；要在流转时留言 → 先 `POST /:id/comments` 再 `PUT /:id/status`
- 行级权限：普通角色只能操作**本人创建或被指派**的工单；越权 403
- 附件两套面：工单域 `POST /tickets/:id/attachments`（`ticket:create`）与通用 `POST /attachments`（`attachment:write`，`bizType=ticket&bizId={id}`）；评论附件是「先上传后绑定」（评论体 `attachments` 传 ID，≤10 个）
- 错误语义：`{code,message,data}`；2001 未认证 / 2003 无权限（含越权）/ 4004 不存在 / 4090 乐观锁冲突 / 4220 配额超限
- PAT 限流：单令牌默认 120 次/分钟，429 时退避重试

## 排障与验证

排障按序查 [references/ticket-api.md](./references/ticket-api.md) §错误码与 GAPS；调用被 403 时先用 `GET /auth/me` 核对 PAT 身份角色与权限码。验证自动化脚本：先对 demo 环境跑一遍创建→回复→resolve→close 链路并保留请求/响应证据（仿 `.dev/deploy/DEMO-*EVIDENCE.md` 模式）。
