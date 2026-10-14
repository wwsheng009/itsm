-- 回滚 20261014e：移除匹配层级快照两列（报表回退纯重算口径；无常量数据迁移）
DO $$
BEGIN
    IF to_regclass('public.tickets') IS NOT NULL THEN
        ALTER TABLE tickets DROP COLUMN IF EXISTS sla_matched_layer;
        RAISE NOTICE 'sla 20261014e down: tickets.sla_matched_layer dropped';
    END IF;

    IF to_regclass('public.sla_violations') IS NOT NULL THEN
        ALTER TABLE sla_violations DROP COLUMN IF EXISTS matched_layer;
        RAISE NOTICE 'sla 20261014e down: sla_violations.matched_layer dropped';
    END IF;
END $$;
