# Bot 应用与「Bot 调用 MCP 工具」运行验证

> 文档类型：证据（使用指南 + 运行验证）
> Status: draft
> 编制日期：2026-09-30
> 适用范围：Bot 能力的入口/开关/权限口径；Bot 经 LLM 调用 MCP 外部工具的端到端验证（只读链路 + 写工具审批链路）
> 目标读者：产品、测试、运维、后续实现者
> 关联文档：`docs/plan/ai-bot-capability-landing-implementation-plan-2026-09-27.md`、`docs/plan/itsm-mcp-external-tool-integration-implementation-plan-2026-09-27.md`、`docs/plan/evidence/mcp-m2/tool-catalog-bot-mcp-evidence.md`
> 核查基线：代码 `8d930aae` + 本轮 `config.go` 增量（`MCP_WRITE_ENABLED` 环境变量绑定）
> 核查方式：本机真实进程（后端 `main.exe` + mock MCP 服务器 `:19090`）+ 真实浏览器 + **两条 LLM 链路**（真实模型 `deepseek-openai` 与确定性替身 `mock`）
> 状态口径：`implemented → unit_verified → integration_verified → flow_verified → accepted`

## 0. 结论先行（TL;DR）

1. **Bot 在哪里用**：日常使用在 **`/ai/chat` 顶部的 Bot 选择器**（`BotSelector`）；配置与授权在 **`/admin/bots`（Bot 管理与授权）**；两者共用一套后端契约（`/api/v1/agent/bots` 与 `/api/v1/admin/bots*`）。
2. **Bot 怎么生效**：请求体带 `botId`（0/缺省 = 默认助手）→ 后端按 `授权 ∩ RBAC ∩ 风险上限 ∩ 入口` 组装工具面 → LLM 只能看到该面内的工具；写工具**永不直接执行**，一律落 `pending` 待人工审批（Gate3）。
3. **「Bot 调用 MCP 工具」已在本机端到端跑通**（三条链路，全部留痕可查）：
   - **场景 A（真实模型自主决策）**：`deepseek-openai` 驱动，模型自行选择调用 `mcp__mock__list_issues`，两次执行落审计（id 55/56，`status=executed`，`provider=mcp`，`server=mock`，`risk=read`）；模型伪造的 `user_id` 参数被剥离并留痕（`permissionReason=args_stripped:user_id`）。
   - **场景 B（确定性替身，CI 口径）**：`llm.provider=mock` + `LLM_MOCK_ENABLED=true`（且关闭多 Provider 灰度），同一句话稳定触发 5 轮 `tool_call_started/finished`，随后被**工具循环护栏** `tool loop exceeded max rounds (5)` 中断（护栏本身也被验证）。
   - **场景 C（写工具 Gate3）**：`create_issue`（`read_only=false`、`risk=act_low`）→ 5 次调用全部落 `pending`（id 67–71，`needsApproval=true`、带 TTL，**零执行**）→ 批准 67 后 `status=done`（`approvedBy=1`、结果 `{"created":true}`）→ 拒绝 68 后 `status=rejected` + 原因留痕。
4. **浏览器侧同样成立**：`/ai/chat` 选「MCP 联调助手」→ 发送含 `__tool__` 的消息 → 「过程证据」面板逐条显示 `mcp__mock__list_issues  MCP · mock  完成 0.1–0.2s` 与 mock 返回体。
5. 本轮唯一代码改动：`mcp.write_enabled` 增加 **`MCP_WRITE_ENABLED` 环境变量绑定**（本地/CI 无需改 `config.yaml` 即可联调写工具面），配置用例全绿。

## 1. Bot 功能怎么用

### 1.1 三个开关（默认值即"可用且安全"）

