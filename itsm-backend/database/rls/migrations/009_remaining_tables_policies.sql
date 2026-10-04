-- =============================================================================
-- RLS Migration 009 — 长尾业务域策略扩展（R2 批次 7）
--
-- 承接 008（受管 74 表）。批次 7 覆盖 CMDB/资产/云、变更扩展、审批链与工单审批、
-- 事件规则引擎、问题/发布、组织/合同/值班、工单配置视图标签、调查、SLA 策略、
-- 服务目录、BPMN 定义/部署/绑定、AI Provider 配置、通用附件与遗留别名表，
-- 共 74 张现有 tenant_id 表（数据分布：13 张有数据、其余为空表 fail-closed）。
--
-- 策略谓词：与 003–008 一致，统一 get_current_tenant_id()（fail-closed）。
--
-- 关键取舍（迁移审查记录）：
--   1. 只 ENABLE 不 FORCE：与既往批次一致；owner 连接（CLI/seed/迁移工具）不受约束。
--   2. 写路径核查（enforce 前置，聚焦有数据表）：
--      - attachments(82)：通用附件服务 Create 请求 ctx（上传 API），显式 tenant_id；
--      - ci_types(40)/departments(15)/teams(19)/tags(15)/ticket_categories(14)/
--        service_catalogs(27)/ticket_views(5)/standard_changes(5)/sla_policies(6)/
--        sla_alert_rules(8)：管理台请求 ctx CRUD；只读诊断经 ga_readiness countOrZero
--        （已于批次 4 改 system 作用域）；
--      - approval_workflows(15)/approval_chains/approval_records/ticket_approvals：
--        审批服务（service_request/BPMN）请求 ctx 或命令租户 ctx；
--      - incident_events(6) 及 incident_rules 家族：事件服务/规则引擎请求 ctx；
--        incident_escalation_service.QueryEscalationRules 全量查询在 RLS 下按 ctx 收窄
--        （租户会话只见本租户），语义兼容；
--      - process_definitions(95)/process_deployments(90)/process_bindings(80)：
--        bpmn_process_definition_service 全部显式 tenantID + requireBPMNTenantContext；
--        ga_readiness 绑定计数已 system；
--      - llm_provider_configs(2)：请求 ctx（管理 API/解析链）；本批次修复——
--        registry loadSnapshot/定向查询按目标租户重绑定 ctx，
--        CountUsableLLMProviderInstances 启动探针显式 system 作用域。
--   3. 平台级保留（本批次不含，待专门语义批次）：users / roles（登录前全局查询与
--      tenant guard）、permission_definitions（平台词表，全 0/NULL）、
--      tenant_installations / bootstrap_tokens（平台安装与引导令牌）。
--   4. 幂等：重复执行安全；缺表跳过并 NOTICE。
--   5. 回滚：009_remaining_tables_policies_rollback.sql（DROP POLICY + DISABLE）。
--
-- 应用方式（无 psql 环境）：
--   go run -tags rlsapply ./cmd/rls-apply -files 009_remaining_tables_policies.sql
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

-- 1) 批次 7 表清单：ENABLE RLS + tenant_isolation 策略（幂等）
DO $$
DECLARE
    t text;
    p text;
BEGIN
    FOREACH t IN ARRAY ARRAY[
        -- CMDB / CI / 云 / 发现 / 资产
        'configuration_items', 'configuration_item_histories', 'ci_attribute_definitions',
        'ci_relationships', 'ci_tags', 'ci_types', 'applications', 'microservices',
        'cloud_accounts', 'cloud_resources', 'cloud_services',
        'discovery_jobs', 'discovery_results', 'discovery_sources',
        'cmdb_export_tasks', 'cmdb_import_tasks', 'cmdb_identity_migration_conflicts',
        'cmdb_saved_views', 'assets', 'asset_licenses',
        -- 变更扩展
        'change_approval_chains', 'change_approvals', 'change_implementation_plans',
        'change_pi_rs', 'change_risk_assessments', 'change_rollback_executions',
        'change_rollback_plans', 'standard_changes', 'cab_members',
        -- 审批链与工单审批
        'approval_workflows', 'approval_chains', 'approval_records', 'ticket_approvals',
        -- 事件规则引擎
        'incident_rules', 'incident_escalation_rules', 'incident_rule_executions',
        'incident_metrics', 'incident_events',
        -- 问题 / 发布
        'problems', 'root_cause_analyses', 'known_errors', 'releases',
        -- 组织 / 合同 / 值班
        'teams', 'departments', 'customer_branches', 'source_organizations',
        'relationship_types', 'service_customers', 'contracts', 'support_contracts',
        'external_contract_references', 'vendors', 'on_call_schedules', 'on_call_shifts',
        'engineer_skills',
        -- 工单配置 / 视图 / 标签
        'ticket_categories', 'ticket_assignment_rules', 'ticket_automation_rules',
        'ticket_views', 'ticket_tags', 'tags',
        -- 调查
        'surveys', 'survey_responses',
        -- SLA 策略 / 告警规则
        'sla_policies', 'sla_alert_rules',
        -- 服务目录
        'service_catalogs',
        -- BPMN 定义 / 部署 / 绑定
        'process_definitions', 'process_deployments', 'process_bindings',
        -- AI Provider 配置
        'llm_provider_configs',
        -- 通用附件
        'attachments',
        -- 遗留别名表
        'alerts', 'ai_analysis_result', 'endpoint_ac_ls'
    ]
    LOOP
        IF to_regclass('public.' || quote_ident(t)) IS NULL THEN
            RAISE NOTICE 'rls 009: skip % (table not found)', t;
            CONTINUE;
        END IF;
        EXECUTE format('ALTER TABLE %I ENABLE ROW LEVEL SECURITY', t);
        p := 'tenant_isolation_' || t;
        EXECUTE format('DROP POLICY IF EXISTS %I ON %I', p, t);
        EXECUTE format(
            'CREATE POLICY %I ON %I USING (tenant_id = get_current_tenant_id()) WITH CHECK (tenant_id = get_current_tenant_id())',
            p, t);
        RAISE NOTICE 'rls 009: % enabled (policy %)', t, p;
    END LOOP;
END $$;
