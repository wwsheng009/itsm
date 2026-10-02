-- IP-P2-1 回滚：移除 provider 维度列与索引（数据变更保留策略下亦仅用于开发/应急）
DROP INDEX IF EXISTS idx_msp_allocations_provider;
ALTER TABLE msp_allocations DROP COLUMN IF EXISTS provider_tenant_id;
