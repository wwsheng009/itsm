DO $$
DECLARE
    t text := 'sla_definition_snapshots';
    p text := 'sla_definition_snapshots_tenant_all';
BEGIN
    IF to_regclass('public.' || quote_ident(t)) IS NOT NULL THEN
        EXECUTE format('DROP POLICY IF EXISTS %I ON %I', p, t);
        EXECUTE format('ALTER TABLE %I DISABLE ROW LEVEL SECURITY', t);
        RAISE NOTICE 'rls 020 rollback: % policy dropped', t;
    END IF;
END $$;
