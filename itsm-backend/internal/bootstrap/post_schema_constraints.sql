-- Post-schema hardening for customer master-data P0/P1/P2 and contract C1.
--
-- Why this file exists:
--   ent.Schema.Create() rebuilds managed tables and drops constraints it does not
--   know about. The versioned migration ledger then skips the original SQL on the
--   next init, leaving composite tenant FKs / CHECKs / defaults absent. This file
--   is embedded in the binary and executed after every Schema.Create, and every
--   statement below is idempotent.

-- ── Column defaults (ent emits NOT NULL timestamps without DB defaults) ──────
ALTER TABLE customer_contacts
  ALTER COLUMN created_at SET DEFAULT NOW(),
  ALTER COLUMN updated_at SET DEFAULT NOW();
ALTER TABLE customer_tenant_links
  ALTER COLUMN created_at SET DEFAULT NOW(),
  ALTER COLUMN updated_at SET DEFAULT NOW();
ALTER TABLE customer_relationships
  ALTER COLUMN created_at SET DEFAULT NOW(),
  ALTER COLUMN updated_at SET DEFAULT NOW();
ALTER TABLE project_customer_links
  ALTER COLUMN created_at SET DEFAULT NOW(),
  ALTER COLUMN updated_at SET DEFAULT NOW();
ALTER TABLE customer_external_ids
  ALTER COLUMN created_at SET DEFAULT NOW();
ALTER TABLE contract_service_items
  ALTER COLUMN created_at SET DEFAULT NOW(),
  ALTER COLUMN updated_at SET DEFAULT NOW();
ALTER TABLE contract_amendments
  ALTER COLUMN created_at SET DEFAULT NOW(),
  ALTER COLUMN updated_at SET DEFAULT NOW();
ALTER TABLE contract_usage_ledger
  ALTER COLUMN created_at SET DEFAULT NOW();
ALTER TABLE contract_project_links
  ALTER COLUMN created_at SET DEFAULT NOW(),
  ALTER COLUMN updated_at SET DEFAULT NOW();

