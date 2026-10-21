#!/usr/bin/env bash
# 238 demo 发布 C 步：up -d + 健康 + A/B 指纹
set -u
R=/home/n9e/itsm/repo
cd "$R" || { echo "NO_REPO"; exit 1; }
NEW="${NEW:?NEW required}"
OLD="${OLD:-1.8.0-dev.0aa276da}"

echo "===== [C0] 镜像 / VERSION 闸 ====="
echo 'passw0rd' | sudo -S -p '' docker image inspect "itsm-backend:$NEW"  >/dev/null 2>&1 && echo "ok backend"  || { echo "MISSING backend"; exit 9; }
echo 'passw0rd' | sudo -S -p '' docker image inspect "itsm-frontend:$NEW" >/dev/null 2>&1 && echo "ok frontend" || { echo "MISSING frontend"; exit 9; }
grep -q "^VERSION=$NEW$" .env.demo || { echo "VERSION_MISMATCH"; exit 9; }

echo "===== [C1] up -d ====="
echo 'passw0rd' | sudo -S -p '' env VERSION="$NEW" docker compose --env-file .env.demo -f docker-compose.demo.yml up -d \
  itsm-init-demo itsm-backend-demo itsm-worker-demo itsm-frontend-demo 2>&1 | tail -10

echo "===== [C2] init ====="
timeout 300 sudo docker wait itsm-init-demo; echo "init_wait_exit=$?"
echo 'passw0rd' | sudo -S -p '' docker logs --tail 12 itsm-init-demo 2>&1 | tail -12

echo "===== [C3] ledger ====="
echo -n "ledger_demo="
echo 'passw0rd' | sudo -S -p '' docker exec itsm-postgres-prod psql -U itsm -d itsm_demo -tAc "select count(*) from schema_migrations" 2>&1 | head -1

echo "===== [C4] containers ====="
sleep 10
echo 'passw0rd' | sudo -S -p '' docker ps --format '{{.Names}}|{{.Image}}|{{.Status}}' | grep demo | sort

echo "===== [C5] health ====="
for i in $(seq 1 18); do
  ST=$(echo 'passw0rd' | sudo -S -p '' docker inspect -f '{{.State.Health.Status}}' itsm-backend-demo 2>/dev/null || echo missing)
  echo "backend-demo: $ST"; [ "$ST" = "healthy" ] && break; sleep 10
done
for i in $(seq 1 12); do
  ST=$(echo 'passw0rd' | sudo -S -p '' docker inspect -f '{{.State.Health.Status}}' itsm-frontend-demo 2>/dev/null || echo missing)
  echo "frontend-demo: $ST"; [ "$ST" = "healthy" ] && break; sleep 10
done
curl -s -o /dev/null -w 'root=%{http_code}\n' --max-time 15 http://127.0.0.1:8089/
curl -s --max-time 15 http://127.0.0.1:8089/api/v1/version; echo

echo "===== [C6] A/B 指纹 ====="
for spec in "old-backend|itsm-backend:$OLD|/app/main|owner-team-candidates" \
            "new-backend|itsm-backend:$NEW|/app/main|owner-team-candidates" \
            "old-frontend|itsm-frontend:$OLD|/usr/share/nginx/html|归口团队" \
            "new-frontend|itsm-frontend:$NEW|/usr/share/nginx/html|归口团队" \
            "new-frontend-dept|itsm-frontend:$NEW|/usr/share/nginx/html|所属部门" \
            "new-frontend-fixed|itsm-frontend:$NEW|/usr/share/nginx/html|fixed"; do
  IFS='|' read -r label img path pat <<< "$spec"
  echo -n "$label="
  echo 'passw0rd' | sudo -S -p '' docker run --rm --entrypoint sh "$img" -c "test -e $path || { echo path-missing; exit 0; }; if [ -d $path ]; then grep -rl '$pat' $path 2>/dev/null | wc -l; else grep -ac '$pat' $path 2>/dev/null; fi; true"
done
echo "C-STATUS: DONE"
