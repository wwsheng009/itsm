#!/usr/bin/env bash
# 238 demo 验收探针 P5：权限目录/角色映射/用户角色 精确核对（只读）
set -u
DB=itsm_demo
PSQL="docker exec itsm-postgres-prod psql -U itsm -d $DB"

echo "== ROLE-LIKE-TABLES =="
$PSQL -tAc "SELECT table_name FROM information_schema.tables WHERE table_schema='public' AND table_name LIKE '%role%' ORDER BY table_name" | tr '\n' ' '; echo
echo "== USER_ROLE-TABLE-SCHEMA =="
$PSQL -tAc "SELECT table_name||'.'||column_name||':'||data_type FROM information_schema.columns WHERE table_name LIKE 'user%role%' ORDER BY table_name, ordinal_position" | tr '\n' ' '; echo
echo "== PERMISSIONS-SCHEMA =="
$PSQL -tAc "SELECT column_name||':'||data_type FROM information_schema.columns WHERE table_name='permissions' ORDER BY ordinal_position" | tr '\n' ' '; echo
echo "== ROLE_PERMISSIONS-SCHEMA =="
$PSQL -tAc "SELECT column_name||':'||data_type FROM information_schema.columns WHERE table_name='role_permissions' ORDER BY ordinal_position" | tr '\n' ' '; echo
echo "== MISSING-SLA-CODES(t2) =="
$PSQL -c "SELECT code, id AS t1_id FROM permissions p1 WHERE tenant_id=1 AND code IN ('sla:grant:read','sla:grant:write','sla:contract:read','sla:contract:write','sla:settlement:read','sla:violation:settle') AND NOT EXISTS (SELECT 1 FROM permissions p2 WHERE p2.tenant_id=2 AND p2.code=p1.code)"
echo "== T2-PERMS-SLA =="
$PSQL -c "SELECT id, code FROM permissions WHERE tenant_id=2 AND code LIKE 'sla%' ORDER BY code"
echo "== T2-ROLE46-PERMS(sla) =="
$PSQL -c "SELECT p.code FROM role_permissions rp JOIN permissions p ON p.id=rp.permission_id WHERE rp.role_id=46 AND p.code LIKE 'sla%' ORDER BY p.code"
echo "== USER_ROLES-ROWS =="
$PSQL -c "SELECT * FROM user_roles LIMIT 24" 2>&1 | head -16
echo "== PROVIDER-DEFS(t2) =="
$PSQL -c "SELECT id, name, service_type, priority, response_time, resolution_time, is_active FROM sla_definitions WHERE tenant_id=2 ORDER BY id LIMIT 30"
echo "== PROBE5-DONE =="
