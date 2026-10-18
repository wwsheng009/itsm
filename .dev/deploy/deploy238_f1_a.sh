#!/usr/bin/env bash
# 238 demo 更新 A 步：F1 分诊降级回落修复（eb214870）
set -u
R=/home/n9e/itsm/repo
cd "$R" || { echo "NO_REPO"; exit 1; }
NEW="${NEW:?NEW required}"
BRANCH="${BRANCH:-deploy-demo-f1-eb214870}"
BUNDLE="${BUNDLE:-/tmp/itsm-f1-eb214870.bundle}"

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
