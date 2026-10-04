-- =============================================================================
-- RLS Migration 005 — AI/连接器/邮件域策略扩展（R2 批次 3）
--
-- 背景：003/004 落地后 enforce 演练 70/70；本迁移承接《rls-shadow-observation-2026-10-03》
-- §5 第 7 条「其余 ~100 张业务表按批次推进」，批次 3 覆盖「AI 会话/工具、MCP、
-- 连接器与邮件摄取、多租户域名与供给任务」域（14 表）：
--   AI 会话：conversations / messages
--   AI 工具：mcp_servers / mcp_server_tools / tool_invocations
--   连接器：connector_configs / connector_inbound_dedups
--   邮件摄取：email_conversations / email_intake_analyses / email_outbound_messages /
--             inbound_email_messages / feishu_ticket_syncs
--   其他：domain_configs（租户专属域名）/ provisioning_tasks（供给流水）
--
-- 策略谓词：与 003/004 一致，统一 get_current_tenant_id()（fail-closed）。
--   USING / WITH CHECK: tenant_id = get_current_tenant_id()
--
-- 关键取舍（迁移审查记录）：
--   1. 只 ENABLE 不 FORCE：与 003/004/009 约定一致；请求路径经低权角色 itsm_app 强制生效。
--      CLI/seed/迁移工具（owner 连接）不受约束，待工具链 GUC 化后统一评估 FORCE。
--   2. conversations.tenant_id 可空（历史/系统会话）：纯 NULL 行对任何租户会话不可见，
--      即 fail-closed——与 2026-10-03 messages 收尾的「双方缺失 fail-closed」口径一致；
--      系统作用域（admin 连接）仍可维护。当前联调库 NULL 行 = 0。
--      写入侧：handlers/ai/repository_impl.go 的 Conversation.Create 显式 SetTenantID。
--   3. 邮件/连接器 worker 在 enforce 下应按摄取来源注入租户 ctx 或走 system 作用域；
--      本批次先以低权探针 + enforce 验收观察（失败即 fail-closed，不会越权）。
--   4. 幂等：重复执行安全；缺表跳过并 NOTICE。
--   5. 回滚：005_ai_connector_tables_policies_rollback.sql（DROP POLICY + DISABLE）。
--
-- 应用方式（无 psql 环境）：
--   go run -tags rlsapply ./cmd/rls-apply -files 005_ai_connector_tables_policies.sql
--   go run -tags rlsapply ./cmd/rls-apply -verify
-- =============================================================================

-- 0) 候选函数（前向修复实现；幂等）
CREATE OR REPLACE FUNCTION get_current_tenant_id() RETURNS INTEGER AS $$
BEGIN
    RETURN NULLIF(current_setting('app.current_tenant', true), '')::INTEGER;
EXCEPTION
    WHEN invalid_text_representation THEN
        RETURN NULL;
END;
$$ LANGUAGE plpgsql STABLE;

COMMENT ON FUNCTION get_current_tenant_id() IS
    'Returns app.current_tenant for tenant RLS; missing/invalid context fails closed';

-- 1) 批次 3 表清单：ENABLE RLS + tenant_isolation 策略（幂等）
DO $$
DECLARE
    t text;
    p text;
BEGIN
    FOREACH t IN ARRAY ARRAY[
        -- AI 会话与消息（conversations 可空：NULL 对租户不可见 = fail-closed）
        'conversations',
        'messages',
        -- AI 工具与 MCP
        'mcp_servers',
        'mcp_server_tools',
        'tool_invocations',
        -- 连接器
        'connector_configs',
        'connector_inbound_dedups',
        -- 邮件摄取
        'email_conversations',
        'email_intake_analyses',
        'email_outbound_messages',
        'inbound_email_messages',
        'feishu_ticket_syncs',
        -- 多租户域名与供给
        'domain_configs',
        'provisioning_tasks'
    ]
    LOOP
        IF to_regclass('public.' || quote_ident(t)) IS NULL THEN
            RAISE NOTICE 'rls 005: skip % (table not found)', t;
            CONTINUE;
        END IF;
        EXECUTE format('ALTER TABLE %I ENABLE ROW LEVEL SECURITY', t);
        p := 'tenant_isolation_' || t;
        EXECUTE format('DROP POLICY IF EXISTS %I ON %I', p, t);
        EXECUTE format(
            'CREATE POLICY %I ON %I USING (tenant_id = get_current_tenant_id()) WITH CHECK (tenant_id = get_current_tenant_id())',
            p, t);
        RAISE NOTICE 'rls 005: % enabled (policy %)', t, p;
    END LOOP;
END $$;

-- 2) 验证语句（可选运行）
-- SELECT c.relname, c.relrowsecurity, c.relforcerowsecurity
-- FROM pg_class c JOIN pg_namespace n ON n.oid = c.relnamespace
-- WHERE n.nspname='public' AND c.relname IN ('conversations','messages','mcp_servers',
--   'mcp_server_tools','tool_invocations','connector_configs','connector_inbound_dedups',
--   'email_conversations','email_intake_analyses','email_outbound_messages',
--   'inbound_email_messages','feishu_ticket_syncs','domain_configs','provisioning_tasks')
-- ORDER BY c.relname;
