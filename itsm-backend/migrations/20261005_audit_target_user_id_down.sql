-- TUM-3 回滚：移除审计目标用户列与索引（幂等）。
DROP INDEX IF EXISTS idx_audit_target_user;
ALTER TABLE audit_logs DROP COLUMN IF EXISTS target_user_id;
