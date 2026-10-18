#!/usr/bin/env bash
# F2 探针：get_ci_impact vs get_ci_relationships（ci_id=1，admin/tenant1），输出纯 ASCII JSON
set -u
BASE="http://127.0.0.1:8089/api/v1"
JAR=/tmp/be8_verify_cookies.txt
PY='import sys,json;d=json.load(sys.stdin);print(json.dumps(d,ensure_ascii=True))'
TOK=$(curl -s -b "$JAR" -c "$JAR" "$BASE/csrf-token" | python3 -c 'import sys,json;print(json.load(sys.stdin).get("data",{}).get("csrf_token",""))')
echo "== ci rows (id/tenant) =="
docker exec itsm-postgres-prod psql -U itsm -d itsm_demo -P pager=off -c "SELECT id, tenant_id, ci_number FROM configuration_items ORDER BY id LIMIT 8"
echo "== get_ci_impact ci_id=1 =="
curl -s --max-time 60 -b "$JAR" -H "X-CSRF-Token: $TOK" -H 'Content-Type: application/json' \
  -X POST "$BASE/agent/tools/execute" -d '{"name":"get_ci_impact","args":{"ci_id":1,"hops":3}}' | python3 -c "$PY"
echo "== get_ci_relationships ci_id=1 =="
curl -s --max-time 60 -b "$JAR" -H "X-CSRF-Token: $TOK" -H 'Content-Type: application/json' \
  -X POST "$BASE/agent/tools/execute" -d '{"name":"get_ci_relationships","args":{"ci_id":1}}' | python3 -c "$PY"
echo "== get_ci ci_id=1 =="
curl -s --max-time 60 -b "$JAR" -H "X-CSRF-Token: $TOK" -H 'Content-Type: application/json' \
  -X POST "$BASE/agent/tools/execute" -d '{"name":"get_ci","args":{"ci_id":1}}' | python3 -c "$PY"
echo "F2-PROBE-DONE"
