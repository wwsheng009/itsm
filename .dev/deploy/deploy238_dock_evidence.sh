#!/usr/bin/env bash
# 238 demo 发布终态证据采集（dock-plan 全量交付 d51ed172；供 DEMO-DOCK 证据文档引用）
# 用法：NEW=1.8.0-dev.d51ed172 OLD=1.8.0-dev.c05cc49e bash /tmp/deploy238_dock_evidence.sh
set -u
R=/home/n9e/itsm/repo
cd "$R" || { echo "NO_REPO"; exit 1; }
NEW="${NEW:-1.8.0-dev.d51ed172}"
OLD="${OLD:-1.8.0-dev.c05cc49e}"

echo "===== [E1] git 分支/HEAD ====="
echo "branch=$(git branch --show-current)"
echo "head=$(git rev-parse HEAD)"
git log --oneline -2

echo "===== [E2] .env.demo 关键行 ====="
grep -E '^(VERSION|DEMO_HTTP_PORT|DEPLOYMENT_MODE)=' .env.demo

echo "===== [E3] demo 容器 ====="
docker ps --format '{{.Names}}|{{.Image}}|{{.Status}}' | grep demo | sort
echo "init_demo=$(docker inspect -f '{{.State.Status}}|exit={{.State.ExitCode}}' itsm-init-demo 2>/dev/null || echo missing)"

echo "===== [E4] 版本 / HTTP ====="
curl -s --max-time 10 http://127.0.0.1:8089/api/v1/version; echo
curl -s -o /dev/null -w 'root=%{http_code}\n' --max-time 10 http://127.0.0.1:8089/

echo "===== [E5] ledger ====="
echo -n "ledger_demo="; docker exec itsm-postgres-prod psql -U itsm -d itsm_demo -tAc "select count(*) from schema_migrations" 2>&1 | head -1
docker exec itsm-postgres-prod psql -U itsm -d itsm_demo -tAc "select version from schema_migrations order by version desc limit 10" 2>&1

echo "===== [E6] 本批迁移文件（20261018*） ====="
ls -1 itsm-backend/migrations/ | grep -E '^20261018' | sort

echo "===== [E7] A/B 指纹（镜像内 grep） ====="
for spec in "old-backend|itsm-backend:$OLD|/app/main|assistant/context" \
            "new-backend|itsm-backend:$NEW|/app/main|assistant/context" \
            "old-frontend|itsm-frontend:$OLD|/usr/share/nginx/html|assistant/context" \
            "new-frontend|itsm-frontend:$NEW|/usr/share/nginx/html|assistant/context" \
            "old-frontend-dock|itsm-frontend:$OLD|/usr/share/nginx/html|assistant-dock" \
            "new-frontend-dock|itsm-frontend:$NEW|/usr/share/nginx/html|assistant-dock" \
            "old-backend-tele|itsm-backend:$OLD|/app/main|ai_telemetry_events" \
            "new-backend-tele|itsm-backend:$NEW|/app/main|ai_telemetry_events"; do
  IFS='|' read -r label img path pat <<< "$spec"
  echo -n "$label="
  docker run --rm --entrypoint sh "$img" -c "test -e $path || { echo path-missing; exit 0; }; if [ -d $path ]; then grep -rl '$pat' $path 2>/dev/null | wc -l; else grep -ac '$pat' $path 2>/dev/null; fi; true"
done
echo "EVIDENCE-DONE"
