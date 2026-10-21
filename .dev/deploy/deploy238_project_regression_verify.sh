#!/usr/bin/env bash
# 238 demo 运行时验证（b3f9762c）：回归遗留 A2 / B4 / B12 / 报表部门维度
set -u
R=/home/n9e/itsm/repo
cd "$R" || { echo "NO_REPO"; exit 1; }
BASE=http://127.0.0.1:8089
CK=/tmp/prr-ck.txt
PW=$(grep '^DEMO_ADMIN_PASSWORD=' .env.demo | cut -d= -f2-)
SUDO="echo passw0rd | sudo -S -p ''"

csrf() { curl -s --max-time 15 -b "$CK" -c "$CK" "$BASE/api/v1/csrf-token" | python3 -c "import sys,json;print((json.load(sys.stdin).get('data') or {}).get('csrf_token') or '')"; }

echo "===== [V1] version / login ====="
curl -s --max-time 15 "$BASE/api/v1/version"; echo
curl -s --max-time 15 -c "$CK" -X POST -H 'Content-Type: application/json' \
  -d "{\"username\":\"admin\",\"password\":\"$PW\"}" "$BASE/api/v1/auth/login" >/dev/null
T=$(csrf); echo "csrf_len=${#T}"

echo "===== [V2] A2 归口候选端点 ====="
code=$(curl -s -o /tmp/prr-v2.out -w '%{http_code}' --max-time 20 -b "$CK" "$BASE/api/v1/projects/owner-team-candidates?limit=50")
echo "owner-team-candidates -> $code"
python3 - <<'PY'
import json
d=json.load(open('/tmp/prr-v2.out'))
items=d.get('data') or []
print('count=', len(items))
for t in items[:6]:
    print(' ', t.get('id'), t.get('name'), t.get('code'))
PY

echo "===== [V3] A2 归口写入 → 列表回显 → 报表分组 ====="
TEAM=$(python3 -c "import json;d=json.load(open('/tmp/prr-v2.out'));i=d.get('data') or [];print(i[0]['id'] if i else '')")
echo "team_id=${TEAM:-none}"
if [ -n "${TEAM:-}" ]; then
  T=$(csrf)
  # 用回归报告里的模板链路验证项目（PRJ-20261021-3114）
  PID=$($SUDO docker exec itsm-postgres-prod psql -U itsm -d itsm_demo -tAc \
    "select id from projects where code='PRJ-20261021-3114' and deleted_at is null limit 1" 2>/dev/null | tr -d ' ')
  echo "project_id=${PID:-none}"
  if [ -n "${PID:-}" ]; then
    T=$(csrf)
    code=$(curl -s -o /tmp/prr-v3.out -w '%{http_code}' --max-time 20 -b "$CK" -c "$CK" \
      -X PUT -H 'Content-Type: application/json' -H "X-CSRF-Token: $T" \
      -d "{\"ownerTeamId\":$TEAM}" "$BASE/api/v1/projects/$PID")
    echo "set ownerTeamId -> $code"
    python3 - <<'PY'
import json
p=json.load(open('/tmp/prr-v3.out')).get('data') or {}
print('ownerTeamId=', p.get('ownerTeamId'), 'ownerTeamName=', p.get('ownerTeamName'),
      'departmentName=', p.get('departmentName'))
PY
    # 列表回显
    curl -s --max-time 20 -b "$CK" "$BASE/api/v1/projects?page=1&pageSize=20" -o /tmp/prr-v3b.out
    python3 - <<'PY'
import json
d=json.load(open('/tmp/prr-v3b.out'))
for p in (d.get('data') or {}).get('items') or []:
    if p.get('code')=='PRJ-20261021-3114':
        print('list ->', p.get('code'), 'ownerTeamId=', p.get('ownerTeamId'), 'ownerTeamName=', p.get('ownerTeamName'))
PY
    # 报表分组
    curl -s --max-time 20 -b "$CK" "$BASE/api/v1/projects/report-groups" -o /tmp/prr-v3c.out
    python3 - <<'PY'
import json
d=json.load(open('/tmp/prr-v3c.out'))
rows=d.get('data') or []
print('groups=', len(rows))
for r in rows:
    print(' ', 'team=', r.get('ownerTeamName'), 'dept=', r.get('departmentName') or r.get('departmentId'),
          'count=', r.get('projectCount'))
PY
  fi
fi

echo "===== [V4] B12 一次通过率（自动放行剔除） ====="
curl -s --max-time 30 -b "$CK" "$BASE/api/v1/projects/metrics" -o /tmp/prr-v4.out
python3 - <<'PY'
import json
g=((json.load(open('/tmp/prr-v4.out')).get('data') or {}).get('gateFirstPass') or {})
print('passed=', g.get('passed'), 'firstPass=', g.get('firstPass'),
      'autoApproved=', g.get('autoApproved'), 'rate=', g.get('rate'))
PY

echo "===== [V5] 页面可达 ====="
for path in /projects /projects/templates /projects/reports /projects/portfolio; do
  curl -s -o /dev/null -w "$path=%{http_code}\n" --max-time 15 "$BASE$path"
done

echo "===== [V6] 日志扫描 ====="
$SUDO docker logs --since 10m itsm-backend-demo 2>&1 | grep -iE 'panic|fatal|"level":"error"' | head -5
echo "VERIFY-DONE"