-- ── Customer master P0/P1/P2 constraints ────────────────────────────────────
DO $$
BEGIN
  IF NOT EXISTS (
    SELECT 1 FROM support_contracts c
    JOIN service_customers sc ON sc.id = c.customer_id
    WHERE c.tenant_id <> sc.tenant_id
  ) AND NOT EXISTS (
    SELECT 1 FROM pg_constraint
    WHERE conrelid = 'support_contracts'::regclass AND conname = 'fk_sc_customer_tenant'
  ) THEN
    ALTER TABLE support_contracts
      ADD CONSTRAINT fk_sc_customer_tenant
      FOREIGN KEY (tenant_id, customer_id)
      REFERENCES service_customers (tenant_id, id);
  END IF;

  IF NOT EXISTS (
    SELECT 1 FROM support_contracts c
    JOIN customer_branches b ON b.id = c.branch_id
    WHERE c.branch_id IS NOT NULL
      AND (c.tenant_id <> b.tenant_id OR c.customer_id <> b.customer_id)
  ) AND NOT EXISTS (
    SELECT 1 FROM pg_constraint
    WHERE conrelid = 'support_contracts'::regclass AND conname = 'fk_contract_branch_tenant_customer'
  ) THEN
    ALTER TABLE support_contracts
      ADD CONSTRAINT fk_contract_branch_tenant_customer
      FOREIGN KEY (tenant_id, customer_id, branch_id)
      REFERENCES customer_branches (tenant_id, customer_id, id);
  END IF;

  IF NOT EXISTS (
    SELECT 1 FROM pg_constraint
    WHERE conrelid = 'customer_contacts'::regclass AND conname = 'fk_contact_customer_tenant'
  ) THEN
    ALTER TABLE customer_contacts
      ADD CONSTRAINT fk_contact_customer_tenant
      FOREIGN KEY (tenant_id, customer_id)
      REFERENCES service_customers (tenant_id, id);
  END IF;

  IF NOT EXISTS (
    SELECT 1 FROM pg_constraint
    WHERE conrelid = 'customer_contacts'::regclass AND conname = 'fk_contact_branch_tenant'
  ) THEN
    ALTER TABLE customer_contacts
      ADD CONSTRAINT fk_contact_branch_tenant
      FOREIGN KEY (tenant_id, customer_id, branch_id)
      REFERENCES customer_branches (tenant_id, customer_id, id);
  END IF;

  IF NOT EXISTS (
    SELECT 1 FROM pg_constraint
    WHERE conrelid = 'customer_tenant_links'::regclass AND conname = 'fk_ctlink_customer_tenant'
  ) THEN
    ALTER TABLE customer_tenant_links
      ADD CONSTRAINT fk_ctlink_customer_tenant
      FOREIGN KEY (tenant_id, customer_id)
      REFERENCES service_customers (tenant_id, id);
  END IF;

  IF NOT EXISTS (
    SELECT 1 FROM pg_constraint
    WHERE conrelid = 'customer_relationships'::regclass AND conname = 'ck_rel_not_self'
  ) THEN
    ALTER TABLE customer_relationships
      ADD CONSTRAINT ck_rel_not_self CHECK (from_customer_id <> to_customer_id);
  END IF;

  IF NOT EXISTS (
    SELECT 1 FROM pg_constraint
    WHERE conrelid = 'customer_relationships'::regclass AND conname = 'fk_rel_from_tenant'
  ) THEN
    ALTER TABLE customer_relationships
      ADD CONSTRAINT fk_rel_from_tenant
      FOREIGN KEY (tenant_id, from_customer_id)
      REFERENCES service_customers (tenant_id, id);
  END IF;

  IF NOT EXISTS (
    SELECT 1 FROM pg_constraint
    WHERE conrelid = 'customer_relationships'::regclass AND conname = 'fk_rel_to_tenant'
  ) THEN
    ALTER TABLE customer_relationships
      ADD CONSTRAINT fk_rel_to_tenant
      FOREIGN KEY (tenant_id, to_customer_id)
      REFERENCES service_customers (tenant_id, id);
  END IF;

  IF NOT EXISTS (
    SELECT 1 FROM pg_constraint
    WHERE conrelid = 'project_customer_links'::regclass AND conname = 'fk_pcl_project_tenant'
  ) THEN
    ALTER TABLE project_customer_links
      ADD CONSTRAINT fk_pcl_project_tenant
      FOREIGN KEY (tenant_id, project_id)
      REFERENCES projects (tenant_id, id);
  END IF;

  IF NOT EXISTS (
    SELECT 1 FROM pg_constraint
    WHERE conrelid = 'project_customer_links'::regclass AND conname = 'fk_pcl_customer_tenant'
  ) THEN
    ALTER TABLE project_customer_links
      ADD CONSTRAINT fk_pcl_customer_tenant
      FOREIGN KEY (tenant_id, customer_id)
      REFERENCES service_customers (tenant_id, id);
  END IF;

  IF NOT EXISTS (
    SELECT 1 FROM pg_constraint
    WHERE conrelid = 'project_customer_links'::regclass AND conname = 'fk_pcl_branch_tenant'
  ) THEN
    ALTER TABLE project_customer_links
      ADD CONSTRAINT fk_pcl_branch_tenant
      FOREIGN KEY (tenant_id, customer_id, branch_id)
      REFERENCES customer_branches (tenant_id, customer_id, id);
  END IF;

  IF NOT EXISTS (
    SELECT 1 FROM pg_constraint
    WHERE conrelid = 'customer_external_ids'::regclass AND conname = 'fk_extid_customer_tenant'
  ) THEN
    ALTER TABLE customer_external_ids
      ADD CONSTRAINT fk_extid_customer_tenant
      FOREIGN KEY (tenant_id, customer_id)
      REFERENCES service_customers (tenant_id, id);
  END IF;
END $$;

