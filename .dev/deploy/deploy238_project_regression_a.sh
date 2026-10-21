#!/usr/bin/env bash
# 238 demo 发布 A 步：回归遗留修复（cb6142f1）fetch/checkout + .env.demo VERSION
set -u
R=/home/n9e/itsm/repo
NEW="${NEW:?NEW required}"
BRANCH="${BRANCH:-deploy-demo-project-regression-cb6142f1}"
BUNDLE="${BUNDLE:-/tmp/itsm-project-regression-cb6142f1.bundle}"
PREREQ="${PREREQ:-0aa276dad44fc6062f70499c18e4f37f8dd9e795}"
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
test -f itsm-backend/handlers/project/owner_team_handler.go && echo "ok owner_team_handler.go"
test -f itsm-backend/handlers/project/review_regression_20261021_test.go && echo "ok regression test"
grep -c "FieldDepartmentID" itsm-backend/handlers/project/report_repository_impl.go
grep -c "owner-team-candidates" itsm-backend/router/common_system_routes.go
grep -c "ownerTeamCandidates" "itsm-frontend/src/pages/(main)/projects/index.tsx"
grep -c "fixed: 'right'" "itsm-frontend/src/pages/(main)/projects/detail.tsx"
grep -c "归口团队" "itsm-frontend/src/pages/(main)/projects/index.tsx"
echo "A-STATUS: DONE"
