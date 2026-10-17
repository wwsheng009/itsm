-- 项目管理 P1 结项清单（PE-15）数据层兜底
--
-- 内容：
--   1) project_closures 索引（表/列由 ent Schema.Create 先行创建，本文件为幂等兜底）；
--   2) survey_responses.project_id 增列 + 部分索引（P0 DDL §3 约定，P0 批次遗漏，PE-15 补齐；
--      结项 CSAT 的答卷经该列关联项目）。
--
-- RLS：project_closures 已列入 database/rls/migrations/015_project_management_policies.sql
--      覆盖清单（表创建后重跑 015 即自动获得 tenant_isolation 策略）。
--      survey_responses 不在 RLS 清单（与 P0 口径一致）。
-- 回滚：20261021_project_management_pe15_down.sql

DO $$
BEGIN
    IF to_regclass('public.project_closures') IS NULL THEN
        RAISE NOTICE 'project-pe15 20261021: skip project_closures (table not found)';
    ELSE
        CREATE UNIQUE INDEX IF NOT EXISTS uk_project_closures
            ON project_closures (tenant_id, project_id);
        RAISE NOTICE 'project-pe15 20261021: project_closures indexes ensured';
    END IF;
END $$;

DO $$
BEGIN
    IF to_regclass('public.survey_responses') IS NULL THEN
        RAISE NOTICE 'project-pe15 20261021: skip survey_responses.project_id (table not found)';
    ELSE
        ALTER TABLE survey_responses ADD COLUMN IF NOT EXISTS project_id BIGINT;
        CREATE INDEX IF NOT EXISTS idx_survey_responses_project
            ON survey_responses (tenant_id, project_id) WHERE project_id IS NOT NULL;
        RAISE NOTICE 'project-pe15 20261021: survey_responses.project_id ensured';
    END IF;
END $$;