# V2-02 异步任务体验（先给可用答案 + 后台精算完成后通知）· 证据

- 批次：V2-02（方案 §7「V 系列」末项 / §11.4-4「时效体验：先答后精 + 完成通知」）
- 日期：2026-10-18
- 状态：`integration_verified`（前后端一体；含诚实降级与完成通知）
- 原则：**只读生成**（不写任何业务字段）；**先给可用答案**（同步返回零 LLM 的快速参考）；
  **诚实降级**（未配置 Provider 不假装精算）；完成通知 best-effort，前端回执不打断（§3.6 不打扰原则）。

## 1. 交付摘要

### 1.1 后端

| 面 | 内容 |
|---|---|
| 异步精算服务 | `handlers/ai/async_answer.go`（新增）：`AsyncAnswerService` = 快速参考（内嵌帮助索引命中，零 LLM）+ 后台精算（goroutine、有界并发 4、超时 60s 可覆盖）+ 完成通知（best-effort） |
| 任务表语义 | 内存任务表：TTL 30m、容量上限 200（优先淘汰已收口）、**租户 + 用户双隔离**（他人/跨租户/未知/过期统一 not_found → 404，不泄露存在性） |
| 诚实降级 | 未装配生成器（Provider 未配置）→ 任务立即 `degraded`（`reason=generator_unavailable`），快速参考照常返回；精算失败 → `failed` + `reason`（`timeout` / `provider_error`），快速参考仍可用且**不投递完成通知** |
| 端点 | `handlers/ai/async_answer_handler.go`（新增）：`POST /api/v1/ai/async/answers`（prompt 1..1000 字，空/超长 400；立即返回 `{taskId,status,fast}`）；`GET /api/v1/ai/async/answers/:id`（任务快照）；服务未装配 → 503 fail-visible |
| 完成通知 | 精算成功 → 通知中心 `Notification`（`type=ai_async_answer`、`ActionURL=/ai/chat`）；投递失败仅记日志，**不影响任务结果** |
| 装配 | `internal/bootstrap/app.go`：`NewLLMAsyncAnswerGenerator(llmGateway, sugar)` + `NewEntAsyncAnswerNotifier(ent client)`；LLM 未装配 → 生成器 nil → 诚实 degraded |
| RBAC | 两条路由均 `ai:read`；`go run ./cmd/authz-gen` 已再生（precheck 新增 `/api/v1/ai/async/answers` 与 `/api/v1/ai/async/answers/*`） |
| 埋点 | 复用既有 `task_started` / `task_completed` / `task_failed`（**事件字典零变更**，含 durationMs/model/errorCode） |

### 1.2 前端

- `src/lib/api/ai-api.ts`：`aiStartAsyncAnswer` / `aiGetAsyncAnswer` + 任务快照类型（`running/done/degraded/failed`）。
- `src/lib/ai/async/assistant-async-store.ts`（新增）：任务表（容量 10）+ `mergeServerTask` 未读语义
  （running=已读；首次收口=未读；已读后重复轮询保持已读）+ 发起/轮询入口
  （发起失败返回 null fail-visible；轮询失败保留现状**不虚假置错**；用户移除为终态，轮询**不复活**已移除回执）；
  埋点复用 `task_started/completed/failed`。
- `src/components/ai/dock/AssistantAsyncAnswerCard.tsx`（新增）：面板内回执卡——
  running 展示「先看这些（快速参考）」+ 精算中；done 展示精算回答（`refined`）；degraded/failed 如实说明原因；
  可移除回执；**面板打开即视为已读**（清 FAB 未读回执）；挂载期按 3s 轮询 running 任务（无 running → 零定时器零请求）。
- `AssistantChatPanel.tsx`：输入坞新增「后台精算」入口（`data-testid="assistant-async-start"`）——
  **只发起只读后台任务，绝不发送对话/提交任何写动作**；回执卡渲染在输入区上方。
- `AssistantDock.tsx`：FAB 状态点——运行中呼吸点（`assistant-fab-running`）+ 未读回执并入徽标
  （`fabBadgeCount = 主动信号 badgeCount + 异步未读`），对应 §3.2「running/未读回执显示状态点」。

## 2. 复跑命令

