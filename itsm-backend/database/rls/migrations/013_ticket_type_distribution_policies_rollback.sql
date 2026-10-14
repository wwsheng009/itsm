-- =============================================================================
-- RLS Migration 013 rollback — 工单类型受控下发追踪表策略回滚
--
-- 与 013_ticket_type_distribution_policies.sql 对齐：DROP 本迁移创建的策略并 DISABLE RLS。
-- 不改动 helper 函数与 003–012 既有策略。幂等。
--
-- 应用方式：
--   go run -tags rlsapply ./cmd/rls-apply -files 013_ticket_type_distribution_policies_rollback.sql
-- =============================================================================

DO $$
DECLARE
    t text := 'ticket_type_distributions';
    p text := 'tenant_isolation_ticket_type_distributions';
BEGIN
    IF to_regclass('public.' || quote_ident(t)) IS NULL THEN
        RAISE NOTICE 'rls 013 rollback: skip % (table not found)', t;
    ELSE
        EXECUTE format('DROP POLICY IF EXISTS %I ON %I', p, t);
        EXECUTE format('ALTER TABLE %I DISABLE ROW LEVEL SECURITY', t);
        RAISE NOTICE 'rls 013 rollback: % disabled', t;
    END IF;
END $$;
