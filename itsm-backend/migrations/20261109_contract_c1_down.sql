-- 合同建模 C1 回滚（会删除 C1 新表；support_contracts 新增字段保留兼容，不自动 DROP COLUMN）

DROP TABLE IF EXISTS contract_project_links;
DROP TABLE IF EXISTS contract_usage_ledger;
DROP TABLE IF EXISTS contract_amendments;
DROP TABLE IF EXISTS contract_service_items;
