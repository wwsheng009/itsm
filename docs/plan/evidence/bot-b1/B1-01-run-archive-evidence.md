# B1-01 实施证据（run/step/event 三表与 run_id 贯通）

> 文档类型：实施证据（任务 B1-01）
> Status: draft
> 编制日期：2026-09-27
> 任务：B1-01（run/step/event 三表与 `run_id` 贯通）
> 关联文档：`docs/plan/ai-bot-capability-landing-implementation-plan-2026-09-27.md`（§4.2 B1-01、§5.2 AB1-01、§5.3 BT-01/BT-02）、阶段一报告 §5.3(b)/§7-4
> 核查方式：本机真实 ent + SQLite（迁移/约束/租户隔离）+ 真实 `/api/v1/ai/chat/stream` HTTP 链路（运行档案落库与关闭态）

## 1. 结论

| 项 | 判定 | 说明 |
| --- | --- | --- |
| B1-01 | **`integration_verified`** | 三表落表 + 唯一约束 + 租户隔离 + run_id 贯通（读/写两条调用路径）+ 一次真实对话产生运行档案（run/step/events）全部有可复跑断言；关闭态（未注入）零写入已验证 |
| 遗留（2 项，不阻断本判定） | — | ①**事件名仍为草案**：`run_started`/`run_finished` 取自方案 §5.4 v2 清单，**BP3 单一事件注册表评审**后冻结（B1-03 落地）；②**保留策略（BQ4）**未实现归档任务（events 90 天 / runs+steps 180 天），归运维与 B4；本任务只落表结构 |

## 2. 交付物

| # | 文件 | 说明 |
| --- | --- | --- |
| 1 | `itsm-backend/ent/schema/bot_run.go`（新建） | `bot_runs`：`tenant_id`、`conversation_id`（可空）、`bot_id`（预留）、`entrypoint`、`status`、`model`、`budget_json`、`error_code`、`started_at`/`finished_at`、时间戳；索引首列含租户（`tenant_id+started_at` / `tenant_id+conversation_id` / `tenant_id+status`） |
| 2 | `itsm-backend/ent/schema/bot_step.go`（新建） | `bot_steps`：`tenant_id`、`run_id`（外键）、`step_index`、`type=llm|tool|confirm`、`payload_ref`、`duration_ms`、`error_code`；**唯一** `(run_id, step_index)` |
| 3 | `itsm-backend/ent/schema/bot_event.go`（新建） | `bot_events`：`tenant_id`、`run_id`、`seq`、`type`、`payload_json`；**唯一** `(run_id, seq)`（先落库后广播的审计同源基础） |
| 4 | `itsm-backend/ent/**`（重新生成） | `go generate ./ent`（`entgo.io/ent/cmd/ent generate ./schema`），新增 `botrun`/`botstep`/`botevent` 包与 client/mutation/migrate 刷新 |
| 5 | `itsm-backend/service/bot/run.go`（新建） | `RunStore`：`StartRun`（status=running）/`AppendStep`/`AppendEvent`（**事务内取号 max(seq)+1，唯一冲突重试一次**）/`FinishRun`（置 status+error_code+finished_at，租户维度 Where）/`ListRunSteps`/`ListRunEvents` |
| 6 | `itsm-backend/service/bot/context.go`（新建） | `WithRunID`/`RunIDFromContext`：运行 ID 的调用链内传递（不承载鉴权语义；无值 = 既有行为） |
| 7 | `itsm-backend/handlers/ai/service.go`（修改） | ①`botRuns` 字段 + `SetBotRunStore`（未注入时零行为变化）；②`chatStream` 拆为**包装层 + `chatStreamInner`**：起运行 → 注入 run_id → 收口（记 `llm` 步骤 + `run_finished` 事件 + 置状态），收口写入用 `context.WithoutCancel` 保证客户端断开也留完成态；所有运行态写入失败只告警、**绝不影响聊天主链路**；③`recordToolAudit` 与写工具 pending 落库两处写入 `RunID: bot.RunIDFromContext(ctx)` |
| 8 | `itsm-backend/internal/bootstrap/app.go`（修改） | `bot.enabled=true` 时注入 `botService.NewRunStore(client)`；默认 false → 不注入、零额外写入 |
| 9 | `itsm-backend/ent/schema/bot_migration_test.go`（新建） | 迁移：空库建表 + 列清单 + 迁移幂等；`(run_id, seq|step_index)` 唯一冲突；租户谓词隔离；旧行 `tool_invocations.run_id` 为 NULL 可读且可回填关联 |
| 10 | `itsm-backend/tests/botintegration/b1_run_test.go`（新建） | `RunStore` 生命周期与隔离；**run_id 贯通**：运行上下文内的读工具审计与写工具待审批记录都带 `run_id`，运行外为 0 |
| 11 | `itsm-backend/tests/botintegration/b1_chat_run_test.go`（新建） | 真实 `/api/v1/ai/chat/stream`：一次对话 → 1 run（已收口）+ ≥1 llm step + ≥2 events（seq 从 0 连续）；二次对话 → 第二条运行；未注入 RunStore → 运行表零写入 |

