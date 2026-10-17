-- 项目管理 P1 结项清单（PE-15）回滚
--
-- 说明：仅回滚索引与增列兜底；表由 ent 管理（如需彻底回滚：
--      DROP TABLE IF EXISTS project_closures; —— 会丢失结项清单记录，谨慎使用）。
--      survey_responses.project_id 的 DROP 会丢弃 CSAT 与项目的关联，谨慎使用。

DROP INDEX IF EXISTS uk_project_closures;

DO $$
BEGIN
    IF to_regclass('public.survey_responses') IS NULL THEN
        RAISE NOTICE 'project-pe15 down 20261021: skip survey_responses (table not found)';
    ELSE
        DROP INDEX IF EXISTS idx_survey_responses_project;
        ALTER TABLE survey_responses DROP COLUMN IF EXISTS project_id;
        RAISE NOTICE 'project-pe15 down 20261021: survey_responses.project_id dropped';
    END IF;
END $$;