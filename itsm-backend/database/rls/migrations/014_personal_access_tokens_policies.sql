-- =============================================================================
-- RLS Migration 014 — 个人访问令牌（PAT）表策略（docs/plan/personal-access-token-plan-2026-10-16.md D-PAT-11）
--
-- 背景：personal_access_tokens 为用户自助凭据行，归属创建时作用域租户（tenant_id）。
--   认证期点查发生在租户上下文建立之前（AuthMiddleware 先于 TenantMiddleware），
--   走带审计的 tenantctx.SystemContext（RLS enforce 驱动对无租户上下文语句 fail-closed，
--   system bypass 是唯一豁免；先例：bootstrap_tokens，RLS 010）。
--   管理面查询使用正常租户上下文 + user_id 过滤。
--
-- 本迁移（幂等，重复执行安全）：personal_access_tokens ENABLE RLS +
--   tenant_isolation 策略（tenant_id = ctx，fail-closed；与 003–013 一致只 ENABLE 不 FORCE）。
--
-- 应用方式（无 psql 环境）：
--   go run -tags rlsapply ./cmd/rls-apply -files 014_personal_access_tokens_policies.sql
-- 回滚：014_personal_access_tokens_policies_rollback.sql
-- =============================================================================

-- 0) 候选函数（与 003–013 相同的前向修复实现；幂等）
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

-- 1) personal_access_tokens：tenant 隔离（只 ENABLE 不 FORCE）
DO $$
DECLARE
    t text := 'personal_access_tokens';
    p text := 'tenant_isolation_personal_access_tokens';
BEGIN
    IF to_regclass('public.' || quote_ident(t)) IS NULL THEN
        RAISE NOTICE 'rls 014: skip % (table not found)', t;
    ELSE
        EXECUTE format('ALTER TABLE %I ENABLE ROW LEVEL SECURITY', t);
        EXECUTE format('DROP POLICY IF EXISTS %I ON %I', p, t);
        EXECUTE format(
            'CREATE POLICY %I ON %I USING (tenant_id = get_current_tenant_id()) WITH CHECK (tenant_id = get_current_tenant_id())',
            p, t);
        RAISE NOTICE 'rls 014: % enabled (policy %)', t, p;
    END IF;
END $$;
