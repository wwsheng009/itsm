-- 20261018 llm_provider_configs 增加完整 Endpoint 开关（expand，幂等可重跑）。
-- endpoint_full_path=true 时 provider 直接使用 endpoint，不追加协议默认资源路径，
-- 用于千帆 Token Plan 等非标准/完整请求地址。

BEGIN;

ALTER TABLE llm_provider_configs
    ADD COLUMN IF NOT EXISTS endpoint_full_path BOOLEAN NOT NULL DEFAULT FALSE;

COMMIT;
