-- 20261019 回滚 AI LLM 调用观测扩展（T5-07b-lite 的可观测部分）。

BEGIN;

DROP INDEX IF EXISTS ai_llm_calls_tenant_provider_created_idx;
DROP INDEX IF EXISTS ai_llm_calls_tenant_error_created_idx;
DROP INDEX IF EXISTS ai_llm_calls_tenant_success_created_idx;
DROP INDEX IF EXISTS ai_llm_calls_tenant_created_idx;

ALTER TABLE ai_llm_calls
    DROP COLUMN IF EXISTS error_message,
    DROP COLUMN IF EXISTS http_status,
    DROP COLUMN IF EXISTS error_class,
    DROP COLUMN IF EXISTS tokens_estimated,
    DROP COLUMN IF EXISTS task_type,
    DROP COLUMN IF EXISTS provider_source,
    DROP COLUMN IF EXISTS tenant_id;

COMMIT;
