-- SLA 合同化专项 P1-1 合同模型包 rollback（20261014b_msp_contract_tables_indexes.sql）
--
-- 说明：表与列由 ent Schema.Create 管理（回滚表结构需同步回退 ent schema 定义）；
-- 本文件仅回滚本迁移创建的索引。

DROP INDEX IF EXISTS uk_msp_contracts_tenant_no;
DROP INDEX IF EXISTS idx_msp_contracts_customer_status;
DROP INDEX IF EXISTS uk_msp_contract_bindings_type;
DROP INDEX IF EXISTS idx_msp_contract_bindings_definition;
DROP INDEX IF EXISTS idx_msp_contract_bindings_tenant;
