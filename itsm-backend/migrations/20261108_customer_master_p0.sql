-- 客户主数据 P0（设计文档：docs/customer-master-data/02-ddl-and-migration.md）
--
-- 范围：
--   1. service_customers 八组字段（标识/分类/分级/联系/服务/财务/生命周期/治理）
--   2. customer_branches 升级为服务点/地点
--   3. customer_contacts、customer_tenant_links 两张 P0 关联表
--   4. 存量回填：code / customer_type / tenant link / email_domains / lifecycle_stage
--   5. 同租户复合唯一键与可安全追加的复合外键
--
-- 全部语句可重复执行；不包含顶层事务控制（migration/tx_control_test 守卫）。

-- ── 1. service_customers 八组字段 ──────────────────────────────────────────
ALTER TABLE service_customers
  ADD COLUMN IF NOT EXISTS code                VARCHAR(64),
  ADD COLUMN IF NOT EXISTS legal_name          VARCHAR(255),
  ADD COLUMN IF NOT EXISTS en_name             VARCHAR(255),
  ADD COLUMN IF NOT EXISTS tax_id              VARCHAR(64),
  ADD COLUMN IF NOT EXISTS normalized_tax_id   VARCHAR(64),
  ADD COLUMN IF NOT EXISTS duns_number         VARCHAR(32),
  ADD COLUMN IF NOT EXISTS registration_no     VARCHAR(64),
  ADD COLUMN IF NOT EXISTS email_domains       JSONB DEFAULT '[]'::jsonb,
  ADD COLUMN IF NOT EXISTS customer_type       VARCHAR(32) NOT NULL DEFAULT 'direct',
  ADD COLUMN IF NOT EXISTS industry            VARCHAR(64),
  ADD COLUMN IF NOT EXISTS sub_industry        VARCHAR(64),
  ADD COLUMN IF NOT EXISTS size_class          VARCHAR(32),
  ADD COLUMN IF NOT EXISTS ownership_type      VARCHAR(32),
  ADD COLUMN IF NOT EXISTS tier                VARCHAR(16) NOT NULL DEFAULT 'C',
  ADD COLUMN IF NOT EXISTS vip_level           SMALLINT NOT NULL DEFAULT 0,
  ADD COLUMN IF NOT EXISTS credit_rating       VARCHAR(16),
  ADD COLUMN IF NOT EXISTS website             VARCHAR(255),
  ADD COLUMN IF NOT EXISTS main_phone          VARCHAR(64),
  ADD COLUMN IF NOT EXISTS service_email       VARCHAR(320),
  ADD COLUMN IF NOT EXISTS address             JSONB DEFAULT '{}'::jsonb,
  ADD COLUMN IF NOT EXISTS timezone            VARCHAR(64),
  ADD COLUMN IF NOT EXISTS language            VARCHAR(16) DEFAULT 'zh-CN',
  ADD COLUMN IF NOT EXISTS service_tier        VARCHAR(32),
  ADD COLUMN IF NOT EXISTS sla_definition_id   BIGINT,
  ADD COLUMN IF NOT EXISTS support_hours       VARCHAR(64),
  ADD COLUMN IF NOT EXISTS coverage_scope      JSONB DEFAULT '{}'::jsonb,
  ADD COLUMN IF NOT EXISTS account_manager_id  BIGINT,
  ADD COLUMN IF NOT EXISTS service_owner_id    BIGINT,
  ADD COLUMN IF NOT EXISTS cs_manager_id       BIGINT,
  ADD COLUMN IF NOT EXISTS oncall_group_id     BIGINT,
  ADD COLUMN IF NOT EXISTS billing_currency    VARCHAR(8),
  ADD COLUMN IF NOT EXISTS payment_terms       VARCHAR(32),
  ADD COLUMN IF NOT EXISTS credit_limit        NUMERIC(18,2),
  ADD COLUMN IF NOT EXISTS invoice_title       VARCHAR(255),
  ADD COLUMN IF NOT EXISTS billing_address     JSONB DEFAULT '{}'::jsonb,
  ADD COLUMN IF NOT EXISTS lifecycle_stage     VARCHAR(32) NOT NULL DEFAULT 'prospect',
  ADD COLUMN IF NOT EXISTS onboarded_at        TIMESTAMPTZ,
  ADD COLUMN IF NOT EXISTS contract_start_at   TIMESTAMPTZ,
  ADD COLUMN IF NOT EXISTS contract_end_at     TIMESTAMPTZ,
  ADD COLUMN IF NOT EXISTS churn_at            TIMESTAMPTZ,
  ADD COLUMN IF NOT EXISTS churn_reason        VARCHAR(255),
  ADD COLUMN IF NOT EXISTS data_source         VARCHAR(32) DEFAULT 'manual',
  ADD COLUMN IF NOT EXISTS data_quality_score  SMALLINT NOT NULL DEFAULT 0,
  ADD COLUMN IF NOT EXISTS merged_into_id      BIGINT,
  ADD COLUMN IF NOT EXISTS version             BIGINT NOT NULL DEFAULT 1,
  ADD COLUMN IF NOT EXISTS deleted_at          TIMESTAMPTZ,
  ADD COLUMN IF NOT EXISTS created_by          BIGINT,
  ADD COLUMN IF NOT EXISTS updated_by          BIGINT;

