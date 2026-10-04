-- =============================================================================
-- RLS Migration 008 回滚 — 移除批次 6（自动化/机器人与运维命令域 10 表）策略
-- 注意：回滚只影响本批次表；003–007/009 的策略不受影响。
-- =============================================================================
DO $$
DECLARE
    t text;
BEGIN
    FOREACH t IN ARRAY ARRAY[
        'bot_runs', 'bot_steps', 'bot_events', 'bot_artifacts',
        'bot_templates', 'bot_tool_grants',
        'ai_analysis_results', 'ai_feedbacks', 'llm_user_preferences',
        'operational_commands'
    ]
    LOOP
        IF to_regclass('public.' || quote_ident(t)) IS NULL THEN
            CONTINUE;
        END IF;
        EXECUTE format('DROP POLICY IF EXISTS %I ON %I', 'tenant_isolation_' || t, t);
        EXECUTE format('ALTER TABLE %I NO FORCE ROW LEVEL SECURITY', t);
        EXECUTE format('ALTER TABLE %I DISABLE ROW LEVEL SECURITY', t);
        RAISE NOTICE 'rls 008 rollback: % policy dropped', t;
    END LOOP;
END $$;
