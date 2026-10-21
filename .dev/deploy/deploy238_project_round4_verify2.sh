#!/usr/bin/env bash
# 核查项目 9 的任务构成（确认 20009 的来源）+ 清理第四轮测试数据
set -u
cd /home/n9e/itsm/repo || exit 1
BASE=http://127.0.0.1:8089
CK=/tmp/pr4b-ck.txt
PW=$(grep '^DEMO_ADMIN_PASSWORD=' .env.demo | cut -d= -f2-)

echo "===== 项目 9 全部任务 ====="
echo passw0rd | sudo -S -p '' docker exec itsm-postgres-prod psql -U itsm -d itsm_demo -tAc \
  "select id||'|'||name||'|est='||coalesce(estimate_hours,0)||'|deleted='||coalesce(deleted_at::text,'-') from project_tasks where project_id=9 order by id" 2>&1

echo "===== 第四轮新增的测试任务（含关键字） ====="
echo passw0rd | sudo -S -p '' docker exec itsm-postgres-prod psql -U itsm -d itsm_demo -tAc \
  "select id||'|'||name||'|est='||coalesce(estimate_hours,0) from project_tasks where project_id=9 and deleted_at is null and (name like '%越界%' or name like '%钳制%' or name like '%边界%') order by id" 2>&1

curl -s -c "$CK" -X POST -H 'Content-Type: application/json' \
  -d "{\"username\":\"admin\",\"password\":\"$PW\"}" "$BASE/api/v1/auth/login" >/dev/null

echo "===== 清理第四轮测试任务（软删） ====="
IDS=$(echo passw0rd | sudo -S -p '' docker exec itsm-postgres-prod psql -U itsm -d itsm_demo -tAc \
  "select id from project_tasks where project_id=9 and deleted_at is null and (name like '%越界工时拒绝保存验证%' or name like '%工时钳制观测%' or name like '%边界%' or name like '%第四轮%')" 2>/dev/null | tr -d ' ')
for id in $IDS; do
  T=$(curl -s -b "$CK" -c "$CK" "$BASE/api/v1/csrf-token" | python3 -c "import sys,json;print(json.load(sys.stdin)['data']['csrf_token'])")
  code=$(curl -s -o /dev/null -w '%{http_code}' -b "$CK" -c "$CK" -X DELETE -H "X-CSRF-Token: $T" "$BASE/api/v1/projects/9/tasks/$id")
  echo "delete task $id -> $code"
done

echo "===== 清理后汇总 ====="
curl -s -b "$CK" "$BASE/api/v1/projects/9/hours-summary" | python3 -m json.tool
echo "CHECK-DONE"
