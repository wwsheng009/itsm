-- =============================================================================
-- RLS Migration 020 — SLA 定义快照租户隔离（Q-09 裁决 B）
--
-- 表：sla_definition_snapshots（customer 租户内副本）
-- 策略：tenant_id = get_current_tenant_id()（fail-closed）
-- 说明：provider 写入 customer 租户由显式下发动作完成；customer 只读本租户行。
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
    t text := 'sla_definition_snapshots';
    p text := 'sla_definition_snapshots_tenant_all';
BEGIN
    IF to_regclass('public.' || quote_ident(t)) IS NULL THEN
        RAISE NOTICE 'rls 020: skip % (table not found)', t;
    ELSE
        EXECUTE format('ALTER TABLE %I ENABLE ROW LEVEL SECURITY', t);
        EXECUTE format('DROP POLICY IF EXISTS %I ON %I', p, t);
        EXECUTE format(
            'CREATE POLICY %I ON %I USING (tenant_id = get_current_tenant_id()) WITH CHECK (tenant_id = get_current_tenant_id())',
            p, t);
        RAISE NOTICE 'rls 020: % enabled (policy %)', t, p;
    END IF;
END $$;
