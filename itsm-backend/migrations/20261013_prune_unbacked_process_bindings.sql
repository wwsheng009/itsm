-- =============================================================================
-- Prune unbacked demo process bindings (OB-11 / F8)
-- =============================================================================
-- 背景（2026-10-12，238 全新库生产部署实测，见
--   docs/multi-tenant/evidence/238-deploy-20261012/run-summary.md §F8）：
--   历史迁移 20260620_process_routing_enhancement.sql 的示例路由绑定引用了内置 BPMN
--   模板目录（itsm-backend/service/bpmn，go:embed）中不存在的 4 个流程定义 key：
--     change_emergency_flow / incident_general_flow / release_test_flow / expense_approval_flow
--   → 全新库 init 在 workflow-core 校验（verifyWorkflowTemplates：绑定必须指向存在的
--   流程定义）失败 → workflow-core 事务回滚 → compose 依赖链中止（站点 502）。
--
-- 修复：前向迁移删除这些无法解析的示例绑定（幂等；已有环境若无此类行为 no-op）。
--   不改历史迁移文件（避免 schema_migrations checksum 漂移，见 migration/migrator.go:107）。
--   产品规范绑定由 seeder（config/seed）生成，均指向真实存在的模板。
-- =============================================================================

DELETE FROM process_bindings
WHERE process_definition_key IN (
    'change_emergency_flow',
    'incident_general_flow',
    'release_test_flow',
    'expense_approval_flow'
);
