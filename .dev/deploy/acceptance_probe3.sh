#!/usr/bin/env bash
# 238 demo 验收探针 P3：角色分配表 + provider 定义清单 + SLA 列（只读）
set -u
DB=itsm_demo
PSQL="docker exec itsm-postgres-prod psql -U itsm -d $DB"

echo "== ROLE-TABLES =="
$PSQL -tAc "SELECT table_name FROM information_schema.tables WHERE table_name LIKE '%role%' ORDER BY table_name" | tr '\n' ' '; echo
echo "== USER_ROLES-SCHEMA =="
$PSQL -tAc "SELECT table_name||'.'||column_name FROM information_schema.columns WHERE table_name IN ('user_roles','user_role_assignments','role_users') ORDER BY table_name, ordinal_position" | tr '\n' ' '; echo
echo "== MANAGER-ROLE-ROWS =="
$PSQL -c "SELECT * FROM user_roles WHERE user_id IN (1,2) LIMIT 30" 2>&1 | head -20
echo "== PROVIDER-DEFS(t2) =="
$PSQL -c "SELECT id, name, service_type, priority, response_time, resolution_time, is_active, owner_provider_tenant_id FROM sla_definitions WHERE tenant_id=2 ORDER BY id LIMIT 30"
echo "== DEF-COUNT-BY-TENANT =="
$PSQL -c "SELECT tenant_id, count(*), count(*) FILTER (WHERE owner_provider_tenant_id>0) AS owner_set FROM sla_definitions GROUP BY tenant_id ORDER BY tenant_id"
echo "== SLA-COLUMNS =="
$PSQL -tAc "SELECT table_name||'.'||column_name FROM information_schema.columns WHERE table_name IN ('tickets','incidents') AND column_name LIKE 'sla%' ORDER BY table_name, column_name"
echo "== PROBE3-DONE =="
