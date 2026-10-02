-- IP-P2-4a：workbench_views 工作台自定义视图（保存的过滤器组合，支持同 provider 分享）
-- 幂等可重放：建表/索引 IF NOT EXISTS。
-- 可见性由服务层收敛：owner 全部可见；is_shared 对同 provider 租户可见（只读）。
CREATE TABLE IF NOT EXISTS workbench_views (
id            bigserial PRIMARY KEY,
tenant_id     int    NOT NULL REFERENCES tenants(id),
owner_user_id bigint NOT NULL REFERENCES users(id),
name          varchar(60) NOT NULL,
filters       jsonb   NOT NULL DEFAULT '{}'::jsonb,
is_shared     boolean NOT NULL DEFAULT false,
is_default    boolean NOT NULL DEFAULT false,
created_at    timestamptz NOT NULL DEFAULT now(),
updated_at    timestamptz NOT NULL DEFAULT now()
);
-- 同一 owner 视图名唯一
CREATE UNIQUE INDEX IF NOT EXISTS uq_workbench_views_owner_name
ON workbench_views (tenant_id, owner_user_id, name);
-- 每 owner 至多一个默认视图（部分唯一）
CREATE UNIQUE INDEX IF NOT EXISTS uq_workbench_views_owner_default
ON workbench_views (owner_user_id) WHERE is_default;
-- 查询辅助：按 provider 取分享视图 / 按 owner 列默认
CREATE INDEX IF NOT EXISTS idx_workbench_views_shared ON workbench_views (tenant_id, is_shared);
CREATE INDEX IF NOT EXISTS idx_workbench_views_owner  ON workbench_views (owner_user_id, is_default);
