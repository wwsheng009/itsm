-- =============================================================================
-- RLS Migration 017 回滚 — 文档待办状态表策略
-- 仅移除策略（表结构与数据保留；如需清表请显式 DROP TABLE）。
-- =============================================================================

DO $$
BEGIN
    IF to_regclass('public.ai_doc_todo_states') IS NOT NULL THEN
        EXECUTE 'DROP POLICY IF EXISTS tenant_isolation_ai_doc_todo_states ON ai_doc_todo_states';
        EXECUTE 'ALTER TABLE ai_doc_todo_states DISABLE ROW LEVEL SECURITY';
        RAISE NOTICE 'rls 017 rollback: ai_doc_todo_states policy dropped';
    END IF;
END $$;