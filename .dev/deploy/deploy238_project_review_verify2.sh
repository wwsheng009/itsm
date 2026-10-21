#!/usr/bin/env bash
# 238 demo 补充验证：新建项目编号/必填/删除门禁 + 项目页可达
set -u
R=/home/n9e/itsm/repo
cd "$R" || { echo "NO_REPO"; exit 1; }
BASE=http://127.0.0.1:8089
CK=/tmp/pr2-ck.txt
ADMIN_PW=$(grep '^DEMO_ADMIN_PASSWORD=' .env.demo | cut -d= -f2-)

curl -s --max-time 15 -c "$CK" -X POST -H 'Content-Type: application/json' \
  -d "{\"username\":\"admin\",\"password\":\"$ADMIN_PW\"}" "$BASE/api/v1/auth/login" >/dev/null
CSRF=$(curl -s --max-time 15 -b "$CK" -c "$CK" "$BASE/api/v1/csrf-token" | python3 -c "import sys,json;print((json.load(sys.stdin).get('data') or {}).get('csrf_token') or '')")
echo "csrf_len=${#CSRF}"

echo "===== [W1] A2/A6 新建项目：编号日期 / 部门写入 / 名称富化 ====="
BODY='{"name":"审查修复验证项目 20261021","managerId":1,"departmentId":1,"startDate":"2026-10-21T00:00:00Z","endDate":"2026-12-31T00:00:00Z","type":"internal"}'
code=$(curl -s -o /tmp/pr2-w1.out -w '%{http_code}' --max-time 20 -b "$CK" -c "$CK" \
  -X POST -H 'Content-Type: application/json' -H "X-CSRF-Token: $CSRF" -d "$BODY" "$BASE/api/v1/projects")
echo "create -> $code"
python3 - <<'PY'
import json
d=json.load(open('/tmp/pr2-w1.out'))
p=d.get('data') or {}
print('id=', p.get('id'), 'code=', p.get('code'), 'status=', p.get('status'))
print('managerId=', p.get('managerId'), 'managerName=', p.get('managerName'))
print('departmentId=', p.get('departmentId'), 'departmentName=', p.get('departmentName'))
print('health=', p.get('health'), 'quality=', p.get('healthDataQuality'), 'gaps=', p.get('healthDataGaps'))
open('/tmp/pr2-id.txt','w').write(str(p.get('id') or ''))
PY

echo "===== [W2] 同名无归属仍应 400（A1 严格模式） ====="
code=$(curl -s -o /tmp/pr2-w2.out -w '%{http_code}' --max-time 20 -b "$CK" -c "$CK" \
  -X POST -H 'Content-Type: application/json' -H "X-CSRF-Token: $CSRF" -d '{"name":"审查修复验证-无部门"}' "$BASE/api/v1/projects")
echo "create -> $code"; head -c 200 /tmp/pr2-w2.out; echo

echo "===== [W3] 页面可达 ====="
for path in /projects /projects/6 /projects/templates /projects/reports /projects/portfolio; do
  curl -s -o /dev/null -w "$path=%{http_code}\n" --max-time 15 "$BASE$path"
done

echo "===== [W4] 清理验证项目（软删） ====="
NID=$(cat /tmp/pr2-id.txt)
if [ -n "${NID:-}" ]; then
  CSRF=$(curl -s --max-time 15 -b "$CK" -c "$CK" "$BASE/api/v1/csrf-token" | python3 -c "import sys,json;print((json.load(sys.stdin).get('data') or {}).get('csrf_token') or '')")
  code=$(curl -s -o /tmp/pr2-w4.out -w '%{http_code}' --max-time 20 -b "$CK" -c "$CK" \
    -X DELETE -H "X-CSRF-Token: $CSRF" "$BASE/api/v1/projects/$NID")
  echo "delete $NID -> $code"; head -c 160 /tmp/pr2-w4.out; echo
fi
echo "W-STATUS: DONE"
