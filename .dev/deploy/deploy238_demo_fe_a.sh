#!/usr/bin/env bash
# demo 前端更新 A 步：fetch/checkout 新提交 → 更新 VERSION → retag backend/ai
# 241014: dashboard KPI 磁贴点击下转（b86819be）
set -u
R=/home/n9e/itsm/repo
cd "$R" || { echo "NO_REPO"; exit 1; }
NEW="${NEW:?NEW required}"
OLD="${OLD:-1.8.0-dev.22ee3603}"
BRANCH="${BRANCH:-deploy-demo-fe-b86819be}"
BUNDLE="${BUNDLE:-/tmp/itsm-demo-frontend-b86819be.bundle}"

echo "===== [A1] fetch/checkout ====="
git fetch "$BUNDLE" HEAD || { echo "FETCH_FAILED"; exit 9; }
git checkout -f -B "$BRANCH" FETCH_HEAD || { echo "CHECKOUT_FAILED"; exit 9; }
git log --oneline -1

echo "===== [A2] .env.demo VERSION -> $NEW ====="
cp .env.demo ".env.demo.bak-$(date +%Y%m%d-%H%M%S)"
sed -i "s/^VERSION=.*/VERSION=$NEW/" .env.demo
grep -E '^(VERSION|DEMO_DB_NAME|DEMO_HTTP_PORT)=' .env.demo

echo "===== [A3] retag backend/ai（代码未变，仅前端改动） ====="
docker tag "itsm-backend:$OLD" "itsm-backend:$NEW" && echo "retag-backend-ok"
docker tag "itsm-ai-service:$OLD" "itsm-ai-service:$NEW" && echo "retag-ai-ok"

echo "===== [A4] 后台构建前端镜像 ====="
export DOCKER_BUILDKIT=1
rm -f /tmp/build-demo-fe-b86819be.log
setsid nohup bash -c "cd $R && VERSION=$NEW docker compose --env-file .env.demo -f docker-compose.demo.yml build itsm-frontend >> /tmp/build-demo-fe-b86819be.log 2>&1; echo BUILD_EXIT=\$? >> /tmp/build-demo-fe-b86819be.log" >/dev/null 2>&1 &
sleep 8
tail -6 /tmp/build-demo-fe-b86819be.log 2>/dev/null || echo "(log pending)"
echo "FE-BUILD-LAUNCHED"
