# O1-02 实时健康与告警（SLO + 通知中心投递） · 交付证据

- 日期：2026-10-18
- 任务：O1-02（方案 §13.5 实时健康与告警｜验收：告警进通知中心；SLO 定义）
- 状态：`integration_verified`（快照/SLO/派发端点 + 前端健康卡；定时巡检与订阅偏好映射归后续）
- 交付物：
  - 后端：`service/ai_health.go`（快照 + 纯函数 SLO 评估 + 告警派发）+ `ToolQueue.Len/Cap`
    （队列积压只读口径）+ `handlers/ai/health.go` + 路由
    `GET /api/v1/ai/health`（ai:read）、`POST /api/v1/ai/health/alerts/dispatch`（ai:write）+
    bootstrap 装配（复用同一 `bot.MetricsService` 实例）+ `authz-gen` 预检再生（两条路由已登记）
  - 前端：`aiGetAIHealth()` / `aiDispatchAIHealthAlerts()`（`lib/api/ai-api.ts`）+
    `/ai/observability`「实时健康与 SLO」卡（指标 + SLO 标签 + notes + `ai:write` 才渲染的派发按钮 +
    派发结果/失败提示）

## 1. SLO 定义（§13.5 建议值的实现口径）

| rule | 指标 | 阈值 | 样本下限 | 数据源 |
|---|---|---|---|---|
| `ai_task_success_rate` | 任务成功率（gte） | ≥95% | 完成+失败 runs ≥5 | `bot_runs`（MetricsService） |
| `ai_tool_error_rate` | 工具错误率（lte） | ≤5% | 调用数 ≥5 | `tool_invocations`（MetricsService） |
| `ai_first_token_p95` | 首 token P95（lte） | ≤3000ms | 有 latency 埋点 | `ai_telemetry_events.latency_ms`（first_token 为主事件，口径记录于代码注释） |
| `ai_error_recovery_rate` | 错误恢复率（gte） | ≥90% | `error_shown` ≥5 | 埋点 `error_shown`/`error_recovered` 计数 |
| `ai_llm_success_rate` | LLM 调用成功率（gte） | ≥95% | 调用数 ≥5 | `ai_llm_calls`（平台级 byProvider 聚合） |

样本不足 → `insufficient_data`（**不误报**）；快照数据源缺失（遥测未装配等）写入 `notes` 显式降级。

## 2. 告警投递口径（通知中心）

- 收件人：租户**活跃管理员**（`role ∈ admin/super_admin/tenant_admin`）→ 无管理员时回退活跃用户，上限 50；
- 去重：同租户 + 同标题 + 30 分钟内已存在 → 跳过（`skipped` 计数），避免告警风暴；
- 通知行：`type=ai_health`、`action_url=/ai/observability`（通知中心直接可见、可跳转）；
- 严重级：临界违规 `warning`；明显偏离（gte 低于阈值 10%+ / lte 超阈值 50%+）为 `critical`；
- 形态约束：仅显式触发派发（管理台按钮/API），不自动弹窗（与 §3.6 不打扰原则一致）。

## 3. 验证证据（可复跑）

```powershell
cd itsm-backend
gofmt -l service/ai_health.go service/ai_health_test.go service/tool_queue.go handlers/ai/health.go handlers/ai/health_handler_test.go
go test ./service/ ./handlers/ai/ ./router/ ./middleware/ ./service/bot/ -count=1   # 5 包全 ok（service 57s）
go run ./cmd/authz-gen      # 再生：/api/v1/ai/health → ai:read；/api/v1/ai/health/alerts/dispatch → ai:write

cd ../itsm-frontend
npx jest src/components/ai src/lib/ai src/lib/store src/lib/api/__tests__/ai-api.test.ts --runInBand  # 35 suites/293 tests
npx jest --testPathPattern "ai/observability" --runInBand        # 1 suite / 10 tests
npx tsc --noEmit ; npx eslint "src/pages/*/ai/observability/*" src/lib/api/ai-api.ts src/lib/i18n/translations.ts
```

关键断言：
- 纯函数 SLO：80% 任务成功率 → `breach` + `critical`；33% 工具错误率 → `breach`；
  0.9 恢复率 → `ok`；样本 1 → `insufficient_data`；`nil` 快照 → `nil`；无违规 → 零告警；
- 快照：10 runs（8/2）→ `total=10`、`successRate=0.8`；6 次工具调用（4/2）→ `errorRate=1/3`；
  队列 stub（3/100）→ `available=true`；遥测 nil → `notes` 非空；窗口 clamp（999 → 30）；
- 派发：2 名活跃管理员 → `recipients=2`、`created=2×告警数`；通知行 `type=ai_health`、
  `action_url=/ai/observability`、停用/普通用户不投递；**第二次派发 created=0（30 分钟去重）**；
  跨租户数据不足 → 零告警、零通知（租户隔离）；无管理员租户 → 回退活跃用户（停用排除）；
- 端点：GET 200（快照 + SLO 数组）、`?days=999` clamp、未装配 503；POST 200 + 通知落库 + 去重；
- 前端：健康卡指标（80.0% / 33.3% / 2/100 / 95.0%）、SLO 标签（违规/通过）、notes 展示；
  `ai:write` 下派发按钮可见 → 调 `aiDispatchAIHealthAlerts(days)` 并显示「新建 2 条…收件人 2」；
  无 `ai:write` 不渲染派发按钮（负向）。

## 4. 边界与条件项（不阻塞）

- 定时巡检（sweeper 周期评估 + 派发）与「AI 健康」通知的订阅/退订偏好映射
  （`notification-preferences` 的 eventType）归后续迭代；当前为显式触发（避免后台打扰默认开启）；
- 首 token P95 目前取埋点 `latency_ms` 窗口 P95（`first_token` 为主事件）；如需按事件名拆分 P95
  需在 O0-02 聚合层加 `byName × latency` 维度；
- Provider 维度为平台级口径（`ai_llm_calls` 无 `tenant_id`，与既有 `/ai/metrics` 一致）；
- SLO 阈值为代码常量（方案建议值）；按租户策略覆盖（system_configs）归后续；
- 浏览器 E2E（通知中心看到 `ai_health` 通知并跳转看板）归联调。