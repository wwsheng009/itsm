-- 20261018 回滚 endpoint_full_path 开关。

BEGIN;

ALTER TABLE llm_provider_configs
    DROP COLUMN IF EXISTS endpoint_full_path;

COMMIT;
