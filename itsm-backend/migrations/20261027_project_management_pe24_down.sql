-- =============================================================================
-- 项目管理 P2 组合视图与度量（PE-24）回滚：移除门打回计数列
-- 配对：20261027_project_management_pe24.sql
-- =============================================================================

DO $$
BEGIN
    IF to_regclass('public.project_gates') IS NOT NULL THEN
        ALTER TABLE project_gates DROP COLUMN IF EXISTS reject_count;
        RAISE NOTICE 'project-pe24 20261027 down: project_gates.reject_count dropped';
    ELSE
        RAISE NOTICE 'project-pe24 20261027 down: skip project_gates (table not found)';
    END IF;
END $$;