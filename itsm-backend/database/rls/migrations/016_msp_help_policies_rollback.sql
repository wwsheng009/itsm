-- =============================================================================
-- RLS Migration 016 回滚 — MSP 帮助空间与分发授权表策略
-- 仅移除策略（表结构与数据保留；如需清表请显式 DROP TABLE）。
-- =============================================================================

DO $$
BEGIN
    IF to_regclass('public.msp_help_docs') IS NOT NULL THEN
        EXECUTE 'DROP POLICY IF EXISTS tenant_isolation_msp_help_docs ON msp_help_docs';
        EXECUTE 'ALTER TABLE msp_help_docs DISABLE ROW LEVEL SECURITY';
        RAISE NOTICE 'rls 016 rollback: msp_help_docs policy dropped';
    END IF;

    IF to_regclass('public.msp_help_distributions') IS NOT NULL THEN
        EXECUTE 'DROP POLICY IF EXISTS tenant_isolation_msp_help_distributions ON msp_help_distributions';
        EXECUTE 'ALTER TABLE msp_help_distributions DISABLE ROW LEVEL SECURITY';
        RAISE NOTICE 'rls 016 rollback: msp_help_distributions policy dropped';
    END IF;
END $$;