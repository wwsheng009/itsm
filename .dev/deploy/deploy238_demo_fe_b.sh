#!/usr/bin/env bash
# demo 前端更新 B2 步：用 .env.prod 上下文构建 itsm-frontend:$NEW（同 L3 模式）
# 241014: dashboard KPI 磁贴点击下转（b86819be）
set -u
R=/home/n9e/itsm/repo
cd "$R" || { echo "NO_REPO"; exit 1; }
NEW="${NEW:?NEW required}"

echo "===== [B2-1] 后台构建 itsm-frontend:$NEW (.env.prod 上下文) ====="
export DOCKER_BUILDKIT=1
export VERSION="$NEW"
rm -f /tmp/build-demo-fe-b86819be.log
setsid nohup bash -c "cd $R && VERSION=$NEW docker compose --env-file .env.prod -f docker-compose.prod.yml build itsm-frontend >> /tmp/build-demo-fe-b86819be.log 2>&1; echo BUILD_EXIT=\$? >> /tmp/build-demo-fe-b86819be.log" >/dev/null 2>&1 &
sleep 12
tail -8 /tmp/build-demo-fe-b86819be.log 2>/dev/null || echo "(log pending)"
pgrep -fa 'docker compose.*build' | head -2
echo "FE-BUILD-LAUNCHED"