-- 旧唯一约束改为软删感知的部分唯一索引。
ALTER TABLE service_customers
  DROP CONSTRAINT IF EXISTS service_customers_tenant_id_normalized_name_key;
DROP INDEX IF EXISTS service_customers_tenant_id_normalized_name;

CREATE UNIQUE INDEX IF NOT EXISTS uq_customer_tenant_name
  ON service_customers (tenant_id, normalized_name)
  WHERE deleted_at IS NULL;

CREATE UNIQUE INDEX IF NOT EXISTS uq_customer_tenant_code
  ON service_customers (tenant_id, code)
  WHERE deleted_at IS NULL AND code IS NOT NULL AND code <> '';

CREATE UNIQUE INDEX IF NOT EXISTS uq_customer_tenant_taxid
  ON service_customers (tenant_id, normalized_tax_id)
  WHERE deleted_at IS NULL AND normalized_tax_id IS NOT NULL AND normalized_tax_id <> '';

CREATE UNIQUE INDEX IF NOT EXISTS uq_customer_tenant_id
  ON service_customers (tenant_id, id);

CREATE INDEX IF NOT EXISTS idx_customer_tenant_tier
  ON service_customers (tenant_id, tier, status);

CREATE INDEX IF NOT EXISTS idx_customer_tenant_stage
  ON service_customers (tenant_id, lifecycle_stage, status);

CREATE INDEX IF NOT EXISTS idx_customer_tenant_am
  ON service_customers (tenant_id, account_manager_id);

-- ── 2. customer_branches 服务点/地点字段 ──────────────────────────────────
ALTER TABLE customer_branches
  ADD COLUMN IF NOT EXISTS code             VARCHAR(64),
  ADD COLUMN IF NOT EXISTS branch_type      VARCHAR(32) NOT NULL DEFAULT 'site',
  ADD COLUMN IF NOT EXISTS address          JSONB DEFAULT '{}'::jsonb,
  ADD COLUMN IF NOT EXISTS timezone         VARCHAR(64),
  ADD COLUMN IF NOT EXISTS contact_phone    VARCHAR(64),
  ADD COLUMN IF NOT EXISTS email_domains    JSONB DEFAULT '[]'::jsonb,
  ADD COLUMN IF NOT EXISTS service_tier     VARCHAR(32),
  ADD COLUMN IF NOT EXISTS support_hours    VARCHAR(64),
  ADD COLUMN IF NOT EXISTS is_primary       BOOLEAN NOT NULL DEFAULT FALSE,
  ADD COLUMN IF NOT EXISTS linked_tenant_id BIGINT,
  ADD COLUMN IF NOT EXISTS deleted_at       TIMESTAMPTZ;

