# I0-04 回执与结果落点（Outcome Contract） · 交付证据

- 日期：2026-10-18
- 任务：I0-04（方案 I 系列：回执与结果落点（Outcome Contract）｜验收：创建/草稿任务回执可跳转、可验证）
- 状态：`integration_verified`（事件契约扩展 + 派生规则 + 回执卡 + 双入口接线；浏览器 E2E 归联调）
- 交付物：
  - 后端（事件契约，可选字段）：`ToolStreamEvent.ArtifactID`（`handlers/ai/tool_events.go`，产物类工具 done 事件回带）
    + `toolResultArtifactID`（int/int64/float64/json.Number 兼容）+ 发射点接线（`service.go` 聊天工具链）
  - 前端：
    - 契约：`AIToolStreamEvent.artifactId?`（旧后端不产生 → 降级）
    - 派生：`lib/ai/outcome.ts`（`deriveAssistantOutcomes`：artifact / draft / navigation；去重、上限 4）
    - 回执卡：`components/ai/OutcomeReceipt.tsx`（kind 标签 + 引用 + 「打开」/「复制引用」+ 埋点）
    - 接线：`ChatMessageItem`（每条助手消息自动派生并渲染）+ `useAIChatSession.openOutcome`
      （SPA 导航 + `handoff(from=outcome_receipt)`）→ 工作区与面板双入口传入

## 1. 落点口径（实现即契约）

| 触发 | 落点 | 动作 |
|---|---|---|
| 工具 done + `artifactId` | `artifact: artifact:<id>` | 复制引用（无产物查看路由时不臆造跳转） |
| `draft_ticket_fields` | `draft → /tickets/create`（applyTarget `ticket.create.form`） | 打开创建页（字段草案） |
| `draft_kb_article` | `draft → /knowledge/articles/new`（applyTarget `knowledge.article.form`） | 打开新建文章页 |
| `help_create_draft` | `draft → /ai/help-coverage` | 打开覆盖率看板（草稿待评审） |
| `create_ticket/update_ticket/link_ticket_ci`（pending 事件）或 `pendingApprovals` | `navigation → /ai/approval` | 去审批中心（引用 `invocation:<id>`） |

去重键 `kind:ref`；上限 4 条；无落点不渲染回执卡（旧后端/纯问答零影响）。
「可验证」= 回执卡展示稳定引用（`artifact:31` / `invocation:9`）且可一键复制；「可跳转」= 注入 SPA 导航
（缺省 `window.location.assign` 兜底）。

## 2. 验证证据（可复跑）

```powershell
cd itsm-backend
gofmt -l -w handlers/ai/tool_events.go handlers/ai/tool_events_artifact_test.go handlers/ai/service.go
go test ./handlers/ai/ -run 'ToolResultArtifactID|ToolStreamEvent_ArtifactID' -count=1   # ok
go test ./handlers/ai/ -count=1                                                          # 全包 ok

cd ../itsm-frontend
npx jest src/components/ai src/lib/ai --coverage=false --watchAll=false --runInBand
# → 26 suites / 170 tests（新增：派生 5 例 + 回执卡 4 例 + 消息级集成 2 例）
npx tsc --noEmit ; npx eslint src/lib/ai/outcome.ts src/components/ai/OutcomeReceipt.tsx src/components/ai/chat-core/ChatMessageItem.tsx …
```

关键断言：
- 后端：`toolResultArtifactID` 兼容 int/int64/float64/json.Number，非法/无产物/非 map → 0；
  事件 JSON 仅在产物 id>0 时含 `"artifactId"`（其余事件字节形状不变——`TestToolStreamEvent_ArtifactIDJSONContract`）；
- 派生：产物落点、草稿规则（含 route/applyTarget）、pending+pendingApprovals 去重、无落点为空、上限 4；
- 回执卡：kind 标签/引用/描述渲染；仅 `route` 存在时显示「打开」；打开与复制分别记录 `inline_applied(open|copy)`；
- 消息级：`draft_ticket_fields(done, artifactId=31)` + `pendingApprovals=[9]` → 回执含「产物 #31/去创建工单/去审批中心 #9」，
  「打开」调用注入的 SPA 导航；无落点消息不渲染回执卡。

## 3. 边界与条件项（不阻塞）

- **entity（业务对象）落点**待后端回带对象 id（如 create_ticket 审批通过后的 ticketId）——当前写路径 pending 以
  `invocation:<id>` 表意；下一迭代可在审批结果/会话回读时补 `entityRef`；
- 事件契约新增为**可选字段**（`omitempty`），旧客户端忽略；`tool_events.go` 契约注释已同步登记；
- 产物查看页（artifact viewer）尚不存在：artifact 落点提供稳定引用与复制，深链在后续批次与产物页一并交付；
- 浏览器级 E2E（流式回合 → 回执出现 → 打开/复制）归联调。