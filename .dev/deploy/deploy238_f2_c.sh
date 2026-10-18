#!/usr/bin/env bash
# 238 demo 更新 C 步：up -d（backend/worker/frontend/ai-service）+ 健康检查 + A/B 指纹（F2）
set -u
R=/home/n9e/itsm/repo
cd "$R" || { echo "NO_REPO"; exit 1; }
NEW="${NEW:?NEW required}"
OLD_BE="${OLD_BE:-1.8.0-dev.eb214870}"
OLD_AI="${OLD_AI:-1.8.0-dev.eb214870}"
OLD_FE="${OLD_FE:-1.8.0-dev.eb214870}"

echo "===== [C0] 镜像存在性 ====="
docker image inspect "itsm-backend:$NEW"    >/dev/null 2>&1 && echo "ok itsm-backend:$NEW"    || { echo "MISSING backend"; exit 9; }
docker image inspect "itsm-ai-service:$NEW" >/dev/null 2>&1 && echo "ok itsm-ai-service:$NEW" || { echo "MISSING ai-service"; exit 9; }
if docker image inspect "itsm-frontend:$NEW" >/dev/null 2>&1; then
  echo "ok itsm-frontend:$NEW (exists)"
else
  docker tag "itsm-frontend:$OLD_FE" "itsm-frontend:$NEW" && echo "aliased itsm-frontend:$OLD_FE -> $NEW"
fi
grep -q "^VERSION=$NEW$" .env.demo || { echo "VERSION_MISMATCH"; grep '^VERSION=' .env.demo; exit 9; }

echo "===== [C1] up -d（backend/worker/frontend/ai-service） ====="
docker compose --env-file .env.demo -f docker-compose.demo.yml up -d itsm-backend-demo itsm-worker-demo itsm-frontend-demo itsm-ai-service-demo 2>&1 | tail -15

echo "===== [C2] 容器快照 ====="
sleep 10
docker ps -a --format '{{.Names}}|{{.Image}}|{{.Status}}' | grep demo | sort

echo "===== [C3] 健康检查 + HTTP ====="
for i in $(seq 1 12); do
  ST=$(docker inspect -f '{{.State.Health.Status}}' itsm-backend-demo 2>/dev/null || echo missing)
  echo "backend-demo health: $ST"
  [ "$ST" = "healthy" ] && break
  sleep 10
done
for i in $(seq 1 12); do
  ST=$(docker inspect -f '{{.State.Health.Status}}' itsm-ai-service-demo 2>/dev/null || echo missing)
  echo "ai-service-demo health: $ST"
  [ "$ST" = "healthy" ] && break
  sleep 10
done
curl -s -o /dev/null -w 'root=%{http_code}\n' --max-time 15 http://127.0.0.1:8089/
curl -s -o /dev/null -w 'health=%{http_code}\n' --max-time 15 http://127.0.0.1:8089/api/v1/health
curl -s --max-time 15 http://127.0.0.1:8089/api/v1/version; echo

echo "===== [C4] A/B 新代码指纹 ====="
old_be_ref="${OLD_BE_REF:-itsm-backend:$OLD_BE}"
new_be_ref="itsm-backend:$NEW"
# 旧 ai-service 镜像可能已是悬空镜像（无 repo:tag），支持用 OLD_AI_REF 传整引用/镜像 ID。
old_ai_ref="${OLD_AI_REF:-itsm-ai-service:$OLD_AI}"
new_ai_ref="itsm-ai-service:$NEW"
for spec in "old-backend-sidecar-target-marker|$old_be_ref|/app/main|sidecar LLM target" \
            "new-backend-sidecar-target-marker|$new_be_ref|/app/main|sidecar LLM target" \
            "old-ai-override-marker|$old_ai_ref|/app/api/guidance.py|_call_llm_override" \
            "new-ai-override-marker|$new_ai_ref|/app/api/guidance.py|_call_llm_override"; do
  IFS='|' read -r label img path pat <<< "$spec"
  echo -n "$label="
  docker run --rm --entrypoint sh "$img" -c "test -f $path || { echo file-missing; exit 0; }; grep -ac '$pat' $path; true"
done
echo "C-STATUS: DONE"