## 3. 运行记录（本机，pwsh）

```text
# 迁移与约束
go test ./ent/schema/ -run 'TestBotSchema' -count=1 -v
→ PASS ×3（建表/隔离/旧行兼容） ok itsm-backend/ent/schema 2.598s

# 运行态与贯通
go test ./tests/botintegration/ -count=1 -run 'TestB1' -v -timeout 15m
→ PASS TestB1RunStoreLifecycleAndIsolation / TestB1RunIDPropagationToToolInvocations
→ PASS TestB1ChatStreamWritesRunArchive / TestB1RunStoreNilSafe

# 回归（本轮受影响包）
go test ./tests/botintegration/ ./handlers/ai/ ./service/bot/ ./ent/schema/ -count=1 -timeout 25m
→ ok tests/botintegration 7.258s | ok handlers/ai 12.668s | ok service/bot 0.373s | ok ent/schema 9.416s   （exit 0）
go vet ./internal/bootstrap/ → exit 0；gofumpt 无输出
```

## 4. 设计口径（供 B1-02/B1-03 复用）

| 项 | 口径 |
| --- | --- |
| 事件序号 | 由 `RunStore.AppendEvent` 在读取 `max(seq)` 后写入；并发撞号 → 重新取号重试一次，仍冲突则报错（**不**静默丢事件）。B1-02 若引入单写者，可直接复用该语义 |
| 步骤序号 | 由调用方给出；唯一约束兜底（重复即错误，禁止 UPDATE 既有步骤行） |
| 运行收口 | `status ∈ {running, completed, failed}`（`cancelled` 预留）；错误码 `cancelled`/`timeout`/`chat_error`（`botRunErrorCode`） |
| 关闭态 | 未注入 `RunStore`（`bot.enabled=false`）时聊天链路零额外写入；已由 `TestB1RunStoreNilSafe` 锁定 |
| 与 B0-03 口径一致 | 会话归属仍走 `conversationIDFrom(ctx)`；运行归属走 `bot.RunIDFromContext(ctx)`；两者都只在 >0 时入列 |

## 5. 未闭环与后续归属

| # | 项 | 归属 |
| --- | --- | --- |
| 1 | 事件名冻结与兼容策略（含 MCP 工具事件同表） | **BP3 评审 → B1-03**（与 MCP M1-03 同一注册表、同一 PR 节奏） |
| 2 | 事件/运行数据保留与归档任务（BQ4） | 运维 + B4（本任务只落表结构，不做清理任务） |
| 3 | 步骤粒度：当前仅 1 条 `llm` 步骤 | **B1-02**（RunManager 按工具/确认落细粒度步骤 + 预算护栏） |
| 4 | 真实对话中的工具调用 run_id 端到端（模型实际发起工具调用） | B1-02 集成用例覆盖（本轮以真实 `ExecuteTool` 路径 + 运行上下文断言替代，未起真实模型） |

## 6. 变更记录

| 日期 | 变更 | 说明 |
| --- | --- | --- |
| 2026-09-27 | 初稿 | B1-01 交付：三表 + `RunStore` + run_id 贯通 + 聊天档案；判定 `integration_verified`（条件：事件名待 BP3 冻结） |