ALTER TABLE customer_branches
  DROP CONSTRAINT IF EXISTS customer_branches_tenant_id_customer_id_normalized_name_key;
DROP INDEX IF EXISTS customer_branches_tenant_id_customer_id_normalized_name;

CREATE UNIQUE INDEX IF NOT EXISTS uq_branch_tenant_name
  ON customer_branches (tenant_id, customer_id, normalized_name)
  WHERE deleted_at IS NULL;

CREATE UNIQUE INDEX IF NOT EXISTS uq_branch_tenant_code
  ON customer_branches (tenant_id, code)
  WHERE deleted_at IS NULL AND code IS NOT NULL AND code <> '';

CREATE UNIQUE INDEX IF NOT EXISTS uq_branch_primary
  ON customer_branches (tenant_id, customer_id)
  WHERE is_primary AND status = 'active' AND deleted_at IS NULL;

CREATE UNIQUE INDEX IF NOT EXISTS uq_branch_tenant_id
  ON customer_branches (tenant_id, id);

CREATE UNIQUE INDEX IF NOT EXISTS uq_branch_tenant_customer_id
  ON customer_branches (tenant_id, customer_id, id);

-- ── 3. customer_contacts ───────────────────────────────────────────────────
CREATE TABLE IF NOT EXISTS customer_contacts (
  id               BIGSERIAL PRIMARY KEY,
  tenant_id        BIGINT NOT NULL,
  customer_id      BIGINT NOT NULL REFERENCES service_customers(id),
  branch_id        BIGINT REFERENCES customer_branches(id),
  name             VARCHAR(255) NOT NULL,
  name_en          VARCHAR(255),
  title            VARCHAR(128),
  department       VARCHAR(128),
  email            VARCHAR(320),
  normalized_email VARCHAR(320),
  mobile           VARCHAR(64),
  phone            VARCHAR(64),
  wechat           VARCHAR(128),
  contact_role     VARCHAR(32) NOT NULL DEFAULT 'business',
  is_primary       BOOLEAN NOT NULL DEFAULT FALSE,
  language         VARCHAR(16),
  timezone         VARCHAR(64),
  opt_in           BOOLEAN NOT NULL DEFAULT TRUE,
  linked_user_id   BIGINT,
  source           VARCHAR(32) NOT NULL DEFAULT 'manual',
  status           VARCHAR(16) NOT NULL DEFAULT 'active',
  deleted_at       TIMESTAMPTZ,
  created_at       TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  updated_at       TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE UNIQUE INDEX IF NOT EXISTS uq_contact_email
  ON customer_contacts (tenant_id, customer_id, normalized_email)
  WHERE status = 'active' AND deleted_at IS NULL
    AND normalized_email IS NOT NULL AND normalized_email <> '';

CREATE UNIQUE INDEX IF NOT EXISTS uq_contact_primary
  ON customer_contacts (tenant_id, customer_id, contact_role)
  WHERE is_primary AND status = 'active' AND deleted_at IS NULL;

CREATE INDEX IF NOT EXISTS idx_contact_tenant_customer
  ON customer_contacts (tenant_id, customer_id, status);

CREATE INDEX IF NOT EXISTS idx_contact_tenant_email
  ON customer_contacts (tenant_id, normalized_email);

CREATE UNIQUE INDEX IF NOT EXISTS uq_contact_tenant_id
  ON customer_contacts (tenant_id, id);

-- ── 4. customer_tenant_links ───────────────────────────────────────────────
CREATE TABLE IF NOT EXISTS customer_tenant_links (
  id                BIGSERIAL PRIMARY KEY,
  tenant_id         BIGINT NOT NULL,
  customer_id       BIGINT NOT NULL REFERENCES service_customers(id),
  linked_tenant_id  BIGINT NOT NULL,
  link_type         VARCHAR(24) NOT NULL DEFAULT 'primary',
  is_billing_anchor BOOLEAN NOT NULL DEFAULT FALSE,
  provision_status  VARCHAR(24) NOT NULL DEFAULT 'pending',
  provisioned_at    TIMESTAMPTZ,
  note              VARCHAR(500),
  deleted_at        TIMESTAMPTZ,
  created_at        TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  updated_at        TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE UNIQUE INDEX IF NOT EXISTS uq_ctlink
  ON customer_tenant_links (tenant_id, customer_id, linked_tenant_id)
  WHERE deleted_at IS NULL;

CREATE UNIQUE INDEX IF NOT EXISTS uq_ctlink_primary
  ON customer_tenant_links (tenant_id, customer_id)
  WHERE link_type = 'primary' AND deleted_at IS NULL;

CREATE UNIQUE INDEX IF NOT EXISTS uq_ctlink_linked_tenant
  ON customer_tenant_links (tenant_id, linked_tenant_id)
  WHERE deleted_at IS NULL;

CREATE UNIQUE INDEX IF NOT EXISTS uq_ctlink_billing
  ON customer_tenant_links (tenant_id, customer_id)
  WHERE is_billing_anchor AND deleted_at IS NULL;

CREATE INDEX IF NOT EXISTS idx_ctlink_tenant_linked
  ON customer_tenant_links (tenant_id, linked_tenant_id);

CREATE UNIQUE INDEX IF NOT EXISTS uq_ctlink_tenant_id
  ON customer_tenant_links (tenant_id, id);

-- ── 5. 存量回填（M2/M3/M4/M5/M7）──────────────────────────────────────────
UPDATE service_customers sc
SET code = 'CUS-' || LPAD(sc.id::text, 4, '0')
WHERE sc.code IS NULL
  AND NOT EXISTS (
    SELECT 1 FROM service_customers existing
    WHERE existing.tenant_id = sc.tenant_id
      AND existing.code = 'CUS-' || LPAD(sc.id::text, 4, '0')
  );

UPDATE service_customers
SET customer_type = CASE
      WHEN linked_customer_tenant_id IS NULL THEN 'direct'
      ELSE 'msp_endcustomer'
    END
WHERE customer_type IS NULL OR customer_type = '' OR customer_type = 'direct';

INSERT INTO customer_tenant_links
  (tenant_id, customer_id, linked_tenant_id, link_type, is_billing_anchor)
SELECT sc.tenant_id, sc.id, sc.linked_customer_tenant_id, 'primary', TRUE
FROM service_customers sc
WHERE sc.linked_customer_tenant_id IS NOT NULL
  AND NOT EXISTS (
    SELECT 1 FROM customer_tenant_links l
    WHERE l.tenant_id = sc.tenant_id
      AND l.customer_id = sc.id
      AND l.linked_tenant_id = sc.linked_customer_tenant_id
      AND l.deleted_at IS NULL
  )
-- 存量可能存在「多个客户档案指向同一 linked_tenant_id」的脏数据；
-- 反向唯一索引只允许保留一条映射，迁移选择保留最早的一条，不阻断上线。
ON CONFLICT DO NOTHING;

WITH domain_counts AS (
  SELECT ec.tenant_id, ec.customer_id, d.domain, COUNT(*) AS hits
  FROM email_conversations ec
  JOIN source_organizations so
    ON so.id = ec.source_organization_id
   AND so.tenant_id = ec.tenant_id
  CROSS JOIN LATERAL jsonb_array_elements_text(COALESCE(so.email_domains, '[]'::jsonb)) AS d(domain)
  WHERE ec.customer_id IS NOT NULL
    AND d.domain <> ''
  GROUP BY ec.tenant_id, ec.customer_id, d.domain
), ranked AS (
  SELECT tenant_id, customer_id, domain,
         ROW_NUMBER() OVER (PARTITION BY tenant_id, customer_id ORDER BY hits DESC, domain) AS rn
  FROM domain_counts
)
UPDATE service_customers sc
SET email_domains = jsonb_build_array(r.domain)
FROM ranked r
WHERE sc.tenant_id = r.tenant_id
  AND sc.id = r.customer_id
  AND r.rn = 1
  AND (sc.email_domains IS NULL OR sc.email_domains = '[]'::jsonb);

UPDATE service_customers sc
SET lifecycle_stage = 'active'
WHERE sc.status = 'active'
  AND sc.lifecycle_stage = 'prospect'
  AND (
    sc.linked_customer_tenant_id IS NOT NULL
    OR EXISTS (
      SELECT 1 FROM customer_tenant_links l
      WHERE l.tenant_id = sc.tenant_id
        AND l.customer_id = sc.id
        AND l.deleted_at IS NULL
    )
  );

-- ── 6. 同租户复合外键（仅在存量数据无跨租户引用时追加）──────────────────
DO $$
BEGIN
  IF NOT EXISTS (
    SELECT 1 FROM support_contracts c
    JOIN service_customers sc ON sc.id = c.customer_id
    WHERE c.tenant_id <> sc.tenant_id
  ) THEN
    IF NOT EXISTS (
      SELECT 1 FROM pg_constraint WHERE conname = 'fk_sc_customer_tenant'
    ) THEN
      ALTER TABLE support_contracts
        ADD CONSTRAINT fk_sc_customer_tenant
        FOREIGN KEY (tenant_id, customer_id)
        REFERENCES service_customers (tenant_id, id);
    END IF;
  END IF;
END $$;

DO $$
BEGIN
  IF NOT EXISTS (
    SELECT 1 FROM support_contracts c
    JOIN customer_branches b ON b.id = c.branch_id
    WHERE c.branch_id IS NOT NULL
      AND (c.tenant_id <> b.tenant_id OR c.customer_id <> b.customer_id)
  ) THEN
    IF NOT EXISTS (
      SELECT 1 FROM pg_constraint WHERE conname = 'fk_contract_branch_tenant_customer'
    ) THEN
      ALTER TABLE support_contracts
        ADD CONSTRAINT fk_contract_branch_tenant_customer
        FOREIGN KEY (tenant_id, customer_id, branch_id)
        REFERENCES customer_branches (tenant_id, customer_id, id);
    END IF;
  END IF;
END $$;

DO $$
BEGIN
  IF NOT EXISTS (
    SELECT 1 FROM pg_constraint WHERE conname = 'fk_contact_customer_tenant'
  ) THEN
    ALTER TABLE customer_contacts
      ADD CONSTRAINT fk_contact_customer_tenant
      FOREIGN KEY (tenant_id, customer_id)
      REFERENCES service_customers (tenant_id, id);
  END IF;
  IF NOT EXISTS (
    SELECT 1 FROM pg_constraint WHERE conname = 'fk_contact_branch_tenant'
  ) THEN
    ALTER TABLE customer_contacts
      ADD CONSTRAINT fk_contact_branch_tenant
      FOREIGN KEY (tenant_id, customer_id, branch_id)
      REFERENCES customer_branches (tenant_id, customer_id, id);
  END IF;
END $$;

DO $$
BEGIN
  IF NOT EXISTS (
    SELECT 1 FROM pg_constraint WHERE conname = 'fk_ctlink_customer_tenant'
  ) THEN
    ALTER TABLE customer_tenant_links
      ADD CONSTRAINT fk_ctlink_customer_tenant
      FOREIGN KEY (tenant_id, customer_id)
      REFERENCES service_customers (tenant_id, id);
  END IF;
END $$;
