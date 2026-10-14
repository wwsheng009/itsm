#!/usr/bin/env bash
# 238 demo 增量修复 C 步：确认镜像 → up -d backend/worker/frontend → 容器快照
set -u
R=/home/n9e/itsm/repo
cd "$R" || { echo "NO_REPO"; exit 1; }
NEW="${NEW:?NEW required}"

echo "===== [C0] 构建产物与构建日志 ====="
grep -E 'BUILD_(BACKEND|FRONTEND)_EXIT|BUILD_ALL_DONE' /tmp/build-fix.log 2>/dev/null || echo "(status log missing)"
docker image inspect "itsm-backend:$NEW" >/dev/null 2>&1 && echo "backend-image-ok" || { echo "BACKEND_IMAGE_MISSING"; exit 9; }
docker image inspect "itsm-frontend:$NEW" >/dev/null 2>&1 && echo "frontend-image-ok" || { echo "FRONTEND_IMAGE_MISSING"; exit 9; }

echo "===== [C1] VERSION 确认 ====="
grep -E '^VERSION=' .env.demo
grep -q "^VERSION=$NEW$" .env.demo || { echo "VERSION_MISMATCH"; exit 9; }

echo "===== [C2] up -d（backend/worker/frontend） ====="
docker compose --env-file .env.demo -f docker-compose.demo.yml up -d itsm-backend-demo itsm-worker-demo itsm-frontend-demo 2>&1 | tail -12

echo "===== [C3] 容器快照 ====="
sleep 6
docker ps -a --format '{{.Names}}|{{.Image}}|{{.Status}}' | grep demo | sort
echo "C-STATUS: DONE"
