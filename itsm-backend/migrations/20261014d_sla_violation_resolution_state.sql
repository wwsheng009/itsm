-- SLA 合同化专项 P2-1 违约归责状态机（D-SLA-8，方案 §13.1）
--
-- sla_violations 增列兜底（正常路径由 ent Schema.Create 完成；本文件保证 SQL-only 环境一致）。
-- 语义：
--   resolution_state: pending|accepted|disputed|waived（默认 pending；accepted/waived 为终态）；
--   dispute_reason / waived_reason：对应状态的必填理由（应用层校验）；
--   waived_by / waived_at：豁免操作留痕；
--   settled_flag：已进结算快照标记（true 后随快照锁定，禁止任何状态变更——§13.3）。

ALTER TABLE sla_violations ADD COLUMN IF NOT EXISTS resolution_state VARCHAR(20) NOT NULL DEFAULT 'pending';
ALTER TABLE sla_violations ADD COLUMN IF NOT EXISTS dispute_reason TEXT;
ALTER TABLE sla_violations ADD COLUMN IF NOT EXISTS waived_reason TEXT;
ALTER TABLE sla_violations ADD COLUMN IF NOT EXISTS waived_by INTEGER;
ALTER TABLE sla_violations ADD COLUMN IF NOT EXISTS waived_at TIMESTAMPTZ;
ALTER TABLE sla_violations ADD COLUMN IF NOT EXISTS settled_flag BOOLEAN NOT NULL DEFAULT FALSE;
