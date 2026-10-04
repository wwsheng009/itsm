-- =============================================================================
-- RLS Migration 004 — 生命周期域策略扩展（R2 批次 2）
--
-- 背景：003 落地了工单核心/组织/成员/工作台（批次 1），enforce 演练 70/70；
-- 本迁移承接《rls-shadow-observation-2026-10-03》§5 第 6 条「其余业务表按批次推进」，
-- 批次 2 覆盖「工单生命周期协作」域（均为 NOT NULL tenant_id 的请求面/worker 表）：
--   通知：notifications / notification_deliveries / notification_preferences /
--         ticket_notifications
--   SLA：sla_definitions / sla_metrics / sla_violations / sla_alert_histories
--   知识库：knowledge_articles / knowledge_article_likes
--   服务请求：service_requests / service_request_approvals / service_catalog_items
--   邀请：invitations（创建=请求 ctx；Inspect/Accept 预认证已按 D-14 走 system 作用域）
--   事件：incidents / incident_alerts
--   工单配置：ticket_types / ticket_templates
--
-- 策略谓词：与 003 一致，统一 get_current_tenant_id()（fail-closed）。
--   USING / WITH CHECK: tenant_id = get_current_tenant_id()
--
-- 关键取舍（迁移审查记录）：
--   1. 只 ENABLE 不 FORCE：与 003/009 约定一致；请求路径经低权角色 itsm_app 强制生效。
--      CLI/seed/迁移工具（owner 连接）不受约束，待工具链 GUC 化后统一评估 FORCE。
--   2. 通知投递 worker（commandbus）按命令 TenantID 注入 ctx
--      （internal/commandbus/commandbus.go:241），故 notifications/notification_deliveries
--      的写入在 enforce 下按命令租户校验，符合"请求面零 bypass"口径。
--   3. 幂等：重复执行安全；缺表跳过并 NOTICE。
--   4. 回滚：004_lifecycle_tables_policies_rollback.sql（DROP POLICY + DISABLE）。
--
-- 应用方式（无 psql 环境）：
--   go run -tags rlsapply ./cmd/rls-apply -files 004_lifecycle_tables_policies.sql
--   go run -tags rlsapply ./cmd/rls-apply -verify
-- =============================================================================

-- 0) 候选函数（与 003 相同的前向修复实现；幂等）
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

-- 1) 批次 2 表清单：ENABLE RLS + tenant_isolation 策略（幂等）
DO $$
DECLARE
    t text;
    p text;
BEGIN
    FOREACH t IN ARRAY ARRAY[
        -- 通知（worker 按 cmd.TenantID 注入 ctx；含投递/偏好/工单通知桥接）
        'notifications',
        'notification_deliveries',
        'notification_preferences',
        'ticket_notifications',
        -- SLA
        'sla_definitions',
        'sla_metrics',
        'sla_violations',
        'sla_alert_histories',
        -- 知识库
        'knowledge_articles',
        'knowledge_article_likes',
        -- 服务请求与目录
        'service_requests',
        'service_request_approvals',
        'service_catalog_items',
        -- 邀请（预认证路径见 D-14 system 作用域）
        'invitations',
        -- 事件
        'incidents',
        'incident_alerts',
        -- 工单配置
        'ticket_types',
        'ticket_templates'
    ]
    LOOP
        IF to_regclass('public.' || quote_ident(t)) IS NULL THEN
            RAISE NOTICE 'rls 004: skip % (table not found)', t;
            CONTINUE;
        END IF;
        EXECUTE format('ALTER TABLE %I ENABLE ROW LEVEL SECURITY', t);
        p := 'tenant_isolation_' || t;
        EXECUTE format('DROP POLICY IF EXISTS %I ON %I', p, t);
        EXECUTE format(
            'CREATE POLICY %I ON %I USING (tenant_id = get_current_tenant_id()) WITH CHECK (tenant_id = get_current_tenant_id())',
            p, t);
        RAISE NOTICE 'rls 004: % enabled (policy %)', t, p;
    END LOOP;
END $$;

-- 2) 验证语句（可选运行）
-- SELECT c.relname, c.relrowsecurity, c.relforcerowsecurity
-- FROM pg_class c JOIN pg_namespace n ON n.oid = c.relnamespace
-- WHERE n.nspname='public' AND c.relname IN ('notifications','notification_deliveries',
--   'notification_preferences','ticket_notifications','sla_definitions','sla_metrics',
--   'sla_violations','sla_alert_histories','knowledge_articles','knowledge_article_likes',
--   'service_requests','service_request_approvals','service_catalog_items','invitations',
--   'incidents','incident_alerts','ticket_types','ticket_templates')
-- ORDER BY c.relname;
