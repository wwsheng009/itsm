#!/usr/bin/env bash
# 238 demo 发布 B 步：构建 itsm-backend + itsm-frontend 镜像（0aa276da；后台，日志轮询）
# 用法：NEW=1.8.0-dev.0aa276da bash /tmp/deploy238_project_review_b.sh
set -u
R=/home/n9e/itsm/repo
cd "$R" || { echo "NO_REPO"; exit 9; }
NEW="${NEW:?NEW required}"
LOG="/tmp/build-project-review-${NEW}.log"
rm -f "$LOG"

echo "===== [B0] 磁盘与清理 ====="
df -h / | tail -1
echo 'passw0rd' | sudo -S -p '' docker image prune -f | tail -2
df -h / | tail -1

echo "===== [B1] 启动后台构建（backend+frontend，prod compose 上下文） ====="
export DOCKER_BUILDKIT=1
setsid nohup bash -c "cd $R && echo 'passw0rd' | sudo -S -p '' env VERSION=$NEW docker compose --env-file .env.prod -f docker-compose.prod.yml build itsm-backend itsm-frontend >> $LOG 2>&1; echo BUILD_EXIT=\$? >> $LOG" >/dev/null 2>&1 &
sleep 12
tail -10 "$LOG" 2>/dev/null || echo "(log pending)"
pgrep -fa 'docker compose.*build' | head -3
echo "BUILD_STARTED log=$LOG"
