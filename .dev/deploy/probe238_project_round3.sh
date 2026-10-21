#!/usr/bin/env bash
# 第三轮回归：DB 现状核查（工时越界值 / 归口落库）
set -u
cd /home/n9e/itsm/repo || exit 1
S="echo passw0rd | sudo -S -p ''"

echo "===== projects ====="
echo passw0rd | sudo -S -p '' docker exec itsm-postgres-prod psql -U itsm -d itsm_demo -tAc \
  "select id||'|'||code||'|team='||coalesce(owner_team_id,0)||'|dept='||coalesce(department_id,0)||'|'||status from projects where deleted_at is null order by id" 2>&1

echo "===== tasks with estimate > 1000 ====="
echo passw0rd | sudo -S -p '' docker exec itsm-postgres-prod psql -U itsm -d itsm_demo -tAc \
  "select id||'|proj='||project_id||'|'||name||'|est='||coalesce(estimate_hours,0)||'|act='||coalesce(actual_hours,0) from project_tasks where deleted_at is null and coalesce(estimate_hours,0) > 1000 order by id" 2>&1

echo "===== max estimate per project ====="
echo passw0rd | sudo -S -p '' docker exec itsm-postgres-prod psql -U itsm -d itsm_demo -tAc \
  "select project_id||'|sum_est='||coalesce(sum(estimate_hours),0)||'|cnt='||count(*) from project_tasks where deleted_at is null group by project_id order by project_id" 2>&1

echo "===== teams (tenant 1) ====="
echo passw0rd | sudo -S -p '' docker exec itsm-postgres-prod psql -U itsm -d itsm_demo -tAc \
  "select id||'|'||name||'|'||status||'|deleted='||coalesce(deleted_at::text,'-') from teams where tenant_id=1 order by id" 2>&1

echo "DB-DONE"
