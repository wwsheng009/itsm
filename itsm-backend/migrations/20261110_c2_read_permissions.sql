-- 20261110_c2_read_permissions
-- 目的：补齐客户租户内 msp_* 角色的读面权限（C2 基线）。
--
-- 背景：深切换后 provider 员工在客户租户内使用 /problems、/changes、
-- /service-requests、/incidents、/releases 列表；这些路由要求 problem:read /
-- change:read / service_request:read / incident:read / release:read。
-- 此前 C2 只给了 problem:write / change:write / service_request:write，
-- 导致 GET 列表 403（code 2003），但统计/详情仍可能被误判为“有数字无列表”。
--
-- 范围：仅客户类型租户的 5 个 msp_* 角色；幂等（NOT EXISTS 去重）。
-- 回滚：20261110_c2_read_permissions_down.sql 删除本次补入的授权。

INSERT INTO role_permissions (role_id, permission_id, tenant_id)
SELECT r.id, p.id, r.tenant_id
FROM roles r
JOIN tenants t ON t.id = r.tenant_id
JOIN permissions p ON p.tenant_id = r.tenant_id
WHERE t.type IN ('msp_customer', 'saas_customer', 'customer')
  AND r.code IN ('msp_viewer', 'msp_tech', 'msp_specialist', 'msp_manager', 'msp_admin')
  AND p.code IN ('problem:read', 'change:read', 'service_request:read', 'incident:read', 'release:read')
  AND NOT EXISTS (
      SELECT 1
      FROM role_permissions rp
      WHERE rp.role_id = r.id
        AND rp.permission_id = p.id
        AND rp.tenant_id = r.tenant_id
  );
