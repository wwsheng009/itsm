#!/usr/bin/env bash
# 238 demo 验收探针 P6：角色身份 + 端口发布 + tenant2 角色表（只读）
set -u
DB=itsm_demo
PSQL="docker exec itsm-postgres-prod psql -U itsm -d $DB"

echo "== ROLES(id in 40,44,45,46) =="
$PSQL -c "SELECT id, tenant_id, code, name FROM roles WHERE id IN (40,44,45,46) ORDER BY id"
echo "== T2-ROLES =="
$PSQL -c "SELECT id, code FROM roles WHERE tenant_id=2 ORDER BY id"
echo "== PG-PORTS =="
docker port itsm-postgres-prod 2>/dev/null || echo "(no published ports)"
echo "== PG-NET =="
docker inspect -f '{{range $k,$v := .NetworkSettings.Networks}}{{$k}} {{end}}' itsm-postgres-prod
echo "== HOST-GO =="
which go || echo "(no go toolchain on host)"
echo "== PROBE6-DONE =="
