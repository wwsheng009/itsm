#!/usr/bin/env bash
# 238 demo 发布 A 步：ai-assistant-dock-plan 全量交付（d51ed172）fetch/checkout + .env.demo VERSION
# 用法：NEW=1.8.0-dev.d51ed172 bash /tmp/deploy238_dock_a.sh
set -u
R=/home/n9e/itsm/repo
NEW="${NEW:?NEW required}"
BRANCH="${BRANCH:-deploy-demo-dock-d51ed172}"
BUNDLE="${BUNDLE:-/tmp/itsm-dock-d51ed172.bundle}"
PREREQ="${PREREQ:-c05cc49ecfdab2364c183a53c47c6dcf5f1cdfdf}"
cd "$R" || { echo "NO_REPO"; exit 1; }

echo "===== [A0] pre-state ====="
git log --oneline -1
echo "pre-branch=$(git branch --show-current)"
if git cat-file -e "$PREREQ" 2>/dev/null; then echo "prereq ok: $PREREQ"; else echo "PREREQ_MISSING: $PREREQ"; fi
echo -n "pre-ledger-demo="; docker exec itsm-postgres-prod psql -U itsm -d itsm_demo -tAc "select count(*) from schema_migrations" 2>&1 | head -1
echo "pre-docker-ps:"; docker ps --format '{{.Names}}|{{.Image}}|{{.Status}}' | grep demo | sort

echo "===== [A1] fetch/checkout ====="
git fetch "$BUNDLE" "refs/heads/feat/msp-p0-hardening:refs/heads/$BRANCH" || { echo "FETCH_FAILED"; exit 9; }
git checkout -f "$BRANCH" || { echo "CHECKOUT_FAILED"; exit 9; }
echo "HEAD-SHA=$(git rev-parse HEAD)"
git log --oneline -3

echo "===== [A2] .env.demo VERSION -> $NEW ====="
cp .env.demo ".env.demo.bak-$(date +%Y%m%d-%H%M%S)"
sed -i "s/^VERSION=.*/VERSION=$NEW/" .env.demo
grep -E '^(VERSION|DEMO_DB_NAME|DEMO_HTTP_PORT|RLS_MODE|DEPLOYMENT_MODE)=' .env.demo
grep -q "^VERSION=$NEW$" .env.demo || { echo "VERSION_SET_FAILED"; exit 9; }

echo "===== [A3] 本批迁移清单（20261018*） ====="
ls -1 itsm-backend/migrations/ 2>/dev/null | grep -E '^20261018' | sort
echo "A-STATUS: DONE"
