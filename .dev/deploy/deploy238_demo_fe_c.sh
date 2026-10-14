#!/usr/bin/env bash
# demo 前端更新 C 步：确认 .env.demo VERSION → up -d 重建前端容器 → 健康检查
# 241014: dashboard KPI 磁贴点击下转（b86819be）
set -u
R=/home/n9e/itsm/repo
cd "$R" || { echo "NO_REPO"; exit 1; }
NEW="${NEW:?NEW required}"

echo "===== [C1] .env.demo VERSION 确认 ====="
grep -E '^VERSION=' .env.demo
grep -q "^VERSION=$NEW$" .env.demo || { echo "VERSION_MISMATCH"; exit 9; }

echo "===== [C2] up -d（重建 itsm-frontend-demo） ====="
docker compose --env-file .env.demo -f docker-compose.demo.yml up -d itsm-frontend-demo 2>&1 | tail -6

echo "===== [C3] 容器状态 ====="
sleep 8
docker ps -a --format '{{.Names}}|{{.Image}}|{{.Status}}' | grep demo | sort

echo "===== [C4] 前端健康检查 ====="
for i in 1 2 3 4 5 6; do
  ST=$(docker inspect -f '{{.State.Health.Status}}' itsm-frontend-demo 2>/dev/null || echo missing)
  echo "frontend-demo health: $ST"
  [ "$ST" = "healthy" ] && break
  sleep 10
done

echo "===== [C5] 首页探活 ====="
curl -fsS -o /dev/null -w "front-http=%{http_code}\n" http://127.0.0.1:8089/ || echo "curl-failed"
docker exec itsm-nginx-demo wget -q -O- http://itsm-frontend-demo:3000/health 2>&1 | head -2
echo "DEMO-FE-C-DONE"
