-- IP-P2-1 收尾（§5.0-A）：msp_allocations.provider_tenant_id 置 NOT NULL（单独迁移）。
-- 前置条件（2026-10-03 复核满足）：回填差异清单为零（scripts/msp/verify-allocation-provider-backfill.sql
-- 4/4 为 0）+ N=1/N=2 e2e 全绿（router/msp_a11_a12_e2e_test.go，v1.39）。
-- 幂等：已为 NOT NULL 则跳过；残余 NULL 先按客户归属再回填一次，仍存在则显式报错阻断（先处理巡检差异）。
-- 回滚：_down.sql 恢复可空（列/索引保留）。

DO $$
BEGIN
  IF EXISTS (
    SELECT 1 FROM information_schema.columns
    WHERE table_schema = current_schema()
      AND table_name = 'msp_allocations'
      AND column_name = 'provider_tenant_id'
      AND is_nullable = 'NO'
  ) THEN
    RAISE NOTICE 'msp_allocations.provider_tenant_id 已是 NOT NULL，跳过';
    RETURN;
  END IF;

  UPDATE msp_allocations a
     SET provider_tenant_id = c.msp_provider_id
    FROM tenants c
   WHERE c.id = a.customer_tenant_id
     AND a.provider_tenant_id IS NULL
     AND c.msp_provider_id IS NOT NULL
     AND c.msp_provider_id <> 0;

  IF EXISTS (SELECT 1 FROM msp_allocations WHERE provider_tenant_id IS NULL) THEN
    RAISE EXCEPTION 'provider_tenant_id 仍存在未回填行，NOT NULL 收尾被阻断：请先处理 scripts/msp/verify-allocation-provider-backfill.sql 差异清单';
  END IF;

  ALTER TABLE msp_allocations ALTER COLUMN provider_tenant_id SET NOT NULL;
END $$;
