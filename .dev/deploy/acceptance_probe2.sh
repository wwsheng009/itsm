#!/usr/bin/env bash
# 238 demo 验收探针 P2：RBAC 权限映射与 MSP 角色（只读）
set -u
DB=itsm_demo
PSQL="docker exec itsm-postgres-prod psql -U itsm -d $DB"

echo "== ROLES(msp/provider) =="
$PSQL -c "SELECT id, tenant_id, code, name FROM roles WHERE code LIKE 'msp%' OR code LIKE 'provider%' ORDER BY id LIMIT 40"
echo "== PERMS(sla/msp) =="
$PSQL -c "SELECT id, code FROM permissions WHERE code LIKE 'sla%' OR code LIKE 'msp%' ORDER BY code LIMIT 120"
echo "== ROLE-PERMS(msp roles ∩ sla/msp) =="
$PSQL -c "SELECT r.code AS role, p.code AS perm FROM role_permissions rp JOIN roles r ON r.id=rp.role_id JOIN permissions p ON p.id=rp.permission_id WHERE (r.code LIKE 'msp%' OR r.code LIKE 'provider%') AND (p.code LIKE 'sla%' OR p.code LIKE 'msp%') ORDER BY 1,2 LIMIT 200"
echo "== USERS(t2) =="
$PSQL -c "SELECT id, username, role, msp_role FROM users WHERE tenant_id=2 ORDER BY id"
echo "== PROBE2-DONE =="
