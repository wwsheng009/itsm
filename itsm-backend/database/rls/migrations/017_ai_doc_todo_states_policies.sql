-- =============================================================================
-- RLS Migration 017 — 文档待办状态表策略（H1-01/H1-03/H1-04 条件项闭环）
--
-- 背景：
--   - ai_doc_todo_states：租户内「文档待办认领/忽略」状态（owner = 租户，
--     pageKey 维度唯一；待办内容本身为派生数据不入库）。
--
-- 本迁移（幂等，重复执行安全）：ENABLE RLS + tenant 隔离策略
--   （tenant_id = ctx，fail-closed；与 003–016 一致只 ENABLE 不 FORCE）。
--
-- 应用方式（无 psql 环境）：
--   go run -tags rlsapply ./cmd/rls-apply -files 017_ai_doc_todo_states_policies.sql
--   go run -tags rlsapply ./cmd/rls-apply -verify
-- 回滚：017_ai_doc_todo_states_policies_rollback.sql
-- =============================================================================

-- 0) 候选函数（与 003–016 相同的前向修复实现；幂等）
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

-- 1) ai_doc_todo_states：tenant 隔离（只 ENABLE 不 FORCE）
DO $$
DECLARE
    t text := 'ai_doc_todo_states';
    p text := 'tenant_isolation_ai_doc_todo_states';
BEGIN
    IF to_regclass('public.' || quote_ident(t)) IS NULL THEN
        RAISE NOTICE 'rls 017: skip % (table not found)', t;
    ELSE
        EXECUTE format('ALTER TABLE %I ENABLE ROW LEVEL SECURITY', t);
        EXECUTE format('DROP POLICY IF EXISTS %I ON %I', p, t);
        EXECUTE format(
            'CREATE POLICY %I ON %I USING (tenant_id = get_current_tenant_id()) WITH CHECK (tenant_id = get_current_tenant_id())',
            p, t);
        RAISE NOTICE 'rls 017: % enabled (policy %)', t, p;
    END IF;
END $$;