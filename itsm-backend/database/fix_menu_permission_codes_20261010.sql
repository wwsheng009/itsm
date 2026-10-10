-- fix_menu_permission_codes_20261010.sql
-- 菜单挂载收口（2026-10-10）：菜单 permission_code 对齐页面入口守卫/主接口，
-- 配合 service/menu_service.go 的严格匹配（不再做 action/resource 别名映射）：
--   - 此前 system:write 经别名（system→system_config）误命中 system_config:write，
--     使租户管理员（admin/provider_tenant_admin）看到 /admin/tenants 等平台面入口；
--   - 存量租户的菜单码按下表对齐（pkg/seeder/seeder.go 同步更新，新租户自动生效）。
-- 幂等：仅更新仍为旧码的目标行。

UPDATE menus SET permission_code = 'service_catalog:read'
WHERE path IN ('/service-catalog', '/service-catalog/approvals') AND permission_code = 'service:read';

UPDATE menus SET permission_code = 'system_config:write'
WHERE path IN ('/admin/vector-store', '/admin/system-config') AND permission_code = 'system:read';

UPDATE menus SET permission_code = 'system_config:write'
WHERE path = '/admin/menus' AND permission_code = 'system:write';

UPDATE menus SET permission_code = 'team:write'
WHERE path = '/admin/teams' AND permission_code = 'team:read';
