#!/usr/bin/env bash
# 238 demo 发布 C 步：up -d（init/backend/worker/frontend）→ 迁移 → 健康/HTTP → A/B 指纹
# 用法：NEW=1.8.0-dev.d51ed172 bash /tmp/deploy238_dock_c.sh
set -u
R=/home/n9e/itsm/repo
cd "$R" || { echo "NO_REPO"; exit 1; }
NEW="${NEW:?NEW required}"
OLD="${OLD:-1.8.0-dev.c05cc49e}"
DC="docker compose --env-file .env.demo -f docker-compose.demo.yml"

echo "===== [C0] 镜像 / VERSION 闸 ====="
docker image inspect "itsm-backend:$NEW"  >/dev/null 2>&1 && echo "ok itsm-backend:$NEW"  || { echo "MISSING backend"; exit 9; }
docker image inspect "itsm-frontend:$NEW" >/dev/null 2>&1 && echo "ok itsm-frontend:$NEW" || { echo "MISSING frontend"; exit 9; }
grep -q "^VERSION=$NEW$" .env.demo || { echo "VERSION_MISMATCH"; exit 9; }

echo "===== [C1] up -d ====="
$DC up -d itsm-init-demo itsm-backend-demo itsm-worker-demo itsm-frontend-demo 2>&1 | tail -12

echo "===== [C2] itsm-init-demo（迁移/播种） ====="
timeout 300 docker wait itsm-init-demo; echo "init_wait_exit=$?"
docker logs --tail 40 itsm-init-demo 2>&1 | tail -40

echo "===== [C3] ledger post ====="
echo -n "ledger_demo="; docker exec itsm-postgres-prod psql -U itsm -d itsm_demo -tAc "select count(*) from schema_migrations" 2>&1 | head -1
docker exec itsm-postgres-prod psql -U itsm -d itsm_demo -tAc "select version from schema_migrations order by version desc limit 5" 2>&1

echo "===== [C4] containers ====="
sleep 10
docker ps --format '{{.Names}}|{{.Image}}|{{.Status}}' | grep demo | sort

echo "===== [C5] health ====="
for i in $(seq 1 18); do
  ST=$(docker inspect -f '{{.State.Health.Status}}' itsm-backend-demo 2>/dev/null || echo missing)
  echo "backend-demo: $ST"; [ "$ST" = "healthy" ] && break; sleep 10
done
for i in $(seq 1 12); do
  ST=$(docker inspect -f '{{.State.Health.Status}}' itsm-frontend-demo 2>/dev/null || echo missing)
  echo "frontend-demo: $ST"; [ "$ST" = "healthy" ] && break; sleep 10
done
curl -s -o /dev/null -w 'root=%{http_code}\n' --max-time 15 http://127.0.0.1:8089/
curl -s --max-time 15 http://127.0.0.1:8089/api/v1/version; echo

echo "===== [C6] A/B 指纹（镜像内 grep） ====="
for spec in "old-backend|itsm-backend:$OLD|/app/main|assistant/context" \
            "new-backend|itsm-backend:$NEW|/app/main|assistant/context" \
            "old-frontend|itsm-frontend:$OLD|/usr/share/nginx/html|assistant/context" \
            "new-frontend|itsm-frontend:$NEW|/usr/share/nginx/html|assistant/context"; do
  IFS='|' read -r label img path pat <<< "$spec"
  echo -n "$label="
  docker run --rm --entrypoint sh "$img" -c "test -e $path || { echo path-missing; exit 0; }; if [ -d $path ]; then grep -rl '$pat' $path 2>/dev/null | wc -l; else grep -ac '$pat' $path 2>/dev/null; fi; true"
done
echo "C-STATUS: DONE"
