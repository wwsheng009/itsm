#!/usr/bin/env bash
# 238 demo 发布 B 步：构建镜像（50a1831b；后台）
set -u
R=/home/n9e/itsm/repo
cd "$R" || { echo "NO_REPO"; exit 9; }
NEW="${NEW:?NEW required}"
LOG="/tmp/build-project-round3-${NEW}.log"
rm -f "$LOG"

df -h / | tail -1
echo 'passw0rd' | sudo -S -p '' docker image prune -f | tail -2

export DOCKER_BUILDKIT=1
setsid nohup bash -c "cd $R && echo 'passw0rd' | sudo -S -p '' env VERSION=$NEW docker compose --env-file .env.prod -f docker-compose.prod.yml build itsm-backend itsm-frontend >> $LOG 2>&1; echo BUILD_EXIT=\$? >> $LOG" >/dev/null 2>&1 &
sleep 12
tail -6 "$LOG" 2>/dev/null || echo "(log pending)"
echo "BUILD_STARTED log=$LOG"
