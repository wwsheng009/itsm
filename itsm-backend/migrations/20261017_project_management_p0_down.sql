-- 项目管理模块 P0 数据层回滚（破坏性）：删除本批次新增的列与索引。
--
-- 警告：DROP COLUMN 会丢弃列内数据（projects 扩展字段 / 单据 project_id）；
-- 仅在开发/联调环境回滚使用，生产环境回滚前必须备份。
-- 表本体（project_members / project_tasks）由 ent 管理，另行按需删除。

DO $$
BEGIN
    IF to_regclass('public.projects') IS NOT NULL THEN
        DROP INDEX IF EXISTS idx_projects_tenant_status;
        DROP INDEX IF EXISTS idx_projects_tenant_type;
        DROP INDEX IF EXISTS idx_projects_owner_team;
        DROP INDEX IF EXISTS idx_projects_tenant_manager;
        ALTER TABLE projects DROP COLUMN IF EXISTS type;
        ALTER TABLE projects DROP COLUMN IF EXISTS parent_project_id;
        ALTER TABLE projects DROP COLUMN IF EXISTS health;
        ALTER TABLE projects DROP COLUMN IF EXISTS progress;
        ALTER TABLE projects DROP COLUMN IF EXISTS actual_start;
        ALTER TABLE projects DROP COLUMN IF EXISTS actual_end;
        ALTER TABLE projects DROP COLUMN IF EXISTS closed_at;
        ALTER TABLE projects DROP COLUMN IF EXISTS owner_team_id;
        ALTER TABLE projects DROP COLUMN IF EXISTS version;
        ALTER TABLE projects DROP COLUMN IF EXISTS deleted_at;
        RAISE NOTICE 'project-p0 20261017 down: projects columns dropped';
    END IF;

    IF to_regclass('public.tickets') IS NOT NULL THEN
        DROP INDEX IF EXISTS idx_tickets_project;
        ALTER TABLE tickets DROP COLUMN IF EXISTS project_id;
    END IF;

    IF to_regclass('public.incidents') IS NOT NULL THEN
        DROP INDEX IF EXISTS idx_incidents_project;
        ALTER TABLE incidents DROP COLUMN IF EXISTS project_id;
    END IF;

    IF to_regclass('public.problems') IS NOT NULL THEN
        DROP INDEX IF EXISTS idx_problems_project;
        ALTER TABLE problems DROP COLUMN IF EXISTS project_id;
    END IF;

    IF to_regclass('public.changes') IS NOT NULL THEN
        DROP INDEX IF EXISTS idx_changes_project;
        ALTER TABLE changes DROP COLUMN IF EXISTS project_id;
    END IF;

    IF to_regclass('public.service_requests') IS NOT NULL THEN
        DROP INDEX IF EXISTS idx_service_requests_project;
        ALTER TABLE service_requests DROP COLUMN IF EXISTS project_id;
    END IF;

    IF to_regclass('public.project_members') IS NOT NULL THEN
        DROP INDEX IF EXISTS uk_project_members_live;
        DROP INDEX IF EXISTS idx_project_members_tenant_user;
    END IF;

    IF to_regclass('public.project_tasks') IS NOT NULL THEN
        DROP INDEX IF EXISTS idx_project_tasks_project;
        DROP INDEX IF EXISTS idx_project_tasks_assignee;
        DROP INDEX IF EXISTS idx_project_tasks_parent;
    END IF;
END $$;