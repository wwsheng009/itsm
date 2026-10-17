-- 项目管理 P1 阶段门（PE-12）数据层兜底
--
-- 内容：project_gates 索引（表/列由 ent Schema.Create 先行创建，
--      本文件为幂等兜底，对齐既有双轨惯例）。
--
-- RLS：project_gates 已列入 database/rls/migrations/015_project_management_policies.sql
--      覆盖清单（表创建后重跑 015 即自动获得 tenant_isolation 策略）。
-- 回滚：20261020_project_management_pe12_down.sql

DO $$
BEGIN
    IF to_regclass('public.project_gates') IS NULL THEN
        RAISE NOTICE 'project-pe12 20261020: skip project_gates (table not found)';
    ELSE
        CREATE UNIQUE INDEX IF NOT EXISTS uk_project_gates
            ON project_gates (tenant_id, project_id, gate);
        CREATE INDEX IF NOT EXISTS project_gates_project_id_status
            ON project_gates (project_id, status);
        RAISE NOTICE 'project-pe12 20261020: project_gates indexes ensured';
    END IF;
END $$;
