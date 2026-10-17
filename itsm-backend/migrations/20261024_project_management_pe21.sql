-- =============================================================================
-- 项目管理 P2 风险/问题台账（PE-21）数据层兜底迁移
--
-- 背景：project_risks 承载风险/问题台账（kind=risk|issue）+ 转问题单/变更回写
--   （related_problem_id / related_change_id）；表由 ent Schema.Create 先行创建，
--   本文件幂等补齐索引（对齐 pe11/pe17 双轨惯例）。
-- 应用：go run -tags migrate . （或 psql -f）
-- 回滚：20261024_project_management_pe21_down.sql
-- =============================================================================

DO $$
BEGIN
    IF to_regclass('public.project_risks') IS NOT NULL THEN
        CREATE INDEX IF NOT EXISTS idx_project_risks_tenant_project_status
            ON project_risks (tenant_id, project_id, status);
        CREATE INDEX IF NOT EXISTS idx_project_risks_tenant_project_level
            ON project_risks (tenant_id, project_id, level);
        RAISE NOTICE 'project-pe21 20261024: project_risks indexes ensured';
    ELSE
        RAISE NOTICE 'project-pe21 20261024: skip (table not found; ent migrate 未执行?)';
    END IF;
END $$;
