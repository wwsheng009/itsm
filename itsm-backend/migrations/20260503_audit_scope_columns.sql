-- IP-P0-10 审计作用域扩展列（实施方案 §3.0-B3；在线加列，全部可空）
-- 生产建议逐条 CONCURRENTLY 建索引；本文件为幂等版本，可重复执行。

ALTER TABLE audit_logs ADD COLUMN IF NOT EXISTS actor_account varchar(64);
ALTER TABLE audit_logs ADD COLUMN IF NOT EXISTS membership_id bigint;
ALTER TABLE audit_logs ADD COLUMN IF NOT EXISTS target_tenant_id int;
ALTER TABLE audit_logs ADD COLUMN IF NOT EXISTS source varchar(32);

-- 按作用域查询：租户 × 目标租户 × 时间
CREATE INDEX IF NOT EXISTS idx_audit_scope ON audit_logs (tenant_id, target_tenant_id, created_at);

-- 历史行 source 保持 NULL（读侧映射 legacy），不做回填。
