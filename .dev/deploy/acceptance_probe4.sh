#!/usr/bin/env bash
# 238 demo 验收探针 P4：msp_manager 角色-权限精确核对（只读）
set -u
DB=itsm_demo
PSQL="docker exec itsm-postgres-prod psql -U itsm -d $DB"

echo "== MSP_MANAGER-ROLE-ROWS =="
$PSQL -c "SELECT id, tenant_id, code FROM roles WHERE code='msp_manager' ORDER BY tenant_id"
echo "== ROLE44(t2)-PERMS(sla:*) =="
$PSQL -c "SELECT p.code FROM role_permissions rp JOIN permissions p ON p.id=rp.permission_id WHERE rp.role_id=44 AND p.code LIKE 'sla%' ORDER BY p.code"
echo "== ROLE44-ALL-COUNT =="
$PSQL -c "SELECT count(*) FROM role_permissions WHERE role_id=44"
echo "== MSP_MANAGER-PERMS-BY-TENANT(contract/grant/settlement) =="
$PSQL -c "SELECT r.tenant_id, p.code, count(*) FROM role_permissions rp JOIN roles r ON r.id=rp.role_id JOIN permissions p ON p.id=rp.permission_id WHERE r.code='msp_manager' AND p.code IN ('sla:contract:read','sla:contract:write','sla:grant:read','sla:grant:write','sla:settlement:read','sla:violation:settle') GROUP BY 1,2 ORDER BY 1,2"
echo "== PERM-ROWS(contract) =="
$PSQL -c "SELECT id, tenant_id, code FROM permissions WHERE code IN ('sla:contract:read','sla:contract:write') ORDER BY code, tenant_id"
echo "== RLS-MODE-PERMISSION-CFG(env hints) =="
grep -iE 'PERMISSION|RBAC' /home/n9e/itsm/repo/.env.demo || echo "(none in .env.demo)"
echo "== PROBE4-DONE =="
