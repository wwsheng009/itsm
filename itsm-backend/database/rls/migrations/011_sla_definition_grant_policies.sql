-- =============================================================================
-- RLS Migration 011 — SLA 定义下发授权通道（D-SLA-1，方案 Accepted v1.11 §4.1/§4.1b）
--
-- 背景：sla_definition_grants 为 provider → customer 的 SLA 定义下发授权表；
--   授权行归属 provider 租户（tenant_id = provider）；customer 端只读
--   （经本迁移登记的显式授信通道），写路径保持 provider-only。
--
-- 本迁移做三件事（幂等，重复执行安全）：
--   1) sla_definition_grants：ENABLE RLS + 两条策略：
--      - sla_grant_provider_all      FOR ALL    tenant_id = ctx         （provider 读写）
--      - sla_grant_customer_read     FOR SELECT customer_tenant_id = ctx（customer 只读授信）
--      （多策略为 OR 语义：customer ctx 仅获得 SELECT，INSERT/UPDATE/DELETE 仍被
--        provider 策略的 USING/WITH CHECK 限住——四层写保护之 DB 层，方案 §4.1）
--   2) sla_definitions 追加授信可见策略（v1.9 RLS 通道具体形态）：
--      - sla_definition_customer_read FOR SELECT USING (EXISTS(授权行))
--      仅放宽 SELECT 路径；既有 tenant_isolation_sla_definitions（FOR ALL）
--      与其 WITH CHECK 原样保留，写路径不变。
--   3) 复核语句（注释）供人工验证。
--
-- 关键取舍：与 003–010 一致——只 ENABLE 不 FORCE；请求路径经低权角色 itsm_app 生效；
--   谓词统一 get_current_tenant_id()（fail-closed，NULL 上下文不匹配任何行）。
--
-- 应用方式（无 psql 环境）：
--   go run -tags rlsapply ./cmd/rls-apply -files 011_sla_definition_grant_policies.sql
--   go run -tags rlsapply ./cmd/rls-apply -verify
-- 回滚：011_sla_definition_grant_policies_rollback.sql
-- =============================================================================

-- 0) 候选函数（与 003–010 相同的前向修复实现；幂等）
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

-- 1) sla_definition_grants：provider 读写 + customer 只读授信
DO $$
DECLARE
    t text := 'sla_definition_grants';
    p text;
BEGIN
    IF to_regclass('public.' || quote_ident(t)) IS NULL THEN
        RAISE NOTICE 'rls 011: skip % (table not found)', t;
    ELSE
        EXECUTE format('ALTER TABLE %I ENABLE ROW LEVEL SECURITY', t);

        -- provider 全量读写（授权是 provider 的资产）
        p := 'sla_grant_provider_all';
        EXECUTE format('DROP POLICY IF EXISTS %I ON %I', p, t);
        EXECUTE format(
            'CREATE POLICY %I ON %I USING (tenant_id = get_current_tenant_id()) WITH CHECK (tenant_id = get_current_tenant_id())',
            p, t);

        -- customer 只读授信（匹配 L1 与 provided 端点；不放宽任何写路径）
        p := 'sla_grant_customer_read';
        EXECUTE format('DROP POLICY IF EXISTS %I ON %I', p, t);
        EXECUTE format(
            'CREATE POLICY %I ON %I FOR SELECT USING (customer_tenant_id = get_current_tenant_id())',
            p, t);

        RAISE NOTICE 'rls 011: % enabled (policies: % + customer_read)', t, 'sla_grant_provider_all';
    END IF;
END $$;

-- 2) sla_definitions：追加授信可见策略（仅 SELECT 放宽；既有策略与写路径不变）
DO $$
DECLARE
    t text := 'sla_definitions';
    p text;
BEGIN
    IF to_regclass('public.' || quote_ident(t)) IS NULL THEN
        RAISE NOTICE 'rls 011: skip % (table not found)', t;
    ELSE
        p := 'sla_definition_customer_read';
        EXECUTE format('DROP POLICY IF EXISTS %I ON %I', p, t);
        EXECUTE format(
            'CREATE POLICY %I ON %I FOR SELECT USING (EXISTS (' ||
            'SELECT 1 FROM sla_definition_grants g ' ||
            'WHERE g.sla_definition_id = %I.id ' ||
            'AND g.customer_tenant_id = get_current_tenant_id() ' ||
            'AND g.is_active))',
            p, t, t);
        RAISE NOTICE 'rls 011: % customer read channel enabled (policy %)', t, p;
    END IF;
END $$;

-- 3) 验证语句（可选运行）：
-- SELECT c.relname, c.relrowsecurity, c.relforcerowsecurity
-- FROM pg_class c JOIN pg_namespace n ON n.oid = c.relnamespace
-- WHERE n.nspname='public' AND c.relname IN ('sla_definition_grants','sla_definitions')
-- ORDER BY c.relname;
-- SELECT polname, polcmd, pg_get_expr(polqual, polrelid) AS using_expr
-- FROM pg_policy
-- WHERE polrelid IN ('sla_definition_grants'::regclass, 'sla_definitions'::regclass)
-- ORDER BY polname;
