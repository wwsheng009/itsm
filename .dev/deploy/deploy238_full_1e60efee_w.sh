#!/usr/bin/env bash
# 238 demo 全栈更新 W 步：构建轮询（只读，可重复执行）
set -u
echo "== status =="
cat /tmp/build-full-1e60efee.log 2>/dev/null || echo "(status pending)"
echo "== backend tail =="
tail -4 /tmp/build-full-backend.log 2>/dev/null || echo "(backend log pending)"
echo "== frontend tail =="
tail -4 /tmp/build-full-frontend.log 2>/dev/null || echo "(frontend log pending)"
echo "== images =="
docker image inspect itsm-backend:1.8.0-dev.1e60efee --format 'backend={{.Id}}' 2>/dev/null || echo "backend=IMAGE-PENDING"
docker image inspect itsm-frontend:1.8.0-dev.1e60efee --format 'frontend={{.Id}}' 2>/dev/null || echo "frontend=IMAGE-PENDING"
pgrep -fa "docker compose.*build" | head -2 || true
echo "W-STATUS: END"
