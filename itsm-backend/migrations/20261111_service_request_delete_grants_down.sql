-- 回滚 D10：删除 admin / provider_tenant_admin 的 service_request:delete 授权。
DELETE FROM role_permissions rp
USING roles r, permissions p
WHERE rp.role_id = r.id
  AND rp.permission_id = p.id
  AND rp.tenant_id = r.tenant_id
  AND r.code IN ('admin', 'provider_tenant_admin')
  AND p.code = 'service_request:delete';
