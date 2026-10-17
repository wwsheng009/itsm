-- =============================================================================
-- 项目管理 P2 资源负荷（PE-23）数据层兜底迁移
--
-- 背景：周容量两级配置 =
--   projects.default_weekly_capacity_hours（项目默认；NULL=内置 40h）
--   project_members.weekly_capacity_hours（成员覆盖；NULL=继承项目默认）
--   权威定义 docs/project-management/plan/p2-resource-heatmap-design.md §3；
--   列由 ent Schema.Create 先行创建，本文件幂等补齐（对齐 pe11/pe21/pe22 双轨惯例）。
-- 说明：无新表 → RLS 015 覆盖清单/rls-apply 受管清单无变更。
-- 应用：go run -tags migrate . （或 psql -f）
-- 回滚：20261026_project_management_pe23_down.sql
-- =============================================================================

DO $$
BEGIN
    IF to_regclass('public.projects') IS NOT NULL THEN
        ALTER TABLE projects ADD COLUMN IF NOT EXISTS default_weekly_capacity_hours double precision;
        RAISE NOTICE 'project-pe23 20261026: projects.default_weekly_capacity_hours ensured';
    ELSE
        RAISE NOTICE 'project-pe23 20261026: skip projects (table not found; ent migrate 未执行?)';
    END IF;

    IF to_regclass('public.project_members') IS NOT NULL THEN
        ALTER TABLE project_members ADD COLUMN IF NOT EXISTS weekly_capacity_hours double precision;
        RAISE NOTICE 'project-pe23 20261026: project_members.weekly_capacity_hours ensured';
    ELSE
        RAISE NOTICE 'project-pe23 20261026: skip project_members (table not found; ent migrate 未执行?)';
    END IF;
END $$;