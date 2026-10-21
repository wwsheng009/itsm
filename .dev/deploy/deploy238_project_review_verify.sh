#!/usr/bin/env bash
# 238 demo 运行时验证（0aa276da）：项目功能审查修复的 5 条关键行为
set -u
R=/home/n9e/itsm/repo
cd "$R" || { echo "NO_REPO"; exit 1; }
BASE=http://127.0.0.1:8089
CK=/tmp/project-review-ck.txt
ADMIN_PW=$(grep '^DEMO_ADMIN_PASSWORD=' .env.demo | cut -d= -f2-)
SUDO="echo passw0rd | sudo -S -p ''"

echo "===== [V1] version / login ====="
curl -s --max-time 15 "$BASE/api/v1/version"; echo
LOGIN=$(curl -s --max-time 15 -c "$CK" -X POST -H 'Content-Type: application/json' \
  -d "{\"username\":\"admin\",\"password\":\"$ADMIN_PW\"}" "$BASE/api/v1/auth/login")
echo "$LOGIN" | head -c 120; echo
echo "cookie_lines=$(wc -l < "$CK" 2>/dev/null || echo 0)"
CSRF=$(curl -s --max-time 15 -b "$CK" -c "$CK" "$BASE/api/v1/csrf-token" | python3 -c "import sys,json;print((json.load(sys.stdin).get('data') or {}).get('csrf_token') or '')")
echo "csrf_len=${#CSRF}"

echo "===== [V2] A1 严格必填：仅 name 创建应 400 ====="
code=$(curl -s -o /tmp/pr-v2.out -w '%{http_code}' --max-time 20 -b "$CK" -c "$CK" \
  -X POST -H 'Content-Type: application/json' -H "X-CSRF-Token: $CSRF" \
  -d '{"name":"审查验证-空壳项目"}' "$BASE/api/v1/projects")
echo "create-without-owner -> $code"; head -c 300 /tmp/pr-v2.out; echo

echo "===== [V3] A2/B3 列表：名称字段与部门 ====="
curl -s --max-time 20 -b "$CK" "$BASE/api/v1/projects?page=1&pageSize=5" -o /tmp/pr-v3.out
python3 - <<'PY'
import json
d=json.load(open('/tmp/pr-v3.out'))
items=(d.get('data') or {}).get('items') or []
print('items=', len(items))
for p in items[:5]:
    print(p.get('id'), '|', p.get('name'), '| managerId=', p.get('managerId'),
          '| managerName=', p.get('managerName'), '| deptId=', p.get('departmentId'),
          '| deptName=', p.get('departmentName'),
          '| health=', p.get('health'), '| quality=', p.get('healthDataQuality'),
          '| gaps=', p.get('healthDataGaps'))
PY

echo "===== [V4] A4 门审批方式（对第一个项目） ====="
PID=$(python3 -c "import json;d=json.load(open('/tmp/pr-v3.out'));print(((d.get('data') or {}).get('items') or [{}])[0].get('id') or '')")
echo "project_id=${PID:-none}"
if [ -n "${PID:-}" ]; then
  curl -s --max-time 20 -b "$CK" "$BASE/api/v1/projects/$PID/gates" -o /tmp/pr-v4.out
  python3 - <<'PY'
import json
d=json.load(open('/tmp/pr-v4.out'))
data=d.get('data') or {}
for r in data.get('readiness') or []:
    print('readiness', r.get('gate'), 'ready=', r.get('ready'),
          'mode=', r.get('approvalMode'), 'configMissing=', r.get('autoApprovalConfigMissing'))
for g in data.get('items') or []:
    print('gate', g.get('gate'), g.get('status'), 'mode=', g.get('approvalMode'), 'auto=', g.get('autoApproved'))
PY

echo "===== [V5] A8 结项阻塞口径（blockerTotal） ====="
  curl -s --max-time 20 -b "$CK" "$BASE/api/v1/projects/$PID/closure" -o /tmp/pr-v5.out
  python3 - <<'PY'
import json
d=json.load(open('/tmp/pr-v5.out'))
data=d.get('data') or {}
print('status=', data.get('status'), 'blockerTotal=', data.get('blockerTotal'),
      'blockers_len=', len(data.get('blockers') or []))
PY
fi

echo "===== [V6] B12 一次通过率（自动放行单列） ====="
curl -s --max-time 30 -b "$CK" "$BASE/api/v1/projects/metrics" -o /tmp/pr-v6.out
python3 - <<'PY'
import json
d=json.load(open('/tmp/pr-v6.out'))
g=((d.get('data') or {}).get('gateFirstPass') or {})
print('passed=', g.get('passed'), 'firstPass=', g.get('firstPass'),
      'autoApproved=', g.get('autoApproved'), 'rate=', g.get('rate'),
      'detailsTotal=', g.get('detailsTotal'))
rows=g.get('details') or []
print('detail_auto_rows=', sum(1 for r in rows if r.get('autoApproved')),
      'detail_firstpass_rows=', sum(1 for r in rows if r.get('firstPass')))
PY

echo "===== [V7] A6 报告时间/编号（业务时区） ====="
CSRF=$(curl -s --max-time 15 -b "$CK" -c "$CK" "$BASE/api/v1/csrf-token" | python3 -c "import sys,json;print((json.load(sys.stdin).get('data') or {}).get('csrf_token') or '')")
curl -s --max-time 20 -b "$CK" -c "$CK" "$BASE/api/v1/projects/$PID/reports/generate" \
  -X POST -H 'Content-Type: application/json' -H "X-CSRF-Token: $CSRF" -d '{}' -o /tmp/pr-v7.out
python3 - <<'PY'
import json,re
d=json.load(open('/tmp/pr-v7.out'))
payload=(d.get('data') or {})
md=(payload.get('markdown') or '')
print('report_generatedAt=', (payload.get('data') or {}).get('generatedAt'))
for line in md.splitlines()[:8]:
    print(line)
PY

echo "===== [V8] 日志扫描（近 10m） ====="
echo 'passw0rd' | sudo -S -p '' docker logs --since 10m itsm-backend-demo 2>&1 | grep -iE 'panic|fatal|"level":"error"' | head -6
echo "VERIFY-DONE"
