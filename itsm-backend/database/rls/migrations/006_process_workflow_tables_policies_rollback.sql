-- =============================================================================
-- RLS Migration 006 回滚 — 移除批次 4（流程/工作流/审批引擎域 14 表）策略并关闭 RLS
-- 注意：回滚只影响本批次表；003/004/005/009 的策略不受影响。
-- =============================================================================
DO $$
DECLARE
    t text;
BEGIN
    FOREACH t IN ARRAY ARRAY[
        'process_instances', 'process_tasks', 'process_audit_logs', 'process_variables',
        'process_timers', 'process_version_changelogs', 'process_approval_decisions',
        'process_execution_histories',
        'workflow_instances', 'workflow_tasks', 'workflow_templates', 'workflow_versions',
        'workflows',
        'bpmn_permissions'
    ]
    LOOP
        IF to_regclass('public.' || quote_ident(t)) IS NULL THEN
            CONTINUE;
        END IF;
        EXECUTE format('DROP POLICY IF EXISTS %I ON %I', 'tenant_isolation_' || t, t);
        EXECUTE format('ALTER TABLE %I NO FORCE ROW LEVEL SECURITY', t);
        EXECUTE format('ALTER TABLE %I DISABLE ROW LEVEL SECURITY', t);
        RAISE NOTICE 'rls 006 rollback: % policy dropped', t;
    END LOOP;
END $$;
