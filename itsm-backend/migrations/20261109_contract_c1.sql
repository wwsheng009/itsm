-- 合同建模 C1（设计文档：docs/customer-master-data/06 §7.2/§7.3、02 §12.3）
--
-- 范围：
--   1. support_contracts 合同聚合根字段（CC-2~CC-7）
--   2. contract_service_items / contract_amendments / contract_usage_ledger / contract_project_links
--   3. 同租户复合外键、部分唯一索引、存量服务项汇总回填
--
-- 不包含：contracts（采购域）D-1/D-2 高风险改造 M16/M17、018/019 SLA 快照收回。
-- 全部语句可重复执行；不包含顶层事务控制。

-- ── 1. support_contracts 合同聚合根 ─────────────────────────────────────────
ALTER TABLE support_contracts
  ADD COLUMN IF NOT EXISTS effective_from          TIMESTAMPTZ,
  ADD COLUMN IF NOT EXISTS effective_until         TIMESTAMPTZ,
  ADD COLUMN IF NOT EXISTS signed_at               TIMESTAMPTZ,
  ADD COLUMN IF NOT EXISTS amount                  NUMERIC(18,2),
  ADD COLUMN IF NOT EXISTS currency                VARCHAR(8),
  ADD COLUMN IF NOT EXISTS contract_type           VARCHAR(32) NOT NULL DEFAULT 'support',
  ADD COLUMN IF NOT EXISTS contract_name           VARCHAR(255),
  ADD COLUMN IF NOT EXISTS parent_contract_id      BIGINT,
  ADD COLUMN IF NOT EXISTS renewal_of_contract_id  BIGINT,
  ADD COLUMN IF NOT EXISTS amendment_count         INT NOT NULL DEFAULT 0,
  ADD COLUMN IF NOT EXISTS sla_definition_id       BIGINT,
  ADD COLUMN IF NOT EXISTS auto_renew              BOOLEAN NOT NULL DEFAULT FALSE,
  ADD COLUMN IF NOT EXISTS terminated_at           TIMESTAMPTZ,
  ADD COLUMN IF NOT EXISTS termination_reason      VARCHAR(255),
  ADD COLUMN IF NOT EXISTS deleted_at              TIMESTAMPTZ,
  ADD COLUMN IF NOT EXISTS version                 BIGINT NOT NULL DEFAULT 1,
  ADD COLUMN IF NOT EXISTS created_by              BIGINT,
  ADD COLUMN IF NOT EXISTS updated_by              BIGINT;

UPDATE support_contracts
SET effective_from = COALESCE(effective_from, start_at),
    effective_until = COALESCE(
      effective_until,
      CASE WHEN end_at IS NULL THEN NULL ELSE date_trunc('day', end_at) + interval '1 day' END
    )
WHERE effective_from IS NULL OR effective_until IS NULL;

-- 旧全局式 ent 唯一索引改为软删除感知的部分唯一索引。
DROP INDEX IF EXISTS support_contracts_tenant_id_normalized_contract_number;
CREATE UNIQUE INDEX IF NOT EXISTS uq_sc_tenant_no
  ON support_contracts (tenant_id, normalized_contract_number)
  WHERE deleted_at IS NULL;

CREATE UNIQUE INDEX IF NOT EXISTS uq_sc_tenant_id
  ON support_contracts (tenant_id, id);

CREATE INDEX IF NOT EXISTS idx_sc_tenant_effective
  ON support_contracts (tenant_id, effective_until)
  WHERE status = 'active' AND deleted_at IS NULL;

CREATE INDEX IF NOT EXISTS idx_sc_tenant_type
  ON support_contracts (tenant_id, contract_type, status);

DO $$
BEGIN
  IF NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conname = 'ck_sc_amount_nonneg') THEN
    ALTER TABLE support_contracts
      ADD CONSTRAINT ck_sc_amount_nonneg CHECK (amount IS NULL OR amount >= 0);
  END IF;
END $$;

-- ── 2. contract_service_items ───────────────────────────────────────────────
CREATE TABLE IF NOT EXISTS contract_service_items (
  id                   BIGSERIAL PRIMARY KEY,
  tenant_id            BIGINT NOT NULL,
  contract_id          BIGINT NOT NULL REFERENCES support_contracts(id),
  catalog_item_id      BIGINT,
  item_code            VARCHAR(64),
  item_name            VARCHAR(255) NOT NULL,
  spec                 VARCHAR(500),
  billing_unit         VARCHAR(32) NOT NULL DEFAULT 'month',
  quantity             NUMERIC(18,4) NOT NULL DEFAULT 1,
  period_count         NUMERIC(18,4) NOT NULL DEFAULT 1,
  unit_price           NUMERIC(18,2) NOT NULL DEFAULT 0,
  amount               NUMERIC(18,2) NOT NULL DEFAULT 0,
  quota_total          NUMERIC(18,4),
  quota_used           NUMERIC(18,4) NOT NULL DEFAULT 0,
  note                 VARCHAR(500),
  sla_definition_id    BIGINT,
  response_target_min  INT,
  resolve_target_min   INT,
  sort_order           INT NOT NULL DEFAULT 0,
  status               VARCHAR(16) NOT NULL DEFAULT 'active',
  deleted_at           TIMESTAMPTZ,
  created_by           BIGINT,
  updated_by           BIGINT,
  created_at           TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  updated_at           TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  CONSTRAINT ck_csi_qty    CHECK (quantity >= 0),
  CONSTRAINT ck_csi_period CHECK (period_count > 0),
  CONSTRAINT ck_csi_price  CHECK (unit_price >= 0),
  CONSTRAINT ck_csi_used   CHECK (quota_used >= 0)
);

