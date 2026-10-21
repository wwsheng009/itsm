#!/usr/bin/env bash
# 238 demo 发布 A 步：第四轮回归修复（36992fe7）
set -u
R=/home/n9e/itsm/repo
NEW="${NEW:?NEW required}"
BRANCH="${BRANCH:-deploy-demo-project-round4-36992fe7}"
BUNDLE="${BUNDLE:-/tmp/itsm-project-round4-36992fe7.bundle}"
PREREQ="${PREREQ:-50a1831b17c58f9008fc0f2bd6ba611464c431fa}"
cd "$R" || { echo "NO_REPO"; exit 1; }

echo "===== [A0] pre-state ====="
git log --oneline -1
echo "pre-branch=$(git branch --show-current)"
git cat-file -e "$PREREQ" 2>/dev/null && echo "prereq ok" || { echo "PREREQ_MISSING"; exit 9; }

echo "===== [A1] fetch/checkout ====="
git fetch "$BUNDLE" "refs/heads/$BRANCH:refs/heads/$BRANCH" || { echo "FETCH_FAILED"; exit 9; }
git checkout -f "$BRANCH" || { echo "CHECKOUT_FAILED"; exit 9; }
echo "HEAD-SHA=$(git rev-parse HEAD)"
git log --oneline -2

echo "===== [A2] VERSION ====="
cp .env.demo ".env.demo.bak-$(date +%Y%m%d-%H%M%S)"
sed -i "s/^VERSION=.*/VERSION=$NEW/" .env.demo
grep -E '^(VERSION|DEMO_DB_NAME|DEMO_HTTP_PORT)=' .env.demo
grep -q "^VERSION=$NEW$" .env.demo || { echo "VERSION_SET_FAILED"; exit 9; }

echo "===== [A3] 落树核对 ====="
grep -c "overflow: hidden" itsm-frontend/src/styles/globals.css
grep -n "B1（第四轮根因）" "itsm-frontend/src/pages/(main)/projects/detail.tsx"
grep -c "onHeaderCell" "itsm-frontend/src/pages/(main)/projects/detail.tsx"
grep -c "归属与计划" "itsm-frontend/src/pages/(main)/projects/index.tsx"
test -f "itsm-frontend/src/pages/(main)/projects/__tests__/task-hours-limit.test.ts" && echo "ok guard test"
# 关键：工时控件不得再有 max
awk '/name="estimateHours"/,/<\/Form.Item>/' "itsm-frontend/src/pages/(main)/projects/detail.tsx" | grep -c "max=" || echo "no-max-in-estimateHours(ok)"
echo "A-STATUS: DONE"
