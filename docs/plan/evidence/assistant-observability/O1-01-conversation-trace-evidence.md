# O1-01 条件项闭环证据：纯问答轨迹（conversation 维度）+ 审计页行内跳转

- 日期：2026-10-18
- 状态：**integration_verified**（本机）
- 方案出处：`docs/plan/ai-assistant-dock-plan-2026-10-18.md` O1-01 进度块条件项原文：
  「纯问答轨迹（conversation 维度）与审计页行内跳转/面板诊断抽屉（O2-02）复用归后续」。
  - 面板诊断抽屉复用（O2-02）已在上一批交付；本批闭环 **conversation 维度轨迹** 与 **审计页行内跳转**。

## 1. 交付内容

### 1.1 后端：会话维度全链路轨迹

- `service/bot/conversation_trace.go`（新增）
  - `GetConversationTrace(ctx, tenantID, conversationID)`：租户前置（跨租户与不存在同口径
    `ErrConversationNotFound`，不泄漏存在性）；一次聚合 **会话档案 + 消息回合（ID 升序）+
    运行列表（started_at 倒序）+ 工具调用（确认/审批/verify）+ 产物**；
  - **纯问答（无 bot_runs）同样可回放**：消息与工具/产物均按 `conversation_id` 归属读取；
  - 截断口径（显式，不伪装全量）：单条消息内容按 2000 跑数截断（`truncated`）、单次窗口 200 条
    （`messagesTruncated` + `messageCount` 如实总量）、运行列表窗口 50；
  - 脱敏：`arguments` 绝不回显，只回 `argsRedacted`——与 run 轨迹共用 `mapTraceTool` /
    `mapTraceArtifact`（`trace.go` 的重复映射同步收敛为同一实现，口径零分叉）。
- `handlers/ai/conversation_trace.go`（新增）+ `handlers/ai/service.go` 委托（nil 轨迹服务 →
  `ErrTraceServiceUnavailable`）：`GET /api/v1/ai/conversations/:id/trace`
  - 400 非法 id；404 未命中或跨租户（同口径）；503 轨迹服务未装配；500 其他（warn 日志带 tenant/conversation）。
- `router/ai_routes.go` 注册（`ai:read`）；`go run ./cmd/authz-gen` 再生
  `middleware/rbac_precheck_gen.go`，新增 `/api/v1/ai/conversations/*/trace → ai:read`。

### 1.2 前端：会话维度回放 + 双入口 + 审计行内跳转

- `src/lib/api/ai-api.ts`：`AIConversationTrace*` 契约 + `aiGetConversationTrace`（数组/缺省字段归一）；
  `ToolApproval.runId?` 补齐（后端 `runId` 原样返回）。
- `src/components/ai/run-trace/ConversationTraceView.tsx`（新增）：会话档案 → 消息回合（角色/内容/截断标记）
  → 运行记录（跳 `?runId=`）→ 工具调用与确认 → 产物 → 业务对象；复制会话 ID 埋点
  （`handoff(source=trace_copy, extra.reference=conversation)`）。
- `DiagnosticsDrawer`：深度轨迹 **run 维度优先**；无 run 且有会话 ID → **回退会话维度**（加载/失败/无目标三态）；
  无任何 ID 才显示降级提示。关闭即释放（懒加载口径不变）。
- `/ai/observability`：新增 `?conversationId=` 深链与「回放会话」入口；与 run 维度 **互斥**（避免双请求歧义，
  同 URL 同时给出时 runId 优先）。
- `/ai/audit` 工具调用审计表：新增「轨迹」列行内跳转——有 run 优先 `?runId=`，否则 `?conversationId=`；
  两者皆无则不渲染入口（不臆造目标）。
- i18n：`aiObservability.conversationTrace*` 键（zh/en，占位符 `{shown}/{total}/{count}` 与
  `useI18n` 替换实现一致）。
