-- 项目管理 P1 数据层（PE-13 基线 / PE-14 里程碑与依赖）
--
-- 内容：project_milestones / project_task_dependencies / project_baselines 索引兜底
--      （表由 ent Schema.Create 先行创建，本文件为幂等兜底，对齐既有双轨惯例）。
--
-- RLS：三表已列入 database/rls/migrations/015_project_management_policies.sql 覆盖清单
--      （表创建后重跑 015 即自动获得 tenant_isolation 策略）。
-- 回滚：20261018_project_management_p1_down.sql

DO $$
BEGIN
    IF to_regclass('public.project_milestones') IS NULL THEN
        RAISE NOTICE 'project-p1 20261018: skip project_milestones (table not found)';
    ELSE
        CREATE INDEX IF NOT EXISTS idx_project_milestones ON project_milestones (tenant_id, project_id, status);
        RAISE NOTICE 'project-p1 20261018: project_milestones index ensured';
    END IF;

    IF to_regclass('public.project_task_dependencies') IS NULL THEN
        RAISE NOTICE 'project-p1 20261018: skip project_task_dependencies (table not found)';
    ELSE
        CREATE UNIQUE INDEX IF NOT EXISTS uk_project_task_deps
            ON project_task_dependencies (task_id, depends_on_task_id);
        CREATE INDEX IF NOT EXISTS idx_project_task_deps_reverse
            ON project_task_dependencies (depends_on_task_id);
        RAISE NOTICE 'project-p1 20261018: project_task_dependencies indexes ensured';
    END IF;

    IF to_regclass('public.project_baselines') IS NULL THEN
        RAISE NOTICE 'project-p1 20261018: skip project_baselines (table not found)';
    ELSE
        CREATE UNIQUE INDEX IF NOT EXISTS uk_project_baselines
            ON project_baselines (project_id, version);
        RAISE NOTICE 'project-p1 20261018: project_baselines index ensured';
    END IF;
END $$;