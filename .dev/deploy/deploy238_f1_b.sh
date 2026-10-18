#!/usr/bin/env bash
# 238 demo 更新 B 步：仅构建 backend 镜像（本次改动纯后端；前端在 C 步用旧镜像打新 tag）
set -u
R=/home/n9e/itsm/repo
cd "$R" || { echo "NO_REPO"; exit 9; }
NEW="${NEW:?NEW required}"
LOG="/tmp/build-f1-${NEW}.log"
rm -f "$LOG"
setsid nohup bash -c "cd $R && VERSION=$NEW docker compose --env-file .env.prod -f docker-compose.prod.yml build itsm-backend >> $LOG 2>&1; echo BUILD_EXIT=\$? >> $LOG" >/dev/null 2>&1 &
echo "BUILD_STARTED log=$LOG"
