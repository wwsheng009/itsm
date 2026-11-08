-- 客户主数据 P1（设计文档：docs/customer-master-data/01 §5.1/§5.4、02 §4/§6、§9）
--
-- 范围：
--   1. customer_relationships（客户关系网）
--   2. project_customer_links（项目 ↔ 客户，同租户闭环，遵守 D-01）
--   3. projects(tenant_id,id) 复合唯一键
--   4. 同租户复合外键
--
-- 全部语句可重复执行；不包含顶层事务控制。

-- ── 1. customer_relationships ─────────────────────────────────────────────
CREATE TABLE IF NOT EXISTS customer_relationships (
  id               BIGSERIAL PRIMARY KEY,
  tenant_id        BIGINT NOT NULL,
  from_customer_id BIGINT NOT NULL REFERENCES service_customers(id),
  to_customer_id   BIGINT NOT NULL REFERENCES service_customers(id),
  relation_type    VARCHAR(32) NOT NULL,
  relation_role    VARCHAR(16) NOT NULL DEFAULT 'outbound',
  is_bidirectional BOOLEAN NOT NULL DEFAULT FALSE,
  effective_from   DATE,
  effective_to     DATE,
  note             VARCHAR(500),
  status           VARCHAR(16) NOT NULL DEFAULT 'active',
  deleted_at       TIMESTAMPTZ,
  created_by       BIGINT,
  created_at       TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  updated_at       TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  CONSTRAINT ck_rel_not_self CHECK (from_customer_id <> to_customer_id)
);

CREATE UNIQUE INDEX IF NOT EXISTS uq_rel_pair_type
  ON customer_relationships (tenant_id, from_customer_id, to_customer_id, relation_type)
  WHERE status = 'active' AND deleted_at IS NULL;

CREATE INDEX IF NOT EXISTS idx_rel_tenant_from
  ON customer_relationships (tenant_id, from_customer_id, status);

CREATE INDEX IF NOT EXISTS idx_rel_tenant_to
  ON customer_relationships (tenant_id, to_customer_id, status);

CREATE UNIQUE INDEX IF NOT EXISTS uq_rel_tenant_id
  ON customer_relationships (tenant_id, id);

-- ── 2. project_customer_links ─────────────────────────────────────────────
CREATE TABLE IF NOT EXISTS project_customer_links (
  id            BIGSERIAL PRIMARY KEY,
  tenant_id     BIGINT NOT NULL,
  project_id    BIGINT NOT NULL,
  customer_id   BIGINT NOT NULL REFERENCES service_customers(id),
  branch_id     BIGINT REFERENCES customer_branches(id),
  relation_type VARCHAR(24) NOT NULL DEFAULT 'delivery',
  is_primary    BOOLEAN NOT NULL DEFAULT FALSE,
  note          VARCHAR(500),
  deleted_at    TIMESTAMPTZ,
  created_by    BIGINT,
  created_at    TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  updated_at    TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE UNIQUE INDEX IF NOT EXISTS uq_pclink
  ON project_customer_links (tenant_id, project_id, customer_id, relation_type)
  WHERE deleted_at IS NULL;

CREATE UNIQUE INDEX IF NOT EXISTS uq_pclink_primary
  ON project_customer_links (tenant_id, project_id)
  WHERE is_primary AND deleted_at IS NULL;

CREATE INDEX IF NOT EXISTS idx_pclink_tenant_customer
  ON project_customer_links (tenant_id, customer_id);

CREATE UNIQUE INDEX IF NOT EXISTS uq_pclink_tenant_id
  ON project_customer_links (tenant_id, id);

-- ── 3. 父端复合唯一键与复合外键 ───────────────────────────────────────────
CREATE UNIQUE INDEX IF NOT EXISTS uq_project_tenant_id
  ON projects (tenant_id, id);

DO $$
BEGIN
  IF NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conname = 'fk_rel_from_tenant') THEN
    ALTER TABLE customer_relationships
      ADD CONSTRAINT fk_rel_from_tenant
      FOREIGN KEY (tenant_id, from_customer_id)
      REFERENCES service_customers (tenant_id, id);
  END IF;
  IF NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conname = 'fk_rel_to_tenant') THEN
    ALTER TABLE customer_relationships
      ADD CONSTRAINT fk_rel_to_tenant
      FOREIGN KEY (tenant_id, to_customer_id)
      REFERENCES service_customers (tenant_id, id);
  END IF;
END $$;

DO $$
BEGIN
  IF NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conname = 'fk_pcl_project_tenant') THEN
    ALTER TABLE project_customer_links
      ADD CONSTRAINT fk_pcl_project_tenant
      FOREIGN KEY (tenant_id, project_id)
      REFERENCES projects (tenant_id, id);
  END IF;
  IF NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conname = 'fk_pcl_customer_tenant') THEN
    ALTER TABLE project_customer_links
      ADD CONSTRAINT fk_pcl_customer_tenant
      FOREIGN KEY (tenant_id, customer_id)
      REFERENCES service_customers (tenant_id, id);
  END IF;
  IF NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conname = 'fk_pcl_branch_tenant') THEN
    ALTER TABLE project_customer_links
      ADD CONSTRAINT fk_pcl_branch_tenant
      FOREIGN KEY (tenant_id, customer_id, branch_id)
      REFERENCES customer_branches (tenant_id, customer_id, id);
  END IF;
END $$;
