#!/usr/bin/env bash
# 238 demo 全栈更新 A 步：SLA 专项 P2-2+P2-3+P3 @1.8.0-dev.c700c060（增量 1e60efee..c700c060）
# checkout → .env.demo VERSION → retag ai-service → 后台顺序构建 backend -> frontend
set -u
R=/home/n9e/itsm/repo
cd "$R" || { echo "NO_REPO"; exit 1; }
NEW="${NEW:?NEW required}"
OLD="${OLD:-1.8.0-dev.1e60efee}"
BRANCH="${BRANCH:-deploy-demo-c700c060}"
BUNDLE="${BUNDLE:-/tmp/itsm-1.8.0-dev.c700c060-full.bundle}"

echo "===== [A0] 磁盘与预备 ====="
df -h / | tail -1
docker image prune -f 2>&1 | tail -1

echo "===== [A1] fetch/checkout ====="
git fetch "$BUNDLE" HEAD || { echo "FETCH_FAILED"; exit 9; }
git checkout -f -B "$BRANCH" FETCH_HEAD || { echo "CHECKOUT_FAILED"; exit 9; }
git log --oneline -1
git status --short | head -3
echo "HEAD-SHA=$(git rev-parse HEAD)"

echo "===== [A2] .env.demo VERSION -> $NEW ====="
cp .env.demo ".env.demo.bak-$(date +%Y%m%d-%H%M%S)"
sed -i "s/^VERSION=.*/VERSION=$NEW/" .env.demo
grep -E '^(VERSION|DEMO_DB_NAME|DEMO_HTTP_PORT|RLS_MODE|DEPLOYMENT_MODE)=' .env.demo

echo "===== [A3] retag ai-service（代码未变，仅 tag 对齐） ====="
docker tag "itsm-ai-service:$OLD" "itsm-ai-service:$NEW" && echo "retag-ai-ok" || echo "[warn] retag-ai-failed"

echo "===== [A4] 启动后台顺序构建（backend -> frontend） ====="
export DOCKER_BUILDKIT=1
rm -f /tmp/build-full-c700c060.log /tmp/build-full-backend.log /tmp/build-full-frontend.log
setsid nohup bash -c "cd $R && VERSION=$NEW docker compose --env-file .env.prod -f docker-compose.prod.yml build itsm-backend > /tmp/build-full-backend.log 2>&1; B=\$?; echo BUILD_BACKEND_EXIT=\$B >> /tmp/build-full-c700c060.log; if [ \"\$B\" = \"0\" ]; then VERSION=$NEW docker compose --env-file .env.prod -f docker-compose.prod.yml build itsm-frontend > /tmp/build-full-frontend.log 2>&1; F=\$?; else F=99; fi; echo BUILD_FRONTEND_EXIT=\$F >> /tmp/build-full-c700c060.log; echo BUILD_ALL_DONE >> /tmp/build-full-c700c060.log" >/dev/null 2>&1 &
sleep 10
tail -3 /tmp/build-full-c700c060.log 2>/dev/null || echo "(status pending)"
echo "A-STATUS: DONE"
