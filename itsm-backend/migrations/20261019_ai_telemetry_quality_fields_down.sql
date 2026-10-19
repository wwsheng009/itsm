-- 回滚 T5-06a 质量层数据源字段（仅用于迁移回滚；正常升级不执行）。

DROP INDEX IF EXISTS ai_telemetry_events_tenant_query_hash_idx;
DROP INDEX IF EXISTS ai_telemetry_events_tenant_model_occurred_idx;

ALTER TABLE ai_telemetry_events
    DROP COLUMN IF EXISTS feedback,
    DROP COLUMN IF EXISTS sources_count,
    DROP COLUMN IF EXISTS query_hash,
    DROP COLUMN IF EXISTS model;
