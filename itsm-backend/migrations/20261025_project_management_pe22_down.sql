-- =============================================================================
-- 项目管理 P2 预算与工时（PE-22）回滚：移除 projects 预算列
-- 配对：20261025_project_management_pe22.sql
-- =============================================================================

DO $$
BEGIN
    IF to_regclass('public.projects') IS NOT NULL THEN
        ALTER TABLE projects DROP COLUMN IF EXISTS budget_currency;
        ALTER TABLE projects DROP COLUMN IF EXISTS budget_amount;
        RAISE NOTICE 'project-pe22 20261025 down: projects budget columns dropped';
    ELSE
        RAISE NOTICE 'project-pe22 20261025 down: skip (table not found)';
    END IF;
END $$;
