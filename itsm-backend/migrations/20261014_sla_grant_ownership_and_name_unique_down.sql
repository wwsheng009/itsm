-- SLA 合同化专项 P0-1 模型包 rollback（20261014_sla_grant_ownership_and_name_unique.sql）
--
-- 注意：
--   - 存量重名清洗不可逆（已改名保留 -dup-<id> 后缀作为审计痕迹；如需恢复需人工比对）；
--   - 列回滚会丢失 owner_provider_tenant_id / sla_exempt 数据；且 ent Schema.Create
--     下次启动会按 schema 定义重建列（本回滚供紧急撤销使用，非对称回滚为仓库惯例）。

DROP INDEX IF EXISTS uk_sla_definitions_tenant_name;
DROP INDEX IF EXISTS uk_sla_definition_grants_active;
DROP INDEX IF EXISTS idx_sla_definition_grants_provider;
DROP INDEX IF EXISTS idx_sla_definition_grants_definition;

ALTER TABLE sla_definitions DROP COLUMN IF EXISTS owner_provider_tenant_id;
ALTER TABLE ticket_types DROP COLUMN IF EXISTS sla_exempt;
