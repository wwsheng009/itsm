-- IP-P2-1 / §5.0-A：msp_allocations.provider_tenant_id（provider 维度）在线加列 + 回填 + 索引
-- 策略：只增不删；字段先可空（回滚点），差异清单归零 + N=1/N=2 e2e 全绿后再单独迁移置 NOT NULL。
-- 幂等：可重复执行。

ALTER TABLE msp_allocations ADD COLUMN IF NOT EXISTS provider_tenant_id int NULL REFERENCES tenants(id);

-- 回填（幂等）：provider = 客户租户的 msp_provider_id（直客为 0/NULL 的客户不产生合法分配）
UPDATE msp_allocations a
   SET provider_tenant_id = c.msp_provider_id
  FROM tenants c
 WHERE c.id = a.customer_tenant_id
   AND a.provider_tenant_id IS NULL
   AND c.msp_provider_id IS NOT NULL
   AND c.msp_provider_id <> 0;

-- 收窄索引：活跃分配按 provider 查询（与 ent schema StorageKey 同源）
CREATE INDEX IF NOT EXISTS idx_msp_allocations_provider
  ON msp_allocations (provider_tenant_id) WHERE deassigned_at IS NULL;

-- 差异清单（人工复核；NOT NULL 收尾前必须为 0）：见 scripts/msp/verify-allocation-provider-backfill.sql
