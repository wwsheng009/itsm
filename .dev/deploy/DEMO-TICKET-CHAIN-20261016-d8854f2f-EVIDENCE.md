# 工单 REST API 实弹验证 — 238 demo / 租户账号 JWT 通道（2026-10-16）

- 环境：`http://172.18.3.238:8089`（itsm-demo 独立栈），构建/工作树 SHA `d8854f2f9294c760146f6b9c4eb227e5585fa47e`（tag `1.8.0-dev.d8854f2f`）
- 账号（统一口令 `Demo@2026!`）：`demo.mfg.admin`(admin)、`demo.mfg.agent`(agent)、`demo.mfg.user`(end_user)、`demo.med.admin`(admin/仁和医疗)、`demo.msp.manager`(provider_admin)
- 认证：`POST /api/v1/auth/login {username,password}`；**响应体不含 token**（`AuthResult.AccessToken` 为 `json:"-"`），JWT 仅在 httpOnly `Set-Cookie: access_token=…`。脚本从 Set-Cookie 提取 JWT，改用 `Authorization: Bearer`（Bearer 头跳过 CSRF；cookie 会话写请求需 `X-CSRF-Token`）。
- 产物原文（含全部请求/响应 JSON）：`%TEMP%\itsm-ticketops-238\`（本机 PowerShell 会话临时目录，文件清单见文末）。

## 链路 1：admin 主链路 — TKT-202610-000057（id 58）

| # | 步骤 | 结果 |
|---|---|---|
| 1 | `GET /ticket-types?page=1&pageSize=50` | code=0 total=9（id13 `general_incident` 通用事件） |
| 2 | `POST /tickets` 带 `ticketTypeId=13`、未给 formFields | **HTTP 400 code=1001「字段 故障现象 为必填项」**（自定义必填字段被强制执行） |
| 3 | `POST /tickets` 回退默认类型 | code=0 id=58 number=TKT-202610-000057 status=new priority=high |
| 4 | `GET /tickets/TKT-202610-000057` 工单号反查 | code=0，status=new，version=1 |
| 5 | `GET /tickets/search?q=E2E` | code=0，命中 1 条（TKT-…057） |
| 6 | `GET /tickets?status=new&priority=high` | code=0 total=4（过滤生效） |
| 7 | `GET /tickets/stats` | total=19 open=6 inProgress=4 resolved=1 pending=2 highPriority=7 overdue=9 |
| 8 | `POST /tickets/58/assign {assigneeId:6}`（demo.mfg.agent） | OK，status=open，assigneeId=6 |
| 9 | `POST /tickets/58/comments` 公开回复 | OK id=31 |
| 10 | `POST /tickets/58/comments` `isInternal=true` | OK id=32，isInternal=true；列表 total=2 |
| 11 | `PUT /tickets/58/status {status:pending}` → `{status:in_progress}` | 均 OK（pending → in_progress） |
| 12 | `POST /tickets/58/close`（前置非法） | **HTTP 400 code=4000「invalid status transition: in_progress → closed（仅已解决的工单可关闭）」** |
| 13 | `POST /tickets/58/resolve {resolution}` | OK status=resolved version=5 |
| 14 | `POST /tickets/58/close {closeReason}` | OK status=closed version=6 |
| 15 | 重复 `close`（终态重放） | HTTP 400 拒绝 |
| 16 | `GET /tickets/58/history` | 5 项：resolved>assigned>commented>commented>created（新→旧） |
| 17 | `GET /tickets/58/sla` | code=0，status=new(当时快照) respDeadline=2026-10-16T15:05:26Z，breach=false |

## 链路 2：类型化建单 + 附件/评论绑定 + 乐观锁 — TKT-202610-000058（id 59）

| # | 步骤 | 结果 |
|---|---|---|
| 1 | `POST /tickets` `ticketTypeId=13` + `formFields={"symptom":"…"}` | code=0；**详情回读** ticketTypeId=13、ticketTypeCode=`general_incident`、formFields 保留（创建响应本身未回填 ticketTypeId） |
| 2 | `PUT /tickets/59 {version:999}`（乐观锁探测） | **HTTP 500 code=5001「操作失败」（非文档所述 409/4090）** |
| 3 | `PUT /tickets/59 {version:1}`（真实陈旧版本，当前=2） | 同样 **HTTP 500 code=5001**；工单未被修改（version 保持 2） |
| 4 | `PUT /tickets/59 {version:2,title}`（正确版本） | OK version=3（响应 version=2→3；后续 resolve/close 推进到 4） |
| 5 | `POST /tickets/59/attachments`（multipart `file=@e2e-attachment.txt`） | OK id=5，mime=text/plain，size=41，url=`/api/v1/tickets/59/attachments/5/preview` |
| 6 | 评论绑定 id=5（工单域附件） | **被拒**（校验仅接受通用附件面 `biz_type=ticket+biz_id=59+usage=comment_attachment`，见 `service/ticket_comment_service.go` validateCommentAttachments） |
| 7 | `POST /attachments` `file + bizType=ticket + bizId=59 + usage=comment_attachment` | OK id=6 usage=comment_attachment |
| 8 | `POST /tickets/59/comments {attachments:[6]}` | OK id=33，attachmentRefs=1（fileName=e2e-attachment.txt） |
| 9 | `POST /tickets/59/resolve` → `close` | resolved → closed（history 3 项） |
| 10 | `GET /tickets/59` 终态 | status=closed version=4 |

## 链路 3：end_user 自助 + 行级范围探测（TKT-202610-000059，id 60）

| # | 步骤 | 结果 |
|---|---|---|
| 1 | `demo.mfg.user` 建单 → resolve → close | 全部 OK（id=60，status new→resolved→closed） |
| 2 | `GET /tickets/59`（读他人单） | **HTTP 200**（读侧未见行级过滤；列表可见全租户 21 条） |
| 3 | `PUT /tickets/59`（写他人单） | **HTTP 403 code=2003「无权限修改该工单：仅创建人、受理人或管理员可操作」** |
| 4 | `POST /tickets/59/comments`（评论他人单，已 closed） | **HTTP 200 创建成功 id=34**（评论未做行级限制，closed 仍可评） |

## 租户/身份隔离

| 用例 | 结果 |
|---|---|
| med(租户4) token 读 mfg 工单 59 | HTTP 404 code=4004「工单不存在」 |
| med 自身列表 | code=0 total=16 |
| msp(`demo.msp.manager`) 列表 | code=0 total=0；读 mfg-59 → 404 |
| mfg token + `X-Tenant-Code: demo-med`（存在但不同租户） | **HTTP 401 code=2001「租户不匹配」reasonCode=TENANT_MISMATCH_REJECTED** |
| mfg token + `X-Tenant-Code: NO_SUCH_TENANT`（不存在） | HTTP 404 code=4004「租户不存在」 |
| `/auth/me` 带任意 X-Tenant-Code | 200（该端点不在租户中间件链上） |

## 与技能文档的差异（本次实测）

1. **乐观锁冲突未按契约返回 409**：`ticket/repository_impl.go` 乐观锁检查返回裸 `fmt.Errorf("version conflict: …")`（318-319、404-405、464-465 行区域），`RespondError` 兜底 → 500/5001。文档原称 409 + `{ticketId,currentVersion,serverVersion}`——需修代码（映射 `VersionConflictError`）或文档标注 GAP。**调用方当前应按 5001 处理重试。**
2. **登录响应体不含 token**：只发 httpOnly cookie；外部脚本需从 `Set-Cookie` 提取 JWT 并用 Bearer（Bearer 免 CSRF）。
3. **评论附件必须走通用附件面**（`POST /attachments` + `usage=comment_attachment`）；工单域 `/tickets/:id/attachments` 的 ID 会被评论绑定校验拒绝。
4. **行级范围仅写侧强制**：PUT/DELETE 越权 403；GET 列表/详情不套行级过滤；评论创建不限制。
5. **自定义类型必填字段**：`ticketTypeId` + 必填 `customFields` 需在 `formFields` 传值（key=`customFields[].id`，如 `symptom`）；缺报 1001「字段 X 为必填项」，传未定义 key 报 1001「包含未定义字段」。
6. 创建响应可能不回填 `ticketTypeId`，以详情回读为准。

## 原始证据文件（%TEMP%\itsm-ticketops-238\）

`01-login.json` `02-me.json` `03-ticket-types.json` `04-stats.json` `05-users.json` `06-create.json` `07-detail.json` `08-search.json` `09-history.json` `10-sla.json` `11-list.json` `12-comment-public.json` `13-comment-internal.json` `14-comments.json` `15-close-early.json` `16-resolve.json` `17-close.json` `20-create2.json` `20b-detail2.json` `21-lock-409.json` `22-upload.json` `23-comment-with-att.json` `24-comments2.json` `25-lock-stale.json` `26-upload-generic.json` `27-comment-with-att2.json` `28-comments2.json` `30-med-read-mfg-ticket.json` `31-user-read-admin-ticket.json` `32-user-create.json` `33-tenant-mismatch.json` `34-msp-read-mfg-ticket.json` `35-user-wrong-tenant.json` `36-admin-wrong-tenant-list.json` `40-med-login.json` `41-mismatch-real.json` `42-enduser-put-others.json` `43-enduser-comment-others.json` + `cookies-*.txt`/`token-*.txt`

## 遗留数据（demo 环境，可清理）

- `TKT-202610-000057`（admin 主链路，closed；1 公开 + 1 内部评论、已指派 demo.mfg.agent）
- `TKT-202610-000058`（admin 类型化+附件链路，closed；通用附件 id6 + 评论 id33 绑定）
- `TKT-202610-000059`（end_user 自助链路，closed；含 admin 域外评论 id34）---

## 附录：缺口修复记录（2026-10-16，本工作树；demo 待重新发布）

| # | 缺口（本文件实测） | 修复内容 | 回归测试 |
|---|---|---|---|
| 1 | `PUT /tickets/:id` 乐观锁冲突回落 **500/5001**（与 409/4090 契约不符） | ① `common.IsVersionConflictError`/`ErrorHandler` 改用 `errors.As`（兼容 `%w` 包装）；② `repository/ticket/repository_impl.go` 版本检查 + CAS 未命中（含事务路径）返回 `common.VersionConflictError`，并发场景回读最新版本给出准确 `serverVersion`；③ `handlers/ticket/service.go` 增加版本预检（类型化）；④ `handlers/ticket/handler.go` 冲突分支改 `errors.As` | `TestHandler_Update_VersionConflict409`（HTTP 409 + code 4090 + 字段）；`TestRepository_Update_RejectsStaleVersion`（类型化断言）；`common.TestVersionConflictError`（包装链） |
| 2 | 行级收窄静默失效：`datascope.OwnedOrAssigned=2` 直接 int 转换 → `ticket.DataScope`（OwnedOrAssigned=1）不匹配，等价 `All`；end_user 列表可见全租户（阻断8 回归；本文件"链路 3"的 21 条即此因） | `handlers/ticket/repository_impl.go` 新增 `toRepositoryDataScope` 显式映射（Department 等未支持档位按安全收窄）；替换直接转换 | `TestHandler_List_DataScopeNarrowing`（end_user→OwnedOrAssigned+uid；admin→All）；`TestRepository_List_DataScopeOwnedOrAssigned`（sqlite：自建+被指派=2、他人不可见、uid≤0 fail-closed=0） |

验证（2026-10-16）：`go build -p 1 ./...` 通过；`go test -p 1 ./common/ ./handlers/ticket/ ./repository/ticket/ ./service/ -run ...` 全绿（含上述新增用例）。

保留为文档化行为（未改代码，属设计/待产品决策）：
- 详情 `GET /tickets/:id` 与评论读取未套行级过滤（同租户可见/可评，closed 仍可评）——见 references GAPS-12；
- 登录响应体不含 token（仅 httpOnly cookie）——安全设计，见 GAPS-11；
- 评论附件只接受通用附件面 + `usage=comment_attachment`——见 §7 注记。

待办：238 demo 仍运行 `d8854f2f` 旧构建；按 `itsm-deploy` 重新发布后，线上复测：陈旧 version → 409/4090；`demo.mfg.user` 列表应仅见本人创建/被指派。---

## 附录 2：skill 脚本工具（scripts/itsm_ticket.py）交付与实测（2026-10-16）

- 位置/形态：`.agents/skills/itsm-ticket-operations/scripts/itsm_ticket.py`；零依赖（Python 3 stdlib，单一文件，450 行）；`python -m py_compile` 通过；`skill-creator/quick_validate.py` 输出 "Skill is valid!"。
- 设计动机：把每次手写都易错的逻辑固化为低自由度工具——cookie 取 JWT、Bearer/CSRF、multipart 上传、409 读-改-写重试、formFields 必填提示、错误 JSON 化。
- token 优先级：`--token` > `--token-file` > `ITSM_TOKEN`/`ITSM_PAT`；`--base` 默认 `ITSM_BASE_URL` 或 `http://localhost:8090/api/v1`。
- 实测（238 demo，`demo.mfg.admin`）：

