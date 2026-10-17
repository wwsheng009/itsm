-- =============================================================================
-- 项目管理 P2 预算与工时（PE-22）数据层兜底迁移
--
-- 背景：轻量预算 = projects.budget_amount / budget_currency 两个可空列
--   （不做财务级核算；权威定义 docs/project-management/plan/p2-budget-hours-design.md §4）；
--   任务级 actual_hours 字段已随 P0 ent schema 存在，无需本迁移。
--   列由 ent Schema.Create 先行创建，本文件幂等补齐（对齐 pe11/pe21 双轨惯例）。
-- 应用：go run -tags migrate . （或 psql -f）
-- 回滚：20261025_project_management_pe22_down.sql
-- =============================================================================

DO $$
BEGIN
    IF to_regclass('public.projects') IS NOT NULL THEN
        ALTER TABLE projects ADD COLUMN IF NOT EXISTS budget_amount double precision;
        ALTER TABLE projects ADD COLUMN IF NOT EXISTS budget_currency varchar(8);
        RAISE NOTICE 'project-pe22 20261025: projects budget columns ensured';
    ELSE
        RAISE NOTICE 'project-pe22 20261025: skip (table not found; ent migrate 未执行?)';
    END IF;
END $$;
