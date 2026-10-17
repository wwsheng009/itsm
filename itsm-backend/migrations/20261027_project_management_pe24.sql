-- =============================================================================
-- 项目管理 P2 组合视图与度量（PE-24）数据层兜底迁移
--
-- 背景：阶段门一次通过率需要「曾被打回」的唯一事实来源 =
--   project_gates.reject_count（reject 裁决 CAS 内原子 +1；历史行默认 0）。
--   权威定义 docs/project-management/plan/p2-portfolio-metrics-design.md §2 D-42/§3；
--   列由 ent Schema.Create 先行创建，本文件幂等补齐（对齐 pe21/pe22/pe23 双轨惯例）。
-- 说明：无新表 → RLS 015 覆盖清单/rls-apply 受管清单无变更。
-- 应用：go run -tags migrate . （或 psql -f）
-- 回滚：20261027_project_management_pe24_down.sql
-- =============================================================================

DO $$
BEGIN
    IF to_regclass('public.project_gates') IS NOT NULL THEN
        ALTER TABLE project_gates ADD COLUMN IF NOT EXISTS reject_count integer NOT NULL DEFAULT 0;
        RAISE NOTICE 'project-pe24 20261027: project_gates.reject_count ensured';
    ELSE
        RAISE NOTICE 'project-pe24 20261027: skip project_gates (table not found; ent migrate 未执行?)';
    END IF;
END $$;