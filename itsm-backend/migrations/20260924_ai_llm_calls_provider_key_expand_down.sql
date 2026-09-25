-- 20260924 回滚 ai_llm_calls.provider_key（多 LLM Provider 支持与可切换方案 BE-1，§3.1.3）。
-- 幂等：列不存在时安全跳过。

BEGIN;

ALTER TABLE ai_llm_calls DROP COLUMN IF EXISTS provider_key;

COMMIT;
