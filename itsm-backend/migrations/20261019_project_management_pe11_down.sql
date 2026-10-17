-- 项目管理 P1 模板（PE-11）数据层回滚（索引 + 应用列；表本体由 ent 管理）。
-- 警告：删除 template_id 后项目将丢失"已应用模板"幂等闸门（重复应用可能再次生成）。

DO $$
BEGIN
    IF to_regclass('public.project_templates') IS NOT NULL THEN
        DROP INDEX IF EXISTS uq_project_templates_tenant_name;
        DROP INDEX IF EXISTS idx_project_templates_tenant_status;
    END IF;
END $$;

ALTER TABLE projects DROP COLUMN IF EXISTS template_id;
ALTER TABLE projects DROP COLUMN IF EXISTS template_applied_at;
ALTER TABLE projects DROP COLUMN IF EXISTS template_applied_by;