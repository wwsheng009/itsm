-- RLS 025 rollback: drop only C1 contract policies.

DO $$
DECLARE
    t text;
BEGIN
    FOREACH t IN ARRAY ARRAY[
        'contract_service_items',
        'contract_amendments',
        'contract_usage_ledger',
        'contract_project_links'
    ]
    LOOP
        IF to_regclass('public.' || quote_ident(t)) IS NOT NULL THEN
            EXECUTE format('DROP POLICY IF EXISTS %I ON %I', 'tenant_isolation_' || t, t);
        END IF;
    END LOOP;
END $$;
