# V0-01 工单详情三件套（Inline 优先）+ 基线采集 · 交付证据

- 日期：2026-10-18
- 任务：V0-01（方案 §7 V 系列：工单详情三件套 = 分诊建议 + 答复草稿 + 知识推荐，**Inline 优先**；验收 = 处理一张单的点击/输入/耗时下降 ≥30%，对照上线前 1 周基线）
- 状态：`integration_verified`（三件套 Inline 落地 + 基线埋点接线；≥30% 对照属运营灰度动作，依赖 1 周真实基线）
- 交付物：
  - 后端：`handlers/ai/reply_draft.go`（`POST /ai/tickets/:id/reply-draft`：只读生成 + KB 引用 + ai_audit）、
    `router/ai_routes.go`（`ai:read` + `ticket:read` 双门禁）
  - 前端：`components/business/TicketAIInlineAssistant.tsx`（草稿 + 知识推荐 Inline 面板）、
    `CommentPanel` 新增 `externalDraft`（**只填入、不提交**）、`TicketDetail` 接线（草稿 → 评论框 nonce 注入）、
    `lib/api/ai-api.ts`（`aiCreateReplyDraft` + `ReplyDraftResult/ReplyDraftCitation` 契约）

## 1. 三件套映射（Inline 优先）

| 件 | 实现 | Inline 动作 | 写边界 |
|---|---|---|---|
| 分诊建议 | 既有 `AISuggestionPanel`（`/ai/triage` + 采纳写回 + `ai_audit`，本次复用未改） | 「采纳建议」直接更新工单分类/优先级 | 写回经既有 `updateTicket` 权限链 |
| 答复草稿 | **新增** `TicketAIInlineAssistant` + `POST /ai/tickets/:id/reply-draft`（工单 + KB Top3 grounding，空 model = 部署默认模型） | 生成/编辑/复制/**填入回复框** | 端点只读（无业务写入）；填入仅覆盖评论输入框，**提交仍由用户点击** |
| 知识推荐 | 复用 `POST /ai/rag/search(kb)`（访问者身份注入 → 分类可见性过滤） | 「引用到草稿」把来源标题追加到草稿 | 只读检索 |

后端契约（`reply-draft`）：200 `{ticketId, draft, citations[{title,source,snippet,score}], latencyMs, degraded:false}`；
404 工单不存在/跨租户；503 Provider 未配置或生成失败（**不静默返回空草稿**）；审计落 `ticket_reply_draft`。

## 2. 基线采集（方案 §7 V 系列注："试点上线前先用同一埋点采集现状"）

Inline 面板接线的事件与维度（O0-01 同一 SDK → O0-02 事件表 → O0-03 看板）：

| 事件 | 触发 | 维度/载荷 |
|---|---|---|
| `inline_preview` | 知识推荐返回 / 草稿生成成功 | `pageKey=ticket.detail`、`targetType=ticket`、`targetId`、`extra.panel=kb\|reply_draft`、`extra.count` |
| `inline_applied` | 填入回复框 / 复制 / 引用到草稿 | `extra.panel=reply\|kb`、`extra.action=fill\|copy\|quote`、`extra.chars` |
| `task_started` / `task_completed` | 草稿生成开始/成功 | `taskCardId=reply_draft`、`durationMs`、`outcomeType=draft`（**耗时口径**） |
| `task_failed` / `error_shown` | 草稿生成失败 | `extra.task`、`errorCode=reply_draft_failed`（失败率口径） |

对照指标定义（处理一张单，试点 vs 基线 1 周）：
- **点击次数**：`inline_applied(action=fill)` 后到评论提交之间的 `message_sent`/评论提交次数对比人工流程；
- **输入字符数**：复用 `message_sent` 的长度口径（chat-telemetry 只报长度，不报原文）+ 草稿填入长度差异；
- **耗时**：`task_completed.durationMs`（AI 生成）与「进入详情页 → 评论提交」会话时长（事件时间差）；
- 基线/对照的自动聚合报告归 **O2-01**（方案 §11 对照报告）；本批交付其所需事件与维度。

## 3. 验证证据（可复跑）

```powershell
cd itsm-backend
gofmt -l -w handlers/ai/reply_draft.go handlers/ai/reply_draft_test.go router/ai_routes.go
go test ./handlers/ai/ -count=1                       # 全包 ok（reply-draft 4 例：成功/隔离·404/未装配 503/Provider 失败 503）
go run ./cmd/authz-gen && go test ./middleware/ -run 'TestPrecheckMapIsFresh|TestRoutePrecheckAlignment' -count=1

cd ../itsm-frontend
npx jest "src/components/business/__tests__/TicketAIInlineAssistant.test.tsx" src/components/ai src/lib/ai src/lib/api/__tests__/ai-api.test.ts --runInBand
# → 21 suites / 198 tests（Inline 组件 5 例：KB 自动检索与埋点 / 草稿成功与 task_completed / 失败与 error_shown / 填入回调 / 引用到草稿）
npx tsc --noEmit ; npx eslint src/components/business/TicketAIInlineAssistant.tsx src/components/ticket/TicketDetail.tsx src/components/business/detail-tabs/CommentPanel.tsx
```

组件关键断言：KB 检索 query = `标题 + 描述`、`inline_preview(panel=kb)`；生成成功 → 输入框含草稿 + `task_completed(durationMs)`；
失败 → 可见错误 + `task_failed` + `error_shown`；「填入回复框」只回调文本并记 `inline_applied(fill)`；「引用到草稿」追加 `（引用：… kb#id）` 并记 `inline_applied(quote)`。

## 4. 既有测试问题（登记，非本批回归）

`src/components/ticket/__tests__/TicketDetailAssignSearch.test.tsx` 在当前工作树失败（4/4）：
其 `auth-store` mock 只提供 `useAuthStore()`、缺 `getState`，而 `RecordProjectLink → ProjectSelect → usePermissions`
走 `useAuthStore.getState()`；对照实验：临时禁用 V0-01 渲染块后**同样 4 failed**，且 `ProjectSelect/RecordProjectLink/
use-permissions` 均未改动（usage 行不在本批 diff 内）→ **既有 mocking gap**。建议治理（补 mock `getState` 或 mock RecordProjectLink）。

## 5. 边界与条件项（不阻塞）

- 「处理一张单耗时/点击/字符数下降 ≥30%」为**试点验收门槛**：需生产环境按上表采集 ≥1 周基线后对照；本批不伪造该结论；
- 真实 Provider E2E（配置 LLM 后生成中文草稿）归联调；KB 引用当前为 `kb#<id>` 文本来源，深链跳转待知识域路由批次；
- 「填入回复框」要求评论 Tabs 已挂载（CommentPanel 存在）；未挂载时用户仍可复制草稿（已提供复制按钮）；
- 灰度开关 `assistant.dock.enabled`/页面白名单沿用既有平台配置，不在本批改动。