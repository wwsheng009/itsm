# O0-02 条件项闭环：AI 埋点事件保留期清理（2026-10-18）

- 任务：方案 `docs/plan/ai-assistant-dock-plan-2026-10-18.md` §13.1.4 合规（租户隔离 + 脱敏 + **保留期**）
  与 §13.3 数据策略（`ai_telemetry_event` 表「租户前置索引 + **保留期策略**」）的落地；
  同时闭环 O0-03 进度块登记的条件项「保留期清理待补」（表迁移头注释即写着
  「暂由运维侧按需清理（后续 O 系列任务补定时清理）」）。
- 状态：`integration_verified`（单测 + 编译 + 既有包回归全绿；真实 PG 删除语义回归待环境）。

## 1. 交付物

| 类型 | 路径 | 说明 |
| --- | --- | --- |
| 服务 | `itsm-backend/service/ai_telemetry_housekeeping.go`（新增） | 保留期清理执行器：批量删除 + 循环扫空 + 定时循环 |
| 单测 | `itsm-backend/service/ai_telemetry_housekeeping_test.go`（新增） | 10 例（sqlmock，不依赖真实数据库） |
| 配置 | `itsm-backend/config/config.go` | 新增 `ai_telemetry` 块：`retention_days` / `cleanup_interval_minutes` / `cleanup_batch_size`（<=0 回落缺省） |
| 接线 | `itsm-backend/internal/bootstrap/app.go` | `startBackgroundTasks` 内启动（worker/all 模式起停，rawDB 管理池） |
| 迁移 | `itsm-backend/migrations/20261018_ai_telemetry_events_retention.sql`（新增） | 幂等索引 `ai_telemetry_events_occurred_idx (occurred_at)` |
| 回归 | `itsm-backend/migration/ai_telemetry_retention_discovery_test.go`（新增） | 自发现器必须加载该迁移且内容含幂等索引语句 |

## 2. 契约（实现口径）

- **删除条件**：`occurred_at < now - retention_days`（严格小于；等于 cutoff 的行不删，边界归下一轮）。
  本表只存行为/性能元数据（前端已脱敏），删除不影响对话、run 档案、轨迹回放。
- **批量与扫空**：每批只删最旧 `batch_size` 行（子查询 `ORDER BY occurred_at LIMIT $2`），
  循环直至单批删除数 `< batch_size` 或达到单轮批次硬上限（默认 200 批 × 5000 行 = 100 万行/轮，
  剩余留待下一轮）——避免单条大 DELETE 长锁，进度不丢。
- **幂等**：重复执行只作用于同一 criteria；无匹配/空表 → `0 行 0 错误`。
- **失败语义**：中途失败返回「已删行数 + 错误」（不吞部分进度）；`Start` 轮询失败仅告警，
  不阻断进程与后续轮次。
- **跨租户**：`tenantctx.SystemContext`（先例 `pat_housekeeping`；管理池 + BYPASSRLS 角色语义）。
- **缺省**：保留 90 天（与聚合回溯上限 `assistantTelemetryMaxLookbackDays=90` 一致，
  清理不会削掉看板窗口内数据）、间隔 24h、单批 5000 行。
- **索引**：既有索引均为 `(tenant_id, ...)` 前缀，跨租户的 `occurred_at` 范围扫描无法命中；
  新增 `(occurred_at)` 使清理为索引范围扫描 + 有界批量。

## 3. 复跑命令与实测结果

```powershell
# 后端 itsm-backend
gofmt -l -w service/ai_telemetry_housekeeping.go service/ai_telemetry_housekeeping_test.go config/config.go internal/bootstrap/app.go migration/ai_telemetry_retention_discovery_test.go
go build ./config/ ./service/ ./internal/bootstrap/     # exit 0
go test ./service/ -run 'TestAITelemetryHousekeeping' -count=1 -v   # 10/10 PASS（1.6s）
go test ./service/ ./config/ ./migration/ -count=1      # ok service 59.5s / config 1.5s / migration 1.0s
go test ./internal/bootstrap/ -count=1                  # ok 1.6s
```

单测清单（均为 `--- PASS`）：

1. `Defaults` —— 缺省回落 90 天 / 24h / 5000 行，且自定义值透传；
2. `NilDB` —— 未装配 db 返回 `ErrAssistantTelemetryUnavailable`；nil 接收者 `Start` 不 panic；
3. `RunOnceBatchesUntilDrained` —— 5000+300 两批 = 5300 行，第三批不发起；cutoff 容差断言；
4. `RunOnceEmptyIsIdempotent` —— 连续两轮 0 行 0 错误；
5. `RunOnceCustomRetentionCutoff` —— 保留期 7 天 → cutoff ≈ now-7d；
6. `RunOnceStopsAtBatchCap` —— 批次硬上限 3 → 恰 3 次执行、总 6 行、无第 4 次；
7. `RunOnceErrorStopsLoop` —— 首批失败：0 行 + 错误（含 `purge assistant telemetry events`）；
8. `RunOncePartialProgressOnMidwayError` —— 首批 5000 成功后失败：返回 5000 + 错误；
9. `RunOnceRowsAffectedUnavailable` —— 驱动不支持 RowsAffected：保守终止并报错；
10. `StartRunsImmediatelyAndStopsOnCancel` —— 启动即跑一轮；ctx 取消后无额外执行。

删除语句契约由 sqlmock 精确模式锁定（`regexp.QuoteMeta(assistantTelemetryPurgeStmt)`）：
必须包含 `DELETE FROM ai_telemetry_events`、`WHERE occurred_at < $1`、`ORDER BY occurred_at`、`LIMIT $2`。

迁移回归（`migration` 包）：`TestFilesystemMigrations_AITelemetryRetentionIndex`
断言自发现器加载版本 `20261018_ai_telemetry_events_retention`，且 SQL 含
`ai_telemetry_events_occurred_idx` 与 `IF NOT EXISTS`（幂等）。

## 4. 未覆盖 / 诚实边界

- **真实 PG 行为未在本轮验证**（本机 Docker 未运行）：RLS 管理池角色实际绕过、
  部分索引命中计划、`DELETE ... WHERE id IN (SELECT ... LIMIT)` 在大表上的执行计划，
  均待真实 PG 冒烟；已在方案文档保留条件项。
- 未做时间分区（方案 §13.3 提到「时间分区」是目标形态之一）：本批以「保留期清理 + 支撑索引」
  闭环保留期策略；分区改造涉及存量表在线迁移，不在本轮范围。
- 清理仅针对 `ai_telemetry_events`；`ai_llm_calls` / `ai_feedbacks` 的保留期不在本条件项口径内。
- 未新增 HTTP 端点（保留期是后台治理策略，无用户交互面）；观测通过启动日志
  （`AI telemetry housekeeping started/purged events`）与 `tenantctx` bypass 审计日志。