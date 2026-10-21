#!/usr/bin/env bash
# 238 demo 第四轮验证：B1 写入拒绝 + 汇总护栏 + 页面可达
set -u
cd /home/n9e/itsm/repo || exit 1
BASE=http://127.0.0.1:8089
CK=/tmp/pr4-ck.txt
PW=$(grep '^DEMO_ADMIN_PASSWORD=' .env.demo | cut -d= -f2-)

csrf() { curl -s -b "$CK" -c "$CK" "$BASE/api/v1/csrf-token" | python3 -c "import sys,json;print((json.load(sys.stdin).get('data') or {}).get('csrf_token') or '')"; }

curl -s -c "$CK" -X POST -H 'Content-Type: application/json' \
  -d "{\"username\":\"admin\",\"password\":\"$PW\"}" "$BASE/api/v1/auth/login" >/dev/null
echo "version=$(curl -s --max-time 10 $BASE/api/v1/version)"

echo "===== [V1] 后端仍拒绝越界（防线未回退） ====="
T=$(csrf)
code=$(curl -s -o /tmp/pr4-v1.out -w '%{http_code}' -b "$CK" -c "$CK" \
  -X POST -H 'Content-Type: application/json' -H "X-CSRF-Token: $T" \
  -d '{"name":"第四轮越界校验","estimateHours":999999999}' \
  "$BASE/api/v1/projects/9/tasks")
echo "999999999 -> $code"; head -c 180 /tmp/pr4-v1.out; echo

T=$(csrf)
code=$(curl -s -o /tmp/pr4-v2.out -w '%{http_code}' -b "$CK" -c "$CK" \
  -X POST -H 'Content-Type: application/json' -H "X-CSRF-Token: $T" \
  -d '{"name":"第四轮边界10001","estimateHours":10001}' \
  "$BASE/api/v1/projects/9/tasks")
echo "10001 -> $code"

echo "===== [V2] 汇总护栏仍在（历史 1000000h 已排除） ====="
curl -s -b "$CK" "$BASE/api/v1/projects/9/hours-summary" -o /tmp/pr4-v3.out
python3 - <<'PY'
import json
d=json.load(open('/tmp/pr4-v3.out')).get('data') or {}
print('estimateHours=', d.get('estimateHours'),
      '| invalidHoursTasks=', d.get('invalidHoursTasks'),
      '| invalidEstimateHours=', d.get('invalidEstimateHours'),
      '| maxTaskHours=', d.get('maxTaskHours'))
PY

echo "===== [V3] 页面可达 ====="
for path in /projects /projects/9 /projects/templates; do
  curl -s -o /dev/null -w "$path=%{http_code}\n" --max-time 15 "$BASE$path"
done

echo "===== [V4] 前端产物含新标记 ====="
echo passw0rd | sudo -S -p '' docker exec itsm-frontend-demo sh -c \
  "grep -rl '归属与计划' /usr/share/nginx/html 2>/dev/null | wc -l" 2>/dev/null
echo passw0rd | sudo -S -p '' docker exec itsm-frontend-demo sh -c \
  "grep -rl '不得超过' /usr/share/nginx/html 2>/dev/null | wc -l" 2>/dev/null

echo "===== [V5] 日志 ====="
echo passw0rd | sudo -S -p '' docker logs --since 10m itsm-backend-demo 2>&1 | grep -iE 'panic|fatal|"level":"error"' | head -5
echo "VERIFY-DONE"
