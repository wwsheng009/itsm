-- 20260508 users 首登/活跃租户列（IP-P1-5）
-- 背景：bootstrap 管理员首登强制改密（must_change_password）+ 最近活跃租户留痕
--       （last_active_tenant_id，登录/切换时更新）。对应 P0 DDL 清单 B1。
-- 幂等：在线加列，可空 / 默认 false。

ALTER TABLE users ADD COLUMN IF NOT EXISTS last_active_tenant_id int NULL REFERENCES tenants(id);
ALTER TABLE users ADD COLUMN IF NOT EXISTS must_change_password boolean NOT NULL DEFAULT false;
