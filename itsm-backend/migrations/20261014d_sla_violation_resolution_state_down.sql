-- SLA 合同化专项 P2-1 rollback（20261014d_sla_violation_resolution_state.sql）
--
-- 警告：列回滚会丢失归责状态/争议/豁免留痕数据；且 ent Schema.Create 下次启动
-- 会按 schema 定义重建列（本回滚供紧急撤销使用，非对称回滚为仓库惯例）。

ALTER TABLE sla_violations DROP COLUMN IF EXISTS resolution_state;
ALTER TABLE sla_violations DROP COLUMN IF EXISTS dispute_reason;
ALTER TABLE sla_violations DROP COLUMN IF EXISTS waived_reason;
ALTER TABLE sla_violations DROP COLUMN IF EXISTS waived_by;
ALTER TABLE sla_violations DROP COLUMN IF EXISTS waived_at;
ALTER TABLE sla_violations DROP COLUMN IF EXISTS settled_flag;
