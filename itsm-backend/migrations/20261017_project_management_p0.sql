-- 项目管理模块 P0 数据层（PE-1；权威定义：docs/project-management/plan/p0-data-ddl.md）
--
-- 内容：
--   1) projects 增列（type/parent_project_id/health/progress/actual_start/actual_end/
--      closed_at/owner_team_id/version/deleted_at）——列由 ent Schema.Create 先行创建，
--      本文件为幂等兜底（对齐既有双轨惯例）；
--   2) 业务单据五表增列 project_id（同租户关联；部分索引兜底）；
--   3) project_members / project_tasks（表由 ent 创建）索引兜底。
--
-- RLS：project_members / project_tasks 策略见 database/rls/migrations/015_project_management_policies.sql
-- 回滚：20261017_project_management_p0_down.sql（破坏性，见文件头注释）

DO $$
BEGIN
    IF to_regclass('public.projects') IS NULL THEN
        RAISE NOTICE 'project-p0 20261017: skip projects (table not found)';
    ELSE
        ALTER TABLE projects ADD COLUMN IF NOT EXISTS type VARCHAR(16) NOT NULL DEFAULT 'internal';
        ALTER TABLE projects ADD COLUMN IF NOT EXISTS parent_project_id BIGINT;
        ALTER TABLE projects ADD COLUMN IF NOT EXISTS health VARCHAR(8) NOT NULL DEFAULT 'green';
        ALTER TABLE projects ADD COLUMN IF NOT EXISTS progress INTEGER NOT NULL DEFAULT 0;
        ALTER TABLE projects ADD COLUMN IF NOT EXISTS actual_start TIMESTAMPTZ;
        ALTER TABLE projects ADD COLUMN IF NOT EXISTS actual_end TIMESTAMPTZ;
        ALTER TABLE projects ADD COLUMN IF NOT EXISTS closed_at TIMESTAMPTZ;
        ALTER TABLE projects ADD COLUMN IF NOT EXISTS owner_team_id BIGINT;
        ALTER TABLE projects ADD COLUMN IF NOT EXISTS version INTEGER NOT NULL DEFAULT 1;
        ALTER TABLE projects ADD COLUMN IF NOT EXISTS deleted_at TIMESTAMPTZ;
        CREATE INDEX IF NOT EXISTS idx_projects_tenant_status  ON projects (tenant_id, status);
        CREATE INDEX IF NOT EXISTS idx_projects_tenant_type    ON projects (tenant_id, type);
        CREATE INDEX IF NOT EXISTS idx_projects_owner_team     ON projects (tenant_id, owner_team_id);
        CREATE INDEX IF NOT EXISTS idx_projects_tenant_manager ON projects (tenant_id, manager_id);
        RAISE NOTICE 'project-p0 20261017: projects columns ensured';
    END IF;

    IF to_regclass('public.tickets') IS NULL THEN
        RAISE NOTICE 'project-p0 20261017: skip tickets (table not found)';
    ELSE
        ALTER TABLE tickets ADD COLUMN IF NOT EXISTS project_id BIGINT;
        CREATE INDEX IF NOT EXISTS idx_tickets_project ON tickets (tenant_id, project_id) WHERE project_id IS NOT NULL;
    END IF;

    IF to_regclass('public.incidents') IS NULL THEN
        RAISE NOTICE 'project-p0 20261017: skip incidents (table not found)';
    ELSE
        ALTER TABLE incidents ADD COLUMN IF NOT EXISTS project_id BIGINT;
        CREATE INDEX IF NOT EXISTS idx_incidents_project ON incidents (tenant_id, project_id) WHERE project_id IS NOT NULL;
    END IF;

    IF to_regclass('public.problems') IS NULL THEN
        RAISE NOTICE 'project-p0 20261017: skip problems (table not found)';
    ELSE
        ALTER TABLE problems ADD COLUMN IF NOT EXISTS project_id BIGINT;
        CREATE INDEX IF NOT EXISTS idx_problems_project ON problems (tenant_id, project_id) WHERE project_id IS NOT NULL;
    END IF;

    IF to_regclass('public.changes') IS NULL THEN
        RAISE NOTICE 'project-p0 20261017: skip changes (table not found)';
    ELSE
        ALTER TABLE changes ADD COLUMN IF NOT EXISTS project_id BIGINT;
        CREATE INDEX IF NOT EXISTS idx_changes_project ON changes (tenant_id, project_id) WHERE project_id IS NOT NULL;
    END IF;

    IF to_regclass('public.service_requests') IS NULL THEN
        RAISE NOTICE 'project-p0 20261017: skip service_requests (table not found)';
    ELSE
        ALTER TABLE service_requests ADD COLUMN IF NOT EXISTS project_id BIGINT;
        CREATE INDEX IF NOT EXISTS idx_service_requests_project ON service_requests (tenant_id, project_id) WHERE project_id IS NOT NULL;
    END IF;

    IF to_regclass('public.project_members') IS NULL THEN
        RAISE NOTICE 'project-p0 20261017: skip project_members indexes (table not found)';
    ELSE
        CREATE UNIQUE INDEX IF NOT EXISTS uk_project_members_live
            ON project_members (tenant_id, project_id, user_id) WHERE status = 'active';
        CREATE INDEX IF NOT EXISTS idx_project_members_tenant_user
            ON project_members (tenant_id, user_id);
        RAISE NOTICE 'project-p0 20261017: project_members indexes ensured';
    END IF;

    IF to_regclass('public.project_tasks') IS NULL THEN
        RAISE NOTICE 'project-p0 20261017: skip project_tasks indexes (table not found)';
    ELSE
        CREATE INDEX IF NOT EXISTS idx_project_tasks_project  ON project_tasks (tenant_id, project_id, status);
        CREATE INDEX IF NOT EXISTS idx_project_tasks_assignee ON project_tasks (tenant_id, assignee_id, status);
        CREATE INDEX IF NOT EXISTS idx_project_tasks_parent   ON project_tasks (parent_task_id);
        RAISE NOTICE 'project-p0 20261017: project_tasks indexes ensured';
    END IF;
END $$;