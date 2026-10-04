-- =============================================================================
-- RLS Migration 008 — 自动化/机器人与运维命令域策略扩展（R2 批次 6）
--
-- 承接 007（受管 64 表）。批次 6 覆盖「bot 运行/事件/产物、AI 结果与反馈、
-- LLM 个人默认、运维命令 outbox」共 10 表：
--   bot_runs / bot_steps / bot_events / bot_artifacts
--   bot_templates / bot_tool_grants
--   ai_analysis_results / ai_feedbacks / llm_user_preferences
--   operational_commands（commandbus 持久化 outbox）
--
-- 策略谓词：与 003–007 一致，统一 get_current_tenant_id()（fail-closed）。
--   USING / WITH CHECK: tenant_id = get_current_tenant_id()
--
-- 关键取舍（迁移审查记录）：
--   1. 只 ENABLE 不 FORCE：与既往批次约定一致；CLI/seed/迁移工具（owner 连接）不受约束。
--   2. 写路径核查（enforce 前置）：
--      - bot 运行面（RunStore StartRun/AppendStep/AppendEvent、ArtifactStore Create/Get/List）
--        本批次修复：以 in.TenantID / tenantID 显式重绑定 ctx，覆盖入口异步触发场景；
--      - bot 模板/授权管理（service/bot/admin.go）与 seeder/provision CLI 均携带明确
--        tenantID（管理台请求 ctx / owner 连接），不受阻；
--      - ai_feedbacks / llm_user_preferences：写入走请求 ctx（反馈按钮 / 个人默认 API），
--        raw SQL 仓库经 RLS 驱动按 ctx 路由；ai_analysis_results 保存为请求 ctx；
--      - operational_commands：写入来自 commandbus Enqueue/EnqueueTx（租户请求事务或
--        BPMN handler —— handler ctx 由 commandbus 按命令租户注入）；worker 的领取/心跳/
--        就绪检查已使用 tenantctx.SystemContext（commandbus.go），enforce-ready。
--   3. 平台级豁免登记：ai_llm_calls 明确无 tenant_id（LLM 网关平台级时延指标），
--      不在本批次；与 permission_definitions 一同纳入「平台级保留项」清单。
--   4. 幂等：重复执行安全；缺表跳过并 NOTICE。
--   5. 回滚：008_automation_tables_policies_rollback.sql（DROP POLICY + DISABLE）。
--
-- 应用方式（无 psql 环境）：
--   go run -tags rlsapply ./cmd/rls-apply -files 008_automation_tables_policies.sql
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

-- 1) 批次 6 表清单：ENABLE RLS + tenant_isolation 策略（幂等）
DO $$
DECLARE
    t text;
    p text;
BEGIN
    FOREACH t IN ARRAY ARRAY[
        -- bot 运行与产物
        'bot_runs',
        'bot_steps',
        'bot_events',
        'bot_artifacts',
        -- bot 配置
        'bot_templates',
        'bot_tool_grants',
        -- AI 结果/反馈/个人默认
        'ai_analysis_results',
        'ai_feedbacks',
        'llm_user_preferences',
        -- 运维命令 outbox
        'operational_commands'
    ]
    LOOP
        IF to_regclass('public.' || quote_ident(t)) IS NULL THEN
            RAISE NOTICE 'rls 008: skip % (table not found)', t;
            CONTINUE;
        END IF;
        EXECUTE format('ALTER TABLE %I ENABLE ROW LEVEL SECURITY', t);
        p := 'tenant_isolation_' || t;
        EXECUTE format('DROP POLICY IF EXISTS %I ON %I', p, t);
        EXECUTE format(
            'CREATE POLICY %I ON %I USING (tenant_id = get_current_tenant_id()) WITH CHECK (tenant_id = get_current_tenant_id())',
            p, t);
        RAISE NOTICE 'rls 008: % enabled (policy %)', t, p;
    END LOOP;
END $$;
