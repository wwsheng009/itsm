-- =============================================================================
-- RLS Migration 005 回滚 — 移除批次 3（AI/连接器/邮件域 14 表）策略并关闭 RLS
-- 注意：回滚只影响本批次表；003/004/009 的策略不受影响。
-- =============================================================================
DO $$
DECLARE
    t text;
BEGIN
    FOREACH t IN ARRAY ARRAY[
        'conversations', 'messages',
        'mcp_servers', 'mcp_server_tools', 'tool_invocations',
        'connector_configs', 'connector_inbound_dedups',
        'email_conversations', 'email_intake_analyses', 'email_outbound_messages',
        'inbound_email_messages', 'feishu_ticket_syncs',
        'domain_configs', 'provisioning_tasks'
    ]
    LOOP
        IF to_regclass('public.' || quote_ident(t)) IS NULL THEN
            CONTINUE;
        END IF;
        EXECUTE format('DROP POLICY IF EXISTS %I ON %I', 'tenant_isolation_' || t, t);
        EXECUTE format('ALTER TABLE %I NO FORCE ROW LEVEL SECURITY', t);
        EXECUTE format('ALTER TABLE %I DISABLE ROW LEVEL SECURITY', t);
        RAISE NOTICE 'rls 005 rollback: % policy dropped', t;
    END LOOP;
END $$;
