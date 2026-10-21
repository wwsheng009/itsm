#!/usr/bin/env bash
# 238 demo 运行时验证（50a1831b）：B1 工时上限/汇总护栏 + 越界历史数据
set -u
R=/home/n9e/itsm/repo
cd "$R" || { echo "NO_REPO"; exit 1; }
BASE=http://127.0.0.1:8089
CK=/tmp/pr3r-ck.txt
PW=$(grep '^DEMO_ADMIN_PASSWORD=' .env.demo | cut -d= -f2-)

csrf() { curl -s -b "$CK" -c "$CK" "$BASE/api/v1/csrf-token" | python3 -c "import sys,json;print((json.load(sys.stdin).get('data') or {}).get('csrf_token') or '')"; }

curl -s -c "$CK" -X POST -H 'Content-Type: application/json' \
  -d "{\"username\":\"admin\",\"password\":\"$PW\"}" "$BASE/api/v1/auth/login" >/dev/null
echo "version=$(curl -s --max-time 10 $BASE/api/v1/version)"
echo "csrf_len=$(${csrf} | wc -c)"

echo "===== [V1] B1 写入拒绝：estimateHours=999999999 ====="
PID=9
T=$(csrf)
code=$(curl -s -o /tmp/pr3r-v1.out -w '%{http_code}' -b "$CK" -c "$CK" \
  -X POST -H 'Content-Type: application/json' -H "X-CSRF-Token: $T" \
  -d '{"name":"越界工时回归验证","estimateHours":999999999}' \
  "$BASE/api/v1/projects/$PID/tasks")
echo "create task with 999999999h -> $code"
head -c 220 /tmp/pr3r-v1.out; echo

echo "===== [V2] 边界：10000 通过 / 10001 拒绝 ====="
T=$(csrf)
code=$(curl -s -o /tmp/pr3r-v2a.out -w '%{http_code}' -b "$CK" -c "$CK" \
  -X POST -H 'Content-Type: application/json' -H "X-CSRF-Token: $T" \
  -d '{"name":"边界工时-10000","estimateHours":10000}' \
  "$BASE/api/v1/projects/$PID/tasks")
echo "10000 -> $code"
NEWTASK=$(python3 -c "import json;print((json.load(open('/tmp/pr3r-v2a.out')).get('data') or {}).get('id') or '')")
T=$(csrf)
code=$(curl -s -o /tmp/pr3r-v2b.out -w '%{http_code}' -b "$CK" -c "$CK" \
  -X POST -H 'Content-Type: application/json' -H "X-CSRF-Token: $T" \
  -d '{"name":"边界工时-10001","estimateHours":10001}' \
  "$BASE/api/v1/projects/$PID/tasks")
echo "10001 -> $code"; head -c 200 /tmp/pr3r-v2b.out; echo

echo "===== [V3] 汇总护栏：项目 9 含历史 1000000h ====="
curl -s -b "$CK" "$BASE/api/v1/projects/$PID/hours-summary" -o /tmp/pr3r-v3.out
python3 - <<'PY'
import json
d=json.load(open('/tmp/pr3r-v3.out')).get('data') or {}
print('taskCount=', d.get('taskCount'),
      'estimatedTaskCount=', d.get('estimatedTaskCount'),
      'estimateHours=', d.get('estimateHours'),
      'actualHours=', d.get('actualHours'))
print('invalidHoursTasks=', d.get('invalidHoursTasks'),
      'invalidEstimateHours=', d.get('invalidEstimateHours'),
      'maxTaskHours=', d.get('maxTaskHours'))
PY

echo "===== [V4] 清理边界任务 ====="
if [ -n "${NEWTASK:-}" ]; then
  T=$(csrf)
  curl -s -o /dev/null -w "delete $NEWTASK=%{http_code}\n" -b "$CK" -c "$CK" \
    -X DELETE -H "X-CSRF-Token: $T" "$BASE/api/v1/projects/$PID/tasks/$NEWTASK"
fi

echo "===== [V5] 页面可达 ====="
for path in /projects /projects/9 /projects/templates; do
  curl -s -o /dev/null -w "$path=%{http_code}\n" --max-time 15 "$BASE$path"
done

echo "===== [V6] 日志 ====="
echo passw0rd | sudo -S -p '' docker logs --since 10m itsm-backend-demo 2>&1 | grep -iE 'panic|fatal|"level":"error"' | head -5
echo "VERIFY-DONE"
