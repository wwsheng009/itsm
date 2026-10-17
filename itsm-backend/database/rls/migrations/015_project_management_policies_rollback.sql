-- =============================================================================
-- RLS Migration 015 回滚 — 项目管理 P0 表策略
--
-- 幂等：表不存在时跳过；仅删除本批次策略并 DISABLE RLS。
-- 应用方式：
--   go run -tags rlsapply ./cmd/rls-apply -files 015_project_management_policies_rollback.sql
-- =============================================================================

DO $$
DECLARE
    t text;
    p text;
BEGIN
    FOREACH t IN ARRAY ARRAY[
        'project_members',
        'project_tasks',
        'project_task_dependencies',
        'project_milestones',
        'project_baselines',
        'project_templates'
    ]
    LOOP
        IF to_regclass('public.' || quote_ident(t)) IS NULL THEN
            RAISE NOTICE 'rls 015 rollback: skip % (table not found)', t;
            CONTINUE;
        END IF;
        p := 'tenant_isolation_' || t;
        EXECUTE format('DROP POLICY IF EXISTS %I ON %I', p, t);
        EXECUTE format('ALTER TABLE %I DISABLE ROW LEVEL SECURITY', t);
        RAISE NOTICE 'rls 015 rollback: % policy dropped, RLS disabled', t;
    END LOOP;
END $$;