#!/usr/bin/env bash
# 238 demo 终态采集（0aa276da）
set -u
R=/home/n9e/itsm/repo
cd "$R" || { echo "NO_REPO"; exit 1; }
S="echo passw0rd | sudo -S -p ''"

echo "branch=$(git branch --show-current)"
echo "head=$(git rev-parse HEAD)"
echo "VERSION=$(grep '^VERSION=' .env.demo | cut -d= -f2-)"
echo
echo "--- containers ---"
echo 'passw0rd' | sudo -S -p '' docker ps --format '{{.Names}}|{{.Image}}|{{.Status}}' | grep demo | sort
echo
echo "--- init ---"
echo 'passw0rd' | sudo -S -p '' docker inspect -f '{{.State.Status}}|exit={{.State.ExitCode}}' itsm-init-demo
echo "ledger_demo=$(echo 'passw0rd' | sudo -S -p '' docker exec itsm-postgres-prod psql -U itsm -d itsm_demo -tAc 'select count(*) from schema_migrations' | tr -d ' ')"
echo
echo "--- version ---"
curl -s --max-time 15 http://127.0.0.1:8089/api/v1/version; echo
echo
echo "--- images ---"
echo 'passw0rd' | sudo -S -p '' docker images --format '{{.Repository}}:{{.Tag}}|{{.Size}}' | grep -E 'itsm-(backend|frontend):1.8.0-dev.(0aa276da|3cfc1dcc)' | sort
echo
echo "--- 项目域数据抽样 ---"
echo 'passw0rd' | sudo -S -p '' docker exec itsm-postgres-prod psql -U itsm -d itsm_demo -tAc \
  "select id,code,coalesce(manager_id,0),coalesce(department_id,0),status from projects where deleted_at is null order by id" 2>&1
echo
echo "--- 门审批留痕抽样 ---"
echo 'passw0rd' | sudo -S -p '' docker exec itsm-postgres-prod psql -U itsm -d itsm_demo -tAc \
  "select project_id,gate,status,coalesce(nullif(decision_comment,''),'-') from project_gates order by project_id,gate limit 12" 2>&1
echo
echo "EVIDENCE-DONE"
