#!/usr/bin/env bash
# 238 demo 发布 A 步：第三轮回归修复（50a1831b）
set -u
R=/home/n9e/itsm/repo
NEW="${NEW:?NEW required}"
BRANCH="${BRANCH:-deploy-demo-project-round3-50a1831b}"
BUNDLE="${BUNDLE:-/tmp/itsm-project-round3-50a1831b.bundle}"
PREREQ="${PREREQ:-b3f9762c3e620df15117c9746c9671df9adc8f17}"
cd "$R" || { echo "NO_REPO"; exit 1; }

echo "===== [A0] pre-state ====="
git log --oneline -1
echo "pre-branch=$(git branch --show-current)"
git cat-file -e "$PREREQ" 2>/dev/null && echo "prereq ok" || { echo "PREREQ_MISSING"; exit 9; }
echo 'passw0rd' | sudo -S -p '' docker ps --format '{{.Names}}|{{.Image}}|{{.Status}}' | grep demo | sort

echo "===== [A1] fetch/checkout ====="
git fetch "$BUNDLE" "refs/heads/$BRANCH:refs/heads/$BRANCH" || { echo "FETCH_FAILED"; exit 9; }
git checkout -f "$BRANCH" || { echo "CHECKOUT_FAILED"; exit 9; }
echo "HEAD-SHA=$(git rev-parse HEAD)"
git log --oneline -2

echo "===== [A2] .env.demo VERSION -> $NEW ====="
cp .env.demo ".env.demo.bak-$(date +%Y%m%d-%H%M%S)"
sed -i "s/^VERSION=.*/VERSION=$NEW/" .env.demo
grep -E '^(VERSION|DEMO_DB_NAME|DEMO_HTTP_PORT)=' .env.demo
grep -q "^VERSION=$NEW$" .env.demo || { echo "VERSION_SET_FAILED"; exit 9; }

echo "===== [A3] 新代码落树核对 ====="
grep -n "MaxTaskHours = " itsm-backend/handlers/project/budget_entity.go
grep -c "InvalidHoursTasks" itsm-backend/handlers/project/repository_impl.go
test -f itsm-backend/handlers/project/review_round3_20261021_test.go && echo "ok round3 test"
grep -c "TASK_HOURS_MAX" "itsm-frontend/src/pages/(main)/projects/detail.tsx"
grep -c "已排除" "itsm-frontend/src/pages/(main)/projects/detail.tsx"
grep -c "minWidth: 260" "itsm-frontend/src/pages/(main)/projects/detail.tsx"
grep -c "fixed: 'right'" "itsm-frontend/src/pages/(main)/projects/index.tsx"
echo "A-STATUS: DONE"
