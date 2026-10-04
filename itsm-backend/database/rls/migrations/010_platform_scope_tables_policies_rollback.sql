-- =============================================================================
-- RLS Migration 010 回滚 — 移除批次 8（bootstrap / marketplace 安装域 2 表）策略
-- =============================================================================
DO $$
DECLARE
    t text;
BEGIN
    FOREACH t IN ARRAY ARRAY[
        'bootstrap_tokens',
        'tenant_installations'
    ]
    LOOP
        IF to_regclass('public.' || quote_ident(t)) IS NULL THEN
            CONTINUE;
        END IF;
        EXECUTE format('DROP POLICY IF EXISTS %I ON %I', 'tenant_isolation_' || t, t);
        EXECUTE format('ALTER TABLE %I NO FORCE ROW LEVEL SECURITY', t);
        EXECUTE format('ALTER TABLE %I DISABLE ROW LEVEL SECURITY', t);
        RAISE NOTICE 'rls 010 rollback: % policy dropped', t;
    END LOOP;
END $$;
