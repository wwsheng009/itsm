# M1-10 补充证据：真实栈联调（M1-10 runtime joint-debug）

> 文档类型：实施证据（任务 M1-10 补充；对应缺口台账 A2/A4 的「真实栈联调」部分）
> Status: draft
> 执行日期：2026-10-10 深夜～2026-10-11 凌晨（本地开发栈）
> 环境：分支 `feat/msp-p0-hardening`（后端二进制从 HEAD 重建）；Windows + pwsh；真实 Postgres（SSH 隧道 :15433/itsm_prod）+ Redis；前端 Vite dev :3000；`cmd/mcp-mockserver` :19090（`-tools default`）
> 联调姿态（临时进程 env，联调后已还原）：`MCP_ALLOW_HTTP=true`、`MCP_ALLOW_PRIVATE_NETWORKS=true`、`MCP_ALLOWED_PORTS=19090`、`LLM_PROVIDER=mock + LLM_MOCK_ENABLED=true + LLM_MOCK_TRIGGER=__tool__ + LLM_MOCK_TOOL_NAME=mcp__mock__create_issue`、`BOT_ENABLED=true`
> 关联：`M1-10-flow-acceptance-evidence.md`（2026-09-27 服务端 E2E，`flow_verified`）；台账 `gap-register-2026-09-27.md` A2/A4/A9

## 1. 范围与结论

| 项 | 判定 | 说明 |
| --- | --- | --- |
| M1-10 真实进程 API 全链路 | ✅ 通过 | 真实后端进程（非测试进程）+ 真实 mock MCP over HTTP：建服务器 → 测试连接（协议 2025-06-18，6 工具，3ms）→ 启用 healthy → 工具治理启用 → 目录 → 只读/写执行 → 审批 → 真实执行回填 → 调用留痕（state=all）→ 指标（`itsm_mcp_server_connected=1` 等 34 行）→ 清理（禁用+删除，服务器数与基线一致） |
| A2（管理页人工冒烟 + 截图） | ✅ 本轮闭环（验收级 E2E 仍归 M2-05/CI） | 真实栈 + 平台管理员 + 真实浏览器：`/admin/mcp-servers` 列表/统计/能力开关卡/工具治理表全部正确渲染（A9 修复维持有效，投影名/只读/风险/分类/生效状态齐全）；截图见 §5 |
| A4-A1-05（待审批卡片「手工」部分） | ✅ 本轮闭环 | 真实浏览器对话页：mock LLM 经 Bot9 授权触发 MCP 写工具 → SSE `approval_pending` → 对话内待审批卡片（来源徽标 MCP·mock、风险 act_low、确认/拒绝 + 前往审批 + 刷新状态）实时渲染；截图见 §5 |
| A4-A1-06（审批页浏览器面） | ✅ 本轮闭环（CI E2E 归 M2-05） | 审批队列列表（来源/风险/确认时限/权限校验/状态/原始名）+ 详情三元组（服务器/可调用名/角色快照/脱敏参数）+ `mcp:admin` 可见的「工具治理」跳转；截图见 §5 |
| 缺陷发现与修复 | ✅ 1 项修复 + 1 项登记 | D-3（本轮修复）：ChatStream 工具执行丢失会话归属 → 对话内审计断链 + 执行期 Bot 策略解析与下发面失配；D-4（登记未修）：无会话上下文的直执行入口在 BOT_ENABLED=true 时 Bot 解析落空一律拒绝（fail-closed） |

## 2. 执行记录（真实进程，非测试进程）

### 2.1 API 全链路（`.dev/mcp-e2e-live.ps1`，password 已修正为当前 admin 口令）

```text
baseline: server[1] mock healthy tools=6/3（前次 ssrf_blocked 状态在 allow_http 开启后恢复 healthy）
create_server id=5 e2e-mock-1010-235618 → configured/disabled
test_connection ok=True protocol=2025-06-18 tool_count=6 duration_ms=3
enable_server → healthy tool_count=4
enable_tool_list_issues → read_only=False risk=high（未分类默认，fail-closed 进审批）
execute list_issues → 200 pending（invocation 83）
enable_tool_create_issue + execute → 见 §3.1（脚本侧 403，定性为 CSRF 脚本工件）
invocations state=all → invocation hit id=83 tool=mcp__mock__create_issue... provider=mcp server=... callable=...
metrics :8080/metrics → itsm_mcp_lines=34，server_connected{e2e-mock}=1，tool_face_tools{tenant 1}=3
cleanup → disable 202 + delete 200，final_servers=1（baseline=1）
```

### 2.2 直执行写审批链（curl，绕过脚本）

