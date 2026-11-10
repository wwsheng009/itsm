-- 20261110_c2_read_permissions (down)
-- 回滚：删除本次为 C2 msp_* 角色补入的读权限。
-- 注意：若客户管理员在本次迁移后手工授予了同码，也会被删除；回滚前请确认。

DELETE FROM role_permissions rp
USING roles r, tenants t, permissions p
WHERE rp.role_id = r.id
  AND rp.permission_id = p.id
  AND rp.tenant_id = r.tenant_id
  AND t.id = r.tenant_id
  AND t.type IN ('msp_customer', 'saas_customer', 'customer')
  AND r.code IN ('msp_viewer', 'msp_tech', 'msp_specialist', 'msp_manager', 'msp_admin')
  AND p.code IN ('problem:read', 'change:read', 'service_request:read', 'incident:read', 'release:read');
