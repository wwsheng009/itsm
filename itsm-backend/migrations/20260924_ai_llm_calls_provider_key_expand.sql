-- 20260924 ai_llm_calls 增加 provider_key（多 LLM Provider 支持与可切换方案 BE-1，§3.1.3，幂等可重跑）。
-- 该表为平台级 LLM 调用观测（无租户维度，见 internal/schema/tenant_guard.go 豁免条目），
-- provider_key 仅用于区分同协议多实例，不引入租户过滤。
-- 依赖 ai_llm_calls 已由早期迁移（020_add_ai_vector_observability_storage）创建。

BEGIN;

ALTER TABLE ai_llm_calls ADD COLUMN IF NOT EXISTS provider_key TEXT NULL;

COMMIT;
