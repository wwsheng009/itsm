-- =============================================================================
-- RLS Migration 010 — 平台面收口（R2 批次 8：bootstrap / marketplace 安装域）
--
-- 承接 009（受管 148 表）。批次 8 纳入 2 张平台-租户交界表：
--   - bootstrap_tokens：租户级一次性首管引导令牌（预认证 HTTP /bootstrap/* 与
--     cmd/initialize CLI 使用）；HTTP 侧已按目标租户显式绑定 ctx
--     （router/bootstrap_routes.go），CLI 侧以 owner 连接执行不受影响；
--   - tenant_installations：市场组件安装记录（service/marketplace 全部按
--     tenantID 显式过滤；本批次补齐 7 个入口的 ctx 重绑定）。
--
-- 平台级 RLS 豁免（本批次不纳管，见 msp-exempt-tables-quarterly-review.md §6）：
--   - users：身份根表。登录/注册/找回/刷新等预认证路径天然跨租户查询；且
--     tenant_id 语义为「home 租户」，切换租户后按 user_id 读取自身身份
--     （RBAC/MSP/GetMe）与 tenant_id 过滤不等价——租户策略会误伤身份读取。
--     补偿控制：user_tenant_memberships 等关系表已 RLS；users 的应用层读写
--     均带 tenant 过滤/自有身份，且本批次维持审计留痕。
--   - roles：平台 RBAC 词表（ADR-0001 / TenantExemptTables 同源语义）；
--     角色解析跨 home/target 作用域，租户策略语义不成立。
--   - permission_definitions：平台级权限字典（tenant_id 全 0/NULL；当前无
--     运行时代码读取）。
--   复核期：2026-12-31（随季度复核），届时评估身份作用域策略（user-scope GUC）。
--
-- 策略谓词：与 003–009 一致（get_current_tenant_id()，fail-closed，只 ENABLE 不 FORCE）。
-- 幂等：重复执行安全；缺表跳过并 NOTICE。
-- 回滚：010_platform_scope_tables_policies_rollback.sql
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
    t text;
    p text;
BEGIN
    FOREACH t IN ARRAY ARRAY[
        'bootstrap_tokens',
        'tenant_installations'
    ]
    LOOP
        IF to_regclass('public.' || quote_ident(t)) IS NULL THEN
            RAISE NOTICE 'rls 010: skip % (table not found)', t;
            CONTINUE;
        END IF;
        EXECUTE format('ALTER TABLE %I ENABLE ROW LEVEL SECURITY', t);
        p := 'tenant_isolation_' || t;
        EXECUTE format('DROP POLICY IF EXISTS %I ON %I', p, t);
        EXECUTE format(
            'CREATE POLICY %I ON %I USING (tenant_id = get_current_tenant_id()) WITH CHECK (tenant_id = get_current_tenant_id())',
            p, t);
        RAISE NOTICE 'rls 010: % enabled (policy %)', t, p;
    END LOOP;
END $$;
