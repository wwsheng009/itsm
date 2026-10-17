-- =============================================================================
-- 项目管理 P2 资源负荷（PE-23）回滚：移除周容量列
-- 配对：20261026_project_management_pe23.sql
-- =============================================================================

DO $$
BEGIN
    IF to_regclass('public.projects') IS NOT NULL THEN
        ALTER TABLE projects DROP COLUMN IF EXISTS default_weekly_capacity_hours;
        RAISE NOTICE 'project-pe23 20261026 down: projects.default_weekly_capacity_hours dropped';
    ELSE
        RAISE NOTICE 'project-pe23 20261026 down: skip projects (table not found)';
    END IF;

    IF to_regclass('public.project_members') IS NOT NULL THEN
        ALTER TABLE project_members DROP COLUMN IF EXISTS weekly_capacity_hours;
        RAISE NOTICE 'project-pe23 20261026 down: project_members.weekly_capacity_hours dropped';
    ELSE
        RAISE NOTICE 'project-pe23 20261026 down: skip project_members (table not found)';
    END IF;
END $$;