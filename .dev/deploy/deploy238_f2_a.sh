#!/usr/bin/env bash
# 238 demo 更新 A 步：F2 应用内 LLM 配置请求级下发 guidance sidecar（c05cc49e）
set -u
R=/home/n9e/itsm/repo
cd "$R" || { echo "NO_REPO"; exit 1; }
NEW="${NEW:?NEW required}"
BRANCH="${BRANCH:-deploy-demo-f2-c05cc49e}"
BUNDLE="${BUNDLE:-/tmp/itsm-llmdown-c05cc49e.bundle}"
PREREQ="${PREREQ:-eb214870d3c0df3f02e94f73b84f31b962fd6cba}"

echo "===== [A0] 前置版本（bundle prerequisite）存在性 ====="
if git cat-file -e "$PREREQ" 2>/dev/null; then
  echo "prereq ok: $PREREQ"
else
  echo "PREREQ_MISSING: $PREREQ（fetch 可能失败，需换全量 bundle）"
fi

echo "===== [A1] fetch/checkout ====="
git fetch "$BUNDLE" "refs/heads/feat/msp-p0-hardening:refs/heads/$BRANCH" || { echo "FETCH_FAILED"; exit 9; }
git checkout -f "$BRANCH" || { echo "CHECKOUT_FAILED"; exit 9; }
git log --oneline -3
echo "HEAD-SHA=$(git rev-parse HEAD)"

echo "===== [A2] .env.demo VERSION ====="
cp .env.demo ".env.demo.bak-$(date +%Y%m%d-%H%M%S)"
sed -i "s/^VERSION=.*/VERSION=$NEW/" .env.demo
grep -E '^(VERSION|LLM_TIMEOUT|DEMO_DB_NAME|DEMO_HTTP_PORT|RLS_MODE|DEPLOYMENT_MODE)=' .env.demo
echo "A-STATUS: DONE"