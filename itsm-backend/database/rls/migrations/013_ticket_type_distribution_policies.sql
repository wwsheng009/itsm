-- =============================================================================
-- RLS Migration 013 — 工单类型受控下发追踪表策略（D-TT-1，方案 §4.5a）
--
-- 背景：ticket_type_distributions 为 provider → customer 的类型下发追踪行；
--   行归属 customer 租户（tenant_id=副本租户，对齐 workflow_template_distributions 先例）。
--   §4.5a 决策："零跨租户读——副本就是普通 customer 租户行"：provider 侧访问经
--   mspguard（allocation + 客户 active）授权后，以客户租户作用域重绑定 ctx
--   （应用层约定），不进 RLS 例外通道；跨客户扫描（预览/发布 fan-out）走
--   tenantctx.WithSystemBypass（MSP 管理操作经评审的显式系统上下文）。
--
-- 本迁移（幂等，重复执行安全）：ticket_type_distributions ENABLE RLS +
--   tenant_isolation 策略（tenant_id = ctx，fail-closed；与 003–012 一致只 ENABLE 不 FORCE）。
--
-- 应用方式（无 psql 环境）：
--   go run -tags rlsapply ./cmd/rls-apply -files 013_ticket_type_distribution_policies.sql
-- 回滚：013_ticket_type_distribution_policies_rollback.sql
-- =============================================================================

-- 0) 候选函数（与 003–012 相同的前向修复实现；幂等）
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

-- 1) ticket_type_distributions：tenant 隔离（只 ENABLE 不 FORCE）
DO $$
DECLARE
    t text := 'ticket_type_distributions';
    p text := 'tenant_isolation_ticket_type_distributions';
BEGIN
    IF to_regclass('public.' || quote_ident(t)) IS NULL THEN
        RAISE NOTICE 'rls 013: skip % (table not found)', t;
    ELSE
        EXECUTE format('ALTER TABLE %I ENABLE ROW LEVEL SECURITY', t);
        EXECUTE format('DROP POLICY IF EXISTS %I ON %I', p, t);
        EXECUTE format(
            'CREATE POLICY %I ON %I USING (tenant_id = get_current_tenant_id()) WITH CHECK (tenant_id = get_current_tenant_id())',
            p, t);
        RAISE NOTICE 'rls 013: % enabled (policy %)', t, p;
    END IF;
END $$;