-- ── Contract C1 CHECK constraints ───────────────────────────────────────────
DO $$
BEGIN
  IF NOT EXISTS (
    SELECT 1 FROM pg_constraint
    WHERE conrelid = 'contract_service_items'::regclass AND conname = 'ck_csi_qty'
  ) THEN
    ALTER TABLE contract_service_items ADD CONSTRAINT ck_csi_qty CHECK (quantity >= 0);
  END IF;
  IF NOT EXISTS (
    SELECT 1 FROM pg_constraint
    WHERE conrelid = 'contract_service_items'::regclass AND conname = 'ck_csi_period'
  ) THEN
    ALTER TABLE contract_service_items ADD CONSTRAINT ck_csi_period CHECK (period_count > 0);
  END IF;
  IF NOT EXISTS (
    SELECT 1 FROM pg_constraint
    WHERE conrelid = 'contract_service_items'::regclass AND conname = 'ck_csi_price'
  ) THEN
    ALTER TABLE contract_service_items ADD CONSTRAINT ck_csi_price CHECK (unit_price >= 0);
  END IF;
  IF NOT EXISTS (
    SELECT 1 FROM pg_constraint
    WHERE conrelid = 'contract_service_items'::regclass AND conname = 'ck_csi_used'
  ) THEN
    ALTER TABLE contract_service_items ADD CONSTRAINT ck_csi_used CHECK (quota_used >= 0);
  END IF;
  IF NOT EXISTS (
    SELECT 1 FROM pg_constraint
    WHERE conrelid = 'contract_usage_ledger'::regclass AND conname = 'ck_usage_delta'
  ) THEN
    ALTER TABLE contract_usage_ledger
      ADD CONSTRAINT ck_usage_delta CHECK (quantity_delta <> 0 OR amount_delta <> 0);
  END IF;
  IF NOT EXISTS (
    SELECT 1 FROM pg_constraint
    WHERE conrelid = 'contract_project_links'::regclass AND conname = 'ck_cpl_amount'
  ) THEN
    ALTER TABLE contract_project_links
      ADD CONSTRAINT ck_cpl_amount CHECK (allocated_amount IS NULL OR allocated_amount >= 0);
  END IF;
  IF NOT EXISTS (
    SELECT 1 FROM pg_constraint
    WHERE conrelid = 'contract_project_links'::regclass AND conname = 'ck_cpl_hours'
  ) THEN
    ALTER TABLE contract_project_links
      ADD CONSTRAINT ck_cpl_hours CHECK (allocated_hours IS NULL OR allocated_hours >= 0);
  END IF;
  IF NOT EXISTS (
    SELECT 1 FROM pg_constraint
    WHERE conrelid = 'contract_project_links'::regclass AND conname = 'ck_cpl_pct'
  ) THEN
    ALTER TABLE contract_project_links
      ADD CONSTRAINT ck_cpl_pct CHECK (allocation_pct IS NULL OR (allocation_pct >= 0 AND allocation_pct <= 100));
  END IF;
  IF NOT EXISTS (
    SELECT 1 FROM pg_constraint
    WHERE conrelid = 'contract_project_links'::regclass AND conname = 'ck_cpl_billing_only'
  ) THEN
    ALTER TABLE contract_project_links ADD CONSTRAINT ck_cpl_billing_only CHECK (
      relation_type = 'billing'
      OR (allocated_amount IS NULL AND allocated_hours IS NULL
          AND allocation_pct IS NULL AND service_item_id IS NULL)
    );
  END IF;
  IF NOT EXISTS (
    SELECT 1 FROM pg_constraint
    WHERE conrelid = 'contract_project_links'::regclass AND conname = 'ck_cpl_billing_item'
  ) THEN
    ALTER TABLE contract_project_links ADD CONSTRAINT ck_cpl_billing_item
      CHECK (relation_type <> 'billing' OR service_item_id IS NOT NULL);
  END IF;
  IF NOT EXISTS (
    SELECT 1 FROM pg_constraint
    WHERE conrelid = 'contract_project_links'::regclass AND conname = 'ck_cpl_billing_amount'
  ) THEN
    ALTER TABLE contract_project_links ADD CONSTRAINT ck_cpl_billing_amount CHECK (
      relation_type <> 'billing'
      OR allocated_amount IS NOT NULL
      OR allocated_hours IS NOT NULL
      OR allocation_pct IS NOT NULL
    );
  END IF;
  IF NOT EXISTS (
    SELECT 1 FROM pg_constraint
    WHERE conrelid = 'contract_project_links'::regclass AND conname = 'ck_cpl_primary_basis'
  ) THEN
    ALTER TABLE contract_project_links ADD CONSTRAINT ck_cpl_primary_basis
      CHECK (NOT is_primary OR relation_type = 'basis');
  END IF;
