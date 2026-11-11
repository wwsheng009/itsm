-- 20261111_service_request_delete_grants
-- 产品决策 D10：租户管理员可删除服务请求。
-- 为存量租户补齐 admin / provider_tenant_admin 的 service_request:delete；
-- 新租户由 authz.BuiltinRolePermissionCodes + role permission sync 自动获得。
-- 幂等：NOT EXISTS 去重。

INSERT INTO role_permissions (role_id, permission_id, tenant_id)
SELECT r.id, p.id, r.tenant_id
FROM roles r
JOIN permissions p ON p.tenant_id = r.tenant_id
WHERE r.code IN ('admin', 'provider_tenant_admin')
  AND p.code = 'service_request:delete'
  AND NOT EXISTS (
      SELECT 1
      FROM role_permissions rp
      WHERE rp.role_id = r.id
        AND rp.permission_id = p.id
        AND rp.tenant_id = r.tenant_id
  );
