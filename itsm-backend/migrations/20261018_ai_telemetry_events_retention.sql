-- O0-02 条件项闭环：AI 埋点事件保留期清理的支撑索引
-- （方案 docs/plan/ai-assistant-dock-plan-2026-10-18.md §13.1.4 合规 / §13.3 数据策略）。
--
-- 背景：保留期清理按 occurred_at < cutoff 分批删除最旧行
-- （service/ai_telemetry_housekeeping.go，跨租户系统任务）。既有索引均为
-- (tenant_id, name/page_key, occurred_at) 形态，跨租户的 occurred_at 范围扫描
-- 无法命中其前缀，本索引使清理走索引范围扫描 + 有界批量，避免全表扫描。
--
-- 幂等：CREATE INDEX IF NOT EXISTS；表/RLS 定义见 20261018_ai_telemetry_events.sql。

CREATE INDEX IF NOT EXISTS ai_telemetry_events_occurred_idx
    ON ai_telemetry_events (occurred_at);
