-- RLS 022 rollback: drop only the P0 customer-master policies.

DO $$
BEGIN
    IF to_regclass('public.customer_tenant_links') IS NOT NULL THEN
        DROP POLICY IF EXISTS tenant_isolation_customer_tenant_links ON customer_tenant_links;
    END IF;
    IF to_regclass('public.customer_contacts') IS NOT NULL THEN
        DROP POLICY IF EXISTS tenant_isolation_customer_contacts ON customer_contacts;
    END IF;
END $$;
