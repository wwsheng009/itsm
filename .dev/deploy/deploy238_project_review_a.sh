#!/usr/bin/env bash
# 238 demo 发布 A 步：项目功能审查修复（0aa276da）fetch/checkout + .env.demo VERSION
# 用法：NEW=1.8.0-dev.0aa276da bash /tmp/deploy238_project_review_a.sh
set -u
R=/home/n9e/itsm/repo
NEW="${NEW:?NEW required}"
BRANCH="${BRANCH:-deploy-demo-project-review-0aa276da}"
BUNDLE="${BUNDLE:-/tmp/itsm-project-review-0aa276da.bundle}"
PREREQ="${PREREQ:-3cfc1dccba549b451ab9499177e6faceabdc0332}"
cd "$R" || { echo "NO_REPO"; exit 1; }

echo "===== [A0] pre-state ====="
git log --oneline -1
echo "pre-branch=$(git branch --show-current)"
if git cat-file -e "$PREREQ" 2>/dev/null; then echo "prereq ok: $PREREQ"; else echo "PREREQ_MISSING: $PREREQ"; exit 9; fi
echo "pre-docker-ps:"
echo 'passw0rd' | sudo -S -p '' docker ps --format '{{.Names}}|{{.Image}}|{{.Status}}' | grep demo | sort

echo "===== [A1] fetch/checkout ====="
git fetch "$BUNDLE" "refs/heads/$BRANCH:refs/heads/$BRANCH" || { echo "FETCH_FAILED"; exit 9; }
git checkout -f "$BRANCH" || { echo "CHECKOUT_FAILED"; exit 9; }
echo "HEAD-SHA=$(git rev-parse HEAD)"
git log --oneline -3

echo "===== [A2] .env.demo VERSION -> $NEW ====="
cp .env.demo ".env.demo.bak-$(date +%Y%m%d-%H%M%S)"
sed -i "s/^VERSION=.*/VERSION=$NEW/" .env.demo
grep -E '^(VERSION|DEMO_DB_NAME|DEMO_HTTP_PORT|RLS_MODE|DEPLOYMENT_MODE)=' .env.demo
grep -q "^VERSION=$NEW$" .env.demo || { echo "VERSION_SET_FAILED"; exit 9; }

echo "===== [A3] 新代码落树核对 ====="
test -f itsm-backend/handlers/project/business_time.go && echo "ok business_time.go"
test -f itsm-backend/handlers/project/display_service.go && echo "ok display_service.go"
test -f itsm-backend/handlers/project/review_20261021_test.go && echo "ok review_20261021_test.go"
grep -c "SetStrictCreate(true)" itsm-backend/internal/bootstrap/app.go
grep -c "autoApprovalConfigMissing" itsm-backend/handlers/project/gate_entity.go
grep -c "gateDecisionAutoApproved" itsm-backend/handlers/project/p2_metrics_service.go
grep -c "formatBusinessDateTime" "itsm-frontend/src/lib/formatters.ts"
grep -c "closureBlockerTotal" "itsm-frontend/src/pages/(main)/projects/detail.tsx"
test -f "docs/project-management/reviews/2026-10-21-功能审查修复台账.md" && echo "ok review ledger"
echo "A-STATUS: DONE"
