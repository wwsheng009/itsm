# O1-01 单任务全链路轨迹（run_id 回放） · 交付证据

- 日期：2026-10-18
- 任务：O1-01（方案 §13.4 单任务全链路轨迹｜验收：会话→run→工具→确认→产物→业务对象可回放）
- 状态：`integration_verified`（后端数据面 + 端点契约 + 前端回放视图/看板入口；浏览器 E2E 归联调）
- 交付物：
  - 后端数据面：`service/bot/trace.go`（`TraceService.GetRunTrace`：一次查询聚合 run / steps / events /
    tool_invocations / artifacts；租户前置；跨租户与不存在同口径 `ErrRunNotFound`；原始参数不回显）
  - 端点：`GET /api/v1/ai/runs/:id/trace`（`ai:read`；`handlers/ai/run_trace.go`；未装配 → 503，未命中 → 404）
    + 装配 `internal/bootstrap/app.go`（`SetTraceService`）+ RBAC 预检再生
    （`middleware/rbac_precheck_gen.go` 新增 `/api/v1/ai/runs/*/trace → ai:read`）
  - 前端：`aiGetRunTrace()`（`lib/api/ai-api.ts`，字面量路径可静态解析）+ `components/ai/run-trace/trace-model.ts`
    （确认标签/业务对象落点/耗时/载荷摘要纯逻辑）+ `RunTraceView.tsx`（链路六段渲染 + 复制轨迹 ID）
  - 入口：`/ai/observability` 新增「轨迹回放」卡片（run_id 输入 + `?runId=<id>` 深链自动加载 + 404 提示）

## 1. 链路映射（实现即契约）

| 链路节点（§13.4） | 数据来源 | 前端展示 |
|---|---|---|
| 会话 | `bot_runs.conversation_id` + `bot_id` | 「会话 #N」（Bot #M） |
| run | `bot_runs`（entrypoint/status/model/耗时/error_code/budget） | 运行卡（状态 Tag + Descriptions） |
| step / event | `bot_steps`（step_index 升序）、`bot_events`（seq 升序 + payload 摘要） | 步骤与事件列表 |
| 工具 → 确认 | `tool_invocations`（含 `confirmationState` 规范化六态、risk、verify、error_code、脱敏 args） | 工具调用与确认卡（确认 Tag + verify Tag） |
| 产物 | `bot_artifacts`（kind/tool/title/content）+ 事件 `artifactId` 引用 | 产物卡（`artifact:<id>` 引用可复制） |
| 业务对象 | run/tool 的 `target_type` + `target_id`（去重） | 业务对象卡（ticket/incident/change/ci 直达链接） |

只读边界：轨迹接口不含任何写操作；原始 `arguments` 绝不回显（仅 `args_redacted`，测试含负向断言）。

## 2. 验证证据（可复跑）

```powershell
cd itsm-backend
gofmt -l service/bot/trace.go service/bot/trace_test.go handlers/ai/run_trace.go handlers/ai/run_trace_handler_test.go
go test ./handlers/ai/ ./service/bot/ ./router/ ./middleware/ -count=1   # 4 包全 ok
go run ./cmd/authz-gen                                                   # 再生预检：/api/v1/ai/runs/*/trace → ai:read

cd ../itsm-frontend
npx jest src/components/ai src/lib/ai src/lib/store src/lib/api/__tests__/ai-api.test.ts --coverage=false --watchAll=false --runInBand
# → 35 suites / 293 tests 全绿（新增：trace-model 5 例 + RunTraceView 3 例）
npx jest --testPathPattern "ai/observability" --coverage=false --watchAll=false --runInBand
# → 1 suite / 7 tests（原 5 例 + 轨迹回放 2 例）
npx tsc --noEmit ; npx eslint src/components/ai/run-trace "src/pages/*/ai/observability/index.tsx" src/lib/api/ai-api.ts
```

关键断言：
- 后端 service：链路完整性（run 字段/步骤升序/事件 seq 与 payload 解码/工具确认态 `confirmed`+verify/
  产物 kind+title）；**脱敏负向断言**（序列化结果不含原始参数 `secret-args`，仅含 `***`）；
  跨租户 → `ErrRunNotFound`（不泄漏存在性）；未命中/非法入参同口径；nil 服务 → 可区分错误（503 语义）。
- 后端 handler：全链路 200 + 六段字段齐备；未命中 404；非法 id 400；未装配 503。
- 前端：链路六段渲染 + 「已确认」/`verify:verified`/`artifact:31`/`ticket #42` 链接；空段占位；
  「复制轨迹 ID」写入剪贴板并显示已复制态（埋点先行、剪贴板失败静默降级）；
  看板输入 run_id 回放 + `?runId=` 深链自动加载 + 404 提示「轨迹不存在或跨租户不可见」。

## 3. 边界与条件项（不阻塞）

- 纯问答（无工具、无 run 落库）不在本端点范围：按 §13.4 需以 `conversation_id + message_id` 聚合，
  归后续（会话维度轨迹）；
- 轨迹检索入口目前为 `/ai/observability`（含深链）；`/ai/audit` 工具行内「查看轨迹」跳转（runId>0 时）
  与面板内诊断抽屉（O2-02）复用 `RunTraceView`，归 O2-02 落地；
- token 计量未接线（沿用 B4-02 口径：以步数/工具数/时延为代理），轨迹 DTO 已预留 budgetJson 字段；
- 真实数据量下的分页/裁剪策略（超长 events/artifacts）后续按需引入上限参数；浏览器 E2E 归联调。