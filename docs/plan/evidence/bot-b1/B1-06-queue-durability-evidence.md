# B1-06 实施证据（队列持久化与恢复）

> 文档类型：实施证据（任务 B1-06）
> Status: draft
> 编制日期：2026-09-27
> 任务：B1-06（队列持久化与恢复；依赖 B1-05、BQ3）
> 关联文档：`docs/plan/ai-bot-capability-landing-implementation-plan-2026-09-27.md`（§4.2 B1-06）、`docs/plan/itsm-mcp-external-tool-integration-implementation-plan-2026-09-27.md`（M1-02 共用队列）
> 核查方式：真实 ent/SQLite + 真实队列 worker（不 mock 消费路径）；条件状态迁移的并发/重复恢复断言

## 1. 结论

| 项 | 判定 | 说明 |
| --- | --- | --- |
| B1-06 | **`integration_verified`** | 不做新表：以 `tool_invocations`（`approval_state=approved ∧ status=pending`）为持久队列；启动扫描 `RecoverPending` 恢复未执行单；消费端 `claimForExecution` 条件状态迁移（pending→running + `attempt_count+1`）保证**重复恢复/并发消费只执行一次**；失败即终态并落 `last_error_code`；执行后按 `verify_state`/`verify_note` 回读校验 |
| 满队列行为 | ✅（M1-02 已落地并在本任务回归） | `Enqueue` 满队列返回 `ErrToolQueueFull`；恢复扫描容量不足时返回 `ErrToolQueueRecoveryOverflow` 并保留 `Skipped` 计数（下轮再扫，不丢单） |
| 遗留 | 3 项 | ①重试策略：写工具**恒不自动重试**（单次执行，防重复副作用；读工具重试在 provider 层 M1-08 策略内）；②跨进程多实例消费未引入分布式租约（靠 DB 条件更新天然互斥，足够单实例多 worker）；③verify 覆盖仅工单类（其它工具落 `skipped`，接口已留好） |

## 2. 交付物

| # | 文件 | 说明 |
| --- | --- | --- |
| 1 | `itsm-backend/service/tool_queue_durable.go`（新建） | `RecoverPending`（扫描 approved+pending → 重新入队，返回 Found/Enqueued/Skipped）、`claimForExecution`（条件状态迁移抢占 + 尝试次数累加） |
| 2 | `itsm-backend/service/tool_verify.go`（新建） | `ToolVerifier` 接口 + `TicketWriteVerifier`（`update_ticket` 回读比对状态/处理人；`create_ticket` 校验结果含 ID）+ `VerifyState*` 常量 |
| 3 | `itsm-backend/service/tool_queue.go`（修改） | worker 入口先 `claimForExecution`（抢占失败即跳过）；`finalize` 失败路径补 `last_error_code`、成功路径写 `verify_state`/`verify_note`；新增 `SetVerifier` 与 `verifyResult`（以落库参数快照为准） |
| 4 | `itsm-backend/internal/bootstrap/app.go`（修改） | 注入 `TicketWriteVerifier`；启动时执行 `RecoverPending(200)` 并记录日志（容量不足仅告警，不阻断启动） |
| 5 | `itsm-backend/tests/botintegration/b1_queue_recovery_test.go`（新建） | 5 个用例：恢复执行 / 重复恢复不双执行 / 失败终态 + 错误码 / verify 两态 / 待审批单不受影响 |
| 6 | `itsm-backend/tests/botintegration/b0_flow_test.go`（修改） | 桩 provider 增加可注入执行错误（`setExecErr`，并发安全） |

## 3. 恢复与抢占时序

```text
启动：RecoverPending(200)
  查 approval_state=approved ∧ status=pending（按 id 升序，跨租户运维视角）
  → 逐条 Enqueue（满则停止并返回 Skipped，下轮再扫）

消费（每条 job）：
  claimForExecution: UPDATE ... WHERE id=? AND tenant=? AND approval_state='approved' AND status='pending'
                     SET status='running', attempt_count=attempt_count+1
  affected=0 → 已被他人抢占/状态已变 → 直接跳过（不执行）
  affected=1 → 执行 → finalize（done/failed + duration/error_code/verify_*）
```

**为什么不新增队列表**：`tool_invocations` 已经是「已批准写工具」的持久事实，另立队列会引入双写一致性问题；用状态机 + 条件更新即可获得同样的持久性与幂等性，且与 MCP M1-02 共用同一实现（方案 S6 要求「禁止两套队列」）。

## 4. 运行记录（本机，pwsh）

```text
go test ./tests/botintegration/ -run 'TestB1QueueRecovery|TestB1QueueVerifier' -v
→ PASS ×5（恢复执行 / 重复恢复不双执行 / 失败终态 / verify 两态 / 待审批不受影响）
go build ./... → exit 0
```

**未执行的验证（明确登记）**：真实进程 `kill -9 → 重启` 的端到端演练（需可重启的进程与持久 DB 环境）
未在本机执行；本任务改为在测试内**直接构造崩溃现场**（落库 approved+pending 不经过队列）证明恢复语义等价，进程级演练归 CI/E2E（M2-05 类）。

## 5. 与既有契约的关系

| 契约点 | 变更 | 影响面 |
| --- | --- | --- |
| worker 消费前置 | 新增条件抢占（原先直接执行） | 未变更状态机的记录（非 approved+pending）不再被执行 —— 这正是恢复语义要求的 fail-closed |
| `verify_state`/`verify_note` | 成功终态由空串变为 `skipped`（无校验器）或 `verified`/`failed`（工单类） | 新增列语义；审批/审计页未消费该字段，前端零改动 |
| `last_error_code` | 失败终态补写（与 `error_code` 同值） | 供队列恢复/告警按列筛选 |
| 启动路径 | 新增恢复扫描（O(待执行单数)，启动时一次） | 关闭态下通常为 0 条；扫描失败仅告警不阻断启动 |

## 6. 遗留与归属

| # | 项 | 归属 |
| --- | --- | --- |
| 1 | 进程级重启演练（kill → 重启 → 继续执行） | CI/E2E（M2-05 类） |
| 2 | verify 覆盖扩展（CMDB 关系、工单类型等） | 后续按需注册 `ToolVerifier`（接口已就绪） |
| 3 | 恢复指标（恢复条数/失败数） | 与 M2-06 指标面统一 |
| 4 | 多实例部署的消费语义（当前依赖 DB 条件更新，天然互斥，无租约续期） | 多实例上线前评估 |

## 7. 变更记录

| 日期 | 变更 | 说明 |
| --- | --- | --- |
| 2026-09-27 | 初稿 | B1-06 交付：持久队列（复用 tool_invocations）+ 启动恢复 + 条件抢占 + 失败终态留痕 + 工单类回读校验；5 例全绿，判定 `integration_verified` |
