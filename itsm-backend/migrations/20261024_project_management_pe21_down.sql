-- =============================================================================
-- 项目管理 P2 风险/问题台账（PE-21）回滚：仅删除本迁移补齐的索引（表由 ent 管理）。
-- =============================================================================

DO $$
BEGIN
    IF to_regclass('public.project_risks') IS NOT NULL THEN
        DROP INDEX IF EXISTS idx_project_risks_tenant_project_status;
        DROP INDEX IF EXISTS idx_project_risks_tenant_project_level;
        RAISE NOTICE 'project-pe21 20261024 rollback: project_risks indexes dropped';
    END IF;
END $$;