| 开关 | 位置 | 默认 | 作用 |
| --- | --- | --- | --- |
| `bot.enabled` | `BOT_ENABLED`（`.env`/环境变量） | `false`（本机已置 `true`） | Bot 管理路由（`/api/v1/admin/bots*`）、工作区选择器（`/api/v1/agent/bots`）与策略门禁是否装配；关闭时整组路由不注册，聊天行为与引入 Bot 前一致 |
| `mcp.enabled` | `MCP_ENABLED` 或 `config.yaml` 的 `mcp.enabled` | **`true`**（2026-09-27 起） | MCP 管理面/组件就绪；**服务器、工具、写面仍默认拒绝**（D7） |
| `mcp.write_enabled` | `MCP_WRITE_ENABLED`（本轮新增绑定）或 `mcp.write_enabled` | `false` | 外部**写工具是否进入工具面**（模型可见/可提交审批）；置 `true` 后每次写调用仍须 `mcp:write`（Gate2）+ 人工审批（Gate3） |

> 回滚四级开关：`mcp.enabled`(L1) → `mcp.write_enabled`(L1.5) → 服务器 `enabled`(L2) → 工具 `enabled`/quarantine(L3)。

### 1.2 入口清单（在哪里调用 Bot）

| # | 入口 | 位置（代码） | 权限 | 说明 |
| --- | --- | --- | --- | --- |
| 1 | **对话页 Bot 选择器** | `itsm-frontend/src/components/ai/BotSelector.tsx:62`；挂载于 `itsm-frontend/src/components/ai/AIChat.tsx:1239` | 登录 + `ai:read` | 数据来自 `GET /api/v1/agent/bots`（`itsm-backend/router/bot_routes.go:35`），后端按角色 `audience` 过滤；**只影响新会话**，已有会话锁定（`locked`） |
| 2 | **对话请求携带 botId** | `itsm-backend/handlers/ai/handler.go:303`（`/ai/chat`）与 `:433`（`/ai/chat/stream`） | `ai:read` | `botId=0/缺省` = 默认助手；显式 `botId` 必须是本租户模板，否则 404（`handler.go:341`、`service.go:599`） |
| 3 | **会话归属** | `itsm-backend/handlers/ai/service.go:517` | — | 新建会话写入 `conversation.bot_id`；历史会话按库中归属解析，选择器不回溯改写 |
| 4 | **页面 launcher 入口上下文** | 请求字段 `entrypoint/targetType/targetId/summary`（`handler.go:434`） | `ai:read` | B3-01：详情页/工作台把上下文带给 Bot（如从工单详情发起） |
| 5 | **管理面（模板与授权）** | `/admin/bots` 页面；API `itsm-backend/router/bot_routes.go:22-31` | `ai:read`（读）/ `ai:write`（写） | 模板 CRUD、授权 CRUD（`PUT /api/v1/admin/bots/:id/grants`） |
| 6 | **运行指标** | `/ai/bot-metrics` 页面；API `GET /api/v1/ai/bot-metrics` | `ai:read` | 运行/步骤/工具/确认/成本代理六段聚合；`bot.enabled=false` 时 503，前端隐藏看板 |
| 7 | **工具目录（本轮新增）** | `/admin/tools` 页面；API `GET /api/v1/agent/tools/catalog` | `ai:read` | 查「Bot 可以授权哪些工具」，含内置/MCP 来源、风险、只读标注 |
| 8 | **审批入口** | `/ai/approval` 页面 + 对话内待审批卡片；API `GET /api/v1/agent/tools/invocations`、`POST /api/v1/agent/tools/:id/approve` | `ai:read` / `ai:write` | 写工具（内置与 MCP）统一走这里 |

### 1.3 工具面判定（Bot 与 MCP 的关系）

`chatToolDecision`（`itsm-backend/handlers/ai/service.go:559`）是唯一判定点：

