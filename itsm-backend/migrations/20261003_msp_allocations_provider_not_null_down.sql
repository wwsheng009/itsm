-- IP-P2-1 回滚：provider_tenant_id 恢复可空（列与收窄索引保留；读侧兼容代码随代码回退）。
DO $$
BEGIN
  IF EXISTS (
    SELECT 1 FROM information_schema.columns
    WHERE table_schema = current_schema()
      AND table_name = 'msp_allocations'
      AND column_name = 'provider_tenant_id'
      AND is_nullable = 'NO'
  ) THEN
    ALTER TABLE msp_allocations ALTER COLUMN provider_tenant_id DROP NOT NULL;
  END IF;
END $$;
