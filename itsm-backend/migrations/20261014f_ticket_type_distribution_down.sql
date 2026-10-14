-- P2-3 D-TT-1 rollback（20261014f_ticket_type_distribution.sql）
--
-- 说明：表结构由 ent Schema.Create 管理（回退需同步回退 ent schema 定义）；
-- 本文件回滚本迁移创建的索引与 ticket_types 的 6 个下发字段。

DROP INDEX IF EXISTS uk_ticket_type_distributions_copy;
DROP INDEX IF EXISTS idx_ticket_type_distributions_tenant_provider;
DROP INDEX IF EXISTS idx_ticket_type_distributions_source;

DO $$
BEGIN
    IF to_regclass('public.ticket_types') IS NOT NULL THEN
        ALTER TABLE ticket_types DROP COLUMN IF EXISTS origin;
        ALTER TABLE ticket_types DROP COLUMN IF EXISTS origin_provider_tenant_id;
        ALTER TABLE ticket_types DROP COLUMN IF EXISTS origin_type_id;
        ALTER TABLE ticket_types DROP COLUMN IF EXISTS controlled_by_provider;
        ALTER TABLE ticket_types DROP COLUMN IF EXISTS sync_status;
        ALTER TABLE ticket_types DROP COLUMN IF EXISTS last_synced_at;
        RAISE NOTICE 'sla 20261014f down: ticket_types distribution columns dropped';
    END IF;
END $$;