- a11y：`a11y-surfaces.test.tsx` 新增「纯问答会话轨迹抽屉」axe 扫描（扫描面 6→7）。

## 2. 可复跑命令与实测结果

### 后端（`itsm-backend/`）

```pwsh
go test ./service/bot/ ./handlers/ai/ -count=1     # ok（新增 4 + 2 例：链路/截断/租户隔离/端点契约）
go run ./cmd/authz-gen                              # 再生 precheck：新增 /api/v1/ai/conversations/*/trace（ai:read）
go test ./router/ -count=1                          # ok 5.9s（路由注册与 RBAC 对齐）
go test ./tests/botintegration/ -count=1            # ok 5.0s（真实 handler→SSE 回归）
go test ./service/ -count=1                         # ok 53.7s（整包回归）
```

新增覆盖（关键断言）：
- 链路：会话档案/消息升序/运行倒序/工具确认（`pending`）/产物；**纯问答 `Runs` 为空仍回放消息与工具**；
- 截断：单条 >2000 跑数 → 截断 + `truncated=true`；消息总数 >200 → 窗口 200 + `messagesTruncated=true`；
- 租户隔离：跨租户 = `ErrConversationNotFound`（与不存在同口径）；非法入参同口径；nil 服务非 NotFound（→503）；
- 端点：200 链路、404（未命中）、400（非法 id）、503（未装配）；
- 脱敏：响应 JSON 不含 `secret-args`，含 `***`。

### 前端（`itsm-frontend/`）

```pwsh
npx jest --runTestsByPath 'src/components/ai/run-trace/__tests__/ConversationTraceView.test.tsx' `
  'src/components/ai/run-trace/__tests__/RunTraceView.test.tsx' `
  'src/components/ai/diagnostics/__tests__/DiagnosticsDrawer.test.tsx' `
  'src/components/ai/__tests__/AIChat.diagnostics.test.tsx' `
  'src/components/ai/__tests__/a11y-surfaces.test.tsx' --coverage=false --watchAll=false --runInBand
  # 通过（ConversationTraceView 3 例 / Drawer 9 例含 3 例条件项 / AIChat 诊断 2 例 / axe 7 例）

npx jest --runTestsByPath 'src/pages/(main)/ai/observability/__tests__/index.test.tsx' `
  'src/pages/(main)/ai/audit/__tests__/index.test.tsx' --runInBand
  # 2 suites / 19 tests 通过（observability 新增会话轨迹加载 + ?conversationId= 深链 404 两例；
  #  audit 新增行内跳转三断言：run 优先 / 会话回退 / 无 ID 不渲染）

npx jest src/components/ai src/lib/ai --runInBand     # 48 suites / 305 tests 全绿
npx jest src/lib/api --runInBand                      # 81 suites / 1373 tests 全绿
npx tsc --noEmit                                      # exit 0
npx eslint <12 个变更文件>                             # exit 0
```

## 3. 边界与诚实说明

- **真实 PG/RLS 冒烟与浏览器 E2E 待环境**（本机 Docker 未运行）：跨租户/未命中口径为 sqlite 内存库 +
  `tenantctx` RLS 语义验证；登记为环境依赖条件项。
- **审计页行内跳转范围**：AI 场景审计行（`ai_feedbacks.item_type='ai_audit'`）不含 conversation/run ID，
  行内跳转只覆盖工具调用审计行（数据真源 `tool_invocations`，含 `conversation_id`/`run_id`）。
- **会话轨迹不含遥测事件**：`ai_telemetry_events` 属观测面（O0 系列），本轨迹只聚合业务数据面
  （会话/消息/运行/工具/产物），不扩张口径（避免把采样埋点当作轨迹事实）。
- **运行列表窗口 50 条**：超出部分可经对应 run 轨迹逐条查看；分页不在本批范围。
- 消息内容截断上限 2000 跑数/单次 200 条为**传输与渲染保护**，非数据删除（数据库原值不动）。