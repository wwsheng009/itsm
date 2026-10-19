-- T5-06a 质量层数据源：为 AI 埋点事件补充模型、问题哈希、引用数与回答级反馈字段。
--
-- 口径（docs/plan/ai-assistant-dock-plan-2026-10-18.md §14.2 T5-06a）：
--   - model：本次回答实际生效的模型名（静态回退未配置时为空串，不伪造）；
--   - query_hash：问题文本的 FNV-1a 哈希，仅用于同题聚合/关联，不存原文；
--   - sources_count：stream_end 携带的引用条数；NULL = 旧客户端未上报（不计入引用覆盖率分母）；
--   - feedback：message_feedback 的回答级反馈（helpful / unhelpful）。
--
-- 幂等：ADD COLUMN IF NOT EXISTS / CREATE INDEX IF NOT EXISTS，可重复执行。

ALTER TABLE ai_telemetry_events
    ADD COLUMN IF NOT EXISTS model VARCHAR(128) NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS query_hash VARCHAR(64) NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS sources_count INTEGER NULL,
    ADD COLUMN IF NOT EXISTS feedback VARCHAR(16) NOT NULL DEFAULT '';

CREATE INDEX IF NOT EXISTS ai_telemetry_events_tenant_model_occurred_idx
    ON ai_telemetry_events (tenant_id, model, occurred_at);
CREATE INDEX IF NOT EXISTS ai_telemetry_events_tenant_query_hash_idx
    ON ai_telemetry_events (tenant_id, query_hash);
