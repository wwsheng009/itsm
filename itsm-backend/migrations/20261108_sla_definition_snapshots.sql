-- 2026-11-08 SLA 定义快照（Q-09 裁决 B / 020）
-- customer 租户只读 provider 下发的 SLA 条款；L1/provided 不再读 sla_definitions 原行。
-- 回滚：20261108_sla_definition_snapshots_down.sql

CREATE TABLE IF NOT EXISTS sla_definition_snapshots (
  id                   BIGSERIAL PRIMARY KEY,
  tenant_id            BIGINT NOT NULL,
  source_kind          VARCHAR(24) NOT NULL,
  source_contract_id   BIGINT,
  source_contract_no   VARCHAR(160),
  source_binding_id    BIGINT,
  source_grant_id      BIGINT,
  source_definition_id BIGINT NOT NULL,
  source_tenant_id     BIGINT NOT NULL,
  source_version       BIGINT NOT NULL,
  definition_name      VARCHAR(255) NOT NULL,
  description          TEXT,
  service_type         VARCHAR(64),
  priority             VARCHAR(32),
  ticket_type_id       BIGINT,
  terms                JSONB NOT NULL DEFAULT '{}'::jsonb,
  effective_from       TIMESTAMPTZ NOT NULL,
  effective_until      TIMESTAMPTZ,
  status               VARCHAR(16) NOT NULL DEFAULT 'active',
  payload_hash         VARCHAR(64) NOT NULL,
  synced_at            TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  created_at           TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  updated_at           TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  CONSTRAINT ck_sds_source_kind CHECK (source_kind IN ('contract_binding','grant')),
  CONSTRAINT ck_sds_status CHECK (status IN ('active','superseded','expired','revoked')),
  CONSTRAINT ck_sds_terms CHECK (jsonb_typeof(terms) = 'object'),
  CONSTRAINT ck_sds_time CHECK (effective_until IS NULL OR effective_until > effective_from)
);

CREATE INDEX IF NOT EXISTS idx_sds_tenant_effective
  ON sla_definition_snapshots (tenant_id, source_kind, status, effective_from, effective_until);
CREATE INDEX IF NOT EXISTS idx_sds_contract
  ON sla_definition_snapshots (tenant_id, source_contract_id, status);
CREATE INDEX IF NOT EXISTS idx_sds_ticket_type
  ON sla_definition_snapshots (tenant_id, ticket_type_id, status);

CREATE UNIQUE INDEX IF NOT EXISTS uk_sds_contract_version
  ON sla_definition_snapshots (tenant_id, source_contract_id, source_binding_id, source_version)
  WHERE source_kind = 'contract_binding'
    AND source_contract_id IS NOT NULL
    AND source_binding_id IS NOT NULL;

CREATE UNIQUE INDEX IF NOT EXISTS uk_sds_grant_version
  ON sla_definition_snapshots (tenant_id, source_grant_id, source_version)
  WHERE source_kind = 'grant'
    AND source_grant_id IS NOT NULL;