| 步骤 | 结果 |
|---|---|
| `login --save` | OK（从 Set-Cookie 提取 JWT；savedTo 落盘） |
| `me` | role=admin，tenantId=3 |
| `types --show-fields` | total=9；id13 requiredFormFields key=`symptom`（故障现象） |
| `create` 缺 formFields | exit=1，http=400，code=1001 + hint（引导 `--show-fields`/`--form-field`） |
| `create --form-field symptom=…` | id=61，TKT-202610-000060，status=new |
| `comment` | id=35 |
| `attachment-upload` / `comment-attachment-upload` | id=7（工单域）/ id=8（usage=comment_attachment） |
| `comment --attachment 8` | attachmentRefs=1 |
| `update --title`（自动读-改-写） | version=2（成功） |
| `update --version 999`（显式） | exit=1，http=500，code=5001（旧 demo 构建；乐观锁修复部署后应 409/4090） |
| `resolve` → `close` | resolved → closed |
| `close` 重放 | exit=1，http=400，code=4000（终态拒绝） |
| `history` / `attachment-list` | n=4 / total=2 |
| `get 999999` | exit=1，http=404，code=4004 |

- 工具自身缺陷（实跑发现并已修）：**Python 3.13 argparse 子命令默认值会覆盖主 parser 已解析的全局参数**（`--token-file X me` 丢成空串）→ 子命令侧公共参数改用 `argparse.SUPPRESS`；前/后/混合三种参数顺序复测通过。
- 遗留 demo 数据：`TKT-202610-000060`（id 61，closed；2 评论、2 附件，标注 [E2E工具]）。
- 未做：未提供 Node.js 版本（同环境 Python 在部署链路已可用，零依赖即可覆盖；如后续需要 JS 分发再补）。---

