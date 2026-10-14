-- =============================================================================
-- RLS Migration 012 rollback — MSP SLA 合同表策略回滚
--
-- 与 012_msp_contract_policies.sql 对齐：DROP 本迁移创建的策略并 DISABLE RLS。
-- 不改动 helper 函数与 003–011 既有策略。幂等。
--
-- 应用方式：
--   go run -tags rlsapply ./cmd/rls-apply -files 012_msp_contract_policies_rollback.sql
-- =============================================================================

DO $$
DECLARE
    t text;
BEGIN
    t := 'msp_contracts';
    IF to_regclass('public.' || quote_ident(t)) IS NULL THEN
        RAISE NOTICE 'rls 012 rollback: skip % (table not found)', t;
    ELSE
        EXECUTE format('DROP POLICY IF EXISTS %I ON %I', 'msp_contract_provider_all', t);
        EXECUTE format('DROP POLICY IF EXISTS %I ON %I', 'msp_contract_customer_read', t);
        EXECUTE format('ALTER TABLE %I DISABLE ROW LEVEL SECURITY', t);
        RAISE NOTICE 'rls 012 rollback: % disabled', t;
    END IF;

    t := 'msp_contract_sla_bindings';
    IF to_regclass('public.' || quote_ident(t)) IS NULL THEN
        RAISE NOTICE 'rls 012 rollback: skip % (table not found)', t;
    ELSE
        EXECUTE format('DROP POLICY IF EXISTS %I ON %I', 'msp_contract_binding_provider_all', t);
        EXECUTE format('DROP POLICY IF EXISTS %I ON %I', 'msp_contract_binding_customer_read', t);
        EXECUTE format('ALTER TABLE %I DISABLE ROW LEVEL SECURITY', t);
        RAISE NOTICE 'rls 012 rollback: % disabled', t;
    END IF;
END $$;
