-- O0-02 观测性：AI Copilot 前端埋点事件表（方案 docs/plan/ai-assistant-dock-plan-2026-10-18.md §13.2/§13.7）。
--
-- 口径：
--   - 仅存元数据：前端 SDK（src/lib/ai/telemetry.ts）已在入口层完成脱敏（敏感字段为哈希），
--     本表不接收对话正文；payload 保留事件载荷（已脱敏）；
--   - 租户隔离走 RLS（与 alerts 同范式）：应用层 tenant_id 只作查询条件，越权由策略兜底；
--   - 聚合读路径：service/ai_assistant_telemetry.go（按 name/page_key/时延/错误率）。
--   - 保留期策略：暂由运维侧按需清理（后续 O 系列任务补定时清理）。

CREATE TABLE IF NOT EXISTS ai_telemetry_events (
    id BIGSERIAL PRIMARY KEY,
    tenant_id BIGINT NOT NULL,
    user_id BIGINT NOT NULL DEFAULT 0,
    session_id VARCHAR(64) NOT NULL DEFAULT '',
    name VARCHAR(64) NOT NULL,
    page_key VARCHAR(128) NOT NULL DEFAULT '',
    route VARCHAR(255) NOT NULL DEFAULT '',
    entrypoint VARCHAR(64) NOT NULL DEFAULT '',
    conversation_id BIGINT NULL,
    run_id VARCHAR(64) NULL,
    latency_ms BIGINT NULL,
    error_code VARCHAR(64) NOT NULL DEFAULT '',
    payload JSONB NOT NULL DEFAULT '{}'::jsonb,
    occurred_at TIMESTAMPTZ NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS ai_telemetry_events_tenant_name_occurred_idx
    ON ai_telemetry_events (tenant_id, name, occurred_at);
CREATE INDEX IF NOT EXISTS ai_telemetry_events_tenant_page_occurred_idx
    ON ai_telemetry_events (tenant_id, page_key, occurred_at);
CREATE INDEX IF NOT EXISTS ai_telemetry_events_tenant_session_idx
    ON ai_telemetry_events (tenant_id, session_id);

ALTER TABLE ai_telemetry_events ENABLE ROW LEVEL SECURITY;
DROP POLICY IF EXISTS tenant_isolation_ai_telemetry_events ON ai_telemetry_events;
CREATE POLICY tenant_isolation_ai_telemetry_events ON ai_telemetry_events
    USING (tenant_id = NULLIF(current_setting('app.current_tenant', true), '')::BIGINT)
    WITH CHECK (tenant_id = NULLIF(current_setting('app.current_tenant', true), '')::BIGINT);