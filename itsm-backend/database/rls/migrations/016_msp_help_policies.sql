-- =============================================================================
-- RLS Migration 016 — MSP 帮助空间与分发授权表策略（P2，方案 §P2）
--
-- 背景：
--   - msp_help_docs：provider 侧帮助空间文档（owner = provider 租户）；
--   - msp_help_distributions：copy-in 分发追踪行（owner = 客户租户，对齐 013 先例：
--     "副本就是普通 customer 租户行"；provider 侧访问经 mspguard 授权后以客户
--     租户作用域重绑定 ctx，零跨租户读）。
--
-- 本迁移（幂等，重复执行安全）：两表 ENABLE RLS + tenant 隔离策略
--   （tenant_id = ctx，fail-closed；与 003–015 一致只 ENABLE 不 FORCE）。
--
-- 应用方式（无 psql 环境）：
--   go run -tags rlsapply ./cmd/rls-apply -files 016_msp_help_policies.sql
--   go run -tags rlsapply ./cmd/rls-apply -verify
-- 回滚：016_msp_help_policies_rollback.sql
-- =============================================================================

-- 0) 候选函数（与 003–015 相同的前向修复实现；幂等）
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

-- 1) msp_help_docs：tenant 隔离（只 ENABLE 不 FORCE）
DO $$
DECLARE
    t text := 'msp_help_docs';
    p text := 'tenant_isolation_msp_help_docs';
BEGIN
    IF to_regclass('public.' || quote_ident(t)) IS NULL THEN
        RAISE NOTICE 'rls 016: skip % (table not found)', t;
    ELSE
        EXECUTE format('ALTER TABLE %I ENABLE ROW LEVEL SECURITY', t);
        EXECUTE format('DROP POLICY IF EXISTS %I ON %I', p, t);
        EXECUTE format(
            'CREATE POLICY %I ON %I USING (tenant_id = get_current_tenant_id()) WITH CHECK (tenant_id = get_current_tenant_id())',
            p, t);
        RAISE NOTICE 'rls 016: % enabled (policy %)', t, p;
    END IF;
END $$;

-- 2) msp_help_distributions：tenant 隔离（行归属客户租户）
DO $$
DECLARE
    t text := 'msp_help_distributions';
    p text := 'tenant_isolation_msp_help_distributions';
BEGIN
    IF to_regclass('public.' || quote_ident(t)) IS NULL THEN
        RAISE NOTICE 'rls 016: skip % (table not found)', t;
    ELSE
        EXECUTE format('ALTER TABLE %I ENABLE ROW LEVEL SECURITY', t);
        EXECUTE format('DROP POLICY IF EXISTS %I ON %I', p, t);
        EXECUTE format(
            'CREATE POLICY %I ON %I USING (tenant_id = get_current_tenant_id()) WITH CHECK (tenant_id = get_current_tenant_id())',
            p, t);
        RAISE NOTICE 'rls 016: % enabled (policy %)', t, p;
    END IF;
END $$;