```powershell
# 前端（itsm-frontend/）
npx jest src/lib/ai/async src/components/ai/dock/__tests__/AssistantAsyncAnswerCard.test.tsx `
  src/components/ai/dock/__tests__/AssistantChatPanel.test.tsx src/components/ai/dock/__tests__/AssistantDock.test.tsx `
  --coverage=false --watchAll=false --runInBand --silent
npx jest src/components/ai src/lib/ai src/lib/api --coverage=false --watchAll=false --runInBand --silent
npx tsc --noEmit
npx eslint src/lib/api/ai-api.ts src/lib/ai/async src/components/ai/dock/AssistantAsyncAnswerCard.tsx `
  src/components/ai/dock/AssistantChatPanel.tsx src/components/ai/dock/AssistantDock.tsx `
  src/components/ai/dock/__tests__/AssistantAsyncAnswerCard.test.tsx `
  src/components/ai/dock/__tests__/AssistantChatPanel.test.tsx src/components/ai/dock/__tests__/AssistantDock.test.tsx

# 后端（itsm-backend/）
gofmt -l -w handlers/ai/async_answer.go handlers/ai/async_answer_handler.go handlers/ai/async_answer_test.go `
  handlers/ai/service.go router/ai_routes.go internal/bootstrap/app.go
go test ./handlers/ai/ -count=1
go test ./router/ ./middleware/ -count=1
go run ./cmd/authz-gen   # 新增路由 → 再生 precheck 地图（已再生）
```

## 3. 实测结果（2026-10-18）

| 门禁 | 结果 |
|---|---|
| 后端 `handlers/ai`（含新增 6 例：快答+精算+通知、失败保快答、无生成器诚实降级、超时映射、参数校验、端点流+越权 404/503） | 全绿 |
| 后端 `router` + `middleware`（含 precheck 新鲜度） | 全绿 |
| `authz-gen` | 已再生；diff 新增 `/api/v1/ai/async/answers`（POST/GET）两条 |
| 前端异步面（store 7 例 + 回执卡 5 例） | **2 suites / 12 tests** 全绿 |
| 面板 + Dock 接线（含新增 2 例） | 4 suites / 26 tests 全绿 |
| AI 表面回归（`components/ai` + `lib/ai` + `lib/api`） | **127 suites / 1655 tests** 全绿（较 V2-01：+2 suites / +14 tests） |
| `npx tsc --noEmit` | exit 0 |
| `npx eslint`（9 个改动文件） | exit 0 |

## 4. 关键断言（负向与边界）

- **先给可用答案**：`POST` 同步返回快速参考（`fast.source=help_index` 命中项）；无命中 `fast.source=none`（不编造内容）。
- **诚实降级**：生成器 nil → `degraded` + `generator_unavailable`，前端展示「当前未配置模型，仅提供快速参考」，`refined` 为空。
- **失败如实**：`provider_error` / `timeout` → `failed` + reason；快速参考仍可用；**不投递完成通知**（单测断言 notifier 0 次）。
- **越权防护**：同租户他人、跨租户、未知/过期任务 → 404（统一 not_found，不泄露存在性）；缺租户上下文 → 401；服务未装配 → 503。
- **写边界**：前端入口只发起只读任务；面板用例断言 `aiChatStream` **未被调用**（不发送对话、不提交写动作）。
- **未读回执闭环**：running（无回执）→ 收口首次未读 → 面板打开即已读 → FAB 徽标清空；已读后重复轮询保持已读；移除为终态（轮询不复活）。
- **零请求不变量**：无 running 任务 → 零轮询定时器；Dock 未展开 → 面板不挂载（沿用懒挂载，未展开零会话请求）。
- **通知 best-effort**：投递失败只记日志，任务结果与前端回执不受影响。

## 5. 条件项（诚实标注）

- 后台精算为**服务端进程内任务**（非持久队列）：进程重启任务丢失；「跨刷新/跨设备续跑」需任务持久化专项。
- 精算产物暂以回执卡独立展示，**未回填到会话消息线程**（依赖 chat-core 线程改造，后续专项）。
- 真实 Provider / 真库 E2E（含通知中心落库与 `notificationWS` 推送）归联调；本机 Docker 未运行，沿用既有条件项。
- 回执卡/入口文案未 i18n、新组件未入 axe 扫描面。
- 通知点击落点为 `/ai/chat`（工作区）；「点击通知直达 Dock 并定位回执」归后续深链专项。

## 6. 证据入口

- 后端：`itsm-backend/handlers/ai/async_answer.go`、`async_answer_handler.go`、`async_answer_test.go`；
  `router/ai_routes.go`；`internal/bootstrap/app.go`；`middleware/rbac_precheck_gen.go`（再生）。
- 前端：`itsm-frontend/src/lib/ai/async/assistant-async-store.ts`（+ `__tests__`）；
  `src/components/ai/dock/AssistantAsyncAnswerCard.tsx`（+ `__tests__`）；
  `AssistantChatPanel.tsx`、`AssistantDock.tsx` 及其测试；`src/lib/api/ai-api.ts`。