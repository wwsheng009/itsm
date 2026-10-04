-- =============================================================================
-- RLS Migration 007 — 鉴权/菜单/配置/审计域策略扩展（R2 批次 5）
--
-- 背景：006 落地后 enforce 演练 70/70 且 missing_tenant=0；本迁移承接
-- 《rls-shadow-observation-2026-10-03》§5 第 9 条「其余 72 张租户表按批次推进」，
-- 批次 5 覆盖「RBAC 关联、权限目录、菜单、系统配置、审计日志、端点 ACL」核心表
-- （6 表）：
--   role_permissions（角色-权限关联，租户级）
--   permissions（权限目录，租户级）
--   menus（菜单，租户级）
--   system_configs（租户配置/能力开关/密码策略）
--   audit_logs（审计日志；含 4 行 tenant NULL/0 的系统/历史行）
--   endpoint_acls（端点级 ACL）
--
-- 策略谓词：与 003–006 一致，统一 get_current_tenant_id()（fail-closed）。
--   USING / WITH CHECK: tenant_id = get_current_tenant_id()
--
-- 关键取舍（迁移审查记录）：
--   1. 只 ENABLE 不 FORCE：与 003–006/009 约定一致；CLI/seed/迁移工具（owner 连接）
--      不受约束，待工具链 GUC 化后统一评估 FORCE。
--   2. 写路径核查（enforce 前置）：
--      - seeder（启动播种）与 `cmd/provision_tenant`（租户基线克隆）均走 owner/管理池，
--        不受策略约束；
--      - role_service/menu_service/system_config_service 写路径按本租户过滤，请求 ctx
--        由 Tenant/RBAC 中间件注入租户作用域；
--      - audit_logs 写入侧（middleware/audit.go 三处）已按「已知租户注入 / 未知 system
--        bypass」收口（批次 3 修复链），登录预认证路径不受阻；
--      - **本批次修复**：capability.ConfigSource 的 load/Update/Clear 以目标 tenantID
--        重绑定 ctx（平台管理员管理指定租户覆盖行时，原 ctx 未必等于目标租户）；
--      - **本批次修复**：router/ga_readiness countOrZero（平台级就绪诊断跨租户计数）
--        显式 system 作用域。
--   3. audit_logs 存量 4 行 tenant_id NULL/0（系统/历史）：对任何租户会话不可见
--      （fail-closed），system 作用域仍可审计；不阻塞本批次。
--   4. 幂等：重复执行安全；缺表跳过并 NOTICE。
--   5. 回滚：007_authz_audit_tables_policies_rollback.sql（DROP POLICY + DISABLE）。
--
-- 应用方式（无 psql 环境）：
--   go run -tags rlsapply ./cmd/rls-apply -files 007_authz_audit_tables_policies.sql
--   go run -tags rlsapply ./cmd/rls-apply -verify
-- =============================================================================

-- 0) 候选函数（前向修复实现；幂等）
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

-- 1) 批次 5 表清单：ENABLE RLS + tenant_isolation 策略（幂等）
DO $$
DECLARE
    t text;
    p text;
BEGIN
    FOREACH t IN ARRAY ARRAY[
        -- RBAC 关联与权限目录
        'role_permissions',
        'permissions',
        -- 菜单
        'menus',
        -- 系统配置（含密码策略/能力开关）
        'system_configs',
        -- 审计日志（NULL/0 租户行对租户会话 fail-closed）
        'audit_logs',
        -- 端点 ACL
        'endpoint_acls'
    ]
    LOOP
        IF to_regclass('public.' || quote_ident(t)) IS NULL THEN
            RAISE NOTICE 'rls 007: skip % (table not found)', t;
            CONTINUE;
        END IF;
        EXECUTE format('ALTER TABLE %I ENABLE ROW LEVEL SECURITY', t);
        p := 'tenant_isolation_' || t;
        EXECUTE format('DROP POLICY IF EXISTS %I ON %I', p, t);
        EXECUTE format(
            'CREATE POLICY %I ON %I USING (tenant_id = get_current_tenant_id()) WITH CHECK (tenant_id = get_current_tenant_id())',
            p, t);
        RAISE NOTICE 'rls 007: % enabled (policy %)', t, p;
    END LOOP;
END $$;

-- 2) 验证语句（可选运行）
-- SELECT c.relname, c.relrowsecurity, c.relforcerowsecurity
-- FROM pg_class c JOIN pg_namespace n ON n.oid = c.relnamespace
-- WHERE n.nspname='public' AND c.relname IN ('role_permissions','permissions','menus',
--   'system_configs','audit_logs','endpoint_acls')
-- ORDER BY c.relname;
