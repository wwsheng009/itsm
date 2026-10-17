-- 项目管理 P1 数据层回滚（仅索引；表本体由 ent 管理，另行按需删除）。
-- 警告：删除唯一索引会使 (project_id, version) / (task_id, depends_on_task_id) 失去幂等约束。

DO $$
BEGIN
    IF to_regclass('public.project_milestones') IS NOT NULL THEN
        DROP INDEX IF EXISTS idx_project_milestones;
    END IF;
    IF to_regclass('public.project_task_dependencies') IS NOT NULL THEN
        DROP INDEX IF EXISTS uk_project_task_deps;
        DROP INDEX IF EXISTS idx_project_task_deps_reverse;
    END IF;
    IF to_regclass('public.project_baselines') IS NOT NULL THEN
        DROP INDEX IF EXISTS uk_project_baselines;
    END IF;
END $$;