-- 20260930_add_users_preferences.sql
-- IP-P1-6c：用户服务端偏好（过滤器/排序等），仅本人可读写。
-- 说明：jsonb + 非空默认，在线加列（PG11+ 常量默认值无重写表）。
ALTER TABLE users ADD COLUMN IF NOT EXISTS preferences jsonb NOT NULL DEFAULT '{}'::jsonb;

COMMENT ON COLUMN users.preferences IS '用户偏好（IP-P1-6c）：workbenchFilter 等白名单键；仅本人可读写';
