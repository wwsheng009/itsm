-- SLA 合同化专项 P2-2 结算报表（方案 Accepted v1.11 §13.5/§13.6）
--
-- 内容：
--   1) tickets.sla_matched_layer：建单时匹配层级快照（L1/L2/L3a/L3b/L3c/L4），
--      结算过滤依据（客户自建违约 L3* 不进合同结算；§13.6 违约基准过滤）。
--   2) sla_violations.matched_layer：违约行来源层级快照（来自工单建单时快照，
--      §13.6 "violation 行的 matched_layer 标识其来源基准"）。
--
-- 说明：
--   - 列由 ent Schema.Create 先行创建（AutoMigrate 先于 SQL 迁移，IM-3 惯例），
--     本文件为幂等兜底（对齐 P0/P1/P2-1 迁移双轨惯例）；
--   - 存量行 NULL：报表以"合同基准重算"兜底（双保险中的重算层，不误伤不误放）；
--   - 不涉及 RLS 变更（无新表）。

DO $$
BEGIN
    IF to_regclass('public.tickets') IS NULL THEN
        RAISE NOTICE 'sla 20261014e: skip tickets (table not found)';
    ELSE
        ALTER TABLE tickets ADD COLUMN IF NOT EXISTS sla_matched_layer VARCHAR(8);
        RAISE NOTICE 'sla 20261014e: tickets.sla_matched_layer ensured';
    END IF;

    IF to_regclass('public.sla_violations') IS NULL THEN
        RAISE NOTICE 'sla 20261014e: skip sla_violations (table not found)';
    ELSE
        ALTER TABLE sla_violations ADD COLUMN IF NOT EXISTS matched_layer VARCHAR(8);
        RAISE NOTICE 'sla 20261014e: sla_violations.matched_layer ensured';
    END IF;
END $$;
