#!/usr/bin/env bash
# 238 demo 增量修复 A 步：SLA MSPContext 装配修复（验收修复，IM-36）@${NEW}
# checkout → .env.demo VERSION → retag ai-service → 后台顺序构建 backend -> frontend
set -u
R=/home/n9e/itsm/repo
cd "$R" || { echo "NO_REPO"; exit 1; }
NEW="${NEW:?NEW required}"
OLD="${OLD:-1.8.0-dev.c700c060}"
BRANCH="${BRANCH:-deploy-demo-slafix}"
BUNDLE="${BUNDLE:?BUNDLE required}"

echo "===== [A0] 磁盘与预备 ====="
df -h / | tail -1
docker image prune -f 2>&1 | tail -1

echo "===== [A1] fetch/checkout ====="
git fetch "$BUNDLE" HEAD || { echo "FETCH_FAILED"; exit 9; }
git checkout -f -B "$BRANCH" FETCH_HEAD || { echo "CHECKOUT_FAILED"; exit 9; }
git log --oneline -2
echo "HEAD-SHA=$(git rev-parse HEAD)"

echo "===== [A2] .env.demo VERSION -> $NEW ====="
cp .env.demo ".env.demo.bak-$(date +%Y%m%d-%H%M%S)"
sed -i "s/^VERSION=.*/VERSION=$NEW/" .env.demo
grep -E '^(VERSION|DEMO_DB_NAME|DEMO_HTTP_PORT|RLS_MODE|DEPLOYMENT_MODE)=' .env.demo

echo "===== [A3] retag ai-service（代码未变，仅 tag 对齐） ====="
docker tag "itsm-ai-service:$OLD" "itsm-ai-service:$NEW" && echo "retag-ai-ok" || echo "[warn] retag-ai-failed"

echo "===== [A4] 启动后台顺序构建（backend -> frontend） ====="
export DOCKER_BUILDKIT=1
rm -f /tmp/build-fix.log /tmp/build-fix-backend.log /tmp/build-fix-frontend.log
setsid nohup bash -c "cd $R && VERSION=$NEW docker compose --env-file .env.prod -f docker-compose.prod.yml build itsm-backend > /tmp/build-fix-backend.log 2>&1; B=\$?; echo BUILD_BACKEND_EXIT=\$B >> /tmp/build-fix.log; if [ \"\$B\" = \"0\" ]; then VERSION=$NEW docker compose --env-file .env.prod -f docker-compose.prod.yml build itsm-frontend > /tmp/build-fix-frontend.log 2>&1; F=\$?; else F=99; fi; echo BUILD_FRONTEND_EXIT=\$F >> /tmp/build-fix.log; echo BUILD_ALL_DONE >> /tmp/build-fix.log" >/dev/null 2>&1 &
sleep 8
echo "A-STATUS: DONE"
