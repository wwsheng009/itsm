# B4-02 实施证据（指标与成本看板）

> 文档类型：实施证据（任务 B4-02）
> Status: draft
> 编制日期：2026-09-27
> 任务：B4-02（运行维度指标：成功/确认/verify/工具错误/时延/成本；后端聚合 + 前端看板）
> 关联文档：`docs/plan/ai-bot-capability-landing-implementation-plan-2026-09-27.md`（§4.5 B4-02、§5.2 AB4-02）、阶段一报告 §5.9
> 核查方式：Go 集成测试（真实 ent/sqlite + 固定时钟）+ HTTP 契约测试 + 前端组件测试 + 静态检查

## 1. 结论

| 项 | 判定 | 说明 |
| --- | --- | --- |
| AB4-02 | ✅ | 指标可查（run 成功率 / 确认率 / verify 失败率 / 工具错误率 / 时延 / 成本代理），按 Bot 与入口分解；看板页面与 503 降级态就位 |
| B4-02 | **`integration_verified`** | 后端聚合 12 组断言 + HTTP 契约 2 例 + 前端 3 例全绿；看板截图归 BT-09/CI（条件项） |

## 2. 交付物

| # | 文件 | 内容 |
| --- | --- | --- |
| 1 | `itsm-backend/service/bot/metrics.go`（新建） | `MetricsService.Summary`：窗口（默认 7 天 / 上限 90 天，可注入时钟）+ 运行/步骤/工具/verify/确认/成本六段聚合 + `ByEntrypoint`/`ByBot` 分解；所有查询带租户前置；行数上限 50000 并在 `notes` 明示截断 |
| 2 | `itsm-backend/service/bot/metrics_test.go`（新建） | 真实 ent/sqlite + 固定时钟：全量聚合、按 Bot/入口过滤、空库零安全、参数校验、窗口收敛与窗口外排除 |
| 3 | `itsm-backend/handlers/ai/service.go` | `SetBotMetrics` + `GetBotMetrics` + `ErrBotMetricsUnavailable`（未装配 = 503，与 `bot.enabled=false` 兼容态一致） |
| 4 | `itsm-backend/handlers/ai/handler.go` | `GetBotMetrics` handler：`days`/`botId`/`entrypoint` 解析 + 503 分支 |
| 5 | `itsm-backend/handlers/ai/bot_metrics_test.go`（新建） | HTTP 契约：未装配 → 503 + 文案；已装配 → 200 + `windowDays/tokensRecorded/runs.total` 形状（空库零安全） |
| 6 | `itsm-backend/router/ai_routes.go` | `GET /api/v1/ai/bot-metrics`（`RequirePermission("ai","read")`） |
| 7 | `itsm-backend/internal/bootstrap/app.go` | `bot.enabled=true` 时装配 `botService.NewMetricsService(client)`（关闭态服务不注入） |
| 8 | `itsm-frontend/src/lib/api/ai-api.ts` | `BotMetrics*` 类型 + `aiGetBotMetrics(params)`（`days/botId/entrypoint`） |
| 9 | `itsm-frontend/src/pages/(main)/ai/bot-metrics/index.tsx`（新建） | 看板：4 张 KPI 卡（运行成功率/确认通过率/回读失败率/工具错误率）+ 成本代理卡（LLM/工具/步数/平均时延，token 未接线提示）+ 入口/Bot 分解表 + 时间窗与入口筛选 + 503「能力未启用」降级 |
| 10 | `itsm-frontend/src/pages/(main)/ai/bot-metrics/__tests__/index.test.tsx`（新建） | 3 例：KPI 与分解表渲染、503 降级、其他错误提示 |
| 11 | `itsm-frontend/src/routes/index.tsx` + `route-paths.ts` | 路由 `ai/bot-metrics`（菜单挂载点评审中，登记见 §5） |
| 12 | `itsm-frontend/src/lib/i18n/translations.ts` | zh/en 各 30 键（`aiBotMetrics.*`） |

## 3. 指标口径（与阶段一报告 §5.9 对齐）

