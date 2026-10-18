#!/usr/bin/env bash
# 238 demo 更新 B 步：构建 backend + itsm-ai-service 两个镜像（F2 双镜像）
# 注意：itsm-ai-service 需要 pip 安装 torch(CPU) 等依赖；若宿主已有同 requirements 的
#       builder 缓存层则只重建 COPY 层，否则耗时较长（后台跑，用日志轮询）。
set -u
R=/home/n9e/itsm/repo
cd "$R" || { echo "NO_REPO"; exit 9; }
NEW="${NEW:?NEW required}"
LOG="/tmp/build-f2-${NEW}.log"
rm -f "$LOG"
setsid nohup bash -c "cd $R && VERSION=$NEW docker compose --env-file .env.prod -f docker-compose.prod.yml build itsm-backend itsm-ai-service >> $LOG 2>&1; echo BUILD_EXIT=\$? >> $LOG" >/dev/null 2>&1 &
echo "BUILD_STARTED log=$LOG"