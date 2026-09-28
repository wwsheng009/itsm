# B0-02 / B0-03 集成证据

> 文档类型：实施证据（任务 B0-02、B0-03）
> Status: draft
> 编制日期：2026-09-27
> 任务：B0-02（ToolInvocation 扩展与联合迁移）、B0-03（审计回填与拒绝原因回填）
> 关联文档：`docs/plan/ai-bot-capability-landing-implementation-plan-2026-09-27.md`（§4.1 B0-02/B0-03、§5.2 AB0-02/AB0-03）
> 核查方式：本机编译 + 定向/全量测试（SQLite 内存库 + mock）；Postgres 侧未跑（见 §5）

## 1. 结论

| 任务 | 依赖 | 本轮判定 | 依据 |
| --- | --- | --- | --- |
| B0-02 | BP2、BQ8（均已完成） | **`integration_verified`** | 字段随 MCP M0-03 联合迁移一次落地（无二次迁移）；实体结构 + 读写契约补齐；零值不写列（既有行为不变）；幂等唯一索引实测生效 |
| B0-03 | B0-02 | **`integration_verified`** | 会话归属注入（含 API 入口）+ 拒绝结论结构化回填 + 审计按会话回溯；RBAC 四件套与既有审计行为不变（回归通过） |

> 口径说明：`integration_verified` = 跨模块（handlers/ai ↔ ent ↔ service）真实 DB（SQLite）往返 + 契约断言通过；`flow_verified` 待 B0-07（端到端零写入流）与 B1 聊天链路。

## 2. B0-02 交付物

| 变更 | 位置 | 说明 |
| --- | --- | --- |
| 实体结构同步 | `handlers/ai/entity.go` | 补齐 12 个字段：`RunID/StepID/TargetType/TargetID/SupportRef/IdempotencyKeyHash/ExpiresAt/VerifyState/VerifyNote/AttemptCount/LastErrorCode/DryRun`（与 ent schema 同源命名，JSON 用 camelCase） |
| 读路径回填 | `handlers/ai/repository_impl.go` | `toToolInvocationDomain` 全量映射；`expires_at` 为 nillable，nil 不伪造零值 |
| 写路径回填 | 同上 | 零值/空串/nil **不写列**（保持既有调用方行为与列默认值）；`dry_run` 仅在 true 时写入 |
| 迁移 | `ent/schema/tool_invocation.go`（M0-03 已落地） | 唯一索引 `(tenant_id, idempotency_key_hash)`；查询索引 `(tenant_id, conversation_id)`；`client.Schema.Create` 统一执行（`internal/bootstrap/app.go`） |
| 契约测试 | `handlers/ai/repository_b0_fields_test.go` | ①全字段往返一致（含 `expires_at`）+ 零值不写列；②幂等唯一：同租户同 hash 冲突、跨租户不冲突、空 hash（读工具）多行不冲突 |

**外键注意（实测暴露）**：`tool_invocations.user_id` 指向 `users`，显式写入非零值必须存在真实行；测试与调用方需保证（生产链路已由登录态保证）。`CreateToolInvocation` 现有实现无条件 `SetUserID`，因此**零值 user_id 会被写成 FK 0 并失败**——本任务未改变该既有语义，仅登记为约束（见 §5 遗留）。

## 3. B0-03 交付物

| 变更 | 位置 | 说明 |
| --- | --- | --- |
| 会话归属注入 | `handlers/ai/service.go` | 新增 `ExecuteToolWithConversation(...)`；`ExecuteTool` 改为委托（**签名与行为不变**，无会话 = 0 = 不入列）；会话 ID 经包内 context 键传递到 pending 创建与审计写入点，不承载安全语义（租户/用户仍为显式参数） |
| API 入口 | `handlers/ai/handler.go` | `POST /api/v1/agent/tools/execute` 请求体新增可选 `conversationId`（增量字段，旧客户端不受影响） |
| 拒绝结论回填 | `handlers/ai/service.go`（`ApproveTool` 拒绝分支 + `backfillToolDecision`） | 拒绝时向 `inv.conversation_id` 追加 `role=assistant` 的结构化消息 `{"type":"tool_approval_decision","invocationId","tool","decision":"rejected","reason"}`；`reason` 经 `redact.ValueSummary(...,512)` 截断/脱敏；无会话归属则跳过；写入失败打点 `itsm_ai_persist_errors_total{operation="backfill_tool_decision"}` |
| run 预留 | 同上 | `run_id/step_id` 已可写可读（B0-02），注入点留待 B1-01 落表后接入同一入口 |
| 契约测试 | `handlers/ai/service_b0_conversation_test.go` | ①带会话调用 → pending 落 `conversation_id`；②既有入口零值不变；③拒绝 → 会话内出现结构化结论（含原因）；④无会话拒绝 → 不写消息 |

## 4. 测试记录（本机）

| 命令 | 结果 |
| --- | --- |
| `gofumpt -w` + `gofumpt -l ./handlers/ai` | 无输出（v0.7.0，与 CI 同版本） |
| `go test ./handlers/ai/ -run 'TestB0_03_\|TestEntRepository_(BotFields\|IdempotencyKey\|ToolMetadataSnapshot\|MCPAudit)\|TestExecuteTool_'` | **ok 4.4s**（含 RBAC/审计既有回归） |
| `go test ./handlers/ai/ ./service/ -count=1 -timeout 25m` | **exit 0 —— 2 包全 ok**：`handlers/ai` 21.6s / `service` 281.9s（受影响包全量，无回归） |

失败并已修复的过程记录（保留以便复现）：幂等唯一测试首跑报 `FOREIGN KEY constraint failed` —— `user_id` 为指向 `users` 的外键，测试未建用户行；补齐租户/用户后通过。

## 5. 遗留与未闭环

1. **`user_id=0` 的 FK 行为**：`CreateToolInvocation` 无条件 `SetUserID`，零值会触发 FK 失败（既有实现，非本任务引入）。若后续存在「系统发起」（Bot 定时/事件触发）调用，需要 B1 决定：写 NULL 还是引入系统用户。已登记，未在本任务改动。
2. **Postgres 侧**：本机无 Docker/Postgres，索引与迁移在 Postgres 的等价行为依赖 CI `mcp-postgres-migrations` 作业；本轮未实测。
3. **拒绝回填的会话消费方**：B1-04 的对话工具循环需把该结构化消息纳入模型上下文（当前只落库，聊天链路尚未有工具循环）。
4. **审计页按会话过滤**：后端已建 `(tenant_id, conversation_id)` 索引；管理/审计页的会话维度筛选属 B2-03。

## 6. 变更记录

| 日期 | 变更 | 说明 |
| --- | --- | --- |
| 2026-09-27 | 初稿 | B0-02/B0-03 交付证据：字段往返与零值契约、幂等唯一性、会话归属注入、拒绝回填；含失败修复过程与被登记的既有约束 |