CREATE UNIQUE INDEX IF NOT EXISTS uq_csi_tenant_id
  ON contract_service_items (tenant_id, id);
CREATE UNIQUE INDEX IF NOT EXISTS uq_csi_contract_code
  ON contract_service_items (tenant_id, contract_id, item_code)
  WHERE deleted_at IS NULL AND item_code IS NOT NULL AND item_code <> '';
CREATE INDEX IF NOT EXISTS idx_csi_tenant_contract
  ON contract_service_items (tenant_id, contract_id, status);

-- ── 3. contract_amendments ──────────────────────────────────────────────────
CREATE TABLE IF NOT EXISTS contract_amendments (
  id               BIGSERIAL PRIMARY KEY,
  tenant_id        BIGINT NOT NULL,
  contract_id      BIGINT NOT NULL REFERENCES support_contracts(id),
  amendment_no     VARCHAR(64) NOT NULL,
  amend_type       VARCHAR(32) NOT NULL,
  effective_from   TIMESTAMPTZ NOT NULL,
  amount_delta     NUMERIC(18,2) NOT NULL DEFAULT 0,
  term_delta_days  INT,
  diff             JSONB DEFAULT '{}'::jsonb,
  reason           VARCHAR(500),
  status           VARCHAR(16) NOT NULL DEFAULT 'draft',
  signed_at        TIMESTAMPTZ,
  created_by       BIGINT,
  updated_by       BIGINT,
  created_at       TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  updated_at       TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE UNIQUE INDEX IF NOT EXISTS uq_amend_no
  ON contract_amendments (tenant_id, contract_id, amendment_no);
CREATE UNIQUE INDEX IF NOT EXISTS uq_amend_tenant_id
  ON contract_amendments (tenant_id, id);
CREATE INDEX IF NOT EXISTS idx_amend_tenant_contract
  ON contract_amendments (tenant_id, contract_id, status);

-- ── 4. contract_usage_ledger ────────────────────────────────────────────────
CREATE TABLE IF NOT EXISTS contract_usage_ledger (
  id               BIGSERIAL PRIMARY KEY,
  tenant_id        BIGINT NOT NULL,
  contract_id      BIGINT NOT NULL,
  service_item_id  BIGINT NOT NULL,
  source_type      VARCHAR(32) NOT NULL,
  source_ref       VARCHAR(160) NOT NULL,
  quantity_delta   NUMERIC(18,4) NOT NULL DEFAULT 0,
  amount_delta     NUMERIC(18,2) NOT NULL DEFAULT 0,
  occurred_at      TIMESTAMPTZ NOT NULL,
  note             VARCHAR(500),
  created_by       BIGINT,
  created_at       TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  CONSTRAINT ck_usage_delta CHECK (quantity_delta <> 0 OR amount_delta <> 0)
);

CREATE UNIQUE INDEX IF NOT EXISTS uq_usage_source
  ON contract_usage_ledger (tenant_id, service_item_id, source_type, source_ref);
CREATE INDEX IF NOT EXISTS idx_usage_tenant_contract
  ON contract_usage_ledger (tenant_id, contract_id, occurred_at);
CREATE INDEX IF NOT EXISTS idx_usage_tenant_item
  ON contract_usage_ledger (tenant_id, service_item_id, occurred_at);
CREATE UNIQUE INDEX IF NOT EXISTS uq_usage_tenant_id
  ON contract_usage_ledger (tenant_id, id);

-- ── 5. contract_project_links ───────────────────────────────────────────────
CREATE TABLE IF NOT EXISTS contract_project_links (
  id               BIGSERIAL PRIMARY KEY,
  tenant_id        BIGINT NOT NULL,
  contract_id      BIGINT NOT NULL REFERENCES support_contracts(id),
  project_id       BIGINT NOT NULL,
  service_item_id  BIGINT,
  relation_type    VARCHAR(24) NOT NULL DEFAULT 'basis',
  allocated_amount NUMERIC(18,2),
  allocated_hours  NUMERIC(10,2),
  allocation_pct   SMALLINT,
  is_primary       BOOLEAN NOT NULL DEFAULT FALSE,
  note             VARCHAR(500),
  effective_from   TIMESTAMPTZ,
  effective_to     TIMESTAMPTZ,
  status           VARCHAR(16) NOT NULL DEFAULT 'active',
  deleted_at       TIMESTAMPTZ,
  created_by       BIGINT,
  created_at       TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  updated_at       TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  CONSTRAINT ck_cpl_amount CHECK (allocated_amount IS NULL OR allocated_amount >= 0),
  CONSTRAINT ck_cpl_hours  CHECK (allocated_hours IS NULL OR allocated_hours >= 0),
  CONSTRAINT ck_cpl_pct    CHECK (allocation_pct IS NULL OR (allocation_pct >= 0 AND allocation_pct <= 100)),
  CONSTRAINT ck_cpl_billing_only CHECK (
    relation_type = 'billing'
    OR (allocated_amount IS NULL AND allocated_hours IS NULL
        AND allocation_pct IS NULL AND service_item_id IS NULL)
  ),
  CONSTRAINT ck_cpl_billing_item CHECK (
    relation_type <> 'billing' OR service_item_id IS NOT NULL
  ),
  CONSTRAINT ck_cpl_billing_amount CHECK (
    relation_type <> 'billing'
    OR allocated_amount IS NOT NULL
    OR allocated_hours IS NOT NULL
    OR allocation_pct IS NOT NULL
  ),
  CONSTRAINT ck_cpl_primary_basis CHECK (NOT is_primary OR relation_type = 'basis')
);

CREATE UNIQUE INDEX IF NOT EXISTS uq_cpl_semantic
  ON contract_project_links (tenant_id, contract_id, project_id, relation_type)
  WHERE deleted_at IS NULL AND status = 'active' AND relation_type <> 'billing';
CREATE UNIQUE INDEX IF NOT EXISTS uq_cpl_billing
  ON contract_project_links (tenant_id, contract_id, project_id, service_item_id)
  WHERE deleted_at IS NULL AND status = 'active' AND relation_type = 'billing';
CREATE UNIQUE INDEX IF NOT EXISTS uq_cpl_primary
  ON contract_project_links (tenant_id, project_id)
  WHERE is_primary AND relation_type = 'basis' AND status = 'active' AND deleted_at IS NULL;
CREATE INDEX IF NOT EXISTS idx_cpl_tenant_contract
  ON contract_project_links (tenant_id, contract_id, status);
CREATE INDEX IF NOT EXISTS idx_cpl_tenant_project
  ON contract_project_links (tenant_id, project_id, status);
CREATE UNIQUE INDEX IF NOT EXISTS uq_cpl_tenant_id
  ON contract_project_links (tenant_id, id);

-- ── 6. 同租户复合外键 ───────────────────────────────────────────────────────
DO $$
BEGIN
  IF NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conname = 'fk_csi_contract_tenant') THEN
    ALTER TABLE contract_service_items ADD CONSTRAINT fk_csi_contract_tenant
      FOREIGN KEY (tenant_id, contract_id) REFERENCES support_contracts (tenant_id, id);
  END IF;
  IF NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conname = 'fk_amend_contract_tenant') THEN
    ALTER TABLE contract_amendments ADD CONSTRAINT fk_amend_contract_tenant
      FOREIGN KEY (tenant_id, contract_id) REFERENCES support_contracts (tenant_id, id);
  END IF;
  IF NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conname = 'fk_usage_contract_tenant') THEN
    ALTER TABLE contract_usage_ledger ADD CONSTRAINT fk_usage_contract_tenant
      FOREIGN KEY (tenant_id, contract_id) REFERENCES support_contracts (tenant_id, id);
  END IF;
  IF NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conname = 'fk_usage_item_tenant') THEN
    ALTER TABLE contract_usage_ledger ADD CONSTRAINT fk_usage_item_tenant
      FOREIGN KEY (tenant_id, service_item_id) REFERENCES contract_service_items (tenant_id, id);
  END IF;
  IF NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conname = 'fk_cpl_contract_tenant') THEN
    ALTER TABLE contract_project_links ADD CONSTRAINT fk_cpl_contract_tenant
      FOREIGN KEY (tenant_id, contract_id) REFERENCES support_contracts (tenant_id, id);
  END IF;
  IF NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conname = 'fk_cpl_project_tenant') THEN
    ALTER TABLE contract_project_links ADD CONSTRAINT fk_cpl_project_tenant
      FOREIGN KEY (tenant_id, project_id) REFERENCES projects (tenant_id, id);
  END IF;
  IF NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conname = 'fk_cpl_service_item_tenant') THEN
    ALTER TABLE contract_project_links ADD CONSTRAINT fk_cpl_service_item_tenant
      FOREIGN KEY (tenant_id, service_item_id) REFERENCES contract_service_items (tenant_id, id);
  END IF;
END $$;

-- ── 7. 存量合同生成一条汇总服务项（M15，不自动建合同-项目链接） ─────────────
INSERT INTO contract_service_items
  (tenant_id, contract_id, item_code, item_name, billing_unit, quantity, period_count, unit_price, amount, status)
SELECT sc.tenant_id, sc.id, 'LEGACY-SUMMARY', '历史合同（未拆分）', 'project', 1, 1,
       COALESCE(sc.amount, 0), COALESCE(sc.amount, 0), 'active'
FROM support_contracts sc
WHERE sc.deleted_at IS NULL
  AND NOT EXISTS (
    SELECT 1 FROM contract_service_items csi
    WHERE csi.tenant_id = sc.tenant_id AND csi.contract_id = sc.id AND csi.deleted_at IS NULL
  );
