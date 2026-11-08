-- RLS 023 rollback: drop only the P1 customer-master policies.

DO $$
BEGIN
    IF to_regclass('public.project_customer_links') IS NOT NULL THEN
        DROP POLICY IF EXISTS tenant_isolation_project_customer_links ON project_customer_links;
    END IF;
    IF to_regclass('public.customer_relationships') IS NOT NULL THEN
        DROP POLICY IF EXISTS tenant_isolation_customer_relationships ON customer_relationships;
    END IF;
END $$;
