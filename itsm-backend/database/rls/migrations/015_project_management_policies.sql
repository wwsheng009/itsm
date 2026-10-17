-- =============================================================================
-- RLS Migration 015 — 项目管理 P0 表策略（PE-1）
--
-- 背景：project_members / project_tasks 为租户内项目管理实体（P0）；
--   P1 表（project_task_dependencies / project_milestones / project_baselines）
--   已列入覆盖清单，表创建后自动获得策略（缺失时 NOTICE 跳过，幂等）。
--   行归属租户（tenant_id），与 003 先例一致：只 ENABLE 不 FORCE，
--   策略谓词 tenant_id = get_current_tenant_id()（GUC 缺失 fail-closed）。
--
-- 应用方式（无 psql 环境）：
--   go run -tags rlsapply ./cmd/rls-apply -files 015_project_management_policies.sql
-- 回滚：015_project_management_policies_rollback.sql
-- =============================================================================

-- 0) 候选函数（与 003–014 相同的前向修复实现；幂等）
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

-- 1) 项目管理表：tenant 隔离（只 ENABLE 不 FORCE）
DO $$
DECLARE
    t text;
    p text;
BEGIN
    FOREACH t IN ARRAY ARRAY[
        'project_members',
        'project_tasks',
        'project_task_dependencies',
        'project_milestones',
        'project_baselines',
        'project_templates',
        'project_gates',
        'project_closures',
        'project_report_templates',
        'project_risks',
        'project_asset_links'
    ]
    LOOP
        IF to_regclass('public.' || quote_ident(t)) IS NULL THEN
            RAISE NOTICE 'rls 015: skip % (table not found)', t;
            CONTINUE;
        END IF;
        p := 'tenant_isolation_' || t;
        EXECUTE format('ALTER TABLE %I ENABLE ROW LEVEL SECURITY', t);
        EXECUTE format('DROP POLICY IF EXISTS %I ON %I', p, t);
        EXECUTE format(
            'CREATE POLICY %I ON %I USING (tenant_id = get_current_tenant_id()) WITH CHECK (tenant_id = get_current_tenant_id())',
            p, t);
        RAISE NOTICE 'rls 015: % enabled (policy %)', t, p;
    END LOOP;
END $$;