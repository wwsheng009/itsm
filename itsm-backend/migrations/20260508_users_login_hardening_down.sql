-- 20260508 回滚：users 首登/活跃租户列（IP-P1-5）
ALTER TABLE users DROP COLUMN IF EXISTS must_change_password;
ALTER TABLE users DROP COLUMN IF EXISTS last_active_tenant_id;
