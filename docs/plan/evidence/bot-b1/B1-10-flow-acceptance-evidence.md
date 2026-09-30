# B1-10 证据（B1 流程验收）

> 验收项：AB1-10（对应任务 B1-10，里程碑级）｜DoD：`flow_verified`
> 实际状态：**`flow_verified`（条件达标）**——主链路「对话发起 → 确认单 → 审批通过 → 队列执行 → 回读校验 → 运行档案收口」在真实 ent/SQLite + 真实 ToolQueue + 真实 ai.Service 上闭环；SSE 侧由 B1-03 的真实帧解析用例覆盖；浏览器级 E2E/截图归 BT-09（B4-01 完整化）与 CI
> 执行人 / 环境：AI 辅助执行；Windows + pwsh，go1.25.13，Node/jest/tsc；基线分支 `feat/bot-mcp-integration`
> 时间：2026-09-27

## 1. 验收范围与证据映射

| 验收内容（§4.2 B1-10） | 执行方式 | 证据 |
| --- | --- | --- |
| 对话 → 确认 → 执行 → 回读（含 run 贯通与脱敏） | 真实 ent/SQLite + 真实队列 + 真实 Service：`StartRun → ExecuteToolWithConversation（写工具待审批）→ AppendStep/Event → ApproveTool → 队列消费 → SetVerifier 回读 → FinishRun` | `tests/botintegration/b1_acceptance_test.go:76`（`TestB1FlowAcceptance_ConfirmExecuteVerifyWithRunArchive`） |
| 运行档案收口与 run-summary | 同一用例第 4 步：`ListRunSteps/ListRunEvents + ToolInvocation count` 装配 `runSummary`（模板见 §3） | 同上断言：`runId/status/entrypoint/conversationId/stepCount/eventCount/toolInvocations/lastEventType` |
| 重启恢复（带 run 上下文） | 「approved+pending 带 run_id」落库 → **新队列实例**（模拟进程重启）`RecoverPending` → 恰好一次执行 | `b1_acceptance_test.go:139`（`TestB1FlowAcceptance_QueueRestartKeepsRunLink`；含重复恢复不双执行断言） |
| SSE v2 事件名/字段（抓包口径） | 真实 HTTP `/ai/chat/stream` 响应体逐帧解析（`parseSSEFrames`） | `b1_chat_run_test.go:263`（预算中止端到端：仅 1 次真实执行 + `error{errorCode=budget_exceeded}` + 无 `done`）；`b1_chat_run_test.go:152`（RunManager 事件序列与 DB 断言一致） |
| 状态机（过期/幂等/冲突/乐观锁） | 状态矩阵 + 真库 | `b1_confirmation_test.go`（4 用例） |
| 队列持久化（尝试次数/错误码/回读两态） | 集成 | `b1_queue_recovery_test.go`（5 用例） |
| 前端：抽屉/证据面板/状态条/审批页 | jest 组件与页面套件 | `confirmation-drawer` 10、`evidence-panel` 13、`tool-approval-card` 9、审批页 5 + B1-09 4、审计页 2、时间线 9 |
| 类型与构建 | tsc | `npx tsc --noEmit` exit 0 |

### AB1-01～AB1-10 判定汇总

