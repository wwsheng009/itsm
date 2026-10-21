#!/usr/bin/env bash
# 复核项目 9 工时汇总（清理边界任务后）
set -u
cd /home/n9e/itsm/repo || exit 1
BASE=http://127.0.0.1:8089
CK=/tmp/pr3c-ck.txt
PW=$(grep '^DEMO_ADMIN_PASSWORD=' .env.demo | cut -d= -f2-)

curl -s -c "$CK" -X POST -H 'Content-Type: application/json' \
  -d "{\"username\":\"admin\",\"password\":\"$PW\"}" "$BASE/api/v1/auth/login" >/dev/null

echo "--- hours-summary (project 9) ---"
curl -s -b "$CK" "$BASE/api/v1/projects/9/hours-summary" | python3 -m json.tool

echo "--- tasks (project 9) ---"
echo passw0rd | sudo -S -p '' docker exec itsm-postgres-prod psql -U itsm -d itsm_demo -tAc \
  "select id||'|'||name||'|est='||coalesce(estimate_hours,0)||'|deleted='||coalesce(deleted_at::text,'-') from project_tasks where project_id=9 order by id" 2>&1
echo "REVIEW-DONE"
