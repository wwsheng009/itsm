-- 客户主数据 P2 回滚（会删除客户外部标识对账表）

DROP TABLE IF EXISTS customer_external_ids;

DROP INDEX IF EXISTS uq_extid_tenant_id;
DROP INDEX IF EXISTS idx_extid_tenant_customer;
DROP INDEX IF EXISTS uq_extid_tenant_system_external;