END $$;

-- ── Contract C1 tenant-safe composite foreign keys ──────────────────────────
DO $$
BEGIN
  IF NOT EXISTS (
    SELECT 1 FROM pg_constraint
    WHERE conrelid = 'contract_service_items'::regclass AND conname = 'fk_csi_contract_tenant'
  ) THEN
    ALTER TABLE contract_service_items ADD CONSTRAINT fk_csi_contract_tenant
      FOREIGN KEY (tenant_id, contract_id) REFERENCES support_contracts (tenant_id, id);
  END IF;
  IF NOT EXISTS (
    SELECT 1 FROM pg_constraint
    WHERE conrelid = 'contract_amendments'::regclass AND conname = 'fk_amend_contract_tenant'
  ) THEN
    ALTER TABLE contract_amendments ADD CONSTRAINT fk_amend_contract_tenant
      FOREIGN KEY (tenant_id, contract_id) REFERENCES support_contracts (tenant_id, id);
  END IF;
  IF NOT EXISTS (
    SELECT 1 FROM pg_constraint
    WHERE conrelid = 'contract_usage_ledger'::regclass AND conname = 'fk_usage_contract_tenant'
  ) THEN
    ALTER TABLE contract_usage_ledger ADD CONSTRAINT fk_usage_contract_tenant
      FOREIGN KEY (tenant_id, contract_id) REFERENCES support_contracts (tenant_id, id);
  END IF;
  IF NOT EXISTS (
    SELECT 1 FROM pg_constraint
    WHERE conrelid = 'contract_usage_ledger'::regclass AND conname = 'fk_usage_item_tenant'
  ) THEN
    ALTER TABLE contract_usage_ledger ADD CONSTRAINT fk_usage_item_tenant
      FOREIGN KEY (tenant_id, service_item_id) REFERENCES contract_service_items (tenant_id, id);
  END IF;
  IF NOT EXISTS (
    SELECT 1 FROM pg_constraint
    WHERE conrelid = 'contract_project_links'::regclass AND conname = 'fk_cpl_contract_tenant'
  ) THEN
    ALTER TABLE contract_project_links ADD CONSTRAINT fk_cpl_contract_tenant
      FOREIGN KEY (tenant_id, contract_id) REFERENCES support_contracts (tenant_id, id);
  END IF;
  IF NOT EXISTS (
    SELECT 1 FROM pg_constraint
    WHERE conrelid = 'contract_project_links'::regclass AND conname = 'fk_cpl_project_tenant'
  ) THEN
    ALTER TABLE contract_project_links ADD CONSTRAINT fk_cpl_project_tenant
      FOREIGN KEY (tenant_id, project_id) REFERENCES projects (tenant_id, id);
  END IF;
  IF NOT EXISTS (
    SELECT 1 FROM pg_constraint
    WHERE conrelid = 'contract_project_links'::regclass AND conname = 'fk_cpl_service_item_tenant'
  ) THEN
    ALTER TABLE contract_project_links ADD CONSTRAINT fk_cpl_service_item_tenant
      FOREIGN KEY (tenant_id, service_item_id) REFERENCES contract_service_items (tenant_id, id);
  END IF;
END $$;
