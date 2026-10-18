#!/usr/bin/env bash
# 238 demo 更新 C 步：up -d + 健康检查 + A/B 新代码指纹（F1）
set -u
R=/home/n9e/itsm/repo
cd "$R" || { echo "NO_REPO"; exit 1; }
NEW="${NEW:?NEW required}"
OLD_BE="${OLD_BE:-1.8.0-dev.fd7514a9}"
OLD_FE="${OLD_FE:-1.8.0-dev.fd7514a9}"

echo "===== [C0] 镜像存在性 ====="
docker image inspect "itsm-backend:$NEW" >/dev/null 2>&1 && echo "ok itsm-backend:$NEW" || { echo "MISSING backend"; exit 9; }
if docker image inspect "itsm-frontend:$NEW" >/dev/null 2>&1; then
  echo "ok itsm-frontend:$NEW (exists)"
else
  docker tag "itsm-frontend:$OLD_FE" "itsm-frontend:$NEW" && echo "aliased itsm-frontend:$OLD_FE -> $NEW"
fi
grep -q "^VERSION=$NEW$" .env.demo || { echo "VERSION_MISMATCH"; grep '^VERSION=' .env.demo; exit 9; }

echo "===== [C1] up -d（backend/worker/frontend） ====="
docker compose --env-file .env.demo -f docker-compose.demo.yml up -d itsm-backend-demo itsm-worker-demo itsm-frontend-demo 2>&1 | tail -12

echo "===== [C2] 容器快照 ====="
sleep 10
docker ps -a --format '{{.Names}}|{{.Image}}|{{.Status}}' | grep demo | sort

echo "===== [C3] 健康检查 + HTTP ====="
for i in $(seq 1 12); do
  ST=$(docker inspect -f '{{.State.Health.Status}}' itsm-backend-demo 2>/dev/null || echo missing)
  echo "backend-demo health: $ST"
  [ "$ST" = "healthy" ] && break
  sleep 10
done
curl -s -o /dev/null -w 'root=%{http_code}\n' --max-time 15 http://127.0.0.1:8089/
curl -s -o /dev/null -w 'health=%{http_code}\n' --max-time 15 http://127.0.0.1:8089/api/v1/health
curl -s --max-time 15 http://127.0.0.1:8089/api/v1/version; echo

echo "===== [C4] A/B 新代码指纹 ====="
echo -n "old-backend-degraded-marker="; docker run --rm --entrypoint sh "itsm-backend:$OLD_BE" -c 'grep -ac "degraded to keyword fallback" /app/main 2>/dev/null || echo n/a'
echo -n "new-backend-degraded-marker="; docker run --rm --entrypoint sh "itsm-backend:$NEW" -c 'grep -ac "degraded to keyword fallback" /app/main 2>/dev/null || echo n/a'
echo "C-STATUS: DONE"
