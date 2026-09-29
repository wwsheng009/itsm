# B2-04 实施证据（BotSelector 与会话归属）

> 文档类型：实施证据（任务 B2-04）
> Status: draft
> 编制日期：2026-09-27
> 任务：B2-04（工作区 Bot 切换；新会话 `conversation → bot` 绑定；依赖 B2-01、B1-08）
> 关联文档：`docs/plan/ai-bot-capability-landing-implementation-plan-2026-09-27.md`（§4.3 B2-04、§5.2 AB2-04）
> 核查方式：ent 迁移与仓储契约（真实 SQLite）+ 服务层归属解析优先级 + handler/路由/预检映射 + 前端组件与 API 客户端测试 + `tsc --noEmit`

## 1. 结论

| 项 | 判定 | 说明 |
| --- | --- | --- |
| B2-04 | **`unit_verified`** | 选择器可用、新会话归属落库、`audience` 过滤生效（AB2-04 三项判据全部达成）；条件项：浏览器级交互与截图归 B2-06/BT-09 |
| 兼容默认 | ✅ | 未选择 = 默认助手（`bot_id=0`）；端点未开启（`bot.enabled=false`）→ 候选为空 → 选择器不渲染；未选择时请求体**不落 `botId` 字段**（与现状逐字节一致） |
| 切换不串改历史 | ✅ | 选择仅对新会话生效：已有会话时选择器锁定；归属解析优先级中「会话已绑定」独立于「本次选择」 |
| 悬念项 | 2 条 | G-B2-04-1：AIChat 级集成断言（选择器变更 → 下一次新会话请求带 `botId`）归 B2-06；O-3：`ticket-attachment-api.test.ts` 1 例**既有失败**（非本任务引入，见 §5） |

## 2. 交付物

| # | 层 | 文件 | 说明 |
| --- | --- | --- | --- |
| 1 | 后端 | `itsm-backend/ent/schema/conversation.go`（修改） | 新增 `bot_id`（default 0）；`go generate ./ent/` 已重新生成 |
| 2 | 后端 | `itsm-backend/handlers/ai/entity.go`、`repository_impl.go`（修改） | 领域实体 `Conversation.BotID` + 读写映射（创建写入、查询/列表读出） |
| 3 | 后端 | `itsm-backend/handlers/ai/service.go`（修改） | ①`WithBotID`/`BotIDFromContext`（ctx 传递本次选择）；②`resolveBotID` 四级优先级；③`ensureConversation` **统一新会话创建入口**（Chat 与 ChatStream 共用，消除两处重复） |
| 4 | 后端 | `itsm-backend/handlers/ai/handler.go`（修改） | `/ai/chat` 与 `/ai/chat/stream` 请求体新增可选 `botId`（增量字段，缺省行为不变） |
| 5 | 后端 | `itsm-backend/service/bot/admin.go`（修改） | `VisibleForRole`（audience 判定，fail-closed）+ `ListVisibleForChat`（非 draft × audience × chat 入口） |
| 6 | 后端 | `itsm-backend/handlers/ai/bot_admin.go`、`router/bot_routes.go`（修改） | `GET /api/v1/agent/bots`（ai:read，最小字段集）；`handler==nil` 时整组不注册；`go run ./cmd/authz-gen` 已再生成预检映射 |
| 7 | 前端 | `itsm-frontend/src/lib/api/ai-api.ts`（修改） | `AIChatStreamRequest.botId` / `AIApi.chat({botId})`（未选择不落字段）+ `BotOption` + `aiListVisibleBots()`（失败/404 → 空数组） |
| 8 | 前端 | `itsm-frontend/src/components/ai/BotSelector.tsx`（新建） | 候选为空/加载中不渲染；默认助手哨兵 0；已有会话锁定（Tooltip 提示"仅影响新会话"） |
| 9 | 前端 | `itsm-frontend/src/components/ai/AIChat.tsx`（修改） | 工具栏接入选择器（Provider 选择器左侧）；挂载时拉取候选；stream 与降级两条路径都带 `botId` |
| 10 | 测试 | 见 §4 | 后端 5 包全量 + 前端 2 套件 |

## 3. 归属解析优先级（`resolveBotID`，可审计）

| 优先级 | 来源 | 语义 |
| --- | --- | --- |
| ① | `WithBotID(ctx)`（请求 `botId`） | 选择器对**新会话**的选择；由 Chat/ChatStream handler 注入 |
| ② | `SetBotIDResolver` 注入的解析器 | 预留扩展点（未装配时跳过） |
| ③ | `conversation.bot_id` | 历史会话回读（不随选择器变化改写） |
| ④ | 0 | 内置默认助手（策略层按 `slug=default-assistant` 解析） |

**创建时写入**：`ensureConversation` 是唯一创建入口；`botId` 取自 ctx（0 = 默认助手）。
**首条消息即绑定**：选择器只对 `conversationId` 为空的首条消息生效（此后该会话归属已固定）。

## 4. 运行记录（本机，pwsh）

| # | 命令 | 结果 |
| --- | --- | --- |
| 1 | `go test ./service/bot/ ./handlers/ai/ ./router/ ./middleware/ ./ent/schema/ -count=1 -timeout 30m` | **全绿** 15.1s / 24.2s / 21.4s / 7.3s / 15.7s |
| 2 | `go build ./...` | exit 0 |
| 3 | `go run ./cmd/authz-gen` | 已生成（含 `/api/v1/agent/bots → ai:read`）；`TestPrecheckMapIsFresh` 绿 |
| 4 | `npx tsc --noEmit` | 干净（exit 0） |
| 5 | `npx jest src/components/ai` | 6 套件 / 52 例全绿（含新增 selector 5 例） |
| 6 | `npx jest ai-api.test.ts + bot-selector.test.tsx` | 2 套件 / 54 例全绿（含 `botId` 落字段边界与 404 兜底） |
| 7 | `gofumpt -l` | 无输出（格式干净） |

覆盖点：`bot_id` 往返与租户隔离；四级优先级（显式选择 > 解析器 > 会话绑定 > 默认）；新会话创建写入选择；`VisibleForRole` 12 组边界；可见列表（draft/入口/audience/租户）；handler 角色过滤与最小字段集；路由注册与 nil-handler 404；预检映射声明。

## 5. 遗留与发现

| # | 项 | 归属/处置 |
| --- | --- | --- |
| 1 | G-B2-04-1：AIChat 级集成断言（选择 → 新会话请求带 `botId`）与浏览器交互/截图 | **B2-06**（B2 集成验收）/ BT-09 |
| 2 | O-3：`itsm-frontend/src/lib/api/__tests__/ticket-attachment-api.test.ts` 1 例失败（CSRF 回归用例），**本任务未触碰该文件**（`git status` 与最近提交 `a48469ec` 可证） | 登记为分支既有失败；建议随 BT-09 或独立小修处置（不得在本任务静默改断言） |
| 3 | 选择器文案未接 i18n（与宿主 AIChat 的硬编码中文风格一致） | 全量 i18n 归一化属全局工程项，登记待办 |

## 6. 变更记录

| 日期 | 变更 | 说明 |
| --- | --- | --- |
| 2026-09-27 | 初稿 | B2-04 交付：`conversation.bot_id` + 归属解析四级优先级 + `GET /api/v1/agent/bots`（audience 过滤）+ `BotSelector` 接入工作区；后端 5 包全量回归全绿、前端 54 例全绿、`tsc`/`gofumpt` 干净；判定 `unit_verified`（条件项与既有失败见 §5） |