1. **黑名单优先**：命中 `admin/permission/删除/任意 IO` 等规则的工具永不下发（与开关、授权无关）；
2. `bot.enabled=false`（未装配策略）：走既有逻辑（只读工具 ∪ 遗留写白名单）；
3. `bot.enabled=true`：**授权 ∩ RBAC ∩ 风险上限 ∩ 入口**；未配置任何授权的 Bot = 兼容默认（等价引入 Bot 前）；**快照读取失败 = fail-closed（不下发任何工具）**。

MCP 工具进入该面的前提：`mcp.enabled` 开 → 服务器 `enabled` → 工具 `enabled` 且未 quarantine → `read_only=true`（只读）或 `mcp.write_enabled=true`（写）。执行面另有 Gate2（`mcp:read`/`mcp:write`）与写工具 Gate3（人工审批）。

### 1.4 最小可用流程（本机实测口径）

```powershell
# 0) 登录 + CSRF（cookie jar）
$jar = Join-Path $env:TEMP 'itsm-mcp-e2e-jar.txt'
#   POST /api/v1/auth/login → GET /api/v1/csrf-token（字段 data.csrf_token；写请求带 X-CSRF-Token）

# 1) 建 Bot（管理面；ai:write）
#   POST /api/v1/admin/bots
#   {"slug":"mcp-demo","name":"MCP 联调助手","audience":"internal",
#    "riskLimit":"act_low","entrypoints":["chat"],"status":"ga"}

# 2) 授权工具（管理面；工具名用投影名 mcp__<server>__<tool>）
#   PUT /api/v1/admin/bots/{id}/grants
#   {"toolName":"mcp__mock__list_issues","riskLimit":"read"}

# 3) 对话（工作区；SSE）
#   POST /api/v1/ai/chat/stream
#   {"query":"请列出当前问题","botId":9}
```

## 2. 场景 A：真实模型自主调用 MCP 工具（`deepseek-openai`）

环境：`mcp.enabled=true` + `MCP_ALLOW_HTTP/PRIVATE/PORTS` 放开（mock `127.0.0.1:19090`）；Bot = `mcp-demo`（id 9，`riskLimit=act_low`，入口 `chat`），**仅授权 1 个 MCP 只读工具** `mcp__mock__list_issues`（`riskLimit=read`）。
请求：`POST /api/v1/ai/chat/stream`，`{"query":"__tool__ 请列出当前问题","botId":9}`。

| 观察点 | 实测结果 |
| --- | --- |
| 工具面 | 模型只看到 1 个工具（`mcp__mock__list_issues`）——授权交集生效 |
| SSE 事件 | `run_started → sources → tool_call_started → step → tool_call_finished → … → delta → done`（两轮工具调用） |
| 工具事件载荷 | `{"tool":"mcp__mock__list_issues","provider":"mcp","server":"mock","phase":"read","status":"started"}`；`finished` 携带 `summary`（`bytes=116`、`content[0].text={"issues":[]}`、`truncated=false`） |
| 最终答复 | 模型基于工具返回撰写的总结（"已调用实时接口查询问题（Issue）列表…当前问题列表：空"），`done` 载荷 `{"conversationId":12,"provider":"deepseek-openai","providerSource":"tenant"}` |
| 审计（`GET /api/v1/agent/tools/invocations?state=all&provider=mcp`） | id 55：`toolName=mcp__mock__list_issues`、`provider=mcp`、`serverName=mock`、`rawToolName=list_issues`、`risk=read`、`status=executed`、`durationMs=12`、`argsRedacted={"requester_id":1,"state":"open"}`、`permissionCheck=passed`、`permissionReason=args_stripped:user_id`、`outputSummary` 含 `{"issues":[]}`；id 56 同形（参数仅注入的 `requester_id`） |

结论：**真实模型 → Bot 策略 → MCP 只读工具 → 结果回灌 → 最终答复 → 审计留痕** 全链路成立；模型尝试传入的身份类参数被剥离并留痕（越权参数防护生效）。

## 3. 场景 B：确定性 LLM 替身（CI/回归口径）

