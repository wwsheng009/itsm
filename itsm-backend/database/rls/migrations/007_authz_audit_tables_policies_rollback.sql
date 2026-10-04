-- =============================================================================
-- RLS Migration 007 回滚 — 移除批次 5（鉴权/菜单/配置/审计域 6 表）策略并关闭 RLS
-- 注意：回滚只影响本批次表；003–006/009 的策略不受影响。
-- =============================================================================
DO $$
DECLARE
    t text;
BEGIN
    FOREACH t IN ARRAY ARRAY[
        'role_permissions', 'permissions', 'menus', 'system_configs',
        'audit_logs', 'endpoint_acls'
    ]
    LOOP
        IF to_regclass('public.' || quote_ident(t)) IS NULL THEN
            CONTINUE;
        END IF;
        EXECUTE format('DROP POLICY IF EXISTS %I ON %I', 'tenant_isolation_' || t, t);
        EXECUTE format('ALTER TABLE %I NO FORCE ROW LEVEL SECURITY', t);
        EXECUTE format('ALTER TABLE %I DISABLE ROW LEVEL SECURITY', t);
        RAISE NOTICE 'rls 007 rollback: % policy dropped', t;
    END LOOP;
END $$;