| 验收项 | 判定 | 关键证据 |
| --- | --- | --- |
| AB1-01 run_id 贯通 | ✅ `integration_verified` | `b1_run_test.go:112` + 本文件 `b1_acceptance_test.go:76`（invocation.RunID 断言） |
| AB1-02 预算中止 + 事件先落库 | ✅ `integration_verified` | `b1_chat_run_test.go:263/152` |
| AB1-03 SSE 契约（帧解析） | ✅ `integration_verified`（HTTP 帧级；浏览器抓包归 E2E） | `b1_chat_run_test.go` 的 `parseSSEFrames` 断言 |
| AB1-04 前端 v2 解析 | ✅ `unit_verified` | `B1-04-sse-v2-frontend-evidence.md`（25 例回归） |
| AB1-05 五态/过期/回放/冲突 | ✅ `integration_verified` | `B1-05-confirmation-state-evidence.md` |
| AB1-06 恢复/attempt/verify_state | ✅ `integration_verified` | `B1-06-queue-durability-evidence.md` + 本文件重启用例 |
| AB1-07 抽屉四态/倒计时/原因必填 | ✅ `unit_verified`（截图待补） | `B1-07-confirmation-drawer-evidence.md` |
| AB1-08 证据面板/状态条 | ✅ `unit_verified` | `B1-08-evidence-panel-evidence.md`（缺口 G-B1-08-1/2 登记） |
| AB1-09 审批页 risk/目标/预览/倒计时 | ✅ `unit_verified` | `B1-09-approval-page-evidence.md` |
| AB1-10 全链路 E2E + run-summary | ✅ `flow_verified`（条件） | 本文件 §2 |

## 2. 执行记录

| # | 命令 | 结果 |
| --- | --- | --- |
| 1 | `go test ./tests/botintegration/ -count=1 -timeout 10m` | **ok 7.7s**（新增 2 用例 + B1-01/05/06 既有 14 用例） |
| 2 | `go test ./tests/botintegration/ ./handlers/ai/ ./service/bot/ ./service/ -count=1` | botintegration **ok 26.0s**、handlers/ai **ok 23.1s**、service/bot **ok 8.1s**；service 包 **FAIL**：`TestBiz_ListProcessInstancesByTenant`（`UNIQUE constraint failed: process_tasks.task_id`）——**既有抖动**（B1-03 已登记，与本线改动无交集；单跑通过） |
| 3 | `npx jest --testPathPattern "(ai/approval\|components/ai)" --coverage=false` | **7 suites / 56 tests 全 PASS** |
| 4 | `npx tsc --noEmit` | exit 0 |

## 3. run-summary 模板（定稿）

```json
{
  "runId": 12,
  "status": "done",
  "entrypoint": "chat",
  "conversationId": 3,
  "stepCount": 1,
  "eventCount": 2,
  "toolInvocations": 1,
  "lastEventType": "tool_call_finished"
}
```

- **来源**：`bot_runs` + `bot_steps`（按 stepIndex 排序）+ `bot_events`（按 seq 排序）+ `tool_invocations`（run_id 关联计数）；无新增存储、无新增接口（运维/审计按需查询组装）。
- **字段语义**：`status` 取 run 终态（`done/failed/cancelled`，收口前为 `running`）；`entrypoint` 标识来源入口（chat / 未来 workflow）；`lastEventType` 便于快速定位最后一次状态迁移。
- **扩展位（未启用）**：预算消耗与模型实例可在此追加（事件载荷不含预算计数，见 G-B1-08-2）。

## 4. 遗留与条件

| # | 项 | 处置 |
| --- | --- | --- |
| 1 | 浏览器级 E2E（对话内确认抽屉点击、审批页倒计时、证据面板真实渲染）与截图 | 归 **BT-09 / B4-01**（Playwright 完整化）；本判定以「服务端全链路 + 组件交互用例 + 类型检查」为 `flow_verified` 判据（与 M1-10 同口径） |
| 2 | `cancelled` 态触发点（B1-05 同源） | 产品语义未定；状态机已支持，新增触发源时补用例 |
| 3 | G-B1-08-1/2（读路径目标字段、预算计数） | 跨线缺口登记，B3 场景页启用时闭环 |
| 4 | `service` 包既有抖动（process_tasks 唯一约束） | 与本线无关；建议单开任务排查（已在 B1-03 证据登记） |

## 5. 变更记录

| 日期 | 变更 | 说明 |
| --- | --- | --- |
| 2026-09-27 | 初稿 | B1-10 交付：全链路验收用例 2 例（确认→执行→回读→run-summary；重启恢复保 run 关联）+ AB1-01～10 逐项判定 + run-summary 模板定稿；判定 **B1 = `flow_verified`（条件达标）** |
