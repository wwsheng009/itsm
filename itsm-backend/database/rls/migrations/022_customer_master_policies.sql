-- =============================================================================
-- RLS Migration 022 — 客户主数据 P0 关联表策略
--
-- 覆盖：
--   customer_contacts     客户联系人
--   customer_tenant_links 客户 ↔ 租户映射（linked_tenant_id 为弱引用）
--
-- 两张表均以 tenant_id 为数据归属；策略谓词与既有 003–021 一致：
--   tenant_id = get_current_tenant_id()
-- GUC 缺失时 get_current_tenant_id() 返回 NULL，条件为 NULL → fail closed。
--
-- 应用：go run -tags rlsapply ./cmd/rls-apply -files 022_customer_master_policies.sql
-- 回滚：022_customer_master_policies_rollback.sql
-- =============================================================================

CREATE OR REPLACE FUNCTION get_current_tenant_id() RETURNS INTEGER AS $$
BEGIN
    RETURN NULLIF(current_setting('app.current_tenant', true), '')::INTEGER;
EXCEPTION
    WHEN invalid_text_representation THEN
        RETURN NULL;
END;
$$ LANGUAGE plpgsql STABLE;

DO $$
DECLARE
    t text;
    p text;
BEGIN
    FOREACH t IN ARRAY ARRAY[
        'customer_contacts',
        'customer_tenant_links'
    ]
    LOOP
        IF to_regclass('public.' || quote_ident(t)) IS NULL THEN
            RAISE NOTICE 'rls 022: skip % (table not found)', t;
            CONTINUE;
        END IF;
        p := 'tenant_isolation_' || t;
        EXECUTE format('ALTER TABLE %I ENABLE ROW LEVEL SECURITY', t);
        EXECUTE format('DROP POLICY IF EXISTS %I ON %I', p, t);
        EXECUTE format(
            'CREATE POLICY %I ON %I USING (tenant_id = get_current_tenant_id()) WITH CHECK (tenant_id = get_current_tenant_id())',
            p, t);
        RAISE NOTICE 'rls 022: % enabled (policy %)', t, p;
    END LOOP;
END $$;