- `POST /agent/tools/execute {mcp__mock__create_issue, args={title,body}}` → **200 pending（invocation 84）**
- `POST /agent/tools/84/approve {approve:true}` → approved
- 详情：**status=done, errorCode='', durationMs=1414ms, provider=mcp, server=mock** —— 审批→队列→真实 MCP 执行→回填全闭环。

### 2.3 对话链路（mock LLM + Bot 策略）

- 前置：mock LLM（触发词 `__tool__`、指定工具 `mcp__mock__create_issue`）+ Bot9「MCP 联调助手」（ga，entrypoint=chat，授权 `mcp__mock__create_issue`@act_low / `mcp__mock__list_issues`@read）+ `BOT_ENABLED=true`（botPolicy 为静态装配，运行时能力开关不能替代，见 §3.3）。
- `POST /ai/chat/stream {query:"…__tool__", botId:9}`（SSE 完整读取）→ **5× `approval_pending`**（invocation 119-124），`tool_call_started {tool:"mcp__mock__create_issue", provider:"mcp", phase:"write"}` 实时下发；tool loop 护栏 5 轮正常熔断（`tool loop exceeded max rounds (5)`）。

### 2.4 浏览器（真实登录会话 + 截图）

1. 平台管理员登录 → `/admin/mcp-servers`：统计卡（服务器总数 3/已启用 3/已连接 3/生效工具 6）、能力开关卡（MCP 总开关/外部写工具面「已由管理台覆盖」/Bot 能力）、服务器行（healthy、协议 2025-06-18、工具 1/6、操作组）——**无 A9 复发**（undefined/空投影名未出现）。
2. 「工具治理」Tab：服务器选择器 + 批量启用/停用 + 工具表（投影名/说明/只读/风险/分类/生效状态/启用工具）。
3. `/ai/chat`（会话 17，绑定 Bot9）发送触发词 → 时间线（MCP · mock · 写操作）+ **对话内待审批卡片**（#141-#145，来源徽标、风险、确认/拒绝、前往审批、刷新状态）。
4. 「前往审批」→ `/ai/approval`：待审批队列（来源 MCP·mock、风险 act_low、确认时限剩余 23h54m、权限校验 passed、状态待审批、原始名 create_issue）+ 展开详情三元组（服务器/可调用名/角色快照/脱敏参数/治理跳转）。
5. UI 点击「通过」#141 → **approved → 真实执行（2155ms MCP 往返）→ failed/`invalid_args`**（mock 服务器拒绝空业务参数）——错误语义正确留痕（参数冻结设计下，审批执行的是冻结参数）；done 路径已由 §2.2（invocation 84）证明。
6. `/ai/audit` 工具调用审计区块可访问（本轮未单独截图，组件测试 5 例覆盖 M1-07）。

## 3. 本轮发现

### 3.1 脚本 403（定性：CSRF 脚本工件，非产品缺陷）

v2 脚本在「治理启用写工具 → 目录 → execute」一步得到 **403 + 空 body + 无审计行**。定位：该次 execute 请求前**没有** `/csrf-token` GET（GIN 日志 23:56:33→:34 之间无该请求），脚本（cookie 会话）缺 CSRF 头被中间件 403；curl（Bearer 认证，CSRF 通道外）同工具同参数 200 pending（invocation 84/85）证实产品写门无恙。此失败不可复现（≥4s 间隔的两次手动复现均通过），FailCount=1 全额归因脚本工件。

### 3.2 D-3（本轮修复）ChatStream 工具执行丢失会话归属

- **现象**：UI 对话（既有会话，前端不重发 botId）中，Bot 授权的 MCP 写工具在下发面可见并被模型调用，但执行期 5 连拒 `tool_not_granted`（invocation 131-135：`bot policy: tool_not_granted`，`conversation_id=None`）。
- **根因**：`ChatStream` 的 execTool 闭包调 `s.ExecuteTool(...)` → `ExecuteToolWithConversation(..., 0)` → `ConversationID` **恒 0**（`ExecuteToolWithConversation` 的注释"聊天路径调用时携带 conversationId"从未兑现）。后果：① 工具审计 `conversation_id` 恒空（B0-03 审计回溯断链，119-145 修复前全部为 None）；② 执行期 `resolveBotID` 只剩请求级 `WithBotID`——既有会话不重发 botId 时解析为 0，与下发面（按 conversation 绑定→Bot9）失配，授权写工具被拒。fail-closed 方向（执行比下发更严），非越权。
- **修复**：`handlers/ai/service.go` execTool 改调 `s.ExecuteToolWithConversation(..., convID)`（convID = 请求携带的会话 ID；新会话流中 convID=0，行为与既有版本一致——会话在流结束后才落库）。
- **实栈 before/after**：修复前 131-135（conversation_id=None + denied）；修复后 141-145（**conversation_id=17** + pending + passed，UI 待审批卡实时渲染）。
- **测试口径**：`handlers/ai` 窄测试全绿（4.1s）；`tests/mcpintegration` 存在 2 例**与本修复无关的存量失败**（stash 基线复现同败：`TestM1Annotation_Gate2ReadVsWrite`/`TestM0Flow_EndToEnd`，`role=admin lacks mcp:read`——测试夹具未跟上平台治理收口后的码表，归属测试维护者）；ChatStream 级自动化回归测试缺口登记于台账 C7。