替身的双条件：`llm.provider=mock` **且** `LLM_MOCK_ENABLED=true`（`service/llm_providers.go:620-625`），行为参数见 `service/llm_mock_provider.go:19-34`。

> **本机踩坑（务必注意）**：`LLM_MULTI_PROVIDER_ENABLED=true` 时，解析链"请求级 → 个人默认 → **租户默认** → 静态回退"会优先命中库里的租户 Provider（本机为 `deepseek-openai`），静态替身**不会生效**；且请求级 `provider:"mock"` 会被拒（`AI_PROVIDER_NOT_FOUND`，见 §7）。联调确定性链路时需临时把 `LLM_MULTI_PROVIDER_ENABLED=false`（本机已按此跑通，随后已恢复）。

配置：`LLM_PROVIDER=mock` + `LLM_MOCK_ENABLED=true` + `LLM_MOCK_TRIGGER=__tool__`（默认）+ `LLM_MOCK_TOOL_NAME=mcp__mock__list_issues` + `LLM_MOCK_TOOL_ARGS={"state":"open"}`。

| 观察点 | 实测结果 |
| --- | --- |
| 回复确定性 | 助手文本 = `（mock LLM）这是 E2E 替身模型的确定性回复。`（`has-mock-reply=True`） |
| 工具事件 | `tool_call_started/finished` 各 5 次，每次 `provider=mcp`、`server=mock`、结果 `{"issues":[]}` |
| 审计 | id 57–61 五条 `executed`（`risk=read`，`durationMs` 14–188ms） |
| **护栏** | 第 6 轮被中断：`event: error` → `{"message":"tool loop exceeded max rounds (5)"}`（工具循环上限 5 轮，防止替身/真实模型无限调用） |

结论：替身链路可稳定复现"Bot → MCP 工具调用 → 审计"，适合放进 CI/E2E 断言；同时验证了**工具循环护栏**。

## 4. 场景 C：写工具 Gate3 审批（`create_issue`）

准备（治理 + 授权）：

```powershell
# 治理：标注为写（read_only=false）+ 风险 act_low + 启用
PUT /api/v1/ai/mcp-servers/1/tools/mcp__mock__create_issue/classification
    {"read_only":false,"risk":"act_low","category":"issue"}
POST /api/v1/ai/mcp-servers/1/tools/mcp__mock__create_issue/enable
# 授权给 Bot 9（授权上限 ≤ 模板上限 act_low）
PUT /api/v1/admin/bots/9/grants  {"toolName":"mcp__mock__create_issue","riskLimit":"act_low"}
# 打开写工具面（L1.5）
MCP_WRITE_ENABLED=true   # 重启后端生效
```

请求：`{"query":"__tool__ 请创建一个问题","botId":9}`（替身指定 `LLM_MOCK_TOOL_NAME=mcp__mock__create_issue`、`LLM_MOCK_TOOL_ARGS={"title":"打印机故障-联调"}`）。

| 阶段 | 实测结果 |
| --- | --- |
| 提交（审批前） | 5 轮均发 `tool_call_started` + **`approval_pending`** + **`confirmation_required`**，**没有 `tool_call_finished`**（未执行）；载荷 `{"id":67..71,"tool":"mcp__mock__create_issue","provider":"mcp","server":"mock","phase":"write","status":"pending"}` |
| 待办查询 | `GET /api/v1/agent/tools/invocations?state=pending&provider=mcp` → 5 条，`risk=act_low`、`needsApproval=true`、`expiresAt=2026-09-30T23:32:5x`（TTL 生效） |
| 批准 | `POST /api/v1/agent/tools/67/approve {"approve":true}` → `{"approvalState":"approved","invocationId":67}`；详情：`status=done`、`approvedBy=1`、`durationMs=210`、`outputSummary` 含 mock 返回 `{"created":true}` |
| 拒绝 | `POST .../68/approve {"approve":false,"reason":"联调演示：拒绝路径"}` → `status=rejected`、`approvalReason` 留痕；69–71 同法清理（`state=pending` 归零） |

