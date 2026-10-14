#!/usr/bin/env bash
# 238 demo 增量修复 W 步：构建轮询（等待 BUILD_ALL_DONE，最多约 12 分钟）
set -u
NEW="${NEW:?NEW required}"
for i in $(seq 1 48); do
  if grep -q 'BUILD_ALL_DONE' /tmp/build-fix.log 2>/dev/null; then break; fi
  sleep 15
done
echo "== status =="
cat /tmp/build-fix.log 2>/dev/null || echo "(status pending)"
echo "== backend tail =="
tail -4 /tmp/build-fix-backend.log 2>/dev/null || echo "(backend log pending)"
echo "== frontend tail =="
tail -4 /tmp/build-fix-frontend.log 2>/dev/null || echo "(frontend log pending)"
echo "== images =="
docker image inspect "itsm-backend:$NEW" --format 'backend-image={{.Id}}' 2>/dev/null || echo "backend=IMAGE-PENDING"
docker image inspect "itsm-frontend:$NEW" --format 'frontend-image={{.Id}}' 2>/dev/null || echo "frontend=IMAGE-PENDING"
echo "W-STATUS: END"
