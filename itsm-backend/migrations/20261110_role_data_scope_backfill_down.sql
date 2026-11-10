-- 20261110_role_data_scope_backfill (down)
-- 恢复内置角色的旧默认 data_scope=all。
-- 说明：is_system 标记为前向元数据修正，不在 down 中回退，避免误伤既有系统角色。

UPDATE roles
SET data_scope = 'all',
    updated_at = now()
WHERE code IN (
    'agent', 'technician', 'end_user', 'guest',
    'it_admin', 'security_admin', 'audit_admin',
    'it_director', 'ops_director', 'ops_manager', 'ops_engineer',
    'dba', 'network_eng', 'l3_expert', 'change_manager', 'service_catalog_admin',
    'dept_manager', 'sd_manager', 'l1_support', 'l2_support'
)
AND data_scope <> 'all';
