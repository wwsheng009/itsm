---
name: itsm-ticket-operations
description: 通过 ITSM REST API 操作工单业务单据（创建工单/查询元数据/搜索列表/状态流转/回复评论/附件管理）。Use when the agent needs to 调用 ITSM 工单 API、创建工单、搜索/列出工单、更新工单状态、回复工单、上传工单附件、查询工单类型/模板/分类等元数据，或为自动化脚本/CI 生成工单调用代码；covers POST/GET/PUT /api/v1/tickets 及 comments/attachments/metadata 端点，含认证（PAT/JWT）、权限码、状态机与字段契约。Do NOT use for 部署（itsm-deploy）或工单模块代码开发（.trae/skills/ticket-module-dev）。
---

# ITSM 工单业务操作（API 调用手册）

面向**外部调用方**（自动化脚本 / CI / CLI / Agent）通过 REST API 操作工单。认证用个人访问令牌（`Authorization: Bearer itsm_pat_…`，在个人中心 → 安全设置创建）或 JWT；PAT 的权限与所属用户完全一致。所有端点在 `Authorization` 头下工作，**租户绑定令牌无需 `X-Tenant-Code`**（传了且不一致会被 401 拒绝）。无 PAT 时可用 `POST /auth/login` 换 JWT：**响应体不含 token**，JWT 仅经 httpOnly cookie 下发（脚本从 `Set-Cookie` 取 `access_token` 后走 Bearer）。

完整端点表、字段契约、错误码与示例：先读 [references/ticket-api.md](./references/ticket-api.md)。部署/环境：见 `itsm-deploy` 技能。

## 脚本工具（scripts/itsm_ticket.py）

零依赖 Python 3 CLI（仅标准库），已把「cookie 取 JWT、Bearer、409 读-改-写重试、multipart 上传、formFields 提示」等易错逻辑固化。外部调用方/自动化优先用它，不要每次重写 curl：

```bash
B=http://172.18.3.238:8089/api/v1
# 1) 登录换 JWT（token 只从 Set-Cookie 取，响应体不含）
python scripts/itsm_ticket.py login --base $B --username demo.mfg.admin --password 'Demo@2026!' --save ./itsm.token
# 2) 后续命令统一 --token-file（或 --token/ITSM_TOKEN/ITSM_PAT）
#    自定义类型先查必填字段 key
python scripts/itsm_ticket.py --base $B --token-file ./itsm.token types --show-fields
python scripts/itsm_ticket.py --base $B --token-file ./itsm.token create --title '打印机故障' --priority high --ticket-type-id 13 --form-field symptom=队列卡住   # 返回 id=61（示例）
python scripts/itsm_ticket.py --base $B --token-file ./itsm.token comment 61 --content '已受理，排查中'
python scripts/itsm_ticket.py --base $B --token-file ./itsm.token update 61 --title '打印机故障（处理中）'   # 409 自动重试
python scripts/itsm_ticket.py --base $B --token-file ./itsm.token resolve 61 --resolution '重建队列'
python scripts/itsm_ticket.py --base $B --token-file ./itsm.token close 61 --reason '用户确认恢复'
```

- `python scripts/itsm_ticket.py --help`：20 个子命令（login/me/types/create/get/list/search/stats/comment/status/update/assign/resolve/close/escalate/history/sla/attachment-list/attachment-upload/comment-attachment-upload）
- 成功 JSON 到 stdout；错误 JSON 到 stderr（含 http/code/reasonCode/hint），退出码 1（参数错误 2）；`--compact` 输出紧凑 JSON
- 全局参数 `--base/--token/--token-file/--timeout/--compact` 放在子命令前后均可
- `update` 默认读-改-写、遇 409 自动重试一次；显式 `--version N` 时保留 409 语义不做重试

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
- **PUT 更新带 `version` 乐观锁**，冲突返回 409 + `{ticketId, currentVersion, serverVersion}`（2026-10-16 修复类型化映射并回归锁定）
- **更新状态不带评论字段**；要在流转时留言 → 先 `POST /:id/comments` 再 `PUT /:id/status`
- 行级权限：列表对非管理角色强制「本人创建/被指派」；PUT/DELETE 越权 403；详情与评论读取未收敛（见 references GAPS-12）
- 附件两套面：工单域 `POST /tickets/:id/attachments`（`ticket:create`）与通用 `POST /attachments`（`attachment:write`，`bizType=ticket&bizId={id}`）；评论附件是「先上传后绑定」（评论体 `attachments` 传 ID，≤10 个）
- 错误语义：`{code,message,data}`；2001 未认证 / 2003 无权限（含越权）/ 4004 不存在 / 4090 乐观锁冲突 / 4220 配额超限
- PAT 限流：单令牌默认 120 次/分钟，429 时退避重试

## 排障与验证

排障按序查 [references/ticket-api.md](./references/ticket-api.md) §错误码与 GAPS；调用被 403 时先用 `GET /auth/me` 核对 PAT 身份角色与权限码。验证自动化脚本：先对 demo 环境跑一遍创建→回复→resolve→close 链路并保留请求/响应证据（仿 `.dev/deploy/DEMO-*EVIDENCE.md` 模式）。
