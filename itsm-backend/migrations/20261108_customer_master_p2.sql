-- 客户主数据 P2（设计文档：docs/customer-master-data/01 §5.6、§7、§8；02 §7、§7.2）
--
-- 范围：
--   1. customer_external_ids（ERP/CRM/OA/外部工单系统对账键）
--   2. 同租户复合外键与索引
--
-- 全部语句可重复执行；不包含顶层事务控制（migration/tx_control_test 守卫）。

CREATE TABLE IF NOT EXISTS customer_external_ids (
  id           BIGSERIAL PRIMARY KEY,
  tenant_id    BIGINT NOT NULL,
  customer_id  BIGINT NOT NULL REFERENCES service_customers(id),
  system_code  VARCHAR(64) NOT NULL,
  external_id  VARCHAR(160) NOT NULL,
  external_url VARCHAR(500),
  created_at   TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE UNIQUE INDEX IF NOT EXISTS uq_extid_tenant_system_external
  ON customer_external_ids (tenant_id, system_code, external_id);

CREATE INDEX IF NOT EXISTS idx_extid_tenant_customer
  ON customer_external_ids (tenant_id, customer_id);

CREATE UNIQUE INDEX IF NOT EXISTS uq_extid_tenant_id
  ON customer_external_ids (tenant_id, id);

DO $$
BEGIN
  IF NOT EXISTS (
    SELECT 1 FROM pg_constraint WHERE conname = 'fk_extid_customer_tenant'
  ) THEN
    ALTER TABLE customer_external_ids
      ADD CONSTRAINT fk_extid_customer_tenant
      FOREIGN KEY (tenant_id, customer_id)
      REFERENCES service_customers (tenant_id, id);
  END IF;
END $$;
