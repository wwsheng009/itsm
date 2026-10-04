-- =============================================================================
-- RLS Migration 004 rollback — 批次 2（生命周期域）策略回滚
--
-- 与 004_lifecycle_tables_policies.sql 对齐：DROP POLICY + DISABLE ROW LEVEL SECURITY。
-- 不改动 helper 函数、002/003 既有策略与试点表 FORCE 状态。幂等。
--
-- 应用方式：
--   go run -tags rlsapply ./cmd/rls-apply -files 004_lifecycle_tables_policies_rollback.sql
-- =============================================================================

DO $$
DECLARE
    t text;
    p text;
BEGIN
    FOREACH t IN ARRAY ARRAY[
        'notifications',
        'notification_deliveries',
        'notification_preferences',
        'ticket_notifications',
        'sla_definitions',
        'sla_metrics',
        'sla_violations',
        'sla_alert_histories',
        'knowledge_articles',
        'knowledge_article_likes',
        'service_requests',
        'service_request_approvals',
        'service_catalog_items',
        'invitations',
        'incidents',
        'incident_alerts',
        'ticket_types',
        'ticket_templates'
    ]
    LOOP
        IF to_regclass('public.' || quote_ident(t)) IS NULL THEN
            RAISE NOTICE 'rls 004 rollback: skip % (table not found)', t;
            CONTINUE;
        END IF;
        p := 'tenant_isolation_' || t;
        EXECUTE format('DROP POLICY IF EXISTS %I ON %I', p, t);
        EXECUTE format('ALTER TABLE %I DISABLE ROW LEVEL SECURITY', t);
        RAISE NOTICE 'rls 004 rollback: % disabled', t;
    END LOOP;
END $$;
