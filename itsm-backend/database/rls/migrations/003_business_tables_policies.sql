-- =============================================================================
-- RLS Migration 003 — 业务核心表策略扩展（R2 批次 1）
--
-- 背景：002 试点（changes/vectors）验证了「驱动 + 双池分流 + 策略」全链路；
-- 本迁移承接《rls-shadow-observation-2026-10-03》§5 第 6 条「策略扩展（R2）」，
-- 首批覆盖：
--   工单核心：tickets / ticket_comments / ticket_attachments / ticket_ccs /
--             ticket_workflow_records
--   组织与成员：user_tenant_memberships / user_tenant_membership_orgs /
--             groups / projects（departments/teams 已由 009/019 覆盖）
--   工作台：workbench_views
--
-- 策略谓词：统一使用候选函数 get_current_tenant_id()（019 已前向修复为
-- `app.current_tenant` + 异常保护，与 009 旧表同一形态）：
--   USING / WITH CHECK: tenant_id = get_current_tenant_id()
--   —— GUC 缺失/为空/非法 → NULL → 不匹配任何行（fail-closed）。
--
-- 关键取舍（迁移审查记录）：
--   1. 只 ENABLE 不 FORCE：请求路径经低权角色 itsm_app（非表 owner）已强制生效；
--      FORCE 会连 owner 连接（CLI / seed / 迁移工具，当前不注入 GUC）一起约束。
--      在工具链 GUC 化之前，保持与 009 旧表一致的「非 FORCE」约定；
--      changes/vectors 试点表维持既有 FORCE 状态不动。
--   2. 幂等：重复执行安全；环境裁剪导致缺表时跳过并 NOTICE。
--   3. 回滚：003_business_tables_policies_rollback.sql（DROP POLICY + DISABLE，
--      不改动 helper 函数与试点表）。
--
-- 应用方式（无 psql 环境）：
--   go run -tags rlsapply ./cmd/rls-apply -files 003_business_tables_policies.sql
--   go run -tags rlsapply ./cmd/rls-apply -verify
-- =============================================================================

-- 0) 候选函数（前向修复后的权威实现；幂等）
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

-- 1) 批次 1 表清单：ENABLE RLS + tenant_isolation 策略（幂等）
DO $$
DECLARE
    t text;
    p text;
BEGIN
    FOREACH t IN ARRAY ARRAY[
        'tickets',
        'ticket_comments',
        'ticket_attachments',
        'ticket_ccs',
        'ticket_workflow_records',
        'user_tenant_memberships',
        'user_tenant_membership_orgs',
        'groups',
        'projects',
        'workbench_views'
    ]
    LOOP
        IF to_regclass('public.' || quote_ident(t)) IS NULL THEN
            RAISE NOTICE 'rls 003: skip % (table not found)', t;
            CONTINUE;
        END IF;
        EXECUTE format('ALTER TABLE %I ENABLE ROW LEVEL SECURITY', t);
        p := 'tenant_isolation_' || t;
        EXECUTE format('DROP POLICY IF EXISTS %I ON %I', p, t);
        EXECUTE format(
            'CREATE POLICY %I ON %I USING (tenant_id = get_current_tenant_id()) WITH CHECK (tenant_id = get_current_tenant_id())',
            p, t);
        RAISE NOTICE 'rls 003: % enabled (policy %)', t, p;
    END LOOP;
END $$;

-- 2) 验证语句（可选运行）
-- SELECT c.relname, c.relrowsecurity, c.relforcerowsecurity
-- FROM pg_class c JOIN pg_namespace n ON n.oid = c.relnamespace
-- WHERE n.nspname='public' AND c.relname IN ('tickets','ticket_comments','ticket_attachments',
--   'ticket_ccs','ticket_workflow_records','user_tenant_memberships','user_tenant_membership_orgs',
--   'groups','projects','workbench_views')
-- ORDER BY c.relname;
