#!/usr/bin/env bash
# 探针 P9：role46（provider_tenant_admin）权限样本 + msp_manager 对照（只读）
set -u
DB=itsm_demo
PSQL="docker exec itsm-postgres-prod psql -U itsm -d $DB"

echo "== ROLE46-COUNT =="
$PSQL -tAc "SELECT count(*) FROM role_permissions WHERE role_id=46"
echo "== ROLE46-ADMIN-CODES =="
$PSQL -tAc "SELECT p.code FROM role_permissions rp JOIN permissions p ON p.id=rp.permission_id WHERE rp.role_id=46 AND p.code IN ('user:write','role:write','system_config:write','system_config:read','department:create','department:update','group:write','ticket_type:manage','team:write','sla:write','sla:read','workflow:write','connector:write','approval:write') ORDER BY p.code"
echo "== ROLE46-SAMPLE30 =="
$PSQL -tAc "SELECT p.code FROM role_permissions rp JOIN permissions p ON p.id=rp.permission_id WHERE rp.role_id=46 ORDER BY p.code LIMIT 30"
echo "== P9-DONE =="
