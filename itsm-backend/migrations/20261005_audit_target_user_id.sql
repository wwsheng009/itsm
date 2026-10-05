-- TUM-3 审计按用户时间线：audit_logs 新增可空列 target_user_id + 复合索引。
-- 历史行保持 NULL（读侧按 legacy 处理，不回溯）；本文件幂等，可重复执行。
-- 方案：docs/multi-tenant/plan/msp-tenant-user-management-enhancement-plan.md §3.4（TUM-3）。

ALTER TABLE audit_logs ADD COLUMN IF NOT EXISTS target_user_id int;

-- 按目标租户 × 目标用户 × 时间 查询（用户治理时间线）
CREATE INDEX IF NOT EXISTS idx_audit_target_user ON audit_logs (target_tenant_id, target_user_id, created_at);
