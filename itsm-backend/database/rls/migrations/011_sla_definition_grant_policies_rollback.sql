-- =============================================================================
-- RLS Migration 011 rollback — SLA 定义下发授权通道回滚
--
-- 与 011_sla_definition_grant_policies.sql 对齐：
--   1) DROP sla_definition_grants 上的两条策略 + DISABLE RLS；
--   2) DROP sla_definitions 上的授信可见策略（仅本迁移添加的 SELECT 策略）；
--      既有 tenant_isolation_sla_definitions 与表的 ENABLE 状态保持不变。
--   不改动 helper 函数、003–010 既有策略与试点表 FORCE 状态。幂等。
--
-- 应用方式：
--   go run -tags rlsapply ./cmd/rls-apply -files 011_sla_definition_grant_policies_rollback.sql
-- =============================================================================

-- 1) sla_definition_grants：撤销两条策略并关闭 RLS
DO $$
DECLARE
    t text := 'sla_definition_grants';
BEGIN
    IF to_regclass('public.' || quote_ident(t)) IS NULL THEN
        RAISE NOTICE 'rls 011 rollback: skip % (table not found)', t;
    ELSE
        EXECUTE format('DROP POLICY IF EXISTS %I ON %I', 'sla_grant_provider_all', t);
        EXECUTE format('DROP POLICY IF EXISTS %I ON %I', 'sla_grant_customer_read', t);
        EXECUTE format('ALTER TABLE %I DISABLE ROW LEVEL SECURITY', t);
        RAISE NOTICE 'rls 011 rollback: % disabled (policies dropped)', t;
    END IF;
END $$;

-- 2) sla_definitions：撤销授信可见策略（保留既有租户隔离策略）
DO $$
DECLARE
    t text := 'sla_definitions';
BEGIN
    IF to_regclass('public.' || quote_ident(t)) IS NULL THEN
        RAISE NOTICE 'rls 011 rollback: skip % (table not found)', t;
    ELSE
        EXECUTE format('DROP POLICY IF EXISTS %I ON %I', 'sla_definition_customer_read', t);
        RAISE NOTICE 'rls 011 rollback: % customer read channel removed', t;
    END IF;
END $$;
