-- 项目管理 P1 模板（PE-11）数据层兜底
--
-- 内容：project_templates 索引 + projects 模板应用列（ADD COLUMN IF NOT EXISTS）
--      （表/列由 ent Schema.Create 先行创建，本文件为幂等兜底，对齐既有双轨惯例）。
--
-- RLS：project_templates 已列入 database/rls/migrations/015_project_management_policies.sql
--      覆盖清单（表创建后重跑 015 即自动获得 tenant_isolation 策略）。
-- 回滚：20261019_project_management_pe11_down.sql

DO $$
BEGIN
    IF to_regclass('public.project_templates') IS NULL THEN
        RAISE NOTICE 'project-pe11 20261019: skip project_templates (table not found)';
    ELSE
        CREATE UNIQUE INDEX IF NOT EXISTS uq_project_templates_tenant_name
            ON project_templates (tenant_id, name);
        CREATE INDEX IF NOT EXISTS idx_project_templates_tenant_status
            ON project_templates (tenant_id, status);
        RAISE NOTICE 'project-pe11 20261019: project_templates indexes ensured';
    END IF;
END $$;

-- 模板应用幂等闸门：projects.template_id 非空即已应用（一项目一模板）
ALTER TABLE projects ADD COLUMN IF NOT EXISTS template_id BIGINT;
ALTER TABLE projects ADD COLUMN IF NOT EXISTS template_applied_at TIMESTAMPTZ;
ALTER TABLE projects ADD COLUMN IF NOT EXISTS template_applied_by BIGINT;