-- RLS 014 回滚：删除 personal_access_tokens 的租户隔离策略（表与数据保留）。
DO $$
DECLARE
    t text := 'personal_access_tokens';
    p text := 'tenant_isolation_personal_access_tokens';
BEGIN
    IF to_regclass('public.' || quote_ident(t)) IS NULL THEN
        RAISE NOTICE 'rls 014 rollback: skip % (table not found)', t;
    ELSE
        EXECUTE format('DROP POLICY IF EXISTS %I ON %I', p, t);
        EXECUTE format('ALTER TABLE %I NO FORCE ROW LEVEL SECURITY', t);
        EXECUTE format('ALTER TABLE %I DISABLE ROW LEVEL SECURITY', t);
        RAISE NOTICE 'rls 014 rollback: % policy dropped, RLS disabled', t;
    END IF;
END $$;
