#!/usr/bin/env bash
# 238 demo A2 归口写入验证（b3f9762c）：候选 → PUT ownerTeamId → 列表回显 → 报表分组
set -u
R=/home/n9e/itsm/repo
cd "$R" || { echo "NO_REPO"; exit 1; }
BASE=http://127.0.0.1:8089
CK=/tmp/prr2-ck.txt
PW=$(grep '^DEMO_ADMIN_PASSWORD=' .env.demo | cut -d= -f2-)

curl -s -c "$CK" -X POST -H 'Content-Type: application/json' \
  -d "{\"username\":\"admin\",\"password\":\"$PW\"}" "$BASE/api/v1/auth/login" >/dev/null

PID=$(echo passw0rd | sudo -S -p '' docker exec itsm-postgres-prod psql -U itsm -d itsm_demo -tAc \
  "select id from projects where code='PRJ-20261021-3114' and deleted_at is null limit 1" | tr -d ' ')
echo "project_id=${PID:-none}"

TEAM=$(curl -s -b "$CK" "$BASE/api/v1/projects/owner-team-candidates?limit=1" \
  | python3 -c "import sys,json;d=json.load(sys.stdin);i=d.get('data') or [];print(i[0]['id'] if i else '')")
echo "team_id=${TEAM:-none}"

T=$(curl -s -b "$CK" -c "$CK" "$BASE/api/v1/csrf-token" \
  | python3 -c "import sys,json;print(json.load(sys.stdin)['data']['csrf_token'])")

code=$(curl -s -o /tmp/prr2.out -w '%{http_code}' -b "$CK" -c "$CK" \
  -X PUT -H 'Content-Type: application/json' -H "X-CSRF-Token: $T" \
  -d "{\"ownerTeamId\":$TEAM}" "$BASE/api/v1/projects/$PID")
echo "PUT ownerTeamId -> $code"
python3 - <<'PY'
import json
d=json.load(open('/tmp/prr2.out'))
p=d.get('data') or {}
print('ownerTeamId=', p.get('ownerTeamId'), 'ownerTeamName=', p.get('ownerTeamName'),
      'departmentName=', p.get('departmentName'))
PY

echo "--- list echo ---"
curl -s -b "$CK" "$BASE/api/v1/projects?page=1&pageSize=20" -o /tmp/prr2-list.out
python3 - <<'PY'
import json
d=json.load(open('/tmp/prr2-list.out'))
for p in (d.get('data') or {}).get('items') or []:
    if p.get('code')=='PRJ-20261021-3114':
        print('list:', p.get('code'), 'teamId=', p.get('ownerTeamId'), 'teamName=', p.get('ownerTeamName'))
PY

echo "--- report groups ---"
curl -s -b "$CK" "$BASE/api/v1/projects/report-groups" -o /tmp/prr2-groups.out
python3 - <<'PY'
import json
rows=json.load(open('/tmp/prr2-groups.out')).get('data') or []
for r in rows:
    print('team=', r.get('ownerTeamName'), '| dept=', r.get('departmentName') or r.get('departmentId'),
          '| count=', r.get('projectCount'))
PY

echo "--- restore (clear ownerTeamId) ---"
T=$(curl -s -b "$CK" -c "$CK" "$BASE/api/v1/csrf-token" \
  | python3 -c "import sys,json;print(json.load(sys.stdin)['data']['csrf_token'])")
curl -s -o /dev/null -w 'restore=%{http_code}\n' -b "$CK" -c "$CK" \
  -X PUT -H 'Content-Type: application/json' -H "X-CSRF-Token: $T" \
  -d '{"ownerTeamId":0}' "$BASE/api/v1/projects/$PID"
echo "A2-DONE"