| 指标 | 定义 | 边界处理 |
| --- | --- | --- |
| 运行成功率 | `completed / (completed + failed)` | `running/cancelled` 不进分母；空数据 = 0（不产出 NaN） |
| 平均运行时长 | 已结束运行（completed/failed）的 `finished_at - started_at` | 无完成样本 = 0 |
| 确认通过/拒绝/过期率 | 分母 = `approved + rejected + expired` | 挂 `pending` 且 `expires_at < now` 计为**过期**（惰性判定与 B1-05 一致）；真 pending 单列 |
| 平均确认时延 | `approved_at - created_at`（已决单） | 无样本省略 |
| verify 失败率 | `failed / (verified + failed)` | `skipped/pending` 不计入分母 |
| 工具错误率 | 执行面调用中 `error_code != ""` 或状态非成功态 | **未执行的 pending/rejected/expired 确认单不计入执行面**；审批通过后执行的调用按已执行计入 |
| 成本代理 | LLM 步数 / 工具调用数 / 总步数 / 平均步数·工具数每运行 | `tokensRecorded=false`（token 未接线，B1-02 遗留），页面显式提示 |
| 分解 | `ByEntrypoint`（chat/ticket_detail/…）、`ByBot`（`bot#<id>` 或 `compat-default`） | 按运行数降序、键名升序稳定排序 |

**与方案的一处口径调整（已记录）**：方案原文为「复用 `/ai/metrics` 扩展」。实现改为**新增专用端点** `GET /ai/bot-metrics`，理由：①既有 `/ai/metrics` 载荷保持字节稳定（回归零风险）；②Bot 指标权限/窗口语义不同（`ai:read` + 窗口上限 90 天）；③`/ai/metrics` 由 telemetry 服务承载，混入 Bot 聚合会让两个数据源耦合。

## 4. 测试与运行记录

| # | 用例/检查 | 命令 | 结果 |
| --- | --- | --- | --- |
| 1 | `service/bot` 指标聚合（4 测试函数 / 12 断言组） | `go test ./service/bot/ -run TestMetricsService -count=1` | ✅ `ok 6.237s` |
| 2 | HTTP 契约（未装配 503 / 已装配 200） | `go test ./handlers/ai/ -run TestGetBotMetrics -count=1` | ✅ `ok 2.507s` |
| 3 | 看板组件（3 例） | `npx jest --testPathPattern "ai/bot-metrics"` | ✅ `Tests: 3 passed` |
| 4 | 类型检查 | `npx tsc --noEmit` | ✅ 0 错误 |
| 5 | 编译（`/ai/bot-metrics` 装配链） | `go build ./...` | ✅ 退出码 0 |

覆盖要点（用例 1）：全量聚合（5 run / 11 步 / 5 工具调用 / 确认四态 / verify 四态 / 成本代理 / 双分解）、按 Bot 过滤（工具与步骤按 run 归属收敛）、按入口过滤、空库零安全 + 参数校验 + 窗口收敛（>90 截断、<=0 默认）+ 窗口外数据排除、**跨租户隔离**（他租户 run/tool 不混入）。

## 5. 遗留与登记

| # | 项 | 归属 |
| --- | --- | --- |
| 1 | token 消耗计量（真实接线：provider usage → `bot_steps`/`bot_runs`） | B4 增补或下一期；当前成本为代理口径（页面已提示） |
| 2 | 看板菜单挂载点（挂 `AI` 组或 `运维` 组）与截图 | 挂载点评审（方案 §4.5 B4-02「评审定」）+ BT-09 截图 |
| 3 | 看板浏览器 E2E（KPI 渲染 + 筛选） | 可并入 `flow-bot-workspace.spec.ts` 扩展或 BT-09 |
| 4 | 指标行数上限（50000）触顶时的分页/抽样策略 | 大租户压测后评估（当前超限在 `notes` 明示） |

## 6. 变更记录

| 日期 | 变更 | 说明 |
| --- | --- | --- |
| 2026-09-27 | 初稿 | B4-02 交付：`MetricsService` 六段聚合 + `GET /ai/bot-metrics` + 看板页面（含 503 降级与 token 未接线提示）；测试 12 断言组 + 2 HTTP + 3 前端例全绿；判定 `integration_verified`（截图与菜单挂载点归 BT-09/评审） |
