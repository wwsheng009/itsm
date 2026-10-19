#!/usr/bin/env bash
# 238 demo 运行时验证：登录 → 新端点冒烟 → 遥测写入/落库 → 新表 → 日志扫描（d51ed172）
# 用法：bash /tmp/deploy238_dock_verify.sh
set -u
R=/home/n9e/itsm/repo
cd "$R" || { echo "NO_REPO"; exit 1; }
BASE=http://127.0.0.1:8089
CK=/tmp/dock-ck.txt
ADMIN_PW=$(grep '^DEMO_ADMIN_PASSWORD=' .env.demo | cut -d= -f2-)

echo "===== [V1] login (admin) ====="
LOGIN=$(curl -s --max-time 15 -c "$CK" -X POST -H 'Content-Type: application/json' \
  -d "{\"username\":\"admin\",\"password\":\"$ADMIN_PW\"}" "$BASE/api/v1/auth/login")
echo "$LOGIN" | head -c 220; echo
echo "cookie_lines=$(wc -l < "$CK" 2>/dev/null || echo 0)"

echo "===== [V2] 新端点冒烟（GET，期望 200） ====="
for u in "/api/v1/ai/assistant/context?pageKey=tickets.list" \
         "/api/v1/ai/help/search?q=%E5%B7%A5%E5%8D%95&limit=3" \
         "/api/v1/ai/help/coverage" \
         "/api/v1/ai/help/todos" \
         "/api/v1/ai/telemetry/summary?days=7"; do
  code=$(curl -s -o /tmp/dock-v2.out -w '%{http_code}' --max-time 20 -b "$CK" "$BASE$u")
  echo "$u -> $code | $(head -c 160 /tmp/dock-v2.out)"
done

echo "===== [V3] 遥测写入（先取 CSRF token；2 有效 + 1 未知事件） ====="
CSRF_JSON=$(curl -s --max-time 15 -b "$CK" -c "$CK" "$BASE/api/v1/csrf-token")
CSRF=$(echo "$CSRF_JSON" | grep -o '"csrf_token":"[^"]*"' | head -1 | cut -d'"' -f4)
echo "csrf_len=${#CSRF}"
POST=$(curl -s --max-time 20 -b "$CK" -X POST -H "X-CSRF-Token: $CSRF" -H 'Content-Type: application/json' \
  -d '{"events":[{"name":"assistant_opened","pageKey":"verify.dock","entrypoint":"dock"},{"name":"unknown_event_should_drop","pageKey":"verify.dock"},{"name":"help_opened","pageKey":"verify.dock","entrypoint":"help"}]}' \
  "$BASE/api/v1/ai/telemetry/events")
echo "$POST"; echo

echo "===== [V4] 遥测落库（psql） ====="
docker exec itsm-postgres-prod psql -U itsm -d itsm_demo -tAc "select count(*) from ai_telemetry_events where page_key='verify.dock'" 2>&1
docker exec itsm-postgres-prod psql -U itsm -d itsm_demo -tAc "select name, count(*) from ai_telemetry_events group by name order by 2 desc limit 5" 2>&1

echo "===== [V4b] summary 反映（pageKey=verify.dock） ====="
curl -s --max-time 20 -b "$CK" "$BASE/api/v1/ai/telemetry/summary?days=7&pageKey=verify.dock" | head -c 400; echo

echo "===== [V5] 新表存在性 ====="
docker exec itsm-postgres-prod psql -U itsm -d itsm_demo -tAc "select to_regclass('public.ai_telemetry_events'), to_regclass('public.ai_doc_todo_states'), to_regclass('public.msp_help_docs'), to_regclass('public.msp_help_distributions')" 2>&1

echo "===== [V6] 日志扫描（backend/worker，近 10m） ====="
docker logs --since 10m itsm-backend-demo 2>&1 | grep -iE 'panic|fatal|level=error|"level":"error"' | head -8
echo "--- worker ---"
docker logs --since 10m itsm-worker-demo 2>&1 | grep -iE 'panic|fatal|level=error|"level":"error"' | head -8
echo "VERIFY-DONE"
