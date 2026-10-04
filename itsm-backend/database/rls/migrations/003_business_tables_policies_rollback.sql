-- =============================================================================
-- RLS Migration 003 回滚 — 批次 1（工单核心 / 组织与成员 / 工作台）
--
-- 逐表：DROP POLICY tenant_isolation_<table> → NO FORCE → DISABLE ROW LEVEL SECURITY。
-- 不改动 get_current_tenant_id()（009/019 旧表仍在使用）与 002 试点表。
-- 幂等：重复执行安全；缺表跳过。
-- =============================================================================

DO $$
DECLARE
    t text;
    p text;
BEGIN
    FOREACH t IN ARRAY ARRAY[
        'tickets',
        'ticket_comments',
        'ticket_attachments',
        'ticket_ccs',
        'ticket_workflow_records',
        'user_tenant_memberships',
        'user_tenant_membership_orgs',
        'groups',
        'projects',
        'workbench_views'
    ]
    LOOP
        IF to_regclass('public.' || quote_ident(t)) IS NULL THEN
            RAISE NOTICE 'rls 003 rollback: skip % (table not found)', t;
            CONTINUE;
        END IF;
        p := 'tenant_isolation_' || t;
        EXECUTE format('DROP POLICY IF EXISTS %I ON %I', p, t);
        EXECUTE format('ALTER TABLE %I NO FORCE ROW LEVEL SECURITY', t);
        EXECUTE format('ALTER TABLE %I DISABLE ROW LEVEL SECURITY', t);
        RAISE NOTICE 'rls 003 rollback: % disabled', t;
    END LOOP;
END $$;
