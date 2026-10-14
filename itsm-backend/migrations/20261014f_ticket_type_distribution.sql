-- SLA 合同化专项 P2-3 D-TT-1 工单类型受控下发（方案 Accepted v1.11 §4.5a）
--
-- 内容：
--   1) ticket_types 增 6 列（origin 三元组 + 受控锁 + 同步状态）——列由 ent Schema.Create
--      先行创建，本文件为幂等兜底（对齐 P0/P1/P2-1/P2-2 双轨惯例）；
--   2) ticket_type_distributions（表由 ent 创建）索引兜底：副本唯一 + provider 收窄 + 源检索。
--
-- RLS：ticket_type_distributions 策略见 database/rls/migrations/013_ticket_type_distribution_policies.sql
--（行归属 customer 租户；provider 访问 = mspguard 授权 + 客户作用域重绑定 ctx，§4.5a"零跨租户读"）。

DO $$
BEGIN
    IF to_regclass('public.ticket_types') IS NULL THEN
        RAISE NOTICE 'sla 20261014f: skip ticket_types (table not found)';
    ELSE
        ALTER TABLE ticket_types ADD COLUMN IF NOT EXISTS origin VARCHAR(16) NOT NULL DEFAULT 'native';
        ALTER TABLE ticket_types ADD COLUMN IF NOT EXISTS origin_provider_tenant_id BIGINT NOT NULL DEFAULT 0;
        ALTER TABLE ticket_types ADD COLUMN IF NOT EXISTS origin_type_id BIGINT NOT NULL DEFAULT 0;
        ALTER TABLE ticket_types ADD COLUMN IF NOT EXISTS controlled_by_provider BOOLEAN NOT NULL DEFAULT FALSE;
        ALTER TABLE ticket_types ADD COLUMN IF NOT EXISTS sync_status VARCHAR(16) NOT NULL DEFAULT '';
        ALTER TABLE ticket_types ADD COLUMN IF NOT EXISTS last_synced_at TIMESTAMPTZ;
        RAISE NOTICE 'sla 20261014f: ticket_types distribution columns ensured';
    END IF;

    IF to_regclass('public.ticket_type_distributions') IS NULL THEN
        RAISE NOTICE 'sla 20261014f: skip distribution indexes (table not found)';
    ELSE
        CREATE UNIQUE INDEX IF NOT EXISTS uk_ticket_type_distributions_copy
            ON ticket_type_distributions (copy_type_id);
        CREATE INDEX IF NOT EXISTS idx_ticket_type_distributions_tenant_provider
            ON ticket_type_distributions (tenant_id, provider_tenant_id);
        CREATE INDEX IF NOT EXISTS idx_ticket_type_distributions_source
            ON ticket_type_distributions (source_type_id);
        RAISE NOTICE 'sla 20261014f: distribution indexes ensured';
    END IF;
END $$;
