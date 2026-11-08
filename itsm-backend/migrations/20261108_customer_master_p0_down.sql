-- 客户主数据 P0 回滚（仅用于明确回滚；会删除 P0 新增字段与两张关联表）

ALTER TABLE support_contracts
  DROP CONSTRAINT IF EXISTS fk_contract_branch_tenant_customer;
ALTER TABLE support_contracts
  DROP CONSTRAINT IF EXISTS fk_sc_customer_tenant;

DROP TABLE IF EXISTS customer_contacts;
DROP TABLE IF EXISTS customer_tenant_links;

DROP INDEX IF EXISTS uq_ctlink_tenant_id;
DROP INDEX IF EXISTS idx_ctlink_tenant_linked;
DROP INDEX IF EXISTS uq_ctlink_billing;
DROP INDEX IF EXISTS uq_ctlink_linked_tenant;
DROP INDEX IF EXISTS uq_ctlink_primary;
DROP INDEX IF EXISTS uq_ctlink;
DROP INDEX IF EXISTS uq_contact_tenant_id;
DROP INDEX IF EXISTS idx_contact_tenant_email;
DROP INDEX IF EXISTS idx_contact_tenant_customer;
DROP INDEX IF EXISTS uq_contact_primary;
DROP INDEX IF EXISTS uq_contact_email;

DROP INDEX IF EXISTS uq_branch_tenant_customer_id;
DROP INDEX IF EXISTS uq_branch_tenant_id;
DROP INDEX IF EXISTS uq_branch_primary;
DROP INDEX IF EXISTS uq_branch_tenant_code;
DROP INDEX IF EXISTS uq_branch_tenant_name;

ALTER TABLE customer_branches
  DROP COLUMN IF EXISTS deleted_at,
  DROP COLUMN IF EXISTS linked_tenant_id,
  DROP COLUMN IF EXISTS is_primary,
  DROP COLUMN IF EXISTS support_hours,
  DROP COLUMN IF EXISTS service_tier,
  DROP COLUMN IF EXISTS email_domains,
  DROP COLUMN IF EXISTS contact_phone,
  DROP COLUMN IF EXISTS timezone,
  DROP COLUMN IF EXISTS address,
  DROP COLUMN IF EXISTS branch_type,
  DROP COLUMN IF EXISTS code;

ALTER TABLE customer_branches
  ADD CONSTRAINT customer_branches_tenant_id_customer_id_normalized_name_key
  UNIQUE (tenant_id, customer_id, normalized_name);

DROP INDEX IF EXISTS idx_customer_tenant_am;
DROP INDEX IF EXISTS idx_customer_tenant_stage;
DROP INDEX IF EXISTS idx_customer_tenant_tier;
DROP INDEX IF EXISTS uq_customer_tenant_id;
DROP INDEX IF EXISTS uq_customer_tenant_taxid;
DROP INDEX IF EXISTS uq_customer_tenant_code;
DROP INDEX IF EXISTS uq_customer_tenant_name;

ALTER TABLE service_customers
  DROP COLUMN IF EXISTS updated_by,
  DROP COLUMN IF EXISTS created_by,
  DROP COLUMN IF EXISTS deleted_at,
  DROP COLUMN IF EXISTS version,
  DROP COLUMN IF EXISTS merged_into_id,
  DROP COLUMN IF EXISTS data_quality_score,
  DROP COLUMN IF EXISTS data_source,
  DROP COLUMN IF EXISTS churn_reason,
  DROP COLUMN IF EXISTS churn_at,
  DROP COLUMN IF EXISTS contract_end_at,
  DROP COLUMN IF EXISTS contract_start_at,
  DROP COLUMN IF EXISTS onboarded_at,
  DROP COLUMN IF EXISTS lifecycle_stage,
  DROP COLUMN IF EXISTS billing_address,
  DROP COLUMN IF EXISTS invoice_title,
  DROP COLUMN IF EXISTS credit_limit,
  DROP COLUMN IF EXISTS payment_terms,
  DROP COLUMN IF EXISTS billing_currency,
  DROP COLUMN IF EXISTS oncall_group_id,
  DROP COLUMN IF EXISTS cs_manager_id,
  DROP COLUMN IF EXISTS service_owner_id,
  DROP COLUMN IF EXISTS account_manager_id,
  DROP COLUMN IF EXISTS coverage_scope,
  DROP COLUMN IF EXISTS support_hours,
  DROP COLUMN IF EXISTS sla_definition_id,
  DROP COLUMN IF EXISTS service_tier,
  DROP COLUMN IF EXISTS language,
  DROP COLUMN IF EXISTS timezone,
  DROP COLUMN IF EXISTS address,
  DROP COLUMN IF EXISTS service_email,
  DROP COLUMN IF EXISTS main_phone,
  DROP COLUMN IF EXISTS website,
  DROP COLUMN IF EXISTS credit_rating,
  DROP COLUMN IF EXISTS vip_level,
  DROP COLUMN IF EXISTS tier,
  DROP COLUMN IF EXISTS ownership_type,
  DROP COLUMN IF EXISTS size_class,
  DROP COLUMN IF EXISTS sub_industry,
  DROP COLUMN IF EXISTS industry,
  DROP COLUMN IF EXISTS customer_type,
  DROP COLUMN IF EXISTS email_domains,
  DROP COLUMN IF EXISTS registration_no,
  DROP COLUMN IF EXISTS duns_number,
  DROP COLUMN IF EXISTS normalized_tax_id,
  DROP COLUMN IF EXISTS tax_id,
  DROP COLUMN IF EXISTS en_name,
  DROP COLUMN IF EXISTS legal_name,
  DROP COLUMN IF EXISTS code;

ALTER TABLE service_customers
  ADD CONSTRAINT service_customers_tenant_id_normalized_name_key
  UNIQUE (tenant_id, normalized_name);
