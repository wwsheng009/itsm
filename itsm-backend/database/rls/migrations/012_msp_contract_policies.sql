-- =============================================================================
-- RLS Migration 012 — MSP SLA 合同表策略（D-SLA-4，方案 §4.4）
--
-- 背景：msp_contracts / msp_contract_sla_bindings 为 provider 侧合同资产；
--   customer 端只读授信（结算报表/下发视图的读面），写路径 provider-only。
--
-- 本迁移（幂等，重复执行安全）：
--   1) msp_contracts：ENABLE RLS +
--      - msp_contract_provider_all      FOR ALL    tenant_id = ctx          （provider 读写）
--      - msp_contract_customer_read     FOR SELECT customer_tenant_id = ctx（customer 只读授信）
--   2) msp_contract_sla_bindings：ENABLE RLS +
--      - msp_contract_binding_provider_all  FOR ALL    tenant_id = ctx      （provider 读写）
--      - msp_contract_binding_customer_read FOR SELECT EXISTS(合同属于 ctx)（customer 只读授信；
--        policy 内子查询对 msp_contracts 的 RLS 亦生效，customer ctx 仅可见自身合同）
--
-- 关键取舍：与 003–011 一致——只 ENABLE 不 FORCE；谓词统一 get_current_tenant_id()（fail-closed）。
--
-- 应用方式（无 psql 环境）：
--   go run -tags rlsapply ./cmd/rls-apply -files 012_msp_contract_policies.sql
--   go run -tags rlsapply ./cmd/rls-apply -verify
-- 回滚：012_msp_contract_policies_rollback.sql
-- =============================================================================

-- 0) 候选函数（与 003–011 相同的前向修复实现；幂等）
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

-- 1) msp_contracts：provider 读写 + customer 只读授信
DO $$
DECLARE
    t text := 'msp_contracts';
    p text;
BEGIN
    IF to_regclass('public.' || quote_ident(t)) IS NULL THEN
        RAISE NOTICE 'rls 012: skip % (table not found)', t;
    ELSE
        EXECUTE format('ALTER TABLE %I ENABLE ROW LEVEL SECURITY', t);

        p := 'msp_contract_provider_all';
        EXECUTE format('DROP POLICY IF EXISTS %I ON %I', p, t);
        EXECUTE format(
            'CREATE POLICY %I ON %I USING (tenant_id = get_current_tenant_id()) WITH CHECK (tenant_id = get_current_tenant_id())',
            p, t);

        p := 'msp_contract_customer_read';
        EXECUTE format('DROP POLICY IF EXISTS %I ON %I', p, t);
        EXECUTE format(
            'CREATE POLICY %I ON %I FOR SELECT USING (customer_tenant_id = get_current_tenant_id())',
            p, t);

        RAISE NOTICE 'rls 012: % enabled (provider_all + customer_read)', t;
    END IF;
END $$;

-- 2) msp_contract_sla_bindings：provider 读写 + customer 只读授信（EXISTS 合同归属）
DO $$
DECLARE
    t text := 'msp_contract_sla_bindings';
    p text;
BEGIN
    IF to_regclass('public.' || quote_ident(t)) IS NULL THEN
        RAISE NOTICE 'rls 012: skip % (table not found)', t;
    ELSE
        EXECUTE format('ALTER TABLE %I ENABLE ROW LEVEL SECURITY', t);

        p := 'msp_contract_binding_provider_all';
        EXECUTE format('DROP POLICY IF EXISTS %I ON %I', p, t);
        EXECUTE format(
            'CREATE POLICY %I ON %I USING (tenant_id = get_current_tenant_id()) WITH CHECK (tenant_id = get_current_tenant_id())',
            p, t);

        p := 'msp_contract_binding_customer_read';
        EXECUTE format('DROP POLICY IF EXISTS %I ON %I', p, t);
        EXECUTE format(
            'CREATE POLICY %I ON %I FOR SELECT USING (EXISTS (' ||
            'SELECT 1 FROM msp_contracts c ' ||
            'WHERE c.id = %I.contract_id ' ||
            'AND c.customer_tenant_id = get_current_tenant_id()))',
            p, t, t);

        RAISE NOTICE 'rls 012: % enabled (provider_all + customer_read)', t;
    END IF;
END $$;

-- 3) 验证语句（可选运行）：
-- SELECT polname, polcmd, pg_get_expr(polqual, polrelid) AS using_expr
-- FROM pg_policy
-- WHERE polrelid IN ('msp_contracts'::regclass, 'msp_contract_sla_bindings'::regclass)
-- ORDER BY polname;
