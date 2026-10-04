-- =============================================================================
-- RLS Migration 009 回滚 — 移除批次 7（长尾业务域 74 表）策略
-- 注意：回滚只影响本批次表；003–008/010 的策略不受影响。
-- =============================================================================
DO $$
DECLARE
    t text;
BEGIN
    FOREACH t IN ARRAY ARRAY[
        'configuration_items', 'configuration_item_histories', 'ci_attribute_definitions',
        'ci_relationships', 'ci_tags', 'ci_types', 'applications', 'microservices',
        'cloud_accounts', 'cloud_resources', 'cloud_services',
        'discovery_jobs', 'discovery_results', 'discovery_sources',
        'cmdb_export_tasks', 'cmdb_import_tasks', 'cmdb_identity_migration_conflicts',
        'cmdb_saved_views', 'assets', 'asset_licenses',
        'change_approval_chains', 'change_approvals', 'change_implementation_plans',
        'change_pi_rs', 'change_risk_assessments', 'change_rollback_executions',
        'change_rollback_plans', 'standard_changes', 'cab_members',
        'approval_workflows', 'approval_chains', 'approval_records', 'ticket_approvals',
        'incident_rules', 'incident_escalation_rules', 'incident_rule_executions',
        'incident_metrics', 'incident_events',
        'problems', 'root_cause_analyses', 'known_errors', 'releases',
        'teams', 'departments', 'customer_branches', 'source_organizations',
        'relationship_types', 'service_customers', 'contracts', 'support_contracts',
        'external_contract_references', 'vendors', 'on_call_schedules', 'on_call_shifts',
        'engineer_skills',
        'ticket_categories', 'ticket_assignment_rules', 'ticket_automation_rules',
        'ticket_views', 'ticket_tags', 'tags',
        'surveys', 'survey_responses',
        'sla_policies', 'sla_alert_rules',
        'service_catalogs',
        'process_definitions', 'process_deployments', 'process_bindings',
        'llm_provider_configs',
        'attachments',
        'alerts', 'ai_analysis_result', 'endpoint_ac_ls'
    ]
    LOOP
        IF to_regclass('public.' || quote_ident(t)) IS NULL THEN
            CONTINUE;
        END IF;
        EXECUTE format('DROP POLICY IF EXISTS %I ON %I', 'tenant_isolation_' || t, t);
        EXECUTE format('ALTER TABLE %I NO FORCE ROW LEVEL SECURITY', t);
        EXECUTE format('ALTER TABLE %I DISABLE ROW LEVEL SECURITY', t);
        RAISE NOTICE 'rls 009 rollback: % policy dropped', t;
    END LOOP;
END $$;
