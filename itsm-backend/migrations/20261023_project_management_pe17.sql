-- =============================================================================
-- 项目管理 P1 状态报告模板（PE-17）数据层兜底迁移
--
-- 背景：project_report_templates 承载"状态报告模板 + 周期"（数据驱动）；
--   表由 ent Schema.Create 先行创建，本文件幂等补齐索引（对齐 pe11 双轨惯例）。
-- 应用：go run -tags migrate . （或 psql -f）
-- 回滚：20261023_project_management_pe17_down.sql
-- =============================================================================

DO $$
BEGIN
    IF to_regclass('public.project_report_templates') IS NOT NULL THEN
        CREATE UNIQUE INDEX IF NOT EXISTS uq_project_report_templates_tenant_name
            ON project_report_templates (tenant_id, name);
        CREATE INDEX IF NOT EXISTS idx_project_report_templates_tenant_status
            ON project_report_templates (tenant_id, status);
        RAISE NOTICE 'project-pe17 20261023: project_report_templates indexes ensured';
    ELSE
        RAISE NOTICE 'project-pe17 20261023: skip (table not found; ent migrate 未执行?)';
    END IF;
END $$;
