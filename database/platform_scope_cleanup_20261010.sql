-- ============================================================
-- 平台治理域收口：存量收回（D1=A 执行，2026-10-10）
--
-- 设计文档：docs/multi-tenant/plan/platform-permission-shielding-design.md §8
-- 决策材料：docs/multi-tenant/plan/platform-permission-shielding-d1-decision.md §8
--
-- 背景：平台治理家族由 {system} 扩集为 {system, tenant, mcp}（D7 重估：MCP/向量库为平台资产；
-- tenant=租户治理属性）。非平台根租户的目录行与角色授权按 D1=A 直接收回（开发环境），
-- 开通克隆链已同步跳过（防再生，见 pkg/seeder/tenant_provisioner.go）。
--
-- 范围：非平台根租户（tenants.code <> 'default'，D4=B 兜底口径）；平台根（模板租户）保留。
--
-- 执行前置：备份受影响行（见 .tmp/platform_scope_cleanup_backup_20261010.json）
-- 执行后复验（期望 0 / 0 / 0，每非根租户）：
--   SELECT COUNT(*) FROM permissions WHERE tenant_id=<非根> AND resource IN ('system','tenant','mcp');
--   SELECT COUNT(*) FROM role_permissions rp JOIN permissions p ON p.id=rp.permission_id
--     WHERE rp.tenant_id=<非根> AND p.resource IN ('system','tenant','mcp');
--   SELECT COUNT(*) FROM user_roles ur JOIN roles r ON r.id=ur.role_id JOIN users u ON u.id=ur.user_id
--     WHERE u.tenant_id=<非根> AND r.code IN ('sysadmin','audit_admin','it_director','ops_director','security_admin');
-- ============================================================

BEGIN;

-- ① 授权收回：删除非平台根租户中引用平台治理家族码的授权行。
DELETE FROM role_permissions rp
USING permissions p, tenants t
WHERE rp.permission_id = p.id
  AND rp.tenant_id = t.id
  AND t.code <> 'default'
  AND p.resource IN ('system', 'tenant', 'mcp');

-- ② 目录行收回：删除非平台根租户的平台治理家族码行。
DELETE FROM permissions p
USING tenants t
WHERE p.tenant_id = t.id
  AND t.code <> 'default'
  AND p.resource IN ('system', 'tenant', 'mcp');

-- ③ 菜单行改挂平台码（全租户，含平台根）：仅平台角色持有，租户侧不再挂载。
UPDATE menus
SET permission_code = 'system:write'
WHERE path = '/admin/menus'
  AND permission_code <> 'system:write';

UPDATE menus
SET permission_code = 'system:read'
WHERE path = '/admin/vector-store'
  AND permission_code <> 'system:read';

COMMIT;