## 附录 3：238 demo 发布与线上复测（2026-10-16，tag `1.8.0-dev.d01b916e`）

- 通道：git bundle（基线=远端 HEAD `d8854f2f` → `d01b916e`）+ prod compose 构建 `itsm-backend`；仅 `up -d itsm-backend-demo itsm-worker-demo`（init 重建跑迁移；**未 down -v**）。
- 步骤表：

| 步骤 | 命令/脚本 | 结果 |
|---|---|---|
| A 取码 | `deploy238_ticketfix_a.sh`（BUNDLE=/tmp/itsm-ticketfix.bundle） | HEAD=`d01b916e407572f8bb16e7753f5cf61ffedff3cd` |
| B 构建 | `deploy238_ticketfix_b.sh`（NEW=1.8.0-dev.d01b916e） | BUILD_EXIT=0；镜像 `itsm-backend:1.8.0-dev.d01b916e` = sha256:068114eb8d9c…（旧 d8854f2f = sha256:229aedd0b244…，镜像确已更新） |
| C 上线 | `deploy238_ticketfix_c.sh` | frontend/ai retag 对齐；`.env.demo` VERSION=d01b916e；init 重建后 Exited(0)；backend/worker Recreated + Healthy；health=200 / root=200 |

- 线上复测（`http://172.18.3.238:8089`）：
  1. **乐观锁 409/4090**：`update 59 --version 1`（当前版本 4）→ `{"ok":false,"http":409,"code":4090,"message":"工单(ID:59) 版本冲突：客户端版本 1，服务器版本 4","data":{"currentVersion":1,"serverVersion":4,"ticketId":59}}` ✅（修复前为 500/5001）
  2. **行级收窄**：`demo.mfg.user`(end_user) `list` total=**9**（修复前 21+；同租户管理员视角 total=23），9 条全部 `requester=8`（本人创建）✅
  3. **冒烟链路**（新构建，`scripts/itsm_ticket.py` 通道）：create id=62 `TKT-202610-000061` → comment id=37 → update v2 → resolve → close ✅
  4. 容器：`itsm-backend-demo`/`itsm-worker-demo` = `itsm-backend:1.8.0-dev.d01b916e`（healthy）；近 10 分钟日志无 panic/error（唯一命中为既有 `tenant_guard` warn：`user_pir_reviews` 未登记豁免，与本次变更无关）。
- 遗留 demo 数据：`TKT-202610-000061`（id 62，[E2E复测]，已关闭）。