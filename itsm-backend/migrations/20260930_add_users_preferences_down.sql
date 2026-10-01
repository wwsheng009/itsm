-- 20260930_add_users_preferences_down.sql
-- 回滚：删除偏好列（前端偏好回退 URL/localStorage，不阻塞）。
ALTER TABLE users DROP COLUMN IF EXISTS preferences;
