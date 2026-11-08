DO $$
DECLARE
    t text := 'sla_definitions';
    p text := 'sla_definition_customer_read';
BEGIN
    IF to_regclass('public.' || quote_ident(t)) IS NOT NULL
       AND to_regclass('public.sla_definition_grants') IS NOT NULL THEN
        EXECUTE format('DROP POLICY IF EXISTS %I ON %I', p, t);
        EXECUTE format(
            'CREATE POLICY %I ON %I FOR SELECT USING (EXISTS (' ||
            'SELECT 1 FROM sla_definition_grants g ' ||
            'WHERE g.sla_definition_id = %I.id ' ||
            'AND g.customer_tenant_id = get_current_tenant_id() ' ||
            'AND g.is_active))',
            p, t, t);
        RAISE NOTICE 'rls 021 rollback: % restored', p;
    END IF;
END $$;
