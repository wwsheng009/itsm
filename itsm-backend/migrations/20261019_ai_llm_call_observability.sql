-- 20261019 AI LLM 调用观测扩展：租户/任务/错误分类（T5-07b-lite 的可观测部分）
-- 说明：ai_llm_calls 原为平台级聚合表；新增 tenant_id 允许租户维度查询，
-- 历史行保持 NULL（不回填），新行在请求上下文有租户时写入。

BEGIN;

ALTER TABLE ai_llm_calls
    ADD COLUMN IF NOT EXISTS tenant_id BIGINT NULL,
    ADD COLUMN IF NOT EXISTS provider_source TEXT NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS task_type TEXT NOT NULL DEFAULT 'unknown',
    ADD COLUMN IF NOT EXISTS tokens_estimated BOOLEAN NOT NULL DEFAULT TRUE,
    ADD COLUMN IF NOT EXISTS error_class TEXT NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS http_status INT NULL,
    ADD COLUMN IF NOT EXISTS error_message TEXT NOT NULL DEFAULT '';

CREATE INDEX IF NOT EXISTS ai_llm_calls_tenant_created_idx
    ON ai_llm_calls (tenant_id, created_at DESC);
CREATE INDEX IF NOT EXISTS ai_llm_calls_tenant_success_created_idx
    ON ai_llm_calls (tenant_id, success, created_at DESC);
CREATE INDEX IF NOT EXISTS ai_llm_calls_tenant_error_created_idx
    ON ai_llm_calls (tenant_id, error_class, created_at DESC);
CREATE INDEX IF NOT EXISTS ai_llm_calls_tenant_provider_created_idx
    ON ai_llm_calls (tenant_id, provider_key, created_at DESC);

COMMIT;
