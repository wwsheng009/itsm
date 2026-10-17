-- 项目管理 P1 状态报告模板（PE-17）数据层回滚（索引；表本体由 ent 管理）。

DO $$
BEGIN
    IF to_regclass('public.project_report_templates') IS NOT NULL THEN
        DROP INDEX IF EXISTS uq_project_report_templates_tenant_name;
        DROP INDEX IF EXISTS idx_project_report_templates_tenant_status;
    END IF;
END $$;