结论：**MCP 写工具经 Bot 链路绝不会"未批先执行"**；批准后才真正调用外部服务器并写回结果，拒绝路径留原因、可审计。

## 5. 浏览器实测（`/ai/chat`）

| 步骤 | 结果 |
| --- | --- |
| 打开 `/ai/chat`，展开 Bot 选择器 | 选项含「MCP 联调助手」（`GET /api/v1/agent/bots` 200；选择器 `data-testid="bot-selector"`） |
| 选中「MCP 联调助手」，发送 `__tool__ 请列出当前问题` | 助手文本为替身回复；「过程证据」面板显示 **步骤 6 · 工具调用 5**，逐条 `mcp__mock__list_issues  MCP · mock  完成 0.1–0.2s` + mock 返回体；结尾提示 `tool loop exceeded max rounds (5)`「已中断」 |
| 关闭写工具面后再看目录 | `/admin/tools` 中 `create_issue` 显示为 MCP 写工具（未启用时不可授权执行） |

## 6. 复跑命令（PowerShell，本机口径）

```powershell
# 前置：后端 main.exe（:8090）+ mock MCP 服务器（:19090）
cd E:\projects\itsm\itsm-backend
go run ./cmd/mcp-mockserver -addr :19090 -tools default

# 确定性替身（临时改 .env 后重启 backend）
#   LLM_PROVIDER=mock / LLM_MOCK_ENABLED=true / LLM_MULTI_PROVIDER_ENABLED=false
#   LLM_MOCK_TOOL_NAME=mcp__mock__list_issues（写链路改 mcp__mock__create_issue）
#   MCP_WRITE_ENABLED=false|true

# 只读链路
curl.exe -s -N -b $jar -H 'Content-Type: application/json' -H "X-CSRF-Token: $csrf" `
  -d '{"query":"__tool__ 请列出当前问题","botId":9}' http://127.0.0.1:8090/api/v1/ai/chat/stream

# 审计
curl.exe -s -b $jar 'http://127.0.0.1:8090/api/v1/agent/tools/invocations?state=all&provider=mcp&limit=5'

# 浏览器
#   http://localhost:3000/ai/chat → Bot 选择器选「MCP 联调助手」→ 发送含 __tool__ 的消息
```

## 7. 未覆盖 / 残留风险

| # | 项 | 状态 | 说明 |
| --- | --- | --- | --- |
| N-1 | 真实外部系统写入 | 【未核实】 | 本机为 mock MCP（`{"created":true}`），真实服务器行为/错误语义未验证 |
| N-2 | 请求级 `provider:"mock"` | 受限 | 多 Provider 灰度开启时被拒（`AI_PROVIDER_NOT_FOUND`）；确定性链路需关灰度或用 CI 的全新库 |
| N-3 | 浏览器端写审批卡片自动化 | 【未核实】 | `tests/e2e/flows/flow-mcp-chat-chain.spec.ts` 已就位但本机未跑真实栈；本轮写审批验证走 API |
| N-4 | 替身触发词导致的多轮调用 | 已知行为 | 同一触发词每轮都会命中，直到 5 轮护栏；E2E 断言宜取首轮事件或在触发后改写消息 |
| N-5 | MCP 工具 strict 脱敏档 | 已登记 | 见 MCP 方案变更记录「跨线回归修复」：MCP 工具暂无 strict 档可配置入口 |

## 8. 变更记录

| 日期 | 变更人 | 内容 |
| --- | --- | --- |
| 2026-09-30 | AI 辅助执行 | 初稿：Bot 入口/开关/权限使用指南 + 三条链路实测（真实模型 / 确定性替身 / 写工具 Gate3）+ 浏览器实测 + 复跑命令与残留风险 |
