-- RLS 024 rollback: drop only the P2 customer-master policy.

DO $$
BEGIN
    IF to_regclass('public.customer_external_ids') IS NOT NULL THEN
        DROP POLICY IF EXISTS tenant_isolation_customer_external_ids ON customer_external_ids;
    END IF;
END $$;