### 3.3 D-4（登记未修）无会话上下文的直执行入口 Bot 解析落空

- **现象**：`BOT_ENABLED=true`（botPolicy 已装配）时，`POST /agent/tools/execute`（无 conversation、无 botId，如脚本/CLI/工具目录页试跑）→ Gate 2.5 `CheckTool(bot 0)` → 一律 `tool_not_granted`（invocation 146/147；修复前的 invocation 84 之所以通过，是当时进程 `BOT_ENABLED=false` → Gate 2.5 整段跳过）。
- **定性**：fail-closed（拒绝方向）；但「Bot 系统开启后，无会话上下文的合法直执行写调用全部不可用」属功能缺口——需要产品决策：直执行入口的 Bot 归属（默认助手模板？跳过 Gate 2.5？显式 botId 参数？）。登记台账 C7，归属后端 + 产品。

### 3.4 观察（体验项，不阻塞）

- 审批页偶发「挂载时列表请求落在 token 过期窗口 → 渲染空列表且无错误横幅」（本轮复现 1 次，手动刷新恢复）；与 B7 观察①的「队列无 pending 数据」无关。
- `/admin/mcp-servers` 首屏骨架 → 数据约 3-5s（dev 模式），非本轮范围。

## 4. 环境与凭据处置（如实登记）

- 平台 `admin`（id=1）口令按 `.dev/connection-guide.md` §228「口令已遗失需另行重置」的指定通道 `cmd/reset_password` 重置（审计 `user.admin_password_reset`，`-must-change=false`，新口令仅在会话对话中告知、未写入任何提交文件）。
- 联调姿态 env 均为进程级注入（`start-backend.ps1`），`.env` 未改动；联调后已按原姿态重启（`MCP_ALLOW_HTTP` 关闭、mock LLM 关闭、`BOT_ENABLED` 关闭、日志级别回 error）；运行时能力位恢复 `bot.enabled=false`（`mcp.enabled/mcp.write_enabled` 维持本轮之前的覆盖态）。
- 临时服务器（dbg403-1010、e2e-mcp-chat）已禁用+删除；遗留 pending 全部驳回（17 条，200/条，理由「M1-10 联调清理」）；pending 归零、服务器只剩基线 `mock`（id=1）。数据库中保留的 invoked/denied/rejected 行为审计事实，按「只增不删」口径保留。
- mockserver（:19090）保持运行（`.tmp` 外无残留进程；如需停止：`Stop-Process -Name mcp-mockserver`）。

## 5. 截图留痕（`.tmp/web-check/m1-10/`，未跟踪）

| 文件 | 内容 |
| --- | --- |
| `01-admin-mcp-servers.png` / `02-admin-mcp-servers-loaded.png` | 管理页首屏 / 数据就绪（A2 冒烟） |
| `03-admin-tool-governance.png` | 工具治理表（投影名/只读/风险/分类/生效状态） |
| `04-chat-approval-pending-card.png` | 对话内待审批卡片 ×3 + 时间线（A1-05 手工） |
| `05-approval-page-pending.png` | 审批队列列表（来源/风险/时限/权限校验/原始名）（A1-06） |
| `06-approval-detail-triple.png` | 详情三元组 + 脱敏参数 + 治理跳转（A1-06） |

## 6. 对判定的影响

| 台账项 | 变化 |
| --- | --- |
| A2（管理页冒烟+截图） | 「真实栈 + 登录会话 + 浏览器」三条件本轮满足 → 冒烟部分闭环；验收级 E2E（CI `e2e-mcp.yml` 首绿）仍归 M2-05 |
| A4（A1-05 手工 / A1-06 E2E） | A1-05 手工 ✅；A1-06 浏览器面 ✅；A1-06 的 CI E2E 归 M2-05 不变 |
| M1-10 | `flow_verified` 维持 + 补齐真实栈联调证据（本文件） |
| 新增 | C7（D-4 直执行 Bot 解析缺口，登记未修）；D-3 修复随本分支提交 |
