-- =============================================================================
-- RLS Migration 006 — 流程/工作流/审批引擎域策略扩展（R2 批次 4）
--
-- 背景：005 落地后 enforce 演练 70/70 且 missing_tenant=0；本迁移承接
-- 《rls-shadow-observation-2026-10-03》§5 第 8 条「其余 ~50 张租户表按批次推进」，
-- 批次 4 覆盖「流程引擎（process_*）、工作流（workflow_*）、BPMN 权限（bpmn_permissions）」
-- 域（14 表）：
--   流程引擎：process_instances / process_tasks / process_audit_logs / process_variables /
--             process_timers / process_version_changelogs / process_approval_decisions /
--             process_execution_histories
--   工作流：  workflow_instances / workflow_tasks / workflow_templates / workflow_versions /
--             workflows
--   权限：    bpmn_permissions
--
-- 策略谓词：与 003/004/005 一致，统一 get_current_tenant_id()（fail-closed）。
--   USING / WITH CHECK: tenant_id = get_current_tenant_id()
--
-- 关键取舍（迁移审查记录）：
--   1. 只 ENABLE 不 FORCE：与 003/004/005/009 约定一致；请求路径经低权角色 itsm_app 强制生效。
--      CLI/seed/迁移工具（owner 连接）不受约束，待工具链 GUC 化后统一评估 FORCE。
--   2. 租户上下文来源核查（enforce 前置）：
--      - HTTP 路径：TenantMiddleware/RBACMiddleware 已把 tenant_id 注入 request.Context
--        （middleware/tenant.go、rbac.go），BPMN 处理器再叠加 BPMN 专用 key（handlers/bpmn）；
--      - 事务性触发：ticket/incident 的 workflow outbox 经 commandbus 按命令租户注入 ctx
--        （批次 2 已实证：commandbus.WithTenantID 模式）；
--      - 超时扫描：service/bpmn_timeout_scanner.go 显式同时注入 tenantctx 与 BPMN key；
--      - 非请求面：router/ga_readiness.go 的 classifyWorkflowTasks 属平台级诊断，
--        本批次同 PR 改为 system 作用域（tenantctx.WithSystemBypass），避免被策略收窄。
--   3. process_instances / process_tasks / process_audit_logs 为高活跃写表（联调库 148/151/151 行），
--      本批次探针覆盖正例；workflow_templates 6 行；其余空表按 fail-closed 验证。
--   4. 幂等：重复执行安全；缺表跳过并 NOTICE。
--   5. 回滚：006_process_workflow_tables_policies_rollback.sql（DROP POLICY + DISABLE）。
--
-- 应用方式（无 psql 环境）：
--   go run -tags rlsapply ./cmd/rls-apply -files 006_process_workflow_tables_policies.sql
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

-- 1) 批次 4 表清单：ENABLE RLS + tenant_isolation 策略（幂等）
DO $$
DECLARE
    t text;
    p text;
BEGIN
    FOREACH t IN ARRAY ARRAY[
        -- 流程引擎
        'process_instances',
        'process_tasks',
        'process_audit_logs',
        'process_variables',
        'process_timers',
        'process_version_changelogs',
        'process_approval_decisions',
        'process_execution_histories',
        -- 工作流
        'workflow_instances',
        'workflow_tasks',
        'workflow_templates',
        'workflow_versions',
        'workflows',
        -- BPMN 权限
        'bpmn_permissions'
    ]
    LOOP
        IF to_regclass('public.' || quote_ident(t)) IS NULL THEN
            RAISE NOTICE 'rls 006: skip % (table not found)', t;
            CONTINUE;
        END IF;
        EXECUTE format('ALTER TABLE %I ENABLE ROW LEVEL SECURITY', t);
        p := 'tenant_isolation_' || t;
        EXECUTE format('DROP POLICY IF EXISTS %I ON %I', p, t);
        EXECUTE format(
            'CREATE POLICY %I ON %I USING (tenant_id = get_current_tenant_id()) WITH CHECK (tenant_id = get_current_tenant_id())',
            p, t);
        RAISE NOTICE 'rls 006: % enabled (policy %)', t, p;
    END LOOP;
END $$;

-- 2) 验证语句（可选运行）
-- SELECT c.relname, c.relrowsecurity, c.relforcerowsecurity
-- FROM pg_class c JOIN pg_namespace n ON n.oid = c.relnamespace
-- WHERE n.nspname='public' AND c.relname IN ('process_instances','process_tasks',
--   'process_audit_logs','process_variables','process_timers','process_version_changelogs',
--   'process_approval_decisions','process_execution_histories',
--   'workflow_instances','workflow_tasks','workflow_templates','workflow_versions',
--   'workflows','bpmn_permissions')
-- ORDER BY c.relname;
